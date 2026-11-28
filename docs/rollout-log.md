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

### Một bản cache trong WP Rocket: mu-plugin `dott-single-cache.php` (27/11 khuya, php81)
- WP Rocket 3.16 không có nút tắt "Separate cache for mobile". Đặt `cache_mobile` = 0 (như đã làm lúc đầu trên `/tttest/`) là **sai**: khách mobile không được PHP phục vụ cache và request mobile không ghi cache. Chi tiết ở design 5b.
- Đã cài `wp-content/mu-plugins/dott-single-cache.php` trên php81 (áp dụng cả mạng php81). Plugin ép `cache_mobile` = 1, `do_caching_mobile_files` = 0. Đã tạo lại config cho site 3, site 4 và `advanced-cache.php`, rồi xóa cache 2 site test.
- Test:
  - Ghi thử `do_caching_mobile_files` = 1 (giả lập lúc lưu giao diện hay nâng cấp): file config vẫn ra `$rocket_cache_mobile = 1; $rocket_do_caching_mobile_files = 0`.
  - Trang chưa có cache, request **mobile đầu tiên** render (0,9–1,3 s) và ghi `index-https.html` + `_gzip`. Không có file mobile, không có `.mobile-active`. Các request sau, mobile và desktop, đều được nginx HIT trong khoảng 5 ms.
- **Từ giờ cả `/tttest-en/` cũng là một bản cache.** Đường "tách mobile" không còn site test nào; nếu cần thử lại thì gỡ mu-plugin.

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

### Prefork ghi cache thật trên php81 (27/11 khuya)
- Chi tiết: `findings-php-render-2026-11-27.md` mục 4b. Script trong repo: `tools/prefork/` (`prefork-render.php`, `compare-20.sh`, `full-run.sh`). Trên server: `/home/php81conZSI/prefork/`, `/root/pf-compare-20.sh`, `/root/pf-full-run.sh`.
- Kết quả:
  - **1.024 URL trong 73 s** (14 URL/s), 0 lỗi. Qua HTTP mất 942,8 s.
  - Load trung bình 0,92. 2 tiến trình con song song không nhanh hơn, vì máy 1 vCPU.
  - So với render qua HTTP: 19/20 trang giống hệt. Trang còn lại lệch ở danh sách bài, và danh sách này cũng lệch giữa các lần render HTTP.
- Thay đổi trên php81 trong lúc test:
  - `wp-config.php` thêm `WP_DEBUG_DISPLAY` = false (backup `wp-config.php.bak-2026-11-27-debug`).
  - Cache `/tttest/` bị xóa và tạo lại nhiều lần. Hiện đang là bản do prefork tạo.
- **Chưa thử trên production.** Số đo php81 không có Redis, nên phải đo lại trên production trước khi kết luận.

### Sửa lỗi 500 trên php81 `/` và `/en/` (28/11, chủ site đồng ý)
- **Triệu chứng:**
  - blog 1 `/`, blog 2 `/en/` và `/wp-admin/` báo "There has been a critical error";
  - `/tttest/` và `/tttest-en/` vẫn chạy bình thường.
- **Nguyên nhân (lỗi của Claude khi dựng môi trường test):**
  - Lệnh `wp --url=.../tttest/ plugin activate ct-site-enhancer` bật plugin này cho **toàn mạng**, vì plugin có header `Network: True`. WP-CLI tự chuyển sang bật cấp mạng.
  - Plugin này trùng hàm `dynamic_upload_dir()` với theme `generatepress-child` của blog 1 và 2, nên 2 site đó bị fatal.
  - Lỗi có từ 27/11 khoảng 21:39.
- **Sửa:**
  - Chạy `wp plugin deactivate --network ct-site-enhancer`.
  - Backup option ở `/root/php81-active_sitewide_plugins.bak-2026-11-28.json`.
  - Sau khi sửa: `/`, `/en/`, `/tttest/`, `/tttest-en/` đều trả 200, không có fatal mới.
  - Đổi lại, 2 site test không còn favicon, user agent và hook comment của plugin này. Phần cache, tt-core và sticky không phụ thuộc vào plugin.
- **Bài học:**
  - Trước khi bật plugin trên một site con, kiểm tra header `Network:` của plugin.
  - Sau mỗi thay đổi trên multisite, kiểm tra **mọi** blog, không chỉ site đang test.
- Kiểm tra thêm khi tìm lỗi:
  - Plugin sticky 3.2.0 đưa module vào tt-core bằng action `tt_core_modules` (code đưa thẳng vào lúc build). Vì vậy `uploads/tt-core/<theme>/modules/` trống là đúng.
  - `core.js` của cả vi (`50d59ad81d`) và en đều có module `ct-multi-sticky-sidebar`.

### Tool renew: 1 URL và danh sách URL (28/11, php81 + bucket test)
- **Lệnh:** `dott-cache renew -config /etc/dott-cache/upload-test.json -url <URL>` hoặc `-paths list.txt`. Code ở `cmd/dott-cache/renew.go`.
- **Các bước:**
  1. Xóa object cũ trên mọi đích S3 (cả key `index-https.html` và `index-mobile-https.html`). Bỏ qua bước này bằng `-keep-s3`.
  2. Gửi danh sách path qua `ssh main` tới script bọc `/root/dott-renew/renew.sh` (nguồn `tools/prefork/renew.sh`). Script này:
     - lọc path hợp lệ;
     - xóa file cache cũ trên đĩa;
     - chia path theo blog (khớp tiền tố dài nhất trong `wp site list`);
     - chạy prefork mỗi blog một lần, có `nice`/`ionice` và `flock`;
     - trả kết quả dạng `R {json}`, kết thúc bằng `DONE`.
  3. Path nào prefork báo ok thì lấy HTML qua HTTP (nginx trả file mới, HIT) và upload lên các đích.
- Script bọc đặt trong `/root` (root, 700), **không** đặt trong thư mục của user site. Lý do: root chạy script này, nên nếu user site sửa được file thì sẽ thành leo quyền.
- **Kết quả:**

| Test | Kết quả |
|---|---|
| 1 URL (`/tttest/thanh-pho-huntington-bang-west-virginia/`) | 2,6 s tổng: xóa S3 0,7 s, prefork 1,1 s (boot 450 ms + 80 ms), lấy HTML và upload 0,7 s |
| 306 dòng (250 vi, 50 en, `/`, `/en/`, 4 dòng lỗi) | 60,6 s tổng: xóa S3 17,5 s, prefork 30,5 s (vi 12,1 URL/s, en 13,6 URL/s), upload 12,6 s. **300/303 ok.** Load VPS chính max 0,96, trung bình 0,47 |

- **Các dòng lỗi được báo đúng:**
  - Bị bỏ ngay ở VPS phụ: host khác, URL ghép hỏng (`https://hosthttps://host/...`), path có `..`.
  - Báo lỗi sau prefork:
    - 404 cho path không tồn tại;
    - "WP Rocket không ghi file cache" cho `/` và `/en/` (2 blog này không bật WP Rocket).
  - Lệnh trả exit 1 và in danh sách path chưa xong.
- **Lỗi đã sửa trong lúc test:**
  - Lần chạy đầu, 250 URL ghép hỏng bị **bỏ qua mà không báo lỗi**.
    - Nguyên nhân: Go cắt sai thành `//php81.../`, sau đó script bọc không xếp được path này vào blog nào.
    - Sửa: Go kiểm tra host và dạng path (`checkLine`). Script bọc từ chối `//`, và báo lỗi mọi path không có kết quả.
  - `https://example.com/...` từng bị cắt thành path của php81. Bây giờ bị từ chối.
- **Lỗi font chữ trên S3** (`congtoanphp/_cache_html/tttest/thanh-pho-huntington-bang-west-virginia/`):
  - Object được upload từ trước khi đặt `WP_DEBUG_DISPLAY` = false, nên có PHP Notice in trước `<!DOCTYPE>`.
  - Trình duyệt vì vậy chạy quirks mode, và `<meta charset>` nằm ở byte 2010, quá 1024 byte đầu, nên trình duyệt đoán sai bảng mã.
  - Header S3 cũng chỉ có `text/html`, không có charset.
  - **Sửa:**
    - mặc định `Content-Type: text/html; charset=utf-8`;
    - `origin.validate` từ chối HTML không bắt đầu bằng `<!DOCTYPE` hoặc có `<b>Notice</b>`/`Warning`/`Fatal error`;
    - đã renew trang đó.
  - **Còn lại:** object test cũ khác (upload trước 22:4x ngày 27/11) có thể cũng dính Notice. Chạy `renew` cho cả 1.024 URL nếu cần dùng lại.
- **Ghi chú hiệu suất:**
  - Bước xóa S3 chiếm 30% thời gian, vì mỗi URL cần 4 lệnh DELETE.
  - Hướng cải thiện: dùng `DeleteObjects` (tối đa 1000 key mỗi request), hoặc dùng `-keep-s3`, vì PUT vẫn ghi đè.
- **Chưa kiểm chứng:** prefork ghi đè lên file cache **đang có**. Script bọc luôn xóa file trước, nên trường hợp này chưa xảy ra.

### Bố cục mới: `/root/tool/cache-system` trên VPS phụ, prefork nằm trong website (28/11, chủ site đồng ý)
- Thiết kế: design mục 11b.
- **VPS phụ:**
  - `/opt/dott-cache` và `/etc/dott-cache` được chuyển hết về `/root/tool/cache-system/{bin,src,config/{sites,secrets},data,logs,test}`. Mọi thư mục là 700.
  - Key S3 được `mv` sang `config/secrets/s3.env` (600), không sao chép.
  - Config test cũ nằm ở `test/old-config/`.
  - `/usr/local/bin/dott-cache` là symlink tới `bin/dott-cache`.
  - `/root/.claude/settings.json` thêm 3 luật cấm đọc `config/secrets` (giữ luật cũ). Backup: `settings.json.bak-2026-11-28`.
- **VPS chính:**
  - `prefork-render.php` mới cài ở `php81.../wp-content/mu-plugins/dott-cache/` (user site, 644). Gọi qua HTTP trả 200 với 0 byte, không chạy gì.
  - Bộ chuyển lệnh cài ở `/usr/local/sbin/dott-prefork` (root, 755). Domain lạ bị từ chối.
  - Đã xóa `/root/dott-renew/`.
  - Bản cũ `/home/php81conZSI/prefork/` (dữ liệu đo 27/11) **vẫn để lại**.
- **Thay đổi trong prefork:**
  - đọc path từ stdin;
  - tự lọc path;
  - tự chia blog: lấy danh sách site bằng một lần bootstrap riêng, lưu tạm 10 phút;
  - xóa cache cũ của **từng URL ngay trước khi render** bằng `unlink`;
  - báo lỗi mọi path không có kết quả.
- **Đo tốc độ xóa** (1.200 file trong `/tmp` trên VPS chính, đã dọn):

| Cách xóa | Thời gian |
|---|---|
| bash vòng lặp gọi `rm` cho từng file | 555–562 ms |
| `xargs rm -f` | 25 ms |
| PHP `unlink`, tính cả 23 ms khởi động | 30–66 ms |

- **Test lại với `dott-cache renew -site php81.congtoan.net`:**

