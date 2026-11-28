package main

// dott-cache serve: service chạy liên tục trên VPS phụ (thiết kế: docs/design-pool.md).
//
//	dott-cache serve -sites php81.congtoan.net[,diaocthongthai.com] [-listen 127.0.0.1:8790]
//
//   - TCP 127.0.0.1:<port>: chỉ POST /event (WordPress gọi qua SSH reverse tunnel) và GET /health.
//     Mọi process trên VPS chính gọi được cổng loopback đó, nên mỗi site có token riêng
//     (config/secrets/hook-<site>.token).
//   - Unix socket data/serve.sock (chỉ root): POST /submit, GET /status cho lệnh "dott-cache submit|pool".
//     Không đi qua tunnel.
//   - Mỗi site: inbox + pool (data/<site>/pool.db), một resolver (sự kiện → việc), giữ khóa site suốt
//     thời gian chạy (lệnh ghi CLI cũ bị từ chối, phải dùng submit).
//   - Một worker cho mọi site (chung 1 vCPU VPS chính): lô → render → upload (song song) → KV → xong.
//     Upload + KV của lô trước chạy gối đầu với render của lô sau.

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/congtoan/dott-cache-management/internal/kv"
	"github.com/congtoan/dott-cache-management/internal/pool"
	"github.com/congtoan/dott-cache-management/internal/s3"
)

// Sự kiện thô từ mu-plugin dott-cache-events.php.
type hookEvent struct {
	B        int      `json:"b"`
	Type     string   `json:"type"` // post | comment | touch | term
	ID       int64    `json:"id"`
	Post     int64    `json:"post"`
	Tax      string   `json:"tax"`
	OldURL   string   `json:"old_url"`
	OldTerms []string `json:"old_terms"` // "taxonomy:term_id"
}

type serveSite struct {
	name    string
	cfgPath string
	cfg     Config
	clients []*s3.Client
	pool    *pool.Pool
	token   string
	kvc     *kv.Client
	flt     *urlFilter
	lock    *os.File
	wake    chan struct{} // resolver
	mu      sync.Mutex
	blogs   map[int]string // blog_id → path, cập nhật mỗi lần gọi --ids
	logMu   sync.Mutex
	// recentDel: key KV vừa xóa (5 phút). KV đọc ra trễ tới khoảng 60 s, nên GET ngay sau khi xóa có thể
	// vẫn trả "có": các key này luôn được PUT lại khi URL được làm mới.
	recentDel map[string]time.Time
}

var (
	serveSites  = map[string]*serveSite{}
	workerWake  = make(chan struct{}, 1)
	serveLogMu  sync.Mutex
	debounce    = 3 * time.Second
	maxDebounce = 15 * time.Second
)

func logf(f string, a ...any) {
	serveLogMu.Lock()
	fmt.Printf(time.Now().Format("15:04:05 ")+f+"\n", a...)
	serveLogMu.Unlock()
}

