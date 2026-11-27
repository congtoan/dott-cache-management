# HANDOFF – Cache System v2 (cập nhật 2026-11-27 tối)

> Điểm bắt đầu cho hội thoại mới. Đọc file này, rồi tới [docs/HISTORY.md](docs/HISTORY.md) (lịch sử, các tag, những điều từng hiểu sai), [docs/design-server-only.md](docs/design-server-only.md) và [docs/rollout-log.md](docs/rollout-log.md).
> Tổng kết đợt prototype 27–28/11 kèm số đo: [docs/SUMMARY-2026-11-27.md](docs/SUMMARY-2026-11-27.md).
> Mọi thứ cũ hơn (pipeline Python, PC) đã chuyển vào `legacy/`. Chỉ xem lại khi cần tham khảo logic.

## 1. Bối cảnh trong 1 phút

| | VPS chính (production) | VPS phụ `claude-control` |
|---|---|---|
| IP / SSH | 45.77.32.177 : **8282**, root (mật khẩu, trong skill `vps-diaocthongthai`) | 139.180.158.221 : 22, **chỉ key**: `ssh claude-control` từ PC |
| Máy | 1 vCPU, 2 GB RAM (swap cao), ổ khoảng 87%, Ubuntu 20.04, HostVN | 1 vCPU, 1 GB RAM, Ubuntu 26.04, **Go 1.26.8** đã cài |
| Vai trò | nginx, PHP 8.1-FPM, MariaDB 10.5, Redis, **WordPress multisite** (blog 1 vi `/`, blog 2 en `/en/`; `/es/` bỏ qua), WP Rocket 3.16 | Claude Remote Control 24/7 (**không đụng `claude-rc`**), giám sát `/root/health` (23:45, Telegram), sẽ chạy service `dott-cache` (Go) |
| Đi sang máy kia | — | `ssh main "..."` (key giới hạn IP, **không mở tunnel được**), `main-ro <lệnh>` (chỉ đọc) |

Ping giữa 2 máy: **0,43 ms** (cùng datacenter Singapore).

**Đường đi của cache hiện tại:**
- WP Rocket ghi 4 file mỗi URL trên đĩa VPS chính, ở `.../wp-content/cache/wp-rocket/diaocthongthai.com/<path>/`: `index-https.html`, `index-https.html_gzip`, `index-mobile-https.html`, `index-mobile-https.html_gzip`.
- Các file này được upload lên S3 Linode **Chicago** `us-ord-1`, bucket `cdn.diaocthongthai.com`, prefix `_cache_html/`.
- Cloudflare KV `THONGTHAI_CACHE` (namespace `cc831ca12e954d0b8a875a3453abacc7`) có key = path chuẩn hóa (lowercase, `/` cuối). Hiện có **60.687 key**, khớp DB.
- Worker `thongthai-cache` (nguồn: `D:\Draft\2025-09-07 CF WORKER.JS\thongthai-cache-v7.js`): **chỉ khách Bắc/Nam Mỹ** được phục vụ từ S3; còn lại vào origin.

Header S3 phải giữ khi upload: `ContentType: text/html`, `CacheControl: public, max-age=31536000`, `ACL: public-read`, thêm `ContentEncoding: gzip` cho file `_gzip`.

## 2. Đang chạy gì trên VPS chính (cache cũ)
- **Không có renew nền nào chạy.** Screen `F1` chỉ còn một bash rảnh (`cache_renew_from_db.py` đã dừng, không rõ từ lúc nào). `en-map-of` đã **xong** 27/11 16:31 (32.879 OK, 2 lỗi).
- Cron cũ vẫn bật: `cron-cache-clean-v2.py` (0,4,8,…h) và `cron-cache-segment-update-v2.py` (3:45, 15:45).
  - Cả 2 **không upload S3** (F2).
  - segment-update **không có timeout**, có thể treo lại. 3 tiến trình treo đã được kill 27/11.
- SQLite `/root/tool/diaocthongthai.com/cache-tools/output/cache_urls.db` (60.687 URL) là nguồn URL của tool cũ. v2 sẽ import một lần.
- Plugin `wp-rocket-no-cache-auto-purge` network-active: **WP Rocket không tự xóa cache khi sửa bài**.

