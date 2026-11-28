# HANDOFF – Cache System v2 (cập nhật 2026-11-28 18:10)

> Điểm bắt đầu cho hội thoại mới. **Đọc file này trước.** Tra cứu khi cần:
> - [docs/SUMMARY-2026-11-28.md](docs/SUMMARY-2026-11-28.md): tổng kết ngày 28/11 (đã làm gì, kết quả);
> - [docs/design-pool.md](docs/design-pool.md): **kiến trúc sự kiện + pool** (hướng hiện tại);
> - [docs/rollout-log.md](docs/rollout-log.md): nhật ký và số đo từng bước (dài, chỉ đọc phần cần);
> - [docs/INVENTORY-2026-11-28.md](docs/INVENTORY-2026-11-28.md): kiểm kê trước rollout (một phần đã cũ);
> - [docs/design-server-only.md](docs/design-server-only.md) (mục 11b: bố cục thư mục), [docs/design-tt-core.md](docs/design-tt-core.md), [docs/HISTORY.md](docs/HISTORY.md).

## 1. Hệ thống trong 1 phút

| | VPS chính (production) 45.77.32.177:8282 | VPS phụ `claude-control` 139.180.158.221 |
|---|---|---|
| Vai trò | WordPress multisite diaocthongthai.com (blog 1 vi `/`, blog 2 en `/en/`, bỏ qua `/es/`), WP Rocket 3.16, nginx + rocket-nginx | Tool Go `dott-cache`: render (prefork qua SSH), upload S3, Cloudflare KV, service `serve` (sự kiện + pool) |
| Truy cập | `python vps.py run "..."` (skill `vps-diaocthongthai`) | `ssh claude-control "..."`; từ VPS phụ: `ssh main` (key root, `no-port-forwarding`), `ssh main-tunnel` (chỉ để tunnel) |
| Thư mục | `wp-content/mu-plugins/dott-cache/*.php`, `mu-plugins/dott-single-cache.php`, `mu-plugins/dott-cache-events.php` (chỉ php81), `/usr/local/sbin/dott-prefork` | `/root/tool/cache-system/{bin,src,config/{sites,secrets},data,logs,lists,test}` |
| Site test | php81.congtoan.net (blog 3 `/tttest/`, blog 4 `/tttest-en/`), bucket `congtoanphp`, KV `php81-cache-test` | |

**Hướng v2:**
- Một bản HTML cho mọi thiết bị: theme tt-core, mu-plugin `dott-single-cache`, sticky 3.2.0.
- nginx trả thẳng file cache; S3 chỉ có `index-https.html`; Worker v2 chọn bucket theo châu lục.
- **Cập nhật theo sự kiện** (mu-plugin → tunnel → `serve` → pool) + **đối soát hằng ngày** (`reconcile`) + **toàn site hằng tháng** (`submit -all`). Bỏ lịch `sync` 8 giờ/lần.
- Site là nguồn sự thật; KV/S3/đĩa là trạng thái thật; `state.db` (danh sách URL) sẽ bỏ sau khi pool chạy ổn trên production.
- Chuyển production từng blog: **vi đã chuyển (28/11)**, en làm lần 2.

## 2. Lệnh `dott-cache` (VPS phụ)
```
# Pool (docs/design-pool.md); cần dott-cache-serve đang chạy
dott-cache serve -sites a.com[,b.com] [-listen 127.0.0.1:8790]      (systemd dott-cache-serve.service)
dott-cache submit -site <d> -url|-urls|-txt ... [-remove] [-prio 3] | -all -blog /
dott-cache pool status
dott-cache reconcile -site <d> -blog / [-dry-run] [-max-pct 2] [-force]
# CLI cũ (đọc state.db); bị từ chối khi serve đang giữ khóa site đó
dott-cache urls sync|list|stats ...; update -all|-url|-txt|-segment ... -blog / [-yes]; sync; replace; kv ...
```
- Điều tiết theo load `max_load` 1,5 cho lô prefork; lô sự kiện ≤ 4 URL (HTTP) không chờ load.
- `write_blogs` trong config: chỉ cho ghi các blog được liệt kê.
- Log `serve`: `journalctl -u dott-cache-serve`, kết quả từng URL `logs/<site>/serve-YYYYMMDD.jsonl`.

## 3. Trạng thái (28/11 18:10)