func wakeWorker() {
	select {
	case workerWake <- struct{}{}:
	default:
	}
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	sites := fs.String("sites", "", "danh sách domain, cách nhau dấu phẩy")
	listen := fs.String("listen", "127.0.0.1:8790", "địa chỉ nhận sự kiện (chỉ loopback)")
	sock := fs.String("socket", "", "unix socket cho submit/status (mặc định data/serve.sock)")
	deb := fs.Duration("debounce", 3*time.Second, "gom sự kiện liền nhau")
	fs.Parse(args)
	debounce = *deb
	if *sites == "" {
		die("cần -sites")
	}
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || (host != "127.0.0.1" && host != "::1") {
		die("-listen phải là 127.0.0.1:<port> (quy tắc: service chỉ bind loopback)")
	}
	if *sock == "" {
		*sock = filepath.Join(home(), "data", "serve.sock")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	quietRemote = true

	for _, name := range strings.Split(*sites, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		s := &serveSite{name: name, cfgPath: resolveConfig("", name), wake: make(chan struct{}, 1), blogs: map[int]string{}, recentDel: map[string]time.Time{}}
		s.cfg, s.clients = setup(s.cfgPath)
		f, ok := tryLockFile(filepath.Join(siteData(name, s.cfg), ".lock"))
		if !ok {
			die("site %s: đang có lệnh khác giữ khóa", name)
		}
		s.lock = f
		if s.pool, err = pool.Open(siteData(name, s.cfg)); err != nil {
			die("pool %s: %v", name, err)
		}
		tb, err := os.ReadFile(filepath.Join(home(), "config", "secrets", "hook-"+name+".token"))
		if err != nil || len(strings.TrimSpace(string(tb))) < 32 {
			die("site %s: thiếu config/secrets/hook-%s.token (>= 32 ký tự)", name, name)
		}
		s.token = strings.TrimSpace(string(tb))
		if s.cfg.KV.NamespaceID != "" {
			env := loadEnv(firstNonEmpty(s.cfg.KV.CFEnv, home()+"/config/secrets/cf.env"))
			s.kvc = kv.New(env["CF_API_TOKEN"], env["CF_ACCOUNT_ID"], s.cfg.KV.NamespaceID)
		}
		if s.flt, err = newURLFilter(s.cfg.URLs); err != nil {
			die("config urls %s: %v", name, err)
		}
		serveSites[strings.ToLower(s.cfg.Origin.Host)] = s
		if strings.ToLower(name) != strings.ToLower(s.cfg.Origin.Host) {
			serveSites[strings.ToLower(name)] = s
		}
		st, _ := s.pool.Stats()
		logf("site %s: %d việc trong pool, %d sự kiện trong inbox, write_blogs %v, legacy_blogs %v", name, st.Total, st.Inbox, s.cfg.WriteBlogs, s.cfg.LegacyBlogs)
	}

	// TCP: chỉ /event và /health.
	pub := http.NewServeMux()
	pub.HandleFunc("/event", handleEvent)
	pub.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	tcp := &http.Server{Addr: *listen, Handler: pub, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	// Unix socket: /submit, /status.
	adm := http.NewServeMux()
	adm.HandleFunc("/submit", handleSubmit)
	adm.HandleFunc("/status", handleStatus)
	os.Remove(*sock)
	ul, err := net.Listen("unix", *sock)
	if err != nil {
		die("socket: %v", err)
	}
	os.Chmod(*sock, 0o600)
	unixSrv := &http.Server{Handler: adm}

	go func() {
		if err := tcp.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logf("LỖI listen %s: %v", *listen, err)
			stop()
		}
	}()
	go unixSrv.Serve(ul)
	for _, s := range uniqueSites() {
		// Map blog → path (cho write_blogs): hỏi site một lần lúc khởi động (bài 0 = không tồn tại).
		if _, blogs, err := s.queryIDs(ctx, []string{"1:p0"}); err == nil {
			s.mu.Lock()
			s.blogs = blogs
			s.mu.Unlock()
			logf("site %s: blog %v", s.name, blogs)
		} else {
			logf("site %s: chưa lấy được map blog: %v", s.name, err)
		}
		go s.resolver(ctx)
		s.wake <- struct{}{}
	}
	logf("serve: nghe %s và %s", *listen, *sock)
	runWorker(ctx)
	logf("serve: dừng (đợi lô đang chạy xong)")
	sh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	tcp.Shutdown(sh)
	unixSrv.Shutdown(sh)
	cancel()
	for _, s := range uniqueSites() {
		s.pool.Close()
	}
}

func tryLockFile(p string) (*os.File, bool) {
	os.MkdirAll(filepath.Dir(p), 0o700)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		die("khóa: %v", err)
	}
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		f.Close()
		return nil, false
	}
	return f, true
}

// ---------- Nhận sự kiện ----------

func handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "đọc body", http.StatusBadRequest)
		return
	}
	var in struct {
		Host   string            `json:"host"`
		Events []json.RawMessage `json:"events"`
	}
	if json.Unmarshal(body, &in) != nil || len(in.Events) == 0 || len(in.Events) > 2000 {
		http.Error(w, "body lạ", http.StatusBadRequest)
		return
	}
	s := serveSites[strings.ToLower(in.Host)]
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if s == nil || subtle.ConstantTimeCompare([]byte(tok), []byte(s.token)) != 1 {
		logf("từ chối /event: host %q, token sai hoặc site không phục vụ", in.Host)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	bodies := make([]string, len(in.Events))
	for i, e := range in.Events {
		bodies[i] = string(e)
	}
	if err := s.pool.InboxAdd(bodies); err != nil {
		logf("%s: lưu inbox lỗi: %v", s.name, err)
		http.Error(w, "lưu lỗi", http.StatusInternalServerError)
		return
	}
	logf("%s: nhận %d sự kiện", s.name, len(bodies))
	select {
	case s.wake <- struct{}{}:
	default:
	}
	w.WriteHeader(http.StatusAccepted)
}

