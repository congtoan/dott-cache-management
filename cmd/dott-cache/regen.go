package main

// "update -all" (tên cũ: regen-all): tạo lại cache cho TOÀN BỘ URL active trong DB (khi có thay đổi chung
// cho cả site: plugin mới, sửa theme...; và chạy theo lịch, vd hằng tháng, thay cho vòng renew vô hạn cũ).
//
//	dott-cache update -site <domain> -all               # chỉ kiểm kê và in kế hoạch (không xóa, không ghi)
//	dott-cache update -site <domain> -all -yes          # chạy thật
//	                  [-batch 200] [-purge-all-first] [-keep-orphans] [-force]
//
// Điều tiết theo load (config "throttle", mặc định max_load 1.5, resume_load 1.0): trước mỗi lô, Go đọc
// load của VPS chính và nghỉ nếu cao; trong lô, PHP kiểm tra mỗi 5 URL và nghỉ tối đa 60 s mỗi lần.
//
// Theo nguyên tắc đã thống nhất: mỗi URL chỉ có MỘT bản cache trên server (index-https.html +
// index-https.html_gzip, không có bản mobile), và chỉ upload index-https.html lên S3.
//
// "Mồ côi" = file cache (server, S3) hoặc key KV của một path KHÔNG có trong tập URL active của DB
// (bài đã xóa hoặc chuyển nháp, đổi slug, URL bị loại theo config, trang phân trang không còn,
// thư mục test). Cách xác định: lấy thư mục chứa file (S3: bỏ prefix và tên file), chuẩn hóa
// như Worker (viết thường, "/" cuối), so với tập path active. Rủi ro duy nhất là DB THIẾU URL, nên có 2 lớp chặn:
//   - DB phải được "urls sync" toàn bộ trong vòng 26 giờ;
//   - số path mồ côi không được vượt max_remove_pct (mặc định 2%) số URL.
// Vượt một trong hai thì dừng trước khi xóa, trừ khi có -force.
//
// Các bước (mồ côi được xóa TRƯỚC khi generate, chủ site chốt 28/11):
//  1. Kiểm kê: LIST S3 trên mọi đích MỘT lần (manifest, ghi ra logs/<domain>/...-s3-manifest-<đích>.txt),
//     list KV, đếm file trên server (dott-prefork --purge-cache --dry-run). Chỉ key thật sự có mới được xóa.
//  2. Xóa KV mồ côi TRƯỚC (để Worker không dẫn khách tới object sắp bị xóa).
//  3. Dọn server (dott-prefork --purge-cache): xóa thư mục mồ côi, mọi bản mobile và .mobile-active
//     (hệ thống cũ có 5 file mỗi URL), file lạ. Bản desktop của URL trong DB được giữ tới lúc render
//     (prefork xóa từng URL ngay trước khi render). -purge-all-first: xóa hết ngay từ đầu.
//  4. Xóa S3 MỘT lần: index-https.html mồ côi và mọi file KHÔNG phải index-https.html (bản mobile,
//     _gzip của hệ thống cũ), theo lô 1.000 key, nhiều lô song song mỗi đích. index-https.html của
//     URL trong DB được GIỮ và ghi đè khi upload, nên Worker luôn có bản để phục vụ trong lúc regen.
//     -keep-orphans: không xóa mồ côi ở cả 3 nơi (chỉ báo cáo).
//  5. Thứ tự: trang chủ và bài đơn → trang 1 của archive (category, tag, taxonomy, post type) →
//     trang phân trang. Prefork theo lô (-batch URL mỗi lần ssh). Lô nào xong thì lấy HTML qua HTTP và upload lên mọi
//     đích NGAY, song song với lô prefork tiếp theo. URL prefork lỗi (không phải 404) thì thử lại bằng HTTP.
//  6. Kiểm tra lại: LIST S3 lần nữa và đếm file trên server.

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/congtoan/dott-cache-management/internal/kv"
	"github.com/congtoan/dott-cache-management/internal/s3"
	"github.com/congtoan/dott-cache-management/internal/store"
)

// lastPurge / lastPurgeRaw: dòng "PURGE {json}" gần nhất mà runRemote đọc được (dạng in ra / JSON gốc).
// Lệnh CLI chạy tuần tự nên không cần khóa.
var lastPurge, lastPurgeRaw string

type s3Inventory struct {
	html, other, orphan int
	orphanDirs          map[string]bool
	keys                []string // mọi key cần xóa (theo chế độ)
	htmlKeep            map[string]bool
}

