package main

// Lệnh events: cập nhật cache gần như ngay khi sửa bài / có bình luận mới. Chạy mỗi phút (systemd timer):
//   dott-cache events -site <domain> -blog / [-dry-run] [-max 1000]
//
// Nguồn: mu-plugin dott-cache-events.php ghi sự kiện ra <thư mục site>/dott-cache-events/YYYYMMDD.jsonl;
// lệnh này đọc phần mới qua "ssh main dott-prefork <domain> --events --after=<mốc>" (chỉ đọc file, không
// nạp WordPress, không chờ khóa prefork). Mốc lưu trong state.db (meta "events_cursor:<blog>").
//
// Các bước:
//  1. Lấy sự kiện sau mốc. Lần đầu (chưa có mốc): chỉ lưu mốc hiện tại, không xử lý gì.
//  2. Lọc theo -blog và theo config "urls" (skip_blogs, taxonomies, exclude, ...), như urls sync.
//     Cùng một path xuất hiện nhiều lần thì sự kiện sau cùng thắng (refresh hay remove).
//  3. Lưu mốc mới TRƯỚC khi xử lý: lỗi giữa chừng không làm lặp mãi; "sync" 8 giờ/lần là lưới an toàn
//     (bắt bài sửa và bài có bình luận trong 48 giờ), "sync -full" hằng ngày bắt URL đã mất.
//  4. remove: KV → server → S3 (như sync), đánh dấu removed trong DB.
//     refresh: "update -txt" (≤ 4 URL qua HTTP, nhiều hơn qua prefork; upload; thêm key KV thiếu).
// Đang có lệnh khác giữ khóa site (sync, update -all): bỏ lượt này, không lỗi; mốc giữ nguyên nên lượt
// sau xử lý bù. Quá -max URL trong một lượt (vd sửa hàng loạt): không xử lý, chỉ báo; sync sẽ bắt.

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/congtoan/dott-cache-management/internal/store"
)

type cacheEvent struct {
	T       int64       `json:"t"`
	B       int         `json:"b"`
	BP      string      `json:"bp"`
	R       string      `json:"r"`
	ID      int64       `json:"id"`
	Refresh []exportURL `json:"refresh"`
	Remove  []exportURL `json:"remove"`
}

var cursorRe = regexp.MustCompile(`^[0-9]{8}:[0-9]+$`)

