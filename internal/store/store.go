// Package store: trạng thái của một site trong SQLite (data/<domain>/state.db).
//
// Chỉ một tiến trình ghi mỗi lúc (lệnh CLI hoặc service), chế độ WAL nên lệnh đọc chạy song song được.
// Thư viện modernc.org/sqlite: Go thuần, không cần cgo.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Schema hiện tại. Các bảng cho cache/S3/KV/hàng đợi sẽ thêm ở các bước sau (design mục 8).
const schema = `
CREATE TABLE IF NOT EXISTS urls (
	path       TEXT PRIMARY KEY,           -- path như WordPress in ra, có "/" cuối (vd /en/map-of-x/)
	blog_id    INTEGER NOT NULL,
	kind       TEXT NOT NULL,              -- single | term | pt_archive | home
	obj_type   TEXT NOT NULL DEFAULT '',   -- post type hoặc taxonomy
	obj_id     INTEGER NOT NULL DEFAULT 0, -- ID bài hoặc term_id
	page       INTEGER NOT NULL DEFAULT 1, -- trang phân trang của archive
	modified   INTEGER,                    -- post_modified_gmt (unix) của bài đơn
	status     TEXT NOT NULL DEFAULT 'active', -- active | removed
	first_seen INTEGER NOT NULL,
	last_seen  INTEGER NOT NULL,
	removed_at INTEGER
);
CREATE INDEX IF NOT EXISTS urls_status ON urls(status);
CREATE INDEX IF NOT EXISTS urls_obj ON urls(blog_id, kind, obj_id);

CREATE TABLE IF NOT EXISTS runs (
	id        INTEGER PRIMARY KEY,
	kind      TEXT NOT NULL,    -- urls-full | urls-since
	started   INTEGER NOT NULL,
	finished  INTEGER,
	seen      INTEGER, added INTEGER, changed INTEGER, removed INTEGER, excluded INTEGER,
	note      TEXT
);

CREATE TABLE IF NOT EXISTS meta (k TEXT PRIMARY KEY, v TEXT);
`

// Open mở (tạo nếu chưa có) data/<site>/state.db.
func Open(dir string) (*sql.DB, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, "state.db")
	db, err := sql.Open("sqlite", "file:"+p+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // một kết nối ghi: không có tranh chấp khóa
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("tạo schema: %w", err)
	}
	return db, nil
}

// Meta đọc giá trị trong bảng meta ("" nếu chưa có).
func Meta(db *sql.DB, k string) string {
	var v string
	_ = db.QueryRow(`SELECT v FROM meta WHERE k = ?`, k).Scan(&v)
	return v
}

// SetMeta ghi giá trị vào bảng meta.
func SetMeta(tx interface {
	Exec(string, ...any) (sql.Result, error)
}, k, v string) error {
	_, err := tx.Exec(`INSERT INTO meta(k, v) VALUES(?, ?) ON CONFLICT(k) DO UPDATE SET v = excluded.v`, k, v)
	return err
}
