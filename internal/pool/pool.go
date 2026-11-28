// Package pool: hàng đợi việc có ưu tiên của một site, lưu trong SQLite (data/<domain>/pool.db).
// Thiết kế: docs/design-pool.md mục 6.
//
//   - Mỗi path một dòng. Việc mới cho cùng path gộp vào dòng cũ: đổi hành động, giữ ưu tiên cao hơn,
//     tăng gen. Worker chỉ xóa dòng (coi là xong) khi gen không đổi kể từ lúc lấy lô.
//   - inbox: sự kiện thô từ WordPress, lưu ngay khi nhận (trả 202), resolver xử lý sau.
//
// Chỉ một tiến trình (dott-cache serve) mở file này.
package pool

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS pool (
	path     TEXT PRIMARY KEY,
	action   TEXT NOT NULL,             -- refresh | remove
	prio     INTEGER NOT NULL,          -- 0 gỡ, 1 sự kiện bài, 2 sự kiện liên quan, 3 thủ công, 4 hàng loạt
	ord      INTEGER NOT NULL DEFAULT 0,-- thứ tự trong cùng mức
	gen      INTEGER NOT NULL DEFAULT 1,
	state    TEXT NOT NULL DEFAULT 'queued', -- queued | running
	src      TEXT NOT NULL DEFAULT '',
	tries    INTEGER NOT NULL DEFAULT 0,
	next_at  INTEGER NOT NULL DEFAULT 0,
	last_err TEXT NOT NULL DEFAULT '',
	created  INTEGER NOT NULL,
	updated  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS pool_pick ON pool(state, prio, ord, created);
CREATE TABLE IF NOT EXISTS inbox (
	id       INTEGER PRIMARY KEY,
	received INTEGER NOT NULL,
	body     TEXT NOT NULL              -- một sự kiện (JSON)
);
`

// Mức ưu tiên.
const (
	PrioRemove  = 0
	PrioEvent   = 1
	PrioRelated = 2
	PrioManual  = 3
	PrioBulk    = 4
)

type Pool struct{ db *sql.DB }

type Item struct {
	Path, Action, Src string
	Prio, Ord         int
	Gen               int64
	Tries             int
	LastErr           string
}

func Open(dir string) (*Pool, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "pool.db")+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("tạo schema: %w", err)
	}
	// Khởi động lại: việc đang chạy dở trở về hàng đợi (file cache trên VPS chính vẫn còn, làm lại an toàn).
	if _, err := db.Exec(`UPDATE pool SET state='queued' WHERE state='running'`); err != nil {
		db.Close()
		return nil, err
	}
	return &Pool{db: db}, nil
}

func (p *Pool) Close() error { return p.db.Close() }

// Add đưa việc vào pool, gộp theo path. Trả về số dòng mới và số dòng đã gộp.
// Quy tắc gộp: việc mới thay hành động cũ (sự kiện tới sau là trạng thái mới nhất), trừ khi việc mới là
// refresh hàng loạt (prio >= 3) mà dòng cũ đang chờ gỡ: giữ gỡ. Ưu tiên lấy mức cao hơn (số nhỏ hơn);
// gỡ luôn ở mức 0. Mỗi lần gộp tăng gen và xóa bộ đếm lỗi.
func (p *Pool) Add(items []Item) (added, merged int, err error) {
	tx, err := p.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	for _, it := range items {
		if it.Action == "remove" {
			it.Prio = PrioRemove
		}
		var act string
		var prio int
		e := tx.QueryRow(`SELECT action, prio FROM pool WHERE path=?`, it.Path).Scan(&act, &prio)
		switch {
		case e == sql.ErrNoRows:
			if _, err := tx.Exec(`INSERT INTO pool(path, action, prio, ord, src, created, updated) VALUES(?,?,?,?,?,?,?)`,
				it.Path, it.Action, it.Prio, it.Ord, it.Src, now, now); err != nil {
				return 0, 0, err
			}
			added++
		case e != nil:
			return 0, 0, e
		default:
			na, np := it.Action, it.Prio
			if act == "remove" && na == "refresh" && it.Prio >= PrioManual {
				na, np = "remove", PrioRemove
			} else if na == act && prio < np {
				np = prio
			}
			if _, err := tx.Exec(`UPDATE pool SET action=?, prio=?, ord=?, src=?, gen=gen+1, tries=0, next_at=0, last_err='', updated=? WHERE path=?`,
				na, np, it.Ord, it.Src, now, it.Path); err != nil {
				return 0, 0, err
			}
			merged++
		}
	}
	return added, merged, tx.Commit()
}

// Take lấy một lô và đánh dấu running:
//   - còn việc gỡ: tối đa maxRemove việc gỡ;
//   - không thì refresh: nếu việc đầu là sự kiện (prio <= 2) thì lấy tối đa maxEvent việc prio <= 2,
//     ngược lại lấy tối đa maxBulk việc cùng mức với việc đầu.
func (p *Pool) Take(maxRemove, maxEvent, maxBulk int) (action string, items []Item, err error) {
	now := time.Now().Unix()
	var prio int
	e := p.db.QueryRow(`SELECT action, prio FROM pool WHERE state='queued' AND next_at<=? ORDER BY prio, ord, created LIMIT 1`, now).Scan(&action, &prio)
	if e == sql.ErrNoRows {
		return "", nil, nil
	}
	if e != nil {
		return "", nil, e
	}
	q, args := `SELECT path, action, src, prio, ord, gen, tries FROM pool WHERE state='queued' AND next_at<=? AND action=?`, []any{now, action}
	n := maxBulk
	switch {
	case action == "remove":
		n = maxRemove
	case prio <= PrioRelated:
		q += ` AND prio<=?`
		args = append(args, PrioRelated)
		n = maxEvent
	default:
		q += ` AND prio=?`
		args = append(args, prio)
	}
	q += ` ORDER BY prio, ord, created LIMIT ?`
	args = append(args, n)
	rows, err := p.db.Query(q, args...)
	if err != nil {
		return "", nil, err
	}
	for rows.Next() {
		var it Item
		rows.Scan(&it.Path, &it.Action, &it.Src, &it.Prio, &it.Ord, &it.Gen, &it.Tries)
		items = append(items, it)
	}
	rows.Close()
	for _, it := range items {
		if _, err := p.db.Exec(`UPDATE pool SET state='running' WHERE path=? AND gen=?`, it.Path, it.Gen); err != nil {
			return "", nil, err
		}
	}
	return action, items, nil
}

// HeadPrio: mức ưu tiên của việc đầu hàng đợi đã tới giờ (false nếu không có).
func (p *Pool) HeadPrio() (int, bool) {
	var prio int
	if p.db.QueryRow(`SELECT prio FROM pool WHERE state='queued' AND next_at<=? ORDER BY prio, ord, created LIMIT 1`, time.Now().Unix()).Scan(&prio) != nil {
		return 0, false
	}
	return prio, true
}

// Done: việc xong. Xóa dòng nếu gen không đổi; nếu đã đổi (có việc mới trong lúc chạy) thì trả về hàng
// đợi. Trả về true nếu gen không đổi.
func (p *Pool) Done(it Item) (bool, error) {
	r, err := p.db.Exec(`DELETE FROM pool WHERE path=? AND gen=?`, it.Path, it.Gen)
	if err != nil {
		return false, err
	}
	if n, _ := r.RowsAffected(); n == 1 {
		return true, nil
	}
	_, err = p.db.Exec(`UPDATE pool SET state='queued' WHERE path=?`, it.Path)
	return false, err
}

// Current: gen và action hiện tại của path (false nếu không còn trong pool).
func (p *Pool) Current(path string) (int64, string, bool) {
	var g int64
	var a string
	if p.db.QueryRow(`SELECT gen, action FROM pool WHERE path=?`, path).Scan(&g, &a) != nil {
		return 0, "", false
	}
	return g, a, true
}

// Backoff giữa các lần thử lại; quá MaxTries thì bỏ việc.
var Backoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 30 * time.Minute}

const MaxTries = 5

// Fail: việc lỗi. Hẹn thử lại theo Backoff; quá MaxTries thì xóa và trả dropped=true.
// Nếu gen đã đổi thì chỉ trả về hàng đợi (việc mới, không tính lỗi).
func (p *Pool) Fail(it Item, msg string) (dropped bool, err error) {
	g, _, ok := p.Current(it.Path)
	if !ok {
		return false, nil
	}
	if g != it.Gen {
		_, err = p.db.Exec(`UPDATE pool SET state='queued' WHERE path=?`, it.Path)
		return false, err
	}
	t := it.Tries + 1
	if t >= MaxTries {
		_, err = p.db.Exec(`DELETE FROM pool WHERE path=? AND gen=?`, it.Path, it.Gen)
		return true, err
	}
	next := time.Now().Add(Backoff[min(t-1, len(Backoff)-1)]).Unix()
	_, err = p.db.Exec(`UPDATE pool SET state='queued', tries=?, next_at=?, last_err=? WHERE path=? AND gen=?`, t, next, msg, it.Path, it.Gen)
	return false, err
}

// Stats: số việc theo (action, prio, state), số dòng inbox, việc đang chờ thử lại.
type Stats struct {
	Groups   map[string]int `json:"groups"` // "refresh/p1/queued" → n
	Total    int            `json:"total"`
	Inbox    int            `json:"inbox"`
	Retrying []Item         `json:"retrying,omitempty"`
}

func (p *Pool) Stats() (Stats, error) {
	s := Stats{Groups: map[string]int{}}
	rows, err := p.db.Query(`SELECT action, prio, state, COUNT(*) FROM pool GROUP BY 1,2,3`)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var a, st string
		var pr, n int
		rows.Scan(&a, &pr, &st, &n)
		s.Groups[fmt.Sprintf("%s/p%d/%s", a, pr, st)] = n
		s.Total += n
	}
	rows.Close()
	p.db.QueryRow(`SELECT COUNT(*) FROM inbox`).Scan(&s.Inbox)
	r2, err := p.db.Query(`SELECT path, action, prio, tries, last_err FROM pool WHERE tries>0 ORDER BY updated DESC LIMIT 20`)
	if err == nil {
		for r2.Next() {
			var it Item
			r2.Scan(&it.Path, &it.Action, &it.Prio, &it.Tries, &it.LastErr)
			s.Retrying = append(s.Retrying, it)
		}
		r2.Close()
	}
	return s, nil
}

// ---- inbox ----

func (p *Pool) InboxAdd(bodies []string) error {
	tx, err := p.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	for _, b := range bodies {
		if _, err := tx.Exec(`INSERT INTO inbox(received, body) VALUES(?,?)`, now, b); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type InboxRow struct {
	ID       int64
	Received int64
	Body     string
}

func (p *Pool) InboxTake(limit int) ([]InboxRow, error) {
	rows, err := p.db.Query(`SELECT id, received, body FROM inbox ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InboxRow
	for rows.Next() {
		var r InboxRow
		rows.Scan(&r.ID, &r.Received, &r.Body)
		out = append(out, r)
	}
	return out, nil
}

func (p *Pool) InboxDelete(upTo int64) error {
	_, err := p.db.Exec(`DELETE FROM inbox WHERE id<=?`, upTo)
	return err
}
