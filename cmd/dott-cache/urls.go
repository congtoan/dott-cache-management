package main

// Lệnh urls: danh sách URL cần cache của một site, lưu trong SQLite data/<domain>/state.db.
//
//	dott-cache urls sync  -site <domain>                    # lấy toàn bộ (lịch dự kiến: 1 lần/ngày): thêm mới, cập nhật, đánh dấu URL đã mất
//	dott-cache urls sync  -site <domain> -since auto        # chỉ bài đổi từ lần trước (lịch dự kiến: 8 giờ/lần)
//	dott-cache urls sync  -site <domain> -from export.txt   # đọc kết quả export có sẵn (test)
//	dott-cache urls stats -site <domain>
//
// Nguồn: ssh <renew.ssh_host> "<renew.command> --export-urls [--since=UNIX]" chạy export-urls.php trong
// website (nạp WordPress một lần mỗi blog). Lọc theo config "urls" (taxonomy, exclude, skip_blogs...).
// Lần full mà số URL mất vượt max_remove_pct (mặc định 2%) thì dừng, không ghi, trừ khi có -force-remove:
// phòng trường hợp export lỗi giữa chừng làm "mất" hàng loạt URL.

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/congtoan/dott-cache-management/internal/store"
)

type exportURL struct {
	P     string  `json:"p"`
	B     int     `json:"b"`
	BP    string  `json:"bp"`
	K     string  `json:"k"`
	T     string  `json:"t"`
	ID    int64   `json:"id"`
	PG    int     `json:"pg"`
	M     int64   `json:"m"`
	Terms []int64 `json:"terms"`
}

func cmdURLs(args []string) {
	if len(args) < 1 {
		usage()
	}
	switch args[0] {
	case "sync":
		cmdURLSync(args[1:])
	case "stats":
		cmdURLStats(args[1:])
	default:
		usage()
	}
}

func siteData(site string, cfg Config) string {
	if site == "" {
		site = cfg.Origin.Host
	}
	return filepath.Join(home(), "data", site)
}

