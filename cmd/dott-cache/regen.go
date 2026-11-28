package main

// Lệnh regen-all: tạo lại cache cho TOÀN BỘ URL active trong DB (khi có thay đổi chung cho cả site:
// plugin mới, sửa theme...).
//
//	dott-cache regen-all -site <domain>                 # chỉ kiểm kê và in kế hoạch (không xóa, không ghi)
//	dott-cache regen-all -site <domain> -yes            # chạy thật
//	                     [-batch 500] [-purge-all-first] [-delete-orphans]
//
// Theo nguyên tắc đã thống nhất: mỗi URL chỉ có MỘT bản cache trên server (index-https.html +
// index-https.html_gzip, không có bản mobile), và chỉ upload index-https.html lên S3.
//
// Các bước:
//  1. Kiểm kê S3: LIST prefix trên mọi đích MỘT lần (giống manifest của hệ thống cũ), ghi ra
//     logs/<domain>/s3-manifest-<đích>-<thời gian>.txt. Chỉ những key thật sự có mới được xóa
//     (server và S3 có thể lệch nhau). Kiểm kê server: dott-prefork --purge-cache --dry-run.
//  2. Dọn server (dott-prefork --purge-cache): xóa mọi bản mobile và .mobile-active (hệ thống cũ có
//     5 file mỗi URL), file trong thư mục mồ côi (URL không còn trong DB), file lạ.
//     Bản desktop của URL trong DB được giữ tới lúc render (prefork xóa từng URL ngay trước khi
//     render), để khách không phải chờ PHP render trong suốt thời gian regen.
//     -purge-all-first: xóa hết mọi file cache ngay từ đầu.
//  3. Xóa S3 MỘT lần ngay từ đầu (chủ site chốt 28/11): mọi file KHÔNG phải index-https.html
//     (index-mobile-https.html, index-https.html_gzip, index-mobile-https.html_gzip... của hệ thống cũ),
//     theo lô 1.000 key (DeleteObjects), nhiều lô song song mỗi đích.
//     index-https.html được GIỮ và ghi đè khi upload, nên Worker luôn có bản để phục vụ trong lúc regen.
//     index-https.html của URL không còn trong DB (mồ côi): chỉ báo cáo, xóa khi có -delete-orphans.
//  4. Thứ tự: trang chủ và bài đơn → trang 1 của archive (category, tag, taxonomy, post type) →
//     trang phân trang. Prefork theo lô (-batch URL mỗi lần ssh). Lô nào xong thì lấy HTML qua HTTP và upload lên mọi
//     đích NGAY, song song với lô prefork tiếp theo. URL prefork lỗi (không phải 404) thì thử lại bằng HTTP.
//  5. Kiểm tra lại: LIST S3 lần nữa và đếm file trên server.

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/congtoan/dott-cache-management/internal/kv"
	"github.com/congtoan/dott-cache-management/internal/s3"
	"github.com/congtoan/dott-cache-management/internal/store"
)

// lastPurge: dòng "PURGE {json}" gần nhất mà runRemote đọc được (lệnh CLI chạy tuần tự nên không cần khóa).
var lastPurge string

type s3Inventory struct {
	html, other, orphan int
	keys                []string // mọi key cần xóa (theo chế độ)
	htmlKeep            map[string]bool
}