| Test | Kết quả |
|---|---|
| 1 URL | 3,1 s: prefork 1,7 s, gồm lần đầu lấy danh sách site |
| 303 URL | 57,6 s (trước đây 60,6 s): prefork 26,5 s (trước đây 30,5 s), 300 ok, 3 lỗi được báo đúng. Load VPS chính tối đa 0,49 |

  - Object trên cả 2 bucket có `Content-Type: text/html; charset=utf-8`, bắt đầu bằng `<!DOCTYPE html>`, có `cached@` mới.
  - Log tự ghi vào `logs/php81.congtoan.net/renew-<thời gian>.jsonl`.
- **Lỗi của Claude khi sửa tài liệu (đã khôi phục, chưa commit):** một lệnh PowerShell thay chuỗi hàng loạt bị tách cặp thay thế thành từng ký tự, và thay mọi chữ "S" thành "c" trong `findings-php-render`. Đã khôi phục từ git rồi sửa lại bằng Edit.

### `renew -mode auto|http|prefork` và phát hiện nginx `open_file_cache` (28/11)
- **Chế độ:**
  - `auto` (mặc định): tối đa `-http-max` (4) URL thì dùng `http`, nhiều hơn thì dùng `prefork`.
  - `http`: chạy `dott-prefork <domain> --delete-only` (PHP chỉ `unlink`, không nạp WordPress, không chờ khóa), rồi GET để PHP-FPM render như khách thật và WP Rocket tự ghi cache.
  - `prefork`: URL nào lỗi (không phải 404 hay path sai) thì tự thử lại bằng HTTP.
  - Một path chỉ tính là xong khi fetch ok và upload ok lên **mọi** đích.
- **Số đo trên php81:**

| Test | Kết quả |
|---|---|
| 1 URL, http | 3,0 s: xóa trên đĩa 0,4 s, render qua HTTP 1,2 s |
| 1 URL, prefork | 2,8 s |
| 30 URL, http | 28,9 s (khoảng 0,85 s/URL vì php81 không có Redis) |
| 30 URL, prefork | 7,3 s |
| 6 URL có 404 | Cả 2 chế độ báo đúng 404, không thử lại bằng HTTP |

  Kết luận: với vài URL, 2 cách ngang nhau; từ khoảng 5 URL trở lên, prefork nhanh hơn rõ. Ngưỡng 4 giữ nguyên cho tới khi đo trên production.
- **Phát hiện: nginx trả bản cache CŨ sau khi file đã bị xóa hoặc thay mới.**
  - `/etc/nginx/nginx.conf` có `open_file_cache max=2000 inactive=20s; open_file_cache_valid 120s; open_file_cache_min_uses 2;`. URL nào vừa được đọc từ 2 lần trở lên trong 20 s thì nginx giữ file handle cũ, tới 120 s mới kiểm lại.
  - Đã tái hiện:
    - GET 2 lần;
    - `--delete-only` báo `deleted:2`, thư mục đã trống;
    - GET lần nữa vẫn nhận `HIT` với `cached@` cũ.
  - Trên php81, renew prefork rồi renew http ngay sau đó: 5/5 URL bị trả bản cũ.
  - **Hậu quả nếu không chặn:** tool upload bản cũ lên S3, và khách thấy bản cũ tới 2 phút. Nếu chỉ tắt `open_file_cache` trong `location`, phép `if (-f …)` ở cấp server vẫn dùng cache, nên sau khi xóa nginx sẽ trả 404.
  - **Đã chặn ở tool:** `origin.MinCachedAt`. renew chỉ nhận bản có `cached@` >= thời điểm bắt đầu trừ 5 s. Bản cũ thì thử lại 3 lần, sau đó báo lỗi "origin vẫn trả bản cache cũ… open_file_cache" và **không upload**. Đã test: 5 URL bị chặn đúng.
  - **Cách sửa gốc (chờ chủ site đồng ý, vì file dùng chung cho production):** thêm `open_file_cache off;` ở đầu `/etc/nginx/extra/rocket-page-cache.conf` (cấp server, áp dụng cho cả `if -f` lẫn `location`), sau đó `nginx -t` và reload.
  - Production hiện chưa bị ảnh hưởng: thư mục cache còn `.mobile-active`, nên request vẫn đi qua PHP.
- **Đã sửa gốc (28/11, chủ site đồng ý):**
  - Thêm `open_file_cache off;` ở đầu `/etc/nginx/extra/rocket-page-cache.conf`, kèm comment giải thích lý do. Nguồn: `deploy/nginx/rocket-page-cache.conf`.
  - Backup: `rocket-page-cache.conf.bak-2026-11-28`.
  - `nginx -t` ok, đã `systemctl reload nginx`. File này được include cho diaocthongthai.com và php81.
  - Kiểm tra sau reload: `diaocthongthai.com/`, `/en/`, `php81/tttest/`, `php81/` đều 200.
  - Tái hiện lại lỗi cũ: GET 2 lần (HIT) → `--delete-only` → GET trả **MISS** (render mới) → GET trả **HIT với `cached@` mới**. Không còn bản cũ.
  - Kiểm tra `MinCachedAt` trong tool vẫn giữ, để phòng trường hợp khác.

### Danh sách URL → SQLite, Cloudflare KV test (28/11)
- **`export-urls.php`** (`wp/mu-plugins/dott-cache/`, đã cài trên php81) được gọi qua `dott-prefork <domain> --export-urls [--since=N]` (có khóa chung). Bộ chuyển lệnh đã cập nhật, có backup `/usr/local/sbin/dott-prefork.bak-2026-11-28`.
- **php81:** 4 blog, 1.489 URL trong 2,6–3 s.
- **Production** (chỉ đọc, bằng bản tạm `/tmp/dott-export-urls.php` chạy bằng user site, **không cài vào site**, đã xóa sau khi chạy):
  - 65.657 URL trong 18 s (blog 1: 24.346, blog 2: 41.309, `/es/`: 2), load tối đa 1,11;
  - `-since` 8 giờ: 4 s, 0 bài.
- **SQLite:** `modernc.org/sqlite` v1.63.0 (go.mod). Lần build đầu 89 s, RAM tối đa 475 MB, trong scope `MemoryMax=600M`.
- **`dott-cache urls sync`:**
  - php81: lần đầu thêm 1.489 URL; chạy lại thì 0 thay đổi; `-since auto` cũng 0 thay đổi.
  - diaocthongthai.com: đọc từ file export, có config lọc lấy từ tool cũ (`deploy/config/sites/diaocthongthai.com.json`). Giữ 61.175 URL, bỏ 4.480 URL, trong đó 4.401 thuộc taxonomy `current_location_kw` (tool cũ cũng không lấy taxonomy này).
- **So với `cache_urls.db` cũ (60.687 URL):**
  - 60.678 URL trùng nhau.
  - 9 URL chỉ có ở DB cũ: 8 term có count = 0 và 1 bài không còn trong database.
  - 497 URL chỉ có ở DB mới: 263 trang phân trang của tag, 36 tag, 15 trang phân trang của category, 173 bài, cùng một số trang EDD.
  - Thử 16 URL mới ngẫu nhiên: đều trả 200. Phân trang chính xác: trang 192 trả 200, trang 193 trả 404.
  - Đã thêm `^/(en/)?checkout/` vào exclude (5 trang EDD checkout).
- **Cloudflare KV:**
  - Token KV được chép từ `secrets.json` trên PC vào `config/secrets/cf.env` (600), không hiện ra hội thoại.
  - Namespace test `php81-cache-test` có id `a8dfab77d27f4db7ac1c015eaca013f7`. **Không đụng KV của diaocthongthai.**
  - Kết quả `dott-cache kv sync`:
    - PUT 1.489 key: 0,6 s, list kiểm tra đủ;
    - chạy lại: không có gì đổi;
    - gỡ 5 URL trong DB: xóa 5 key, kiểm tra còn thừa 0;
    - `urls sync` đưa 5 URL quay lại: PUT lại 5 key.
  - **Còn thiếu:** `kv sync` chưa kiểm tra URL đã có trên S3 hay chưa (chưa có bảng `url_targets`), nên hiện chỉ dùng với namespace test.

### Worker test cho php81 (28/11)
- **Code:** `deploy/worker/dott-cache-worker.js`. Script deploy: `deploy/worker/deploy-test.sh` (Cloudflare API, token đọc từ `config/secrets/cf.env`).
- **Deploy:** `dott-cache-php81-test.congtoan-real.workers.dev`, dùng KV test `php81-cache-test`. Chưa gắn route vào domain nào.
- **Khác Worker v7:**
  - chỉ một bản `index-https.html`, không còn bản mobile và `_gzip`;
  - châu lục → bucket theo `TARGETS` (NA/SA → Chicago, EU → Frankfurt), châu lục khác vào origin;
  - bỏ qua khi có cookie đăng nhập, bình luận hoặc giỏ hàng;
  - bỏ qua cả `/<blog>/wp-admin/`, `/<blog>/wp-json/` của multisite (v7 chỉ khớp ở gốc site);
  - có header `X-Dott-Cache` cho biết response đi đường nào;
  - giả lập châu lục bằng `X-Dott-Continent`, chỉ có hiệu lực khi kèm `X-Dott-Test-Key` đúng secret (file `config/secrets/worker-test.env`).
- **Kết quả test** (từ VPS phụ ở Singapore):

| Trường hợp | Kết quả |
|---|---|
| Khách thật (AS) | `ORIGIN-region-AS` |
| Giả lập NA, SA | `S3-NA` / `S3-SA` (lần đầu 1,6 s, các lần sau 0,09 s) |
| Giả lập EU | `S3-EU` |
| Giả lập OC | origin |
| Sai key | không giả lập được |
| Path không có trong KV | `ORIGIN-kv-miss` (404) |
| Chữ hoa, thiếu `/` cuối | 301 về URL chuẩn |
| Có cookie đăng nhập | `ORIGIN-cookie` |
| Có query string | `ORIGIN-query` |
| `wp-admin` | `ORIGIN-bypass` (sau khi sửa) |

  - Nội dung qua Worker giống hệt object S3 (md5 khớp ở cả NA và EU).
  - Cloudflare tự nén `br`.
- **Đổi trong uploader:** key S3 được chuẩn hóa viết thường (`kv.Key`), vì Worker dựng URL S3 từ path đã chuẩn hóa. Trên php81 mọi path vốn đã viết thường, nên không có gì thay đổi.

### `dott-cache regen-all`: tạo lại cache toàn site (28/11, php81)
- **Các bước** (chi tiết ở đầu `cmd/dott-cache/regen.go`; không có `-yes` thì chỉ kiểm kê):
  1. LIST S3 một lần trên mỗi đích và ghi manifest (`logs/<site>/regen-all-<thời gian>-s3-manifest-<đích>.txt`). Kiểm kê server bằng `dott-prefork --purge-cache --dry-run`.
  2. Dọn server: xóa bản mobile, `.mobile-active`, file lạ, file trong thư mục mồ côi. Giữ bản desktop cho tới lúc prefork render URL đó.
  3. Xóa trên S3 **một lần ngay từ đầu** (chủ site chốt) mọi file **không phải** `index-https.html`, chỉ những key đã có trong LIST, bằng DeleteObjects theo lô 1.000 key, 4 lô song song mỗi đích. `index-https.html` được giữ rồi **ghi đè** khi upload. `index-https.html` mồ côi chỉ được báo cáo, xóa khi có `-delete-orphans`.
  4. Thứ tự (chủ site chốt): trang chủ và bài đơn → trang 1 của archive (category → tag → taxonomy khác, post type) → trang phân trang. Prefork theo lô 500 URL. Lô nào xong thì upload song song với lô tiếp theo. URL lỗi (không phải 404) thì thử lại bằng HTTP.
  5. Kiểm tra lại: LIST S3 lần nữa và đếm file trên server.