// ---------- Resolver: sự kiện → việc trong pool ----------

func (s *serveSite) resolver(ctx context.Context) {
	tick := time.NewTicker(30 * time.Second) // dự phòng: inbox còn sót (lỗi SSH lần trước)
	defer tick.Stop()
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-tick.C:
		}
		// Gom: chờ thêm khi sự kiện còn tới liên tục (Gutenberg lưu 2 request, sửa nhiều bài liền nhau).
		t0 := time.Now()
		for time.Since(t0) < maxDebounce {
			select {
			case <-s.wake:
				continue
			case <-time.After(debounce):
			case <-ctx.Done():
				return
			}
			break
		}
		for ctx.Err() == nil {
			rows, err := s.pool.InboxTake(500)
			if err != nil || len(rows) == 0 {
				break
			}
			if err := s.resolve(ctx, rows); err != nil {
				logf("%s: xử lý sự kiện lỗi (giữ lại trong inbox, thử lại sau 30 s): %v", s.name, err)
				break
			}
			s.pool.InboxDelete(rows[len(rows)-1].ID)
		}
	}
}

type idsRow struct {
	exportURL
	Q     string `json:"q"`
	Pages int    `json:"pages"`
}

// queryIDs: ssh main dott-prefork <site> --export-urls --ids=...
func (s *serveSite) queryIDs(ctx context.Context, ids []string) (map[string][]idsRow, map[int]string, error) {
	c := s.cfg.Renew.Command + " --export-urls --ids=" + strings.Join(ids, ",")
	var last error
	for attempt, wait := range []time.Duration{0, 2 * time.Second, 5 * time.Second} {
		if attempt > 0 {
			time.Sleep(wait)
		}
		out, err := exec.CommandContext(ctx, "ssh", "-n", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", s.cfg.Renew.SSHHost, c).Output()
		if err != nil && len(out) == 0 {
			last = err
			continue
		}
		res := map[string][]idsRow{}
		blogs := map[int]string{}
		done := false
		sc := bufio.NewScanner(strings.NewReader(string(out)))
		sc.Buffer(make([]byte, 64<<10), 4<<20)
		for sc.Scan() {
			l := sc.Text()
			switch {
			case strings.HasPrefix(l, "U "):
				var r idsRow
				if json.Unmarshal([]byte(l[2:]), &r) == nil {
					res[fmt.Sprintf("%d:%s", r.B, r.Q)] = append(res[fmt.Sprintf("%d:%s", r.B, r.Q)], r)
				}
			case strings.HasPrefix(l, "BLOGS "):
				var m map[string]string
				if json.Unmarshal([]byte(l[6:]), &m) == nil {
					for k, v := range m {
						if id, err := strconv.Atoi(k); err == nil {
							blogs[id] = strings.ToLower(v)
						}
					}
				}
			case strings.HasPrefix(l, "DONE"):
				done = true
			}
		}
		if !done {
			last = fmt.Errorf("export --ids dừng giữa chừng (%v)", err)
			continue
		}
		return res, blogs, nil
	}
	return nil, nil, last
}

// blogOf: path thuộc blog nào (path blog dài nhất là tiền tố).
func (s *serveSite) blogOf(p string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	best := ""
	lp := strings.ToLower(p)
	for _, bp := range s.blogs {
		if strings.HasPrefix(lp, bp) && len(bp) > len(best) {
			best = bp
		}
	}
	return best
}

// allowed: write_blogs hoặc legacy_blogs của config.
func (s *serveSite) allowed(p string) bool {
	if len(s.cfg.WriteBlogs) == 0 || s.isLegacy(p) {
		return true
	}
	bp := s.blogOf(p)
	for _, b := range s.cfg.WriteBlogs {
		if strings.EqualFold(b, bp) {
			return true
		}
	}
	return false
}

// isLegacy: path thuộc blog kiểu cũ (legacy_blogs: tách bản mobile, Worker đọc theo LEGACY_PREFIXES).
func (s *serveSite) isLegacy(p string) bool {
	if len(s.cfg.LegacyBlogs) == 0 {
		return false
	}
	bp := s.blogOf(p)
	if bp == "" {
		return false
	}
	for _, b := range s.cfg.LegacyBlogs {
		if strings.EqualFold(b, bp) {
			return true
		}
	}
	return false
}

func (s *serveSite) resolve(ctx context.Context, rows []pool.InboxRow) error {
	var evs []hookEvent
	idset := map[string]bool{}
	for _, r := range rows {
		var e hookEvent
		if json.Unmarshal([]byte(r.Body), &e) != nil || e.B <= 0 {
			logf("%s: bỏ sự kiện lạ: %.200s", s.name, r.Body)
			continue
		}
		switch e.Type {
		case "post":
			idset[fmt.Sprintf("%d:p%d", e.B, e.ID)] = true
			for _, t := range e.OldTerms {
				if tax, id, ok := strings.Cut(t, ":"); ok && validTax(tax) && isNum(id) {
					idset[fmt.Sprintf("%d:t:%s:%s", e.B, tax, id)] = true
				}
			}
		case "comment", "touch":
			idset[fmt.Sprintf("%d:p%d", e.B, e.Post)] = true
		case "term":
			if !validTax(e.Tax) {
				continue
			}
			idset[fmt.Sprintf("%d:t:%s:%d", e.B, e.Tax, e.ID)] = true
		default:
			logf("%s: bỏ sự kiện loại %q", s.name, e.Type)
			continue
		}
		evs = append(evs, e)
	}
	if len(idset) == 0 {
		return nil
	}
	ids := make([]string, 0, len(idset))
	for k := range idset {
		ids = append(ids, k)
	}
	sort.Strings(ids)
	t0 := time.Now()
	res, blogs, err := s.queryIDs(ctx, ids)
	if err != nil {
		return err
	}
	s.mu.Lock()
	for k, v := range blogs {
		s.blogs[k] = v
	}
	s.mu.Unlock()
	tQuery := time.Since(t0)

	plan := map[string]pool.Item{} // path → việc (việc sau ghi đè việc trước, trừ khi hạ ưu tiên)
	skipped := map[string]int{}
	put := func(u exportURL, action string, prio int, src string) {
		if u.P == "" {
			return
		}
		if u.BP == "" {
			u.BP = s.blogOf(u.P)
		}
		if !s.allowed(u.P) {
			skipped["ngoài write_blogs"]++
			return
		}
		if why := s.flt.reject(u); why != "" {
			skipped[why]++
			return
		}
		if old, ok := plan[u.P]; ok && old.Action == action && old.Prio < prio {
			prio = old.Prio
		}
		plan[u.P] = pool.Item{Path: u.P, Action: action, Prio: prio, Src: src}
	}
	terms := map[string]idsRow{} // term cần kiểm tra phân trang
	addRows := func(key, src string, single bool) {
		for _, r := range res[key] {
			switch r.K {
			case "single":
				if single {
					put(r.exportURL, "refresh", pool.PrioEvent, src)
				}
			case "term":
				put(r.exportURL, "refresh", pool.PrioRelated, src)
				terms[r.P] = r
			case "gone":
				if r.P != "" { // term rỗng: gỡ trang 1 nếu đang có
					terms[r.P] = r
				}
			}
		}
	}
	for _, e := range evs {
		pk := fmt.Sprintf("%d:p%d", e.B, e.ID)
		switch e.Type {
		case "post":
			var live *idsRow
			for i, r := range res[pk] {
				if r.K == "single" {
					live = &res[pk][i]
				}
			}
			src := fmt.Sprintf("post %d:%d", e.B, e.ID)
			addRows(pk, src, true)
			for _, t := range e.OldTerms {
				if tax, id, ok := strings.Cut(t, ":"); ok {
					addRows(fmt.Sprintf("%d:t:%s:%s", e.B, tax, id), src, false)
				}
			}
			for _, r := range res[fmt.Sprintf("%d:home", e.B)] {
				put(r.exportURL, "refresh", pool.PrioRelated, src)
			}
			if op := s.oldPath(e.OldURL); op != "" && (live == nil || !strings.EqualFold(op, live.P)) {
				put(exportURL{P: op, K: "single"}, "remove", pool.PrioRemove, src+" (url cũ)")
			}
		case "comment", "touch":
			for _, r := range res[fmt.Sprintf("%d:p%d", e.B, e.Post)] {
				if r.K == "single" {
					put(r.exportURL, "refresh", pool.PrioEvent, fmt.Sprintf("%s %d:%d", e.Type, e.B, e.Post))
				}
			}
		case "term":
			tk := fmt.Sprintf("%d:t:%s:%d", e.B, e.Tax, e.ID)
			src := fmt.Sprintf("term %d:%s:%d", e.B, e.Tax, e.ID)
			addRows(tk, src, false)
			gone := len(res[tk]) == 0
			for _, r := range res[tk] {
				gone = gone || r.K == "gone"
			}
			if op := s.oldPath(e.OldURL); op != "" && gone {
				put(exportURL{P: op, K: "term", T: e.Tax}, "remove", pool.PrioRemove, src+" (đã xóa)")
			}
		}
	}

	// Phân trang và term rỗng: hỏi S3 (đích đầu tiên) xem trang đang có hay không. Không hỏi KV: KV đọc
	// ra trễ tới khoảng 60 s (đo 28/11: trang vừa PUT 12 s trước, GET vẫn trả "không có", nên trang thừa
	// không bị gỡ). S3 đọc ngay sau khi ghi là thấy.
	if len(s.clients) > 0 {
		c0 := s.clients[0]
		for base, r := range terms {
			checks := map[string]string{} // path → "need" (phải có) | "drop" (phải không còn)
			switch {
			case r.K == "gone":
				checks[base] = "drop"
				checks[base+"page/2/"] = "drop"
			case r.Pages > 1:
				checks[fmt.Sprintf("%spage/%d/", base, r.Pages)] = "need"
				checks[fmt.Sprintf("%spage/%d/", base, r.Pages+1)] = "drop"
			default:
				checks[base+"page/2/"] = "drop"
			}
			for p, want := range checks {
				html := keyFor(c0.T.Prefix, p, "index-https.html")
				has := false
				for _, k := range keysInDirs(ctx, c0, []string{p}, true) {
					has = has || k == html
				}
				u := exportURL{P: p, B: r.B, BP: r.BP, K: "term", T: r.T, PG: 2}
				if want == "need" && !has {
					put(u, "refresh", pool.PrioRelated, "phân trang mới "+base)
				} else if want == "drop" && has {
					put(u, "remove", pool.PrioRemove, "phân trang thừa "+base)
				}
			}
		}
	}

	items := make([]pool.Item, 0, len(plan))
	nr, nd := 0, 0
	for _, it := range plan {
		items = append(items, it)
		if it.Action == "remove" {
			nd++
		} else {
			nr++
		}
	}
	added, merged, err := s.pool.Add(items)
	if err != nil {
		return err
	}
	logf("%s: %d sự kiện → làm mới %d, gỡ %d (mới %d, gộp %d); --ids %d mục %.1f s%s",
		s.name, len(evs), nr, nd, added, merged, len(ids), tQuery.Seconds(), skipStr(skipped))
	if len(items) > 0 {
		wakeWorker()
	}
	return nil
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

func skipStr(m map[string]int) string {
	if len(m) == 0 {
		return ""
	}
	return "; bỏ qua: " + countStr(m)
}

// oldPath: URL cũ thô từ plugin → path (chỉ khi đúng host, không có query string).
func (s *serveSite) oldPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "?") || checkLine(raw, s.cfg.Origin.Host) != nil {
		return ""
	}
	return normPath(raw)
}

