package main

// Phạm vi URL dùng chung cho "update" và "replace":
//   -all | -url URL | -urls [a,b] | -txt [file] | -segment [link,...] (-segment-mode) | -blog /en/
//
// -blog: chỉ lấy URL của một blog (subsite) của multisite. Giá trị là path blog ("/" là site chính,
// "/en/"...) hoặc blog_id. Một path thuộc blog "/" khi nó không thuộc blog nào có path dài hơn
// (vd /en/abc/ thuộc /en/, không thuộc /). Map blog_id → path do "urls sync" lưu trong DB (meta blog_paths).

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/congtoan/dott-cache-management/internal/store"
)

// blogScope: nil = mọi blog.
type blogScope struct {
	ID      int
	Path    string   // vd "/en/"
	exclude []string // path của các blog khác nằm bên trong Path (vd với "/": "/en/", "/es/")
}

// In: path có thuộc blog này không.
func (s *blogScope) In(p string) bool {
	if s == nil {
		return true
	}
	p = strings.ToLower(p)
	if !strings.HasPrefix(p, s.Path) {
		return false
	}
	for _, e := range s.exclude {
		if strings.HasPrefix(p, e) {
			return false
		}
	}
	return true
}

func (s *blogScope) String() string {
	if s == nil {
		return "mọi blog"
	}
	return fmt.Sprintf("blog %d %s", s.ID, s.Path)
}

// loadBlogScope: "" → nil (mọi blog).
func loadBlogScope(db *sql.DB, v string) *blogScope {
	if v == "" {
		return nil
	}
	bps := map[string]string{}
	json.Unmarshal([]byte(store.Meta(db, "blog_paths")), &bps)
	if len(bps) == 0 {
		die("DB chưa có map blog → path: chạy 'dott-cache urls sync' (toàn bộ) trước")
	}
	s := &blogScope{}
	if id, err := strconv.Atoi(v); err == nil {
		s.ID, s.Path = id, bps[v]
	} else {
		p := strings.ToLower(v)
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		if !strings.HasSuffix(p, "/") {
			p += "/"
		}
		for k, bp := range bps {
			if strings.ToLower(bp) == p {
				s.ID, _ = strconv.Atoi(k)
				s.Path = strings.ToLower(bp)
			}
		}
	}
	if s.Path == "" {
		var have []string
		for k, bp := range bps {
			have = append(have, k+"="+bp)
		}
		sort.Strings(have)
		die("không có blog %q; các blog: %s", v, strings.Join(have, " "))
	}
	for _, bp := range bps {
		bp = strings.ToLower(bp)
		if bp != s.Path && strings.HasPrefix(bp, s.Path) {
			s.exclude = append(s.exclude, bp)
		}
	}
	return s
}

// scopeFlags: các cờ phạm vi đăng ký trên một FlagSet.
type scopeFlags struct {
	one, urls, txt, seg, segMode, blog *string
}

func addScopeFlags(fs *flag.FlagSet) *scopeFlags {
	return &scopeFlags{
		one:     fs.String("url", "", "một URL"),
		urls:    fs.String("urls", "", "danh sách URL cách nhau dấu phẩy; không có giá trị thì hỏi"),
		txt:     fs.String("txt", "", "file danh sách URL; không có giá trị thì dùng lists/<domain>/update.txt"),
		seg:     fs.String("segment", "", "link archive cách nhau dấu phẩy; không có giá trị thì hỏi"),
		segMode: fs.String("segment-mode", "all", "all (archive + phân trang + bài) | archive | posts"),
		blog:    fs.String("blog", "", "chỉ một blog: path (\"/\", \"/en/\") hoặc blog_id"),
	}
}

func (sf *scopeFlags) any() bool {
	return *sf.one != "" || *sf.urls != "" || *sf.txt != "" || *sf.seg != ""
}

var stdinReader = bufio.NewReader(os.Stdin)

func askLine(q string) string {
	fmt.Print(q)
	l, _ := stdinReader.ReadString('\n')
	return strings.TrimSpace(l)
}

// gatherPaths: gom URL từ -url/-urls/-txt/-segment (bỏ trùng), lọc theo -blog.
func gatherPaths(ctx context.Context, cfg Config, db *sql.DB, site string, sf *scopeFlags, bs *blogScope) []string {
	seen := map[string]bool{}
	var paths []string
	outScope := 0
	add := func(raw, src string) {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.HasPrefix(raw, "#") {
			return
		}
		if err := checkLine(raw, cfg.Origin.Host); err != nil {
			fmt.Printf("  bỏ %q (%s): %v\n", raw, src, err)
			return
		}
		p := normPath(raw)
		if !bs.In(p) {
			outScope++
			return
		}
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	split := func(s string) []string {
		return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' })
	}
	if *sf.one != "" {
		add(*sf.one, "-url")
	}
	if *sf.urls != "" {
		v := *sf.urls
		if v == askValue {
			v = askLine("Nhập URL, cách nhau dấu phẩy: ")
		}
		for _, u := range split(v) {
			add(u, "-urls")
		}
	}
	if *sf.txt != "" {
		f := *sf.txt
		if f == askValue {
			f = defaultListFile(cfg, site)
		}
		n0 := len(paths)
		for _, p := range readPaths(f, 0, cfg.Origin.Host) {
			add(p, f)
		}
		fmt.Printf("đọc %s: %d URL\n", f, len(paths)-n0)
	}
	if *sf.seg != "" {
		v := *sf.seg
		if v == askValue {
			v = askLine("Nhập link archive (category, tag, trang chủ...), cách nhau dấu phẩy: ")
		}
		for _, p := range expandSegment(ctx, cfg, db, split(v), *sf.segMode) {
			add(p, "-segment")
		}
	}
	if outScope > 0 {
		fmt.Printf("  bỏ %d URL không thuộc %s\n", outScope, bs)
	}
	return paths
}

// activePaths: mọi URL active (của một blog nếu có), theo thứ tự ưu tiên.
func activePaths(db *sql.DB, bs *blogScope) []string {
	q := `SELECT path FROM urls WHERE status='active'`
	var args []any
	if bs != nil {
		q += ` AND blog_id = ?`
		args = append(args, bs.ID)
	}
	q += ` ORDER BY CASE WHEN kind='home' THEN 0 WHEN kind='single' THEN 1 WHEN page=1 THEN 2 ELSE 3 END,
		page, blog_id, CASE obj_type WHEN 'category' THEN 0 WHEN 'post_tag' THEN 1 ELSE 2 END, path`
	rows, err := db.Query(q, args...)
	if err != nil {
		die("%v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		rows.Scan(&p)
		out = append(out, p)
	}
	return out
}
