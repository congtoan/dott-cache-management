package main

// Lệnh gửi việc vào pool của "dott-cache serve" (qua unix socket data/serve.sock):
//
//	dott-cache submit -site <domain> -url URL | -urls a,b | -txt file [-remove] [-prio 3]
//	dott-cache submit -site <domain> -all [-blog /]      export toàn bộ → pool mức 4 (trang chủ → bài → archive → phân trang)
//	dott-cache pool status
//
// Việc gộp theo path trong pool (xem internal/pool). write_blogs vẫn được serve kiểm tra.

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/congtoan/dott-cache-management/internal/pool"
)

func cmdSubmit(args []string) {
	fs := flag.NewFlagSet("submit", flag.ExitOnError)
	site := fs.String("site", "", "domain")
	one := fs.String("url", "", "một URL")
	urls := fs.String("urls", "", "danh sách URL cách nhau dấu phẩy")
	txt := fs.String("txt", "", "file danh sách URL")
	all := fs.Bool("all", false, "export toàn bộ URL của site rồi đưa vào pool (mức 4)")
	blog := fs.String("blog", "", "với -all: chỉ blog có path này (vd / hoặc /en/)")
	remove := fs.Bool("remove", false, "gỡ thay vì làm mới")
	prio := fs.Int("prio", -1, "mức ưu tiên (mặc định 3; -all: 4)")
	fs.Parse(args)
	if *site == "" {
		die("cần -site")
	}
	cfg := loadConfig(resolveConfig("", *site))
	action := "refresh"
	if *remove {
		action = "remove"
	}
	var items []pool.Item
	if *all {
		if *remove {
			die("-all không dùng với -remove")
		}
		p := pool.PrioBulk
		if *prio >= 0 {
			p = *prio
		}
		items = exportAllItems(cfg, *blog, p)
	} else {
		p := pool.PrioManual
		if *prio >= 0 {
			p = *prio
		}
		var raw []string
		if *one != "" {
			raw = append(raw, *one)
		}
		for _, u := range strings.FieldsFunc(*urls, func(r rune) bool { return r == ',' || r == ' ' }) {
			raw = append(raw, u)
		}
		if *txt != "" {
			raw = append(raw, readPaths(*txt, 0, cfg.Origin.Host)...)
		}
		seen := map[string]bool{}
		for _, l := range raw {
			if err := checkLine(l, cfg.Origin.Host); err != nil {
				fmt.Printf("bỏ %q: %v\n", l, err)
				continue
			}
			pth := normPath(l)
			if !seen[pth] {
				seen[pth] = true
				items = append(items, pool.Item{Path: pth, Action: action, Prio: p, Src: "submit"})
			}
		}
	}
	if len(items) == 0 {
		die("không có URL nào")
	}
	body, _ := json.Marshal(submitReq{Site: *site, Items: items})
	resp, err := unixClient().Post("http://serve/submit", "application/json", bytes.NewReader(body))
	if err != nil {
		die("%v: %v", errNoServe, err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		die("serve trả %d: %s", resp.StatusCode, strings.TrimSpace(string(out)))
	}
	fmt.Printf("đã gửi %d việc (%s, mức %d): %s", len(items), action, items[0].Prio, out)
}

// exportAllItems: export toàn bộ (giữ khóa prefork trên VPS chính), lọc theo config và blog, sắp thứ tự.
func exportAllItems(cfg Config, blog string, prio int) []pool.Item {
	t0 := time.Now()
	cmd := exec.Command("ssh", "-n", "-o", "BatchMode=yes", cfg.Renew.SSHHost, cfg.Renew.Command+" --export-urls")
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
	if err != nil || !done {
		die("export lỗi hoặc dừng giữa chừng (%v): không gửi gì", err)
	}
	flt, err := newURLFilter(cfg.URLs)
	if err != nil {
		die("config urls: %v", err)
	}
	if blog != "" && !strings.HasSuffix(blog, "/") {
		blog += "/"
	}
	var items []pool.Item
	seen := map[string]bool{}
	skipped := 0
	for _, u := range urls {
		if (blog != "" && !strings.EqualFold(u.BP, blog)) || flt.reject(u) != "" || seen[u.P] {
			skipped++
			continue
		}
		seen[u.P] = true
		rank := 2
		switch {
		case u.K == "home":
			rank = 0
		case u.K == "single":
			rank = 1
		case u.PG > 1:
			rank = 3
		}
		items = append(items, pool.Item{Path: u.P, Action: "refresh", Prio: prio, Ord: rank*1_000_000 + u.PG, Src: "submit -all"})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Ord < items[j].Ord })
	fmt.Printf("export %d URL trong %.1f s, giữ %d, bỏ %d\n", len(urls), time.Since(t0).Seconds(), len(items), skipped)
	return items
}

func cmdPool(args []string) {
	if len(args) == 0 || args[0] != "status" {
		die("dùng: dott-cache pool status")
	}
	resp, err := unixClient().Get("http://serve/status")
	if err != nil {
		die("%v: %v", errNoServe, err)
	}
	defer resp.Body.Close()
	var st map[string]pool.Stats
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		die("%v", err)
	}
	names := make([]string, 0, len(st))
	for n := range st {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		s := st[n]
		fmt.Printf("%s: %d việc trong pool, %d sự kiện chưa xử lý\n", n, s.Total, s.Inbox)
		ks := make([]string, 0, len(s.Groups))
		for k := range s.Groups {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks {
			fmt.Printf("  %-24s %d\n", k, s.Groups[k])
		}
		for _, it := range s.Retrying {
			fmt.Printf("  thử lại: %s (%s, lần %d): %s\n", it.Path, it.Action, it.Tries, it.LastErr)
		}
	}
}