## 3. Quyết định của chủ site (đã chốt)
- Hướng **v2 = chia 2 máy, Go trên VPS phụ** (design v2). Triển khai **từng giai đoạn**, test kỹ, chạy dry-run/chỉ đọc trước. **Mọi thao tác ghi trên VPS chính đều hỏi trước**, backup trước.
- Báo cáo qua **Telegram** (dùng chung bot với `/root/health`).
- "Clear all cache": mặc định chỉ xóa trên server, **không xóa S3**.
- **Không** upload HTML lên bucket Singapore. Bỏ qua `/es/`.
- **Không dùng fail2ban cho web.** Site đi qua Cloudflare nên iptables ban IP vô tác dụng. nginx lấy IP thật qua `real_ip_header X-Forwarded-For` + `/etc/nginx/extra/cloudflare.conf`. Jail `sshd` vẫn nên giữ.
- Repo `congtoan/dott-cache-management`, nhánh `v2` (thư mục này). Commit theo git config sẵn có. Tên **Cong Toan** + email noreply là quy ước cho 2 repo plugin public.

## 4. Đã làm và đã đo (chi tiết trong rollout-log + findings)

**Giai đoạn 0 (gần xong):**
- Tag `v0-legacy`, nhánh `v2`. Go trên VPS phụ.
- Kiểm tra chứng chỉ origin bằng **SPKI pin** `oqhm5KRTcPw6B6FNpgTMxww20FoxF3uoY1oBeD+TSAM=` (Cloudflare Origin cert, hạn 2038; đã test bằng Go).
- Kill 3 tiến trình treo.
- **Còn lại:** chủ site tự tạo **S3 key mới** (chỉ quyền trên bucket) và **CF token mới** (chỉ KV Edit). Cần trước giai đoạn 4.

**Giai đoạn 1 (xong):**
- Lấy `.html` qua HTTP từ origin **khớp md5 với đĩa 40/40**.
- nginx **không** trả file `_gzip` mà nén lại lúc gửi. Vì vậy v2 lấy `.html` không gửi `Accept-Encoding`, rồi VPS phụ tự gzip.

**Đo hiệu suất** ([docs/findings-php-render-2026-11-27.md](docs/findings-php-render-2026-11-27.md)):
- Render PHP thật mất **0,2–0,35 s/trang** (trang chủ khoảng 0,6 s). Trong đó bootstrap (50 plugin) **khoảng 180 ms**, tức 75–85%. DB 0–14 ms. `rocket_buffer` khoảng 2 ms (tối ưu WP Rocket gần như tắt hết).
- Tool cũ chậm (7,8 s/URL/worker) chủ yếu vì upload S3 tuần tự và chặn 1 s kiểm tra CPU, **không phải vì PHP**.
- Ước tính renew 60k URL: tool cũ khoảng 65 giờ; **v2 khoảng 9 giờ**; v2 + prefork khoảng 2–3 giờ.
- **Prefork chạy được**, HTML giống bản HTTP (chỉ lệch giá trị ngẫu nhiên của honeypot và timestamp WPForms). Script: `tools/lab-2026-11-27/fork.php`.
- **Cloudflare tự nén** (brotli/gzip) response do Worker tạo, kể cả khi Worker trả `.html` không nén. Vì vậy `_gzip` trên S3 không bắt buộc.
- Bản desktop và mobile chỉ khác vài dòng (theme nạp `thongthai-desktop.js` thay vì `thongthai-mobile.js`, sticky sidebar). Nếu gộp thành một bản HTML thì giảm một nửa số lần render và số file.
- Ý tưởng "Go trung gian trả kết quả có sẵn cho WordPress" **không đáng làm**: chi phí nằm ở PHP nạp 50 plugin, và dữ liệu đã có trong Redis local.
- Query Monitor đang bật trên production (drop-in `db.php` chạy ở mọi request). Các plugin dev/admin cũng vẫn bật thường trực.