**Production site vi (blog 1): đã chuyển.**
| Thành phần | Trạng thái |
|---|---|
| Theme | generatepress_child **v0.7.0** (tt-core, build 19 module, `core.js?ver=62d9b17e24`). Backup `/root/backup-p2-2026-11-28/` |
| Plugin sticky | **3.2.0** (network). Module sticky trong build vi; en chạy như cũ |
| WP Rocket config blog 1 | Tạo lại 17:05: `do_caching_mobile_files = 0`. Backup `/root/backup-p4-2026-11-28/` |
| mu-plugins | `dott-single-cache.php` (blog 1), `dott-cache/{prefork-render,export-urls (--ids),replace-cache}.php` |
| Cron cũ, screen | **Đã xóa / đóng** (P5). Backup `/root/backup-p5-2026-11-28/crontab-root.txt` |
| **P6 `update -all -blog /`** | **Đang chạy nền** từ 17:12 (`systemd-run` unit `dott-p6-vi` trên VPS phụ, log `logs/diaocthongthai.com/p6-update-all-vi.txt`). Đã dọn S3/server/KV; đang render 24.273 URL (lô 10/122 lúc 18:05, 2–2,8 URL/s, dự kiến xong khoảng 20:30–21:00). 1 URL prefork lỗi do wpdatatables ghi file tạm (`tempfile*.csv`, không có quyền ghi), đã thử lại bằng HTTP |
| Worker | **v2 từ 17:45** (script `thongthai-cache`, route `diaocthongthai.com/*`): NA/SA → S3 Chicago `cdn.diaocthongthai.com`, châu khác → origin, `LEGACY_PREFIXES ["/en/"]`. Quay lại v7: `bash deploy/worker/deploy-production.sh rollback` trên VPS phụ (bản v7 ở `test/p7/v7-live/`) |
| Config VPS phụ | `write_blogs: ["/"]` |
| Pool production | **Chưa bật** (chưa có mu-plugin events, file hook; `serve` chỉ phục vụ php81) |

**Site en (blog 2):** chưa đổi gì: theme không tt-core, WP Rocket vẫn tách mobile, S3 vẫn có file kiểu cũ; Worker phục vụ kiểu v7 (`LEGACY_PREFIXES`).

**VPS phụ:**
- `dott-cache-serve.service` (`-sites php81.congtoan.net`) và `dott-cache-tunnel.service` đang chạy, **chưa enable**.
- `serve` giữ khóa site php81: lệnh ghi CLI cho php81 bị từ chối (dùng `submit` hoặc dừng serve).
- Key tunnel `/root/.ssh/id_dott_tunnel`, alias `main-tunnel`. Trên VPS chính: `restrict,port-forwarding,permitlisten="127.0.0.1:8790",permitopen="127.0.0.1:1",command="/bin/false"`. Backup `authorized_keys.bak-2026-11-28-1626`.
- Token hook php81: `config/secrets/hook-php81.congtoan.net.token`; trên site: `/home/php81conZSI/php81.congtoan.net/dott-cache-hook.json`.

## 4. Việc tiếp theo
1. **Khi P6 xong** (`systemctl is-active dott-p6-vi` = inactive):
   - đọc cuối log `p6-update-all-vi.txt` (số ok/lỗi, kiểm tra lại);
   - `dott-cache reconcile -site diaocthongthai.com -blog / -dry-run` (không cần serve khi dry-run) để xem thiếu/mồ côi;
   - kiểm tra vài URL desktop/iPhone (nginx HIT, `core.js`), S3 không còn mobile/`_gzip`;
   - ghi kết quả vào rollout-log mục P6.
2. **P8, bật pool cho production cả 2 blog** (sau khi en chuyển xong): kế hoạch chi tiết [docs/plan-P8.md](docs/plan-P8.md). Phương án `legacy_blogs` cho en đã bỏ (en chuyển hẳn trong ngày 28/11). Tóm tắt (hỏi chủ site trước):
   - token `hook-diaocthongthai.com.token`, file `/home/diaocthDP4f/diaocthongthai.com/dott-cache-hook.json` (600, user site);
   - cài `mu-plugins/dott-cache-events.php` vào production;
   - `serve -sites php81.congtoan.net,diaocthongthai.com` (sửa unit), `systemctl enable --now dott-cache-serve dott-cache-tunnel`;
   - cài `dott-cache-reconcile@vi.timer` (03:30), hằng tháng `dott-cache-blog.sh vi submit-all`;
   - test: sửa một bài vi và xem log serve. Lưu ý `serve` sẽ giữ khóa site production, nên CLI `update` phải chạy qua `submit`.
