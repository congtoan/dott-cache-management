// dott-cache: hệ thống cache v2 (chạy trên VPS phụ).
//
// Bản thử (27–28/11): các lệnh upload, delete, renew (renew.go).
//
//	dott-cache upload -site php81.congtoan.net -paths paths.txt [-limit N] [-mobile-copy] [-log out.jsonl]
//	dott-cache delete -site ... -paths paths.txt      # xóa object đã upload (dọn bucket test)
//	dott-cache renew  -site ... -url URL | -paths list.txt   # xóa cache cũ (S3 + đĩa), prefork, upload
//
// Thư mục trên VPS phụ: /root/tool/cache-system/{bin,src,config/{sites,secrets},data,logs,test}.
//
// Luồng upload: đọc danh sách path → pool lấy HTML từ origin (gọi thẳng IP, nhận file cache
// nguyên văn) → mỗi trang được đẩy song song lên MỌI đích S3, mỗi đích một pool riêng.
// HTML chỉ lấy từ origin MỘT lần cho mọi đích.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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
	"github.com/congtoan/dott-cache-management/internal/origin"
	"github.com/congtoan/dott-cache-management/internal/s3"
)

type Config struct {
	Origin  origin.Config     `json:"origin"`
	Targets []s3.Target       `json:"targets"`
	S3Env   string            `json:"s3_env"`
	Headers map[string]string `json:"headers"`
	Renew   RenewConfig       `json:"renew"`
	URLs    URLConfig         `json:"urls"`
	KV      KVConfig          `json:"kv"`
}

// URLConfig: quy tắc chọn URL cần cache (lệnh "urls sync"). Áp ở VPS phụ, nên đổi quy tắc không phải sửa site.
type URLConfig struct {
	SkipBlogs         []string            `json:"skip_blogs"`         // path blog bỏ qua, vd ["/es/"]
	Taxonomies        map[string][]string `json:"taxonomies"`         // path blog → taxonomy được lấy; blog không có trong map: lấy mọi taxonomy
	Exclude           []string            `json:"exclude"`            // regex trên path, khớp thì bỏ
	ExcludePagination []string            `json:"exclude_pagination"` // path archive không lấy page/N/
	SkipHome          bool                `json:"skip_home"`          // không lấy trang chủ của blog
	SkipPTArchive     bool                `json:"skip_pt_archive"`    // không lấy archive của post type
	MaxRemovePct      float64             `json:"max_remove_pct"`     // chặn khi một lần full xóa quá N% URL (mặc định 2)
}

// loadConfig chỉ đọc file config (không cần key S3).
func loadConfig(cfgPath string) Config {
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		die("đọc config: %v", err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		die("config JSON: %v", err)
	}
	return cfg
}

// RenewConfig: cách gọi prefork trên VPS chính.
type RenewConfig struct {
	SSHHost string `json:"ssh_host"` // alias trong /root/.ssh/config, vd "main"
	Command string `json:"command"`  // vd "bash /home/php81conZSI/prefork/renew.sh" (đọc path từ stdin)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "upload":
		cmdUpload(os.Args[2:], false)
	case "delete":
		cmdUpload(os.Args[2:], true)
	case "renew":
		cmdRenew(os.Args[2:])
	case "urls":
		cmdURLs(os.Args[2:])
	case "kv":
		cmdKV(os.Args[2:])
	case "regen-all":
		cmdRegenAll(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `dùng (thư mục gốc: $DOTT_CACHE_HOME, mặc định /root/tool/cache-system):
  dott-cache upload -site <domain> -paths paths.txt [-limit N] [-mobile-copy] [-log out.jsonl]
  dott-cache delete -site <domain> -paths paths.txt
  dott-cache renew  -site <domain> (-url URL | -paths paths.txt) [-mode auto|http|prefork] [-http-max 4] [-limit N] [-keep-s3] [-no-delete] [-log out.jsonl]
  dott-cache urls sync -site <domain> [-since auto|8h|UNIX] [-from export.txt] [-dry-run] [-force-remove]
  dott-cache urls stats -site <domain>
  dott-cache regen-all -site <domain> [-yes] [-batch 500] [-purge-all-first] [-delete-orphans]   (không có -yes: chỉ kiểm kê)
  dott-cache kv create -site <domain> -title <tên> | kv list -site <domain> [-prefix P] | kv sync -site <domain> [-dry-run] [-force-remove]
  -site <domain> dùng config/sites/<domain>.json; -config <file> thay cho -site. Không có -log thì ghi vào logs/<domain>/.`)
	os.Exit(2)
}

func die(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "LỖI: "+f+"\n", a...)
	os.Exit(1)
}

