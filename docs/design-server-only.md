# Thiết kế pipeline cache server-only – bản nháp v1 (2026-11-25)

> Mục tiêu: **mọi thứ chạy trên VPS, tự động hoàn toàn**. Không còn bước chạy tay trên PC, không chép file DB qua lại.
> Bản này để thảo luận; chưa có code. Các lỗi F được nhắc tới nằm trong [findings-2026-11-25.md](findings-2026-11-25.md).

## 1. Nguyên tắc

1. **Một nguồn sự thật cho trạng thái cache**: bảng trong MariaDB trên VPS (thay cho SQLite và bản sao trên PC).
2. **Hướng sự kiện trước, quét định kỳ sau.** Thay đổi trên WordPress được đưa vào **hàng đợi** ngay lập tức; quét định kỳ chỉ làm lưới an toàn.
3. **Chỉ một tiến trình gọi PHP để tạo cache** (`dott-cache-worker`), có giới hạn tốc độ và tự lùi lại khi server bận. Không còn nhiều cron cùng curl đè lên nhau.
4. **Kiểm tra sau từng bước** (F13, F17): trigger xong phải thấy đủ 4 file mới; upload xong phải thấy `ok`; ghi/xóa KV xong phải list lại.
5. **Không làm chậm WordPress**: hook PHP chỉ INSERT vào hàng đợi, không gọi mạng.
6. **Giữ nguyên Worker Cloudflare** (`thongthai-cache-v7.js`) và cấu trúc S3 hiện tại; KV tiếp tục dùng key dạng path (đã sửa F4).
7. **Tài nguyên có hạn mức**: chạy bằng systemd với `MemoryMax`, `Nice`, `IOSchedulingClass=idle`; VPS chỉ có 2 GB RAM và còn phục vụ 3 site.

## 2. Kiến trúc tổng thể

```
                    ┌────────────────────────── WordPress (PHP-FPM) ──────────────────────────┐
                    │  mu-plugin dott-cache-queue.php                                          │
                    │   • after_rocket_clean_post / after_rocket_clean_files (WP Rocket purge) │
                    │   • transition_post_status (publish / unpublish / trash)                 │
                    │   • post_updated khi slug đổi (URL cũ → gỡ, URL mới → thêm)              │
                    │  → INSERT INTO dott_cache.queue (chỉ 1 câu SQL, không gọi mạng)          │
                    └──────────────────────────────┬───────────────────────────────────────────┘
                                                   │
   ┌──────────────── url_sync (timer 15') ─────────┤          ┌──────── pins (thay 2 cron cũ) ────────┐
   │ đọc thai_posts / thai_2_posts có thay đổi     │          │ URL ưu tiên làm mới định kỳ           │
   │ (lưới an toàn khi hook bị bỏ qua: import SQL) ├──► queue ◄┤ (clean_*.txt: 55h; urgent: 1h;       │
   │ hằng đêm: WP-CLI liệt kê archive + phân trang │          │  segment/tag: 12h)                    │
   └───────────────────────────────────────────────┘          └───────────────────────────────────────┘
                                                   │
                                                   ▼
             ┌──────────────── dott-cache-worker (systemd, chạy liên tục) ────────────────┐
             │ 1. lấy việc theo thứ tự: purge/new  >  pins đến hạn  >  renew URL cũ nhất │
             │ 2. refresh:  xóa S3 → xóa disk → trigger desktop + mobile (127.0.0.1)    │
             │              → kiểm đủ 4 file mới → upload S3 Chicago → đảm bảo có key KV │
             │    remove:   xóa KV → xóa S3 → xóa disk → đánh dấu removed               │
             │ 3. ghi kết quả vào urls (một transaction ngắn mỗi URL)                    │
             │ 4. tự lùi lại khi load cao / RAM thấp / PHP-FPM bận                       │
             └─────────────────────────────────────────────────────────────────────────────┘
                                                   │
                     ┌─────────────────────────────┴──────────────────────────┐
             reconcile (timer hằng tuần, 3:30 sáng CN)              report (timer hằng ngày)
             đối chiếu DB ↔ disk ↔ S3 ↔ KV, đưa việc sửa vào queue   tóm tắt + cảnh báo
```

