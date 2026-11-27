# Nhật ký triển khai Cache System v2

Mỗi giai đoạn trong [design-server-only.md](design-server-only.md) mục 14 có một mục ở đây: làm gì, test gì, số liệu, vấn đề còn lại. Chỉ sang giai đoạn sau khi chủ site đã duyệt.

---

## Giai đoạn 0 – Chuẩn bị (2026-11-27, đang làm)

### Đã xong
- **Repo**
  - Tag `v0-legacy` (commit `701c930`) đánh dấu bộ tool Python cũ.
  - Nhánh `v2` đã push lên `origin`.
  - Thư mục PC `D:\Claude\2026-11-26 Cache System NEW - Server only` (từ 27/11 tối đã **chuyển** thành `D:\Claude\2026-11-27 Cache System v2` bằng `git worktree move`) là **git worktree** của repo `dott-cache-management`, theo nhánh `v2`. Thư mục cũ `D:\Claude\2026-11-25 Cache Management` vẫn ở nhánh `main`.
  - `secrets.json` không có trong worktree (được gitignore). v2 không dùng file này.
- **Go trên VPS phụ**
  - `apt install golang-go` cho **go1.26.8 linux/amd64**, chiếm 77 MB.
  - Build lần đầu (chỉ thư viện chuẩn: `net/http`, `crypto/tls`) mất **77 giây, RAM tối đa 221 MB**, khi đặt `GOMEMLIMIT=200MiB GOFLAGS=-p=1`. Sau khi build, RAM VPS phụ vẫn còn khoảng 566 MB trống, `claude-rc` không bị ảnh hưởng.
  - Quy ước: luôn build với `GOMEMLIMIT=200MiB GOFLAGS=-p=1`.
