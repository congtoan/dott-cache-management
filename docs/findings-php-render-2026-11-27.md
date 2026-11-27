# FINDINGS – Hiệu suất render PHP, prefork, nén Cloudflare (2026-11-27)

> Trả lời tài liệu [batch-render-wp-rocket.md](../batch-render-wp-rocket.md) và ý tưởng "trung gian Go trả kết quả có sẵn cho WordPress".
> Tất cả đều đo thật trên production và chỉ đọc. Lúc đo VPS chính đang tải thấp (load 0,3), en-map-of đã chạy xong.
> Script đo nằm trong [tools/lab-2026-11-27/](../tools/lab-2026-11-27/). Các file tạm trên 2 VPS đã xóa.

## 1. Môi trường (trả lời câu 1–3, 11–12 của tài liệu)

| Mục | Giá trị |
|---|---|
| WordPress | 6.7.1, **multisite** (blog 1 = vi `/`, blog 2 = en `/en/`), không dùng Polylang/WPML |
| PHP | 8.1.24. FPM: opcache bật (288 MB, `revalidate_freq=180`), `pm=ondemand`, max_children 26, max_requests 500. CLI: opcache tắt |
| Extension | `pcntl`, `posix`, `redis`, `igbinary`, OPcache đều có ở CLI → prefork làm được |
| Plugin active | **50** (tính cả network). Có các plugin dev/admin vẫn bật trên production: `query-monitor` (kèm drop-in `wp-content/db.php`, ghi lại mọi query của **mọi** request), `wp-crontrol`, `better-search-replace`, `wp-all-import-pro`, `wp-all-export-pro`, `post-type-switcher` |
| Object cache | `object-cache.php` (Redis Object Cache) hoạt động tốt: nhiều trang en chỉ còn **2 query DB** |
| WP Rocket | 3.16. **Hầu hết tối ưu output đều tắt**: minify/combine CSS-JS, delay JS, defer, RUCSS, lazyload, CDN đều = 0; chỉ bật `minify_google_fonts`. `cache_mobile=1`, `do_caching_mobile_files=1` (cache mobile riêng). `cache_query_strings=[]` |
| MariaDB | `innodb_buffer_pool_size` = **128 MB** cho DB 2,6 GB. Nhưng DB không phải nút thắt (xem mục 2) |
| Mạng | VPS phụ ↔ VPS chính: ping **0,43 ms** (cùng datacenter Singapore). Tới S3 Chicago: RTT khoảng 220 ms; GET 1 object bằng kết nối mới **khoảng 1,1 s**, trên kết nối đã mở sẵn khoảng 0,1–0,25 s |

## 2. Thời gian một trang thật sự đi đâu

### 2.1 Render PHP qua HTTP, PHP-FPM đã nóng (bỏ qua cache bằng `?djtprobe=N`)
Đo 10 URL × desktop/mobile × 2 lần, gọi từ VPS phụ thẳng vào origin:

| Trang | Thời gian |
|---|---|
| Trang chủ vi `/` (659 KB) | 0,56–0,60 s |
| Bài viết vi, archive, phân trang | 0,22–0,57 s (phần lớn 0,25–0,35 s) |
| Trang en (about-us, country maps, map-of, phân trang) | 0,20–0,43 s (phần lớn 0,20–0,30 s) |

→ **Một lần render PHP chỉ mất khoảng 0,2–0,35 s** (trang chủ khoảng 0,6 s), không phải 3–4 s như ta tưởng.

### 2.2 Bootstrap (WordPress + 50 plugin, gần như không render)
`/wp-json/wp/v2/types/post` (trả 401 ngay sau bootstrap): **175–190 ms** (giá trị nhỏ nhất, cả vi lẫn en).

### 2.3 Phần riêng của từng trang (prefork: bootstrap 1 lần, fork mỗi URL, code đã nóng)

| URL | Tổng (sau bootstrap) | Trong đó DB |
|---|---|---|
| `/` trang chủ vi | 424 ms | 98 ms / 747 query |
| `/3171-can-ho-glory-heights...` (bài vi) | 332 ms | 14 ms / 92 query |
| `/ban-tin-du-an/`, `/page/5/` | 67–70 ms | 8–11 ms |
| `/13-dieu-kien-...` (bài vi) | 44–52 ms | 2 ms |
| `/en/`, `/en/about-us/`, `/page/2/` | 43–66 ms | 0 ms / 2 query |
| `/en/afghanistan-maps/`, `/en/map-of-ho-chi-minh-city/` | 101–109 ms (mobile 135 ms) | 0–13 ms |