## 4b. tt-core: một file JS cho desktop và mobile (bản nháp 27/11)
- Thiết kế: [docs/design-tt-core.md](docs/design-tt-core.md). Code: [theme/](theme/) gồm engine, 18 module vi, 8 module en, và `functions.php` mới có đường lui.
- `node theme/tools/verify-parity.js`: module ghép lại **khớp JS cũ** theo từng thiết bị. Chỉ lệch 2 dòng có chủ đích ở theme vi (`xemthem`, `topOffset`). Chạy thử desktop, mobile và iPad đều đạt.
- **Chưa làm:**
  - `engine/vendor/` đã có: Peast v.1.18.2, minify 1.3.76, path-converter 1.1.3.
  - **Đã test trên php81.congtoan.net** (site 3 `/tttest/` và site 4 `/tttest-en/`): build, minify, desktop/mobile, lỗi cú pháp, rollback, đường lui đều đạt. Dashboard trên trình duyệt cũng đã thử: Kiểm tra cú pháp, Lưu và Generate, Quay về bản trước đều đạt. Chi tiết ở design-tt-core mục 11b.
- Sticky sidebar: chủ site đã cập nhật plugin lên **3.2.0** (dùng module tt-core) và đã kiểm tra trên php81. Còn để sau: preload LCP trang chủ, wpDataTables.

## 4c. Tối 27/11: một bản HTML, rocket-nginx, Go uploader, prefork (chi tiết: rollout-log, design 5b–5c, findings 4b)
- **Đính chính:** trước đây **không có rocket-nginx**. File cache do `advanced-cache.php` (PHP) trả. Response render mới **cũng có `Last-Modified`**, nên dấu hiệu đúng của file cache là dòng `cached@`.
- **rocket-nginx đã bật** (`/etc/nginx/extra/rocket-page-cache.conf`, nguồn `deploy/nginx/`) cho diaocthongthai.com và php81. Backup `*.conf.bak-2026-11-27`.
  - Thư mục còn `.mobile-active` thì nginx bỏ qua, nên trên production **chưa có tác dụng** cho tới khi chuyển sang một bản HTML.
  - Khi đã là một bản HTML: nginx trả thẳng file trong 5–13 ms.
- **Một bản cache trong WP Rocket 3.16:** không có nút tắt trong giao diện; **không được đặt `cache_mobile` = 0** (khách mobile sẽ không được phục vụ cache). Cách dùng: mu-plugin `wp/mu-plugins/dott-single-cache.php` ép `cache_mobile` = 1, `do_caching_mobile_files` = 0. Đã cài trên php81, **chưa cài production**. `advanced-cache.php` dùng chung cho cả mạng multisite.
- **php81.congtoan.net là môi trường test:**
  - Site 3 `/tttest/` (vi, 1.000 bài thật) và site 4 `/tttest-en/` (en, 218 bài `map_*`), cùng plugin và thiết lập lấy từ production (bỏ license), cấu hình ACF, `WP_DEBUG_DISPLAY` = false.
  - **Không có Redis**, nên render chậm hơn production.
- **Bucket test** `congtoanphp` (us-ord-1, eu-central-1). Key S3 nằm ở `/etc/dott-cache/s3.env` trên VPS phụ (600; Claude trên VPS phụ bị cấm đọc).
- **Go uploader thử** `/opt/dott-cache/bin/dott-cache upload` (code: `cmd/`, `internal/`):
  - 1.024 URL × 2 bucket: **42,9 URL/s** với 16 luồng mỗi đích khi đã có cache, 0 lỗi, md5 khớp đĩa.
  - Khi phải render qua HTTP: 1,1 URL/s.
- **Prefork ghi cache thật** (`tools/prefork/`):
  - 1.024 URL trong **73 s** (14 URL/s), so với 942 s qua HTTP. 19/20 trang giống hệt bản HTTP.
  - Chỉ dùng 1 tiến trình con, vì VPS chính có 1 vCPU.
  - Trên production dự kiến chỉ nhanh khoảng 3–5 lần (vì có Redis). **Chưa đo trên production.**