- **Xác thực chứng chỉ origin**
  - Dùng **SPKI pin** (SHA-256 của public key, dạng base64) thay cho CA gốc của Cloudflare, nên không cần tải file CA.
  - Pin: `oqhm5KRTcPw6B6FNpgTMxww20FoxF3uoY1oBeD+TSAM=`. Đây là public key, không phải bí mật.
    - Lấy từ file `/etc/nginx/ssl/diaocthongthai.com/cloudflare_cert.pem` trên VPS chính (Cloudflare Origin Certificate, hết hạn **13/11/2038**).
    - Đã đối chiếu: khớp với chứng chỉ mà origin trả về trong handshake TLS.
    - Ghi chú: `cert.pem` (Let's Encrypt, hết hạn từ 01/2024) vẫn còn trong thư mục nhưng đã bị comment trong cấu hình nginx, không dùng nữa.
  - Đã test bằng chương trình Go:
    - Pin đúng: `HTTP/2.0 200`, `Content-Encoding: gzip`, có `Last-Modified`, 19.582 byte, **10 ms**, giống kết quả curl.
    - Pin sai: kết nối bị từ chối (`origin pin mismatch`).
  - Khi Cloudflare Origin Certificate được thay mới thì phải cập nhật pin trong config. Service sẽ báo lỗi rõ ràng và tạm dừng, không âm thầm bỏ qua kiểm tra.
- **Skill `vps-claude-control`** trên VPS phụ đã sửa (có backup `*.bak-2026-11-27`):
  - Ghi đúng là UFW đang bật, chỉ mở 22/tcp.
  - Thêm thông tin Go.
  - Ghi rõ SSH tunnel tới MariaDB không dùng được (`no-port-forwarding`).

### Còn lại của giai đoạn 0
- [x] **Kill 3 tiến trình treo** `cron-cache-segment-update-v2.py` (PID 4482, 44223, 53896, đã treo 22–37 ngày; F1). Chủ site đồng ý; làm lúc khoảng 14:00 ngày 27/11.
  - Cả 3 dừng bằng SIGTERM, không cần SIGKILL. Tiến trình cha (cron sh) cũng thoát.
  - `en-map-of` (PID 31442) vẫn chạy bình thường.
  - Chưa sửa timeout trong script, nên cron 3:45/15:45 vẫn có thể treo lại. v2 sẽ thay cron này ở giai đoạn 6.
  - Phát hiện thêm: screen `F1` hiện **chỉ còn một bash rảnh**, `cache_renew_from_db.py` **không còn chạy** (thời điểm dừng chưa rõ; trong log của `cache-tools` chỉ có log en-map-of). Tức là hiện **không có renew nền** nào đang chạy.
- [ ] **S3 key mới** chỉ có quyền trên bucket `cdn.diaocthongthai.com` và **Cloudflare token mới** chỉ có quyền Workers KV Edit: chủ site tự tạo trên trang quản trị. Chưa cần ngay, giai đoạn 1 chỉ đọc. Cần có trước giai đoạn 4–5.

---

## Giai đoạn 1 – Đo và thử (2026-11-27, kiểm tra nhanh xong)

### Kiểm tra: nginx có trả đúng file WP Rocket trên đĩa không
Câu hỏi được kiểm tra: khi VPS phụ gọi một URL đã có cache, nội dung nhận về có **giống hệt file đang nằm trên đĩa** không? Phép kiểm tra không so với bản PHP render mới, cũng không so với S3, nên chuyện cache cũ hơn thực tế không ảnh hưởng.

- **Mẫu:** 20 URL gồm trang chủ vi và en, bài viết vi, page en, archive và archive phân trang (vi `/ban-tin-du-an/page/N/`, en `/maps-of-all-philippine-provinces/page/N/`). Tránh `/en/map-of-*` vì en-map-of đang chạy.
- **Cách làm (chỉ đọc):** VPS phụ gọi 4 request cho mỗi URL: desktop/mobile, có và không có `Accept-Encoding: gzip`. md5 của file trên đĩa được đọc qua `ssh main` **trước và sau** khi gọi, để loại trường hợp file bị thay giữa chừng (không có trường hợp nào).
- **Kết quả:**

| | Kết quả |
|---|---|
| Bản `.html` desktop và mobile (gọi không có gzip) | ✅ **40/40 khớp md5** với `index-https.html` và `index-mobile-https.html` trên đĩa. Có `Last-Modified`, bản mobile khác bản desktop, mỗi request khoảng 8–25 ms (không gọi PHP) |
| Bản gzip (gọi có `Accept-Encoding: gzip`) | ⚠️ 40/40 **lệch md5** với file `_gzip` trên đĩa, nhưng **giải nén ra thì giống hệt file `.html` trên đĩa**. Nghĩa là nginx **không trả file `_gzip`** mà nén file `.html` ngay lúc gửi (`gzip on`, `gzip_comp_level 2` trong `/etc/nginx/extra/gzip.conf`). File HTTP nhận được lớn hơn khoảng 12% so với `_gzip` trên đĩa |

**Kết luận và điều chỉnh thiết kế (design mục 5):**
- Lấy qua HTTP **đúng nội dung** file cache, nên phương án A dùng được.
- Chỉ lấy **2 bản `.html`** (desktop và mobile), **không gửi `Accept-Encoding`**. ~~nginx gửi file tĩnh bằng sendfile~~ **Đính chính 27/11 tối:** không có rocket-nginx. File được `advanced-cache.php` của WP Rocket trả bằng `readfile()` (khoảng 6–8 ms, không bootstrap WordPress). Có tốn một worker PHP-FPM trong vài ms, nhưng vẫn rẻ. Xem design mục 5b.
- VPS phụ **tự tạo `_gzip`** (gzip -9) trước khi upload S3. Đo được: 40 file trung bình 114 KB mất 293 ms tổng cộng, tức khoảng 7 ms mỗi file trên VPS phụ. Bản gzip -9 nhỏ hơn bản nginx level 2 khoảng 13%.
- Băng thông: khoảng 2 × 114 KB = **khoảng 230 KB mỗi URL**. 60 nghìn URL thì khoảng **14 GB mỗi vòng renew**. Với vòng 30 ngày, con số này nhỏ so với hạn mức băng thông của Vultr.
- `_gzip` trên S3 không còn giống từng byte với `_gzip` trên đĩa VPS chính, nhưng **giải nén ra giống hệt nhau**. Worker Cloudflare chỉ cần một file gzip hợp lệ.

**Phát hiện thêm (không thuộc cache system, chỉ ghi lại):**
- ~~rocket-nginx có `location ~ ..._gzip$`~~ **Đính chính 27/11 tối:** rocket-nginx **không được include** trong nginx. WP Rocket `advanced-cache.php` đọc `_gzip` bằng `readgzfile()` (giải nén), rồi nginx nén lại. Tức là **mỗi lượt khách qua Cloudflare đều tốn CPU giải nén và nén lại** trên VPS chính, dù WP Rocket đã có sẵn file `_gzip`.
- Nếu muốn tiết kiệm CPU VPS chính thì có thể bật rocket-nginx (đã có sẵn ở `/etc/nginx/rocket-nginx/`) hoặc bỏ `_gzip`. Cần chủ site quyết định; chưa đụng tới.

---

## Môi trường test php81.congtoan.net (2026-11-27 tối, chủ site đồng ý)

Mục đích: mô phỏng diaocthongthai.com để test tt-core, cache một bản HTML, upload S3 nhiều đích, đổi version. **Chưa đụng site chính** (production chỉ đọc).

| Việc | Chi tiết |
|---|---|
| Site test | Site 3 `/tttest/` (theme `generatepress_child` + tt-core, đóng vai vi), site 4 `/tttest-en/` (`generatepress_en`, đóng vai en). Site 1 và 2 của php81 giữ nguyên |
| Plugin chép từ production | wp-rocket **3.16**, wp-rocket-no-cache-auto-purge, wp-rocket-preload-change-parameters, wp-rocket-cache-count, gp-premium, ct-site-enhancer (network), honeypot, kk-star-ratings, luckywp-table-of-contents, seo-by-rank-math (+pro), shortcoder, ct-multi-sticky-sidebar **3.0.0** (network, thay bản 2.0.0). Bản cũ (wp-rocket 3.18, sticky 2.0.0) đã chuyển sang `/home/php81conZSI/plugin-backup-2026-11-27/`. Bật trên site 3 và site 4 giống blog 1 và 2 của production, thêm ACF Pro và WPForms. **Không chép:** wpdatatables, smart-slider-3, redis-cache, query-monitor, wp-all-import/export, redirection |
| Thiết lập chép | `wp_rocket_settings` (**bỏ các trường license**: consumer_key, consumer_email, secret_key, license, secret_cache_key), `generate_*`, `theme_mods_generatepress_*` (bỏ menu và logo), `sidebars_widgets` và `widget_*`, `lss_settings`/`lss_runtime`, `kksr_*`, `lwptoc_*`. Permalink `/%postname%/`, tag base `tags` (giống production). Cấu hình ACF (`acf-post-type`, `acf-taxonomy`, `acf-field-group`, `acf-field`): vi 23 bản ghi, en 8 (đăng ký `map_0`…`map_3`) |
| Bài viết | Lấy ngẫu nhiên bài thật từ production (chỉ đọc DB), kèm chuyên mục, tag, meta: **vi 1.000 `post`** (22 chuyên mục); **en 218** (193 `map_3`, 24 `map_2`, 1 `map_0`). Blog en của production chỉ có 18 bài loại `post`, phần lớn nội dung là `map_*`. File JSON tạm trong `/tmp` đã xóa |
| `wp-config.php` của php81 | WP Rocket tự đặt `WP_CACHE` = true. Đã backup `wp-config.php.bak-2026-11-27` |
| License WP Rocket | `rocket_valid_key()` = yes trên site test (license php81 có sẵn từ trước, Claude không nhập key) |

**Kết quả kiểm tra:**
- Cache chạy giống production: render lần đầu 0,6–1,6 s, các lần sau 10–30 ms do `advanced-cache.php` trả. Có **4 file mỗi URL** (desktop, mobile, mỗi loại thêm `_gzip`).
- Bản desktop và mobile chỉ khác ở: `lss-3.js` (sticky sidebar, để sau), honeypot `wpa_field_info` (giá trị ngẫu nhiên), `rocket_lcp_data` (cờ mobile của WP Rocket), `cached@`. Phần tt-core giống hệt nhau.
- **Lỗi gặp và đã sửa:** `advanced-cache.php` được tạo lúc "cache mobile" còn tắt nên thiếu `Mobile_Detect`, làm khách mobile nhận bản desktop. Đã tạo lại bằng `rocket_generate_advanced_cache_file()`. Ghi nhớ: **sau khi đổi `cache_mobile` phải tạo lại file này.**
- **Đính chính thiết kế:** response render mới **cũng có `Last-Modified`**. Dấu hiệu đúng của file cache là comment `cached@` (design mục 5, điểm 2).
- Bài `map_*` lúc đầu trả 404 vì permalink của site mới là `/%year%/...`. Đổi về `/%postname%/` thì hết lỗi.

### Bucket S3 test (chủ site tạo 27/11)
- `congtoanphp` ở **us-ord-1** (Chicago) và **eu-central-1** (Frankfurt). Dùng key giống bucket `cdn.diaocthongthai.com` Chicago, đọc từ `cache-tools/utils.py` trên VPS chính. Không in ra, không chép đi đâu.
- Kiểm tra từ VPS chính (Singapore) bằng boto3, mỗi bucket ghi rồi xóa 1 object `_tt-test/ping.txt`:

| Bucket | head_bucket (kết nối mới) | list | PUT nhỏ, cùng kết nối (5 lần) |
|---|---|---|---|
| Chicago | 0,67 s | 0,25 s | **0,45 s** mỗi lần |
| Frankfurt | 0,49 s | 0,28 s | **0,32 s** mỗi lần |

- Cả 2 bucket đang trống (KeyCount = 0) và key hiện tại có quyền ghi.
- PUT mất khoảng 2 RTT: boto3 gửi `Expect: 100-continue` rồi mới gửi body. Bản Go nên tắt cơ chế này để còn khoảng 1 RTT. Với 8 goroutine cho mỗi đích, dự kiến **18–25 PUT/s cho mỗi đích**. Sẽ đo lại từ VPS phụ.
- Vẫn nên tạo **key riêng, quyền hẹp** cho dott-cache (design mục 12) trước khi đưa key sang VPS phụ.

### rocket-nginx: nginx phục vụ thẳng file cache (chủ site yêu cầu, 27/11 tối)
- File cấu hình: `/etc/nginx/extra/rocket-page-cache.conf` (nguồn trong repo: `deploy/nginx/rocket-page-cache.conf`). Lấy phần PAGE CACHE của rocket-nginx 3.0.2, có sửa:
  - Bỏ các block CSS/JS/media, để giữ header 1 năm `immutable` của `staticfiles.conf`.
  - Bỏ quy tắc brotli, vì nginx này không có brotli: nhận gzip thì trả thẳng `_gzip`.
  - Dùng `location ^~ /wp-content/cache/wp-rocket/`. Lý do: `wordpress_secure.conf` có location `.html$` đặt `expires 365d`. Nếu nó bắt được file HTML thì trang sẽ bị cache 1 năm.
  - Bỏ qua thêm theo UA `facebookexternalhit|WhatsApp`, giống WP Rocket.
- Đã include trong server 443 của **php81.congtoan.net** và **diaocthongthai.com**, đặt sau khối `if (!-e $request_filename)` và sau luật chặn UA rỗng.
  - Backup: `/etc/nginx/conf.d/*.conf.bak-2026-11-27`.
  - `nginx -t` OK, đã `systemctl reload nginx`.
  - congtoan.net **không** áp dụng, vì không dùng WP Rocket.
- **Giới hạn quan trọng:** thư mục URL nào có `.mobile-active` (WP Rocket đang tách cache mobile) thì rocket-nginx **bỏ qua** và để PHP trả như cũ. Production đang tách mobile nên hiện **chưa có lợi ích**: kiểm tra 4 URL × desktop/mobile, tất cả 200, có `cached@`, có gzip, UA rỗng vẫn 403, error log không có lỗi mới. rocket-nginx chỉ phát huy khi chuyển sang một bản HTML.
- Kết quả trên php81 `/tttest/`, đã chuyển sang **một bản HTML** (`cache_mobile` = 0) để thử:

| Request | Kết quả |
|---|---|
| Desktop, lần 2 | **HIT** 5–13 ms, md5 giống file trên đĩa, `text/html; charset=utf-8`, không có Cache-Control (giống trước) |
| Có `Accept-Encoding: gzip, br` | HIT, trả `_gzip` (16 KB thay vì 64 KB) |
| Mobile | HIT, cùng file với desktop |
| `?utm_source=` | HIT |
| `?x=1`, cookie `wordpress_logged_in_`, UA facebookexternalhit, POST | BYPASS, PHP render |
| `/tttest-en/` (vẫn tách mobile) | BYPASS, PHP trả đúng bản desktop/mobile |

- Lỗi gặp và đã sửa: `advanced-cache.php` **dùng chung cho cả mạng multisite** và được tạo theo thiết lập của site đang chạy lệnh. Tạo từ site đã tắt cache mobile thì site còn tách mobile bị trả nhầm bản desktop. Đã tạo lại từ `/tttest-en/`. Trên production cả 2 blog cùng thiết lập nên không gặp lỗi này. Khi chuyển production sang một bản HTML thì phải chuyển **cả 2 blog cùng lúc**, rồi tạo lại file này.
- Header `X-Rocket-Nginx-Serving-Static` có lúc không xuất hiện, do giới hạn của `add_header` ở cấp server. Chỉ là header debug.

### Sticky sidebar 3.2.0 (chủ site cập nhật) trên site test
- Module `ct-multi-sticky-sidebar` (device desktop, nguồn plugin) đã có trong bản build tt-core của cả 2 site. HTML không còn thẻ `lss-*.js`.
- So HTML render desktop với mobile: chỉ còn lệch honeypot. Có lúc lệch thêm `og:image`, nhưng đó là hiện tượng **riêng của php81**: thẻ chỉ có ở lần render đầu tiên của mỗi bài, thiết bị nào cũng vậy (đã thử thứ tự M-D-D-M trên 4 bài). Nhiều khả năng do bài test không có ảnh đại diện. Production render nhiều lần vẫn luôn có `og:image`.
- Trình duyệt:
  - Desktop 1366px: tải `lss-3.js`, sidebar `position: fixed` khi cuộn, 0 lỗi.
  - Desktop 887px (< `minWidth` 901): không tải.
  - Mobile: module bị bỏ qua, không tải.

### Key S3 trên VPS phụ (chủ site giao Claude quản lý)
- `/etc/dott-cache/s3.env` (root, 600; thư mục 700), gồm `S3_ACCESS_KEY` và `S3_SECRET_KEY`. Lấy thẳng từ VPS chính qua `ssh main`, không hiện ra hội thoại. Đây cùng key với bucket `cdn.diaocthongthai.com` Chicago.
- Đã thử từ VPS phụ bằng `curl --aws-sigv4`: list `congtoanphp` us-ord-1 OK (0,69 s), eu-central-1 OK (0,50 s). Key được đưa vào curl qua file config tạm, không lộ trên `ps`.
- Claude trên VPS phụ: đã thêm luật cấm `Read(/etc/dott-cache/**)`, `Bash(cat /etc/dott-cache/*)`, `Bash(*/etc/dott-cache/*)` vào `/root/.claude/settings.json` (siết quyền, có backup `.bak-2026-11-27`).

### Bản thử Go uploader `dott-cache upload` (27/11 khuya)
- Code: `cmd/dott-cache`, `internal/s3` (tự ký SigV4, chỉ dùng thư viện chuẩn), `internal/origin`. Binary nằm ở `/opt/dott-cache/bin/dott-cache` trên VPS phụ (9,5 MB), build bằng Go 1.26.8 với `GOMEMLIMIT=200MiB`. Chạy bằng `systemd-run -p MemoryMax=150M`.
- Luồng xử lý: lấy HTML từ origin **một lần** (gọi thẳng IP 45.77.32.177, kiểm tra SPKI pin, không gửi `Accept-Encoding`, phải có `cached@`), rồi đẩy song song lên mọi đích S3. Mỗi đích có pool riêng, PUT có `Content-MD5`, kiểm tra ETag bằng MD5, không gửi `Expect: 100-continue`.
- Cấu hình test: `/etc/dott-cache/upload-test.json`. Origin là php81 (SPKI pin `snOoK9P5…IXZU=`, Cloudflare Origin cert, hạn 2038). 2 đích `congtoanphp` us-ord-1 và eu-central-1, prefix `_cache_html/`. Danh sách path: `/opt/dott-cache/test/paths-tttest.txt` (1.024 URL của `/tttest/`).
- Đối chiếu: object `_cache_html/tttest/hello-world/index-https.html` trên cả 2 bucket có md5 **giống file trên đĩa**, `Content-Type: text/html`, `Cache-Control: public, max-age=31536000` (giống tool cũ).

| Lần chạy | Luồng lấy HTML / PUT mỗi đích | Tổng | URL/giây | Lấy HTML p50 | PUT p50 Frankfurt / Chicago | Lỗi | Tải VPS chính |
|---|---|---|---|---|---|---|---|
| 10 URL đầu (phải render) | 1 / 8 | 8,9 s | 1,1 | 837 ms | 248 / 301 ms | 0 | không đổi |
| 1.024 URL, **1.014 trang phải render** | 1 / 8 | **942,8 s** (15,7 phút) | 1,1 | 849 ms (p95 1,3 s) | 348 / 447 ms | 0 | load 1 phút lên khoảng 1,1–1,2 |
| 1.024 URL, đã có cache (nginx HIT) | 4 / 8 | **45,2 s** | 22,6 | **2 ms** | 230 / 328 ms | 0 | không đổi (0,45 → 0,35) |
| 1.024 URL, đã có cache | 4 / 16 | **23,9 s** | **42,9** | 2 ms | 232 / 343 ms | 0 | không đổi; RAM VPS phụ còn 558 MB trống |

**Kết luận:**
- Khi đã có cache, tốc độ **tăng tuyến tính theo số luồng PUT** và giới hạn bởi Chicago (khoảng 1,5 RTT mỗi PUT). Với 16 luồng mỗi đích: 60 nghìn URL × 2 đích khoảng **23 phút**. Tool cũ cần khoảng 65 giờ.
- Lấy HTML qua nginx tĩnh gần như miễn phí (2 ms, không đụng PHP). Vì vậy rocket-nginx kết hợp một bản HTML là điều kiện để upload hàng loạt nhanh mà không tốn tải VPS chính.
- Khi phải render, nút cổ chai là PHP: khoảng 0,85 s mỗi trang ở 1 slot (khoảng 70 trang/phút), và load VPS chính khoảng 1,1. Đây là phần prefork có thể rút ngắn. Hiện chỉ cần khi render lại toàn site.
- Object test (1.024 trang × 2 bucket) **vẫn để trên bucket test** để dùng cho bước tiếp theo. Dọn bằng `dott-cache delete -config ... -paths ...`.

### Việc không liên quan nhưng cần biết
- `tmp_update_en_map_of.py` (screen `en-map-of`) lúc 13:47 ngày 27/11 đã chạy tới 30.612/33.079 URL, có 2 lỗi. Không bị đụng tới.
