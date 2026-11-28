package main

// Lệnh update: tạo lại cache (server + S3 + KV) cho một phạm vi URL. Gộp các tool cũ:
//   cache_create_from_db.py, cache_renew_from_db.py  → update -all (chạy theo lịch, vd hằng tháng)
//   cache-urls-update.py + .txt                      → update -txt [file] | -urls [a,b,...] | -url URL
//   cache-segment-update.py                          → update -segment [link archive,...] [-segment-mode]
//
//	dott-cache update -site <domain> -all [-yes] [-force] [-keep-orphans] [-purge-all-first]   (xem regen.go)
//	dott-cache update -site <domain> -url https://<domain>/bai-viet/
//	dott-cache update -site <domain> -urls                   # hỏi: nhập URL, cách nhau dấu phẩy
//	dott-cache update -site <domain> -urls "/a/,/b/"
//	dott-cache update -site <domain> -txt                    # đọc lists/<domain>/update.txt
//	dott-cache update -site <domain> -txt /đường/dẫn/list.txt
//	dott-cache update -site <domain> -segment                # hỏi: nhập link archive (category, tag...)
//	dott-cache update -site <domain> -segment "/category/x/" -segment-mode all|archive|posts
//	  [-mode auto|http|prefork] [-http-max 4] [-batch 200] [-yes] [-log out.jsonl]
//
// Có thể dùng nhiều nguồn cùng lúc (vd -txt và -segment): danh sách được gộp, bỏ trùng.
// Danh sách tạo sẵn bằng "dott-cache urls list ... -out lists/<domain>/x.txt" rồi "update -txt ...".
//
// Với danh sách (không phải -all), các bước:
//  1. Sắp thứ tự: trang chủ, bài đơn → trang 1 của archive → trang phân trang (theo DB; URL không có
//     trong DB được coi như bài đơn và có cảnh báo).
//  2. Trên S3: xóa file kiểu cũ (bản mobile, _gzip) của đúng các path này nếu có (LIST để biết có hay không).
//     index-https.html được giữ và ghi đè khi upload.
//  3. Tạo cache: tối đa -http-max URL thì gửi HTTP (xóa file trên đĩa rồi để PHP-FPM render), nhiều hơn
//     thì dùng prefork theo lô, có điều tiết theo load. Prefork xóa 4 file và .mobile-active của từng URL
//     ngay trước khi render.
//  4. Upload index-https.html lên mọi đích; thêm key KV còn thiếu.

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/congtoan/dott-cache-management/internal/kv"
	"github.com/congtoan/dott-cache-management/internal/s3"
	"github.com/congtoan/dott-cache-management/internal/store"
)

const askValue = "\x00ask" // cờ được gọi mà không có giá trị: hỏi người dùng / dùng mặc định

// optionalValue: cho phép "-txt", "-urls", "-segment" đứng một mình (không có giá trị).
func optionalValue(args []string, names ...string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		out = append(out, args[i])
		for _, n := range names {
			if args[i] == "-"+n || args[i] == "--"+n {
				if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
					out = append(out, askValue)
				}
			}
		}
	}
	return out
}