func cmdEvents(args []string) {
	fs := flag.NewFlagSet("events", flag.ExitOnError)
	site := fs.String("site", "", "domain")
	cfgPath := fs.String("config", "", "file cấu hình JSON")
	blog := fs.String("blog", "", "chỉ một blog: path (\"/\", \"/en/\") hoặc blog_id")
	dry := fs.Bool("dry-run", false, "chỉ in việc sẽ làm, không lưu mốc")
	max := fs.Int("max", 1000, "quá N URL trong một lượt thì không xử lý (để sync bắt)")
	quiet := fs.Bool("quiet", true, "không in gì khi không có sự kiện")
	fs.Parse(args)
	cp := resolveConfig(*cfgPath, *site)
	cfg, clients := setup(cp)
	if !*dry && !tryLockSite(*site, cfg) {
		if !*quiet {
			fmt.Println("events: đang có lệnh khác chạy trên site, bỏ lượt này")
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	start := time.Now()

	db, err := store.Open(siteData(*site, cfg))
	if err != nil {
		die("%v", err)
	}
	bs := loadBlogScope(db, *blog)
	if !*dry {
		checkWrite(cfg, bs)
	}
	ckey := "events_cursor:all"
	if bs != nil {
		ckey = "events_cursor:" + bs.Path
	}
	cur := store.Meta(db, ckey)
	if cur == "" {
		cur = "init"
	} else if !cursorRe.MatchString(cur) {
		db.Close()
		die("mốc %s không hợp lệ: %q", ckey, cur)
	}

	// ---- 1. Lấy sự kiện ----
	evs, next, err := fetchEvents(ctx, cfg, cur)
	if err != nil {
		db.Close()
		die("đọc sự kiện: %v", err)
	}
	if cur == "init" {
		fmt.Printf("events %s (%s): chưa có mốc, khởi tạo mốc %s (không xử lý sự kiện cũ)\n", cfg.Origin.Host, bs, next)
		if !*dry {
			store.SetMeta(db, ckey, next)
		}
		db.Close()
		return
	}

	// ---- 2. Lọc, gộp ----
	flt, err := newURLFilter(cfg.URLs)
	if err != nil {
		db.Close()
		die("config urls: %v", err)
	}
	state := map[string]string{} // path → "refresh" | "remove" (sự kiện sau cùng thắng)
	order := []string{}
	reasons := map[string]int{}
	skipped := map[string]int{}
	set := func(u exportURL, act string) {
		if !bs.In(u.P) {
			skipped["ngoài "+bs.String()]++
			return
		}
		if why := flt.reject(u); why != "" {
			skipped[why]++
			return
		}
		if _, ok := state[u.P]; !ok {
			order = append(order, u.P)
		}
		state[u.P] = act
	}
	for _, e := range evs {
		reasons[e.R]++
		for _, u := range e.Remove {
			set(u, "remove")
		}
		for _, u := range e.Refresh {
			set(u, "refresh")
		}
	}
	var refresh, removed []string
	for _, p := range order {
		if state[p] == "remove" {
			removed = append(removed, p)
		} else {
			refresh = append(refresh, p)
		}
	}

	if len(evs) == 0 {
		if !*dry && next != cur {
			store.SetMeta(db, ckey, next)
		}
		db.Close()
		if !*quiet || *dry {
			fmt.Printf("events %s (%s): không có sự kiện mới (mốc %s)\n", cfg.Origin.Host, bs, next)
		}
		return
	}
	fmt.Printf("events %s (%s) %s: %d sự kiện (%s) → làm mới %d URL, gỡ %d URL; mốc %s → %s\n",
		cfg.Origin.Host, bs, time.Now().Format("2006-01-02 15:04:05"), len(evs), countStr(reasons), len(refresh), len(removed), cur, next)
	if len(skipped) > 0 {
		fmt.Printf("  bỏ qua: %s\n", countStr(skipped))
	}
	if len(refresh) > 0 {
		fmt.Printf("  làm mới: %s\n", sample(refresh))
	}
	if len(removed) > 0 {
		fmt.Printf("  gỡ: %s\n", sample(removed))
	}
	if *dry {
		db.Close()
		fmt.Println("(dry-run: không làm gì, không lưu mốc)")
		return
	}

	// ---- 3. Lưu mốc trước khi xử lý ----
	if err := store.SetMeta(db, ckey, next); err != nil {
		db.Close()
		die("lưu mốc: %v", err)
	}
	if len(refresh)+len(removed) > *max {
		db.Close()
		fmt.Printf("  quá %d URL: không xử lý, để sync (8 giờ/lần) và sync -full (hằng ngày) bắt\n", *max)
		return
	}

	// ---- 4a. Gỡ ----
	if len(removed) > 0 {
		now := time.Now().Unix()
		for _, p := range removed {
			db.Exec(`UPDATE urls SET status='removed', removed_at=? WHERE path=? AND status='active'`, now, p)
		}
		db.Close()
		removePaths(ctx, cfg, clients, removed, "gỡ")
	} else {
		db.Close()
	}

	// ---- 4b. Làm mới ----
	if len(refresh) > 0 {
		dir := filepath.Join(home(), "lists", firstNonEmpty(*site, cfg.Origin.Host))
		os.MkdirAll(dir, 0o750)
		f := filepath.Join(dir, "events-"+time.Now().Format("20060102-150405")+".txt")
		os.WriteFile(f, []byte("# dott-cache events "+time.Now().Format("2006-01-02 15:04:05")+"\n"+strings.Join(refresh, "\n")+"\n"), 0o640)
		ua := []string{"-config", cp, "-txt", f, "-yes"}
		if *blog != "" {
			ua = append(ua, "-blog", *blog)
		}
		cmdUpdate(ua)
		os.Remove(f) // log của update đã có danh sách
	}
	fmt.Printf("events xong: %.1f s\n", time.Since(start).Seconds())
}

// fetchEvents gọi "dott-prefork <domain> --events --after=<cur>" qua SSH; thử lại khi SSH lỗi.
func fetchEvents(ctx context.Context, cfg Config, cur string) ([]cacheEvent, string, error) {
	if cfg.Renew.SSHHost == "" || cfg.Renew.Command == "" {
		return nil, "", fmt.Errorf("config thiếu renew.ssh_host / renew.command")
	}
	c := cfg.Renew.Command + " --events --after=" + cur
	var lastErr error
	for attempt, wait := range []time.Duration{0, 2 * time.Second, 5 * time.Second} {
		if attempt > 0 {
			time.Sleep(wait)
		}
		cmd := exec.CommandContext(ctx, "ssh", "-n", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", cfg.Renew.SSHHost, c)
		cmd.Stderr = os.Stderr
		out, err := cmd.StdoutPipe()
		if err != nil {
			return nil, "", err
		}
		if err := cmd.Start(); err != nil {
			return nil, "", err
		}
		var evs []cacheEvent
		next, done, bad := "", false, 0
		var logs []string
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64<<10), 4<<20)
		for sc.Scan() {
			l := sc.Text()
			switch {
			case strings.HasPrefix(l, "E "):
				var e cacheEvent
				if json.Unmarshal([]byte(l[2:]), &e) != nil {
					bad++
					continue
				}
				evs = append(evs, e)
			case strings.HasPrefix(l, "CURSOR "):
				next = strings.TrimSpace(l[7:])
			case strings.HasPrefix(l, "DONE"):
				done = true
			case strings.HasPrefix(l, "# "):
				logs = append(logs, l[2:])
			}
		}
		cmd.Wait()
		if bad > 0 {
			fmt.Printf("  cảnh báo: bỏ %d dòng sự kiện lỗi JSON\n", bad)
		}
		if done && cursorRe.MatchString(next) {
			if cur != "init" {
				for _, l := range logs {
					fmt.Println("  " + l)
				}
			}
			return evs, next, nil
		}
		lastErr = fmt.Errorf("không có DONE/CURSOR hợp lệ (%s)", strings.Join(logs, "; "))
		if ctx.Err() != nil {
			break
		}
	}
	return nil, "", lastErr
}

func countStr(m map[string]int) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	parts := make([]string, len(ks))
	for i, k := range ks {
		parts[i] = fmt.Sprintf("%s %d", k, m[k])
	}
	return strings.Join(parts, ", ")
}
