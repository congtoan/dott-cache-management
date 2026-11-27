// Package s3 là client S3 tối giản (chỉ PUT/HEAD/DELETE object) tự ký AWS SigV4,
// chỉ dùng thư viện chuẩn. Dùng cho Linode Object Storage (path-style URL).
//
// Vì sao không dùng aws-sdk-go-v2: chỉ cần 3 thao tác, tránh hàng chục module phụ thuộc,
// binary nhỏ, và kiểm soát được hành vi mạng:
//   - Giữ kết nối keep-alive, mỗi đích một Transport riêng (MaxIdleConnsPerHost = số worker).
//   - KHÔNG gửi "Expect: 100-continue" (boto3 có gửi, làm mỗi PUT tốn thêm 1 RTT, đo 27/11).
//   - Gửi Content-MD5 và so ETag trả về với MD5 nội dung (S3 PUT một phần: ETag = md5 hex).
package s3

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Target là một đích S3 (một bucket ở một region).
type Target struct {
	ID       string `json:"id"`       // tên ngắn, ví dụ "ord", "fra"
	Endpoint string `json:"endpoint"` // https://us-ord-1.linodeobjects.com
	Region   string `json:"region"`   // us-ord-1 (Linode dùng tên cluster làm region khi ký)
	Bucket   string `json:"bucket"`
	Prefix   string `json:"prefix"`  // ví dụ "_cache_html/"
	Workers  int    `json:"workers"` // số PUT song song tới đích này
}

// Client gửi request đã ký tới một Target.
type Client struct {
	T         Target
	accessKey string
	secretKey string
	http      *http.Client
	host      string
	scheme    string
}

func New(t Target, accessKey, secretKey string) (*Client, error) {
	u, err := url.Parse(t.Endpoint)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("endpoint không hợp lệ %q", t.Endpoint)
	}
	if t.Workers <= 0 {
		t.Workers = 8
	}
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:          t.Workers * 2,
		MaxIdleConnsPerHost:   t.Workers * 2,
		MaxConnsPerHost:       t.Workers * 2,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ForceAttemptHTTP2:     false, // HTTP/1.1, nhiều kết nối song song: dễ đo, dễ đoán
	}
	return &Client{
		T: t, accessKey: accessKey, secretKey: secretKey,
		http: &http.Client{Transport: tr, Timeout: 60 * time.Second},
		host: u.Host, scheme: u.Scheme,
	}, nil
}

// PutResult là kết quả một lần PUT.
type PutResult struct {
	Status   int
	ETag     string
	Duration time.Duration
}

// Put ghi object. headers thêm (Content-Type, Cache-Control, x-amz-acl...) đều được ký.
// Trả lỗi nếu HTTP khác 200 hoặc ETag khác MD5 nội dung.
func (c *Client) Put(ctx context.Context, key string, body []byte, headers map[string]string) (PutResult, error) {
	sum := md5.Sum(body)
	h := map[string]string{"content-md5": base64.StdEncoding.EncodeToString(sum[:])}
	for k, v := range headers {
		h[strings.ToLower(k)] = v
	}
	start := time.Now()
	resp, err := c.do(ctx, http.MethodPut, key, body, h)
	r := PutResult{Duration: time.Since(start)}
	if err != nil {
		return r, err
	}
	defer resp.Body.Close()
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	r.Status = resp.StatusCode
	r.ETag = strings.Trim(resp.Header.Get("ETag"), `"`)
	if resp.StatusCode != http.StatusOK {
		return r, fmt.Errorf("PUT %s: HTTP %d %s", key, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if want := hex.EncodeToString(sum[:]); r.ETag != want {
		return r, fmt.Errorf("PUT %s: ETag %s khác md5 %s", key, r.ETag, want)
	}
	return r, nil
}

// Delete xóa object (dùng để dọn object test).
func (c *Client) Delete(ctx context.Context, key string) error {
	resp, err := c.do(ctx, http.MethodDelete, key, nil, map[string]string{})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("DELETE %s: HTTP %d", key, resp.StatusCode)
	}
	return nil
}

// Head trả HTTP status và ETag của object (404 nếu không có).
func (c *Client) Head(ctx context.Context, key string) (int, string, error) {
	resp, err := c.do(ctx, http.MethodHead, key, nil, map[string]string{})
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, strings.Trim(resp.Header.Get("ETag"), `"`), nil
}

// ---------------------------------------------------------------------------------------
// AWS Signature Version 4

func (c *Client) do(ctx context.Context, method, key string, body []byte, h map[string]string) (*http.Response, error) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	payloadHash := sha256Hex(body)

	h["host"] = c.host
	h["x-amz-date"] = amzDate
	h["x-amz-content-sha256"] = payloadHash

	canonURI := "/" + c.T.Bucket + "/" + encodePath(key)

	names := make([]string, 0, len(h))
	for k := range h {
		names = append(names, k)
	}
	sort.Strings(names)
	var ch strings.Builder
	for _, k := range names {
		ch.WriteString(k + ":" + strings.TrimSpace(h[k]) + "\n")
	}
	signed := strings.Join(names, ";")
	canonReq := strings.Join([]string{method, canonURI, "", ch.String(), signed, payloadHash}, "\n")

	scope := date + "/" + c.T.Region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(canonReq))
	k := hmacSHA256([]byte("AWS4"+c.secretKey), date)
	k = hmacSHA256(k, c.T.Region)
	k = hmacSHA256(k, "s3")
	k = hmacSHA256(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(k, toSign))
	auth := "AWS4-HMAC-SHA256 Credential=" + c.accessKey + "/" + scope + ", SignedHeaders=" + signed + ", Signature=" + sig

	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.scheme+"://"+c.host+canonURI, rd)
	if err != nil {
		return nil, err
	}
	for _, k := range names {
		if k == "host" {
			continue
		}
		req.Header.Set(k, h[k])
	}
	req.Header.Set("Authorization", auth)
	if body != nil {
		req.ContentLength = int64(len(body))
	}
	return c.http.Do(req)
}

// encodePath mã hóa từng đoạn của key theo RFC 3986 (giữ "/"), đúng yêu cầu ký của S3.
func encodePath(key string) string {
	parts := strings.Split(key, "/")
	for i, p := range parts {
		var b strings.Builder
		for _, c := range []byte(p) {
			if ('A' <= c && c <= 'Z') || ('a' <= c && c <= 'z') || ('0' <= c && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~' {
				b.WriteByte(c)
			} else {
				fmt.Fprintf(&b, "%%%02X", c)
			}
		}
		parts[i] = b.String()
	}
	return strings.Join(parts, "/")
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}