## 5. Việc tiếp theo (chủ site chọn)
1. **Service `dott-cache` có hàng đợi** (SQLite, nhận sự kiện từ mu-plugin trên php81, render bằng HTTP hoặc prefork, upload nhiều đích), test trên php81 với bucket test.
2. Đo prefork trên production (chỉ đọc, không ghi cache), khi chủ site cho phép.
3. **Đo tác động của Query Monitor và của từng plugin** lên bootstrap (dùng prefork lab, chỉ đo, không tắt plugin thật).
4. Các quyết định thiết kế đang chờ chủ site (design mục 15):
   - Vòng renew **30 ngày** (đề xuất) hay khoảng 90 ngày.
   - Có cần cảnh báo log gần thời gian thực không.
   - Tạo key SSH thứ hai, chỉ chạy được vài lệnh cố định (`command=`), cho service.
   - Mở Netdata cho IP VPS phụ khi khóa Netdata.
   - Media có đưa vào đối soát không.
   - Gộp desktop và mobile thành một bản HTML: **đã chốt hướng** (tt-core, mu-plugin `dott-single-cache`, rocket-nginx), chưa áp dụng cho production.
   - Chỉ lưu `.html` trên S3 (Worker cần sửa), nhiều đích S3 theo châu lục.
   - **Giữ** `_gzip` trên đĩa: WP Rocket không có tùy chọn tắt, và rocket-nginx trả thẳng file này cho Cloudflare.
   - Ping tức thì qua VPC Vultr, miễn rate limit nginx cho IP VPS phụ (design 15.7, 15.8).

## 6. Cách làm việc (bẫy đã gặp)
- **Lệnh trên VPS chính:** `python vps.py run "lệnh"`. Lệnh nhiều dòng hoặc có `"`: dùng `@'...'@ | python vps.py run -`, vì PowerShell 5.1 làm rơi dấu `"`. `python vps.py put|get|sql|info` cũng có.
- **Lệnh trên VPS phụ:** `ssh claude-control "..."`. Script dài thì viết file vào scratchpad, `scp` lên `/tmp`, chạy `sed -i 's/\r$//'` rồi `bash`. Lý do: CRLF/BOM từ Windows làm hỏng script, và `$` hoặc `'` bị PowerShell ăn.
- Trong script chạy trên VPS phụ, `ssh main` phải dùng `ssh -n main ...`, nếu không nó đọc mất stdin của script.
- Python trên PC lỗi SSL vì Avast chặn HTTPS. Gọi API từ PowerShell hoặc `curl.exe` (dùng kho chứng chỉ Windows), hoặc chạy từ VPS. Token Cloudflare đọc từ `secrets.json`; header để trong file tạm rồi `-H @file`, xóa file sau khi dùng.
- **Gọi origin bỏ qua cache:** thêm query string lạ (`?djtprobe=N`). WP Rocket không cache vì `cache_query_strings=[]`.
- PHP CLI: `opcache.file_cache` gây **segfault**, không dùng. Chạy dưới user site: `runuser -u diaocthDP4f -- php ...`. Khi đo render phải đặt `DONOTCACHEPAGE`.
- Build Go trên VPS phụ: `GOMEMLIMIT=200MiB GOFLAGS=-p=1 go build`. Lần đầu tốn khoảng 220 MB RAM và 77 giây.
- Cloudflare KV bulk delete có thể sót key dù báo 0 lỗi, nên **luôn list lại toàn bộ để kiểm tra**. Dữ liệu đọc ra có độ trễ khoảng 60 giây.
- Không in mật khẩu hay token ra hội thoại. `secrets.json` và skill `vps-diaocthongthai` đều đã gitignored.

## 7. Thư mục và tài liệu liên quan
| Nơi | Nội dung |
|---|---|
| `D:\Claude\2026-11-25 Ket Noi VPS\` | Nơi gốc của skill `vps-diaocthongthai` (bản trong `.claude/skills/` ở đây là **bản sao**; nếu sửa thì sửa cả 2), `TODO_VPS.md` gốc, `wp/mu-plugins/` (mu-plugin Media Cloud), `wp/plugin-repo-build/` |
| `D:\Claude\2026-11-25 Cache Management\` | Nhánh `main` (code Python cũ), `secrets.json` gốc, `data/cache_urls.db` (bản chụp 25/11) |
| `D:\Claude\2026-11-26 Claude Vultr VPS\` | Tài liệu dựng VPS phụ (README, ACCESS, SETUP-PLAN, INSTALL-LOG, `deploy/`) |
| VPS phụ `/root/work/` | Skill `vps-claude-control` (bản gốc), `2026-11-27_vps-log-va-giam-sat.md`, `/root/health/` |
| `D:\Draft\2025-09-07 CF WORKER.JS\` | `thongthai-cache-v7.js` (Worker HTML), `thongthai-cdn-v2.js` (CDN static) |
| GitHub public | `congtoan/ct-media-s3-upload` (v1.7.0), `congtoan/ct-media-overwrite-by-filename` (v9.3.0). Trên site vẫn chạy 1.5 và 9.1.0 |
