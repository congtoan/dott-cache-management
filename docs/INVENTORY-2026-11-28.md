# Kiểm kê Cache System v2 trước khi lên production (28/11/2026)

> Tag `v2-tools-2026-11-28` (commit `a31fd57`). Mọi thứ dưới đây đã test trên **php81.congtoan.net** với bucket test `congtoanphp` và KV test. **Production (diaocthongthai.com) chưa chuyển gì**, ngoài rocket-nginx và `open_file_cache off`.
> Chi tiết từng bước: [rollout-log.md](rollout-log.md). Thiết kế: [design-server-only.md](design-server-only.md) (mục 11b là bố cục thư mục).

## 1. Đã có

### Lệnh `dott-cache` (VPS phụ, `/root/tool/cache-system/bin/dott-cache`)

| Lệnh | Làm gì | Đã test |
|---|---|---|
| `urls sync [-since auto]` | Lấy toàn bộ URL (hoặc phần thay đổi) qua `export-urls.php` và ghi vào SQLite. Chặn khi lần lấy toàn bộ làm mất > 2% URL | php81 (1.489 URL / 3 s), production chỉ đọc (65.657 URL / 18 s); so với `cache_urls.db` cũ: 60.678/60.687 URL trùng |
| `urls list` / `urls stats` | Lọc URL theo loại, post type, taxonomy, blog, regex, số trang, ngày sửa, rồi xuất txt; thống kê | php81 |
| `update -all` | Kiểm kê (manifest S3), **xóa mồ côi trước** (KV → server → S3), xóa file kiểu cũ (mobile, `_gzip` trên S3), prefork theo thứ tự ưu tiên, upload song song, thêm key KV thiếu, kiểm tra lại. Có 2 lớp chặn (DB cũ quá 26 giờ, mồ côi > 2%) | php81: 1.448 URL, 142–276 s |
| `update -url / -urls / -txt / -segment` | Như trên cho một danh sách; tối đa 4 URL thì dùng HTTP, nhiều hơn thì dùng prefork | php81 |
| `replace -content / -version` | Sửa file cache ngay trên đĩa (`replace-cache.php`), tạo lại `_gzip`, kiểm tra nội dung rồi upload lại S3 | php81 |
| `-blog` | Lọc theo subsite cho `update` và `replace`, kể cả `-all` | php81 |
| `kv create/list/sync` | Cloudflare KV qua REST, list lại sau khi ghi để kiểm tra | KV test |
| `upload`, `delete`, `renew` | Lệnh cũ, vẫn chạy (`renew` nên thay bằng `update`) | |

Điều tiết theo load: `max_load` 1,5 (chủ site chốt). Go kiểm tra trước mỗi lô, PHP kiểm tra trong lô.

### Trên VPS chính
- `wp-content/mu-plugins/dott-cache/`: `prefork-render.php`, `export-urls.php`, `replace-cache.php`. Mới cài trên **php81**.
- `/usr/local/sbin/dott-prefork`: bộ chuyển lệnh của root. Gồm danh sách domain, khóa chung, `nice`/`ionice`; chạy PHP của site bằng user của site. **Mới bật php81.**
- `wp/mu-plugins/dott-single-cache.php`: một bản cache cho mọi thiết bị. Mới cài trên **php81**.
- nginx: rocket-nginx và `open_file_cache off` đã bật cho cả diaocthongthai.com và php81.

### Khác
- Theme tt-core (`theme/`), plugin sticky 3.2.0 (chủ site đã làm): đã test trên php81.
- Worker v2 (`deploy/worker/`): chạy trên `workers.dev` với KV test; giả lập châu lục bằng header bí mật.

## 2. So với tool cũ

| Tool cũ | Thay bằng | Ghi chú |
|---|---|---|
| `wp_cli_extract_all_urls.py`, `u1`, `u2`, `DB_init_and_import.py` | `urls sync` | Một lần nạp WordPress mỗi blog thay cho khoảng 60 lệnh `wp` |
| `KV_export/import`, `fix_f4_kv_keys.py` | `kv sync`, bước KV của `update` | |
| `cache_create_from_db.py`, `cache_renew_from_db.py` (vòng lặp vô hạn), `m4` | `update -all` chạy theo lịch hằng tháng | Lịch **chưa cài** |
| `cache-urls-update.py` | `update -txt / -urls / -url` | |
| `cache-segment-update.py` | `update -segment` | |
| `cache_clean_from_db.py` | bước mồ côi của `update -all` | |
| `m0` (manifest S3), `m1`, `m2`, `m3` | kiểm kê và kiểm tra lại của `update -all` | Chưa có lệnh kiểm tra riêng (chỉ đọc), xem mục 3 |
| `replace_cache_content*.py`, `replace_cache_update_version*.py` | `replace` | Tool cũ chỉ quét một cấp thư mục |
| `cron-cache-clean-v2.py`, `cron-cache-segment-update-v2.py` | `update -txt` theo lịch, hoặc `-segment` | **Còn chạy trên production**, phải tắt khi chuyển |

