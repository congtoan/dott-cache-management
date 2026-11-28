# Kế hoạch P8: bật pool (sự kiện) cho production, site vi + en tạm (soạn 28/11 18:05)

> Chưa làm bước nào trên VPS chính. Mọi bước ghi trên VPS chính (đánh dấu **[hỏi]**) cần chủ site duyệt.
> Làm sau khi P6 xong (`systemctl is-active dott-p6-vi` = inactive) và đã kiểm tra P6 (HANDOFF mục 4.1).

## 0. Vì sao cần làm sớm
- **vi:** P5 đã xóa cron cũ; production bật `wp-rocket-no-cache-auto-purge`. Từ khi P6 xong đến khi P8 xong,
  bài vi sửa **không** lên cache (cả origin lẫn S3).
- **en:** chưa từng có cơ chế cập nhật tự động trong hệ này (2 cron cũ chỉ xử lý URL vi; VPS phụ không có timer).
  Đo 28/11: S3 `/en/` sửa lần cuối 24/11, `/en/abkhazia-maps/` 21/11; origin (gọi thẳng IP) trả cùng bản 21/11.

## 1. en tạm: `legacy_blogs` (mã đã viết, nhánh v2)
Blog en vẫn tách mobile (theme không tt-core, WP Rocket `do_caching_mobile_files=1`, Worker `LEGACY_PREFIXES ["/en/"]`).
Thêm vào config: `"legacy_blogs": ["/en/"]` (giữ `write_blogs: ["/"]`).

| | Blog một bản (vi) | Blog kiểu cũ (en) |
|---|---|---|
| Sự kiện của blog được nhận | `write_blogs` | `legacy_blogs` |
| Render | ≤ 4 URL: xóa file + HTTP; > 4: prefork | luôn xóa file (`--delete-only`, xóa cả 5 file kể cả `.mobile-active`) + HTTP |
| Lấy về, upload | UA desktop → `index-https.html` | UA desktop → `index-https.html`, rồi UA iPhone → `index-mobile-https.html` |
| Điều tiết load | lô > 4 URL | lô > 4 URL |
| Gỡ (bài xóa, đổi slug) | KV → đĩa → mọi object S3 của path | như vi (LIST thư mục nên xóa cả bản mobile) |
| `submit -url` | được | được |
| `submit -all` (mức 4) | được | **bị chặn** (mỗi URL 2 lần render HTTP, 41k URL) |
| CLI `update/regen/sync/replace -blog /en/` | | **bị chặn** (`checkWrite`): các lệnh này xóa bản mobile |

- Một lô của pool có thể lẫn vi và en: `doRefresh` tách ra, mỗi phần một lượt.
- Đo trước (chỉ đọc, 28/11): gọi thẳng origin `/en/abkhazia-maps/` bằng UA desktop và iPhone: 2 bản khác md5,
  cùng có `cached@`, nginx `BYPASS` (do `.mobile-active`, PHP trả file): cách lấy 2 bản bằng HTTP dùng được.
- Test: `go vet`, `go test ./cmd/dott-cache/ ./internal/pool/` đạt (test mới `serve_legacy_test.go`: phân loại path
  vi/en/es, không có map blog thì chặn). Build ở `/tmp/dott-cache-legacy` trên VPS phụ, **chưa cài vào `bin/`**.
- **Giới hạn:** chỉ bài/term/bình luận **có sự kiện** mới được làm mới. Trang en cũ mà không ai sửa vẫn cũ tới lần 2
  (chuyển en). Không cài reconcile hằng ngày cho en (chỉ chạy `-dry-run` khi cần xem).
- Lần 2 (chuyển en): bỏ `legacy_blogs`, thêm `"/en/"` vào `write_blogs`.

## 2. Các bước

### A. VPS phụ (không đụng production)
1. Token: `openssl rand -hex 32 > config/secrets/hook-diaocthongthai.com.token` (600).
2. Cài binary mới: backup `bin/dott-cache` → `bin/dott-cache.bak-<ngày>`, `install /tmp/dott-cache-legacy bin/dott-cache`
   (sau khi P6 xong; chép `src/` mới từ repo).
