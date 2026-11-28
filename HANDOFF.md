# HANDOFF – Cache System v2 (cập nhật 2026-11-29 02:10)

> Điểm bắt đầu cho hội thoại mới. **Đọc file này trước.** Tra cứu khi cần:
> - [docs/SUMMARY-2026-11-29.md](docs/SUMMARY-2026-11-29.md): **tổng kết mới nhất** (en chuyển, sửa export-urls, P8 bật pool production; TODO đầy đủ ở mục 5);
> - [docs/SUMMARY-2026-11-28.md](docs/SUMMARY-2026-11-28.md): tổng kết ngày 28/11 (thiết kế pool, rollout vi P2–P7);
> - [docs/design-pool.md](docs/design-pool.md): **kiến trúc sự kiện + pool** (hướng hiện tại);
> - [docs/rollout-log.md](docs/rollout-log.md): nhật ký và số đo từng bước (dài, chỉ đọc phần cần);
> - [docs/INVENTORY-2026-11-28.md](docs/INVENTORY-2026-11-28.md): kiểm kê trước rollout (một phần đã cũ);
> - [docs/design-server-only.md](docs/design-server-only.md) (mục 11b: bố cục thư mục), [docs/design-tt-core.md](docs/design-tt-core.md), [docs/HISTORY.md](docs/HISTORY.md).

## 1. Hệ thống trong 1 phút

| | VPS chính (production) 45.77.32.177:8282 | VPS phụ `claude-control` 139.180.158.221 |
|---|---|---|
| Vai trò | WordPress multisite diaocthongthai.com (blog 1 vi `/`, blog 2 en `/en/`, bỏ qua `/es/`), WP Rocket 3.16, nginx + rocket-nginx | Tool Go `dott-cache`: render (prefork qua SSH), upload S3, Cloudflare KV, service `serve` (sự kiện + pool) |
| Truy cập | `python vps.py run "..."` (skill `vps-diaocthongthai`) | `ssh claude-control "..."`; từ VPS phụ: `ssh main` (key root, `no-port-forwarding`), `ssh main-tunnel` (chỉ để tunnel) |
| Thư mục | `wp-content/mu-plugins/dott-cache/*.php`, `mu-plugins/dott-single-cache.php`, `mu-plugins/dott-cache-events.php` (php81 và production), `/usr/local/sbin/dott-prefork` | `/root/tool/cache-system/{bin,src,config/{sites,secrets},data,logs,lists,test}` |
| Site test | php81.congtoan.net (blog 3 `/tttest/`, blog 4 `/tttest-en/`), bucket `congtoanphp`, KV `php81-cache-test` | |

**Hướng v2:**
- Một bản HTML cho mọi thiết bị: theme tt-core, mu-plugin `dott-single-cache`, sticky 3.2.0.
- nginx trả thẳng file cache; S3 chỉ có `index-https.html`; Worker v2 chọn bucket theo châu lục.
- **Cập nhật theo sự kiện** (mu-plugin → tunnel → `serve` → pool) + **đối soát hằng ngày** (`reconcile`) + **toàn site hằng tháng** (`submit -all`). Bỏ lịch `sync` 8 giờ/lần.
- Site là nguồn sự thật; KV/S3/đĩa là trạng thái thật; `state.db` (danh sách URL) sẽ bỏ sau khi pool chạy ổn trên production.
- Chuyển production từng blog: **vi và en đã chuyển (28/11)**; pool production bật 29/11 01:45 (P8).

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

## 3. Trạng thái (29/11 01:52): cả 2 blog đã chuyển, pool production đang chạy

| Thành phần | vi (blog 1) | en (blog 2) |
|---|---|---|
| Theme | generatepress_child v0.7.0, build `62d9b17e24` (19 module). Backup `/root/backup-p2-2026-11-28/` | generatepress_en v0.7.0, build `1cc3bb7b7f` (9 module). Backup `/root/backup-en-2026-11-28/` |
| Một bản cache | `DOTT_SINGLE_CACHE_BLOGS = array( 1, 2 )`; cả 2 `wp-rocket-config` có `do_caching_mobile_files = 0` (kiểm lại sau khi chủ site lưu thiết lập 21:54/21:56) | như vi |
| Cache toàn blog | P6 xong 28/11 20:18: 24.272/24.273 (+75 trang phân trang, +66 trang `danh-sach-*`); URL còn thiếu (bài 2939) đã sửa và render 01:57. Tổng 24.414 URL | xong 28/11 23:25 (+ chạy lại 8 URL lỗi PUT 00:30): 39.365 URL, reconcile thiếu 0, mồ côi 0 |

- Plugin sticky **3.2.0** (network). Cron cũ, screen: đã xóa (P5, backup `/root/backup-p5-2026-11-28/`).
- mu-plugins: `dott-single-cache.php`, `dott-cache-events.php` (từ 29/11 01:44), `dott-cache/{prefork-render, export-urls, replace-cache}.php`. **`export-urls.php` sửa 28/11 20:53**: số trang archive bằng truy vấn chính (trước đây thiếu phân trang chuyên mục cha), `--ids` có chuyên mục cha. php81 chưa cập nhật bản này.
- **Worker v2** (script `thongthai-cache`, từ 28/11 17:45; `LEGACY_PREFIXES` bỏ 18:30): NA/SA → S3 Chicago, châu khác → origin. Quay lại v7: `bash test/p7/deploy-production.sh rollback` trên VPS phụ.
- **Pool production (P8, 29/11 01:45):** `serve -sites php81.congtoan.net,diaocthongthai.com` (enable), tunnel (enable), file hook `/home/diaocthDP4f/diaocthongthai.com/dott-cache-hook.json`. Sửa bài → S3 mới sau khoảng 10 s. Timer: reconcile vi/en 03:30 hằng ngày; `submit-all` vi ngày 1, en ngày 15 (04:00). `serve` giữ khóa site production: lệnh CLI ghi phải qua `submit`.
- Config VPS phụ: `write_blogs: ["/", "/en/"]`; 8 exclude `danh-sach-*`, `/en/list-of-all-*` đã bỏ (28/11 22:00).
- Key tunnel `/root/.ssh/id_dott_tunnel` (alias `main-tunnel`, `restrict,port-forwarding,permitlisten="127.0.0.1:8790",…`). Token hook: `config/secrets/hook-<site>.token`.