// sample: tối đa 6 phần tử đầu để in ra (đã sắp xếp).
func sample(s []string) string {
	if len(s) == 0 {
		return ""
	}
	if len(s) <= 6 {
		return "[" + strings.Join(s, " ") + "]"
	}
	return fmt.Sprintf("[%s ... và %d khác]", strings.Join(s[:6], " "), len(s)-6)
}

func ageStr(ts int64, age time.Duration) string {
	if ts == 0 {
		return "chưa bao giờ"
	}
	return fmt.Sprintf("%s (%.1f giờ trước)", time.Unix(ts, 0).Format("2006-01-02 15:04"), age.Hours())
}

func purgeNum(k string) int {
	var m map[string]any
	json.Unmarshal([]byte(lastPurgeRaw), &m)
	if v, ok := m[k].(float64); ok {
		return int(v)
	}
	return 0
}

func cmdRegenAll(args []string) {
	fs := flag.NewFlagSet("update -all", flag.ExitOnError)
	site := fs.String("site", "", "domain")
	cfgPath := fs.String("config", "", "file cấu hình JSON")
	yes := fs.Bool("yes", false, "chạy thật (không có cờ này thì chỉ kiểm kê và in kế hoạch)")
	batch := fs.Int("batch", 200, "số URL mỗi lô prefork (lô nhỏ thì điều tiết theo load kịp hơn)")
	purgeFirst := fs.Bool("purge-all-first", false, "xóa hết file cache trên server ngay từ đầu")
	keepOrphans := fs.Bool("keep-orphans", false, "KHÔNG xóa cache mồ côi (URL không còn trong DB) trên server, S3, KV")
	force := fs.Bool("force", false, "bỏ qua kiểm tra DB cũ / số mồ côi vượt ngưỡng")
	blog := fs.String("blog", "", "chỉ một blog: path (\"/\", \"/en/\") hoặc blog_id; dọn mồ côi cũng chỉ trong blog đó")
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
	// Thứ tự (chủ site chốt 28/11): trang chủ, bài đơn trước; rồi trang 1 của archive (category, tag,
	// taxonomy khác, archive post type); các trang phân trang /page/N/ sau cùng, N nhỏ trước.
	// Trang quan trọng có cache mới sớm nhất nếu regen bị dừng giữa chừng.
	bs := loadBlogScope(db, *blog)
	paths := activePaths(db, bs)
	// Mồ côi luôn so với URL active của MỌI blog, để lọc theo blog không làm URL của blog khác thành mồ côi.
	allActive := activePaths(db, nil)
	lastFull, _ := strconv.ParseInt(store.Meta(db, "urls_full_last"), 10, 64)
	db.Close()
	if len(paths) == 0 {
		die("DB không có URL active: chạy 'dott-cache urls sync' trước")
	}
	dbAge := time.Since(time.Unix(lastFull, 0))
	keep := map[string]bool{} // URL trong phạm vi (sẽ generate)
	for _, p := range paths {
		keep[kv.Key(p)] = true
	}
	keepAll := map[string]bool{} // URL active của mọi blog (để xác định mồ côi)
	for _, p := range allActive {
		keepAll[kv.Key(p)] = true
	}
	// Dòng phạm vi gửi kèm cho PHP (--purge-cache): chỉ đụng file trong blog đang chọn.
	purgeInput := allActive
	if bs != nil {
		sc, _ := json.Marshal(map[string]any{"include": bs.Path, "exclude": bs.exclude})
		purgeInput = append([]string{"#scope " + string(sc)}, allActive...)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	start := time.Now()
	fmt.Printf("update -all %s (%s): %d URL trong DB, %d đích (%s), lô %d\n", cfg.Origin.Host, bs, len(paths), len(clients), targetsDesc(cfg.Targets), *batch)

	// ---- 1. Kiểm kê ----
	t := time.Now()
	inv := make([]*s3Inventory, len(clients))
	var wg sync.WaitGroup
	var invErr error
	for i, c := range clients {
		wg.Add(1)
		go func(i int, c *s3.Client) {
			defer wg.Done()
			in := &s3Inventory{htmlKeep: map[string]bool{}, orphanDirs: map[string]bool{}}
			var man strings.Builder
			err := c.List(ctx, c.T.Prefix, func(o s3.ListResult) {
				fmt.Fprintf(&man, "%s\t%d\n", o.Key, o.Size)
				rel := strings.TrimPrefix(o.Key, c.T.Prefix)
				dir, file := "/", rel
				if j := strings.LastIndex(rel, "/"); j >= 0 {
					dir, file = "/"+rel[:j+1], rel[j+1:]
				}
				dir = kv.Key(dir)
				if !bs.In(dir) {
					return // ngoài blog đang chọn: không đụng
				}
				switch {
				case !keepAll[dir]:
					in.orphan++ // mồ côi (mọi loại file)
					in.orphanDirs[dir] = true
					if *keepOrphans {
						return
					}
				case file != "index-https.html":
					in.other++ // bản mobile, _gzip...: xóa ngay từ đầu
				default:
					in.html++
					in.htmlKeep[dir] = true
					return // giữ, upload sẽ ghi đè
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
	maxOrphans := 0
	for i, c := range clients {
		fmt.Printf("[1] S3 %s: index-https.html của URL trong DB %d (giữ, sẽ ghi đè), file kiểu cũ (mobile, _gzip) %d, mồ côi %d file / %d path → sẽ xóa %d object\n",
			c.T.ID, inv[i].html, inv[i].other, inv[i].orphan, len(inv[i].orphanDirs), len(inv[i].keys))
		maxOrphans = max(maxOrphans, len(inv[i].orphanDirs))
		ds := make([]string, 0, len(inv[i].orphanDirs))
		for d := range inv[i].orphanDirs {
			ds = append(ds, d)
		}
		sort.Strings(ds)
		if len(ds) > 0 {
			fmt.Printf("      mồ côi: %s\n", sample(ds))
		}
	}
	// KV: key không có trong tập path active.
	var kvc *kv.Client
	var kvOrphans []string
	if cfg.KV.NamespaceID != "" {
		envPath := cfg.KV.CFEnv
		if envPath == "" {
			envPath = home() + "/config/secrets/cf.env"
		}
		env := loadEnv(envPath)
		kvc = kv.New(env["CF_API_TOKEN"], env["CF_ACCOUNT_ID"], cfg.KV.NamespaceID)
		ks, err := kvc.ListKeys(ctx, "")
		if err != nil {
			die("list KV: %v", err)
		}
		for _, k := range ks {
			if bs.In(k) && !keepAll[k] {
				kvOrphans = append(kvOrphans, k)
			}
		}
		sort.Strings(kvOrphans)
		fmt.Printf("[1] KV: %d key, mồ côi %d %s\n", len(ks), len(kvOrphans), sample(kvOrphans))
		maxOrphans = max(maxOrphans, len(kvOrphans))
	}
	if _, _, err := runRemote(ctx, cfg.Renew.SSHHost, cfg.Renew.Command+" --purge-cache --dry-run", "purge", purgeInput, nil); err != nil {
		die("kiểm kê server: %v", err)
	}
	fmt.Printf("[1] server (trước): %s  (%.1f s)\n", lastPurge, time.Since(t).Seconds())
	maxOrphans = max(maxOrphans, purgeNum("path_mồ_côi"))

	// Hai lớp chặn trước khi xóa mồ côi.
	maxPct := cfg.URLs.MaxRemovePct
	if maxPct <= 0 {
		maxPct = 2
	}
	var stopWhy []string
	if !*keepOrphans {
		if lastFull == 0 || dbAge > 26*time.Hour {
			stopWhy = append(stopWhy, fmt.Sprintf("DB chưa được 'urls sync' toàn bộ trong 26 giờ qua (lần cuối: %s)", ageStr(lastFull, dbAge)))
		}
		if float64(maxOrphans)*100/float64(len(paths)) > maxPct {
			stopWhy = append(stopWhy, fmt.Sprintf("số path mồ côi %d (%.1f%% số URL) vượt ngưỡng %.1f%%", maxOrphans, float64(maxOrphans)*100/float64(len(paths)), maxPct))
		}
	}
	fmt.Printf("[1] DB: urls sync toàn bộ lần cuối %s; mồ côi nhiều nhất %d path (%.2f%%, ngưỡng %.1f%%)\n",
		ageStr(lastFull, dbAge), maxOrphans, float64(maxOrphans)*100/float64(len(paths)), maxPct)
	if len(stopWhy) > 0 && !*force {
		for _, s := range stopWhy {
			fmt.Println("    DỪNG: " + s)
		}
		fmt.Println("    Chạy 'dott-cache urls sync' rồi thử lại; hoặc -keep-orphans (không xóa mồ côi); hoặc -force nếu đã kiểm tra.")
		if *yes {
			os.Exit(1)
		}
	}
	if !*yes {
		fmt.Println("\nChưa xóa hay ghi gì. Chạy lại với -yes để thực hiện.")
		return
	}

	// ---- 2. Xóa KV mồ côi TRƯỚC ----
	if kvc != nil && len(kvOrphans) > 0 && !*keepOrphans {
		t = time.Now()
		failed, err := kvc.DeleteBulk(ctx, kvOrphans)
		fmt.Printf("[2] KV: xóa %d key mồ côi, API báo lỗi %d %s (%.1f s)\n", len(kvOrphans), len(failed), errStr(err), time.Since(t).Seconds())
	} else {
		fmt.Println("[2] KV: không có gì cần xóa")
	}

	// ---- 3. Dọn server ----
	t = time.Now()
	pc := cfg.Renew.Command + " --purge-cache"
	if *purgeFirst {
		pc += " --all"
	}
	if *keepOrphans {
		pc += " --keep-orphans"
	}
	if _, _, err := runRemote(ctx, cfg.Renew.SSHHost, pc, "purge", purgeInput, nil); err != nil {
		die("dọn server: %v", err)
	}
	fmt.Printf("[3] dọn server: %s (%.1f s)\n", lastPurge, time.Since(t).Seconds())

	// ---- 4. Xóa S3 theo lô ----
	t = time.Now()
	for i, c := range clients {
		wg.Add(1)
		go func(c *s3.Client, keys []string) {
			defer wg.Done()
			n, fails := deleteKeys(ctx, c, keys, 4)
			fmt.Printf("[4] S3 %s: xóa %d/%d object, lỗi %d (%.1f s)\n", c.T.ID, n, len(keys), len(fails), time.Since(t).Seconds())
			for k, e := range fails {
				fmt.Printf("      %s: %s\n", k, e)
				break
			}
		}(c, inv[i].keys)
	}
	wg.Wait()

	// ---- 5. Prefork theo lô (có điều tiết theo load) + upload song song ----
	cfg.Origin.MinCachedAt = start.Unix() - 5
	col := newCollector(*logPath)
	tRender := time.Now()
	gs := generate(ctx, cfg, clients, paths, *batch, col, "5")
	all := col.finish()
	fmt.Printf("[5] prefork + upload: %.1f s (upload chạy song song, tổng %.1f s); điều tiết: nghỉ trước lô %d lần (%.0f s), PHP nghỉ %d lần (%d s), load cao nhất trước lô %.2f\n",
		time.Since(tRender).Seconds(), gs.tUpload.Seconds(), gs.waits, gs.waited.Seconds(), gs.phpPauses, gs.phpWaited, gs.maxLoad)
	if kvc != nil {
		kvEnsure(ctx, kvc, cfg, paths, all, len(clients), "5")
	}

	// ---- 6. Kiểm tra lại ----
	t = time.Now()
	for _, c := range clients {
		have, other, orphanHTML, fresh := 0, 0, 0, 0
		var missing []string
		got := map[string]bool{}
		c.List(ctx, c.T.Prefix, func(o s3.ListResult) {
			rel := "/" + strings.TrimPrefix(o.Key, c.T.Prefix)
			dir := rel
			if j := strings.LastIndex(rel, "/"); j >= 0 {
				dir = rel[:j+1]
			}
			if !bs.In(dir) {
				return // ngoài blog đang chọn
			}
			switch {
			case !strings.HasSuffix(rel, "/index-https.html"):
				other++
			case keep[dir]:
				have++
				got[dir] = true
			case keepAll[dir]:
				// URL active của blog khác nằm trong prefix (không xảy ra khi đã lọc bs.In)
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
		fmt.Printf("[6] S3 %s: có index-https.html %d/%d URL (ghi mới lần này %d), file khác còn lại %d, html mồ côi %d",
			c.T.ID, have, len(keep), fresh, other, orphanHTML)
		if len(missing) > 0 {
			fmt.Printf(", thiếu: %s", strings.Join(missing[:min(5, len(missing))], " "))
		}
		fmt.Println()
	}
	runRemote(ctx, cfg.Renew.SSHHost, cfg.Renew.Command+" --purge-cache --dry-run", "purge", purgeInput, nil)
	fmt.Printf("[6] server (sau): %s (%.1f s)\n", lastPurge, time.Since(t).Seconds())

	summary(all, time.Since(start), cfg.Targets)
	bad := failedPaths(paths, all, len(clients))
	fmt.Printf("\nupdate -all xong: %d/%d URL ok, %d URL chưa xong, tổng %.1f s (%.1f URL/s)\n",
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