- **Chuẩn bị test:**
  - Bỏ blog `/` và `/en/` của php81 (không bật WP Rocket) bằng `skip_blogs`. DB còn 1.448 URL.
  - Tạo dữ liệu giả của hệ thống cũ:
    - server: 30 URL có bản mobile, mobile `_gzip` và `.mobile-active`, cùng 1 thư mục mồ côi;
    - S3: 40 URL có bản mobile, `_gzip`, mobile `_gzip` trên mỗi đích, cùng 1 html mồ côi.
- **Kết quả:**

| Bước | Kết quả |
|---|---|
| Kiểm kê | S3: 1.074 html giữ, 120 file kiểu cũ, 1 html mồ côi trên mỗi đích. Server: 1.072 html, 90 file mobile, 38 file mồ côi, 376 URL chưa có cache |
| Dọn server | 0,4 s |
| Xóa S3 | 120/120 object trên mỗi đích, 0,4–0,5 s |
| Prefork | 3 lô: 42,5 s, 41,6 s, 34,1 s (11,8–13,2 URL/s), 0 lỗi, không URL nào phải thử lại bằng HTTP |
| Upload | 1.448 × 2 đích, chạy song song với prefork (tổng 59 s) |
| **Tổng** | **142 s cho 1.448 URL (10,2 URL/s), 1.448/1.448 ok** |
| Kiểm tra lại | Mỗi đích có 1.448/1.448 `index-https.html`, file khác còn lại 0, html mồ côi 1 (giữ đúng như dự kiến). Server: 1.448 html và 1.448 gzip, 0 mobile, 0 mồ côi, 0 URL chưa có cache |
| Qua Worker (NA) | 4 URL mẫu (trang chủ, bài, phân trang, en) có `cached@` trùng với origin |

- **Load VPS chính:** tối đa 2,04, trung bình 1,43 (27 mẫu). Cao hơn lúc chỉ chạy prefork (0,92), vì có thêm request lấy HTML để upload. Trước khi chạy cho production cần **điều tiết theo load** (design mục 6), ví dụ nghỉ giữa các lô khi load > 1,5.
- **Ước tính production** (chưa đo): 61.175 URL × khoảng 12 URL/s ≈ 85 phút, nếu tốc độ prefork giống php81.

### `regen-all`: xóa mồ côi TRƯỚC khi generate (28/11, chủ site chốt)
- **Mồ côi** là file cache (server, S3) hoặc key KV của một path **không có** trong tập URL active của DB. Ví dụ: bài đã xóa hoặc chuyển nháp, đổi slug, URL bị loại theo config, trang phân trang không còn, thư mục test.
- **Cách xác định:** lấy thư mục chứa file (với S3 thì bỏ prefix và tên file), chuẩn hóa như Worker (viết thường, `/` cuối), rồi so với tập path active.
- **Thứ tự mới:**
  1. kiểm kê;
  2. xóa KV mồ côi (trước, để Worker không dẫn khách tới object sắp bị xóa);
  3. dọn server (thư mục mồ côi, mobile, file lạ);
  4. xóa S3 (html mồ côi và mọi file không phải `index-https.html`);
  5. generate;
  6. kiểm tra lại.
  - `-keep-orphans`: chỉ báo cáo, không xóa.
- **Hai lớp chặn trước khi xóa** (rủi ro là DB thiếu URL):
  - `urls sync` toàn bộ phải chạy trong vòng 26 giờ;
  - số path mồ côi nhiều nhất trong 3 nơi phải ≤ `max_remove_pct` (2%).
  - Vượt một trong hai thì in lý do và dừng (exit 1) trước khi xóa, trừ khi có `-force`. Bản kiểm kê có in ví dụ mồ côi.
- **Test trên php81:**
  - Tạo mồ côi giả: server 2 thư mục và 1 thư mục có bản mobile; S3 3 html mồ côi trên mỗi đích; KV 3 key mồ côi.
  - Kiểm kê thấy: S3 4 path mồ côi mỗi đích (thêm 1 cái giả từ lần test trước); server 2 path; KV **44 key**: 3 key giả và **41 key thật** của blog `/`, `/en/` vừa bị loại bằng `skip_blogs` nhưng còn trong KV.
  - 44/1.448 = 3% > 2%: tool **dừng đúng**.
  - Giả lập DB cũ (`urls_full_last` lùi 3 ngày): tool dừng với cả 2 lý do, exit 1, không xóa gì.
  - Chạy với `-force` (sau khi đã kiểm tra danh sách):
    - KV xóa 44 key (0,3 s);
    - server xóa 4 file mồ côi và 4 file mobile;
    - S3 xóa 4/4 object mỗi đích;
    - prefork 3 lô (10,8–11,4 URL/s);
    - **1.448/1.448 ok trong 167 s**.
  - Kiểm kê lại sau khi chạy: S3, KV và server đều 0 mồ côi, 0 file kiểu cũ. KV còn đúng 1.448 key.
- **Load VPS chính:** tối đa **3,54**, trung bình 1,58 (lần trước tối đa 2,04). Khi prefork chạy cùng request lấy HTML để upload, máy 1 vCPU bị quá tải theo từng đợt. **Bắt buộc phải có điều tiết theo load trước khi chạy production.**

### Lệnh `dott-cache update` (gộp tool cũ) và điều tiết theo load (28/11)
- **Gộp các tool cũ:**

| Tool cũ | Lệnh mới |
|---|---|
| `cache_create_from_db.py`, `cache_renew_from_db.py` | `update -all` (= `regen-all` cũ). Bỏ vòng renew vô hạn, thay bằng chạy theo lịch, ví dụ hằng tháng (chưa cài lịch) |
| `cache-urls-update.py` + `.txt` | `update -txt [file]` (không có file thì dùng `lists/<domain>/update.txt`; chưa có thì tạo file mẫu), `update -urls [a,b]` (không có giá trị thì hỏi nhập), `update -url URL` |
| `cache-segment-update.py` | `update -segment [link archive,...]` (không có giá trị thì hỏi nhập) với `-segment-mode all\|archive\|posts`. Bài thuộc term được lấy từ site bằng `export-urls.php --terms=BLOG:TAX:ID` (gồm term con, giống trang archive), có lọc theo DB |
| (mới) | `urls list` lọc DB theo kind, type, blog, regex, `page-max`, ngày sửa, rồi `-out` ra file txt làm đầu vào cho `update -txt` |

- **Với danh sách:**
  - sắp thứ tự ưu tiên theo DB; URL không có trong DB có cảnh báo;
  - xóa file kiểu cũ trên S3 của đúng các path đó (LIST trước, 2 đích song song);
  - tối đa 4 URL thì dùng http, nhiều hơn thì prefork theo lô;
  - thêm key KV còn thiếu, và chỉ PUT key thiếu, vì mỗi key ghi vào KV đều tính một lượt ghi;
  - chạy trong terminal thì hỏi xác nhận, trừ khi có `-yes`.
- `renew` vẫn chạy nhưng nên dùng `update`. Prefork giờ xóa cả `.mobile-active` (5 file kiểu cũ) trước khi render.
- **Test trên php81:**

| Lệnh | Kết quả |
|---|---|
| `-url` (http) | 1/1, 6,1 s |
| `-urls` nhập qua stdin (5 URL hợp lệ, 1 host khác bị bỏ) | 5/5, thứ tự đúng: trang chủ → bài → archive → phân trang |
| `-txt` không có giá trị | tạo file mẫu rồi dừng |
| `urls list -kind single -match ^/tttest/xa- -limit 40 -out` rồi `-txt` | 40/40, 12,4 s |
| `-segment /tttest/category/cac-nuoc/` | 2 trang archive + 12 bài, 14/14, 5,6 s |
| `-segment-mode archive` | 2/2 |
| `urls list -modified-after` | đúng |

- **Điều tiết theo load** (config `throttle`: `max_load` 1.5, `resume_load` 1.0, `max_wait_sec` 300, `pause_sec` 0):
  - Trước mỗi lô, Go đọc load (`dott-prefork <domain> --loadavg`) và nghỉ nếu cao.
  - Trong lô, PHP (`--max-load`) kiểm tra mỗi 5 URL, nghỉ tối đa 60 s mỗi lần.
  - Lô mặc định giảm từ 500 xuống 200, để kiểm tra load thường hơn.
- **`update -all` trên php81 có điều tiết:**
  - 1.448/1.448 ok trong 276 s (khi không điều tiết: 142–167 s).
  - Load lúc bắt đầu đã là 2,14 (có hoạt động khác trên VPS chính), nên tool nghỉ 52 s trước lô đầu. PHP nghỉ 2 lần trong lô (56 s và 26 s).
  - Load trong lúc chạy: tối đa 2,41, trung bình 1,34, 22% số mẫu > 1,5. Khi không điều tiết: tối đa 3,54, trung bình 1,58.
  - **Nhận xét:** một tiến trình prefork tự nó đã chiếm load khoảng 1 trên máy 1 vCPU. Phần vượt quá đến từ hoạt động khác (production, PHP-FPM). Load 1 phút phản ứng chậm, nên vẫn có những đợt vượt ngắn.
  - Muốn chặt hơn thì giảm `max_load` (ví dụ 1,3) và kiểm tra mỗi URL thay vì mỗi 5 URL; đổi lại thời gian chạy sẽ dài hơn. **Chủ site chọn mức trước khi chạy production.**

### Lọc theo subsite (`-blog`) và lệnh `replace` (28/11, chủ site đồng ý 6 điểm)
- **`-blog`** (path `"/"`, `"/en/"` hoặc blog_id) dùng được cho mọi phạm vi của `update` và `replace`, kể cả `-all`.
  - Path thuộc blog `/` khi không thuộc blog nào có path dài hơn (với php81, `/` không gồm `/en/`, `/tttest/`, `/tttest-en/`).
  - Map blog → path do `urls sync` lưu ở meta `blog_paths`.
  - Với `update -all -blog`: mồ côi vẫn so với URL active của **mọi** blog. S3, KV và server (dòng `#scope` gửi kèm cho PHP) chỉ đụng tới phạm vi blog đã chọn.
  - Test: danh sách trộn 2 blog thì bỏ đúng 1 URL ngoài blog. `update -all -blog /tttest-en/` chạy 222/222 trong 36,7 s; kiểm kê và dọn chỉ tính 222 path của blog này.