func cmdRegenAll(args []string) {
	fs := flag.NewFlagSet("regen-all", flag.ExitOnError)
	site := fs.String("site", "", "domain")
	cfgPath := fs.String("config", "", "file cấu hình JSON")
	yes := fs.Bool("yes", false, "chạy thật (không có cờ này thì chỉ kiểm kê và in kế hoạch)")
	batch := fs.Int("batch", 500, "số URL mỗi lô prefork")
	purgeFirst := fs.Bool("purge-all-first", false, "xóa hết file cache trên server ngay từ đầu")
	delOrphans := fs.Bool("delete-orphans", false, "xóa cả index-https.html của URL không còn trong DB trên S3")
	logPath := fs.String("log", "", "ghi kết quả từng URL ra JSONL")
	fs.Parse(args)

	cfg, clients := setup(resolveConfig(*cfgPath, *site))
	*logPath = defaultLog(*logPath, *site, "regen-all")
	if cfg.Renew.SSHHost == "" || cfg.Renew.Command == "" {
		die("config thiếu renew.ssh_host / renew.command")
	}
	db, err := store.Open(siteData(*site, cfg))
	if err != nil {
		die("%v", err)
	}
	var paths []string
	// Thứ tự (chủ site chốt 28/11): trang chủ, bài đơn trước; rồi trang 1 của archive (category, tag,
	// taxonomy khác, archive post type); các trang phân trang /page/N/ sau cùng, N nhỏ trước.
	// Trang quan trọng có cache mới sớm nhất nếu regen bị dừng giữa chừng.
	rows, err := db.Query(`SELECT path FROM urls WHERE status='active'
		ORDER BY CASE WHEN kind='home' THEN 0 WHEN kind='single' THEN 1 WHEN page=1 THEN 2 ELSE 3 END,
		         page, blog_id, CASE obj_type WHEN 'category' THEN 0 WHEN 'post_tag' THEN 1 ELSE 2 END, path`)
	if err != nil {
		die("%v", err)
	}
	for rows.Next() {
		var p string
		rows.Scan(&p)
		paths = append(paths, p)
	}
	rows.Close()
	db.Close()
	if len(paths) == 0 {
		die("DB không có URL active: chạy 'dott-cache urls sync' trước")
	}
	keep := map[string]bool{}
	for _, p := range paths {
		keep[kv.Key(p)] = true
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	start := time.Now()
	fmt.Printf("regen-all %s: %d URL trong DB, %d đích (%s), lô %d\n", cfg.Origin.Host, len(paths), len(clients), targetsDesc(cfg.Targets), *batch)

	// ---- 1. Kiểm kê ----
	t := time.Now()
	inv := make([]*s3Inventory, len(clients))
	var wg sync.WaitGroup
	var invErr error
	for i, c := range clients {
		wg.Add(1)
		go func(i int, c *s3.Client) {
			defer wg.Done()
			in := &s3Inventory{htmlKeep: map[string]bool{}}
			var man strings.Builder
			err := c.List(ctx, c.T.Prefix, func(o s3.ListResult) {
				fmt.Fprintf(&man, "%s\t%d\n", o.Key, o.Size)
				rel := strings.TrimPrefix(o.Key, c.T.Prefix)
				dir, file := "/", rel
				if j := strings.LastIndex(rel, "/"); j >= 0 {
					dir, file = "/"+rel[:j+1], rel[j+1:]
				}
				switch {
				case file != "index-https.html":
					in.other++ // bản mobile, _gzip...: xóa ngay từ đầu
				case keep[dir]:
					in.html++
					in.htmlKeep[dir] = true
					return // giữ, upload sẽ ghi đè
				default:
					in.orphan++
					if !*delOrphans {
						return
					}
				}
				in.keys = append(in.keys, o.Key)
			})
			if err != nil {
				invErr = fmt.Errorf("%s: %w", c.T.ID, err)
			}
			if *logPath != "" {
				mp := strings.TrimSuffix(*logPath, ".jsonl") + "-s3-manifest-" + c.T.ID + ".txt"
				os.WriteFile(mp, []byte(man.String()), 0o640)
			}
			inv[i] = in
		}(i, c)
	}
	wg.Wait()
	if invErr != nil {
		die("LIST S3: %v", invErr)
	}
	for i, c := range clients {
		fmt.Printf("[1] S3 %s: index-https.html của URL trong DB %d (giữ, sẽ ghi đè), file khác (mobile, _gzip) %d, html mồ côi %d → sẽ xóa %d object\n",
			c.T.ID, inv[i].html, inv[i].other, inv[i].orphan, len(inv[i].keys))
	}
	if _, _, err := runRemote(ctx, cfg.Renew.SSHHost, cfg.Renew.Command+" --purge-cache --dry-run", "purge", paths, nil); err != nil {
		die("kiểm kê server: %v", err)
	}
	fmt.Printf("[1] server (trước): %s  (%.1f s)\n", lastPurge, time.Since(t).Seconds())
	if !*yes {
		fmt.Println("\nChưa xóa hay ghi gì. Chạy lại với -yes để thực hiện.")
		return
	}

	// ---- 2. Dọn server ----
	t = time.Now()
	pc := cfg.Renew.Command + " --purge-cache"
	if *purgeFirst {
		pc += " --all"
	}
	if _, _, err := runRemote(ctx, cfg.Renew.SSHHost, pc, "purge", paths, nil); err != nil {
		die("dọn server: %v", err)
	}
	fmt.Printf("[2] dọn server: %s (%.1f s)\n", lastPurge, time.Since(t).Seconds())

	// ---- 3. Xóa S3 theo lô ----
	t = time.Now()
	for i, c := range clients {
		wg.Add(1)
		go func(c *s3.Client, keys []string) {
			defer wg.Done()
			n, fails := deleteKeys(ctx, c, keys, 4)
			fmt.Printf("[3] S3 %s: xóa %d/%d object, lỗi %d (%.1f s)\n", c.T.ID, n, len(keys), len(fails), time.Since(t).Seconds())
			for k, e := range fails {
				fmt.Printf("      %s: %s\n", k, e)
				break
			}
		}(c, inv[i].keys)
	}
	wg.Wait()

	// ---- 4. Prefork theo lô + upload song song ----
	cfg.Origin.MinCachedAt = start.Unix() - 5
	col := newCollector(*logPath)
	tRender := time.Now()
	upQ := make(chan []string, 2)
	upDone := make(chan struct{})
	var tUpload time.Duration
	go func() {
		for b := range upQ {
			t0 := time.Now()
			runUpload(ctx, cfg, b, []string{"index-https.html"}, clients, col.ch)
			tUpload += time.Since(t0)
		}
		close(upDone)
	}()
	nb := (len(paths) + *batch - 1) / *batch
	okTotal, retryTotal := 0, 0
	for i := 0; i < len(paths); i += *batch {
		if ctx.Err() != nil {
			break
		}
		end := min(i+*batch, len(paths))
		b := paths[i:end]
		t0 := time.Now()
		ok, failed, err := runRemoteQuiet(ctx, cfg.Renew.SSHHost, cfg.Renew.Command, b, col.ch)
		if err != nil {
			fmt.Printf("[4] lô %d/%d: LỖI %v\n", i / *batch + 1, nb, err)
		}
		var retry []string
		for p, e := range failed {
			if retryableByHTTP(e) {
				retry = append(retry, p)
			}
		}
		sort.Strings(retry)
		okTotal += len(ok)
		retryTotal += len(retry)
		fmt.Printf("[4] lô %d/%d: prefork %d/%d ok, %d thử lại bằng HTTP, %.1f s (%.1f URL/s)\n",
			i / *batch + 1, nb, len(ok), len(b), len(retry), time.Since(t0).Seconds(), float64(len(ok))/time.Since(t0).Seconds())
		upQ <- append(ok, retry...)
	}
	close(upQ)
	<-upDone
	all := col.finish()
	fmt.Printf("[4] prefork + upload: %.1f s (upload chạy song song, tổng thời gian upload %.1f s)\n", time.Since(tRender).Seconds(), tUpload.Seconds())

	// ---- 5. Kiểm tra lại ----
	t = time.Now()
	for _, c := range clients {
		have, other, orphanHTML, fresh := 0, 0, 0, 0
		var missing []string
		got := map[string]bool{}
		c.List(ctx, c.T.Prefix, func(o s3.ListResult) {
			rel := "/" + strings.TrimPrefix(o.Key, c.T.Prefix)
			switch {
			case !strings.HasSuffix(rel, "/index-https.html"):
				other++
			case keep[strings.TrimSuffix(rel, "index-https.html")]:
				have++
				got[strings.TrimSuffix(rel, "index-https.html")] = true
			default:
				orphanHTML++
			}
		})
		for _, r := range all {
			if r.Stage == "put" && r.OK && r.Target == c.T.ID {
				fresh++
			}
		}
		for k := range keep {
			if !got[k] {
				missing = append(missing, k)
			}
		}
		sort.Strings(missing)
		fmt.Printf("[5] S3 %s: có index-https.html %d/%d URL (ghi mới lần này %d), file khác còn lại %d, html mồ côi %d",
			c.T.ID, have, len(keep), fresh, other, orphanHTML)
		if len(missing) > 0 {
			fmt.Printf(", thiếu: %s", strings.Join(missing[:min(5, len(missing))], " "))
		}
		fmt.Println()
	}
	runRemote(ctx, cfg.Renew.SSHHost, cfg.Renew.Command+" --purge-cache --dry-run", "purge", paths, nil)
	fmt.Printf("[5] server (sau): %s (%.1f s)\n", lastPurge, time.Since(t).Seconds())

	summary(all, time.Since(start), cfg.Targets)
	bad := failedPaths(paths, all, len(clients))
	fmt.Printf("\nregen-all xong: %d/%d URL ok, %d URL chưa xong, tổng %.1f s (%.1f URL/s)\n",
		len(paths)-len(bad), len(paths), len(bad), time.Since(start).Seconds(), float64(len(paths)-len(bad))/time.Since(start).Seconds())
	if *logPath != "" {
		fmt.Printf("log: %s\n", *logPath)
	}
	for i, p := range bad {
		if i >= 20 {
			fmt.Printf("  ... và %d path khác\n", len(bad)-20)
			break
		}
		fmt.Println("  " + p)
	}
	if len(bad) > 0 {
		os.Exit(1)
	}
}

// deleteKeys xóa theo lô 1.000 key, tối đa `workers` lô song song. Trả về số đã xóa và các key lỗi.
func deleteKeys(ctx context.Context, c *s3.Client, keys []string, workers int) (int, map[string]string) {
	q := make(chan []string)
	var mu sync.Mutex
	fails := map[string]string{}
	n := 0
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for b := range q {
				f, err := c.DeleteMany(ctx, b)
				mu.Lock()
				if err != nil {
					for _, k := range b {
						fails[k] = err.Error()
					}
				} else {
					n += len(b) - len(f)
					for k, e := range f {
						fails[k] = e
					}
				}
				mu.Unlock()
			}
		}()
	}
	for i := 0; i < len(keys); i += 1000 {
		q <- keys[i:min(i+1000, len(keys))]
	}
	close(q)
	wg.Wait()
	return n, fails
}

// runRemoteQuiet: như runRemote cho prefork nhưng chỉ in dòng tổng kết, không in log từng blog.
func runRemoteQuiet(ctx context.Context, host, command string, paths []string, results chan<- result) ([]string, map[string]string, error) {
	quietRemote = true
	defer func() { quietRemote = false }()
	return runRemote(ctx, host, command, "prefork", paths, results)
}

var quietRemote bool

// purgeSummary rút gọn JSON của dòng PURGE để in.
func purgeSummary(s string) string {
	var m map[string]any
	if json.Unmarshal([]byte(s), &m) != nil {
		return s
	}
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	var b strings.Builder
	for _, k := range ks {
		fmt.Fprintf(&b, "%s=%v ", k, m[k])
	}
	return strings.TrimSpace(b.String())
}
