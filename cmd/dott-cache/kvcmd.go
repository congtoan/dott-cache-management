package main

// Lệnh kv: đồng bộ key Cloudflare KV (Worker dùng để biết URL nào có bản HTML trên S3).
//
//	dott-cache kv create -site <domain> -title <tên>    # tạo namespace mới (test), in ra id
//	dott-cache kv list   -site <domain> [-prefix /x/]
//	dott-cache kv sync   -site <domain> [-dry-run] [-force-remove]
//
// sync: key cần có = path của các URL active trong data/<domain>/state.db (qua kv.Key: viết thường, "/" cuối).
// BẢN THỬ: chưa kiểm tra URL đã có trên S3 hay chưa, vì trạng thái upload chưa được ghi vào DB. Khi có bảng
// url_targets, chỉ URL đã upload OK lên mọi đích mới được có key (thứ tự an toàn: upload trước, PUT key sau;
// gỡ thì DELETE key trước, xóa S3 sau). Vì vậy hiện chỉ dùng với namespace test.
// Sau khi xóa thì list lại toàn bộ, key còn sót thì xóa lại một lần (F17).

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/congtoan/dott-cache-management/internal/kv"
	"github.com/congtoan/dott-cache-management/internal/store"
)

// KVConfig trong config/sites/<domain>.json.
type KVConfig struct {
	NamespaceID string `json:"namespace_id"`
	CFEnv       string `json:"cf_env"` // file có CF_API_TOKEN, CF_ACCOUNT_ID
	Value       string `json:"value"`  // giá trị của mỗi key (Worker chỉ kiểm tra key có tồn tại); mặc định "1"
}

func cmdKV(args []string) {
	if len(args) < 1 {
		usage()
	}
	sub := args[0]
	fs := flag.NewFlagSet("kv "+sub, flag.ExitOnError)
	site := fs.String("site", "", "domain")
	cfgPath := fs.String("config", "", "file cấu hình JSON")
	title := fs.String("title", "", "tên namespace (kv create)")
	prefix := fs.String("prefix", "", "lọc theo prefix (kv list)")
	dry := fs.Bool("dry-run", false, "chỉ tính, không ghi KV")
	force := fs.Bool("force-remove", false, "cho phép xóa nhiều hơn max_remove_pct key")
	fs.Parse(args[1:])

	cfg := loadConfig(resolveConfig(*cfgPath, *site))
	envPath := cfg.KV.CFEnv
	if envPath == "" {
		envPath = home() + "/config/secrets/cf.env"
	}
	env := loadEnv(envPath)
	if env["CF_API_TOKEN"] == "" || env["CF_ACCOUNT_ID"] == "" {
		die("thiếu CF_API_TOKEN/CF_ACCOUNT_ID trong %s", envPath)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c := kv.New(env["CF_API_TOKEN"], env["CF_ACCOUNT_ID"], cfg.KV.NamespaceID)

	switch sub {
	case "create":
		if *title == "" {
			die("cần -title")
		}
		id, err := c.CreateNamespace(ctx, *title)
		if err != nil {
			die("tạo namespace: %v", err)
		}
		fmt.Printf("đã tạo namespace %q: %s (thêm vào config: \"kv\": {\"namespace_id\": \"%s\"})\n", *title, id, id)
		return
	}
	if cfg.KV.NamespaceID == "" {
		die("config thiếu kv.namespace_id")
	}
	switch sub {
	case "list":
		t := time.Now()
		keys, err := c.ListKeys(ctx, *prefix)
		if err != nil {
			die("list: %v", err)
		}
		fmt.Printf("%d key (%.1f s)\n", len(keys), time.Since(t).Seconds())
		for i, k := range keys {
			if i >= 10 {
				break
			}
			fmt.Println("  " + k)
		}
	case "sync":
		kvSync(ctx, c, cfg, *site, *dry, *force)
	default:
		usage()
	}
}

func kvSync(ctx context.Context, c *kv.Client, cfg Config, site string, dry, force bool) {
	start := time.Now()
	db, err := store.Open(siteData(site, cfg))
	if err != nil {
		die("%v", err)
	}
	want := map[string]bool{}
	rows, err := db.Query(`SELECT path FROM urls WHERE status='active'`)
	if err != nil {
		die("%v", err)
	}
	for rows.Next() {
		var p string
		rows.Scan(&p)
		want[kv.Key(p)] = true
	}
	rows.Close()
	db.Close()

	have, err := c.ListKeys(ctx, "")
	if err != nil {
		die("list: %v", err)
	}
	tList := time.Since(start)
	haveSet := map[string]bool{}
	for _, k := range have {
		haveSet[k] = true
	}
	var put, del []string
	for k := range want {
		if !haveSet[k] {
			put = append(put, k)
		}
	}
	for _, k := range have {
		if !want[k] {
			del = append(del, k)
		}
	}
	sort.Strings(put)
	sort.Strings(del)
	fmt.Printf("kv sync: DB %d key cần có, KV đang có %d (list %.1f s): thêm %d, xóa %d\n", len(want), len(have), tList.Seconds(), len(put), len(del))
	maxPct := cfg.URLs.MaxRemovePct
	if maxPct <= 0 {
		maxPct = 2
	}
	if len(have) >= 100 && float64(len(del))*100/float64(len(have)) > maxPct && !force {
		die("sẽ xóa %d key (%.1f%%), vượt ngưỡng %.1f%%: không ghi. Kiểm tra rồi chạy với -force-remove nếu đúng",
			len(del), float64(len(del))*100/float64(len(have)), maxPct)
	}
	if dry {
		fmt.Println("(dry-run: không ghi KV)")
		return
	}
	val := cfg.KV.Value
	if val == "" {
		val = "1"
	}
	if len(put) > 0 {
		t := time.Now()
		failed, err := c.PutBulk(ctx, put, val)
		fmt.Printf("  PUT %d key: %.1f s, lỗi %d %v\n", len(put), time.Since(t).Seconds(), len(failed), errStr(err))
	}
	if len(del) > 0 {
		t := time.Now()
		failed, err := c.DeleteBulk(ctx, del)
		fmt.Printf("  DELETE %d key: %.1f s, API báo lỗi %d %v\n", len(del), time.Since(t).Seconds(), len(failed), errStr(err))
	}
	if len(put)+len(del) == 0 {
		fmt.Printf("không có gì thay đổi (%.1f s)\n", time.Since(start).Seconds())
		return
	}
	// Kiểm tra lại (F17): list lại toàn bộ, sót thì làm lại một lần.
	for round := 1; round <= 2; round++ {
		have2, err := c.ListKeys(ctx, "")
		if err != nil {
			die("list kiểm tra: %v", err)
		}
		s2 := map[string]bool{}
		var leftDel, leftPut []string
		for _, k := range have2 {
			s2[k] = true
			if !want[k] {
				leftDel = append(leftDel, k)
			}
		}
		for k := range want {
			if !s2[k] {
				leftPut = append(leftPut, k)
			}
		}
		fmt.Printf("  kiểm tra lần %d: KV có %d key, còn thiếu %d, còn thừa %d\n", round, len(have2), len(leftPut), len(leftDel))
		if len(leftPut)+len(leftDel) == 0 || round == 2 {
			break
		}
		if len(leftPut) > 0 {
			c.PutBulk(ctx, leftPut, val)
		}
		if len(leftDel) > 0 {
			c.DeleteBulk(ctx, leftDel)
		}
		time.Sleep(2 * time.Second)
	}
	fmt.Printf("xong trong %.1f s\n", time.Since(start).Seconds())
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return "(" + err.Error() + ")"
}
