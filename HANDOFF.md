# HANDOFF – Cache System v2 (cập nhật 2026-11-28 13:20)

> Điểm bắt đầu cho hội thoại mới. **Chỉ cần đọc file này và [docs/INVENTORY-2026-11-28.md](docs/INVENTORY-2026-11-28.md).**
> Tài liệu khác chỉ đọc khi cần tra cứu:
> - [docs/rollout-log.md](docs/rollout-log.md): nhật ký và số đo từng bước (dài);
> - [docs/design-server-only.md](docs/design-server-only.md): thiết kế, mục 11b là bố cục thư mục;
> - [docs/HISTORY.md](docs/HISTORY.md): mốc, tag, những điều từng hiểu sai;
> - [docs/design-tt-core.md](docs/design-tt-core.md): theme tt-core.

## 1. Hệ thống trong 1 phút

| | VPS chính (production) 45.77.32.177:8282 | VPS phụ `claude-control` 139.180.158.221 |
|---|---|---|
| Vai trò | WordPress multisite diaocthongthai.com (blog 1 vi `/`, blog 2 en `/en/`, bỏ qua `/es/`), WP Rocket 3.16, nginx + rocket-nginx, Redis | Tool Go `dott-cache`: danh sách URL (SQLite), tạo cache (gọi prefork trên VPS chính qua SSH), upload S3, Cloudflare KV |
| Truy cập | `python vps.py run "..."` (skill `vps-diaocthongthai`) | `ssh claude-control "..."`; từ VPS phụ sang VPS chính: `ssh main` |
| Thư mục | `wp-content/mu-plugins/dott-cache/*.php` (trong site), `/usr/local/sbin/dott-prefork` (bộ chuyển lệnh của root) | `/root/tool/cache-system/{bin,src,config/{sites,secrets},data,logs,lists,test}` |
| Site test | php81.congtoan.net (blog 3 `/tttest/` vi, blog 4 `/tttest-en/` en), bucket `congtoanphp` (Chicago + Frankfurt), KV `php81-cache-test`, Worker `dott-cache-php81-test.congtoan-real.workers.dev` | |

**Hướng v2 (đã chốt):**
- Một bản HTML cho mọi thiết bị: theme tt-core (`theme/`), mu-plugin `dott-single-cache`, plugin sticky 3.2.0.
- nginx trả thẳng file cache. Chỉ upload `index-https.html` lên S3.
- Worker v2 chọn bucket theo châu lục.
- Tạo cache bằng prefork. Chạy theo lịch: `sync` 8 giờ/lần, `sync -full` hằng ngày, `update -all` hằng tháng.
- **Chuyển production từng blog:** vi (`-blog /`) trước, kiểm tra xong mới tới en (`-blog /en/`).

## 2. Lệnh `dott-cache` (VPS phụ)
```
dott-cache urls sync|list|stats -site <domain> ...          danh sách URL (SQLite data/<domain>/state.db)
dott-cache update -site <domain> -all [-blog /] [-yes]       tạo lại toàn site/blog (không -yes: chỉ kiểm kê)
dott-cache update -site <domain> -url|-urls|-txt|-segment ... [-blog]   một danh sách
dott-cache sync -site <domain> -blog / [-full] [-dry-run]    thay đổi trên site → cache, S3, KV (chạy theo lịch)
dott-cache replace -site <domain> -content|-version <phạm vi> [-yes]    sửa nhanh file cache, upload lại S3
dott-cache kv create|list|sync -site <domain>
```
Chạy `dott-cache` không tham số để xem đủ cờ.
- Điều tiết theo load: `max_load` 1,5 (chủ site chốt).
- Khóa theo site: 2 lệnh ghi không chạy chồng nhau.
- `write_blogs` trong config: chỉ cho ghi các blog được liệt kê.

## 3. Trạng thái production (28/11 13:20)