### 2.4 Bảng chia thời gian (trả lời câu 4, 9 và giả định A1, A2, A7, A8)

| Phần | Trang thường (vd. `/en/about-us/`, khoảng 220 ms) | Trang chủ vi (khoảng 600 ms) |
|---|---|---|
| **Bootstrap** (nạp 50 plugin, init) | **khoảng 180 ms (75–85%)** | khoảng 180 ms (30%) |
| Main query + template + hook | 40–100 ms | khoảng 330 ms |
| Chờ DB | 0–14 ms (Redis gánh gần hết) | khoảng 100 ms |
| Bước xử lý output của WP Rocket (`rocket_buffer`) | **khoảng 2 ms** (tính năng tối ưu hầu như đều tắt) | khoảng 2 ms |

- **A1 đúng, còn mạnh hơn dự đoán:** bootstrap chiếm khoảng 75–85% thời gian render của trang thường.
- **A7 sai:** DB không phải nút thắt.
- **A8 sai:** bước xử lý của WP Rocket gần như bằng 0.

### 2.5 Phát hiện quan trọng nhất: tool cũ chậm **không phải vì PHP**
Log `tmp_update_en_map_of` (33.077 URL, 2 worker): mỗi URL mất p50 = **7,8 s** (p10 7,3 s, p90 8,4 s), tức khoảng 3,9 s/URL cho cả hệ thống. Trong đó:

| Bước | Thời gian |
|---|---|
| `check_cpu_usage_and_wait()`: `psutil.cpu_percent(interval=1)` **chặn 1 giây** trước mỗi URL | 1,0 s |
| 2 lần render PHP (desktop + mobile) | khoảng 0,6 s |
| Upload S3: **tạo boto3 client mới mỗi URL**, 4 `put_object` **tuần tự** Singapore → Chicago (khoảng 1,1 s cho request đầu, khoảng 0,3–0,5 s cho mỗi request sau), và **khóa chung cho cả 2 worker** | khoảng 2–3 s, chạy nối tiếp nhau |
| Phần còn lại (Python, subprocess curl, đợi khóa) | phần dư |

→ PHP chỉ chiếm khoảng 10–15% thời gian của tool cũ. **Chỉ riêng bản v2** (bỏ chặn 1 s, upload song song trên kết nối giữ sẵn, tự gzip trên VPS phụ) đã đưa mỗi URL về khoảng 2 × 0,27 s = **khoảng 0,55 s tải PHP thật**.

## 3. Ước tính thời gian renew toàn site (60.000 URL × 2 bản)

| Phương án | Mỗi URL | 60k URL, 1 luồng render | Ghi chú |
|---|---|---|---|
| Tool cũ (đã đo) | 3,9 s (2 worker) | **khoảng 65 giờ** | Nghẽn ở upload S3 + chặn 1 s |
| **v2, giữ HTTP render** | khoảng 0,55 s | **khoảng 9 giờ** | Không hack gì WordPress. Chạy 1 slot nên VPS chính dư nhiều CPU |
| v2 + prefork (master bootstrap 1 lần) | khoảng 0,1–0,2 s | **khoảng 2–3 giờ** | Đã chạy thử: HTML giống bản HTTP (mục 4) |
| v2 + prefork + chỉ render 1 bản (bỏ mobile, mục 5) | khoảng 0,05–0,1 s | khoảng 1–1,5 giờ | Cần sửa theme + Worker |

Với vòng renew 30 ngày (khoảng 2.000 URL/ngày): v2 thường cần khoảng **18 phút render mỗi ngày**, prefork cần khoảng 5 phút. **Chỉ riêng v2 đã đủ.** Prefork chỉ đáng làm khi cần "renew toàn site nhanh" sau những thay đổi lớn (theme, menu).

