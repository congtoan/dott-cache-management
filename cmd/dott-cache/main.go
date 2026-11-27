// dott-cache: hệ thống cache v2 (chạy trên VPS phụ).
//
// Bản thử (27/11): chỉ có lệnh "upload".
//
//	dott-cache upload -config /etc/dott-cache/upload-test.json -paths paths.txt [-limit N] [-mobile-copy] [-log out.jsonl]
//	dott-cache delete -config ... -paths paths.txt      # xóa object đã upload (dọn bucket test)
//
// Luồng upload: đọc danh sách path → pool lấy HTML từ origin (gọi thẳng IP, nhận file cache
// nguyên văn) → mỗi trang được đẩy song song lên MỌI đích S3, mỗi đích một pool riêng.
// HTML chỉ lấy từ origin MỘT lần cho mọi đích.
package main

import (
	"bufio"
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

	"github.com/congtoan/dott-cache-management/internal/origin"
	"github.com/congtoan/dott-cache-management/internal/s3"
)

type Config struct {
	Origin  origin.Config     `json:"origin"`
	Targets []s3.Target       `json:"targets"`
	S3Env   string            `json:"s3_env"`
	Headers map[string]string `json:"headers"`
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
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "dùng: dott-cache upload|delete -config file.json -paths paths.txt [-limit N] [-mobile-copy] [-log out.jsonl]")
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

func readPaths(p string, limit int) []string {
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
		if i := strings.Index(l, "://"); i >= 0 { // chấp nhận cả URL đầy đủ
			if j := strings.Index(l[i+3:], "/"); j >= 0 {
				l = l[i+3+j:]
			} else {
				l = "/"
			}
		}
		if !strings.HasSuffix(l, "/") {
			l += "/"
		}
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

// keyFor: path "/en/about-us/" → "<prefix>en/about-us/index-https.html" (giống tool cũ).
func keyFor(prefix, path, file string) string {
	return prefix + strings.TrimPrefix(path, "/") + file
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

func cmdUpload(args []string, del bool) {
	fs := flag.NewFlagSet("upload", flag.ExitOnError)
	cfgPath := fs.String("config", "/etc/dott-cache/upload-test.json", "file cấu hình JSON")
	pathsFile := fs.String("paths", "", "file danh sách path (mỗi dòng một path hoặc URL)")
	limit := fs.Int("limit", 0, "chỉ xử lý N path đầu")
	mobileCopy := fs.Bool("mobile-copy", false, "upload thêm cùng nội dung lên key index-mobile-https.html (giai đoạn chuyển tiếp)")
	logPath := fs.String("log", "", "ghi kết quả từng bước ra file JSONL")
	fs.Parse(args)
	if *pathsFile == "" {
		usage()
	}

	raw, err := os.ReadFile(*cfgPath)
	if err != nil {
		die("đọc config: %v", err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		die("config JSON: %v", err)
	}
	env := loadEnv(cfg.S3Env)
	ak, sk := env["S3_ACCESS_KEY"], env["S3_SECRET_KEY"]
	if ak == "" || sk == "" {
		die("thiếu S3_ACCESS_KEY/S3_SECRET_KEY trong %s", cfg.S3Env)
	}
	if cfg.Headers == nil {
		cfg.Headers = map[string]string{"Content-Type": "text/html", "Cache-Control": "public, max-age=31536000", "x-amz-acl": "public-read"}
	}

	paths := readPaths(*pathsFile, *limit)
	files := []string{"index-https.html"}
	if *mobileCopy {
		files = append(files, "index-mobile-https.html")
	}

	clients := make([]*s3.Client, 0, len(cfg.Targets))
	for _, t := range cfg.Targets {
		c, err := s3.New(t, ak, sk)
		if err != nil {
			die("đích %s: %v", t.ID, err)
		}
		clients = append(clients, c)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	results := make(chan result, 256)
	var logW *bufio.Writer
	if *logPath != "" {
		f, err := os.Create(*logPath)
		if err != nil {
			die("tạo log: %v", err)
		}
		defer f.Close()
		logW = bufio.NewWriter(f)
		defer logW.Flush()
	}
	var all []result
	collected := make(chan struct{})
	go func() {
		enc := (*json.Encoder)(nil)
		if logW != nil {
			enc = json.NewEncoder(logW)
		}
		for r := range results {
			all = append(all, r)
			if enc != nil {
				enc.Encode(r)
			}
		}
		close(collected)
	}()

	start := time.Now()
	fmt.Printf("%s %d path × %d file × %d đích (%s)\n", map[bool]string{false: "upload", true: "delete"}[del], len(paths), len(files), len(clients), targetsDesc(cfg.Targets))

	if del {
		runDelete(ctx, paths, files, clients, results)
	} else {
		runUpload(ctx, cfg, paths, files, clients, results)
	}
	close(results)
	<-collected
	summary(all, time.Since(start), cfg.Targets)
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