func validTax(t string) bool {
	if t == "" || len(t) > 64 {
		return false
	}
	for _, c := range t {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func isNum(s string) bool {
	_, err := strconv.ParseInt(s, 10, 64)
	return err == nil && s != ""
}

// ---------- Worker ----------

const (
	maxRemoveBatch = 500
	maxEventBatch  = 20
	maxBulkBatch   = 40
)

func uniqueSites() []*serveSite {
	var out []*serveSite
	seen := map[*serveSite]bool{}
	for _, s := range serveSites {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func runWorker(ctx context.Context) {
	var upWG sync.WaitGroup // lô đang upload (gối đầu tối đa 1 lô)
	sites := uniqueSites()
	for ctx.Err() == nil {
		worked := false
		// Lấy lô có ưu tiên cao nhất trong mọi site.
		var best *serveSite
		bestPrio := 99
		for _, s := range sites {
			if p, ok := s.headPrio(); ok && p < bestPrio {
				best, bestPrio = s, p
			}
		}
		if best != nil {
			action, items, err := best.pool.Take(maxRemoveBatch, maxEventBatch, maxBulkBatch)
			if err != nil {
				logf("%s: lấy lô lỗi: %v", best.name, err)
			} else if len(items) > 0 {
				worked = true
				if action == "remove" {
					upWG.Wait() // gỡ chạy sau khi lô upload trước xong: không để upload ghi lại object vừa xóa
					best.doRemove(ctx, items)
				} else {
					best.doRefresh(ctx, items, &upWG)
				}
			}
		}
		if !worked {
			select {
			case <-ctx.Done():
			case <-workerWake:
			case <-time.After(30 * time.Second): // việc hẹn thử lại tới giờ
			}
		}
	}
	upWG.Wait()
}

func (s *serveSite) headPrio() (int, bool) {
	return s.pool.HeadPrio()
}

func (s *serveSite) doRemove(ctx context.Context, items []pool.Item) {
	paths := make([]string, len(items))
	for i, it := range items {
		paths[i] = it.Path
	}
	logf("%s: gỡ %d URL: %s", s.name, len(paths), sample(paths))
	remoteLogs = nil
	s.mu.Lock()
	for _, p := range paths {
		s.recentDel[kv.Key(p)] = time.Now()
	}
	s.mu.Unlock()
	removePaths(ctx, s.cfg, s.clients, paths, s.name+" gỡ")
	for _, it := range items {
		s.pool.Done(it)
		s.logResult(it, "removed", "")
	}
}

// doRefresh: tách lô thành path blog một bản HTML và path blog kiểu cũ (legacy_blogs), xử lý riêng.
func (s *serveSite) doRefresh(ctx context.Context, items []pool.Item, upWG *sync.WaitGroup) {
	var normal, legacy []pool.Item
	for _, it := range items {
		if s.isLegacy(it.Path) {
			legacy = append(legacy, it)
		} else {
			normal = append(normal, it)
		}
	}
	if len(normal) > 0 {
		s.refreshBatch(ctx, normal, upWG, false)
	}
	if len(legacy) > 0 && ctx.Err() == nil {
		s.refreshBatch(ctx, legacy, upWG, true)
	}
}

// refreshBatch: render → upload → KV cho một lô.
// legacy: luôn xóa file + HTTP (không prefork: prefork-render chỉ tạo bản desktop), rồi lấy 2 bản:
// UA desktop → index-https.html, UA mobile → index-mobile-https.html (WP Rocket của blog này còn tách mobile).
func (s *serveSite) refreshBatch(ctx context.Context, items []pool.Item, upWG *sync.WaitGroup, legacy bool) {
	paths := make([]string, len(items))
	for i, it := range items {
		paths[i] = it.Path
	}
	t0 := time.Now()
	mode := "prefork"
	if len(paths) <= 4 || legacy {
		mode = "http"
	}
	var st genStats
	remoteLogs = nil // biến toàn cục của runRemote: không để dồn mãi trong service chạy lâu
	if len(paths) > 4 {
		// Lô ≤ 4 URL tốn CPU như vài khách truy cập: không chờ load, để sự kiện cập nhật ngay.
		// Lô lớn hơn (prefork, hoặc HTTP của blog kiểu cũ) vẫn điều tiết theo max_load.
		waitLoad(ctx, s.cfg, &st, s.name)
	}
	ucfg := s.cfg
	ucfg.Origin.MinCachedAt = time.Now().Unix() - 5 // HTML lấy về phải là bản vừa render
	var toUpload []string
	renderFail := map[string]string{}
	if mode == "http" {
		// Xóa file cache trên đĩa; bước lấy HTML (upload) sẽ khiến PHP-FPM render lại.
		ok, failed, err := runRemote(ctx, s.cfg.Renew.SSHHost, s.cfg.Renew.Command+" --delete-only", "disk-delete", paths, nil)
		if err != nil {
			for _, p := range paths {
				renderFail[p] = "xóa cache: " + err.Error()
			}
		}
		for p, e := range failed {
			renderFail[p] = e
		}
		_ = ok
		for _, p := range paths {
			if _, bad := renderFail[p]; !bad {
				toUpload = append(toUpload, p)
			}
		}
	} else {
		th := s.cfg.Throttle.withDefaults()
		cmd := s.cfg.Renew.Command + fmt.Sprintf(" --max-load=%.2f --resume-load=%.2f", th.MaxLoad, th.ResumeLoad)
		thrPauses, thrWaited = 0, 0
		ok, failed, err := runRemoteQuiet(ctx, s.cfg.Renew.SSHHost, cmd, paths, nil)
		toUpload = ok
		for p, e := range failed {
			if retryableByHTTP(e) {
				toUpload = append(toUpload, p)
			} else {
				renderFail[p] = e
			}
		}
		if err != nil {
			got := map[string]bool{}
			for _, p := range toUpload {
				got[p] = true
			}
			for _, p := range paths {
				if !got[p] && renderFail[p] == "" {
					renderFail[p] = "prefork: " + err.Error()
				}
			}
		}
	}
	tRender := time.Since(t0)
	if ctx.Err() != nil {
		// Service đang dừng: không tính là lỗi. Các dòng còn "running" sẽ trở về hàng đợi khi khởi động lại.
		logf("%s: dừng giữa lô %d URL, làm lại khi khởi động", s.name, len(paths))
		return
	}
	byPath := map[string]pool.Item{}
	for _, it := range items {
		byPath[it.Path] = it
	}
	for p, e := range renderFail {
		s.fail(byPath[p], e)
	}

	// Upload + KV gối đầu với lô sau.
	upWG.Wait()
	upWG.Add(1)
	go func() {
		defer upWG.Done()
		t1 := time.Now()
		ch := make(chan result, 256)
		var all []result
		done := make(chan struct{})
		go func() {
			for r := range ch {
				all = append(all, r)
			}
			close(done)
		}()
		var allMobile []result
		runUpload(ctx, ucfg, toUpload, []string{"index-https.html"}, s.clients, ch)
		close(ch)
		<-done
		if legacy && ctx.Err() == nil {
			// Bản mobile: GET sau bản desktop (file desktop đã có, WP Rocket tạo thêm index-mobile-https.html).
			mcfg := ucfg
			mcfg.Origin.UserAgent = legacyMobileUA
			chm := make(chan result, 256)
			doneM := make(chan struct{})
			go func() {
				for r := range chm {
					allMobile = append(allMobile, r)
				}
				close(doneM)
			}()
			runUpload(ctx, mcfg, toUpload, []string{"index-mobile-https.html"}, s.clients, chm)
			close(chm)
			<-doneM
		}
		if ctx.Err() != nil {
			logf("%s: dừng giữa lúc upload %d URL, làm lại khi khởi động", s.name, len(toUpload))
			return
		}
		bad := map[string]string{}
		for _, p := range failedPaths(toUpload, all, len(s.clients)) {
			bad[p] = "upload/fetch"
		}
		if legacy {
			for _, p := range failedPaths(toUpload, allMobile, len(s.clients)) {
				bad[p] = "upload/fetch (mobile)"
			}
			all = append(all, allMobile...)
		}
		for _, r := range all {
			if !r.OK && r.Err != "" {
				if _, ok := bad[r.Path]; ok {
					bad[r.Path] = r.Stage + ": " + r.Err
				}
			}
		}
		// KV: chỉ path còn đúng gen và vẫn là refresh (chưa bị chuyển sang gỡ trong lúc chạy).
		var good []pool.Item
		for _, p := range toUpload {
			it := byPath[p]
			if e, isBad := bad[p]; isBad {
				s.fail(it, e)
				continue
			}
			if g, a, ok := s.pool.Current(p); !ok || g != it.Gen || a != "refresh" {
				s.pool.Done(it) // gen đổi: trả về hàng đợi, lượt sau xử lý theo việc mới
				s.logResult(it, "superseded", "")
				continue
			}
			good = append(good, it)
		}
		kvAdded, kvErr := s.kvEnsureFew(ctx, good)
		for _, it := range good {
			if kvErr != nil {
				s.fail(it, "KV: "+kvErr.Error())
				continue
			}
			s.pool.Done(it)
			s.logResult(it, "ok", "")
		}
		label := mode
		if legacy {
			label += ", kiểu cũ desktop+mobile"
		}
		logf("%s: lô %d URL (%s): render %.1f s, upload+KV %.1f s, ok %d, lỗi %d, KV thêm %d",
			s.name, len(paths), label, tRender.Seconds(), time.Since(t1).Seconds(), len(good), len(renderFail)+len(bad), kvAdded)
		wakeWorker()
	}()
}

// kvEnsureFew: GET từng key (song song 8), PUT các key còn thiếu (một lần bulk).
func (s *serveSite) kvEnsureFew(ctx context.Context, items []pool.Item) (int, error) {
	if s.kvc == nil || len(items) == 0 {
		return 0, nil
	}
	var mu sync.Mutex
	var missing []string
	var firstErr error
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	s.mu.Lock()
	for k, t := range s.recentDel {
		if time.Since(t) > 5*time.Minute {
			delete(s.recentDel, k)
		}
	}
	s.mu.Unlock()
	for _, it := range items {
		s.mu.Lock()
		_, justDeleted := s.recentDel[kv.Key(it.Path)]
		s.mu.Unlock()
		if justDeleted {
			mu.Lock()
			missing = append(missing, kv.Key(it.Path))
			mu.Unlock()
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(p string) {
			defer wg.Done()
			defer func() { <-sem }()
			has, err := s.kvc.Exists(ctx, kv.Key(p))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			if !has {
				missing = append(missing, kv.Key(p))
			}
		}(it.Path)
	}
	wg.Wait()
	if firstErr != nil {
		return 0, firstErr
	}
	if len(missing) == 0 {
		return 0, nil
	}
	val := s.cfg.KV.Value
	if val == "" {
		val = "1"
	}
	failed, err := s.kvc.PutBulk(ctx, missing, val)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	for _, k := range missing {
		delete(s.recentDel, k)
	}
	s.mu.Unlock()
	if len(failed) > 0 {
		return len(missing) - len(failed), fmt.Errorf("PUT lỗi %d key", len(failed))
	}
	return len(missing), nil
}

func (s *serveSite) fail(it pool.Item, msg string) {
	dropped, _ := s.pool.Fail(it, msg)
	st := "retry"
	if dropped {
		st = "failed"
		logf("%s: BỎ %s sau %d lần lỗi: %s", s.name, it.Path, pool.MaxTries, msg)
	}
	s.logResult(it, st, msg)
}

// logResult: logs/<site>/serve-YYYYMMDD.jsonl
func (s *serveSite) logResult(it pool.Item, status, msg string) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	dir := filepath.Join(home(), "logs", s.name)
	os.MkdirAll(dir, 0o750)
	f, err := os.OpenFile(filepath.Join(dir, "serve-"+time.Now().Format("20060102")+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(map[string]any{"t": time.Now().Unix(), "path": it.Path, "action": it.Action, "prio": it.Prio,
		"src": it.Src, "status": status, "err": msg})
	f.Write(append(b, '\n'))
}

// ---------- Unix socket: submit / status ----------

type submitReq struct {
	Site  string      `json:"site"`
	Items []pool.Item `json:"items"`
}

func handleSubmit(w http.ResponseWriter, r *http.Request) {
	var req submitReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<20)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s := serveSites[strings.ToLower(req.Site)]
	if s == nil {
		http.Error(w, "site không phục vụ: "+req.Site, http.StatusNotFound)
		return
	}
	s.mu.Lock()
	nb := len(s.blogs)
	s.mu.Unlock()
	if len(s.cfg.WriteBlogs) > 0 && nb == 0 {
		http.Error(w, "chưa biết map blog: không kiểm tra được write_blogs", http.StatusConflict)
		return
	}
	var ok []pool.Item
	denied := 0
	for _, it := range req.Items {
		if it.Action != "refresh" && it.Action != "remove" {
			continue
		}
		if !s.allowed(it.Path) {
			denied++
			continue
		}
		// Blog kiểu cũ: mỗi URL 2 lần render HTTP, không prefork; không nhận cập nhật hàng loạt (mức 4).
		if it.Action == "refresh" && it.Prio >= pool.PrioBulk && s.isLegacy(it.Path) {
			denied++
			continue
		}
		ok = append(ok, it)
	}
	added, merged, err := s.pool.Add(ok)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	logf("%s: submit %d việc (mới %d, gộp %d, bị chặn write_blogs %d)", s.name, len(ok), added, merged, denied)
	wakeWorker()
	json.NewEncoder(w).Encode(map[string]int{"added": added, "merged": merged, "denied": denied})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	out := map[string]pool.Stats{}
	for _, s := range serveSites {
		if _, done := out[s.name]; done {
			continue
		}
		st, err := s.pool.Stats()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out[s.name] = st
	}
	json.NewEncoder(w).Encode(out)
}

// unixClient: HTTP qua data/serve.sock.
func unixClient() *http.Client {
	sock := filepath.Join(home(), "data", "serve.sock")
	return &http.Client{Timeout: 5 * time.Minute, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		}}}
}

var errNoServe = errors.New("dott-cache serve không chạy (không kết nối được data/serve.sock)")