| Thành phần | Trạng thái |
|---|---|
| rocket-nginx + `open_file_cache off` | **Đang chạy** (`/etc/nginx/extra/rocket-page-cache.conf`). Thư mục còn `.mobile-active` thì vẫn đi qua PHP như cũ |
| `mu-plugins/dott-cache/prefork-render.php` | Đã cài (P1). `export-urls.php` và `replace-cache.php` **chưa cài** |
| `/usr/local/sbin/dott-prefork` | Có `diaocthongthai.com` |
| **mu-plugin `dott-single-cache.php`** | **Đã cài và bật cho blog 1** (`DOTT_SINGLE_CACHE_BLOGS = array(1)`, 28/11 13:17). Option khi đọc ra: blog 1 `do_caching_mobile_files=0`, blog 2 `=1`. **File `wp-rocket-config/diaocthongthai.com.php` CHƯA được tạo lại** (vẫn `do_caching_mobile_files = 1`, 09/03), nên cách phục vụ cache chưa đổi. Xem cảnh báo ở mục 4 |
| Theme | **Chưa có tt-core** (P2 chủ site làm sau). Theme vẫn chọn `thongthai-script.js` / `-mobile.js` theo `wp_is_mobile()` |
| Plugin sticky | 3.0.0 (cần 3.2.0, P3) |
| 8 bài vi test | Đã xóa cache trên server rồi tạo lại bằng prefork: chỉ còn `.html` và `_gzip`, nginx trả HIT cho cả mobile. Khách mobile của 8 bài này nhận JS desktop cho tới P2. Danh sách: `test/t8-prod/list.txt` trên VPS phụ |
| Cron cũ | `cron-cache-clean-v2.py` (phút 10 các giờ 0, 4, 8, 12, 16, 20), `cron-cache-segment-update-v2.py` (3:45, 15:45): **vẫn bật** |
| Config `config/sites/diaocthongthai.com.json` (VPS phụ) | Đích Chicago `cdn.diaocthongthai.com`, KV `cc831…`. **`write_blogs: ["-"]`: chưa cho ghi blog nào** |
| Worker production | Vẫn là v7. Worker v2 (`deploy/worker/dott-cache-worker.js`) đã test trên workers.dev |
| KV production | 60.687 key, không còn key dạng URL đầy đủ |

## 4. Việc tiếp theo (theo thứ tự)

**Lần 1, site vi (`-blog /`):**
1. **P2:** deploy theme tt-core cho `generatepress_child` (chủ site làm), build `wp tt-core build`, kiểm tra desktop và mobile.
2. **P3:** plugin sticky 3.2.0. Plugin dùng chung cả mạng, nhưng an toàn cho en: theme en chưa có tt-core nên plugin chạy như bản cũ.
3. **P4:**
   - Cài `export-urls.php` và `replace-cache.php` vào `mu-plugins/dott-cache/`.
   - **Tạo lại file config WP Rocket của blog 1**, để `do_caching_mobile_files = 0` có hiệu lực (ví dụ lưu thiết lập WP Rocket trong admin site vi, hoặc `wp --url=https://diaocthongthai.com/ eval 'rocket_generate_config_file();'`).
   - Từ lúc này, thư mục còn `.mobile-active` sẽ được PHP trả bản desktop cho cả khách mobile.
4. **P5:** tắt phần vi của 2 cron cũ (hoặc tắt hẳn nếu chấp nhận en không còn cron cũ). Dừng các screen sót (`en-map-of`, `F1`, `F2`).
5. **P6:**
   - Đổi config VPS phụ thành `write_blogs: ["/"]`.
   - `dott-cache urls sync -site diaocthongthai.com` (cần `export-urls.php` ở P4).
   - `update -all -blog /` (chạy kiểm kê trước, rồi `-yes`).
   - Số liệu kiểm kê 28/11: 24.273 URL; S3 có 72.089 file kiểu cũ sẽ xóa; server có 66.468 file mobile; mồ côi 0,46%.
   - Thời gian ước tính 1,5–2 giờ, chưa tính thời gian nghỉ do điều tiết.
6. **P7 (làm ngay sau P6):** deploy Worker v2 lên route production với `REDIRECTS=1`, `LEGACY_PREFIXES=["/en/"]`, `TARGETS={"NA","SA": Chicago}`. Lý do: sau P6 không còn bản mobile và `_gzip` của vi trên S3, nên Worker v7 sẽ đưa khách châu Mỹ của vi về origin. Cần token Cloudflare có quyền Workers Scripts + Workers Routes.
7. **P8:** cài systemd (`deploy/systemd/`, instance `vi`): `sync` 8 giờ/lần, `sync-full` hằng ngày, `update-all` hằng tháng.

