# Kế hoạch P8: bật pool (sự kiện) cho production, cả 2 blog vi và en (soạn 28/11)

> **ĐÃ THỰC HIỆN 29/11 01:43–01:52.** Kết quả và chỗ khác kế hoạch: rollout-log mục "P8 production". Khác kế hoạch:
> - test bằng cách gọi thẳng hàm của plugin (không bắn cả hook WP Rocket);
> - khi tunnel tắt **không có** dòng `error_log` (do `blocking=false`);
> - cài thêm bản repo của `dott-cache-blog.sh` (bản trên VPS chưa có `reconcile`/`submit-all`);
> - `submit-all@en` dời sang ngày 15 bằng drop-in.

> Làm sau khi cả vi (P6) và en (lần 2: theme tt-core, một bản cache, Worker bỏ `LEGACY_PREFIXES`, `update -all -blog /en/`)
> đã chuyển xong. Mọi bước ghi trên VPS chính (đánh dấu **[hỏi]**) cần chủ site duyệt.
>
> Phương án tạm `legacy_blogs` cho en (soạn trước đó cùng ngày) **đã bỏ, không deploy**: en chuyển hẳn sang một bản HTML
> trong ngày 28/11. Phương án đó còn lỗi: `--delete-only` xóa `.mobile-active`, rồi lấy bản desktop trước, nên request
> iPhone sau đó có thể bị nginx trả bản desktop.

## 0. Vì sao cần làm sớm
- P5 đã xóa cron cũ; production bật `wp-rocket-no-cache-auto-purge`. Chừng nào chưa có P8, bài sửa **không** lên
  cache (cả origin lẫn S3), ở cả vi lẫn en.
- en chưa từng có cơ chế cập nhật tự động trong hệ này (2 cron cũ chỉ xử lý URL vi). Đo 28/11: S3 `/en/` sửa lần cuối
  24/11, `/en/abkhazia-maps/` 21/11.

## 1. Cấu hình
- `write_blogs: ["/", "/en/"]` (đặt ở lần 2 trước `update -all -blog /en/`). `serve` nhận sự kiện của cả 2 blog;
  `/es/` bị bỏ (`skip_blogs` và ngoài `write_blogs`).

## 2. Các bước

### A. VPS phụ (không đụng production)
1. Token: `openssl rand -hex 32 > config/secrets/hook-diaocthongthai.com.token` (600).
2. Unit serve: `-sites php81.congtoan.net,diaocthongthai.com`; cài unit `dott-cache-reconcile@`, `dott-cache-submit-all@`
   (từ `deploy/systemd/`).
3. `systemctl restart dott-cache-serve`; log phải có `site diaocthongthai.com: ... write_blogs [/ /en/]` và
   `blog map[1:/ 2:/en/ 3:/es/]`. Từ lúc này `serve` giữ khóa site production: CLI ghi phải qua `submit`.

### B. VPS chính **[hỏi]**
4. Cài `wp-content/mu-plugins/dott-cache-events.php` (owner `diaocthDP4f`, 644, `php -l`). Chưa có file hook thì
   plugin không làm gì. Đợi 3 phút (OPcache).
5. `systemctl enable --now dott-cache-tunnel dott-cache-serve` (VPS phụ; tunnel đang chạy sẵn, chỉ thêm enable).
   Kiểm tra trên VPS chính: `curl -s http://127.0.0.1:8790/health` = `ok`.
6. **Bật:** tạo `/home/diaocthDP4f/diaocthongthai.com/dott-cache-hook.json`
   `{"url":"http://127.0.0.1:8790/event","token":"<token bước 1>"}`, owner `diaocthDP4f`, 600.
   (Ngoài `public_html`; plugin đọc `dirname(ABSPATH)`.)

### C. Test (chủ site sửa bài trong admin, Claude xem log)
7. **vi** rồi **en:** sửa 1 bài (thêm 1 chữ rồi lưu). Mong đợi trong `journalctl -u dott-cache-serve`: nhận sự kiện →
   `làm mới N` (bài + trang 1 các term + trang chủ) → `lô ... (http)` ok. Kiểm tra: S3 `Last-Modified` mới,
   nginx HIT có nội dung mới, desktop và iPhone cùng bản; Worker (giả lập NA bằng `X-Dott-Test-Key`) trả `S3-NA` bản mới.
8. Bình luận được duyệt, đổi slug 1 bài nháp test rồi đổi lại (nếu chủ site đồng ý), gỡ 1 bài test.
9. Tunnel tắt (`systemctl stop dott-cache-tunnel`), sửa bài: editor không bị chậm, `error_log` có
   `dott-cache-events: POST lỗi`. Bật lại tunnel.

### D. Lịch
10. Chạy tay `reconcile -site diaocthongthai.com -blog / -dry-run` và `-blog /en/ -dry-run`, rồi
    `systemctl enable --now dott-cache-reconcile@vi.timer dott-cache-reconcile@en.timer` (03:30) và
    `dott-cache-submit-all@vi.timer dott-cache-submit-all@en.timer` (ngày 1, 04:00; en nên lệch ngày, vd sửa timer en
    thành ngày 15, để 2 lượt toàn site không chồng nhau).
11. Ghi rollout-log mục P8, HANDOFF, commit, push.

## 3. Quay lại
- Tắt nhanh sự kiện: xóa `dott-cache-hook.json` trên VPS chính (plugin thôi gửi).
- Bỏ hẳn: xóa `mu-plugins/dott-cache-events.php`; `systemctl disable --now dott-cache-serve dott-cache-tunnel` và các timer.

## 4. Còn mở (không chặn P8)
- `wp-rocket-no-cache-auto-purge`: sửa menu/widget/thiết lập theme không sinh sự kiện, chờ `submit -all` hằng tháng
  (hoặc `submit -url` tay).
- wpdatatables ghi `tempfile*.csv` vào thư mục hiện hành khi prefork (đã gặp ở P6): HTTP thử lại được, chưa sửa.
