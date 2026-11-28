package main

// Lệnh replace: sửa nhanh file cache ngay trên đĩa VPS chính (không render lại), rồi upload các trang đã đổi
// lên S3. Thay cho replace_cache_content*.py và replace_cache_update_version*.py.
//
//	dott-cache replace -site <domain> -content -search "chuỗi cũ" -replace "chuỗi mới" [-regex] <phạm vi> [-yes]
//	dott-cache replace -site <domain> -version [-files generatepress_child/style.css,...] <phạm vi> [-yes]
//	  phạm vi: -all | -url URL | -urls [a,b] | -txt [file] | -segment [link]; kèm -blog /en/ nếu cần
//
// Luôn chạy thử trước (đếm và in vài ví dụ trước/sau). Chỉ ghi khi có -yes, hoặc trả lời "y" trong terminal.
// Phần sửa file chạy trên VPS chính (replace-cache.php trong site, gọi qua dott-prefork --replace):
// ghi HTML mới, tạo lại _gzip từ HTML mới, cả hai đều ghi file tạm rồi rename.
// Sau đó VPS phụ lấy lại các trang đã đổi qua HTTP, kiểm tra nội dung đã đúng, rồi upload ghi đè lên mọi đích.
// KV không đổi. Không cần purge Cloudflare cho CSS/JS vì URL đã đổi ?ver=.
//
// -version mặc định: style.css và tt-core/core.js của generatepress_child và generatepress_en.
// Version mới lấy ngay trên VPS chính: core.js theo hash trong tt-core/core.manifest.json, file khác theo filemtime.

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/congtoan/dott-cache-management/internal/store"
)

const defaultVersionFiles = "generatepress_child/style.css,generatepress_child/tt-core/core.js,generatepress_en/style.css,generatepress_en/tt-core/core.js"

