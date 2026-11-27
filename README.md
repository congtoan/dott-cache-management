# dott-cache-management – Cache HTML cho diaocthongthai.com

Hệ thống cache HTML (WP Rocket) cho diaocthongthai.com, chạy trên 2 máy:

- **VPS chính** (45.77.32.177): WordPress multisite render trang, WP Rocket ghi file cache, nginx phục vụ khách.
- **VPS phụ `claude-control`** (139.180.158.221): service **Go `dott-cache`** làm các việc nặng:
  - hàng đợi và lấy HTML qua HTTP;
  - upload lên **S3 nhiều đích** qua pool kết nối;
  - Cloudflare KV, đối soát S3 ↔ KV ↔ URL thật, báo Telegram.

Cloudflare Worker phục vụ HTML từ S3 cho khách ở xa, khách gần đi thẳng vào origin.

> **Bắt đầu ở đâu:**
> 1. [HANDOFF.md](HANDOFF.md): trạng thái hiện tại, việc tiếp theo, quy tắc an toàn.
> 2. [docs/SUMMARY-2026-11-27.md](docs/SUMMARY-2026-11-27.md): tổng kết đợt prototype (tt-core, sticky 3.2.0, một bản HTML, rocket-nginx, Go uploader, prefork) kèm số đo.
> 3. [docs/HISTORY.md](docs/HISTORY.md): lịch sử phát triển, vì sao đổi hướng, những điều từng hiểu sai.
> 4. [docs/design-server-only.md](docs/design-server-only.md): thiết kế v2.
> 5. [docs/rollout-log.md](docs/rollout-log.md): nhật ký từng giai đoạn.

## Nhánh và tag
| | |
|---|---|
| `v2` (nhánh này) | Hệ thống mới. Mọi công việc mới làm ở đây |
| `main` | Pipeline Python cũ (v0), **vẫn đang chạy cron trên production**. Sẽ được thay khi `v2` lên `v2.0.0` |
| tag `v0-legacy`, `v1-design`, `v2-phase1`, `v2-handoff-1`, `v2-prototypes-2026-11-27` | Các mốc, giải thích ở docs/HISTORY.md |

Repo **private**. Trên PC, nhánh `v2` là git worktree tại `D:\Claude\2026-11-27 Cache System v2`, còn `main` ở `D:\Claude\2026-11-25 Cache Management`.

## Trạng thái (2026-11-28)
- Thiết kế, đo đạc và **prototype** đã xong:
  - Go uploader;
  - prefork ghi cache;
  - mu-plugin một bản HTML;
  - tt-core;
  - rocket-nginx.
- Tất cả prototype mới test trên **php81.congtoan.net** với bucket test. Số đo chính:
  - một bản HTML cho mọi thiết bị: mỗi URL 1 lần render, 2 file thay vì 4;
  - Go uploader: 42,9 URL/s lên 2 bucket;
  - prefork: 1.024 URL trong 73 s, so với 942 s qua HTTP.
- **Production vẫn chạy pipeline cũ.** Chưa có service `dott-cache` chạy nền.
- Việc tiếp theo: service có hàng đợi (HANDOFF mục 5).

## Cấu trúc
```
HANDOFF.md, CLAUDE.md   ← đọc đầu tiên / hướng dẫn cho Claude Code
cmd/dott-cache/         ← Go: CLI/service (prototype: `upload`, `delete`, `renew`)
internal/origin/        ← Go: gọi origin với SPKI pin
internal/s3/            ← Go: S3 SigV4, chỉ dùng stdlib, pool nhiều đích
go.mod
wp/mu-plugins/          ← mu-plugin WordPress (dott-single-cache.php; sắp có dott-cache.php: sự kiện + REST)
deploy/nginx/           ← rocket-page-cache.conf (rocket-nginx)
theme/                  ← tt-core: engine build JS, module vi/en, functions.php mới (xem theme/README.md)
tools/prefork/          ← render hàng loạt bằng fork, ghi cache WP Rocket thật; renew.sh: script bọc cho `dott-cache renew`
tools/lab-2026-11-27/   ← script đo/thử dùng cho findings (chỉ đọc)
docs/
  SUMMARY-2026-11-27.md              ⭐ tổng kết đợt prototype, số đo, việc tiếp theo
  HISTORY.md                         lịch sử phát triển
  design-server-only.md              ⭐ thiết kế v2 (mục 5b–5c: một bản HTML, nhiều đích S3, đổi version CSS/JS)
  design-tt-core.md                  thiết kế tt-core
  rollout-log.md                     ⭐ nhật ký giai đoạn và số đo
  findings-php-render-2026-11-27.md  đo PHP render, prefork, nén Cloudflare
  findings-2026-11-25.md             lỗi F1–F17 của pipeline cũ
  batch-render-wp-rocket.md          đề xuất prefork ban đầu
  cloudflare.md                      token, KV REST API, Worker
  TODO_VPS.md                        việc tồn đọng VPS chính (bản sao)
legacy/                 ← tool Python cũ (server/, pc/) + docs cũ, chỉ để tham khảo
vps.py                  ← lối tắt SSH vào VPS chính (cần skill vps-diaocthongthai, gitignored)
secrets.example.json    ← mẫu; secrets.json thật bị gitignore
```

## Bí mật
`secrets.json`, `credentials.json` và `.claude/skills/vps-diaocthongthai/` đều bị gitignore, **không bao giờ commit**.
Key S3 của service nằm ở `/etc/dott-cache/s3.env` trên VPS phụ.
