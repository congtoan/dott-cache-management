// Package kv: Cloudflare Workers KV qua REST API (chỉ thư viện chuẩn, không cần wrangler/Node).
//
// Bài học từ test 25/11 (docs/cloudflare.md):
//   - bulk write/delete tối đa 10.000 key một request;
//   - bulk delete có thể trả 200 mà vẫn sót key, nên sau khi xóa phải list lại để kiểm tra;
//   - đọc ra có độ trễ khoảng 60 s (list cập nhật nhanh hơn get).
package kv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const api = "https://api.cloudflare.com/client/v4"

// Client cho một namespace.
type Client struct {
	Token, Account, Namespace string
	http                      *http.Client
}

func New(token, account, namespace string) *Client {
	return &Client{Token: token, Account: account, Namespace: namespace, http: &http.Client{Timeout: 60 * time.Second}}
}

type envelope struct {
	Success  bool            `json:"success"`
	Errors   []apiErr        `json:"errors"`
	Result   json.RawMessage `json:"result"`
	ResultIn struct {
		Cursor string `json:"cursor"`
		Count  int    `json:"count"`
	} `json:"result_info"`
}
type apiErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *Client) do(ctx context.Context, method, path string, body any) (*envelope, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	var last error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*attempt) * time.Second)
			if rd != nil {
				rd.(*bytes.Reader).Seek(0, io.SeekStart)
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, api+path, rd)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			last = err
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			last = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}
		var e envelope
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("HTTP %d, trả về không phải JSON: %.200s", resp.StatusCode, raw)
		}
		if !e.Success {
			return &e, fmt.Errorf("HTTP %d: %v", resp.StatusCode, e.Errors)
		}
		return &e, nil
	}
	return nil, last
}

func (c *Client) ns() string {
	return "/accounts/" + c.Account + "/storage/kv/namespaces/" + c.Namespace
}

// CreateNamespace tạo namespace mới, trả về id.
func (c *Client) CreateNamespace(ctx context.Context, title string) (string, error) {
	e, err := c.do(ctx, "POST", "/accounts/"+c.Account+"/storage/kv/namespaces", map[string]string{"title": title})
	if err != nil {
		return "", err
	}
	var r struct {
		ID string `json:"id"`
	}
	json.Unmarshal(e.Result, &r)
	return r.ID, nil
}

// ListKeys trả về mọi key (có thể lọc theo prefix), 1.000 key mỗi trang.
func (c *Client) ListKeys(ctx context.Context, prefix string) ([]string, error) {
	var out []string
	cursor := ""
	for {
		q := url.Values{"limit": {"1000"}}
		if prefix != "" {
			q.Set("prefix", prefix)
		}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		e, err := c.do(ctx, "GET", c.ns()+"/keys?"+q.Encode(), nil)
		if err != nil {
			return out, err
		}
		var ks []struct {
			Name string `json:"name"`
		}
		json.Unmarshal(e.Result, &ks)
		for _, k := range ks {
			out = append(out, k.Name)
		}
		cursor = e.ResultIn.Cursor
		if cursor == "" || len(ks) == 0 {
			return out, nil
		}
	}
}

type bulkResult struct {
	SuccessfulKeyCount int      `json:"successful_key_count"`
	UnsuccessfulKeys   []string `json:"unsuccessful_keys"`
}

// PutBulk ghi key với cùng một giá trị, mỗi request tối đa 10.000 key. Trả về các key ghi lỗi.
func (c *Client) PutBulk(ctx context.Context, keys []string, value string) ([]string, error) {
	var failed []string
	for _, ch := range chunks(keys, 10000) {
		items := make([]map[string]string, len(ch))
		for i, k := range ch {
			items[i] = map[string]string{"key": k, "value": value}
		}
		e, err := c.do(ctx, "PUT", c.ns()+"/bulk", items)
		if err != nil {
			return append(failed, ch...), err
		}
		var r bulkResult
		json.Unmarshal(e.Result, &r)
		failed = append(failed, r.UnsuccessfulKeys...)
	}
	return failed, nil
}

// DeleteBulk xóa key, mỗi request tối đa 10.000 key. Trả về các key API báo xóa lỗi.
// Lưu ý: kể cả khi không có key nào bị báo lỗi, vẫn phải list lại để kiểm tra (F17).
func (c *Client) DeleteBulk(ctx context.Context, keys []string) ([]string, error) {
	var failed []string
	for _, ch := range chunks(keys, 10000) {
		e, err := c.do(ctx, "POST", c.ns()+"/bulk/delete", ch)
		if err != nil {
			return append(failed, ch...), err
		}
		var r bulkResult
		json.Unmarshal(e.Result, &r)
		failed = append(failed, r.UnsuccessfulKeys...)
	}
	return failed, nil
}

func chunks(s []string, n int) [][]string {
	var out [][]string
	for len(s) > n {
		out = append(out, s[:n])
		s = s[n:]
	}
	if len(s) > 0 {
		out = append(out, s)
	}
	return out
}

// Key: path → key KV, GIỐNG normalizePath() của Worker thongthai-cache-v7.js (và kv_key() của u2 v3.4):
// viết thường, thêm "/" cuối nếu path không có dấu "." (không phải file).
func Key(path string) string {
	if path == "" || path == "/" {
		return "/"
	}
	p := strings.ToLower(path)
	if !strings.Contains(p, ".") && !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}