## 3. Còn thiếu / bỏ sót trong pipeline

### A. Phải có trước khi chạy production tự động

1. **Thay đổi trên site chưa tự đi tới cache, S3 và KV.**
   - `urls sync` chỉ ghi vào DB: URL mới, bài đã sửa, URL đã mất. **Chưa có bước tự xử lý tiếp.**
   - Hậu quả: bài **đã xóa** vẫn còn trên S3 và KV. Worker sẽ phục vụ trang cũ cho khách châu Mỹ cho tới lần `update -all` kế tiếp (tới 1 tháng). Bài **đã sửa** cũng vẫn là bản cũ trên S3.
   - Đề xuất lệnh `dott-cache sync -site <domain>`, chạy sau mỗi `urls sync`:
     - URL mất: DELETE key KV → xóa file trên server → xóa trên S3;
     - URL mới hoặc bài đã sửa: `update` cho chính URL đó, cộng trang chủ và trang 1 của các archive chứa bài.
2. **Sửa bài chưa được cập nhật ngay.**
   - Production bật `wp-rocket-no-cache-auto-purge`, nên WP Rocket không tự xóa cache khi sửa bài. Với lịch 8 giờ/lần, bài sửa có thể cần tới 8 giờ mới cập nhật (cả trên origin).
   - **Bình luận mới không làm đổi `post_modified`**, nên `-since` không bắt được. Tool cũ có hook bình luận trong `ct-site-enhancer` (F12).
   - Cách giải: mu-plugin `dott-cache.php` ghi sự kiện (lưu bài, xóa bài, duyệt bình luận) theo design mục 7. Nếu chưa muốn làm thì `export-urls --since` lấy thêm bài có bình luận được duyệt sau mốc.
   - **Cần chủ site chọn:** cập nhật gần như ngay (mu-plugin) hay chấp nhận trễ tới 8 giờ.
3. **Worker v2 chưa có các quy tắc của Worker v7.** v7 có `handleStaticRedirect` (chuyển ảnh và tài liệu sang `cdn.diaocthongthai.com`) và `handlePageRedirect` (410, `/tag/` → `/tags/`, `/feed/`, `/map-*` → `/en/map-*`…). Phải chuyển các quy tắc này sang v2 trước khi thay v7, nếu không thì **mất các redirect và 410 hiện có**.
4. **Lịch chạy và chống chạy chồng.**
   - Cần systemd timer trên VPS phụ:
     - `urls sync -since` 8 giờ/lần, kèm `sync` (mục 1);
     - `urls sync` toàn bộ 1 lần/ngày;
     - `update -all` hằng tháng.
   - Thêm khóa theo site trên VPS phụ: hiện mới có khóa trên VPS chính (cho prefork), 2 lệnh `update` chạy cùng lúc vẫn có thể giẫm lên nhau ở bước S3/KV.
   - Xoay vòng file trong `logs/`.
5. **Cấu hình production:**
   - `config/sites/diaocthongthai.com.json` cần thêm `targets` (bucket `cdn.diaocthongthai.com` Chicago, prefix `_cache_html/`), `renew.command`, `kv.namespace_id` (`cc831…`), `throttle`.
   - Thêm `diaocthongthai.com` vào `dott-prefork`.
   - Cài `mu-plugins/dott-cache/*.php` vào site production.

### B. Nên có (vận hành và an toàn)

6. **Lưu trạng thái từng URL vào DB.** Hiện DB chỉ có danh sách URL, chưa có bảng `cache` / `url_targets`: lần render, upload và lỗi gần nhất. Vì vậy chưa tự thử lại URL lỗi và chưa báo cáo được độ phủ. Đề xuất: ghi kết quả của `update` và `replace` vào DB, rồi thêm `dott-cache status`.
7. **Báo cáo qua Telegram** (đã chốt dùng chung bot với `/root/health`): kết quả mỗi lần chạy theo lịch, lỗi, URL mất bất thường.
8. **Lệnh kiểm tra riêng, chỉ đọc** (`dott-cache check`): so DB, server, S3 và KV mà không sửa gì. Chạy hằng tuần.
9. **Bảo mật:**
   - Key SSH riêng cho service, bị khóa vào `dott-prefork` bằng `command=` (hiện dùng key root `main` của Claude).
   - Key S3 riêng chỉ có quyền trên bucket.
   - Token Cloudflare hẹp hơn:
     - KV: chỉ Workers KV Edit;
     - Worker: Workers Scripts + Workers Routes, để deploy Worker lên route production;
     - purge: dùng token zone đã có ở `/var/hostvn/.cloudflare.conf`.
   - **Đổi (rotate)** 2 token đã dán trong hội thoại.