## 4. Prefork spike: kết quả (Giai đoạn 1 của tài liệu)
- Chạy được trên production ở chế độ chỉ đọc:
  - Dùng `DONOTCACHEPAGE` nên WP Rocket không ghi file.
  - Session DB đặt `SET SESSION TRANSACTION READ ONLY`.
  - Mỗi tiến trình con có kết nối Redis riêng (`wp_cache_init()`).
  - Con tự kết thúc bằng SIGKILL, không chạy shutdown hook của plugin.
- Không có lỗi DB nào ghi ra (READ ONLY không bị plugin nào vi phạm trong 14 lần render mẫu).
- Master bootstrap ở CLI mất 1,2–1,3 s (opcache CLI phải biên dịch lại). Chi phí này chỉ trả **1 lần cho mỗi đợt**.
  - Lưu ý: `opcache.file_cache` ở CLI gây **segfault**, không dùng.
- **So HTML với bản render qua HTTP (cùng URL, cùng query string), 12 mẫu:** sau khi chuẩn hóa, mỗi trang chỉ lệch đúng 1 dòng.
  - Dòng lệch là `wpa_field_info` (giá trị ngẫu nhiên chống spam của plugin `honeypot`). Hai lần render HTTP liên tiếp cũng lệch dòng này.
  - Trang chủ lệch thêm `data-token-time` của WPForms (timestamp).
  - **Không có khác biệt nội dung nào.** Bản mobile (UA được đặt trước bootstrap) cũng đúng.
- **Bằng chứng về "state dính từ master":** trong prefork, `wpa_field_info` **giống nhau cho mọi trang trong cùng một đợt**, vì giá trị được sinh lúc master bootstrap. Với file cache thì điều này vô hại, vì bản cache vốn đã dùng chung một giá trị cho mọi khách. Nhưng nó cho thấy rủi ro: plugin nào sinh dữ liệu "theo request" lúc `init` thì sẽ bị đóng băng theo đợt. Cần danh sách plugin như vậy trước khi dùng thật.

## 5. Desktop và mobile khác nhau gần như không đáng kể (A6)
- So file cache trên đĩa: `/en/about-us/` lệch 2 dòng, một bài vi lệch 2 dòng, `/ban-tin-du-an/` 11 dòng, `/en/map-of-ho-chi-minh-city/` 4 dòng, trang chủ 39 dòng.
- Khác biệt chính: theme nạp `thongthai-desktop.js` thay vì `thongthai-mobile.js`, và script `ct-multi-sticky-sidebar` chỉ có ở desktop.
- Hệ quả: nếu theme chọn JS theo thiết bị **ở phía trình duyệt** (một bản HTML cho cả hai), thì:
  - Tắt được "Separate cache for mobile" của WP Rocket.
  - **Số lần render giảm một nửa**, số file trên đĩa và trên S3 cũng giảm một nửa.
  - `detectMobile()` trong Worker không cần nữa.

  Đây là đòn bẩy lớn hơn prefork và không cần hack WordPress. Nhưng phải sửa theme và kiểm tra kỹ giao diện mobile (đặc biệt trang chủ có 39 dòng khác).

## 6. Cloudflare có nén response do Worker tạo không? → **CÓ**
Test bằng Worker tạm `djt-compress-test` trên `*.workers.dev`, đã xóa sau khi test; Worker thật không bị đụng. Object `/en/about-us/index-https.html` trên S3 là 59.453 byte:

| Worker trả về | Client gửi `br, gzip` | Client gửi `gzip` | Client gửi `identity` |
|---|---|---|---|
| `.html` không nén (không đặt Content-Encoding) | **brotli**, 12,7 KB | **gzip**, 12,8 KB | 59,5 KB |
| `_gzip` + `Content-Encoding: gzip` (cách Worker v7 đang làm) | **brotli**, 12,5 KB (Cloudflare giải nén rồi nén lại) | gzip, 12,7 KB | 59,5 KB (Cloudflare giải nén) |

**Kết luận:**
- `_gzip` trên S3 **không cần cho người dùng**: dù Worker trả bản nào, Cloudflare cũng tự nén lại, và còn ưu tiên brotli.
- Lợi ích duy nhất của `_gzip` là truyền từ S3 tới Worker ít byte hơn (khoảng 12 KB so với khoảng 59 KB).
- Có thể đơn giản hóa thành **chỉ lưu `.html` trên S3**: số object giảm một nửa, không cần tự gzip. Đổi lại phải sửa Worker (luôn lấy `.html`) và tốn thêm egress từ Linode.
- Ghi chú: object `.html` trên S3 có `Content-Type: text/html`, `Cache-Control: public, max-age=31536000`, ACL public-read. v2 phải giữ đúng các header này.