## 4. Việc tiếp theo (chi tiết: [SUMMARY-2026-11-29.md mục 5](docs/SUMMARY-2026-11-29.md))
**Theo dõi:**
1. Lượt reconcile đầu tiên 29/11 03:30: `journalctl -u dott-cache-reconcile@vi -u dott-cache-reconcile@en --no-pager | tail -n 20`. Pool: `dott-cache pool status`; log `journalctl -u dott-cache-serve`.
2. Test trên production những gì chưa test: sửa bài en, bình luận, đổi slug, xóa bài, sửa term (vi sửa bài đã đạt: 11 s).
3. Lượt `submit-all@vi` đầu tiên ngày 01/12 04:00 (khoảng 3 giờ qua pool; sự kiện vẫn chen trước).

**Nên làm:**
4. Cập nhật `export-urls.php` mới cho php81.
5. Đo tốc độ khách Việt Nam với Worker v2 (châu Á vào origin) so với trước.
6. `submit -stale` (làm mới theo tuổi `cached@`, từ cũ tới mới, mỗi đêm một phần): [design-pool.md mục 8b](docs/design-pool.md). Cũng lấp chỗ hở: khi có bài mới, các trang 2..N của archive chỉ được làm mới ở lượt `submit-all` hằng tháng.
7. Báo Telegram (dùng chung bot `/root/health`): reconcile lỗi, pool có việc `failed`, tunnel rớt.
8. Plugin events ghi `error_log` khi tunnel tắt (hiện `blocking=false` nên mất sự kiện không để lại dấu vết).
9. wpdatatables ghi `tempfile*` vào thư mục hiện hành khi prefork: `chdir` sang thư mục ghi được trong `prefork-render.php`.
10. S3 còn 2 object lạ của blog vi (log P6 báo "2 file khác"): list để xác định. Theo dõi `RedisException` (2 lần lúc 00:23 ngày 29/11).

**Dọn dẹp (khi pool ổn vài tuần):** bỏ `state.db`, `urls sync`, `sync`, `renew`, `regen-all`, các script tạm `test/*.sh`; dọn php81 (theo ý chủ site).

**Chủ site:** đổi mật khẩu đăng nhập diaocthongthai.com và php81, đổi 2 token Cloudflare (đã dán trong hội thoại 28/11). Sau đó: key S3 riêng, token Cloudflare quyền hẹp hơn; bucket Frankfurt nếu muốn phục vụ EU từ S3.

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
- **File commit message ghi UTF-8 không BOM** (`[IO.File]::WriteAllText(..., New-Object Text.UTF8Encoding $false)`): `Out-File -Encoding utf8` và công cụ Write có thể để BOM dính vào tiêu đề commit.
- **Số trang archive:** không dùng `term->count` (bỏ sót bài của chuyên mục con và post type do theme chèn vào). `export-urls.php` dựng truy vấn chính (`ex_term_pages()`).
- **Ngưỡng mồ côi 2%** của `update`/`reconcile` là chốt an toàn: khi bị chặn thì tìm nguyên nhân, đừng vội `-force`.
- **Test sự kiện trên production:** gọi thẳng `Dott_Cache_Events::rocket_clean_post( get_post( ID ) )` bằng wp-cli; không bắn cả hook `after_rocket_clean_post` (còn 4 listener của WP Rocket xóa cache archive).
- Khi chủ site lưu thiết lập WP Rocket, file `wp-rocket-config` được tạo lại: kiểm tra vẫn `do_caching_mobile_files = 0`.
- Script deploy Worker trên VPS phụ nằm ở `test/p7/deploy-production.sh` (bản trong repo: `deploy/worker/`).
- `git push` có thể bị bộ phân loại quyền của Claude Code chặn: commit cục bộ, nhờ chủ site push.

## 6. Git và nơi liên quan
- Repo `congtoan/dott-cache-management` (private), nhánh `v2` (worktree này); `main` là Python cũ ở `D:\Claude\2026-11-25 Cache Management`.
- Theme: repo private `congtoan/dott-generatepress-child` (tag v0.7.0), `dott-generatepress-en`; nguồn tt-core `theme/` trong repo này.
- Plugin sticky: repo private `congtoan/cong-toan-multi-sticky-sidebar` (release v3.2.0, `DEPLOY.md`).
- Worker v7 cũ: `D:\Draft\2025-09-07 CF WORKER.JS\thongthai-cache-v7.js`; bản v7 đang chạy trước 28/11 ở VPS phụ `test/p7/v7-live/worker.js`.
- Skill `vps-diaocthongthai`: `D:\Claude\2026-11-25 Ket Noi VPS\`. VPS phụ: skill `vps-claude-control`.
