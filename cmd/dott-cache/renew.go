package main

// Lệnh renew (bản thử 28/11): tạo lại cache cho 1 URL hoặc một danh sách URL.
//
//	dott-cache renew -site php81.congtoan.net -url https://php81.congtoan.net/tttest/abc/
//	dott-cache renew -site php81.congtoan.net -paths list.txt [-limit N] [-keep-s3] [-no-delete] [-log out.jsonl]
//
// Các bước (theo thứ tự, dừng nếu một bước hỏng toàn bộ):
//  1. Xóa object cũ trên MỌI đích S3 (index-https.html và index-mobile-https.html). Bỏ qua bước này với -keep-s3.
//     Trong lúc chờ, Worker không thấy object nên chuyển khách sang origin, khách không bị lỗi.
//  2. Gửi danh sách path qua ssh tới VPS chính (cfg.renew.command = "/usr/local/sbin/dott-prefork <domain>",
//     đọc path từ stdin). prefork-render.php của site xóa file cache cũ của từng URL ngay trước khi
//     render URL đó, và bootstrap WordPress một lần cho mỗi blog.
//  3. Path nào prefork báo ok thì lấy HTML qua HTTP (lúc này nginx trả file vừa tạo) và upload lên mọi đích.
//
// -mode auto (mặc định): tối đa -http-max URL (mặc định 4) thì dùng "http", nhiều hơn thì dùng "prefork".
//   http:    bước 2 chỉ xóa file cache cũ (dott-prefork --delete-only, không nạp WordPress), bước 3 gửi
//            GET để PHP-FPM render như khách thật và WP Rocket tự ghi cache. Không phải chờ khóa prefork.
//   prefork: như trên; URL nào prefork lỗi (không phải 404 hay path sai) thì tự thử lại bằng HTTP.

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"
)

type preforkLine struct {
	Path      string  `json:"path"`
	OK        bool    `json:"ok"`
	Err       string  `json:"err"`
	Ms        float64 `json:"ms"`
	Bytes     int     `json:"bytes"`
	Optimized bool    `json:"optimized"`
}