func cmdURLSync(args []string) {
	fs := flag.NewFlagSet("urls sync", flag.ExitOnError)
	site := fs.String("site", "", "domain, dùng config/sites/<domain>.json")
	cfgPath := fs.String("config", "", "file cấu hình JSON (thay cho -site)")
	since := fs.String("since", "", "chỉ lấy bài đổi từ: auto (từ lần trước, mặc định 8h nếu chưa có) | 8h | UNIX; bỏ trống = lấy toàn bộ")
	from := fs.String("from", "", "đọc kết quả export từ file thay vì gọi VPS chính")
	dry := fs.Bool("dry-run", false, "chỉ tính, không ghi DB")
	force := fs.Bool("force-remove", false, "cho phép đánh dấu mất nhiều hơn max_remove_pct")
	dataDir := fs.String("data", "", "thư mục DB (mặc định data/<domain>)")
	fs.Parse(args)

	cfg := loadConfig(resolveConfig(*cfgPath, *site))
	dir := *dataDir
	if dir == "" {
		dir = siteData(*site, cfg)
	}
	db, err := store.Open(dir)
	if err != nil {
		die("mở DB: %v", err)
	}
	defer db.Close()

	started := time.Now()
	var sinceUnix int64
	switch s := *since; {
	case s == "":
	case s == "auto":
		if v, err := strconv.ParseInt(store.Meta(db, "urls_since_watermark"), 10, 64); err == nil && v > 0 {
			sinceUnix = v
		} else {
			sinceUnix = started.Add(-8 * time.Hour).Unix()
		}
	case strings.HasSuffix(s, "h") || strings.HasSuffix(s, "m"):
		d, err := time.ParseDuration(s)
		if err != nil {
			die("-since: %v", err)
		}
		sinceUnix = started.Add(-d).Unix()
	default:
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			die("-since: %v", err)
		}
		sinceUnix = v
	}
	full := sinceUnix == 0

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. Lấy danh sách từ site.
	var src io.Reader
	var cmd *exec.Cmd
	if *from != "" {
		f, err := os.Open(*from)
		if err != nil {
			die("%v", err)
		}
		defer f.Close()
		src = f
	} else {
		if cfg.Renew.SSHHost == "" || cfg.Renew.Command == "" {
			die("config thiếu renew.ssh_host / renew.command (hoặc dùng -from)")
		}
		c := cfg.Renew.Command + " --export-urls"
		if !full {
			c += fmt.Sprintf(" --since=%d", sinceUnix)
		}
		cmd = exec.CommandContext(ctx, "ssh", "-n", "-o", "BatchMode=yes", cfg.Renew.SSHHost, c)
		cmd.Stderr = os.Stderr
		out, err := cmd.StdoutPipe()
		if err != nil {
			die("%v", err)
		}
		if err := cmd.Start(); err != nil {
			die("ssh: %v", err)
		}
		src = out
	}
	urls, done, err := readExport(src)
	if cmd != nil {
		cmd.Wait()
	}
	if err != nil {
		die("đọc export: %v", err)
	}
	if !done {
		die("export dừng giữa chừng (không có dòng DONE): không ghi gì vào DB")
	}
	tExport := time.Since(started)

	// 2. Lọc theo config.
	flt, err := newURLFilter(cfg.URLs)
	if err != nil {
		die("config urls: %v", err)
	}
	keep := make([]exportURL, 0, len(urls))
	excluded := map[string]int{}
	seen := map[string]bool{}
	dups := 0
	for _, u := range urls {
		if why := flt.reject(u); why != "" {
			excluded[why]++
			continue
		}
		if seen[u.P] {
			dups++
			continue
		}
		seen[u.P] = true
		keep = append(keep, u)
	}

	// 3. So với DB.
	type row struct {
		status   string
		modified sql.NullInt64
	}
	have := map[string]row{}
	rows, err := db.Query(`SELECT path, status, modified FROM urls`)
	if err != nil {
		die("đọc DB: %v", err)
	}
	for rows.Next() {
		var p string
		var r row
		rows.Scan(&p, &r.status, &r.modified)
		have[p] = r
	}
	rows.Close()
	activeBefore := 0
	for _, r := range have {
		if r.status == "active" {
			activeBefore++
		}
	}
	var added, changed int
	for _, u := range keep {
		r, ok := have[u.P]
		switch {
		case !ok || r.status != "active":
			added++
		case u.K == "single" && (!r.modified.Valid || r.modified.Int64 != u.M):
			changed++
		}
	}
	var removed []string
	if full {
		for p, r := range have {
			if r.status == "active" && !seen[p] {
				removed = append(removed, p)
			}
		}
		sort.Strings(removed)
	}

	// Archive bị ảnh hưởng bởi bài đã đổi (chế độ -since): trang 1 của các term của bài.
	affected := 0
	if !full {
		ids := map[string]bool{}
		for _, u := range keep {
			for _, t := range u.Terms {
				ids[fmt.Sprintf("%d:%d", u.B, t)] = true
			}
		}
		for k := range ids {
			b, t, _ := strings.Cut(k, ":")
			var n int
			db.QueryRow(`SELECT COUNT(*) FROM urls WHERE status='active' AND kind='term' AND page=1 AND blog_id=? AND obj_id=?`, b, t).Scan(&n)
			affected += n
		}
	}

	mode := "full"
	if !full {
		mode = fmt.Sprintf("since %s", time.Unix(sinceUnix, 0).Format("2006-01-02 15:04"))
	}
	fmt.Printf("urls sync (%s): export %d URL trong %.1f s, giữ %d, bỏ %d (trùng %d)\n", mode, len(urls), tExport.Seconds(), len(keep), sumMap(excluded), dups)
	for _, k := range sortedKeys(excluded) {
		fmt.Printf("    bỏ vì %-28s %6d\n", k, excluded[k])
	}
	fmt.Printf("so với DB (%d đang active): thêm %d, bài đã sửa %d, mất %d", activeBefore, added, changed, len(removed))
	if !full {
		fmt.Printf(", archive cần làm mới %d", affected)
	}
	fmt.Println()
	for i, p := range removed {
		if i >= 10 {
			fmt.Printf("    ... và %d URL khác\n", len(removed)-10)
			break
		}
		fmt.Println("    mất: " + p)
	}

	maxPct := cfg.URLs.MaxRemovePct
	if maxPct <= 0 {
		maxPct = 2
	}
	if full && activeBefore >= 100 && float64(len(removed))*100/float64(activeBefore) > maxPct && !*force {
		die("số URL mất %d (%.1f%%) vượt ngưỡng %.1f%%: không ghi. Kiểm tra lại rồi chạy với -force-remove nếu đúng",
			len(removed), float64(len(removed))*100/float64(activeBefore), maxPct)
	}
	if *dry {
		fmt.Println("(dry-run: không ghi DB)")
		return
	}

	// 4. Ghi trong một transaction.
	now := started.Unix()
	tx, err := db.Begin()
	if err != nil {
		die("%v", err)
	}
	up, err := tx.Prepare(`INSERT INTO urls(path, blog_id, kind, obj_type, obj_id, page, modified, status, first_seen, last_seen)
		VALUES(?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)
		ON CONFLICT(path) DO UPDATE SET blog_id=excluded.blog_id, kind=excluded.kind, obj_type=excluded.obj_type,
			obj_id=excluded.obj_id, page=excluded.page, modified=excluded.modified, status='active',
			last_seen=excluded.last_seen, removed_at=NULL`)
	if err != nil {
		die("%v", err)
	}
	for _, u := range keep {
		pg := u.PG
		if pg == 0 {
			pg = 1
		}
		var m any
		if u.K == "single" {
			m = u.M
		}
		if _, err := up.Exec(u.P, u.B, u.K, u.T, u.ID, pg, m, now, now); err != nil {
			tx.Rollback()
			die("ghi %s: %v", u.P, err)
		}
	}
	up.Close()
	for _, p := range removed {
		if _, err := tx.Exec(`UPDATE urls SET status='removed', removed_at=? WHERE path=?`, now, p); err != nil {
			tx.Rollback()
			die("%v", err)
		}
	}
	kind := "urls-full"
	if !full {
		kind = "urls-since"
		// Lần sau lấy từ thời điểm bắt đầu lần này trừ 10 phút (phòng lệch đồng hồ, bài lưu đúng lúc chạy).
		store.SetMeta(tx, "urls_since_watermark", strconv.FormatInt(now-600, 10))
	} else {
		store.SetMeta(tx, "urls_full_last", strconv.FormatInt(now, 10))
	}
	tx.Exec(`INSERT INTO runs(kind, started, finished, seen, added, changed, removed, excluded, note) VALUES(?,?,?,?,?,?,?,?,?)`,
		kind, now, time.Now().Unix(), len(urls), added, changed, len(removed), sumMap(excluded), mode)
	if err := tx.Commit(); err != nil {
		die("commit: %v", err)
	}
	fmt.Printf("đã ghi %s/state.db trong %.1f s (tổng %.1f s)\n", dir, time.Since(started).Seconds()-tExport.Seconds(), time.Since(started).Seconds())
}

