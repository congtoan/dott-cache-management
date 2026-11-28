package main

// dott-cache reconcile: đối soát hằng ngày (thiết kế: docs/design-pool.md mục 8). Không dùng state.db.
//
//	dott-cache reconcile -site <domain> -blog / [-dry-run] [-max-pct 2] [-force]
//
// Đo lần đầu trên php81 /tttest/ (28/11): xem docs/rollout-log.md. Chưa đối soát đĩa VPS chính (để hằng tuần).
//
//   - "cần có"   = export toàn bộ từ site (lọc theo config urls và -blog);
//   - "đang có"  = key KV + object index-https.html trên MỌI đích S3;
//   - thiếu (cần có nhưng thiếu KV hoặc thiếu trên một đích S3) → pool, làm mới, mức 3;
//   - mồ côi (đang có trên KV hoặc S3 nhưng site không còn)     → pool, gỡ (KV → server → S3).
//
// Lớp chặn (không gửi gì, chỉ báo):
//   - export không có dòng DONE, hoặc rỗng;
//   - số mồ côi > max-pct % số URL cần có (mặc định 2), trừ khi -force;
//   - số URL cần có < 50% số key KV của blog (export thiếu bất thường).
// Cần dott-cache serve đang chạy (trừ -dry-run). Việc gộp theo path trong pool, nên chạy lại không sao.

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/congtoan/dott-cache-management/internal/kv"
	"github.com/congtoan/dott-cache-management/internal/pool"
	"github.com/congtoan/dott-cache-management/internal/s3"
)