func cmdUpdate(args []string) {
	for i, a := range args {
		if a == "-all" || a == "--all" {
			cmdRegenAll(append(append([]string{}, args[:i]...), args[i+1:]...))
			return
		}
	}
	args = optionalValue(args, "urls", "txt", "segment")
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	site := fs.String("site", "", "domain")
	cfgPath := fs.String("config", "", "file cấu hình JSON")
	sf := addScopeFlags(fs)
	mode := fs.String("mode", "auto", "auto | http | prefork")
	httpMax := fs.Int("http-max", 4, "-mode auto: tối đa N URL thì dùng http")
	batch := fs.Int("batch", 200, "số URL mỗi lô prefork")
	yes := fs.Bool("yes", false, "không hỏi xác nhận")
	logPath := fs.String("log", "", "ghi kết quả từng URL ra JSONL")
	fs.Parse(args)
	if !sf.any() {
		if *sf.blog != "" {
			die("-blog chỉ lọc phạm vi: dùng kèm -all, -url, -urls, -txt hoặc -segment")
		}
		usage()
	}

	cfg, clients := setup(resolveConfig(*cfgPath, *site))
	*logPath = defaultLog(*logPath, *site, "update")
	if cfg.Renew.SSHHost == "" || cfg.Renew.Command == "" {
		die("config thiếu renew.ssh_host / renew.command")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := store.Open(siteData(*site, cfg))
	if err != nil {
		die("%v", err)
	}
	defer db.Close()
	bs := loadBlogScope(db, *sf.blog)
	lockSite(*site, cfg)
	checkWrite(cfg, bs)
	paths := gatherPaths(ctx, cfg, db, *site, sf, bs)
	if len(paths) == 0 {
		die("không có URL nào")
	}

	// ---- 1. Thứ tự ưu tiên theo DB ----
	paths, cnt, notInDB := orderPaths(db, paths)
	m := *mode
	if m == "auto" {
		m = "prefork"
		if len(paths) <= *httpMax {
			m = "http"
		}
	}
	fmt.Printf("update %s (%s): %d URL (trang chủ %d, bài %d, archive trang 1 %d, phân trang %d), mode %s, %d đích\n",
		cfg.Origin.Host, bs, len(paths), cnt[0], cnt[1], cnt[2], cnt[3], m, len(clients))
	if len(notInDB) > 0 {
		fmt.Printf("  cảnh báo: %d URL không có trong DB (chưa urls sync, hoặc bị loại theo config): %s\n", len(notInDB), sample(notInDB))
	}
	fmt.Printf("  %s\n", sample(paths))
	if fi, _ := os.Stdin.Stat(); !*yes && fi.Mode()&os.ModeCharDevice != 0 {
		if a := strings.ToLower(askLine("Tiếp tục? [y/N] ")); a != "y" && a != "yes" {
			fmt.Println("đã hủy")
			return
		}
	}

	start := time.Now()
	cfg.Origin.MinCachedAt = start.Unix() - 5
	col := newCollector(*logPath)

	// ---- 2. Xóa file kiểu cũ trên S3 của đúng các path này ----
	t := time.Now()
	var wg sync.WaitGroup
	for _, c := range clients {
		wg.Add(1)
		go func(c *s3.Client) { // các đích chạy song song
			defer wg.Done()
			keys := keysInDirs(ctx, c, paths, false)
			n, fails := deleteKeys(ctx, c, keys, 4)
			fmt.Printf("[2] S3 %s: file kiểu cũ (mobile, _gzip) có %d, đã xóa %d, lỗi %d (%.1f s)\n", c.T.ID, len(keys), n, len(fails), time.Since(t).Seconds())
		}(c)
	}
	wg.Wait()

	// ---- 3+4. Tạo cache và upload ----
	t = time.Now()
	if m == "http" {
		ok, _, err := runRemote(ctx, cfg.Renew.SSHHost, cfg.Renew.Command+" --delete-only", "disk-delete", paths, col.ch)
		if err != nil {
			die("xóa cache trên VPS chính: %v", err)
		}
		runUpload(ctx, cfg, ok, []string{"index-https.html"}, clients, col.ch)
		fmt.Printf("[3] http: xóa file trên đĩa, render qua PHP-FPM, upload: %.1f s\n", time.Since(t).Seconds())
	} else {
		gs := generate(ctx, cfg, clients, paths, *batch, col, "3")
		fmt.Printf("[3] prefork + upload: %.1f s; điều tiết: nghỉ trước lô %d lần (%.0f s), PHP nghỉ %d lần (%d s), load cao nhất trước lô %.2f\n",
			time.Since(t).Seconds(), gs.waits, gs.waited.Seconds(), gs.phpPauses, gs.phpWaited, gs.maxLoad)
	}
	all := col.finish()
	if cfg.KV.NamespaceID != "" {
		env := loadEnv(firstNonEmpty(cfg.KV.CFEnv, home()+"/config/secrets/cf.env"))
		kvEnsure(ctx, kv.New(env["CF_API_TOKEN"], env["CF_ACCOUNT_ID"], cfg.KV.NamespaceID), cfg, paths, all, len(clients), "4")
	}

	summary(all, time.Since(start), cfg.Targets)
	bad := failedPaths(paths, all, len(clients))
	fmt.Printf("\nupdate xong: %d/%d URL ok, %.1f s\n", len(paths)-len(bad), len(paths), time.Since(start).Seconds())
	if *logPath != "" {
		fmt.Printf("log: %s\n", *logPath)
	}
	for i, p := range bad {
		if i >= 20 {
			fmt.Printf("  ... và %d path khác\n", len(bad)-20)
			break
		}
		fmt.Println("  chưa xong: " + p)
	}
	if len(bad) > 0 {
		os.Exit(1)
	}
}

// orderPaths: sắp theo thứ tự ưu tiên (trang chủ, bài → archive trang 1 → phân trang) dựa trên DB.
// Trả về: danh sách đã sắp, số URL mỗi nhóm, các URL không có trong DB (coi như bài đơn).
func orderPaths(db *sql.DB, paths []string) ([]string, [4]int, []string) {
	type info struct {
		rank, page, blog, trank int
		inDB                    bool
	}
	want := map[string]bool{}
	for _, p := range paths {
		want[p] = true
	}
	meta := map[string]info{}
	rows, _ := db.Query(`SELECT path, kind, page, blog_id, obj_type FROM urls WHERE status='active'`)
	for rows.Next() {
		var p, k, t string
		var pg, b int
		rows.Scan(&p, &k, &pg, &b, &t)
		if !want[p] {
			continue
		}
		r := 2
		switch {
		case k == "home":
			r = 0
		case k == "single":
			r = 1
		case pg > 1:
			r = 3
		}
		tr := 2
		if t == "category" {
			tr = 0
		} else if t == "post_tag" {
			tr = 1
		}
		meta[p] = info{r, pg, b, tr, true}
	}
	rows.Close()
	var notInDB []string
	for _, p := range paths {
		if !meta[p].inDB {
			notInDB = append(notInDB, p)
			meta[p] = info{rank: 1, page: 1}
		}
	}
	out := append([]string{}, paths...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := meta[out[i]], meta[out[j]]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.page != b.page {
			return a.page < b.page
		}
		if a.blog != b.blog {
			return a.blog < b.blog
		}
		if a.trank != b.trank {
			return a.trank < b.trank
		}
		return out[i] < out[j]
	})
	var cnt [4]int
	for _, p := range out {
		cnt[meta[p].rank]++
	}
	return out, cnt, notInDB
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// defaultListFile: lists/<domain>/update.txt (hoặc lists_dir trong config).
func defaultListFile(cfg Config, site string) string {
	dir := cfg.Lists
	if dir == "" {
		dir = filepath.Join(home(), "lists", firstNonEmpty(site, cfg.Origin.Host))
	}
	f := filepath.Join(dir, "update.txt")
	if _, err := os.Stat(f); err != nil {
		os.MkdirAll(dir, 0o750)
		os.WriteFile(f, []byte("# Mỗi dòng một URL hoặc path. Dòng bắt đầu bằng # bị bỏ qua.\n"), 0o640)
		die("chưa có danh sách: đã tạo file mẫu %s, thêm URL vào rồi chạy lại", f)
	}
	return f
}

// keysInDirs: các object nằm đúng trong thư mục của các path (không tính thư mục con). includeHTML = false:
// chỉ file kiểu cũ (mọi file không phải index-https.html); true: mọi file (dùng khi gỡ URL đã mất).
// Nhiều path thì LIST cả prefix một lần; ít thì LIST theo từng path (song song).
func keysInDirs(ctx context.Context, c *s3.Client, paths []string, includeHTML bool) []string {
	want := map[string]bool{}
	for _, p := range paths {
		want[keyFor(c.T.Prefix, p, "")] = true // "<prefix>tttest/abc/"
	}
	var mu sync.Mutex
	var keys []string
	pick := func(o s3.ListResult) {
		dir, file := o.Key, ""
		if j := strings.LastIndex(o.Key, "/"); j >= 0 {
			dir, file = o.Key[:j+1], o.Key[j+1:]
		}
		if want[dir] && (includeHTML || file != "index-https.html") {
			mu.Lock()
			keys = append(keys, o.Key)
			mu.Unlock()
		}
	}
	if len(paths) > 1000 {
		c.List(ctx, c.T.Prefix, pick)
		return keys
	}
	q := make(chan string)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range q {
				c.List(ctx, d, pick)
			}
		}()
	}
	for d := range want {
		q <- d
	}
	close(q)
	wg.Wait()
	return keys
}