func loadEnv(p string) map[string]string {
	f, err := os.Open(p)
	if err != nil {
		die("không mở được %s: %v", p, err)
	}
	defer f.Close()
	m := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if k, v, ok := strings.Cut(l, "="); ok {
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return m
}

// readPaths đọc file danh sách. host khác rỗng: dòng là URL của host khác, hoặc path dạng lạ
// (bắt đầu bằng "//", có "://" bên trong, có "..") bị bỏ và in cảnh báo, không âm thầm đổi thành path khác.
func readPaths(p string, limit int, host string) []string {
	f, err := os.Open(p)
	if err != nil {
		die("không mở được %s: %v", p, err)
	}
	defer f.Close()
	var out []string
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if host != "" {
			if err := checkLine(l, host); err != nil {
				fmt.Fprintf(os.Stderr, "bỏ dòng %q: %v\n", l, err)
				continue
			}
		}
		l = normPath(l)
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// checkLine: dòng phải là path "/..." hoặc URL http(s)://<host>/..., và path không có dạng lạ.
func checkLine(l, host string) error {
	l = strings.TrimSpace(l)
	if i := strings.Index(l, "://"); i >= 0 {
		rest := l[i+3:]
		h, _, _ := strings.Cut(rest, "/")
		if !strings.EqualFold(h, host) {
			return fmt.Errorf("không phải host %s", host)
		}
		l = strings.TrimPrefix(rest, h)
	}
	switch {
	case !strings.HasPrefix(l, "/") && l != "":
		return errors.New("không bắt đầu bằng /")
	case strings.HasPrefix(l, "//"), strings.Contains(l, "://"):
		return errors.New("path dạng lạ (có // hoặc ://)")
	case strings.Contains(l, ".."):
		return errors.New("path có ..")
	}
	return nil
}

// normPath: chấp nhận cả URL đầy đủ, trả về path có "/" cuối, bỏ query string và #fragment.
func normPath(l string) string {
	l = strings.TrimSpace(l)
	if i := strings.Index(l, "://"); i >= 0 {
		if j := strings.Index(l[i+3:], "/"); j >= 0 {
			l = l[i+3+j:]
		} else {
			l = "/"
		}
	}
	if i := strings.IndexAny(l, "?#"); i >= 0 {
		l = l[:i]
	}
	if !strings.HasPrefix(l, "/") {
		l = "/" + l
	}
	if !strings.HasSuffix(l, "/") {
		l += "/"
	}
	return l
}

// keyFor: path "/en/about-us/" → "<prefix>en/about-us/index-https.html" (giống tool cũ).
//
// Path được chuẩn hóa giống Worker (kv.Key: viết thường, "/" cuối), vì Worker dựng URL S3 từ path đã
// chuẩn hóa. Nếu path có chữ hoa mà key S3 giữ nguyên chữ hoa, Worker sẽ không tìm thấy object.
func keyFor(prefix, path, file string) string {
	return prefix + strings.TrimPrefix(kv.Key(path), "/") + file
}

type result struct {
	Path     string  `json:"path"`
	Stage    string  `json:"stage"` // fetch | put
	Target   string  `json:"target,omitempty"`
	Key      string  `json:"key,omitempty"`
	OK       bool    `json:"ok"`
	Ms       float64 `json:"ms"`
	Bytes    int     `json:"bytes,omitempty"`
	Served   string  `json:"served,omitempty"`
	Rendered bool    `json:"rendered,omitempty"`
	Err      string  `json:"err,omitempty"`
}

// home: thư mục gốc của hệ thống trên VPS phụ (bin/, config/, data/, logs/, src/, test/).
func home() string {
	if h := os.Getenv("DOTT_CACHE_HOME"); h != "" {
		return h
	}
	return "/root/tool/cache-system"
}

// resolveConfig: -config (đường dẫn) có ưu tiên; nếu không có thì dùng -site <domain>,
// tương ứng với file config/sites/<domain>.json.
func resolveConfig(cfgPath, site string) string {
	switch {
	case cfgPath != "":
		return cfgPath
	case site != "":
		if strings.ContainsAny(site, "/\\") || strings.HasPrefix(site, ".") {
			die("tên site không hợp lệ: %q", site)
		}
		return filepath.Join(home(), "config", "sites", site+".json")
	}
	die("cần -site <domain> hoặc -config <file>")
	return ""
}

// defaultLog: khi không có -log và có -site, ghi log vào logs/<site>/<lệnh>-<thời gian>.jsonl.
func defaultLog(logPath, site, cmd string) string {
	if logPath != "" || site == "" {
		return logPath
	}
	dir := filepath.Join(home(), "logs", site)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		die("tạo thư mục log: %v", err)
	}
	return filepath.Join(dir, cmd+"-"+time.Now().Format("20060102-150405")+".jsonl")
}

// setup đọc config, key S3 và tạo client cho mọi đích.
func setup(cfgPath string) (Config, []*s3.Client) {
	cfg := loadConfig(cfgPath)
	env := loadEnv(cfg.S3Env)
	ak, sk := env["S3_ACCESS_KEY"], env["S3_SECRET_KEY"]
	if ak == "" || sk == "" {
		die("thiếu S3_ACCESS_KEY/S3_SECRET_KEY trong %s", cfg.S3Env)
	}
	if cfg.Headers == nil {
		// charset=utf-8: không có thì trình duyệt phải tự đoán bảng mã từ <meta charset>.
		cfg.Headers = map[string]string{"Content-Type": "text/html; charset=utf-8", "Cache-Control": "public, max-age=31536000", "x-amz-acl": "public-read"}
	}
	clients := make([]*s3.Client, 0, len(cfg.Targets))
	for _, t := range cfg.Targets {
		c, err := s3.New(t, ak, sk)
		if err != nil {
			die("đích %s: %v", t.ID, err)
		}
		clients = append(clients, c)
	}
	return cfg, clients
}

// collector gom kết quả từ mọi goroutine, ghi JSONL nếu có -log.
type collector struct {
	ch   chan result
	all  []result
	done chan struct{}
	f    *os.File
	w    *bufio.Writer
}

func newCollector(logPath string) *collector {
	c := &collector{ch: make(chan result, 256), done: make(chan struct{})}
	var enc *json.Encoder
	if logPath != "" {
		f, err := os.Create(logPath)
		if err != nil {
			die("tạo log: %v", err)
		}
		c.f, c.w = f, bufio.NewWriter(f)
		enc = json.NewEncoder(c.w)
	}
	go func() {
		for r := range c.ch {
			c.all = append(c.all, r)
			if enc != nil {
				enc.Encode(r)
			}
		}
		close(c.done)
	}()
	return c
}

func (c *collector) finish() []result {
	close(c.ch)
	<-c.done
	if c.w != nil {
		c.w.Flush()
		c.f.Close()
	}
	return c.all
}

func cmdUpload(args []string, del bool) {
	fs := flag.NewFlagSet("upload", flag.ExitOnError)
	site := fs.String("site", "", "domain, dùng config/sites/<domain>.json")
	cfgPath := fs.String("config", "", "file cấu hình JSON (thay cho -site)")
	pathsFile := fs.String("paths", "", "file danh sách path (mỗi dòng một path hoặc URL)")
	limit := fs.Int("limit", 0, "chỉ xử lý N path đầu")
	mobileCopy := fs.Bool("mobile-copy", false, "upload thêm cùng nội dung lên key index-mobile-https.html (giai đoạn chuyển tiếp)")
	logPath := fs.String("log", "", "ghi kết quả từng bước ra file JSONL")
	fs.Parse(args)
	if *pathsFile == "" {
		usage()
	}
	cfg, clients := setup(resolveConfig(*cfgPath, *site))
	*logPath = defaultLog(*logPath, *site, map[bool]string{false: "upload", true: "delete"}[del])
	paths := readPaths(*pathsFile, *limit, cfg.Origin.Host)
	files := []string{"index-https.html"}
	if *mobileCopy {
		files = append(files, "index-mobile-https.html")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	col := newCollector(*logPath)

	start := time.Now()
	fmt.Printf("%s %d path × %d file × %d đích (%s)\n", map[bool]string{false: "upload", true: "delete"}[del], len(paths), len(files), len(clients), targetsDesc(cfg.Targets))
	if del {
		runDelete(ctx, paths, files, clients, col.ch)
	} else {
		runUpload(ctx, cfg, paths, files, clients, col.ch)
	}
	summary(col.finish(), time.Since(start), cfg.Targets)
}

func targetsDesc(ts []s3.Target) string {
	var s []string
	for _, t := range ts {
		s = append(s, fmt.Sprintf("%s=%s/%s %dw", t.ID, t.Region, t.Bucket, t.Workers))
	}
	return strings.Join(s, ", ")
}

type job struct {
	doc  *origin.Doc
	key  string
	done *sync.WaitGroup
}

func runUpload(ctx context.Context, cfg Config, paths, files []string, clients []*s3.Client, results chan<- result) {
	fetcher := origin.New(cfg.Origin)
	fw := cfg.Origin.Workers
	if fw <= 0 {
		fw = 4
	}

	// Mỗi đích một hàng đợi + pool riêng: đích chậm không chặn đích khác.
	queues := make([]chan job, len(clients))
	var putWG sync.WaitGroup
	for i, c := range clients {
		q := make(chan job, c.T.Workers*2)
		queues[i] = q
		for w := 0; w < c.T.Workers; w++ {
			putWG.Add(1)
			go func(c *s3.Client, q chan job) {
				defer putWG.Done()
				for j := range q {
					pr, err := c.Put(ctx, j.key, j.doc.Body, cfg.Headers)
					r := result{Path: j.doc.Path, Stage: "put", Target: c.T.ID, Key: j.key, OK: err == nil,
						Ms: ms(pr.Duration), Bytes: len(j.doc.Body)}
					if err != nil {
						r.Err = err.Error()
					}
					results <- r
					j.done.Done()
				}
			}(c, q)
		}
	}

	in := make(chan string)
	var fetchWG sync.WaitGroup
	for w := 0; w < fw; w++ {
		fetchWG.Add(1)
		go func() {
			defer fetchWG.Done()
			for p := range in {
				d, err := fetcher.Get(ctx, p)
				if err != nil {
					results <- result{Path: p, Stage: "fetch", Err: err.Error()}
					continue
				}
				results <- result{Path: p, Stage: "fetch", OK: true, Ms: ms(d.Duration), Bytes: len(d.Body), Served: d.Served, Rendered: d.Rendered}
				var wg sync.WaitGroup
				for _, file := range files {
					for i, c := range clients {
						wg.Add(1)
						queues[i] <- job{doc: d, key: keyFor(c.T.Prefix, p, file), done: &wg}
					}
				}
				// Không chờ wg ở đây: worker lấy trang tiếp theo ngay (hàng đợi mỗi đích có giới hạn,
				// nên nếu S3 chậm thì bước fetch tự chậm theo).
			}
		}()
	}
	for _, p := range paths {
		select {
		case <-ctx.Done():
		case in <- p:
			continue
		}
		break
	}
	close(in)
	fetchWG.Wait()
	for _, q := range queues {
		close(q)
	}
	putWG.Wait()
}

func runDelete(ctx context.Context, paths, files []string, clients []*s3.Client, results chan<- result) {
	var wg sync.WaitGroup
	for _, c := range clients {
		q := make(chan string)
		for w := 0; w < c.T.Workers; w++ {
			wg.Add(1)
			go func(c *s3.Client) {
				defer wg.Done()
				for key := range q {
					t := time.Now()
					err := c.Delete(ctx, key)
					r := result{Path: key, Stage: "delete", Target: c.T.ID, Key: key, OK: err == nil, Ms: ms(time.Since(t))}
					if err != nil {
						r.Err = err.Error()
					}
					results <- r
				}
			}(c)
		}
		go func(c *s3.Client, q chan string) {
			for _, p := range paths {
				for _, f := range files {
					q <- keyFor(c.T.Prefix, p, f)
				}
			}
			close(q)
		}(c, q)
	}
	wg.Wait()
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func pct(v []float64, p float64) float64 {
	if len(v) == 0 {
		return 0
	}
	sort.Float64s(v)
	i := int(p * float64(len(v)-1))
	return v[i]
}

func summary(all []result, total time.Duration, ts []s3.Target) {
	type agg struct {
		ok, fail int
		ms       []float64
		bytes    int
		errs     map[string]int
	}
	by := map[string]*agg{}
	rendered, served := 0, map[string]int{}
	for _, r := range all {
		k := r.Stage
		if r.Target != "" {
			k += ":" + r.Target
		}
		a := by[k]
		if a == nil {
			a = &agg{errs: map[string]int{}}
			by[k] = a
		}
		if r.OK {
			a.ok++
			a.ms = append(a.ms, r.Ms)
			a.bytes += r.Bytes
		} else {
			a.fail++
			e := r.Err
			if len(e) > 120 {
				e = e[:120]
			}
			a.errs[e]++
		}
		if r.Stage == "fetch" && r.OK {
			if r.Rendered {
				rendered++
			}
			served[r.Served]++
		}
	}
	keys := make([]string, 0, len(by))
	for k := range by {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Printf("\nTổng thời gian: %.1f s\n", total.Seconds())
	fmt.Printf("%-14s %6s %5s %9s %9s %9s %9s %10s\n", "bước", "ok", "lỗi", "p50 ms", "p95 ms", "max ms", "/giây", "MB")
	for _, k := range keys {
		a := by[k]
		fmt.Printf("%-14s %6d %5d %9.0f %9.0f %9.0f %9.1f %10.1f\n", k, a.ok, a.fail,
			pct(a.ms, .5), pct(a.ms, .95), pct(a.ms, 1), float64(a.ok)/total.Seconds(), float64(a.bytes)/1e6)
		n := 0
		for e, c := range a.errs {
			if n++; n > 5 {
				break
			}
			fmt.Printf("    %d× %s\n", c, e)
		}
	}
	if len(served) > 0 {
		fmt.Printf("fetch: %d trang phải render (chưa có cache); nguồn phục vụ: %v\n", rendered, served)
	}
	_ = ts
}