## 3. Lưu trạng thái: chuyển từ SQLite sang MariaDB

### Vì sao không giữ SQLite
| Vấn đề | SQLite (hiện tại) | MariaDB (đề xuất) |
|---|---|---|
| Nhiều tiến trình cùng ghi | Chỉ một writer tại một thời điểm, khóa cả file; transaction dài sẽ chặn các tiến trình khác (`database is locked`) | InnoDB **khóa theo dòng**: worker cập nhật URL A không chặn url_sync thêm URL B |
| PHP (WordPress) ghi vào hàng đợi | Khó: file thuộc root, PHP-FPM chạy dưới user `diaocthDP4f`, cần thêm extension, rủi ro hỏng file | Dễ: `$wpdb` hoặc kết nối MySQL sẵn có, chỉ cần GRANT INSERT |
| Truy vấn và báo cáo | Phải SSH vào rồi chạy sqlite3 | phpMyAdmin, `vps.py sql`, đối chiếu thẳng với `thai_posts` |
| RAM thêm | 0 | Gần như 0 (MariaDB đang chạy sẵn; 60 nghìn dòng chỉ khoảng 20 MB) |
| Backup | Phải chép file | Đi chung `mysqldump` |

→ Tạo database riêng **`dott_cache`** và 2 user:
- `dott_cache_app`: quyền đầy đủ trên `dott_cache` (dùng cho các service Python).
- `dott_cache_wp`: **chỉ INSERT** vào `dott_cache.queue` (dùng cho mu-plugin).

Không đụng tới database WordPress.

### Schema (phác thảo)
```sql
CREATE TABLE urls (
  id            INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  path          VARCHAR(500) NOT NULL UNIQUE,     -- key chuẩn hóa giống Worker: '/abc/'
  blog_id       TINYINT NOT NULL,                 -- 1=vi, 2=en, 3=es
  page_type     ENUM('home','single','page','archive') NOT NULL,
  object_type   VARCHAR(32), object_id BIGINT,     -- post/term nguồn (để biết URL sinh ra từ đâu)
  page_number   SMALLINT NOT NULL DEFAULT 1,
  status        ENUM('active','removed') NOT NULL DEFAULT 'active',
  disk_at       DATETIME NULL,                    -- lần cuối tạo cache trên disk thành công
  s3_at         DATETIME NULL,                    -- lần cuối upload S3 thành công (đủ 4 file)
  kv_at         DATETIME NULL,                    -- lần cuối xác nhận có key KV
  fail_count    SMALLINT NOT NULL DEFAULT 0, last_error VARCHAR(255) NULL,
  updated_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY k_renew (status, s3_at), KEY k_src (object_type, object_id)
);

CREATE TABLE queue (                              -- chỉ thêm vào; worker nhận việc rồi xóa dòng
  id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  path        VARCHAR(500) NOT NULL,
  action      ENUM('refresh','remove') NOT NULL,
  priority    TINYINT NOT NULL,                   -- 10=purge/publish, 20=archive trang 1, 30=pins, 50=archive trang sâu
  reason      VARCHAR(64),                        -- 'rocket_clean_post', 'unpublish', 'pin', 'reconcile'...
  not_before  DATETIME NULL,                      -- gom việc trùng / thời gian chờ khi thử lại
  claimed_by  VARCHAR(64) NULL, claimed_at DATETIME NULL,
  created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  KEY k_take (claimed_by, priority, id)
);

CREATE TABLE pins (path VARCHAR(500) PRIMARY KEY, max_age_min INT NOT NULL, source VARCHAR(64));
CREATE TABLE state (k VARCHAR(64) PRIMARY KEY, v VARCHAR(255));   -- watermark của url_sync, thống kê
CREATE TABLE runs (id ..., job VARCHAR(32), started_at, finished_at, ok INT, failed INT, note TEXT);
```