3. **Đo** tốc độ khách Việt Nam với Worker v2 (châu Á vào origin) so với trước.
4. **Lần 2, site en:** theme `generatepress_en` v0.7.0 + build → `DOTT_SINGLE_CACHE_BLOGS = array(1, 2)` + tạo lại config `.en` → `write_blogs: ["/", "/en/"]` → `submit -all -blog /en/` (hoặc `update -all`) → bỏ `LEGACY_PREFIXES` trong Worker.
5. Sau khi pool ổn: bỏ `state.db`, `urls sync`, `sync`, lệnh `renew`, `regen-all`; dọn php81 (theo ý chủ site).

**Còn mở:**
- Chủ site **đổi mật khẩu** đăng nhập diaocthongthai.com và php81 (đã dán trong hội thoại 28/11), và **đổi 2 token Cloudflare** đã dán ngày 28/11.
- Báo Telegram (dùng chung bot `/root/health`); key S3 riêng; token Cloudflare hẹp hơn; bucket Frankfurt nếu muốn phục vụ EU từ S3.
- wpdatatables ghi file tạm vào thư mục hiện hành khi prefork: xem có cần `chdir` vào thư mục ghi được trong `prefork-render.php` không.
- Khi có bài mới, chỉ trang 1 archive và trang phân trang mới được làm mới; các trang 2..N chờ `submit -all` hằng tháng.

## 5. Quy tắc (CLAUDE.md) và bẫy đã gặp
- **Mọi thao tác ghi trên VPS chính phải hỏi chủ site trước**, và backup trước khi sửa. Chỉ đọc thì cứ làm. **Claude không nhập mật khẩu**: chủ site tự đăng nhập trong browser pane.
- Không in mật khẩu, key hay token. Secret trên VPS phụ ở `config/secrets/`; trên PC ở `secrets.json` (gitignored). **Không `git add -A`**: add từng file.
- **Script chạy trên VPS:** viết file vào scratchpad, `scp` lên VPS phụ, `sed -i 's/\r$//'`, rồi `bash` (hoặc `ssh main bash -s < file`). Không nhét lệnh có `$`, `"`, `'`, `|` vào `ssh "..."` từ PowerShell (hỏng nhiều lần).
- Commit message có dấu ngoặc kép: dùng `git commit -F <file>`.
- Trong script trên VPS phụ dùng `ssh -n main`. VPS chính đôi khi từ chối khi nhiều kết nối liền nhau; script test phải dừng khi đọc thiếu dữ liệu.
- Build Go trên VPS phụ: `cd /root/tool/cache-system/src && GOMEMLIMIT=300MiB GOFLAGS=-p=1 go build -o /tmp/dott-cache-new ./cmd/dott-cache`, rồi `install` vào `bin/` (backup bản cũ). Nguồn sửa ở repo rồi `scp` lên `src/`. Test: `go test ./internal/pool/`.
- **KV đọc ra trễ tới khoảng 60 s**: không dùng KV GET để kiểm tra vừa ghi/xóa; dùng S3 (đọc ngay sau khi ghi là thấy).
- **OpenSSH 8.2 trên VPS chính** không nhận `permitopen="none"` trong `authorized_keys` (bỏ qua cả dòng key).
- OPcache `revalidate_freq=180`: sau khi sửa PHP hoặc config WP Rocket, đợi 3 phút rồi mới kiểm tra.
- So bản prefork với bản HTTP: so với file cache do request thường tạo, không so với bản có query string.
- WP Rocket: không đặt `cache_mobile=0`. File `wp-rocket-config/<host>[.<blog>].php` chỉ được tạo lại khi lưu thiết lập hoặc `rocket_generate_config_file()`.
- Multisite: plugin `Network: True` bật cho toàn mạng; sau mỗi thay đổi kiểm tra mọi blog.

## 6. Git và nơi liên quan
- Repo `congtoan/dott-cache-management` (private), nhánh `v2` (worktree này); `main` là Python cũ ở `D:\Claude\2026-11-25 Cache Management`.
- Theme: repo private `congtoan/dott-generatepress-child` (tag v0.7.0), `dott-generatepress-en`; nguồn tt-core `theme/` trong repo này.
- Plugin sticky: repo private `congtoan/cong-toan-multi-sticky-sidebar` (release v3.2.0, `DEPLOY.md`).
- Worker v7 cũ: `D:\Draft\2025-09-07 CF WORKER.JS\thongthai-cache-v7.js`; bản v7 đang chạy trước 28/11 ở VPS phụ `test/p7/v7-live/worker.js`.
- Skill `vps-diaocthongthai`: `D:\Claude\2026-11-25 Ket Noi VPS\`. VPS phụ: skill `vps-claude-control`.