- **`dott-cache replace`** thay `replace_cache_content*.py` và `replace_cache_update_version*.py`:
  - `-content -search … -replace … [-regex]`
  - `-version [-files …]`, mặc định `style.css` và `tt-core/core.js` của `generatepress_child` và `generatepress_en`. Version của `core.js` lấy từ `tt-core/core.manifest.json` (hash), file khác lấy theo `filemtime`. Khớp `/themes/<file>?ver=` bất kể tiền tố blog.
  - Phạm vi giống `update`.
  - Phần sửa file nằm ở `wp/mu-plugins/dott-cache/replace-cache.php`, gọi qua `dott-prefork <domain> --replace`. Job JSON và danh sách path đi qua stdin, không qua shell.
  - Ghi HTML mới, **tạo lại `_gzip` từ HTML mới**, cả hai đều ghi file tạm rồi rename.
  - Luôn chạy thử trước (đếm, in ví dụ trước/sau); ghi thật cần `-yes` hoặc trả lời `y`.
  - Sau khi sửa: lấy lại các trang đã đổi qua HTTP, **kiểm tra nội dung** (content: không còn chuỗi cũ, có chuỗi mới; version: có ít nhất một `?ver=` mới), rồi upload ghi đè lên mọi đích. KV không đổi.
  - Lỗi của tool cũ đã sửa: `replace_cache_content.py` chỉ quét **một cấp thư mục** (`os.scandir`), nên bỏ sót `/category/x/`, `/category/x/page/N/` và `/en/...`.
- **Test trên php81:**

| Test | Kết quả |
|---|---|
| `-version -all`, chạy thử | 0 file cần đổi: version trong HTML khớp `filemtime` và manifest |
| `touch` `generatepress_en/style.css` rồi `-version -all -blog /tttest-en/ -yes` | 222 trang (444 chỗ thay), sửa trên đĩa 0,6 s, upload 222/222 trong 10,8 s, tổng 11,7 s. S3 và `_gzip` trên server có `?ver=` mới; blog vi giữ nguyên |
| `-content` thêm dấu test trước `</body>` trên `-segment … -segment-mode archive` | 2/2 trang, S3 có dấu test |
| `-content` xóa dấu test, `-all` | 2/2 trang, S3 sạch |

  - Chạy thử ban đầu mất 60 s cho 1.448 file, vì cũng nghỉ chờ load như khi ghi. Đã bỏ điều tiết cho chạy thử: còn 0,4 s.
- **Lỗi của Claude, đã sửa trước khi commit:** `urls sync` bị treo (deadlock), vì đọc meta bằng `db` trong lúc transaction đang giữ kết nối duy nhất (`SetMaxOpenConns(1)`). Đã chuyển phần đọc ra trước `Begin()`.

### P1: đo prefork trên production (28/11, chủ site giao)
- **Cài đặt:**
  - `wp-content/mu-plugins/dott-cache/prefork-render.php` trên **diaocthongthai.com** (user site, 644). Gọi qua HTTP thì trả 200 với 0 byte; trang chủ và `/en/` vẫn trả 200.
  - Thêm `diaocthongthai.com` vào `/usr/local/sbin/dott-prefork` (backup `.bak-2026-11-28-p1`).
- **Cách chạy:** 39 URL (1 trang chủ, 19 bài vi, 5 archive, 5 trang phân trang, 10 bài en), dùng `--no-delete`, vì production vẫn tách bản mobile. Không upload S3, không đụng KV.
- **Kết quả:**
  - 39/39 ok; `cached@` mới ở 39/39 URL. Như vậy **prefork ghi đè được file đang có** (trước đây chưa kiểm chứng).
  - Bản mobile và `.mobile-active` không đổi.
  - Load VPS chính tối đa 0,76.

| 39 URL trên production | Tổng | Mỗi URL p50 | p95 |
|---|---|---|---|
| Prefork lần 1 | 15,4 s | 278 ms | 592 ms |
| Prefork lần 2 (đã tắt log lỗi DB) | 13,1 s | 215 ms | 643 ms |
| HTTP (PHP-FPM, tuần tự) | 14,5 s | 398 ms | 665 ms |

  - **Trên production, phần lớn thời gian là chính việc render** (khoảng 200 ms mỗi trang), nên prefork chỉ nhanh hơn HTTP khoảng 1,8 lần, không phải 13 lần như trên php81.
  - Ước tính site vi (24.273 URL): khoảng 1,5–2 giờ chưa tính thời gian nghỉ do điều tiết. Site en (36 nghìn URL, trang nhẹ hơn, p50 131 ms): khoảng 1,5 giờ.
- **Phát hiện:**
  - Plugin kk Star Ratings **ghi DB mỗi lượt render** (`UPDATE _kksr_avg`: 785 lần cho 39 URL). Ở chế độ READ ONLY các lệnh ghi này bị chặn (đúng ý), nhưng WordPress ghi log lỗi kèm backtrace cho từng lệnh. Đã tắt log lỗi (`suppress_errors`) trong tiến trình con. Lỗi DB không lọt vào HTML.
  - So với bản render qua HTTP: bản prefork có LCP beacon và footer `cached@`; bản HTTP có `?dottprobe` nên WP Rocket không chèn 2 phần này, nên đây không phải lỗi. Bản prefork thiếu `<link href='https://fonts.gstatic.com' crossorigin rel='preconnect' />`. **Đính chính cùng ngày: đây không phải lỗi**, xem mục "Preconnect fonts.gstatic: không phải lỗi" bên dưới.

### P0: chuẩn bị, chưa đụng site production (28/11)
- **Worker v2:**
  - Có đủ quy tắc của v7: redirect ảnh và tài liệu sang `cdn.diaocthongthai.com`, `/tag/` → `/tags/`, `/feed/`, `/map-*` → `/en/map-*`, `/en/ban-do*`, 410 cho `link_ds_*`. Bật bằng `REDIRECTS=1`.
  - `LEGACY_PREFIXES` cho blog chưa chuyển (vd `["/en/"]`): chọn bản mobile hay desktop theo User-Agent như v7. Chỉ lấy `.html`, không lấy `_gzip`, để Cloudflare tự nén.
  - Đã test trên workers.dev: vi NA desktop và iPhone đều nhận một bản; "en chưa chuyển" với iPhone nhận bản mobile; các redirect và 410 đúng; POST đi origin.
- **`dott-cache sync [-full] -blog …`:**
  - Chạy `urls sync`.
  - URL mất: xóa key KV → xóa file trên server → xóa trên S3.
  - URL mới, bài sửa, bài có bình luận mới, trang 1 của archive liên quan, trang chủ: chạy `update -txt`.
  - `export-urls --since` lấy thêm bài có bình luận đã duyệt viết trong 48 giờ trước mốc.
  - Test trên php81: sửa 1 bài và thêm 1 bình luận → `sync` cập nhật 5 URL. Đưa 1 bài vào thùng rác → `sync -full` xóa key KV, file trên server, object S3, cập nhật trang chủ. Khôi phục bài → `sync -full` thêm lại URL, PUT lại key.
- **SSH thử lại:** trong lúc test, VPS chính từ chối một kết nối SSH (`Connection reset`, nhiều kết nối liền nhau), làm bước xóa file của bài đã gỡ bị bỏ qua. Đã thêm cơ chế thử lại (2 s, 5 s, 10 s) khi chưa nhận dòng output nào, cho mọi lệnh SSH. Kiểm tra lại: 6 lệnh liền nhau đều không lỗi.
- **Khóa theo site** (`data/<site>/.lock`): lệnh ghi thứ hai bị từ chối. Gọi lồng nhau trong cùng tiến trình (`sync` → `update`) không bị chặn.
- **`write_blogs`** trong config: lệnh ghi phải có `-blog` thuộc danh sách; `kv sync` toàn namespace bị chặn. Đã test trên php81.
- **Config production** `config/sites/diaocthongthai.com.json` (nguồn `deploy/config/sites/`):
  - đích Chicago `cdn.diaocthongthai.com`, KV `cc831…`, throttle 1,5;
  - **`write_blogs: ["-"]` = chưa cho ghi blog nào.**
  - Đã thử: `update`, `update -all`, `sync` với `-blog /` đều báo "không cho /". Kiểm kê chỉ đọc vẫn chạy.
- **Kiểm kê chỉ đọc site vi trên production** (`update -all -blog /`, không `-yes`, 28/11):

| | |
|---|---|
| URL trong DB | 24.273 |
| S3 Chicago | 24.028 `index-https.html` giữ; **72.089 file kiểu cũ** (mobile, `_gzip`) sẽ xóa; 10 path mồ côi (38 file) |
| KV | 9 key mồ côi (8 term rỗng và 1 bài đã xóa, như lần so với DB cũ) |
| Server | 22.011 html; **66.468 file mobile** sẽ xóa; 111 path mồ côi (222 file); 2.262 URL chưa có cache. Quét mất 168 s |

  Mồ côi nhiều nhất 111 path (0,46%), dưới ngưỡng 2%.
- **mu-plugin `dott-single-cache` 1.1.0:** hằng số `DOTT_SINGLE_CACHE_BLOGS` (`null` = mọi blog; `array(1)` = chỉ site vi). Đã cài lên php81 (giá trị `null`): `/tttest/` vẫn có `cache_mobile=1`, `do_caching_mobile_files=0`. Sau khi đổi danh sách phải tạo lại file `wp-rocket-config` của blog đó.
- **Lịch chạy (chưa bật):** `deploy/systemd/` gồm `sync@` (8 giờ/lần), `sync-full@` (hằng ngày), `update-all@` (hằng tháng), instance `vi`/`en`. `dott-cache-run.sh` ghi log, dọn log quá 30 ngày, và cho các lịch chạy nối đuôi nhau (`.cronlock`). Script đã chép vào `bin/` và chạy thử dry-run, nhưng **chưa cài unit**.

### Preconnect fonts.gstatic: không phải lỗi (28/11, đính chính P1)
- Trong WP Rocket 3.16, `GoogleFonts\Subscriber` làm 2 việc:
  - thêm preconnect `fonts.gstatic.com` qua `wp_resource_hints`;
  - trong `rocket_buffer` (`process()`), **xóa thẻ đó nếu trang không dùng Google Fonts**.
- Bản so sánh có `?dottprobe` nên WP Rocket không chạy bước tối ưu, và thẻ không bị xóa. Bản prefork và bản do khách thật tạo đều đã qua `rocket_buffer`, nên không có thẻ.
- **Kiểm chứng trên php81** (3 URL: bài, category, en): xóa cache, cho khách thật GET để WP Rocket ghi file, rồi prefork cùng URL. Sau khi bỏ nonce, honeypot và `cached@`: **0 dòng khác nhau**. Cả 2 bản đều không có preconnect; chỉ bản `?dottprobe` có.
- **Production** (chỉ đọc): 0/200 file cache cũ do khách tạo có thẻ này; trang chủ vi và en cũng không có.
- **Bài học:** muốn so với bản HTTP thì phải so với file cache do request thường tạo, không so với bản có query string. Áp dụng cho các lần so sánh sau (kể cả `compare-20.sh`).

