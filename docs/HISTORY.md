# Lịch sử phát triển – dott-cache-management

> Dành cho người hoặc agent mới vào repo: đọc file này để biết **hệ thống đi từ đâu tới đâu, vì sao đổi hướng, và những gì từng bị hiểu sai**.
> Trạng thái hiện tại và việc tiếp theo: [../HANDOFF.md](../HANDOFF.md).
> Chi tiết từng bước: [rollout-log.md](rollout-log.md).

## Tóm tắt 3 giai đoạn

| Giai đoạn | Thời gian | Nhánh / tag | Ý tưởng chính |
|---|---|---|---|
| **v0: pipeline Python cũ** | 2024 → 2026-11-25 | `main`, tag `v0-legacy` | Tool Python chạy rải rác ở PC và VPS chính: lấy URL bằng WP-CLI qua SSH, SQLite `cache_urls.db`, curl để tạo cache WP Rocket, boto3 upload lên S3 Chicago, wrangler ghi Cloudflare KV từ PC |
| **v1: thiết kế "server-only"** | 2026-11-25 | tag `v1-design` (chỉ là tài liệu) | Đưa mọi thứ về chạy trên VPS chính, bỏ phần PC. **Chưa triển khai**: bị thay bởi v2 vì VPS chính chỉ có 1 vCPU / 2 GB RAM |
| **v2: chia 2 máy, Go trên VPS phụ** | từ 2026-11-27 | nhánh `v2`, tag `v2-*` | VPS chính chỉ render (WP Rocket) và phục vụ file. VPS phụ `claude-control` chạy service Go `dott-cache`: hàng đợi, lấy HTML qua HTTP, upload S3 nhiều đích, Cloudflare KV, đối soát, báo Telegram |

## Dòng thời gian

### 2026-11-25: chụp lại hiện trạng, tìm lỗi
- `1658fe4`: snapshot toàn bộ tool trên VPS (`server/`) và PC (`pc/`), lúc đó chưa có trong git.
- Kiểm tra Cloudflare: token, KV REST API, Worker v7 (`67ab8b0`).
- **F4:** khóa KV phải là path đã chuẩn hóa, không phải URL đầy đủ. Đã sửa ở `4417107`.
- Danh sách lỗi **F1–F17** của pipeline cũ, audit việc đồng bộ S3, thiết kế v1 (`60ae427`, tag `v1-design`). Xem [findings-2026-11-25.md](findings-2026-11-25.md).
  - Các lỗi đáng nhớ: cron cũ không upload S3 (F2); `segment-update` gọi HTTP không có timeout nên bị treo (F1).

### 2026-11-26: tool tạm
- `tmp_update_en_map_of` làm mới khoảng 33k trang `/en/map-of-*/` (disk + S3). Chạy xong 27/11, chỉ 2 lỗi. Commit `52ea40f`…`ca53a57`.

### 2026-11-27 sáng: chuyển hướng sang v2
- Chủ site có VPS phụ (139.180.158.221, cùng datacenter Singapore, ping 0,43 ms), muốn dùng Go để giảm tải VPS chính.
- Thiết kế v2 (`9e03b58`). Quy tắc triển khai: **từng giai đoạn, test kỹ, hỏi trước mọi thao tác ghi trên production** (`446c858`).
- Tag `v0-legacy` được đặt ở cuối nhánh `main`. Nhánh `v2` tách ra từ đây.
- **Giai đoạn 0** (`d91b46e`):
  - cài Go 1.26.8 trên VPS phụ;
  - VPS phụ gọi origin, kiểm chứng chỉ bằng SPKI pin của Cloudflare Origin cert;
  - kill 3 tiến trình `segment-update` bị treo.
- **Giai đoạn 1** (`dc162c8`, tag `v2-phase1`): `.html` lấy qua HTTP **khớp md5 với file trên đĩa (40/40)**. Đây là cơ sở để VPS phụ lấy HTML qua HTTP thay vì scp/ssh.

### 2026-11-27 chiều: đo đạc
Commit `cce36f6`. Xem [findings-php-render-2026-11-27.md](findings-php-render-2026-11-27.md).
- PHP render mất 0,2–0,35 s/trang. Phần lớn (khoảng 180 ms) là bootstrap 50 plugin.
- **Tool cũ chậm (7,8 s/URL) vì upload S3 tuần tự**, mỗi lần mở kết nối mới, cộng thêm 1 s chặn để kiểm tra CPU. Không phải vì PHP.
- Cloudflare tự nén response do Worker trả về, nên `_gzip` trên S3 không bắt buộc.
- Prefork (nạp WordPress một lần, fork cho từng URL) chạy được.
- Ý tưởng "Go trung gian tính sẵn dữ liệu cho WordPress": **không đáng làm**.
- Tạo thư mục handoff `D:\Claude\2026-11-27 Cache System v2` (`1eea25b`, tag `v2-handoff-1`).