### Cập nhật DB mà không khóa hay ảnh hưởng tiến trình đang chạy
1. **Transaction ngắn, không gọi mạng bên trong transaction.** Worker nhận việc (1 câu UPDATE, commit), rồi làm curl/S3/KV **ngoài transaction**, rồi ghi kết quả (1 câu UPDATE, commit). Không giữ khóa trong lúc chờ PHP dựng trang.
2. **Nhận việc nguyên tử, không cần `SKIP LOCKED`** (MariaDB 10.5 chưa hỗ trợ):
   ```sql
   UPDATE queue SET claimed_by=%(me)s, claimed_at=NOW()
    WHERE claimed_by IS NULL AND (not_before IS NULL OR not_before<=NOW())
    ORDER BY priority, id LIMIT 10;
   SELECT * FROM queue WHERE claimed_by=%(me)s;
   ```
   Hai worker không bao giờ nhận trùng việc. Việc bị nhận quá 15 phút (worker chết giữa chừng) được nhả lại tự động.
3. **Gom việc trùng**: nếu một URL đã có trong hàng đợi chưa được nhận thì không thêm nữa (mu-plugin dùng `INSERT ... SELECT ... WHERE NOT EXISTS`, worker lọc lại lần nữa). Sửa một bài 5 lần trong 1 phút chỉ tạo cache 1 lần.
4. **Các job khác không ghi chéo cột của nhau**: url_sync chỉ INSERT URL mới và thêm việc vào queue; worker là nơi **duy nhất** ghi `disk_at/s3_at/kv_at/status`; reconcile chỉ đọc rồi thêm việc vào queue, không sửa trực tiếp. Nhờ vậy không có hai tiến trình tranh nhau cùng một dòng.
5. **Ghi hàng loạt theo lô nhỏ** (url_sync, reconcile): tối đa 500 dòng mỗi transaction, `innodb_lock_wait_timeout` ngắn, lỗi deadlock thì thử lại.
6. **Đổi schema không cần dừng hệ thống**: các migration chỉ thêm cột hoặc index, chạy từ thư mục `migrations/` có đánh số.
7. **Chuyển dữ liệu từ SQLite một lần** (`tools/migrate_sqlite_to_mariadb.py`): chụp `.backup` SQLite, import vào `dott_cache.urls`, suy ra `blog_id/page_type` (sửa F3 theo dữ liệu WordPress), đối chiếu số lượng với KV (60.687).

## 4. Lấy URL mới và tự cập nhật vào DB + KV

### 4.1. Hướng sự kiện: mu-plugin `dott-cache-queue.php` (gần như tức thì)
| Hook | Khi nào | Việc đưa vào queue |
|---|---|---|
| `transition_post_status` → `publish` | Đăng bài mới, hoặc bài hẹn giờ (future) đến giờ đăng | `refresh` URL bài (ưu tiên 10) |
| `transition_post_status` từ `publish` sang trạng thái khác | Chuyển nháp, private, trash | `remove` URL bài (ưu tiên 10) |
| `post_updated` khi slug hoặc parent đổi | Đổi URL | `remove` URL cũ, `refresh` URL mới |
| `after_rocket_clean_post($post, $purge_urls, $lang)` | WP Rocket xóa cache khi sửa bài hoặc duyệt comment | `refresh` mọi URL trong `$purge_urls` (bài + trang chủ + archive liên quan), ưu tiên 10/20 |
| `after_rocket_clean_files($urls)` | Xóa cache theo URL (thủ công, plugin khác) | `refresh` các URL đó |
| `after_rocket_clean_domain` | Bấm "Clear all cache" | **Không** đưa 60 nghìn URL vào queue; chỉ ghi cờ `state.full_purge_at`, worker hiểu "tăng tốc renew", report gửi cảnh báo |
| `delete_term` / `edited_term` (slug đổi) | Đổi hoặc xóa chuyên mục, tag | `remove` hoặc `refresh` các URL archive |

