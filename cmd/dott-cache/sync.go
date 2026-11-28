package main

// Lệnh sync: đưa thay đổi trên site tới cache, S3 và KV. Đây là bước chạy theo lịch:
//   8 giờ/lần:   dott-cache sync -site <domain> [-blog /]            (bài sửa, bài mới, bài có bình luận mới)
//   1 lần/ngày:  dott-cache sync -site <domain> -full [-blog /]      (thêm: phát hiện URL đã mất)
//
// Các bước:
//  1. urls sync (-since auto, hoặc toàn bộ với -full). Lần full có chặn khi mất > max_remove_pct.
//  2. URL đã mất (chỉ khi -full): DELETE key KV TRƯỚC, rồi xóa file trên server (dott-prefork --delete-only),
//     rồi xóa mọi object của path đó trên S3 (LIST để biết object nào thật sự có).
//  3. URL mới, bài đã sửa, bài có bình luận mới, trang 1 của archive chứa bài, trang chủ của blog có bài đổi:
//     chạy "update -txt" với danh sách đó (render, upload, thêm key KV còn thiếu).
// -dry-run: chỉ in việc sẽ làm. -blog: chỉ xử lý thay đổi của một blog.

import (
	"context"
	"flag"
	"fmt"
	"os"
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

func cmdSync(args []string) {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	site := fs.String("site", "", "domain")
	cfgPath := fs.String("config", "", "file cấu hình JSON")
	blog := fs.String("blog", "", "chỉ một blog: path (\"/\", \"/en/\") hoặc blog_id")
	full := fs.Bool("full", false, "lấy toàn bộ URL (phát hiện URL đã mất); không có thì chỉ lấy phần thay đổi")
	dry := fs.Bool("dry-run", false, "chỉ in việc sẽ làm")
	fs.Parse(args)
	cp := resolveConfig(*cfgPath, *site)
	cfg, clients := setup(cp)
	lockSite(*site, cfg)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	start := time.Now()

	db, err := store.Open(siteData(*site, cfg))
	if err != nil {
		die("%v", err)
	}
	bs := loadBlogScope(db, *blog)
	db.Close()
	if !*dry {
		checkWrite(cfg, bs)
	}

	// ---- 1. urls sync ----
	sa := []string{"-config", cp}
	if !*full {
		sa = append(sa, "-since", "auto")
	}
	if *dry {
		sa = append(sa, "-dry-run")
	}
	res := urlSync(sa)

	in := func(ps []string) []string {
		var out []string
		for _, p := range ps {
			if bs.In(p) {
				out = append(out, p)
			}
		}
		return out
	}
	removed := in(res.Removed)
	todo := dedupe(append(append(in(res.Added), in(res.Changed)...), in(res.Archives)...))

	// Trang chủ của các blog có bài mới/sửa.
	db, _ = store.Open(siteData(*site, cfg))
	blogs := map[int]bool{}
	for _, p := range append(in(res.Added), in(res.Changed)...) {
		var b int
		if db.QueryRow(`SELECT blog_id FROM urls WHERE path=?`, p).Scan(&b) == nil {
			blogs[b] = true
		}
	}
	for b := range blogs {
		var hp string
		if db.QueryRow(`SELECT path FROM urls WHERE status='active' AND kind='home' AND blog_id=?`, b).Scan(&hp) == nil {
			todo = dedupe(append(todo, hp))
		}
	}
	db.Close()

	fmt.Printf("sync %s (%s): cần cập nhật %d URL (mới %d, sửa/bình luận %d, archive %d, trang chủ %d), URL mất %d\n",
		cfg.Origin.Host, bs, len(todo), len(in(res.Added)), len(in(res.Changed)), len(in(res.Archives)), len(blogs), len(removed))
	if len(removed) > 0 {
		fmt.Printf("  mất: %s\n", sample(removed))
	}
	if *dry {
		fmt.Printf("  cập nhật: %s\n(dry-run: không làm gì)\n", sample(todo))
		return
	}

	// ---- 2. URL đã mất: KV → server → S3 ----
	if len(removed) > 0 {
		removePaths(ctx, cfg, clients, removed, "2")
	}

	// ---- 3. Cập nhật URL mới / đã sửa ----
	if len(todo) == 0 {
		fmt.Printf("sync xong: không có URL cần cập nhật (%.1f s)\n", time.Since(start).Seconds())
		return
	}
	dir := filepath.Join(home(), "lists", firstNonEmpty(*site, cfg.Origin.Host))
	os.MkdirAll(dir, 0o750)
	f := filepath.Join(dir, "sync-"+time.Now().Format("20060102-150405")+".txt")
	sort.Strings(todo)
	os.WriteFile(f, []byte("# dott-cache sync "+time.Now().Format("2006-01-02 15:04")+"\n"+strings.Join(todo, "\n")+"\n"), 0o640)
	ua := []string{"-config", cp, "-txt", f, "-yes"}
	if *blog != "" {
		ua = append(ua, "-blog", *blog)
	}
	cmdUpdate(ua)
	fmt.Printf("sync xong: %.1f s\n", time.Since(start).Seconds())
}

// removePaths gỡ các URL không còn trên site: DELETE key KV TRƯỚC (Worker thôi trỏ vào S3), rồi xóa file
// trên server (dott-prefork --delete-only), rồi xóa mọi object của path đó trên S3 (LIST để biết object nào có).
func removePaths(ctx context.Context, cfg Config, clients []*s3.Client, removed []string, step string) {
	t := time.Now()
	if cfg.KV.NamespaceID != "" {
		env := loadEnv(firstNonEmpty(cfg.KV.CFEnv, home()+"/config/secrets/cf.env"))
		kvc := kv.New(env["CF_API_TOKEN"], env["CF_ACCOUNT_ID"], cfg.KV.NamespaceID)
		keys := make([]string, len(removed))
		for i, p := range removed {
			keys[i] = kv.Key(p)
		}
		failed, err := kvc.DeleteBulk(ctx, keys)
		fmt.Printf("[%s] KV: xóa %d key, lỗi %d %s\n", step, len(keys), len(failed), errStr(err))
	}
	if _, _, err := runRemote(ctx, cfg.Renew.SSHHost, cfg.Renew.Command+" --delete-only", "disk-delete", removed, nil); err != nil {
		fmt.Printf("[%s] xóa file trên server lỗi: %v\n", step, err)
	}
	var wg sync.WaitGroup
	for _, c := range clients {
		wg.Add(1)
		go func(c *s3.Client) {
			defer wg.Done()
			keys := keysInDirs(ctx, c, removed, true)
			n, fails := deleteKeys(ctx, c, keys, 4)
			fmt.Printf("[%s] S3 %s: xóa %d/%d object của URL đã mất, lỗi %d\n", step, c.T.ID, n, len(keys), len(fails))
		}(c)
	}
	wg.Wait()
	fmt.Printf("[%s] gỡ %d URL đã mất: %.1f s\n", step, len(removed), time.Since(t).Seconds())
}

func dedupe(s []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range s {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// ---- Khóa theo site (VPS phụ) và chặn ghi theo blog ----

var siteLocked *os.File

// lockSite: chỉ một lệnh ghi chạy trên một site mỗi lúc (file data/<domain>/.lock). Gọi lồng nhau trong
// cùng tiến trình (sync → update) không bị chặn.
func lockSite(site string, cfg Config) {
	if !tryLockSite(site, cfg) {
		die("đang có lệnh khác chạy trên %s (khóa %s)", firstNonEmpty(site, cfg.Origin.Host), filepath.Join(siteData(site, cfg), ".lock"))
	}
}

// tryLockSite: như lockSite nhưng trả false thay vì dừng khi đang có lệnh khác (dùng cho "events" chạy mỗi phút).
func tryLockSite(site string, cfg Config) bool {
	if siteLocked != nil {
		return true
	}
	dir := siteData(site, cfg)
	os.MkdirAll(dir, 0o700)
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		die("khóa site: %v", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return false
	}
	siteLocked = f
	return true
}

// checkWrite: config có "write_blogs" (vd ["/"]) thì chỉ cho ghi khi phạm vi nằm trong một blog được phép.
// Dùng khi chuyển production từng blog: blog chưa chuyển không bị lệnh nào ghi nhầm.
func checkWrite(cfg Config, bs *blogScope) {
	if len(cfg.WriteBlogs) == 0 {
		return
	}
	if bs == nil {
		die("config chỉ cho ghi blog %v: cần -blog", cfg.WriteBlogs)
	}
	for _, b := range cfg.WriteBlogs {
		if strings.EqualFold(b, bs.Path) {
			return
		}
	}
	die("config chỉ cho ghi blog %v, không cho %s", cfg.WriteBlogs, bs.Path)
}