### Test 8 bài vi trên production: xóa cache trên server rồi tạo lại bằng prefork (28/11, chủ site giao)
- **Bài:** `/xa-thanh-luong-vinh-bao/`, `/xa-gia-phong-sau-sap-nhap-2025/`, `/phuong-thong-nhat-tp-buon-ma-thuot/`, `/thanh-pho-schofield-bang-wisconsin/`, `/xa-phuoc-tan-phu-rieng/`, `/xa-binh-an-dong-nai-sau-sap-nhap-2025/`, `/thanh-pho-cherokee-bang-iowa/`, `/phuong-binh-tan-la-gi/`. Danh sách ở `test/t8-prod/list.txt` trên VPS phụ.
- **Trước:** 6/8 bài có đủ 5 file kiểu cũ; 2 bài có 3 file (`.html`, `_gzip`, `.mobile-active`).
- `dott-prefork --delete-only` xóa 38 file. Sau đó prefork (mặc định, có xóa từng URL).
- **Tốc độ:**
  - 8/8 ok, mỗi URL 530–680 ms (p50 599 ms).
  - Tổng 68 s, trong đó **60 s là nghỉ vì load > 1,5** ngay lúc bắt đầu (điều tiết làm đúng việc).
  - Mỗi URL chậm hơn lần đo P1 (p50 215 ms), vì lúc này máy đang bận: load vượt 1,5.
- **File sau khi tạo:** mỗi bài chỉ có `index-https.html` và `index-https.html_gzip`, **không còn bản mobile, không còn `.mobile-active`**.
- **nginx (gọi thẳng origin):** desktop và iPhone đều nhận `X-Rocket-Nginx-Serving-Static: HIT`, `Content-Encoding: gzip`, cùng một `cached@`.
- **Trình duyệt (built-in browser, qua Cloudflare):**
  - Desktop: trang đúng, nginx HIT, `cached@` của bản prefork, có sidebar, có `lss-1.js`.
  - Mobile (giả lập 375 px): hiển thị bình thường, không tràn ngang, không có lỗi console ngoài AdSense.
  - Lỗi AdSense (`No slot size for availableWidth=0`) cũng có ở bài chưa bị đụng tới, nên không liên quan cache.
- **Đúng như dự đoán, cần lưu ý:** theme production chưa có tt-core, nên khách mobile của 8 bài này nhận **JS bản desktop** (`thongthai-script.js`, `lss-1.js`) thay vì `thongthai-script-mobile.js`. Đây là lý do P2 (tt-core) và P3 (sticky 3.2.0) phải làm trước P4 và P6. **Khôi phục cách tách mobile cho 8 bài:** chạy `dott-prefork --delete-only` cho 8 path, khách truy cập sẽ tạo lại bản desktop, bản mobile và `.mobile-active` qua PHP.

### Cài mu-plugin `dott-single-cache` 1.1.0 trên production, bật cho blog 1 (28/11 13:17, chủ site yêu cầu)
- Cài `wp-content/mu-plugins/dott-single-cache.php` với `DOTT_SINGLE_CACHE_BLOGS = array( 1 )`. File mới, không có bản cũ để backup; muốn gỡ thì xóa file.
- Sau khi cài:
  - `/`, `/en/`, bài test đều 200; `wp-login.php` 302;
  - option khi đọc ra: blog 1 `do_caching_mobile_files=0`, blog 2 `=1`;
  - không có lỗi PHP mới.
- **Chưa tạo lại `wp-rocket-config/diaocthongthai.com.php`** (vẫn `$rocket_do_caching_mobile_files = 1`, ngày 09/03). `advanced-cache.php` và `Buffer\Cache` đọc file này, nên cách phục vụ cache **chưa đổi**. File sẽ được tạo lại khi lưu thiết lập WP Rocket của site vi, hoặc chủ động ở bước P4 (sau P2/P3).
- **Cảnh báo:** nếu ai lưu thiết lập WP Rocket của site vi trước khi có tt-core, khách mobile của toàn site vi sẽ bắt đầu nhận bản desktop.
- **Vì sao 8 bài test không sinh lại bản mobile khi chưa có mu-plugin:** thư mục không còn `.mobile-active`, nên rocket-nginx trả thẳng `index-https.html` cho mọi khách, kể cả mobile. Request không tới PHP, nên WP Rocket không ghi bản mobile. Log 13:04–13:11 có request iPhone và Android vào các bài này, và thư mục vẫn chỉ có 2 file. Trạng thái này sẽ mất nếu một request mobile lọt vào PHP (file bị xóa, request có cookie hoặc query string). Mu-plugin, khi file config đã được tạo lại, là thứ giữ cho bền.

### Cập nhật gần như ngay khi sửa bài / có bình luận: mu-plugin ghi sự kiện (28/11, chủ site chọn)
Chủ site chọn cập nhật gần như ngay (INVENTORY mục 3.A.2), thay vì chờ `sync` 8 giờ/lần. **Đã viết, chưa cài lên VPS chính nào.**
- **Cách làm:** bản đơn giản hơn design mục 7.1. Không tạo bảng trong DB WordPress và không có REST; mu-plugin chỉ ghi file.
  - `wp/mu-plugins/dott-cache-events.php` (1.0.0): ghi `<thư mục site>/dott-cache-events/YYYYMMDD.jsonl`. Thư mục nằm cạnh `public_html` (ngoài web root, trong `open_basedir` của site; site user có quyền ghi, đã kiểm tra chỉ đọc).
  - Hook: `transition_post_status` (đăng, gỡ, sửa bài đang publish), `pre_post_update` / `wp_trash_post` / `before_delete_post` (chụp path cũ trước khi đổi slug hoặc gỡ), bình luận (`comment_post` đã duyệt, `transition_comment_status` vào/ra approved, `edit_comment`, `delete_comment`), `edited_term`, `pre_delete_term`, `after_rocket_clean_post`.
  - Path được tính lúc `shutdown`, khi slug và term đã lưu xong (Gutenberg lưu term sau `wp_update_post`). Cách chuẩn hóa giống `export-urls.php`.
  - Sửa bài: làm mới bài, trang 1 archive của term mới và term cũ, trang chủ. Đổi slug: gỡ URL cũ. Gỡ bài: gỡ URL, làm mới archive và trang chủ. Bình luận: chỉ làm mới bài.
  - Lỗi chỉ ghi `error_log`. Tắt nhanh: `define('DOTT_CACHE_EVENTS_OFF', true)`, hoặc xóa file.
- **Đọc sự kiện:** `wp/mu-plugins/dott-cache/events.php` (CLI, không nạp WordPress), gọi qua `dott-prefork <domain> --events --after=<mốc>`. Không chờ khóa prefork. Mốc `YYYYMMDD:offset`; chỉ trả dòng đã ghi trọn. Xóa file quá 14 ngày.
- **`dott-cache events -site X -blog /`** (VPS phụ, timer `dott-cache-events@vi` mỗi phút):
  - lần đầu chỉ lưu mốc;
  - lọc theo `-blog` và config `urls`; path trùng thì sự kiện sau cùng thắng;
  - lưu mốc **trước** khi xử lý (lỗi không làm lặp mãi; `sync` 8 giờ/lần vẫn là lưới an toàn);
  - gỡ: KV → server → S3 (hàm `removePaths` tách từ `sync`), đánh dấu `removed` trong DB; làm mới: `update -txt`;
  - đang có lệnh khác giữ khóa site thì bỏ lượt, không lỗi, lượt sau xử lý bù; quá `-max` 1000 URL thì chỉ báo.
- **Đã kiểm tra:**
  - `go vet` + build trên VPS phụ OK; `php -l` 2 file PHP OK; `bash -n` `dott-prefork` OK.
  - `events.php` test trên VPS phụ (cài `php-cli`, thư mục tạm): mốc init, đọc qua 2 ngày, dừng trước dòng ghi dở rồi đọc tiếp, `--max`, mốc sai, mốc vượt cỡ file, xóa file quá 14 ngày. **Tìm ra và đã sửa 1 lỗi:** PHP đổi key `"20261128"` thành số nguyên nên so `===` với mốc sai, đọc lại sự kiện cũ.
- **Chưa test:** mu-plugin trong WordPress thật, và `dott-cache events` đầu-cuối. Cần cài lên php81 (VPS chính), nên phải chờ chủ site duyệt.

### Đổi hướng: sự kiện đẩy qua tunnel + pool (28/11, chủ site chốt sau thảo luận)
- Chủ site không muốn kiểu hỏi file mỗi phút. Sau khi bàn: WordPress POST sự kiện, đi qua SSH reverse tunnel (giữ quy tắc bind `127.0.0.1`), tới service Go có pool lưu xuống file. Plugin mỏng (chỉ ID + URL cũ thô), Go hỏi trạng thái mới qua `export-urls --ids`.
- Lý do bỏ port public: tốc độ như nhau (render 200–600 ms/URL mới là nút cổ chai); VPS phụ giữ key root vào production nên không mở thêm cửa.
- Lý do không cho WordPress ghi thẳng KV: thứ tự S3 trước, KV sau; hai nơi cùng ghi KV gây race (bài đã gỡ sống lại); token Cloudflare phải nằm trong site.
- Đo (chỉ đọc): list toàn bộ KV production **46,9 s**; dùng được cho đối soát hằng ngày, không dùng cho từng sự kiện.
- Bản "events đọc file mỗi phút" (commit 6a8f2d1) bị thay. Thiết kế: [design-pool.md](design-pool.md).

### Pool: triển khai và test trên php81 (28/11 16:00–16:25, chủ site giao)
**Đã cài:**
- VPS chính:
  - `dott-prefork` thêm `--ids` (không chờ khóa); bỏ `--events`. Backup: `dott-prefork.bak-2026-11-28-1604`.
  - php81: `export-urls.php` có `--ids` (backup `.bak-2026-11-28-1604`); mu-plugin `dott-cache-events.php` 3.0.0 (mới); `/home/php81conZSI/php81.congtoan.net/dott-cache-hook.json` (600, token).
  - **Production diaocthongthai.com không đổi gì** ngoài `dott-prefork` dùng chung (chỉ thêm `--ids`); sau khi cài `/`, `/en/` đều 200, `--loadavg` chạy.
- VPS phụ: binary mới (backup `bin/dott-cache.bak-2026-11-28-1606`); `dott-cache-serve.service` đang chạy (**chưa enable**); `dott-cache-tunnel.service` đã cài nhưng **dừng** (xem chặn ở dưới); token `config/secrets/hook-php81.congtoan.net.token`.

**Chặn: tunnel chưa chạy được.** Key root của VPS phụ trong `authorized_keys` của VPS chính có `no-port-forwarding` (cùng `from="139.180.158.221"`), nên `ssh -R` bị từ chối. Không gỡ tùy chọn này. Đề xuất: thêm một key riêng cho tunnel với `restrict,port-forwarding,permitlisten="127.0.0.1:8790",permitopen="none",command="/bin/false",from="139.180.158.221"`. Cần chủ site duyệt (sửa cấu hình bảo mật production).

**Cách test khi chưa có tunnel:** mở listener tạm (`socat`, 30 s) trên `127.0.0.1:8790` của VPS chính để **bắt payload thật** của mu-plugin khi chạy `wp` trên php81, rồi gửi lại payload đó vào `serve` trên VPS phụ. Đã test đủ mọi phần trừ đoạn tunnel.