- Mọi URL được chuẩn hóa thành path **trong PHP bằng đúng quy tắc của Worker** (lowercase, `/` cuối).
- Lỗi trong mu-plugin (ví dụ DB `dott_cache` không kết nối được) **được bỏ qua và ghi `error_log`**, không bao giờ làm hỏng thao tác của biên tập viên.
- Thay thế hook comment trong `ct-site-enhancer` (F12); xóa đoạn đó khi mu-plugin đã chạy ổn.

### 4.2. Quét định kỳ: `url_sync` (systemd timer mỗi 15 phút, lưới an toàn)
- Đọc `thai_posts` (vi) và `thai_2_posts` (en) có `post_modified_gmt > watermark` **hoặc** đăng/hẹn giờ trong khoảng đó, so với `urls` để bắt những thay đổi không đi qua hook (import SQL, sửa thẳng DB). Truy vấn chỉ đọc theo index, chạy trong vài trăm ms.
- Permalink lấy bằng `wp post list --post__in=... --field=url` (WP-CLI, theo lô) để đúng 100% với cấu trúc permalink thật, không tự ghép chuỗi.
- **Hằng đêm 2:00**: liệt kê đầy đủ bằng WP-CLI (logic của `wp_cli_extract_all_urls.py` chuyển lên server): trang chủ, page, archive và **toàn bộ phân trang**. Đối chiếu với `urls`: URL mới thì `refresh`, URL không còn thì `remove`. Bắt được các trang phân trang mới sinh khi có thêm bài.
- **Không ghi KV ở bước này.** KV được ghi bởi worker **sau khi upload S3 thành công**, để Worker Cloudflare không trỏ tới file chưa có.

### 4.3. Thứ tự ghi KV / S3 (quy tắc bất biến)
- **Thêm hoặc làm mới:** xóa S3 cũ (nếu có), để khách Mỹ tạm về origin và nhận bản mới thay vì bản cũ; tạo cache; upload S3; rồi `PUT` key KV (nếu chưa có).
- **Gỡ:** `DELETE` key KV trước, rồi xóa S3, rồi xóa disk, rồi đánh dấu `removed`.
- KV ghi theo lô qua **Cloudflare API** (`requests`, token trong `secrets.json`): gom tối đa 10.000 key/request, mỗi 1–5 phút một lần. Sau khi ghi, kiểm tra bằng list và thử lại (F17).

## 5. Các thành phần và lịch chạy

| Thành phần | Loại | Lịch | RAM dự kiến | Thay cho |
|---|---|---|---|---|
| `dott-cache-worker` | systemd service, `Restart=always` | liên tục | khoảng 60 MB | `cache_renew` (screen F1), `m4`, `cache_create`, `cache-urls-update`, 2 cron cũ |
| `dott-cache-sync` | systemd timer | 15 phút (nhanh) + 02:00 (đầy đủ) | khoảng 40 MB, vài giây | A1, u1, u2, KV_export/import |
| `dott-cache-reconcile` | systemd timer | Chủ nhật 03:30 | khoảng 100 MB, khoảng 10 phút | m0, m1, m2, m3, cache_clean |
| `dott-cache-report` | systemd timer | 07:00 hằng ngày | nhỏ | — (mới) |
| mu-plugin `dott-cache-queue.php` | PHP | theo sự kiện | 0 | hook comment trong site-enhancer |
| CLI `dott-cache` | lệnh tay | khi cần | — | `cache-segment-update` (`dott-cache enqueue --archive <url> --with-posts`) |

- **systemd timer thay cho cron**: một job đang chạy thì lần sau không khởi động chồng lên (chặn tận gốc kiểu lỗi F1); log vào journald (`journalctl -u dott-cache-*`); có `TimeoutStartSec` để giết job treo.
- Hạn mức (ví dụ cho worker): `MemoryMax=200M`, `CPUQuota=60%`, `Nice=10`, `IOSchedulingClass=idle`. Nếu vượt hạn thì chỉ worker bị giết rồi tự khởi động lại; MariaDB và PHP không bị ảnh hưởng.