10. **Bucket châu Âu cho production.** Hiện production chỉ có Chicago. Muốn Worker phục vụ khách EU từ Frankfurt thì cần tạo bucket và upload (chạy `update -all` với 2 đích).

### C. Sau khi chạy ổn
11. Backup `state.db` hằng đêm (dựng lại được từ export, nên ưu tiên thấp).
12. Dọn môi trường test: Worker `workers.dev`, KV `php81-cache-test`, object trên bucket `congtoanphp`, các site test php81 (theo ý chủ site).
13. Xóa lệnh cũ `renew` và `regen-all`.

## 4. Hiện trạng production (đo 28/11, chỉ đọc)

| | |
|---|---|
| Cache WP Rocket | 14 GB: 61.367 `index-https.html`, **58.282 bản mobile, 63.759 `.mobile-active`** |
| Ổ đĩa `/home` | 77% (còn 11 GB) |
| WP Rocket | `cache_mobile`=1, `do_caching_mobile_files`=1 (vẫn tách mobile) |
| Theme | **chưa có tt-core** |
| Plugin sticky | **3.0.0** (cần 3.2.0) |
| mu-plugins | `dott-mcloud-overrides.php`, `thongthai-overwrite.php` (chưa có `dott-single-cache`, `dott-cache/`) |
| Cron cũ | `cron-cache-clean-v2.py` (phút 10 các giờ 0, 4, 8, 12, 16, 20), `cron-cache-segment-update-v2.py` (3:45, 15:45): **vẫn bật** |
| Screen | `en-map-of`, `F1`, `F2` (còn sót) |
| KV production | 60.687 key, **0 key dạng URL đầy đủ** (lỗi F4 đã được sửa trước đó) |
| DB tạo từ export | 61.175 URL active; 0 path có chữ hoa; 1 path có %-encode (`/kho%cc%89i-cong-…/`) |

Sau khi chuyển sang một bản HTML, bỏ được khoảng 58 nghìn file mobile và gzip của chúng. Cache sẽ còn khoảng một nửa.

## 5. Đề xuất các bước lên production (mỗi bước hỏi chủ site trước, có backup, ghi rollout-log)

| # | Bước | Ghi trên production? | Quay lại |
|---|---|---|---|
| P0 | Chuẩn bị không đụng site: config production, Worker v2 có đủ redirect của v7, lệnh `sync` (mục 3.1), khóa theo site, timer (chưa bật) | Không | — |
| P1 | Đo prefork trên production với 20–50 URL (`update -txt`, chưa upload S3, chỉ đích test) để biết tốc độ và load thật | Có: ghi đè cache của 20–50 URL | Tự lành (cache được tạo lại) |
| P2 | Deploy theme tt-core (cả 2 child theme), build, kiểm tra desktop/mobile | Có | `functions.php` có đường lui; chép lại theme cũ |
| P3 | Plugin sticky 3.2.0 | Có | Cài lại 3.0.0 |
| P4 | mu-plugin `dott-single-cache` + `mu-plugins/dott-cache/` + `dott-prefork` cho diaocthongthai.com | Có | Xóa file mu-plugin |
| P5 | Tắt 2 cron cũ, dừng các screen sót | Có | Bật lại cron |
| P6 | `urls sync` toàn bộ, rồi `update -all` (chạy thử, rồi `-yes`) với bucket Chicago thật: xóa bản mobile và `_gzip` cũ, tạo một bản HTML | Có (lớn, khoảng 1,5 giờ trở lên) | `update -all` lại; Worker v7 tự về origin khi S3 404 |
| P7 | Thay Worker v7 bằng v2 (route production), theo dõi | Có | Deploy lại v7 |
| P8 | Bật timer: 8 giờ/lần, hằng ngày, hằng tháng; Telegram | Có | Tắt timer |

Thứ tự P2 → P4 quan trọng: **phải có tt-core và sticky 3.2.0 trước khi bật một bản cache**. Nếu không, khách mobile sẽ nhận HTML có JS desktop.