| Test | Kết quả |
|---|---|
| T0 tunnel tắt (không ai nghe cổng) | `wp post update` 1,34 s như bình thường, không lỗi |
| T1 sửa bài | payload có `old_url`, `old_terms`; làm mới 3 URL (bài, category, trang chủ); S3 cả 2 đích có `Last-Modified` mới. Lần này chờ load 47 s (load 1,53), xem sửa 2 |
| T2 bình luận | lần đầu **không có sự kiện** (`wp comment create` không kích `comment_post`), sửa 1; lần sau: làm mới bài, xong sau khoảng 7 s |
| T3 đổi slug | gỡ URL cũ (KV xóa, S3 2 đích hết), thêm URL mới (KV +1, S3 200); khoảng 11 s. T3b trả slug: ngược lại, đúng |
| T4 thùng rác | gỡ bài, làm mới category và trang chủ. T4b khôi phục: thêm lại, KV +1 |
| T8 bảo mật | token sai / thiếu: 403; GET: 405; `/submit` qua TCP: 404 (chỉ có trên unix socket) |
| T5 sự kiện chen giữa `submit -all` (1.226 URL `/tttest/`) | sự kiện sửa bài xong sau **20 s** trong lúc hàng loạt đang chạy; pool có 80 việc running = 2 lô (render và upload gối đầu) |
| T6 restart giữa chừng | pool giữ nguyên 905 việc rồi chạy tiếp. 40 việc của lô đang chạy bị tính lỗi (`context canceled`), xem sửa 3; T6b sau khi sửa: 120/120 ok, 0 retry |
| Hàng loạt | 1.226 URL trong 441 s, kể cả export 3,3 s và các lần nghỉ vì load (22–51 s mỗi lần). Khi load thấp: lô 40 URL render 3–6 s + upload 3 s; khoảng 8 URL/s. 1.228 ok, 0 bỏ. RAM serve tối đa 23 MB |
| T7 race: gỡ đúng lúc đang render | log: `superseded` (không PUT KV) → `removed`; KV 0, S3 2 đích hết. T7b trả lại: đúng |
| `--ids` | 1,2–2,3 s mỗi lô sự kiện (2 lần nạp WordPress khoảng 0,5 s) |

**Lỗi tìm ra và đã sửa:**
1. `get_post(0)` của WordPress trả bài toàn cục, nên `--ids` với ID 0 trả nhầm bài; ID ≤ 0 giờ là "không có".
2. Bình luận thêm bằng `wp_insert_comment` (WP-CLI, import) không kích `comment_post`: thêm hook `wp_insert_comment`.
3. Hủy do dừng service bị tính là lỗi (phải chờ 1 phút thử lại): giờ bỏ qua, việc trở về hàng đợi khi khởi động.

**Thay đổi theo kết quả đo:**
- Lô HTTP (≤ 4 URL, tức một lần sửa bài) **không chờ load**: tốn CPU như vài khách truy cập. Lô prefork vẫn điều tiết `max_load` 1,5.
- `timeout` của POST trong plugin: 2 s → 1 s. Khi bên kia nhận mà không trả lời, lệnh `wp` chậm thêm đúng bằng `timeout` (đo với listener không trả lời: thêm khoảng 2 s). Tunnel tắt thì không chậm.

**Chưa test:** đường thật WordPress → tunnel → serve (chờ key tunnel); phân trang thật (term có nhiều trang thay đổi số trang); sự kiện sửa/xóa term; nhiều site trong một serve; production.

Dữ liệu test để lại trên php81: excerpt của bài 150 có chữ "t0…t5"; 2 bình luận test đã vào thùng rác.

### Key riêng cho tunnel + test đường thật WordPress → tunnel → serve (28/11 16:26–16:30, chủ site duyệt)
- **VPS phụ:** key `/root/.ssh/id_dott_tunnel` (ed25519), alias `main-tunnel` trong `/root/.ssh/config` (backup `config.bak-2026-11-28-1626`), có `BatchMode yes`, `PasswordAuthentication no`.
- **VPS chính:** thêm dòng 2 vào `/root/.ssh/authorized_keys` (backup `authorized_keys.bak-2026-11-28-1626`):
  `from="139.180.158.221",restrict,port-forwarding,permitlisten="127.0.0.1:8790",permitopen="127.0.0.1:1",command="/bin/false"`.
  - Lần đầu dùng `permitopen="none"`: OpenSSH 8.2 không nhận nên **bỏ qua cả dòng key** (đăng nhập bị từ chối, log không ghi lý do). Đổi sang `127.0.0.1:1` (không có gì nghe).
  - Lần đầu client còn thử mật khẩu 2 lần, vì alias chưa có `BatchMode`. Đã thêm `BatchMode` để không gây báo động brute-force.
- **Kiểm tra giới hạn của key:** chạy lệnh: không (exit 1, không có output); `-L` tới cổng 22: bị chặn; `-R` cổng 8791: bị từ chối; `-R 0.0.0.0:8790`: bị từ chối; key cũ vẫn dùng được.
- **Tunnel chạy** (`dott-cache-tunnel.service`, chưa enable): VPS chính `127.0.0.1:8790` nghe; `/health` 200 sau 4,7 ms.

| Test (wp-cli trên php81, không có listener tạm) | Lệnh `wp` | Từ lúc lưu tới khi cache mới có trên server, S3 2 đích và KV |
|---|---|---|
| R1 sửa bài | 2,47 s | 12,7 s (làm mới 3 URL) |
| R2 bình luận đã duyệt | 1,07 s | 8,3 s |
| R3 đổi slug | 1,39 s | 12,7 s (gỡ URL cũ: KV và S3 2 đích hết; URL mới KV +1) |
| R3b trả slug | 1,45 s | 12,7 s |
| R6 sửa term | 1,65 s | 8,8 s |
| R5 tunnel tắt | 1,42 s | (sự kiện mất, như thiết kế) |

- Thời gian chia ra: gom sự kiện khoảng 5 s (`-debounce 3s`, cộng phần chờ tới lượt resolver), `--ids` 1,3–2,1 s, render HTTP 0,4–0,6 s, upload + KV 1,6–4,4 s. Muốn nhanh hơn thì giảm `-debounce`.
- Lệnh `wp` có tunnel và không có tunnel nhanh như nhau (1,1–2,5 s, gần như toàn bộ là thời gian nạp WordPress của wp-cli).
- Chưa test qua trình duyệt (admin sửa bài qua PHP-FPM, Gutenberg 2 request). `wp_remote_post` dùng chung mã nên dự kiến như wp-cli; cần xem khi bật production.
- Bình luận test đã vào thùng rác.

### Pool: phân trang, Gutenberg, đối soát hằng ngày (28/11 16:45–16:56, php81)
- **Phân trang thật** (category 15, 18 bài, 10 bài/trang):
  - đăng 3 bài: `/page/3/` được thêm (`phân trang mới`), KV +1, S3 2 đích 200;
  - xóa 3 bài lần đầu: `/page/3/` **không bị gỡ**. Nguyên nhân: kiểm tra "trang có hay không" bằng KV GET, mà key vừa PUT 12 s trước vẫn được KV trả 404 (KV đọc trễ tới khoảng 60 s).
  - Sửa: kiểm tra bằng S3 (đích đầu tiên; đọc ngay sau khi ghi là thấy). Chạy lại: `/page/3/` bị gỡ (`phân trang thừa → removed`), KV 0, S3 2 đích hết.
  - Cùng loại rủi ro ở bước PUT KV (key vừa xóa vẫn được GET trả "có", nên bỏ qua PUT): `serve` nhớ key vừa xóa trong 5 phút và luôn PUT lại.
  - Giới hạn đã biết: khi có bài mới, chỉ trang 1 và trang mới được làm mới. Các trang 2..N cũ (bài bị đẩy xuống) chờ lần `submit -all` hằng tháng, như tool cũ.
- **Gutenberg qua trình duyệt** (php81, phiên đăng nhập của chủ site trong browser pane): sửa excerpt bài 150, bấm Save. Trình soạn thảo gửi 2 request, nên `serve` nhận 2 sự kiện, gom thành 1 lô 3 URL; có trên S3 và KV khoảng 11 s sau khi bấm.
- **`dott-cache reconcile -site X -blog /`** (mới, không dùng `state.db`): cần có = export; đang có = KV + `index-https.html` trên mọi đích S3; thiếu → pool mức 3; mồ côi → pool gỡ. Chặn: export lỗi/rỗng, mồ côi > `max-pct` (2%), cần có < 50% số key KV.
  - php81 `/tttest/`: 1.226 URL, khớp 0/0; mất khoảng 5 s (export 3 s, list KV + 2 bucket 2 s).
  - Tạo lệch: xóa S3 bài 150 (thiếu) + xóa bài test lúc tunnel tắt (mồ côi): dry-run thấy đúng 1/1; `-max-pct 0.01` từ chối; chạy thật: 2 việc vào pool, xong sau 4 s; chạy lại: 0/0.
  - Production ước tính: list KV khoảng 47 s (đã đo), export 18 s.
- Unit `dott-cache-reconcile@.timer` (03:30 hằng ngày) thay `sync@` và `sync-full@`; `dott-cache-blog.sh` có thêm `reconcile`, `submit-all`. **Chưa cài.**

### P2 + P3 production site vi: theme generatepress_child v0.7.0 (tt-core) + sticky 3.2.0 (28/11 16:58, chủ site yêu cầu)
- **Kiểm tra trước (chỉ đọc):**
  - `functions.php` production trùng bản chụp 27/11 (gốc của v0.7.0): md5 `04f404df…`.
  - `thongthai-script.js` và `-mobile.js` production trùng bản chụp, nên module tt-core khớp code đang chạy.
  - Theme active: blog 1 `generatepress_child`, blog 2 `generatepress_en`. Plugin network 3.0.0.
- **Backup:** `/root/backup-p2-2026-11-28/generatepress_child.tgz` và `ct-multi-sticky-sidebar-3.0.0.tgz`. Để ngoài `wp-content`, vì một thư mục theme bản sao sẽ hiện như một theme khác.
- **Theme:** lấy từ tag v0.7.0 (`git archive`, không đổi xuống dòng), chỉ thêm `tt-core/` (200 file) và `LEGACY-FILES.md`, thay `functions.php`. File cũ giữ nguyên. chown site user, 755/644, `php -l` đạt.
- **Plugin:** zip release v3.2.0 → `rsync --delete` vào `ct-multi-sticky-sidebar`; 644/755, `php -l` mọi file đạt.
- **Build blog 1:** `lss_generate()`, rồi `wp tt-core build`: ver `62d9b17e24`, 51.871 byte (gốc 91.774), minified, checked, **19 module** gồm `600 ct-multi-sticky-sidebar desktop 3.2.0-… plugin yes`. Blog en không build (theme en chưa có tt-core).
- **Kiểm tra:**
  - Ngay sau khi chép: mọi trang 200, `wp-login` 302. Bản render mới vẫn dùng JS cũ (OPcache giữ `functions.php` cũ), đúng đường lui.
  - Sau 3 phút: bài và trang chuyên mục (`?djtprobe=`) với desktop và iPhone đều chỉ có `tt-core/core.js?ver=62d9b17e24`, cùng kích thước HTML. Khác nhau đúng 1 dòng honeypot (`wpa_field_info`, ngẫu nhiên mỗi lần render).
  - en: như cũ (desktop `lss-2.js`, iPhone không).
  - Trình duyệt (phiên admin của chủ site):
    - desktop 1366 px: `TT.has('ct-multi-sticky-sidebar') = true`, 18 module chạy, `lss-1.js` được tải, cuộn 2.500 px thì widget cột phải vẫn trong khung (sidebar dính);
    - 888 px: không tải `lss` (`minWidth` 901, đúng);
    - mobile 375 px: `TT.mobile = true`, 12 module mobile/all, không tải `lss`, không tràn ngang;
    - không có lỗi console; error log PHP không có lỗi mới.
  - Trang chuyên mục vẫn tải `lss-1.js`: **đúng với production** (`disableOn.category = false`; cache cũ do 3.0.0 tạo cũng có `lss-1.js`). Câu "vi tắt sticky ở chuyên mục" trong DEPLOY.md của plugin là thiết lập của php81.