### Worker tự điều tiết
- Tốc độ mặc định: khoảng 2–3 giây mỗi URL (mỗi URL gồm 2 lần dựng PHP: desktop và mobile).
- **Tạm dừng** khi: load average 1 phút > 1.5, `MemAvailable` < 250 MB, hoặc PHP-FPM có quá N tiến trình đang bận. Giờ cao điểm (theo GA4) thì giảm tốc độ renew, nhưng việc ưu tiên 10 (sửa bài) vẫn làm ngay.
- **Renew nền**: khi hàng đợi trống thì lấy URL có `s3_at` cũ nhất. Mục tiêu vòng renew được chỉnh bằng config (hiện khoảng 90 ngày; nếu cần nhanh hơn thì tăng tốc vào ban đêm).

## 6. Luồng xử lý chi tiết của worker (một URL)

```
refresh(path):
  1. s3.delete(4 file)               # khách Mỹ về origin trong lúc tạo lại (không thấy bản cũ)
  2. disk.delete(4 file)             # WP Rocket có thể đã xóa trước đó; xóa lại không sao
  3. trigger(desktop UA), trigger(iPhone UA)   # curl https://127.0.0.1<path> -H Host, timeout 30s
  4. verify: đủ 4 file, mtime >= lúc bắt đầu, kích thước > 2 KB, có </html>
       └─ lỗi → fail_count++, đưa lại vào queue với not_before = now + 2^n phút (tối đa 6h); lỗi quá 5 lần → report
  5. s3.upload(4 file, ACL public-read, gzip header)  → kiểm ok từng file (F13)
  6. kv.ensure(path)                 # gom lô, ghi theo chu kỳ
  7. UPDATE urls SET disk_at=s3_at=NOW(), fail_count=0 WHERE path=...
remove(path):  kv.delete → s3.delete → disk.delete → UPDATE status='removed'
```
Nếu bước 3–5 lỗi sau khi đã xóa S3 ở bước 1, Worker Cloudflare thấy S3 trả 404 và tự về origin, nên khách vẫn xem được trang (chậm hơn, nhưng không bao giờ thấy lỗi).

## 7. Reconcile hằng tuần (lưới an toàn cuối)
1. **DB và WordPress**: chạy liệt kê đầy đủ, thêm hoặc gỡ (giống sync hằng đêm).
2. **DB và disk**: URL `active` thiếu bất kỳ file nào trong 4 file thì `refresh` (thay m1/m4; sửa luôn trường hợp thiếu một nửa của F6).
3. **DB và S3**: list `_cache_html/` theo trang (khoảng 250 request, xử lý dần, không nạp hết vào RAM). URL thiếu file thì `refresh`. File trên S3 không thuộc URL `active` nào thì xóa (thay `cache_clean`; F7 cũng tự được dọn).
4. **DB và KV**: list toàn bộ KV (khoảng 61 request). Thiếu thì PUT, thừa thì DELETE, rồi list lại.
5. **Chặn an toàn**: nếu số thay đổi vượt ngưỡng (ví dụ > 2% tổng số URL) thì **không tự sửa**, chỉ báo cáo để người xem trước. Tránh xóa hàng loạt khi có lỗi logic hoặc API trả dữ liệu thiếu.

## 8. Cấu trúc repo mới (thay toàn bộ `server/`, `pc/`, `tools/` cũ)