func cmdReconcile(args []string) {
	fs := flag.NewFlagSet("reconcile", flag.ExitOnError)
	site := fs.String("site", "", "domain")
	blog := fs.String("blog", "", "path blog (vd / hoặc /en/); bỏ trống = mọi blog")
	dry := fs.Bool("dry-run", false, "chỉ báo, không gửi vào pool")
	maxPct := fs.Float64("max-pct", 2, "chặn khi số mồ côi vượt N% số URL cần có")
	force := fs.Bool("force", false, "bỏ qua chặn max-pct")
	fs.Parse(args)
	if *site == "" || *blog == "" {
		die("cần -site và -blog")
	}
	cfg, clients := setup(resolveConfig("", *site))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	t0 := time.Now()
	bp := strings.ToLower(*blog)
	if bp != "" && !strings.HasSuffix(bp, "/") {
		bp += "/"
	}

	// ---- 1. Cần có: export ----
	cmd := exec.CommandContext(ctx, "ssh", "-n", "-o", "BatchMode=yes", cfg.Renew.SSHHost, cfg.Renew.Command+" --export-urls")
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		die("%v", err)
	}
	if err := cmd.Start(); err != nil {
		die("ssh: %v", err)
	}
	urls, done, err := readExportQuiet(out)
	cmd.Wait()
	if err != nil || !done || len(urls) == 0 {
		die("export lỗi, dừng giữa chừng hoặc rỗng (%v): không làm gì", err)
	}
	flt, err := newURLFilter(cfg.URLs)
	if err != nil {
		die("config urls: %v", err)
	}
	// Các blog khác nằm bên trong blog đang xét (vd với "/": "/en/", "/es/"), để không coi key của chúng là mồ côi.
	var others []string
	for _, u := range urls {
		b := strings.ToLower(u.BP)
		if b != "" && b != bp && strings.HasPrefix(b, bp) {
			others = append(others, b)
		}
	}
	others = dedupe(others)
	inBlog := func(k string) bool {
		if !strings.HasPrefix(k, bp) {
			return false
		}
		for _, o := range others {
			if strings.HasPrefix(k, o) {
				return false
			}
		}
		return true
	}
	want := map[string]string{} // kv.Key(path) → path như site in ra
	for _, u := range urls {
		if bp != "" && !strings.EqualFold(u.BP, bp) {
			continue
		}
		if flt.reject(u) != "" {
			continue
		}
		want[kv.Key(u.P)] = u.P
	}
	tExport := time.Since(t0)

	// ---- 2. Đang có: KV + S3 (song song) ----
	t1 := time.Now()
	kvHave := map[string]bool{}
	s3Have := make([]map[string]bool, len(clients))
	var wg sync.WaitGroup
	var kvErr error
	if cfg.KV.NamespaceID != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			env := loadEnv(firstNonEmpty(cfg.KV.CFEnv, home()+"/config/secrets/cf.env"))
			keys, err := kv.New(env["CF_API_TOKEN"], env["CF_ACCOUNT_ID"], cfg.KV.NamespaceID).ListKeys(ctx, bp)
			kvErr = err
			for _, k := range keys {
				if inBlog(k) {
					kvHave[k] = true
				}
			}
		}()
	}
	s3Errs := make([]error, len(clients))
	for i, c := range clients {
		wg.Add(1)
		go func(i int, c *s3.Client) {
			defer wg.Done()
			m := map[string]bool{}
			pre := c.T.Prefix + strings.TrimPrefix(bp, "/")
			s3Errs[i] = c.List(ctx, pre, func(o s3.ListResult) {
				if !strings.HasSuffix(o.Key, "/index-https.html") {
					return
				}
				k := "/" + strings.TrimSuffix(strings.TrimPrefix(o.Key, c.T.Prefix), "index-https.html")
				if inBlog(k) {
					m[k] = true
				}
			})
			s3Have[i] = m
		}(i, c)
	}
	wg.Wait()
	if kvErr != nil {
		die("list KV lỗi: %v (không làm gì)", kvErr)
	}
	for i, e := range s3Errs {
		if e != nil {
			die("list S3 %s lỗi: %v (không làm gì)", clients[i].T.ID, e)
		}
	}
	tList := time.Since(t1)

	// ---- 3. So ----
	var missing, orphans []string
	noKV, noS3 := 0, 0
	for k, p := range want {
		miss := cfg.KV.NamespaceID != "" && !kvHave[k]
		if miss {
			noKV++
		}
		for _, m := range s3Have {
			if !m[k] {
				miss = true
				noS3++
				break
			}
		}
		if miss {
			missing = append(missing, p)
		}
	}
	seen := map[string]bool{}
	addOrphan := func(k string) {
		if _, ok := want[k]; !ok && !seen[k] {
			seen[k] = true
			orphans = append(orphans, k)
		}
	}
	for k := range kvHave {
		addOrphan(k)
	}
	for _, m := range s3Have {
		for k := range m {
			addOrphan(k)
		}
	}
	sort.Strings(missing)
	sort.Strings(orphans)
	desc := "blog " + bp
	fmt.Printf("reconcile %s (%s) %s: cần có %d URL (export %d trong %.1f s); KV %d key; S3 %s (list %.1f s)\n",
		cfg.Origin.Host, desc, time.Now().Format("2006-01-02 15:04"), len(want), len(urls), tExport.Seconds(), len(kvHave), s3Counts(clients, s3Have), tList.Seconds())
	fmt.Printf("  thiếu %d (thiếu KV %d, thiếu trên ít nhất một đích S3 %d), mồ côi %d\n", len(missing), noKV, noS3, len(orphans))
	if len(missing) > 0 {
		fmt.Printf("  thiếu: %s\n", sample(missing))
	}
	if len(orphans) > 0 {
		fmt.Printf("  mồ côi: %s\n", sample(orphans))
	}

	// ---- 4. Chặn ----
	if cfg.KV.NamespaceID != "" && len(kvHave) >= 100 && len(want) < len(kvHave)/2 {
		die("số URL cần có (%d) < 50%% số key KV (%d): export có thể thiếu, không làm gì", len(want), len(kvHave))
	}
	pct := float64(len(orphans)) * 100 / float64(max(1, len(want)))
	if pct > *maxPct && !*force {
		die("mồ côi %d (%.1f%%) vượt %.1f%%: không làm gì. Xem danh sách rồi chạy lại với -force nếu đúng", len(orphans), pct, *maxPct)
	}
	if *dry {
		fmt.Println("(dry-run: không gửi vào pool)")
		return
	}
	if len(missing)+len(orphans) == 0 {
		fmt.Printf("reconcile xong: khớp, %.1f s\n", time.Since(t0).Seconds())
		return
	}

	// ---- 5. Gửi vào pool ----
	var items []pool.Item
	for _, p := range orphans {
		items = append(items, pool.Item{Path: p, Action: "remove", Prio: pool.PrioRemove, Src: "reconcile mồ côi"})
	}
	for _, p := range missing {
		items = append(items, pool.Item{Path: p, Action: "refresh", Prio: pool.PrioManual, Src: "reconcile thiếu"})
	}
	body, _ := json.Marshal(submitReq{Site: *site, Items: items})
	resp, err := unixClient().Post("http://serve/submit", "application/json", bytes.NewReader(body))
	if err != nil {
		die("%v: %v", errNoServe, err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		die("serve trả %d: %s", resp.StatusCode, strings.TrimSpace(string(rb)))
	}
	fmt.Printf("reconcile xong: gửi %d việc vào pool (gỡ %d, làm mới %d): %s  %.1f s\n",
		len(items), len(orphans), len(missing), strings.TrimSpace(string(rb)), time.Since(t0).Seconds())
}

func s3Counts(clients []*s3.Client, have []map[string]bool) string {
	var s []string
	for i, c := range clients {
		s = append(s, fmt.Sprintf("%s %d", c.T.ID, len(have[i])))
	}
	return strings.Join(s, ", ")
}