3. Config `config/sites/diaocthongthai.com.json`: backup, thêm `"legacy_blogs": ["/en/"]`.
4. Unit serve: `-sites php81.congtoan.net,diaocthongthai.com`; cài unit `dott-cache-reconcile@`, `dott-cache-submit-all@`
   (từ `deploy/systemd/`).
5. `systemctl restart dott-cache-serve`; log phải có `site diaocthongthai.com: ... legacy_blogs [/en/]` và
   `blog map[1:/ 2:/en/ 3:/es/]`. Từ lúc này `serve` giữ khóa site production: CLI ghi phải qua `submit`.

### B. VPS chính **[hỏi]**
6. Cài `wp-content/mu-plugins/dott-cache-events.php` (owner `diaocthDP4f`, 644, `php -l`). Chưa có file hook thì
   plugin không làm gì. Đợi 3 phút (OPcache).
7. `systemctl enable --now dott-cache-tunnel dott-cache-serve` (VPS phụ; tunnel đang chạy sẵn, chỉ thêm enable).
   Kiểm tra trên VPS chính: `curl -s http://127.0.0.1:8790/health` = `ok`.
8. **Bật:** tạo `/home/diaocthDP4f/diaocthongthai.com/dott-cache-hook.json`
   `{"url":"http://127.0.0.1:8790/event","token":"<token bước 1>"}`, owner `diaocthDP4f`, 600.
   (Ngoài `public_html`; plugin đọc `dirname(ABSPATH)`.)

### C. Test (chủ site sửa bài trong admin, Claude xem log)
9. **vi:** sửa 1 bài (thêm 1 chữ rồi lưu). Mong đợi trong `journalctl -u dott-cache-serve`: nhận sự kiện →
   `làm mới N` (bài + trang 1 các term + trang chủ) → `lô ... (http)` ok. Kiểm tra: S3 `Last-Modified` mới,
   nginx HIT có nội dung mới, desktop và iPhone cùng bản.
10. **en:** sửa 1 bài en. Mong đợi: `lô ... (http, kiểu cũ desktop+mobile)` ok; S3 có cả `index-https.html` và
    `index-mobile-https.html` mới, khác md5; Worker (giả lập NA bằng `X-Dott-Test-Key`) trả `S3-NA-legacy` /
    `S3-NA-legacy-mobile` có nội dung mới.
11. Bình luận được duyệt (vi), đổi slug 1 bài nháp test rồi đổi lại (nếu chủ site đồng ý), gỡ 1 bài test.
12. Tunnel tắt (`systemctl stop dott-cache-tunnel`), sửa bài: editor không bị chậm, `error_log` có
    `dott-cache-events: POST lỗi`. Bật lại tunnel.

### D. Lịch
13. `systemctl enable --now dott-cache-reconcile@vi.timer` (03:30) và `dott-cache-submit-all@vi.timer`
    (ngày 1, 04:00). Trước đó chạy tay `reconcile -site diaocthongthai.com -blog / -dry-run`.
14. Ghi rollout-log mục P8, HANDOFF, commit, push.

## 3. Quay lại
- Tắt nhanh sự kiện: xóa `dott-cache-hook.json` trên VPS chính (plugin thôi gửi).
- Bỏ hẳn: xóa `mu-plugins/dott-cache-events.php`; `systemctl disable --now dott-cache-serve dott-cache-tunnel` và các timer.
- Binary cũ: `bin/dott-cache.bak-<ngày>`; config: bản `.bak`.
- en: bỏ `legacy_blogs` là serve lại bỏ qua sự kiện en như trước.

## 4. Còn mở (không chặn P8)
- `wp-rocket-no-cache-auto-purge`: sửa menu/widget/thiết lập theme không sinh sự kiện, chờ `submit -all` hằng tháng
  (hoặc `submit -url` tay).
- wpdatatables ghi `tempfile*.csv` vào thư mục hiện hành khi prefork (đã gặp ở P6): HTTP thử lại được, chưa sửa.