```
dott-cache-management/
├── dott_cache/               # package Python 3.8+, chỉ cần requests, boto3, pymysql, psutil
│   ├── config.py             # đọc /etc/dott-cache/config.json + secrets.json (chmod 600)
│   ├── paths.py              # normalize_path (giống Worker), path → 4 file disk / 4 key S3
│   ├── db.py                 # MariaDB: claim/ack queue, cập nhật urls, retry deadlock
│   ├── wp.py                 # đọc thai_posts/thai_2_posts, gọi WP-CLI liệt kê URL
│   ├── cache.py              # trigger + verify trên disk
│   ├── s3.py                 # upload/delete/list (nhiều bucket theo config, F9)
│   ├── kv.py                 # Cloudflare API: bulk put/delete + kiểm tra + thử lại (F17)
│   ├── throttle.py           # load/RAM/PHP-FPM → tạm dừng hoặc tiếp tục
│   └── cli.py                # dott-cache worker|sync|reconcile|report|status|enqueue|pin
├── wp/mu-plugins/dott-cache-queue.php
├── migrations/001_init.sql
├── deploy/systemd/*.service, *.timer, deploy/install.sh
├── tools/                    # script chạy một lần: migrate_sqlite_to_mariadb.py, fix_f4_kv_keys.py...
├── tests/                    # test cho normalize_path, quy trình claim queue, kv diff...
└── docs/
```
Triển khai: `git pull` (deploy key chỉ đọc) vào `/opt/dott-cache`, rồi `deploy/install.sh` (cài unit, chạy migration). Toàn bộ code cũ và các file 1_/2_/3_ trên VPS được dọn (F8).

## 9. Lộ trình triển khai

| Giai đoạn | Nội dung | Rủi ro | Cách quay lại |
|---|---|---|---|
| **0. Chuẩn bị** | Kill 3 tiến trình treo và thêm timeout (F1). Kiểm tra RAM/swap. Thu hẹp token CF (F16). Gắn tag `v0-legacy` cho repo | Thấp | — |
| **1. Nền tảng** | Tạo DB `dott_cache`, migration, migrate SQLite sang MariaDB (kèm sửa F3). Viết package + `dott-cache status`. Viết test | Thấp (chỉ đọc hệ thống cũ) | Xóa DB `dott_cache` |
| **2. Worker** | Chạy worker ở chế độ `--dry-run` 1 ngày, rồi dừng renew (screen F1) và bật worker thật | Trung bình | Dừng service, chạy lại renew trong screen |
| **3. Sự kiện** | Cài mu-plugin, chạy url_sync. Thử: đăng bài thử, sửa bài, chuyển nháp, xem queue rồi S3 rồi KV. Tắt hook comment cũ | Trung bình | Gỡ file mu-plugin (WordPress chạy tiếp như cũ) |
| **4. Bỏ cron cũ** | Chuyển `url_list/clean_*.txt` và danh sách segment vào `pins`, gỡ 2 cron cũ (F2) | Thấp | Thêm lại dòng crontab |
| **5. Reconcile + report** | Chạy reconcile ở chế độ chỉ báo cáo 2 tuần, sau đó cho tự sửa (có chặn ngưỡng) | Thấp | Tắt timer |
| **6. Dọn dẹp** | Xóa `/root/tool/.../cache-tools` cũ (đã có backup), ngừng dùng thư mục PC, cập nhật skill `vps-diaocthongthai` | Thấp | Khôi phục từ tag `v0-legacy` |

## 10. Câu hỏi còn mở
1. **Kênh thông báo** cho report và cảnh báo: Telegram bot, email, hay chỉ log + `dott-cache status`?
2. **Khi bấm "Clear all cache"** (`rocket_clean_domain`) hoặc đổi theme/menu: tăng tốc renew toàn bộ (ví dụ hoàn tất trong 3–4 đêm), hay giữ nhịp thường?
3. **Mục tiêu vòng renew nền**: giữ khoảng 90 ngày, hay rút xuống (ví dụ 30 ngày) cho site en?
4. **Site `/es/`** (blog 3) có cần cache S3 không? (Hiện DB không có URL `/es/`.)
5. **Bucket Singapore cho HTML**: Worker chỉ dùng Chicago. Có muốn upload HTML lên cả 2 bucket để phòng Chicago gặp sự cố không (Worker sẽ cần fallback giống `thongthai-cdn-v2.js`)?
6. **Đồng bộ media sang AP** (F15): đang dùng cơ chế gì? Có cần đưa vào hệ thống này không?
