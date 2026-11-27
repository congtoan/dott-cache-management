# Thiết kế Cache System mới – bản nháp v2 (2026-11-27): chia 2 máy, viết bằng Go

> **Mục tiêu:** chạy tự động hoàn toàn, không còn bước chạy tay trên PC. Phần điều phối chuyển sang **VPS phụ `claude-control`** để giảm tải cho VPS chính.
> Bản này vẫn để thảo luận, chưa có code. Các lỗi F được nhắc tới nằm trong [findings-2026-11-25.md](findings-2026-11-25.md).
> Bản v1 (mọi thứ trên VPS chính, Python + MariaDB) vẫn xem được trong lịch sử git của file này (commit `ca53a57` trở về trước).

## 0. Khác gì so với v1

| Hạng mục | v1 (2026-11-25) | v2 (bản này) |
|---|---|---|
| Nơi chạy worker, lịch, đối soát | VPS chính | **VPS phụ** (139.180.158.221) |
| Ngôn ngữ | Python (boto3, pymysql, psutil) | **Go**: một file binary, RAM khoảng 15–40 MB |
| Lưu trạng thái | MariaDB `dott_cache` trên VPS chính | **SQLite (WAL) trên VPS phụ**. Chỉ một tiến trình Go ghi nên không còn lỗi khóa. Trên VPS chính chỉ còn một bảng sự kiện nhỏ |
| Lấy HTML để upload S3 | Đọc file trên đĩa (127.0.0.1) | **Gọi HTTP tới origin** và nhận đúng file cache do `advanced-cache.php` của WP Rocket trả về ([mục 5](#5-upload-html-lên-s3-khi-file-cache-nằm-trên-vps-chính)) |
| Biết URL nào thay đổi | mu-plugin INSERT vào MariaDB, worker đọc trực tiếp | mu-plugin INSERT vào bảng sự kiện, **VPS phụ hỏi qua REST** (`/wp-json/dott-cache/v1/...`) |
| Đo tải để tạm dừng | `psutil` trên chính máy chạy | **Netdata API** của VPS chính, thời gian render, dự phòng bằng `ssh main` ([mục 6](#6-tự-điều-tiết-theo-tải-vps-chính)) |
| Báo cáo | Chưa chọn kênh | **Telegram** (giống `/root/health` trên VPS phụ) |

Các quyết định của chủ site (25–27/11):
- Thông báo qua Telegram.
- Khi bấm "Clear all cache": mặc định chỉ xóa trên server, **không xóa S3**.
- `/es/` bỏ qua.
- **Không** upload HTML lên bucket Singapore.
- (27/11 tối) **Một bản HTML** cho desktop và mobile (tt-core). Upload S3 do pool Go trên VPS phụ làm. **Nhiều đích S3** (đang test Chicago và Frankfurt). Đổi version thì sửa HTML cache và purge Cloudflare. Test trên php81.congtoan.net, chưa đụng site chính. Xem mục 5b, 5c, 14b.
- Dùng repo `dott-cache-management`. Code mới nằm ở `D:\Claude\2026-11-27 Cache System v2`. Trên server: `/opt/dott-cache` (VPS phụ) và `/root/tool/diaocthongthai.com/cache-system` (phần nhỏ còn lại trên VPS chính, nếu có).

## 1. Nguyên tắc

1. **VPS chính chỉ làm những việc buộc phải làm ở đó:** render PHP (WP Rocket ghi file cache), phục vụ file, ghi sự kiện. Mọi việc còn lại (lập lịch, hàng đợi, S3, KV, đối soát, báo cáo) chạy ở VPS phụ.
2. **VPS phụ là "bộ não", không nằm trên đường phục vụ khách.** Nếu VPS phụ chết, website vẫn chạy bình thường (WP Rocket trên VPS chính, S3 và Worker Cloudflare vẫn như cũ); chỉ ngừng cập nhật S3/KV cho tới khi VPS phụ sống lại. Hàng đợi sự kiện vẫn được giữ trên VPS chính, nên không mất việc.
3. **Hướng sự kiện trước, quét định kỳ sau** (giống v1).
4. **Một tiến trình duy nhất tạo tải lên VPS chính.** Mọi request render đi qua một bộ giới hạn chung (mặc định 1–2 request cùng lúc). Upload S3 và gọi KV thì được chạy song song nhiều hơn, vì chúng không tốn tài nguyên VPS chính.
5. **Kiểm tra sau từng bước** (F13, F17).
6. **Không làm chậm WordPress:** hook PHP chỉ INSERT một dòng, không gọi mạng.
7. **Giữ nguyên Worker Cloudflare** (`thongthai-cache-v7.js`), cấu trúc S3 `_cache_html/` và key KV dạng path (đã sửa F4).
8. **Tôn trọng ngân sách RAM của VPS phụ:** Claude Remote Control dùng khoảng 250–400 MB. Mọi service mới tổng cộng **dưới 200 MB**, luôn đặt `MemoryMax`. Không đụng `claude-rc.service`.

## 2. Chia việc giữa hai máy

| Việc | VPS chính (45.77.32.177) | VPS phụ `claude-control` |
|---|---|---|
| Render PHP, WP Rocket ghi 4 file cache | ✅ bắt buộc | — |
| Phục vụ file cache cho khách (nginx + rocket-nginx) | ✅ | — |
| Ghi sự kiện thay đổi (mu-plugin) | ✅ 1 câu INSERT | — |
| Xóa cache trên đĩa | ✅ thực thi (WP Rocket `rocket_clean_files`) | ra lệnh qua REST |
| Hàng đợi, ưu tiên, retry, renew theo chu kỳ | — | ✅ |
| Trigger render desktop/mobile | nhận request | ✅ gửi request (có giới hạn) |
| Lấy HTML đã cache, upload S3 Chicago | phục vụ file tĩnh (rẻ) | ✅ |
| Ghi/xóa Cloudflare KV | — | ✅ |
| Database URL và trạng thái | — | ✅ SQLite |
| Đối soát DB ↔ WordPress ↔ S3 ↔ KV ↔ đĩa | trả danh sách (REST / `ssh main`) | ✅ |
| Báo cáo Telegram | — | ✅ |
| Log PHP/nginx: thu thập và phân tích | log nằm ở đây | ✅ đã có (`/root/health`, [mục 10](#10-log-php-và-nginx-đã-có-trên-vps-phụ)) |

**Tải VPS chính giảm được:**
- Hết các tiến trình Python cron và screen: mỗi tiến trình khoảng 60–150 MB RAM, đồng thời chặn tận gốc lỗi treo F1.
- Hết phần mã hóa TLS và AWS SDK khi upload S3, hết các lệnh gọi Cloudflare API.
- Hết việc ghi SQLite và các file log của tool (ổ đang đầy 87%).

**Không giảm được:** thời gian render PHP. Mỗi URL vẫn mất khoảng 2–4 giây CPU trên 1 nhân của VPS chính. Đây là phần nặng nhất, và chỉ có thể điều tiết (mục 6) chứ không chuyển đi được.

## 3. Kiến trúc tổng thể

```
 VPS CHÍNH (production)                                   VPS PHỤ claude-control
 ┌─────────────────────────────────────────┐              ┌──────────────────────────────────────────────────┐
 │ WordPress (PHP-FPM)                      │              │  dott-cache (Go, systemd, MemoryMax=120M)         │
 │  mu-plugin dott-cache.php                │              │                                                  │
 │   • hook publish/unpublish/slug/term/... │   REST/HTTPS │  ┌─ poller ──── GET /changes (30 s) ─────────┐   │
 │     → INSERT thai_dott_cache_events      │◄─────────────┼──┤             POST /ack                      │   │
 │   • REST /wp-json/dott-cache/v1/          │  token + IP  │  │                                             ▼   │
 │      changes, ack, urls, purge, health   │              │  │   SQLite /var/lib/dott-cache/state.db       │   │
 │                                          │              │  │   urls · queue · pins · runs · state        │   │
 │ nginx + rocket-nginx                     │   HTTPS      │  │                                             │   │
 │  file tĩnh wp-rocket/... (Last-Modified) │◄─────────────┼──┤ render-limiter (1–2 slot) ◄── throttle ──┐  │   │
 │                                          │  origin trực │  │   trigger → fetch gzip desktop + mobile    │  │   │
 │ Netdata :19999 (/api/v1/data)            │◄─────────────┼──┼───────────────────────────────────────────┘  │   │
 │                                          │              │  │ uploader (4–8 goroutine) ──► S3 Chicago        │
 │ sshd :8282 (key claude-control)          │◄─────────────┼──┤ kv-batcher (gom 1–5 phút) ──► Cloudflare KV    │
 └─────────────────────────────────────────┘  ssh main    │  │ reconcile (timer CN 03:30), report (07:00)     │
                                                           │  └─ Telegram ◄───────────────────────────────────┘
                                                           │  /root/health (đã có): log + Claude, 23:45         │
                                                           └──────────────────────────────────────────────────┘
```

## 4. Đường giao tiếp giữa 2 máy

Đã kiểm tra thật từ VPS phụ ngày 27/11 (chỉ đọc):

| Đường | Dùng cho | Đã kiểm tra | Lưu ý |
|---|---|---|---|
| **HTTPS origin** `--resolve diaocthongthai.com:443:45.77.32.177` (không qua Cloudflare) | Trigger render, lấy HTML đã cache | ✅ Một file cache được phục vụ trong **khoảng 9 ms**. Bản desktop và mobile khác nhau theo UA. Có header `Last-Modified` = mtime file cache. Không gửi `Accept-Encoding` thì nhận nguyên file `.html` (khớp md5 40/40); gửi gzip thì nginx nén lại lúc gửi, không trả file `_gzip` | Chứng chỉ là **Cloudflare Origin CA**. Go kiểm tra bằng **SPKI pin** của chứng chỉ (`cloudflare_cert.pem`, hạn 2038), không dùng CA và không bỏ qua kiểm tra (rollout-log, giai đoạn 0) |
| **REST mu-plugin** `/wp-json/dott-cache/v1/*` (cũng gọi thẳng origin) | Lấy sự kiện, danh sách URL, yêu cầu xóa cache đĩa | Chưa có, cần viết | Xác thực bằng header `Authorization: Bearer <token>` **và** chỉ chấp nhận IP `139.180.158.221` (kiểm tra `REMOTE_ADDR` trong PHP). nginx dùng `real_ip_header X-Forwarded-For` nhưng chỉ tin các dải IP Cloudflare (`/etc/nginx/extra/cloudflare.conf`). Vì vậy request đi thẳng từ VPS phụ có `REMOTE_ADDR` = 139.180.158.221, còn request qua Cloudflare mang IP của khách, không giả được IP VPS phụ |
| **Netdata API** `http://45.77.32.177:19999/api/v1/data?chart=system.cpu...` | Đo CPU/RAM VPS chính để tạm dừng | ✅ khoảng 30 ms | Khi khóa Netdata (TODO_VPS) thì nhớ **mở riêng cho IP VPS phụ** |
| **`ssh main`** (key `id_main_vps`) | Dự phòng đo tải; đọc danh sách file trên đĩa khi đối soát; thao tác khẩn | ✅ khoảng 0,6 s/lần | Key có `no-port-forwarding`, nên **không mở được SSH tunnel tới MariaDB** (khác checklist mục 4 trong skill `vps-claude-control`). Vì vậy v2 dùng REST, không đọc DB WordPress trực tiếp |

Giới hạn trên VPS chính cần biết:
- nginx `location /`: `limit_req zone=one` **10 request/giây, burst 20**, `limit_conn` **10 kết nối/IP**. Khi vượt ngưỡng nginx trả **403**. Cache system dùng tối đa khoảng 6 kết nối cùng lúc (1–2 render + vài fetch file tĩnh), nên nằm dưới ngưỡng. Code phải coi 403 là tín hiệu "chậm lại", không coi là lỗi URL.
- fail2ban: chủ site **không dùng fail2ban cho web** (site đi qua Cloudflare proxy, rule không hiệu quả). Kiểm tra ngày 27/11: service vẫn đang chạy, nhưng jail `nginx-404` chỉ đọc log congtoan.net và chỉ chặn cổng 80, nên **không ảnh hưởng** tới cache system. Không cần cấu hình gì thêm. Tình trạng fail2ban được theo dõi riêng ở `TODO_VPS.md` mục 8b.

## 5. Upload HTML lên S3 khi file cache nằm trên VPS chính

Đây là điểm khó nhất khi chuyển sang VPS phụ: WP Rocket ghi 4 file trên đĩa VPS chính, còn S3 cần đúng 4 file đó (`index-https.html`, `index-https.html_gzip`, `index-mobile-https.html`, `index-mobile-https.html_gzip`).

### Các phương án

| | A. Lấy qua HTTP từ origin (**đề xuất**) | B. Lấy file qua `ssh main` | C. Giữ một agent upload nhỏ trên VPS chính |
|---|---|---|---|
| Cách làm | Sau khi trigger, GET lại URL **không gửi `Accept-Encoding`** (desktop UA và iPhone UA), nhận nguyên file `.html` trên đĩa. VPS phụ tự tạo `_gzip` (gzip -9). **Đã kiểm chứng ngày 27/11:** 40/40 file `.html` khớp md5 với đĩa; nginx không trả file `_gzip` mà nén lại lúc gửi, nên không lấy bản gzip qua HTTP (xem rollout-log, giai đoạn 1) | `ssh main "cat file1 file2 ..."` hoặc `tar` theo lô, nhận về VPS phụ rồi upload | VPS phụ ra lệnh, agent trên VPS chính tự upload S3 như bây giờ |
| Tải VPS chính | Thấp: `advanced-cache.php` của WP Rocket `readfile()` file cache, khoảng 6–25 ms, **có dùng một worker PHP-FPM nhưng không bootstrap WordPress** (đính chính 27/11 tối, xem mục 5b) | Thấp–vừa: mỗi lô tốn một phiên SSH (mã hóa, fork process) | Như hiện tại: TLS tới S3 + SDK + RAM |
| Chính xác | `.html` giống từng byte với file trên đĩa (đã kiểm chứng). `_gzip` do VPS phụ tạo, giải nén ra giống hệt `.html` | Giống từng byte | Giống từng byte |
| Cần quyền gì | Không cần gì thêm | Đọc file bằng root qua SSH | Secret S3 vẫn nằm trên VPS chính |
| Băng thông | Khoảng 230 KB/URL (2 × 114 KB, chưa nén). 60 nghìn URL khoảng 14 GB mỗi vòng renew | Tương tự | Không đổi |

**Chọn A:**
- Rẻ nhất cho VPS chính, không cần thêm quyền.
- Dùng cùng một kết nối HTTP với bước trigger.

**Dùng B ở đâu:**
- Trong reconcile, để kiểm tra trên đĩa có đủ file hay không (danh sách file + mtime).
- Dự phòng khi A gặp lỗi bất thường.

### Luồng chi tiết của phương án A (một URL)
```
refresh(path):
  t0 = now
  1. purge:   POST /dott-cache/v1/purge {paths:[path]}   # VPS chính gọi rocket_clean_files(), xóa 4 file
  2. trigger: GET origin path, UA desktop   ─┐ đi qua render-limiter (1–2 slot), timeout 60 s
              GET origin path, UA iPhone    ─┘ đo thời gian render → đưa vào throttle (mục 6)
  3. fetch:   GET lại 2 lần, KHÔNG gửi Accept-Encoding (nhận nguyên file .html trên đĩa)
              phải thỏa: 200, không có Content-Encoding (Last-Modified KHÔNG đủ, xem điểm 2 bên dưới),
                         có </html>, có comment "Debug: cached@<ts>" với ts >= t0,
                         > 2 KB, bản mobile khác bản desktop
              → tự gzip -9 ra 2 file _gzip (khoảng 7 ms/file trên VPS phụ)
              └─ không thỏa → trigger lại 1 lần → vẫn lỗi: fail_count++, đưa lại queue với backoff
  4. upload:  4 object lên S3 Chicago, chạy song song (uploader pool), đúng Content-Type / Content-Encoding
              như tool cũ; kiểm tra ETag = MD5 từng file (F13)
  5. kv:      đưa path vào kv-batcher (PUT nếu chưa có)
  6. ghi SQLite: disk_at = s3_at = now, fail_count = 0
remove(path): KV delete → S3 delete 4 file → POST purge → status = 'removed'
```

**Vì sao không xóa S3 trước như v1:**
- Ở v1, bước "xóa S3 trước" giúp khách Mỹ tạm về origin trong lúc tạo lại cache.
- Ở v2, khoảng thời gian từ purge tới upload chỉ vài giây. Bản cũ trên S3 vẫn đúng trang, chỉ cũ hơn, nên **ghi đè luôn** là an toàn và đỡ được một lượt gọi S3.
- Riêng khi đổi slug hoặc gỡ bài thì vẫn xóa ngay (luồng `remove`).

**Phải kiểm tra ở giai đoạn 1, trước khi chạy thật:**
1. So `md5` của file lấy qua HTTP với file trên đĩa (qua `ssh main md5sum`) cho khoảng 50 URL (desktop/mobile, vi/en, trang chủ/bài/archive/phân trang).
2. Xác nhận response đầu tiên do PHP render (khi chưa có cache) **không bao giờ** được dùng để upload. Bước 3 luôn GET lại. **Đính chính sau khi đo trên php81 (27/11 tối):** response render mới (lần đầu, mất 0,6–1,6 s) **cũng có `Last-Modified`**, nên không dùng header này để phân biệt. Dấu hiệu đúng là **comment `Debug: cached@<ts>`**: chỉ có trong file WP Rocket đã ghi xuống đĩa, response render mới không có (đo 5 URL × desktop/mobile). Bước 3 đòi có `cached@` với `ts >= t0`.
3. Content-Type và Content-Encoding trên S3 phải giống hệt bản tool cũ đang upload, để Worker `thongthai-cache-v7.js` không bị ảnh hưởng.

## 5b. Cập nhật 27/11 tối: một bản HTML, nhiều đích S3, upload qua pool trên VPS phụ

Chủ site chốt hướng (27/11):
- **Một bản HTML** cho desktop và mobile. Theme dùng tt-core ([design-tt-core.md](design-tt-core.md)). Sau khi xử lý xong sticky sidebar, preload LCP và wpDataTables, thì tắt "Separate cache for mobile".
- **Upload S3 chuyển hẳn sang VPS phụ**, dùng Go: client S3 dùng chung, kết nối keep-alive, một pool goroutine. VPS chính chỉ báo "path X cần upload", còn pool lo phần còn lại.
- VPS phụ lấy HTML bằng **HTTP tới origin**, tức nhận đúng file cache đang phục vụ cho khách, không dùng SSH.

> **Đính chính (27/11 tối, đo trên production, chỉ đọc):** nginx **không có rocket-nginx** (`nginx -T` không có dòng nào chứa "rocket"; thư mục `/etc/nginx/rocket-nginx/` có nhưng không được include). File cache được **PHP trả** qua `wp-content/advanced-cache.php` của WP Rocket: `readfile()` cho `.html`, `readgzfile()` cho `_gzip` (giải nén rồi nginx nén lại, đúng như kết quả giai đoạn 1), và đặt `Last-Modified` = mtime. Một request tới `/en/about-us/` mất 6–8 ms, không bootstrap WordPress.
> Hệ quả: mỗi lần lấy HTML vẫn chiếm một worker PHP-FPM trong vài ms. Khi lấy hàng loạt phải giới hạn số kết nối đồng thời theo `pm.max_children` của pool, và theo dõi tải. Các chỗ trước đây ghi "nginx sendfile" đã được sửa.
- **Upload lên nhiều S3**, không chỉ một. Bucket test sắp có: Linode **Chicago** và **Frankfurt**. Chủ site sẽ gửi thông tin sau.

### Thay đổi so với mục 5
| | Mục 5 (sáng 27/11) | Bản này |
|---|---|---|
| File mỗi URL | 4 (`.html`, `_gzip` × desktop, mobile) | **1** `index-https.html`. Bỏ `_gzip`, vì Cloudflare tự nén response do Worker trả về (đã đo, findings mục 6) |
| Lấy HTML | 2 GET (desktop UA, iPhone UA) | **1 GET**, không gửi `Accept-Encoding` |
| Kiểm tra HTML | "bản mobile khác bản desktop" | Bỏ điều kiện này. Thay bằng: có `tt-core/core.js` |
| Đích S3 | Chicago `cdn.diaocthongthai.com` | **Danh sách đích** trong config. Mỗi đích có pool, retry và trạng thái riêng |

**Giai đoạn chuyển tiếp** (WP Rocket vẫn còn tách cache mobile, Worker v7 vẫn chọn key theo thiết bị): VPS phụ lấy bản desktop rồi upload **cùng nội dung** lên cả key desktop lẫn key mobile. Worker chưa cần sửa. Khi Worker đã chỉ dùng một key thì bỏ bước upload key mobile.

### Nhiều đích S3
- Config:
  ```json
  "targets": [
    {"id":"ord","endpoint":"https://us-ord-1.linodeobjects.com","bucket":"cdn.diaocthongthai.com","prefix":"_cache_html/","workers":8},
    {"id":"fra","endpoint":"https://eu-central-1.linodeobjects.com","bucket":"...","prefix":"_cache_html/","workers":8}
  ]
  ```
  Endpoint của Frankfurt sẽ xác nhận khi có thông tin bucket.
- Mỗi đích có **client và pool riêng**. Một đích chậm hoặc lỗi không chặn đích khác.
- SQLite thêm bảng `url_targets(path, target, etag, size, uploaded_at, fail_count, last_error)`. Một URL chỉ tính là "xong" khi mọi đích đang bật đều OK. Reconcile (mục 9) chạy riêng cho từng đích.
- HTML chỉ lấy từ origin **một lần**, giữ trong RAM rồi đẩy cho mọi đích. Không lấy lại cho từng đích.
- **Worker cần sửa** để chọn đích theo châu lục của khách (Bắc/Nam Mỹ → Chicago, châu Âu → Frankfurt) và xử lý trường hợp URL đã có ở đích này nhưng chưa có ở đích kia. Ví dụ: giá trị KV ghi danh sách đích đã có (`"ord,fra"`) thay vì chỉ cần có key. Việc này làm riêng, có test.

### Cách VPS chính báo cho VPS phụ
Ý của chủ site là VPS chính gửi path sang pool. Đề xuất cách làm cho chắc chắn:
1. **Nguồn sự thật là bảng sự kiện trên VPS chính** (mục 7.1): hook hoặc tool chỉ INSERT một dòng. VPS phụ lấy về qua REST `/changes` rồi `ack`. Không mất việc khi VPS phụ restart hay mạng chập chờn.
2. **Đánh thức ngay (tùy chọn):** VPS chính gửi một "ping" sang VPS phụ để nó lấy ngay, không phải đợi vòng polling 5–30 giây. Mất ping cũng không sao. Vì quy tắc "service bind `127.0.0.1`, UFW chỉ mở 22" (mục 12), ping chỉ được nghe trên **mạng nội bộ VPC của Vultr** (2 máy cùng datacenter Singapore), có token. Cần chủ site đồng ý bật VPC.
3. Tool hàng loạt (ví dụ đổi version, mục 5c) cũng ghi vào bảng sự kiện, với `action = 'upload'`: chỉ lấy và upload, **không render lại**.

### Tốc độ dự kiến khi upload hàng loạt (60 nghìn URL, 1 file/URL)
| Bước | Giới hạn | Thời gian |
|---|---|---|
| GET file cache từ origin | nginx `limit_req` **10 r/s** cho mỗi IP (mục 4) | khoảng **1 giờ 40 phút** |
| GET, sau khi miễn rate limit cho IP VPS phụ (`geo` + `map` trong nginx, **cần chủ site đồng ý**, `nginx -t` trước khi reload) | `advanced-cache.php` 6–25 ms/file, giữ 4 kết nối (mỗi kết nối chiếm một worker PHP-FPM) | khoảng 15–25 phút |
| PUT S3 cho mỗi đích | 8 goroutine, PUT trên kết nối sẵn khoảng 0,25–0,4 s (RTT Chicago khoảng 220 ms) | khoảng 30–50 phút cho mỗi đích, các đích chạy song song |
| Render lại (khi cache trên đĩa bị xóa) | PHP 0,2–0,6 s/URL, 1–2 slot | khoảng 9 giờ (một bản HTML) |

Các số trên là **ước tính**, sẽ đo lại trên php81 (mục 14b) và với bucket test.

## 5c. Đổi version CSS/JS: sửa HTML cache + purge Cloudflare

Chủ site (27/11): sửa `?ver=` ngay trong HTML cache vẫn cần thiết, vì **có hiệu lực ngay**, kể cả với khách đã có CSS/JS cũ trong cache trình duyệt (`max-age=31536000, immutable`). Purge Cloudflare dùng **kết hợp** với cách này.

Tool cũ `/root/tool/diaocthongthai.com/replace_cache_update_version.py` (và bản `_en`) được viết từ thời chưa có S3 và có 4 bản cache. Có mấy điểm cần làm lại (đã đọc code 27/11):
- **Ghi đè ngay trên file đang phục vụ** (mmap rồi resize). Khách có thể nhận file ghi dở.
- `time.sleep(0.1)` cho mỗi file với 32 luồng trên máy 1 vCPU: riêng thời gian ngủ đã khoảng 12 phút cho 237 nghìn file, lại tranh CPU với PHP-FPM.
- Sửa cả `_gzip` (giải nén rồi nén lại), là phần tốn CPU nhất, trong khi nginx **không bao giờ** trả file `_gzip` (giai đoạn 1).
- Không có chế độ chạy thử, không đếm trước. Sửa xong thì S3 vẫn giữ bản cũ.
- Điểm đúng, giữ lại: quét đệ quy (`rglob`), lấy `ver` = mtime của file tĩnh, xử lý cả URL có và chưa có `?ver=`.

Số file hiện có (đếm 27/11): 60.850 `index-https.html` + 57.923 `index-mobile-https.html` + 2 loại `_gzip`, tổng **237.547 file**. Sau khi chỉ còn một bản HTML và không còn `_gzip` thì còn khoảng **61 nghìn file**.

### Tool mới `dott-cache rewrite-assets` (Go, chạy trên VPS chính)
Phải chạy trên VPS chính vì file cache nằm ở đó.
1. **Đầu vào:** danh sách asset kèm `ver` mới. Ví dụ:
   - `generatepress_child/style.css` → mtime;
   - `generatepress_child/tt-core/core.js` → `ver` trong `core.manifest.json`;
   - tương tự cho `generatepress_en`.

   Có chế độ `--dry-run`: chỉ đếm số file sẽ đổi, in ví dụ 5 file.
2. **Chỉ sửa `index*.html`**, bỏ qua `_gzip`. Nếu `_gzip` còn tồn tại thì xóa hoặc để WP Rocket tự tạo lại (chủ site quyết định).
3. **Ghi file tạm trong cùng thư mục rồi `rename`**: thay một bước, không bao giờ có file ghi dở. Giữ owner và quyền như file cũ. Có tùy chọn giữ nguyên mtime (xem lưu ý về WP Rocket bên dưới).
4. Chạy `nice -n 19`, `ionice -c3`, đọc và ghi tuần tự (1–2 luồng là đủ, vì giới hạn ở đĩa).
5. **Đầu ra:** danh sách path đã đổi. Ghi vào bảng sự kiện `action = 'upload'` (theo lô, ví dụ 500 path mỗi INSERT). Pool trên VPS phụ upload lại lên mọi đích S3.
6. Sau đó purge Cloudflare cho các URL asset (cả `?ver=` cũ lẫn mới).

Lưu ý:
- Đổi mtime file cache có thể làm WP Rocket tính lại thời hạn cache (cần kiểm tra trên php81). Nếu giữ mtime cũ thì header `Last-Modified` cũng giữ nguyên.
- Thứ tự an toàn: deploy file tĩnh mới → chạy rewrite → upload S3 → purge Cloudflare.

## 6. Tự điều tiết theo tải VPS chính

Hàm cũ `check_cpu_usage_and_wait()` ([legacy/server/utils.py](../legacy/server/utils.py)) dùng `psutil.cpu_percent()`. Hàm này đo CPU của **chính máy chạy tool**, nên chạy trên VPS phụ thì sẽ đo sai máy. Bản Go thay bằng goroutine `throttle`, cứ 5–10 giây đọc một lần và bật/tắt cờ cho render-limiter:

| Tín hiệu | Nguồn | Ngưỡng mặc định |
|---|---|---|
| CPU busy (100 − idle, trung bình 10 s) | Netdata `system.cpu` | > 70% thì tạm dừng, < 55% thì chạy lại (có khoảng đệm để không bật/tắt liên tục) |
| RAM còn trống, swap | Netdata `mem.available`, `mem.swap` | `MemAvailable` < 250 MB thì tạm dừng |
| Thời gian render của chính request trigger | đo trực tiếp | Trung vị 20 request gần nhất > 3 lần mức bình thường, hoặc > 10 s, thì giảm còn 1 slot rồi tạm dừng |
| HTTP 403/429/5xx từ origin | đo trực tiếp | Lùi dần 30 s → 10 phút |
| Dự phòng khi Netdata không trả lời | `ssh main cat /proc/loadavg` mỗi 60 s | load 1 phút > 1,5 (máy 1 nhân) thì tạm dừng. Cả hai nguồn đều lỗi thì **tạm dừng để an toàn** |

- **Việc ưu tiên 10** (bài vừa sửa hoặc vừa đăng) được làm ngay cả khi tải ở mức vừa. Chỉ dừng khi tải ở mức "đỏ".
- **Renew nền** chỉ chạy khi tải thấp. Có cấu hình khung giờ chạy nhanh hơn vào ban đêm.

## 7. Lấy URL mới và tự cập nhật

### 7.1. Sự kiện trên VPS chính: mu-plugin `dott-cache.php`
Dùng bảng `thai_dott_cache_events` trong **database WordPress hiện có** (prefix của site chính), nên không cần tạo DB hay user MySQL mới:
```sql
CREATE TABLE thai_dott_cache_events (
  id         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  blog_id    TINYINT NOT NULL,
  path       VARCHAR(500) NOT NULL,          -- đã chuẩn hóa theo quy tắc của Worker
  action     ENUM('refresh','remove') NOT NULL,
  priority   TINYINT NOT NULL,               -- 10 sửa/đăng bài, 20 archive trang 1, 50 archive sâu
  reason     VARCHAR(64),
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```
| Hook | Việc ghi vào bảng sự kiện |
|---|---|
| `transition_post_status` → `publish` (gồm bài hẹn giờ đến giờ đăng) | `refresh` bài, trang chủ, archive trang 1 của chuyên mục và tag |
| `transition_post_status` từ `publish` sang trạng thái khác | `remove` URL bài, `refresh` archive liên quan |
| `post_updated` khi bài đang `publish` | `refresh` URL bài (**cần vì `wp-rocket-no-cache-auto-purge` đang bật**, WP Rocket không tự purge khi sửa bài; xem F11) |
| `post_updated` khi slug hoặc parent đổi | `remove` URL cũ, `refresh` URL mới |
| `after_rocket_clean_post`, `after_rocket_clean_files` | `refresh` các URL WP Rocket đã xóa (thủ công hoặc plugin khác). **Bỏ qua khi lệnh xóa đến từ chính endpoint `purge`**, để không tạo vòng lặp |
| `after_rocket_clean_domain` ("Clear all cache") | Chỉ ghi một sự kiện đặc biệt `full_purge`. Mặc định: **không xóa S3**, worker tăng tốc renew và gửi Telegram |
| `comment_post`, `wp_set_comment_status` → approved | `refresh` URL bài (thay hook trong `ct-site-enhancer`, F12) |
| `edited_term`, `delete_term` | `refresh` hoặc `remove` archive |

Lỗi trong mu-plugin chỉ ghi `error_log`, **không bao giờ làm hỏng thao tác của biên tập viên**.

### 7.2. REST endpoint (trong cùng mu-plugin)
| Endpoint | Việc làm |
|---|---|
| `GET /changes?after_id=N&limit=500` | Trả sự kiện mới |
| `POST /ack {up_to_id}` | Xóa sự kiện VPS phụ đã nhận (giữ thêm 7 ngày để soát lỗi rồi mới xóa) |
| `GET /urls?blog=1&type=post&modified_after=...&page=N` | Danh sách permalink thật, dùng `get_permalink()`/`get_term_link()` (không tự ghép chuỗi). Gồm cả phân trang của archive (dựa trên số bài và `posts_per_page`) |
| `POST /purge {paths:[...]}` | Gọi `rocket_clean_files()` cho tối đa 100 URL, đặt cờ "nguồn = dott-cache" để hook ở trên bỏ qua |
| `GET /health` | Phiên bản plugin, số sự kiện đang chờ, `MemAvailable` (dự phòng cho throttle) |

Bảo mật:
- Token dài (để trong `wp-config.php` dưới dạng hằng `DOTT_CACHE_TOKEN`, và trong `/etc/dott-cache/env` trên VPS phụ).
- Chỉ chấp nhận IP `139.180.158.221`; khi gọi qua Cloudflare thì trả 404.
- Không có endpoint nào trả nội dung nhạy cảm.

### 7.3. Quét định kỳ (lưới an toàn)
- **Mỗi 15 phút:** `GET /urls?modified_after=watermark` để bắt thay đổi không đi qua hook (import SQL, sửa thẳng DB).
- **Hằng đêm 02:00:** lấy danh sách đầy đủ theo trang (khoảng 60 nghìn URL, 500 URL mỗi request, có nghỉ giữa các trang). Đối chiếu với SQLite: URL mới thì `refresh`, URL không còn thì `remove` (có chặn ngưỡng, mục 9).
- Thay thế: A1, u1, u2, `wp_cli_extract_all_urls.py`, KV_export/import.

## 8. Trạng thái trên VPS phụ (SQLite)

- Lý do v1 chọn MariaDB là có nhiều tiến trình cùng ghi, và PHP cần ghi vào hàng đợi. Ở v2 cả hai lý do đều không còn: **chỉ một tiến trình Go ghi** (các goroutine gửi kết quả qua channel cho một writer duy nhất), còn PHP ghi vào bảng sự kiện trên VPS chính.
- Dùng `modernc.org/sqlite` (Go thuần, không cần cgo), chế độ WAL. Lệnh đọc (`dott-cache status`, báo cáo) chạy song song không bị chặn.
- Schema giữ như v1: bảng `urls` (path, blog_id, page_type, object, page_number, status, disk_at, s3_at, kv_at, fail_count, last_error), `queue`, `pins`, `state`, `runs`. Bỏ `claimed_by/claimed_at` vì không còn nhiều worker tranh việc.
- **Chuyển dữ liệu một lần:** lấy `cache_urls.db` trên VPS chính (bản `.backup`), import vào SQLite mới, suy ra `blog_id/page_type` (sửa F3), đối chiếu số lượng với KV (60.687).
- **Backup:** mỗi đêm `VACUUM INTO` một bản rồi upload lên S3 (prefix `_backup/dott-cache/`), giữ 14 bản. Nếu mất VPS phụ thì dựng lại từ bản này, hoặc chạy lại "quét đầy đủ" + reconcile.

## 9. Đối soát (reconcile) S3 ↔ KV ↔ URL thật

Chạy hằng tuần, Chủ nhật 03:30, ở chế độ chỉ báo cáo trong 2 tuần đầu. Phần lớn chạy trên VPS phụ, không tốn tải VPS chính:

| Bước | Nguồn | Tải VPS chính | Phát hiện → hành động |
|---|---|---|---|
| 1. URL thật ↔ SQLite | REST `/urls` (quét đầy đủ) | Thấp (PHP truy vấn, theo trang) | URL mới thì `refresh`; URL đã mất thì `remove` |
| 2. SQLite ↔ S3 | `ListObjectsV2` `_cache_html/` theo trang (khoảng 250 request), xử lý dạng stream | 0 | Thiếu bất kỳ file nào trong 4 file thì `refresh`; file thừa (không thuộc URL `active`) thì xóa (F7); ETag lạ hoặc file < 2 KB thì `refresh` |
| 3. SQLite ↔ KV | List KV (khoảng 61 request) | 0 | Thiếu thì PUT, thừa thì DELETE, rồi **list lại để kiểm tra** (F17: bulk delete có thể sót key) |
| 4. SQLite ↔ đĩa VPS chính | `ssh main` chạy `find .../wp-rocket -name 'index*' -printf '%P %T@\n'` dưới `ionice -c3 nice -n19`, stream về | Vừa (đọc thư mục 14 GB, chạy 1 lần/tuần lúc 03:30) | URL `active` thiếu file thì `refresh`; thư mục cache không thuộc URL nào thì chỉ báo cáo |
| 5. Kiểm tra ngẫu nhiên | Lấy 200 URL bất kỳ, so `md5` bản trên S3 với bản origin đang phục vụ | Rất thấp | Lệch thì `refresh` và đếm vào báo cáo |

**Chặn an toàn:** nếu số thay đổi vượt ngưỡng (mặc định > 2% tổng URL, hoặc > 500 lệnh xóa) thì **không tự sửa**. Chỉ gửi Telegram kèm file danh sách để chủ site xem trước. Cách này tránh xóa hàng loạt khi có lỗi logic hoặc API trả thiếu dữ liệu.

## 10. Log PHP và nginx (đã có trên VPS phụ)

Hiện đã chạy (chi tiết: `/root/work/2026-11-27_vps-log-va-giam-sat.md` trên VPS phụ):
- Cron `45 23 * * * /root/health/run.sh` trên VPS phụ.
- `collect-remote.sh` thu thập số liệu chỉ đọc qua `ssh main`, rồi `claude -p` phân tích theo `prompt.md`. Chỉ nhắn Telegram khi bất thường; Chủ nhật gửi bản tin "vẫn ổn".
- Trên VPS chính, logrotate riêng chạy lúc 0h05 (giữ 14 ngày, nén). Đã bỏ `log_process_daily.py` khỏi cron (cron này từng restart nginx/PHP mỗi đêm).

Cache system **không làm lại** phần này. Có 3 chỗ nối:
1. `dott-cache report` (07:00) dùng **cùng bot Telegram** (đọc chung `telegram.env`, hoặc file env riêng có cùng token), với tiêu đề riêng `[cache]` để dễ lọc.
2. `collect-remote.sh` có thể thêm một dòng `dott-cache status --json` để Claude thấy luôn tình trạng cache trong bản phân tích 23:45 (hàng đợi tồn, lỗi S3/KV, URL lỗi nhiều lần).
3. (Tùy chọn, sau này) Một module Go nhẹ chạy **gần thời gian thực**: mỗi 5 phút đọc phần mới của `php-error.log`/`error.log` (qua `ssh main tail -c +offset`) và chỉ cảnh báo ngay khi có **PHP Fatal** hoặc 5xx tăng đột biến. Phần phân tích sâu vẫn để Claude làm lúc 23:45. Cần chủ site quyết định có muốn hay không.

## 11. Thành phần Go và lịch chạy (VPS phụ)

Một binary `dott-cache` với nhiều lệnh con:

| Lệnh / unit | Loại | Lịch | RAM dự kiến | Thay cho |
|---|---|---|---|---|
| `dott-cache run` (`dott-cache.service`) | service, `Restart=always` | liên tục: poller 30 s, worker, uploader, kv-batcher, throttle | 20–60 MB (`MemoryHigh=80M`, `MemoryMax=120M`) | screen F1 `cache_renew`, `m4`, `cache_create`, `cache-urls-update`, 2 cron cũ (F2), `tmp_update_en_map_of.py` |
| `dott-cache sync --full` | timer | 02:00 | < 40 MB | A1, u1, u2, KV_export/import |
| `dott-cache reconcile` | timer | CN 03:30 | < 80 MB (stream, không nạp hết vào RAM) | m0–m3, `cache_clean` |
| `dott-cache report` | timer | 07:00 | nhỏ | (mới) |
| `dott-cache status` / `enqueue` / `pin` / `pause` / `resume` | lệnh tay | khi cần | — | `cache-segment-update` (`dott-cache enqueue --archive <url> --with-posts`) |
| mu-plugin `dott-cache.php` | PHP trên VPS chính | theo sự kiện | 0 | hook comment trong `ct-site-enhancer` |

**Lợi ích Go áp dụng cụ thể:**
- **RAM thấp, một file chạy:** build trên PC (`$env:GOOS="linux"; $env:GOARCH="amd64"; go build`), chép lên `/opt/dott-cache/bin/`. VPS phụ không cần cài Go, không có venv, không có lỗi wheel của Python 3.14.
- **Goroutine với giới hạn riêng cho từng loại việc:**
  - render-limiter **1–2** (tài nguyên quý nhất: CPU VPS chính)
  - fetch file tĩnh **2–4**
  - upload S3 **4–8** (băng thông VPS phụ → Chicago)
  - KV gom lô, 1 request mỗi 1–5 phút

  Nhờ vậy S3/KV chạy nhanh mà không làm tăng tải VPS chính. `context` + timeout cho mọi request mạng, nên không còn tiến trình treo như F1.
- **Tắt an toàn:** nhận SIGTERM thì ngừng nhận việc mới, làm xong URL đang dở, ghi SQLite rồi thoát. `systemctl restart` không làm mất việc.
- Thư viện dự kiến: `aws-sdk-go-v2` (endpoint Linode `us-ord-1`), `modernc.org/sqlite`, `net/http` chuẩn (Cloudflare API, REST, Netdata, Telegram), `golang.org/x/crypto/ssh` hoặc gọi `ssh` của hệ thống cho `ssh main`.

Unit systemd theo mẫu của skill `vps-claude-control` (`references/deploy-service.md`):
- User riêng `dott-cache`, `ProtectSystem=strict`, `ReadWritePaths=/var/lib/dott-cache`.
- Secret trong `/etc/dott-cache/env` (chmod 600).
- `CPUQuota=50%`, `Nice=10`.
- Riêng `ssh main` cần đọc key `/root/.ssh/id_main_vps`. Phương án: tạo **key thứ hai riêng cho dott-cache** trên VPS chính với `command=` giới hạn đúng 2 lệnh (`find` đối soát, `cat /proc/loadavg`), không dùng chung key của Claude (**cần chủ site đồng ý**).

## 12. Bí mật và bảo mật

| Bí mật | Nằm ở | Ghi chú |
|---|---|---|
| S3 Linode key | `/etc/dott-cache/env` (VPS phụ) | Nên tạo **key mới chỉ có quyền trên bucket `cdn.diaocthongthai.com`** thay vì copy key hiện tại. Thu hồi key cũ trên VPS chính sau khi chuyển xong |
| Cloudflare token | như trên | Token riêng, chỉ có quyền **Workers KV Storage:Edit** trên account (F16); không dùng chung token đã dán trong chat |
| `DOTT_CACHE_TOKEN` (REST) | `wp-config.php` + env VPS phụ | Sinh ngẫu nhiên 48 ký tự |
| Telegram bot | dùng chung với `/root/health` | |

- Claude trên VPS phụ đang bị cấm đọc `/root/.ssh/id_*`. Thêm `/etc/dott-cache/**` vào danh sách cấm trong `/root/.claude/settings.json`; **cần chủ site đồng ý** vì đây là thay đổi quyền.
- Service Go **không mở cổng nào ra ngoài**: chỉ gọi đi, không lắng nghe; endpoint `/health` nếu có thì chỉ bind `127.0.0.1`. UFW VPS phụ giữ như hiện tại (chỉ 22/tcp).

## 13. Cấu trúc repo mới

```
dott-cache-management/
├── cmd/dott-cache/main.go          # lệnh con: run | sync | reconcile | report | status | enqueue | pin | pause | resume
├── internal/
│   ├── config/                     # đọc /etc/dott-cache/config.json + env
│   ├── paths/                      # normalize (giống Worker + kv_key() của u2 v3.4), path → 4 key S3
│   ├── store/                      # SQLite: schema, migration, writer duy nhất
│   ├── origin/                     # HTTP tới origin: CA Cloudflare Origin, trigger, fetch + verify
│   ├── wpapi/                      # REST mu-plugin: changes, ack, urls, purge
│   ├── s3/                         # upload/delete/list theo lô, kiểm tra ETag
│   ├── kv/                         # Cloudflare bulk put/delete + list kiểm tra + thử lại (F17)
│   ├── throttle/                   # Netdata + thời gian render + ssh dự phòng
│   ├── reconcile/
│   └── notify/                     # Telegram
├── wp/mu-plugins/dott-cache.php    # hook sự kiện + REST endpoint
├── deploy/systemd/*.service, *.timer, deploy/install.sh
├── certs/cloudflare-origin-ca.pem
├── tools/                          # chạy một lần: import cache_urls.db, so md5 HTTP ↔ đĩa
├── legacy/                         # server/, pc/, tools/ Python cũ (giữ tới khi bỏ hẳn)
└── docs/
```
- Code mới phát triển ở `D:\Claude\2026-11-27 Cache System v2`, cùng repo GitHub `congtoan/dott-cache-management`, theo nhánh `v2`.
- Gắn tag `v0-legacy` trước khi dời code cũ vào `legacy/`.

## 14. Lộ trình triển khai

### Quy tắc triển khai (chủ site yêu cầu, 27/11)
1. **Làm từng giai đoạn một.** Mỗi giai đoạn phải **test kỹ và đạt tiêu chí "xong"** rồi mới sang giai đoạn sau. Không làm gộp 2 giai đoạn.
2. **Không ảnh hưởng hệ thống đang chạy:** pipeline cũ (screen `F1`, `en-map-of`, các cron) vẫn chạy song song cho tới giai đoạn 5. Bản mới chạy **chỉ đọc hoặc dry-run** trước, sau đó so kết quả với bản cũ.
3. Mọi thao tác **ghi** trên VPS chính (cài mu-plugin, sửa wp-config, dừng screen, sửa cron, kill process) đều phải **hỏi chủ site trước**, backup trước và có sẵn cách quay lại.
4. Cuối mỗi giai đoạn: ghi kết quả test vào `docs/rollout-log.md` (làm gì, test gì, số liệu, vấn đề còn lại), commit, rồi báo chủ site để duyệt sang giai đoạn tiếp theo.
5. Bật dần: số slot render bắt đầu từ 1; reconcile chỉ báo cáo trước; mỗi tính năng có cờ tắt nhanh (`dott-cache pause`, tắt timer, gỡ mu-plugin).

| Giai đoạn | Nội dung | Chạm vào VPS chính? | Cách quay lại |
|---|---|---|---|
| **0. Chuẩn bị** | Tag `v0-legacy`. Tạo S3 key và CF token mới, quyền hẹp. Lấy CA Cloudflare Origin. Kill 3 tiến trình treo (F1) | Có (kill process, **hỏi trước**) | — |
| **1. Đo và thử (chỉ đọc)** | Tool Go nhỏ: fetch 50 URL qua origin, so md5 với đĩa (mục 5); đo throttle Netdata; đo băng thông và thời gian | Chỉ đọc | — |
| **2. Nền tảng** | `dott-cache` với SQLite, import `cache_urls.db`, `status`, test cho `paths` (so với `kv_key()` Python) | Không | Xóa `/var/lib/dott-cache` |
| **3. mu-plugin** | Cài `dott-cache.php` (bảng sự kiện + REST), thêm `DOTT_CACHE_TOKEN` vào wp-config. Thử đăng/sửa/nháp bài và xem sự kiện | Có (**hỏi trước**, backup wp-config) | Xóa file mu-plugin; bảng sự kiện để lại không ảnh hưởng gì |
| **4. Worker dry-run** | `dott-cache run --dry-run` 1–2 ngày: nhận sự kiện, tính việc, **không** purge/upload/KV. So với pipeline cũ | Chỉ đọc | Dừng service |
| **5. Worker thật** | Đợi `en-map-of` xong. Dừng screen F1 (**hỏi trước**). Bật worker thật với render-limiter = 1, theo dõi tải 1 tuần rồi mới tăng lên 2 | Có | Dừng service, chạy lại screen F1 |
| **6. Bỏ cron cũ** | Chuyển `clean_*.txt` và danh sách segment vào `pins`, gỡ 2 cron cũ (F2) | Có (**hỏi trước**) | Thêm lại dòng crontab |
| **7. Reconcile + report** | Chế độ chỉ báo cáo 2 tuần, sau đó cho tự sửa (có chặn ngưỡng). Nối `status --json` vào `/root/health` | Chỉ đọc (find hằng tuần) | Tắt timer |
| **8. Dọn dẹp** | Thu hồi S3 key cũ trên VPS chính, dời code Python vào `legacy/`, cập nhật skill `vps-diaocthongthai` và `vps-claude-control` | Có (**hỏi trước**) | Khôi phục từ tag `v0-legacy` |

## 14b. Môi trường test: php81.congtoan.net

Chủ site (27/11): **chưa test trên site chính**. Mọi thử nghiệm dùng php81.congtoan.net. Site này là WordPress multisite dạng thư mục con, nằm **cùng VPS chính**, user `php81conZSI`.
- Site 3 `/tttest/`: theme `generatepress_child` có tt-core, đóng vai site vi. Site 4 `/tttest-en/`: theme `generatepress_en`.
- Mô phỏng site thật: chép các plugin chính từ diaocthongthai.com (WP Rocket, ct-multi-sticky-sidebar...) và khoảng 1.000 bài viết test.
- Bucket S3 test riêng (Chicago và Frankfurt, chủ site tạo). **Không** dùng bucket `cdn.diaocthongthai.com` để test.
- Vì cùng máy với production nên vẫn áp dụng quy tắc: hỏi trước các thao tác ghi ảnh hưởng chung, như **cấu hình nginx, PHP-FPM, cron**. Phần riêng của php81 (bài, plugin, theme) đã được chủ site cho phép.

## 15. Câu hỏi còn mở
1. **Mục tiêu vòng renew nền:** 30 ngày (đề xuất) hay giữ khoảng 90 ngày? Với 60 nghìn URL, mỗi URL 2 lần render khoảng 3 s, một vòng 30 ngày cần khoảng 2.000 URL/ngày, tức khoảng 1,7 giờ render mỗi ngày.
2. **Cảnh báo log gần thời gian thực** (mục 10.3): có cần không, hay bản phân tích 23:45 là đủ?
3. **Key SSH thứ hai cho dott-cache** có `command=` giới hạn (mục 11): đồng ý tạo, hay chỉ dùng REST và bỏ bước 4 của reconcile?
4. **S3 key và CF token mới, quyền hẹp:** chủ site tự tạo trên Linode/Cloudflare (đề xuất), hay dùng tạm key hiện tại?
5. **Mở Netdata cho IP VPS phụ** khi khóa Netdata (TODO_VPS mục 2): đồng ý đưa vào giai đoạn 0? (Không cần cấu hình fail2ban, xem mục 4.)
6. **Đồng bộ media sang AP** (F15): giữ ngoài hệ thống này (Media Cloud và `ct-media-s3-upload` đang lo), hay cần đưa phần đối soát media vào reconcile?
7. **Ping tức thì từ VPS chính sang VPS phụ** (mục 5b): bật VPC nội bộ của Vultr, hay chỉ dùng polling 5–30 giây?
8. **Miễn rate limit nginx cho IP VPS phụ** (mục 5b): đồng ý thì việc upload hàng loạt nhanh khoảng 5 lần.
9. **Worker chọn đích S3 theo châu lục** (mục 5b): ngoài Bắc/Nam Mỹ → Chicago, châu Âu → Frankfurt, có thêm vùng nào không? Giá trị KV có ghi danh sách đích không?
10. **`_gzip` trên đĩa** (mục 5c): khi rewrite thì xóa, hay để nguyên?