- **Chưa đổi:** file `wp-rocket-config` (P4). WP Rocket vẫn tách cache mobile; HTML cache cũ vẫn trỏ JS cũ (giữ theo `LEGACY-FILES.md`). Bản cache mới (desktop và mobile) dùng `core.js`.
- **Quay lại:** giải nén 2 file backup đè lên, rồi đợi 3 phút OPcache.

### P4 production site vi: tạo lại config WP Rocket blog 1, cài export-urls + replace-cache (28/11 17:05, chủ site yêu cầu)
- **Backup:** `/root/backup-p4-2026-11-28/wp-rocket-config/` (cả thư mục) và `mu-dott-cache/`.
- **mu-plugins/dott-cache:** cài `export-urls.php` (có `--ids`) và `replace-cache.php`; `php -l` đạt; `--export-urls --ids=1:p0` chạy (boot 0,8–1,0 s, 3 blog `/ /en/ /es/`).
- **Tạo lại config blog 1** (`wp --url=https://diaocthongthai.com/ eval 'rocket_generate_config_file();'`). Option trước đó: `cache_mobile=1`, `do_caching_mobile_files=0` (do mu-plugin `dott-single-cache`).
  - Chỉ `diaocthongthai.com.php` đổi; `diaocthongthai.com.en.php` không đổi (md5 như cũ).
  - So dòng với bản 09/03:
    - `$rocket_do_caching_mobile_files` 1 → 0 (mục tiêu);
    - thêm 20 tham số theo dõi được bỏ qua (`hsa_*`, `_hsenc`, `mtm_kwd`…: mặc định mới của WP Rocket, URL có các tham số này vẫn dùng cache);
    - `$rocket_cache_query_strings` `['q']` → rỗng (thiết lập admin đã rỗng từ lâu, chỉ chưa tạo lại file).
  - Đánh giá `?q=`: log hôm nay 1.235 lượt `/search/?q=`, **1.185 URL khác nhau**, 1.181 là bingbot. Cache cho `?q=` hầu như không trúng (khoảng 4%), nên bỏ không làm tăng tải đáng kể. Giữ nguyên.
- **Kiểm tra** (sau 3 phút OPcache; file config được `advanced-cache.php` nạp):
  1. Thư mục còn `.mobile-active` (`/cac-thanh-pho-cua-my/`): iPhone nhận **bản desktop** (`cached@` của file desktop) qua PHP (nginx BYPASS). Đây là HTML cũ, JS desktop cũ: khách mobile của các trang này nhận JS desktop cho tới khi cache được tạo lại (P6).
  2. Xóa cache 1 bài (`/thanh-pho-huntington-bang-west-virginia/`), request iPhone đầu tiên: chỉ tạo `index-https.html` + `_gzip`, **không có file mobile, không có `.mobile-active`**. Lần sau iPhone và desktop đều nginx **HIT** cùng file, có `tt-core/core.js`.
  3. en (`/en/abkhazia-maps/`): iPhone vẫn nhận file mobile (md5 khác desktop; 2 file cùng kích thước nhưng khác nội dung). Không đổi.
  4. Error log PHP: không có lỗi mới.
- **Quay lại:** chép `/root/backup-p4-2026-11-28/wp-rocket-config/diaocthongthai.com.php` đè lên, đợi 3 phút. Lưu thiết lập WP Rocket trong admin site vi sẽ tạo lại file theo option hiện tại (`do_caching_mobile_files=0` chừng nào mu-plugin còn bật cho blog 1).

### P5 production: xóa cron cũ, dừng screen (28/11 17:10, chủ site yêu cầu "dừng hết và xóa cron")
- Backup `/root/backup-p5-2026-11-28/crontab-root.txt`. Xóa 2 dòng `cron-cache-clean-v2.py` (10 phút sau các giờ 0,4,…,20) và `cron-cache-segment-update-v2.py` (3:45, 15:45). Cả hai chỉ xử lý URL vi (danh sách `clean_*.txt` 1.074 URL vi; segment viết cứng URL vi).
- Dừng lượt `cron-cache-clean-v2.py` đang chạy (từ 16:10). Đóng screen `en-map-of` (tool đã xong 27/11 16:31: 33.077/33.079, 2 lỗi), `F1`, `F2` (chỉ còn shell).
- Giữ cron "Listing cache" của theme (2:20, 10:20, 18:20): không thuộc hệ cache.

### P6 production site vi: update -all -blog / (28/11 17:12, chủ site yêu cầu)
- Config VPS phụ `write_blogs: ["/"]` (backup `config/sites/diaocthongthai.com.json.bak-…`). `urls sync` toàn bộ: 61.170 URL, không thêm, không mất (18 s).
- Kiểm kê: 24.273 URL blog 1; S3 72.089 file kiểu cũ + 38 file mồ côi; KV 9 key mồ côi; server 66.549 file mobile + 222 file mồ côi (111 path, 0,46%); 2.022 URL chưa có cache.
- Chạy bằng `systemd-run --unit=dott-p6-vi` (MemoryMax 400M, CPUQuota 50%); log `logs/diaocthongthai.com/p6-update-all-vi.txt`.
- Dọn: KV 9 key; server 66.549 mobile + 222 mồ côi (11 s); S3 72.127 object (41 s), không lỗi.
- Render: lô 200 URL khoảng 72 s (2,8 URL/s), lô đầu nghỉ 90 s vì load. Thứ tự (đo trên log): trang chủ `/` (trang tĩnh), 21.293 bài, 792 archive trang 1, 2.188 phân trang.
- Kiểm tra một URL đã render (`/ban-do-an-do/`): server chỉ còn `index-https.html` + `_gzip`; nginx HIT cùng bản cho desktop và iPhone, có `core.js`; S3 chỉ còn HTML mới (mobile, `_gzip` 403).
- **Kết quả cuối (xong 20:18, tổng 10.961 s, khoảng 3 giờ 3 phút, 2,2 URL/s):**
  - 24.272/24.273 URL ok (fetch nguồn HIT 24.271, MISS 1). KV thêm 488 key còn thiếu, lỗi 0. Unit thoát mã 1 vì còn 1 URL.
  - Điều tiết: nghỉ trước lô 4 lần (238 s), PHP nghỉ 82 lần (3.034 s, khoảng 28% thời gian), load cao nhất trước lô 2,45.
    Phần archive và phân trang ở cuối nhanh hơn bài (lô cuối 7–9 URL/s).
  - URL lỗi: `/cong-bo-du-an-the-western-capital/` (bài `ban_tin_du_an` ID 2939, publish) bị **Rank Math redirect 301 về
    trang chủ**, nên lần nào render cũng lỗi. Cần chủ site chọn: bỏ redirect, hoặc thêm vào `exclude` của config.
  - Lần lỗi khác: 1 URL lô 10 (wpdatatables ghi `tempfile136.csv`, không có quyền ghi), thử lại bằng HTTP thì đạt.
  - Kiểm tra sau:
    - `reconcile -blog / -dry-run` (20:31): cần có 24.273, KV 24.273, S3 24.272; thiếu 1 (URL redirect ở trên), **mồ côi 0**.
    - `/`, `/ban-do-an-do/`, 6 URL ngẫu nhiên trong log (gồm `/xa-phuong-viet-nam/page/354/`,
      `/ban-do-cac-bang-cua-my/page/2/`): desktop và iPhone đều nginx HIT, cùng md5, có `core.js?ver=62d9b17e24`.
    - S3: `index-https.html` 200; `index-mobile-https.html`, `_gzip` 403 (không còn).
    - Server sau khi chạy (dry-run): không còn file mobile; 7 path mồ côi (14 file) xuất hiện trong lúc chạy, chưa xóa.
    - S3 còn "2 file khác" (không phải `index-https.html`) của blog `/`, chưa xác định tên (VPS phụ không có boto3). Không ảnh hưởng
      phục vụ: Worker chỉ đọc `index-https.html`.

### P7 production: Worker v2 thay v7 (28/11 17:45, chủ site yêu cầu "deploy ngay")
- Lý do phải làm ngay: v7 chọn file theo `Accept-Encoding` và User-Agent, nên gần như luôn đọc `index[-mobile]-https.html_gzip`. P6 đã xóa các file này của site vi, nên với v7 mỗi trang vi = KV + một lần gọi S3 hỏng + origin.
- **Cloudflare (chỉ đọc trước):** script `thongthai-cache` trên route `diaocthongthai.com/*` (`cdn.diaocthongthai.com/*` → `thongthai-cdn`, không đụng). v7: một binding `THONGTHAI_CACHE` (namespace `cc831…`), compat 2025-09-06.
- **Bản quay lại:** mã v7 đang chạy được tải về `test/p7/v7-live/worker.js` (9.043 byte). `bash deploy/worker/deploy-production.sh rollback` upload lại v7 với binding cũ.
- **Deploy:** `deploy-production.sh deploy`: upload v2 đè lên `thongthai-cache` (route giữ nguyên, không cần quyền Routes): `CACHE` = `cc831…`, `REDIRECTS=1`, `LEGACY_PREFIXES=["/en/"]`, `TARGETS` NA + SA = bucket Chicago `cdn.diaocthongthai.com`, `TEST_KEY` (secret), compat 2026-11-01.
- **Test qua Cloudflare:**
  - vi NA desktop / NA iPhone / SA: `S3-NA` / `S3-NA` / `S3-SA`, cùng một bản (`core.js`);
  - châu Á (VPS phụ), châu Âu giả lập: `ORIGIN-region-AS/EU`; header giả lập với key sai: bị bỏ qua;
  - cookie đăng nhập: `ORIGIN-cookie`; query string: `ORIGIN-query`; chữ hoa: 301 về chữ thường;
  - en: NA desktop `S3-NA-legacy`, NA iPhone `S3-NA-legacy-mobile` (như v7);
  - `/tag/` → `/tags/` 301, `link_ds_*` 410, ảnh uploads → `cdn.diaocthongthai.com` 301, `/map-*` → `/en/map-*` 301, `wp-login.php` và `/en/wp-json/` đi origin (`ORIGIN-bypass`).
  - POST `admin-ajax.php` qua Cloudflare: 403 do **trang chặn của Cloudflare** (WAF/bot, trước Worker); thẳng origin: 200. Không liên quan Worker.
- Chưa đo: tốc độ thực tế cho khách Việt Nam (v7 cho mọi châu lục lấy từ Chicago; v2 cho châu Á vào origin, nơi nginx trả thẳng file cache).