// expandSegment: link archive → các trang của archive đó (có phân trang) và/hoặc bài thuộc archive.
func expandSegment(ctx context.Context, cfg Config, db *sql.DB, links []string, mode string) []string {
	var out []string
	var terms []string
	for _, l := range links {
		if err := checkLine(l, cfg.Origin.Host); err != nil {
			fmt.Printf("  segment: bỏ %q: %v\n", l, err)
			continue
		}
		p := normPath(l)
		var blog, id int
		var kind, typ string
		err := db.QueryRow(`SELECT blog_id, kind, obj_type, obj_id FROM urls WHERE status='active' AND path=?`, p).Scan(&blog, &kind, &typ, &id)
		if err != nil {
			fmt.Printf("  segment: %s không có trong DB (chạy urls sync trước?), bỏ\n", p)
			continue
		}
		if kind == "single" {
			fmt.Printf("  segment: %s là bài đơn, chỉ cập nhật chính nó\n", p)
			out = append(out, p)
			continue
		}
		var pages []string
		rows, _ := db.Query(`SELECT path FROM urls WHERE status='active' AND blog_id=? AND kind=? AND obj_type=? AND obj_id=? ORDER BY page`, blog, kind, typ, id)
		for rows.Next() {
			var x string
			rows.Scan(&x)
			pages = append(pages, x)
		}
		rows.Close()
		var posts []string
		if mode != "archive" {
			switch kind {
			case "term":
				terms = append(terms, fmt.Sprintf("%d:%s:%d", blog, typ, id))
			case "pt_archive":
				r2, _ := db.Query(`SELECT path FROM urls WHERE status='active' AND blog_id=? AND kind='single' AND obj_type=?`, blog, typ)
				for r2.Next() {
					var x string
					r2.Scan(&x)
					posts = append(posts, x)
				}
				r2.Close()
			case "home":
				fmt.Printf("  segment: %s là trang chủ: chỉ cập nhật các trang phân trang, không kèm bài\n", p)
			}
		}
		if mode != "posts" {
			out = append(out, pages...)
		}
		out = append(out, posts...)
		fmt.Printf("  segment %s (%s %s): %d trang archive", p, kind, typ, len(pages))
		if kind == "pt_archive" && mode != "archive" {
			fmt.Printf(", %d bài", len(posts))
		}
		fmt.Println()
	}
	if len(terms) > 0 {
		// Bài thuộc term: hỏi site (export-urls --terms), lấy cả term con giống trang archive.
		cmd := exec.CommandContext(ctx, "ssh", "-n", "-o", "BatchMode=yes", cfg.Renew.SSHHost, cfg.Renew.Command+" --export-urls --terms="+strings.Join(terms, ","))
		pipe, _ := cmd.StdoutPipe()
		cmd.Start()
		us, done, _ := readExportQuiet(pipe)
		cmd.Wait()
		if !done {
			fmt.Println("  segment: lấy bài thuộc archive bị lỗi (không có DONE)")
		}
		active := map[string]bool{}
		r, _ := db.Query(`SELECT path FROM urls WHERE status='active' AND kind='single'`)
		for r.Next() {
			var x string
			r.Scan(&x)
			active[x] = true
		}
		r.Close()
		skipped := 0
		for _, u := range us {
			if active[u.P] {
				out = append(out, u.P)
			} else {
				skipped++
			}
		}
		fmt.Printf("  segment: %d bài thuộc %d archive (bỏ %d bài không active trong DB)\n", len(us)-skipped, len(terms), skipped)
	}
	return out
}