func cmdReplace(args []string) {
	all := false
	var rest []string
	for _, a := range args {
		if a == "-all" || a == "--all" {
			all = true
			continue
		}
		rest = append(rest, a)
	}
	rest = optionalValue(rest, "urls", "txt", "segment")
	fs := flag.NewFlagSet("replace", flag.ExitOnError)
	site := fs.String("site", "", "domain")
	cfgPath := fs.String("config", "", "file cấu hình JSON")
	content := fs.Bool("content", false, "chế độ thay nội dung")
	search := fs.String("search", "", "chuỗi cần tìm (-content)")
	repl := fs.String("replace", "", "chuỗi thay thế (-content), có thể rỗng")
	isRegex := fs.Bool("regex", false, "-search là regex PCRE, vd '#abc(\\d+)#'")
	version := fs.Bool("version", false, "chế độ cập nhật ?ver= của CSS/JS theme")
	files := fs.String("files", defaultVersionFiles, "file trong wp-content/themes/ (cách nhau dấu phẩy)")
	yes := fs.Bool("yes", false, "ghi thật (không hỏi)")
	samples := fs.Int("samples", 3, "số ví dụ in ra khi chạy thử")
	logPath := fs.String("log", "", "ghi kết quả upload ra JSONL")
	sf := addScopeFlags(fs)
	fs.Parse(rest)
	if *content == *version {
		die("chọn đúng một chế độ: -content hoặc -version")
	}
	if *content && *search == "" {
		die("-content cần -search")
	}
	if !all && !sf.any() {
		die("cần phạm vi: -all, -url, -urls, -txt hoặc -segment (có thể kèm -blog)")
	}

	cfg, clients := setup(resolveConfig(*cfgPath, *site))
	*logPath = defaultLog(*logPath, *site, "replace")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := store.Open(siteData(*site, cfg))
	if err != nil {
		die("%v", err)
	}
	bs := loadBlogScope(db, *sf.blog)
	lockSite(*site, cfg)
	var paths []string
	if !all {
		paths = gatherPaths(ctx, cfg, db, *site, sf, bs)
		if len(paths) == 0 {
			die("không có URL nào")
		}
	}
	db.Close()

	job := map[string]any{"all": all, "samples": *samples, "max_load": cfg.Throttle.withDefaults().MaxLoad}
	if bs != nil {
		job["include"], job["exclude"] = bs.Path, bs.exclude
	}
	if *content {
		job["mode"], job["search"], job["replace"], job["regex"] = "content", *search, *repl, *isRegex
	} else {
		var fl []string
		for _, f := range strings.Split(*files, ",") {
			if f = strings.TrimSpace(f); f != "" {
				fl = append(fl, f)
			}
		}
		job["mode"], job["files"] = "version", fl
	}
	scope := "toàn bộ"
	if !all {
		scope = fmt.Sprintf("%d URL", len(paths))
	}
	fmt.Printf("replace %s %s (%s), %s\n", cfg.Origin.Host, job["mode"], bs, scope)

	run := func(dry bool) ([]string, map[string]any) {
		job["dry"] = dry
		jb, _ := json.Marshal(job)
		in := append([]string{"#job " + string(jb)}, paths...)
		remoteLogs = nil
		lastReplace = ""
		ok, failed, err := runRemote(ctx, cfg.Renew.SSHHost, cfg.Renew.Command+" --replace", "replace", in, nil)
		if err != nil {
			die("replace trên VPS chính: %v", err)
		}
		for p, e := range failed {
			fmt.Printf("  lỗi %s: %s\n", p, e)
		}
		var st map[string]any
		json.Unmarshal([]byte(lastReplace), &st)
		return ok, st
	}

	// ---- 1. Chạy thử ----
	changed, st := run(true)
	fmt.Printf("[1] chạy thử: %v file HTML, sẽ đổi %v file (%v chỗ thay; bản mobile kiểu cũ %v), lỗi %v, %v s\n",
		st["files"], st["changed"], st["replacements"], st["mobile_changed"], st["errors"], st["seconds"])
	if len(changed) == 0 {
		fmt.Println("không có file nào cần đổi")
		return
	}
	go2 := *yes
	if fi, _ := os.Stdin.Stat(); !go2 && fi.Mode()&os.ModeCharDevice != 0 {
		a := strings.ToLower(askLine(fmt.Sprintf("Ghi thật %d trang? [y/N] ", len(changed))))
		go2 = a == "y" || a == "yes"
	}
	if !go2 {
		fmt.Println("Chưa ghi gì. Chạy lại với -yes để thực hiện.")
		return
	}
	checkWrite(cfg, bs)

	// Điều kiện kiểm tra nội dung trước khi upload.
	if *content && !*isRegex {
		if !strings.Contains(*repl, *search) {
			cfg.Origin.MustNotContain = []string{*search}
		}
		if *repl != "" {
			cfg.Origin.MustContain = []string{*repl}
		}
	}
	verRe := regexp.MustCompile(`^version (\S+) = (\S+) `)

	// ---- 2. Ghi thật ----
	start := time.Now()
	changed, st = run(false)
	fmt.Printf("[2] đã sửa trên đĩa: %v trang (%v chỗ thay), lỗi %v, %v s, nghỉ vì load %v lần (%v s)\n",
		len(changed), st["replacements"], st["errors"], st["seconds"], st["pauses"], st["waited"])
	if *version {
		// Mỗi trang chỉ chứa CSS/JS của theme của blog nó, nên chỉ đòi có ÍT NHẤT một version mới.
		for _, l := range remoteLogs {
			if m := verRe.FindStringSubmatch(l); m != nil {
				fmt.Printf("    %s → ?ver=%s\n", m[1], m[2])
				cfg.Origin.MustContainAny = append(cfg.Origin.MustContainAny, "themes/"+m[1]+"?ver="+m[2])
			}
		}
	}

	// ---- 3. Upload các trang đã đổi ----
	col := newCollector(*logPath)
	t := time.Now()
	runUpload(ctx, cfg, changed, []string{"index-https.html"}, clients, col.ch)
	all3 := col.finish()
	bad := failedPaths(changed, all3, len(clients))
	fmt.Printf("[3] upload: %d/%d trang lên %d đích, %.1f s\n", len(changed)-len(bad), len(changed), len(clients), time.Since(t).Seconds())
	for i, p := range bad {
		if i >= 20 {
			fmt.Printf("  ... và %d path khác\n", len(bad)-20)
			break
		}
		fmt.Println("  chưa upload: " + p)
	}
	fmt.Printf("\nreplace xong: %.1f s", time.Since(start).Seconds())
	if *logPath != "" {
		fmt.Printf(", log: %s", *logPath)
	}
	fmt.Println()
	if len(bad) > 0 {
		os.Exit(1)
	}
}

// remoteLogs: các dòng "# ..." của lần runRemote gần nhất; lastReplace: JSON của dòng "REPLACE".
var (
	remoteLogs  []string
	lastReplace string
)