### Lần 2 site en, bước 1–4: theme tt-core, một bản cache, Worker bỏ LEGACY_PREFIXES (28/11 18:14–18:33, chủ site duyệt cả chuỗi)
- Phương án tạm `legacy_blogs` (pool ghi en kiểu tách mobile) **bỏ, không deploy** (commit revert `96dfbd9`): en chuyển hẳn trong ngày. Lỗi chủ site chỉ ra: `--delete-only` xóa `.mobile-active`, lấy bản desktop trước, nên request iPhone sau đó có thể bị nginx trả bản desktop.
- **Bước 1, theme `generatepress_en` v0.7.0** (tag `9102122`):
  - Kiểm tra trước (md5 sau khi bỏ `\r`): `functions.php` `433782e8…`, `thongthai-desktop.js` `2d3a3183…`, `thongthai-mobile.js` `60aba364…` trùng bản chụp `theme/tools/legacy-2026-11-27/generatepress_en/`; `thongthai-script.js` trùng tag. Theme chưa có `tt-core`.
  - Backup `/root/backup-en-2026-11-28/generatepress_en.tgz` (26 mục, ngoài `themes/`). Gói `git archive` (`core.autocrlf=false`): `functions.php`, `LEGACY-FILES.md`, `tt-core/` (190 file), md5 gói khớp 2 đầu.
  - `php -l` mọi file PHP trước khi chép; `functions.php` ghi `.new` rồi `mv` (md5 mới `f3efd6c1…`). Chỉ chown/chmod file mới (site user, 755/644); các file cũ thuộc root giữ nguyên chủ sở hữu.
  - Build blog 2: `lss_generate()`, `wp --url=…/en/ tt-core build`: ver `1cc3bb7b7f`, 20.510 byte (gốc 38.491), minified, checked, **9 module** gồm `600 ct-multi-sticky-sidebar desktop 3.2.0-1795864604 plugin yes`. Blog 1 vẫn `62d9b17e24`.
- **Bước 2, test theme en** (sau 3 phút OPcache):
  - `/en/`, 4 bài, 1 chuyên mục: 200; `wp-login` 302; trang vi 200.
  - `?djtprobe=` desktop và iPhone (6 URL): chỉ có `tt-core/core.js?ver=1cc3bb7b7f`, không còn `thongthai-*.js` trong HTML; hai bản khác đúng 2 dòng `wpa_field_info`.
  - Trình duyệt (phiên admin của chủ site): 1366 px `TT.has('ct-multi-sticky-sidebar') = true`, tải `lss-2.js`; cuộn 1.500 px thì sidebar `top = 20` (`marginTop`), 3.000–3.500 px dừng ở đáy `#content` (3.613 / 3.615). Trang chủ `/en/` không tải `lss` (`lss_settings` blog 2: `disableOn.home = true`). 375 px: `TT.mobile = true`, không tải `lss`, không tràn ngang.
  - Console chỉ có lỗi AdSense `adsbygoogle.push() error: No slot size for availableWidth=0`; tràn ngang ở 1366 px là iframe quảng cáo `aswift_7` (1.200 px). Không so được với JS cũ (đã thay), nên chưa khẳng định lỗi này có từ trước.
  - Error log PHP không có dòng mới. nginx `error.log` chỉ có 404 file của plugin `lightbox-photoswipe` (đã không còn) do bot gọi, có từ trước 18:00.
- **Bước 3, một bản cache en:**
  - `dott-single-cache.php` (backup `.bak-2026-11-28`): `array( 1 )` → `array( 1, 2 )`, `php -l` đạt. Option blog 2 qua filter: `cache_mobile=1`, `do_caching_mobile_files=0`.
  - Backup `/root/backup-en-2026-11-28/wp-rocket-config/`. `rocket_generate_config_file()` cho blog 2. So dòng `diaocthongthai.com.en.php`: `do_caching_mobile_files` 1 → 0; thêm 20 tham số theo dõi (như vi); `cache_query_strings ['q']` → rỗng (như vi; option admin blog 2 đã rỗng, hôm nay 0 lượt `/en/…?q=`). File vi không đổi (md5 `5894d607…`).
  - Test (sau 3 phút): xóa cache `/en/africa-map/` (5 file); iPhone lần đầu `MISS` chỉ tạo `index-https.html` + `_gzip`; sau đó iPhone và desktop đều nginx `HIT` cùng md5 với file trên đĩa, có `core.js`. Trang còn cache cũ (`/en/abkhazia-maps/`, còn `.mobile-active`): iPhone nhận bản desktop cũ qua PHP (`BYPASS`), như vi ở P4.
- **Bước 4, Worker:** `deploy-production.sh` bỏ binding `LEGACY_PREFIXES` (script trên VPS phụ ở `test/p7/`, backup `.bak-2026-11-28-1830`); mã Worker không đổi. Test qua Cloudflare (giả lập châu lục bằng test key):
  - en `/en/abkhazia-maps/`, `/en/africa-map/`, `/en/`: NA desktop và NA iPhone đều `S3-NA`, cùng md5; SA `S3-SA`; EU, châu Á `ORIGIN-region-*`.
  - vi `S3-NA` như cũ; redirect `/tag/`, `/map-*`, `/en/page/2/`, ảnh uploads 301; `link_ds_*` 410; `/en/wp-json/` `ORIGIN-bypass`; chữ hoa 301; query `ORIGIN-query`; key sai bị bỏ qua.
  - Từ lúc này tới khi bước 5 render lại, khách NA/SA dùng mobile nhận bản desktop cũ trên S3 (S3 en còn bản 21/11).
- **Quay lại:** theme: giải nén `generatepress_en.tgz` đè lên và xóa `tt-core/`, `LEGACY-FILES.md`; mu-plugin: chép `.bak-2026-11-28`; config: chép `wp-rocket-config/diaocthongthai.com.en.php` từ backup (đợi 3 phút); Worker: `deploy-production.sh.bak-2026-11-28-1830 deploy` (có `LEGACY_PREFIXES`).
- **Bước 5** (tạo lại cache en), 20:33: config VPS phụ `write_blogs: ["/", "/en/"]` (backup `config/sites/diaocthongthai.com.json.bak-2026-11-28-2033`); `urls sync`: 61.170 URL, không thêm/mất.
  - Kiểm kê `update -all -blog /en/` (không `-yes`): 36.897 URL; S3 `index-https.html` 36.893, file kiểu cũ 110.680 (sẽ xóa), mồ côi 0; KV 61.166 key, mồ côi 0; server 113.861 file mobile, **2.468 path mồ côi (6,7%, vượt ngưỡng 2%) → tool dừng, chưa ghi gì**.
  - Mồ côi: 2.464 là `/en/map-of-us-cities-and-places/page/2..2465/` (có thật: `/page/2465/` 200, `/page/2466/` 404; cache 05–12/11); còn lại `/en/search/` (2), `/en/secretlogin/`, 1 bài có `'` trong slug. Danh sách: VPS phụ `test/en-orphans-2026-11-28.txt`.
  - **Nguyên nhân: lỗi `export-urls.php`** (dòng 311, và dòng 186 của `--ids`): số trang archive = `ceil(term->count / posts_per_page)`, mà `term->count` chỉ đếm bài gắn trực tiếp. Archive chuyên mục cha hiện cả bài chuyên mục con, nên thiếu trang. Đo (WP_Query `include_children`, post type `any`):
    - en `map-of-us-cities-and-places`: count 0, gồm con 29.574 → thiếu 2.464 trang (khớp đúng số mồ côi);
    - vi `tu-lieu`: count 0, gồm con 794 → 67 trang (`/page/67/` 200, `/page/68/` 404), thiếu 66;
    - vi `ban-tin`: count 0, gồm con 124 → tính 11 trang nhưng `/page/11/` 404: cách đếm `any` dư, bản sửa phải đếm đúng như truy vấn chính của archive.
    - Hệ quả với vi: 76 trang phân trang này không có trong DB. P6 đã coi chúng là mồ côi (nằm trong 111 path đã xóa trên server), nên giờ không có trên S3/KV; khách vẫn xem được (Worker `kv-miss` → origin, WP Rocket tạo lại khi có lượt xem).
  - Dừng, chờ chủ site chọn cách xử lý. Chủ site chọn **sửa ngay** rồi mới chạy en.
- **Sửa `export-urls.php`** (20:40–20:53, chủ site duyệt thay trên production):
  - Đo trước (chỉ đọc), 3 cách đếm số trang cho chuyên mục cha, so với HTTP thật (trang cuối 200, trang sau 404):
    A `ceil(count/ppp)` sai cả 3; B `WP_Query include_children, post_type any` sai `ban-tin` (11, thật 10);
    **C dựng truy vấn chính** (đặt tạm `$wp_the_query`, nên hook `pre_get_posts` của theme chạy, vd
    `include_custom_post_types_in_category_archives` chèn CPT vào archive) đúng cả 3.
  - C cho mọi term: vi 788 term 1,6 s, en 68 term 0,8 s. Chỉ 3 term lệch so với A (đều là chuyên mục cha có con, đều thiếu), mọi tag/cdt/khu-vuc khớp.
  - Sửa: hàm `ex_term_pages()` (cách C) dùng cho export toàn bộ và `--ids`; `--ids` trả thêm **chuyên mục tổ tiên** của term gắn với bài (trước đây sửa bài ở chuyên mục con không làm mới trang chuyên mục cha); log `#` liệt kê term có số trang khác `ceil(count/ppp)`.
  - Chạy thử bản mới từ `/tmp` (chỉ đọc DB): 68.196 URL so với 65.657, **+2.539 path, không mất path nào** (vi `ban-tin` +9, `tu-lieu` +66; en `map-of-us-cities-and-places` +2.464); 10,1 s so với 12,9 s. `--ids` cho bài ở chuyên mục con: thêm dòng chuyên mục cha với đúng số trang (2.465 / 67 / 10), các dòng khác như cũ.
  - Thay: md5 cũ `672208c4…` → mới `55538bb0…` (khớp repo). Backup `export-urls.php.bak-2026-11-28` (cạnh file) và `/root/backup-en-2026-11-28/export-urls.php.bak`. **Chưa cập nhật php81.**
  - `urls sync`: thêm 2.539, mất 0. DB: `tu-lieu/page/` 66, `ban-tin/page/` 9, `map-of-us-cities-and-places/page/` 2.464.
  - **Kiểm kê en lại:** 39.361 URL; S3 36.893 `index-https.html`, 110.680 file kiểu cũ sẽ xóa, mồ côi 0; KV mồ côi 0; server 113.861 file mobile, **mồ côi 4 path (0,01%)**.
  - **Render 75 trang vi còn thiếu** (`update -txt lists/diaocthongthai.com/vi-missing-pages-2026-11-28.txt -blog / -yes`): 75/75 ok, 58 s, KV +75. `/tu-lieu/page/67/`, `/ban-tin/page/10/`: nginx HIT, S3 200.
- **Chạy thật en** từ 20:57: `systemd-run --unit=dott-update-en --collect` (MemoryMax 400M, Nice 10, CPUQuota 50%), log `logs/diaocthongthai.com/update-all-en-20261128-2057.txt`, 197 lô. Xem tiến độ: `bash test/en-status.sh` (VPS phụ). Kết quả: xem mục sau khi chạy xong.

### Việc không liên quan nhưng cần biết
- `tmp_update_en_map_of.py` (screen `en-map-of`) lúc 13:47 ngày 27/11 đã chạy tới 30.612/33.079 URL, có 2 lỗi. Không bị đụng tới.