// readExport đọc các dòng "U {json}" / "# log" / "DONE ..." của export-urls.php.
func readExport(r io.Reader) ([]exportURL, bool, error) {
	var out []exportURL
	done := false
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		l := sc.Text()
		switch {
		case strings.HasPrefix(l, "U "):
			var u exportURL
			if err := json.Unmarshal([]byte(l[2:]), &u); err != nil {
				return out, false, fmt.Errorf("dòng lỗi: %.120s", l)
			}
			out = append(out, u)
		case strings.HasPrefix(l, "DONE"):
			done = true
			fmt.Println("    " + l)
		case strings.HasPrefix(l, "# "):
			fmt.Println("    " + l[2:])
		}
	}
	return out, done, sc.Err()
}

type urlFilter struct {
	c       URLConfig
	skip    map[string]bool
	taxes   map[string]map[string]bool
	exclude []*regexp.Regexp
	noPage  []string
}

func newURLFilter(c URLConfig) (*urlFilter, error) {
	f := &urlFilter{c: c, skip: map[string]bool{}, taxes: map[string]map[string]bool{}}
	for _, b := range c.SkipBlogs {
		f.skip[b] = true
	}
	for b, ts := range c.Taxonomies {
		m := map[string]bool{}
		for _, t := range ts {
			m[t] = true
		}
		f.taxes[b] = m
	}
	for _, e := range c.Exclude {
		re, err := regexp.Compile(e)
		if err != nil {
			return nil, fmt.Errorf("exclude %q: %w", e, err)
		}
		f.exclude = append(f.exclude, re)
	}
	f.noPage = c.ExcludePagination
	return f, nil
}