**Lần 2, site en (`-blog /en/`):** theme `generatepress_en` + build → `DOTT_SINGLE_CACHE_BLOGS = array(1, 2)` + tạo lại config `.en` → `write_blogs: ["/", "/en/"]` → `update -all -blog /en/` → bỏ `LEGACY_PREFIXES` trong Worker → timer instance `en`.

**Còn mở, không chặn rollout:**
- Lưu trạng thái render/upload từng URL vào DB, rồi thêm `dott-cache status`.
- Báo Telegram (dùng chung bot với `/root/health`).
- Lệnh kiểm tra riêng, chỉ đọc.
- Key SSH riêng cho service (`command=` bộ chuyển lệnh); key S3 riêng; token Cloudflare hẹp hơn; **đổi 2 token Cloudflare đã dán trong hội thoại 28/11**.
- Bucket Frankfurt cho production (nếu muốn phục vụ khách EU từ S3).
- Sau khi xong: dọn php81 (site test, Worker workers.dev, KV test, bucket `congtoanphp`), bỏ lệnh cũ `renew` và `regen-all`.

## 5. Quy tắc (CLAUDE.md) và bẫy đã gặp
- **Mọi thao tác ghi trên VPS chính phải hỏi chủ site trước**, và backup trước khi sửa. Chỉ đọc thì cứ làm.
- Không in mật khẩu, key hay token. Key nằm trên VPS phụ ở `config/secrets/` (Claude trên VPS phụ bị cấm đọc). Trên PC, token nằm trong `secrets.json` (gitignored).
- **Script dài chạy trên VPS:** viết file vào scratchpad, `scp` hoặc `python vps.py put` lên `/tmp`, chạy `sed -i 's/\r$//'` rồi `bash`. Không nhét lệnh nhiều dòng có `$`, `"`, `'` vào `ssh "..."` từ PowerShell.
- **PowerShell:** không đặt tên hàm `R`/`rd`/`rm`… (trùng alias). Thay chuỗi hàng loạt thì dùng công cụ Edit, không dùng script (ngày 28/11 từng làm hỏng một file).
- Trong script trên VPS phụ phải dùng `ssh -n main`. VPS chính đôi khi từ chối khi có nhiều kết nối SSH liền nhau; `dott-cache` đã tự thử lại.
- Build Go trên VPS phụ: `cd /root/tool/cache-system/src && GOMEMLIMIT=300MiB GOFLAGS=-p=1 go build -o ../bin/dott-cache ./cmd/dott-cache`. Nguồn sửa ở repo rồi `scp` lên `src/`.
- **So bản prefork với bản HTTP:** phải so với file cache do request thường tạo, không so với bản có query string (`?dottprobe` không qua bước tối ưu của WP Rocket).
- Multisite: `wp --url=X plugin activate` với plugin có `Network: True` sẽ bật cho toàn mạng. Sau mỗi thay đổi, kiểm tra mọi blog.
- WP Rocket: không được đặt `cache_mobile=0`, vì khách mobile sẽ mất cache. File `wp-rocket-config/<host>[.<blog>].php` chỉ được tạo lại khi lưu thiết lập.
- Cloudflare KV bulk delete có thể sót key, nên luôn list lại để kiểm tra (tool đã làm).

## 6. Git và nơi liên quan
- Repo `congtoan/dott-cache-management` (private), nhánh `v2` (worktree này); `main` là Python cũ ở `D:\Claude\2026-11-25 Cache Management`. Tag mới nhất: `v2-tools-2026-11-28`.
- Skill gốc `vps-diaocthongthai`: `D:\Claude\2026-11-25 Ket Noi VPS\`. VPS phụ: skill `vps-claude-control`.
- Worker v7 đang chạy: `D:\Draft\2025-09-07 CF WORKER.JS\thongthai-cache-v7.js`.
- Plugin sticky: `D:\Claude\2026-11-20 Sticky Sidebar Project\` (hướng dẫn tích hợp `tt-core-integration.md`).