## 7. Về ý tưởng "trung gian Go trên VPS phụ trả kết quả có sẵn cho WordPress"
Số đo ở trên cho thấy ý tưởng này **sẽ không mang lại tốc độ đáng kể**:

1. **Phần tốn nhất là PHP tự nạp và khởi tạo 50 plugin + theme (khoảng 180 ms)**, không phải query hay tính toán dữ liệu. Go không thể chạy hộ đoạn này, vì PHP vẫn phải nạp code plugin mới chạy được hook. Gọi sang Go không bỏ được bước đó.
2. **Dữ liệu đã có sẵn trong Redis**, chỉ cách PHP vài micro giây. Nhiều trang chỉ còn 2 query DB, tổng DB 0–14 ms. Một dịch vụ Go trên VPS phụ (cách 0,43 ms) sẽ **chậm hơn** Redis local cho đúng việc này.
3. **Khách thật đã được phục vụ từ file cache** (nginx 8–25 ms, hoặc S3 qua Worker). Request PHP chỉ còn ở admin, AJAX, tìm kiếm và lượt renew cache. Không có nhiều request để tăng tốc.
4. Phần riêng của từng trang (40–100 ms) là template và hook của theme/plugin. Muốn Go làm thay thì phải viết lại theme và plugin bằng Go (Mức 3 trong tài liệu), không đáng.

Ý tưởng "làm sẵn một lần, dùng lại nhiều lần" vẫn đúng hướng, nhưng điểm đặt hợp lý là:
- **Bỏ việc bootstrap lặp lại** khi render hàng loạt (prefork, mục 4), và
- **Render ít lần hơn** (bỏ bản mobile riêng, mục 5; chỉ renew khi có thay đổi, nhờ hàng đợi sự kiện của v2).

## 8. Đòn bẩy cho hiệu suất PHP nói chung (request không cache: admin, AJAX, search)
Xếp theo lợi ích trên công sức. **Đều là đề xuất, chưa làm gì:**

1. **Tắt Query Monitor trên production** (bật lại khi cần debug). Drop-in `db.php` của nó chạy ở **mọi** request, ghi lại mọi query kèm backtrace. Cần đo trước/sau, dự kiến giảm một phần đáng kể của 180 ms bootstrap.
2. **Rà 50 plugin:** tắt các plugin chỉ dùng thỉnh thoảng (`wp-crontrol`, `better-search-replace`, `wp-all-import/export-pro`, `post-type-switcher`...) khi không dùng. Đo thời gian nạp từng plugin để biết plugin nào nặng nhất. Bước tiếp theo có thể làm bằng prefork lab, chỉ đọc.
3. **Bỏ bản mobile riêng** (mục 5): giảm một nửa số lần render, số file và số object S3.
4. **v2 như thiết kế** (đã đủ cho renew 30 ngày, khoảng 18 phút render mỗi ngày).
5. **Prefork** chỉ làm khi cần renew toàn site nhanh. Đã có spike chạy đúng. Muốn dùng thật thì cần: chạy thành service trên VPS chính (dưới user site, `MemoryMax`), có danh sách plugin bị "đóng băng state", ghi file đúng quy ước WP Rocket (tự ghi `index-https.html` / `index-mobile-https.html`, vì `rocket_buffer` gần như không làm gì).
6. `innodb_buffer_pool_size` 128 MB là nhỏ, nhưng DB hiện không phải nút thắt. Chỉ tăng khi có thêm RAM.

## 9. Câu hỏi của tài liệu còn mở
- Câu 7 (plugin nhạy cảm với CLI/state, ghi DB ở frontend): mới có 1 trường hợp (`honeypot`). Cần quét kỹ hơn nếu làm prefork.
- Câu 10 (phân loại 60k URL theo template): chưa làm. Không cần nữa vì Mức 2 (lai Go) đã bị loại theo mục 7.