// reject trả về lý do bỏ URL ("" nghĩa là giữ).
func (f *urlFilter) reject(u exportURL) string {
	if f.skip[u.BP] {
		return "blog " + u.BP
	}
	switch u.K {
	case "home":
		if f.c.SkipHome {
			return "trang chủ"
		}
	case "pt_archive":
		if f.c.SkipPTArchive {
			return "archive post type"
		}
	case "term":
		if m, ok := f.taxes[u.BP]; ok && !m[u.T] {
			return "taxonomy " + u.T
		}
	}
	if u.PG > 1 {
		for _, b := range f.noPage {
			if strings.HasPrefix(u.P, b) {
				return "không lấy phân trang"
			}
		}
	}
	for _, re := range f.exclude {
		if re.MatchString(u.P) {
			return "exclude " + re.String()
		}
	}
	return ""
}

func cmdURLStats(args []string) {
	fs := flag.NewFlagSet("urls stats", flag.ExitOnError)
	site := fs.String("site", "", "domain")
	cfgPath := fs.String("config", "", "file cấu hình JSON")
	dataDir := fs.String("data", "", "thư mục DB")
	fs.Parse(args)
	cfg := loadConfig(resolveConfig(*cfgPath, *site))
	dir := *dataDir
	if dir == "" {
		dir = siteData(*site, cfg)
	}
	db, err := store.Open(dir)
	if err != nil {
		die("%v", err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT blog_id, kind, obj_type, status, COUNT(*) FROM urls GROUP BY 1,2,3,4 ORDER BY 1,2,3,4`)
	if err != nil {
		die("%v", err)
	}
	fmt.Printf("%-5s %-11s %-24s %-8s %7s\n", "blog", "kind", "type", "status", "URL")
	total := 0
	for rows.Next() {
		var b, n int
		var k, t, s string
		rows.Scan(&b, &k, &t, &s, &n)
		fmt.Printf("%-5d %-11s %-24s %-8s %7d\n", b, k, t, s, n)
		if s == "active" {
			total += n
		}
	}
	rows.Close()
	fmt.Printf("tổng active: %d\n", total)
	r2, _ := db.Query(`SELECT kind, started, seen, added, changed, removed, excluded, note FROM runs ORDER BY id DESC LIMIT 5`)
	fmt.Println("\n5 lần chạy gần nhất:")
	for r2.Next() {
		var k, note string
		var st, seen, a, c, rm, ex int64
		r2.Scan(&k, &st, &seen, &a, &c, &rm, &ex, &note)
		fmt.Printf("  %s %-10s xuất %d, thêm %d, sửa %d, mất %d, bỏ %d (%s)\n", time.Unix(st, 0).Format("2006-01-02 15:04"), k, seen, a, c, rm, ex, note)
	}
	r2.Close()
}

func sumMap(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

func sortedKeys(m map[string]int) []string {
	k := make([]string, 0, len(m))
	for s := range m {
		k = append(k, s)
	}
	sort.Strings(k)
	return k
}