func cmdRenew(args []string) {
	fs := flag.NewFlagSet("renew", flag.ExitOnError)
	site := fs.String("site", "", "domain, dùng config/sites/<domain>.json")
	cfgPath := fs.String("config", "", "file cấu hình JSON (thay cho -site)")
	noDelete := fs.Bool("no-delete", false, "không xóa file cache cũ trên đĩa VPS chính trước khi render")
	url := fs.String("url", "", "một URL hoặc path cần renew")
	pathsFile := fs.String("paths", "", "file danh sách URL/path, mỗi dòng một")
	limit := fs.Int("limit", 0, "chỉ xử lý N path đầu")
	keepS3 := fs.Bool("keep-s3", false, "không xóa object cũ trên S3 trước (upload sẽ ghi đè)")
	logPath := fs.String("log", "", "ghi kết quả từng bước ra file JSONL")
	mode := fs.String("mode", "auto", "auto | http | prefork. auto: tối đa -http-max URL thì dùng http, nhiều hơn thì dùng prefork")
	httpMax := fs.Int("http-max", 4, "ngưỡng của -mode auto")
	fs.Parse(args)

	if (*url == "") == (*pathsFile == "") {
		usage()
	}
	cfg, clients := setup(resolveConfig(*cfgPath, *site))
	*logPath = defaultLog(*logPath, *site, "renew")
	var paths []string
	if *url != "" {
		if err := checkLine(*url, cfg.Origin.Host); err != nil {
			die("%s: %v", *url, err)
		}
		paths = []string{normPath(*url)}
	} else {
		paths = readPaths(*pathsFile, *limit, cfg.Origin.Host)
	}
	if len(paths) == 0 {
		die("không có path nào")
	}
	if cfg.Renew.SSHHost == "" || cfg.Renew.Command == "" {
		die("config thiếu renew.ssh_host / renew.command")
	}
	m := *mode
	switch m {
	case "auto":
		m = "prefork"
		if len(paths) <= *httpMax {
			m = "http"
		}
	case "http", "prefork":
	default:
		die("-mode phải là auto, http hoặc prefork")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	col := newCollector(*logPath)
	start := time.Now()
	if !*noDelete {
		// Chỉ nhận bản cache tạo trong lần renew này (trừ 5 s phòng lệch đồng hồ giữa 2 VPS).
		cfg.Origin.MinCachedAt = start.Unix() - 5
	}
	fmt.Printf("renew %d path (mode %s), host %s, %d đích (%s)\n", len(paths), m, cfg.Origin.Host, len(clients), targetsDesc(cfg.Targets))

	// 1. Xóa S3.
	if !*keepS3 {
		t := time.Now()
		runDelete(ctx, paths, []string{"index-https.html", "index-mobile-https.html"}, clients, col.ch)
		fmt.Printf("[1/3] xóa object S3 cũ: %.1f s\n", time.Since(t).Seconds())
	} else {
		fmt.Println("[1/3] giữ object S3 cũ (-keep-s3)")
	}

	// 2. Tạo cache mới trên VPS chính.
	//    prefork: một lệnh ssh, PHP xóa cache cũ từng URL rồi render ngay.
	//    http:    một lệnh ssh chỉ xóa file (--delete-only), rồi bước 3 gửi GET: PHP-FPM render như
	//             khách thật và WP Rocket tự ghi cache (origin.Get thấy chưa có cached@ thì GET lại).
	t := time.Now()
	var toFetch []string
	switch {
	case m == "http" && *noDelete:
		toFetch = paths
		fmt.Println("[2/3] giữ file cache cũ trên VPS chính (-no-delete): chỉ upload lại bản đang có")
	case m == "prefork":
		cmd := cfg.Renew.Command
		if *noDelete {
			cmd += " --no-delete"
		}
		ok, failed, err := runRemote(ctx, cfg.Renew.SSHHost, cmd, "prefork", paths, col.ch)
		if err != nil {
			col.finish()
			die("prefork: %v", err)
		}
		toFetch = ok
		// Dự phòng: URL prefork lỗi (không phải 404 hay path sai) thì thử lại bằng HTTP. Prefork đã
		// xóa file cũ của URL đó trước khi render, nên GET sẽ để PHP-FPM render lại.
		var retry []string
		for p, e := range failed {
			if retryableByHTTP(e) {
				retry = append(retry, p)
			}
		}
		sort.Strings(retry)
		toFetch = append(toFetch, retry...)
		fmt.Printf("[2/3] prefork: %d/%d ok, %d URL lỗi sẽ thử lại bằng HTTP, %.1f s\n", len(ok), len(paths), len(retry), time.Since(t).Seconds())
	default: // http
		ok, _, err := runRemote(ctx, cfg.Renew.SSHHost, cfg.Renew.Command+" --delete-only", "disk-delete", paths, col.ch)
		if err != nil {
			col.finish()
			die("xóa cache trên VPS chính: %v", err)
		}
		toFetch = ok
		fmt.Printf("[2/3] xóa file cache cũ trên VPS chính: %d/%d path, %.1f s\n", len(ok), len(paths), time.Since(t).Seconds())
	}

	// 3. Lấy HTML qua HTTP (render nếu chưa có cache), upload.
	t = time.Now()
	if len(toFetch) > 0 {
		runUpload(ctx, cfg, toFetch, []string{"index-https.html"}, clients, col.ch)
	}
	fmt.Printf("[3/3] lấy HTML và upload: %.1f s\n", time.Since(t).Seconds())

	all := col.finish()
	if *logPath != "" {
		fmt.Printf("log: %s\n", *logPath)
	}
	summary(all, time.Since(start), cfg.Targets)
	if bad := failedPaths(paths, all, len(clients)); len(bad) > 0 {
		fmt.Printf("\n%d path chưa xong:\n", len(bad))
		for i, p := range bad {
			if i >= 20 {
				fmt.Printf("  ... và %d path khác (xem -log)\n", len(bad)-20)
				break
			}
			fmt.Println("  " + p)
		}
		os.Exit(1)
	}
}

// runRemote chạy dott-prefork trên VPS chính qua ssh, gửi path qua stdin, đọc kết quả dạng
// "R {json}" / "# log" / "DONE ...". Trả về path thành công và path lỗi (kèm lý do).
//
// Nếu SSH không kết nối được (exit 255, chưa nhận dòng nào: vd VPS chính từ chối vì nhiều kết nối liền nhau,
// gặp 28/11) thì thử lại tối đa 3 lần (2 s, 5 s, 10 s). Đã nhận output thì không thử lại (lệnh có thể đã chạy).
func runRemote(ctx context.Context, host, command, stage string, paths []string, results chan<- result) ([]string, map[string]string, error) {
	var ok []string
	var failed map[string]string
	var err error
	for attempt, wait := range []time.Duration{0, 2 * time.Second, 5 * time.Second, 10 * time.Second} {
		if attempt > 0 {
			fmt.Printf("    SSH tới %s lỗi (%v), thử lại sau %s\n", host, err, wait)
			time.Sleep(wait)
		}
		var lines int
		ok, failed, lines, err = runRemoteOnce(ctx, host, command, stage, paths, results)
		if err == nil || lines > 0 || ctx.Err() != nil {
			return ok, failed, err
		}
	}
	return ok, failed, err
}

func runRemoteOnce(ctx context.Context, host, command, stage string, paths []string, results chan<- result) ([]string, map[string]string, int, error) {
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", host, command)
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\n") + "\n")
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, 0, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, 0, err
	}
	var ok []string
	failed := map[string]string{}
	done := false
	lines := 0
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		l := sc.Text()
		lines++
		switch {
		case strings.HasPrefix(l, "R "):
			var p preforkLine
			if err := json.Unmarshal([]byte(l[2:]), &p); err != nil {
				fmt.Fprintf(os.Stderr, "    dòng lạ từ VPS chính: %s\n", l)
				continue
			}
			if results != nil {
				results <- result{Path: p.Path, Stage: stage, OK: p.OK, Ms: p.Ms, Bytes: p.Bytes, Err: p.Err}
			}
			if p.OK {
				ok = append(ok, p.Path)
			} else {
				failed[p.Path] = p.Err
			}
		case strings.HasPrefix(l, "DONE"):
			done = true
			if !quietRemote {
				fmt.Println("    " + l)
			}
		case strings.HasPrefix(l, "THROTTLE "):
			addThrottle(l[9:])
		case strings.HasPrefix(l, "REPLACE "):
			lastReplace = l[8:]
		case strings.HasPrefix(l, "SAMPLE "):
			var s struct {
				Path, Before, After string
				N                   int
			}
			if json.Unmarshal([]byte(l[7:]), &s) == nil {
				fmt.Printf("    ví dụ %s (%d chỗ):\n      trước: %s\n      sau:   %s\n", s.Path, s.N, oneLine(s.Before), oneLine(s.After))
			}
		case strings.HasPrefix(l, "PURGE "):
			lastPurgeRaw = l[6:]
			lastPurge = purgeSummary(l[6:])
		default:
			if strings.HasPrefix(l, "# ") {
				remoteLogs = append(remoteLogs, l[2:])
			}
			if !quietRemote || !strings.HasPrefix(l, "# ") {
				fmt.Println("    " + strings.TrimPrefix(l, "# "))
			}
		}
	}
	werr := cmd.Wait()
	if !done {
		return ok, failed, lines, fmt.Errorf("lệnh trên VPS chính dừng giữa chừng (%v)", werr)
	}
	return ok, failed, lines, nil
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// retryableByHTTP: lỗi prefork nào đáng thử lại bằng HTTP. 404 và path sai thì HTTP cũng không cứu được.
func retryableByHTTP(err string) bool {
	for _, s := range []string{"404", "không hợp lệ", "không phải host", "không có WP Rocket"} {
		if strings.Contains(err, s) {
			return false
		}
	}
	return true
}

// failedPaths: path chỉ tính là xong khi lấy được HTML (fetch ok) và upload ok lên MỌI đích.
// Cách đếm này đúng cả khi prefork lỗi nhưng thử lại bằng HTTP thành công.
func failedPaths(paths []string, all []result, nTargets int) []string {
	fetched := map[string]bool{}
	puts := map[string]int{}
	for _, r := range all {
		if !r.OK {
			continue
		}
		switch r.Stage {
		case "fetch":
			fetched[r.Path] = true
		case "put":
			puts[r.Path]++
		}
	}
	var out []string
	for _, p := range paths {
		if !fetched[p] || puts[p] < nTargets {
			out = append(out, p)
		}
	}
	return out
}
