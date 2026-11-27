// Package origin lấy HTML đã cache từ origin (VPS chính), gọi thẳng IP, không qua Cloudflare.
package origin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

// Config của origin.
type Config struct {
	Host      string `json:"host"`       // diaocthongthai.com | php81.congtoan.net
	IP        string `json:"ip"`         // 45.77.32.177 (gọi thẳng, không qua Cloudflare)
	SPKIPin   string `json:"spki_pin"`   // base64 sha256 public key; rỗng = kiểm tra CA thông thường
	UserAgent string `json:"user_agent"` // UA desktop
	Workers   int    `json:"workers"`    // số request đồng thời tới origin

	// MinCachedAt (unix, không đọc từ JSON): nếu > 0 thì bản cache phải có cached@ >= giá trị này.
	// renew đặt bằng thời điểm bắt đầu: nginx bật open_file_cache nên sau khi file bị xóa/thay,
	// nginx có thể vẫn trả bản CŨ tới 120 s (đo 28/11). Không kiểm thì sẽ upload bản cũ lên S3.
	MinCachedAt int64 `json:"-"`
}

// Fetcher giữ một http.Client keep-alive tới origin.
type Fetcher struct {
	cfg  Config
	http *http.Client
}

var cachedRe = regexp.MustCompile(`cached@(\d+)`)

func New(cfg Config) *Fetcher {
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0 Safari/537.36 dott-cache"
	}
	tlsCfg := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}
	if cfg.SPKIPin != "" {
		// Cloudflare Origin CA không có trong kho CA hệ thống: kiểm tra bằng pin public key.
		pin := cfg.SPKIPin
		tlsCfg.InsecureSkipVerify = true // thay bằng VerifyPeerCertificate bên dưới, KHÔNG bỏ kiểm tra
		tlsCfg.VerifyPeerCertificate = func(raw [][]byte, _ [][]*x509.Certificate) error {
			for _, r := range raw {
				c, err := x509.ParseCertificate(r)
				if err != nil {
					continue
				}
				s := sha256.Sum256(c.RawSubjectPublicKeyInfo)
				if base64.StdEncoding.EncodeToString(s[:]) == pin {
					return nil
				}
			}
			return errors.New("origin pin mismatch")
		}
	}
	addr := net.JoinHostPort(cfg.IP, "443")
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	tr := &http.Transport{
		Proxy:               nil,
		DialContext:         func(ctx context.Context, n, _ string) (net.Conn, error) { return d.DialContext(ctx, n, addr) },
		TLSClientConfig:     tlsCfg,
		DisableCompression:  true, // không gửi Accept-Encoding: nhận nguyên file .html
		MaxIdleConnsPerHost: cfg.Workers * 2,
		MaxConnsPerHost:     cfg.Workers * 2,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   false,
	}
	return &Fetcher{cfg: cfg, http: &http.Client{Transport: tr, Timeout: 60 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// Doc là một trang HTML đã cache.
type Doc struct {
	Path     string
	Body     []byte
	CachedAt int64         // từ comment "cached@<ts>"
	Served   string        // X-Rocket-Nginx-Serving-Static: HIT | BYPASS | MISS
	Rendered bool          // lần GET đầu chưa có cache (PHP render), đã GET lại
	Duration time.Duration // tổng thời gian lấy (gồm render nếu có)
}

// Get lấy HTML đã cache của path. Nếu lần đầu là PHP render (chưa có "cached@") thì GET lại
// một lần nữa để nhận đúng file WP Rocket đã ghi. Response render mới cũng có Last-Modified
// (đo trên php81), nên dấu hiệu duy nhất đáng tin là comment "cached@".
func (f *Fetcher) Get(ctx context.Context, path string) (*Doc, error) {
	start := time.Now()
	d := &Doc{Path: path}
	var stale int64
	for attempt := 0; attempt < 3; attempt++ {
		body, served, err := f.get(ctx, path)
		if err != nil {
			return nil, err
		}
		if m := cachedRe.FindSubmatch(tailOf(body, 300)); m != nil {
			at, _ := strconv.ParseInt(string(m[1]), 10, 64)
			if f.cfg.MinCachedAt > 0 && at < f.cfg.MinCachedAt {
				stale = at // bản cũ: thử lại, hết lượt thì báo lỗi (không upload)
				time.Sleep(time.Duration(500*(attempt+1)) * time.Millisecond)
				continue
			}
			d.Body, d.Served, d.CachedAt = body, served, at
			d.Duration = time.Since(start)
			if err := validate(body); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			return d, nil
		}
		d.Rendered = true
		time.Sleep(time.Duration(200*(attempt+1)) * time.Millisecond)
	}
	if stale > 0 {
		return nil, fmt.Errorf("%s: origin vẫn trả bản cache cũ (cached@%d, cần >= %d), có thể do nginx open_file_cache", path, stale, f.cfg.MinCachedAt)
	}
	return nil, fmt.Errorf("%s: sau 3 lần vẫn chưa có bản cache (không thấy cached@)", path)
}

func (f *Fetcher) get(ctx context.Context, path string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+f.cfg.Host+path, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", f.cfg.UserAgent)
	resp, err := f.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
	}
	if ce := resp.Header.Get("Content-Encoding"); ce != "" {
		return nil, "", fmt.Errorf("%s: có Content-Encoding %q (cần bản .html thô)", path, ce)
	}
	return body, resp.Header.Get("X-Rocket-Nginx-Serving-Static"), nil
}

func validate(b []byte) error {
	if len(b) < 2048 {
		return fmt.Errorf("HTML quá nhỏ (%d byte)", len(b))
	}
	if !bytes.Contains(tailOf(b, 400), []byte("</html>")) {
		return errors.New("thiếu </html>")
	}
	// Trang phải bắt đầu bằng <!DOCTYPE. Nếu PHP in Notice/Warning trước nó (đã gặp trên php81
	// 27/11, trước khi đặt WP_DEBUG_DISPLAY = false), trình duyệt rơi vào quirks mode và thẻ
	// <meta charset> bị đẩy quá 1024 byte đầu, nên tiếng Việt hiện sai font chữ.
	head := bytes.TrimLeft(b[:min(len(b), 4096)], " \t\r\n\xef\xbb\xbf")
	if !bytes.HasPrefix(bytes.ToLower(head[:min(len(head), 9)]), []byte("<!doctype")) {
		return errors.New("HTML không bắt đầu bằng <!DOCTYPE (có output lạ trước, ví dụ PHP Notice)")
	}
	for _, s := range []string{"<b>Notice</b>:", "<b>Warning</b>:", "<b>Deprecated</b>:", "<b>Fatal error</b>:"} {
		if bytes.Contains(head, []byte(s)) {
			return fmt.Errorf("HTML có lỗi PHP %q", s)
		}
	}
	return nil
}

func tailOf(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[len(b)-n:]
}
