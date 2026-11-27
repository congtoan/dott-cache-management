package main

// Lệnh renew (bản thử 28/11): tạo lại cache cho 1 URL hoặc một danh sách URL.
//
//	dott-cache renew -config /etc/dott-cache/upload-test.json -url https://php81.congtoan.net/tttest/abc/
//	dott-cache renew -config /etc/dott-cache/upload-test.json -paths list.txt [-limit N] [-keep-s3] [-log out.jsonl]
//
// Các bước (theo thứ tự, dừng nếu một bước hỏng toàn bộ):
//  1. Xóa object cũ trên MỌI đích S3 (index-https.html và index-mobile-https.html). Bỏ qua bước này với -keep-s3.
//     Trong lúc chờ, Worker không thấy object nên chuyển khách sang origin, khách không bị lỗi.
//  2. Gửi danh sách path qua ssh tới VPS chính (cfg.renew.command, đọc từ stdin). Script đó xóa file
//     cache cũ trên đĩa rồi chạy prefork: bootstrap WordPress một lần cho cả lô.
//  3. Path nào prefork báo ok thì lấy HTML qua HTTP (lúc này nginx trả file vừa tạo) và upload lên mọi đích.

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
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
	cfgPath := fs.String("config", "/etc/dott-cache/upload-test.json", "file cấu hình JSON")
	url := fs.String("url", "", "một URL hoặc path cần renew")
	pathsFile := fs.String("paths", "", "file danh sách URL/path, mỗi dòng một")
	limit := fs.Int("limit", 0, "chỉ xử lý N path đầu")
	keepS3 := fs.Bool("keep-s3", false, "không xóa object cũ trên S3 trước (upload sẽ ghi đè)")
	logPath := fs.String("log", "", "ghi kết quả từng bước ra file JSONL")
	fs.Parse(args)

	if (*url == "") == (*pathsFile == "") {
		usage()
	}
	cfg, clients := setup(*cfgPath)
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	col := newCollector(*logPath)
	start := time.Now()
	fmt.Printf("renew %d path, host %s, %d đích (%s)\n", len(paths), cfg.Origin.Host, len(clients), targetsDesc(cfg.Targets))

	// 1. Xóa S3.
	if !*keepS3 {
		t := time.Now()
		runDelete(ctx, paths, []string{"index-https.html", "index-mobile-https.html"}, clients, col.ch)
		fmt.Printf("[1/3] xóa object S3 cũ: %.1f s\n", time.Since(t).Seconds())
	} else {
		fmt.Println("[1/3] giữ object S3 cũ (-keep-s3)")
	}

	// 2. Prefork trên VPS chính.
	t := time.Now()
	okPaths, err := runRemotePrefork(ctx, cfg.Renew, paths, col.ch)
	if err != nil {
		col.finish()
		die("prefork: %v", err)
	}
	fmt.Printf("[2/3] prefork: %d/%d ok, %.1f s\n", len(okPaths), len(paths), time.Since(t).Seconds())

	// 3. Lấy HTML qua HTTP, upload.
	t = time.Now()
	if len(okPaths) > 0 {
		runUpload(ctx, cfg, okPaths, []string{"index-https.html"}, clients, col.ch)
	}
	fmt.Printf("[3/3] lấy HTML và upload: %.1f s\n", time.Since(t).Seconds())

	all := col.finish()
	summary(all, time.Since(start), cfg.Targets)
	if bad := failedPaths(all); len(bad) > 0 {
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

// runRemotePrefork chạy lệnh renew trên VPS chính qua ssh, gửi path qua stdin, đọc kết quả dạng
// "R {json}" / "# log" / "DONE ...". Trả về các path prefork đã ghi cache thành công.
func runRemotePrefork(ctx context.Context, rc RenewConfig, paths []string, results chan<- result) ([]string, error) {
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", rc.SSHHost, rc.Command)
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\n") + "\n")
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var ok []string
	done := false
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		l := sc.Text()
		switch {
		case strings.HasPrefix(l, "R "):
			var p preforkLine
			if err := json.Unmarshal([]byte(l[2:]), &p); err != nil {
				fmt.Fprintf(os.Stderr, "    dòng lạ từ VPS chính: %s\n", l)
				continue
			}
			r := result{Path: p.Path, Stage: "prefork", OK: p.OK, Ms: p.Ms, Bytes: p.Bytes, Err: p.Err}
			results <- r
			if p.OK {
				ok = append(ok, p.Path)
			}
		case strings.HasPrefix(l, "DONE"):
			done = true
			fmt.Println("    " + l)
		default:
			fmt.Println("    " + strings.TrimPrefix(l, "# "))
		}
	}
	werr := cmd.Wait()
	if !done {
		return ok, fmt.Errorf("lệnh trên VPS chính dừng giữa chừng (%v)", werr)
	}
	return ok, nil
}

// failedPaths: path có bước prefork, fetch hoặc put bị lỗi (bước delete lỗi không tính).
func failedPaths(all []result) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range all {
		if r.OK || r.Stage == "delete" || seen[r.Path] {
			continue
		}
		seen[r.Path] = true
		out = append(out, r.Path)
	}
	return out
}