### 2026-11-27 tối: prototype
- **tt-core** (`3f3bb72`…`1603c90`): một file JS dùng chung cho desktop và mobile, build bằng PHP và có dashboard. Đã test trên php81. Xem [design-tt-core.md](design-tt-core.md).
- **Thiết kế 5b–5c** (`37f00f1`): một bản HTML cho cả desktop và mobile, nhiều đích S3 qua pool trên VPS phụ, công cụ đổi version CSS/JS.
- **Môi trường test php81.congtoan.net** (site 3 vi, site 4 en, 1.218 bài) và bucket test `congtoanphp` (`4109fd1`, `ea4d695`).
- **rocket-nginx** được bật cho diaocthongthai.com và php81 (`50dc4e1`). Trên production chưa có tác dụng vì thư mục cache còn `.mobile-active`.
- **Go uploader** `dott-cache upload`, chỉ dùng stdlib (`f72b34a`, `416da61`): 42,9 URL/s với 2 bucket × 16 luồng, 0 lỗi.
- mu-plugin **`dott-single-cache`** (`3d57b66`): một file cache cho cả desktop và mobile. Đã cài trên php81, chưa cài production.
- **Prefork ghi cache thật** (`406dfe9`, tag `v2-prototypes-2026-11-27`): 1.024 URL trong 73 s, so với 942 s khi render qua HTTP.

### 2026-11-28: tổng kết
- [SUMMARY-2026-11-27.md](SUMMARY-2026-11-27.md) gom lại toàn bộ đợt prototype: tt-core, sticky 3.2.0, một bản HTML, rocket-nginx, Go uploader, prefork, kèm số đo và việc tiếp theo.
- GitHub Release đầu tiên được tạo trên tag `v2-prototypes-2026-11-27`. Release chỉ có ghi chú, không kèm file.

## Những điều từng hiểu sai (đã đính chính)
Agent mới đọc tài liệu cũ nên biết các điểm này để khỏi lặp lại:
- "fail2ban đã tắt" → **sai**, nó vẫn chạy. Jail `sshd` nên giữ. Jail nginx vô tác dụng vì khách đi qua Cloudflare.
- "nginx không lấy được IP thật" → **sai**. Có `real_ip_header X-Forwarded-For` và các dải IP Cloudflare trong `/etc/nginx/extra/cloudflare.conf`.
- "nginx trả thẳng file `_gzip`" → **sai**. Thực tế nginx nén lại lúc gửi. Trước 27/11 tối **không có rocket-nginx**; file cache do `advanced-cache.php` (PHP) trả.
- "`Last-Modified` cho biết đó là file cache" → **sai**. Response render mới cũng có header này. Dấu hiệu đúng là dòng `cached@`.
- "Đặt `cache_mobile` = 0 để gộp thành một bản" → **sai**: khách mobile sẽ không được phục vụ cache. Cách đúng là mu-plugin `dott-single-cache`.

## Tag
| Tag | Commit | Ý nghĩa |
|---|---|---|
| `v0-legacy` | `701c930` | Cuối nhánh `main`: bản Python cũ cùng tài liệu thiết kế v1/v2 lúc tách nhánh |
| `v1-design` | `60ae427` | Danh sách F1–F17 và thiết kế server-only v1 (không triển khai) |
| `v2-phase1` | `dc162c8` | Giai đoạn 0–1 xong: lấy HTML qua HTTP khớp với đĩa |
| `v2-handoff-1` | `1eea25b` | Thư mục handoff đầu tiên, `legacy/` được tách riêng |
| `v2-prototypes-2026-11-27` | `406dfe9` | tt-core, Go uploader, single-cache, prefork: tất cả mới ở mức prototype trên php81 |
| `v2-tools-2026-11-28` | `a31fd57` | Bộ lệnh `dott-cache` (urls, update, replace, kv), Worker v2 test, bố cục `/root/tool/cache-system`, prefork và export nằm trong site. Đã test trên php81, production chưa chuyển. Kiểm kê: [INVENTORY-2026-11-28.md](INVENTORY-2026-11-28.md) |

Quy ước tag về sau:
- `v2-phaseN` khi xong một giai đoạn trong lộ trình (design mục 14);
- `v2.0.0` khi service chạy production ổn định, lúc đó merge `v2` vào `main`.

GitHub Release chỉ tạo cho mốc lớn (như `v2-prototypes-2026-11-27`, sau này là `v2.0.0`). Release chỉ có ghi chú, không kèm binary: binary Go được build trên VPS phụ.
