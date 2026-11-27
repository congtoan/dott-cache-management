# Cache System v2 – diaocthongthai.com

Hệ thống cache HTML mới cho diaocthongthai.com.
- **VPS chính:** WP Rocket render và ghi cache, nginx phục vụ khách.
- **VPS phụ `claude-control`:** một service **Go** lo hàng đợi, upload S3 Chicago, ghi Cloudflare KV, đối soát và báo Telegram.

> **Hội thoại mới: đọc [HANDOFF.md](HANDOFF.md) trước.**

Repo: `congtoan/dott-cache-management` (private). Thư mục này là **git worktree, nhánh `v2`**. Nhánh `main` (code Python cũ) nằm ở `D:\Claude\2026-11-25 Cache Management`; tag `v0-legacy` đánh dấu bản cũ.

## Cấu trúc
```
HANDOFF.md        ← trạng thái hiện tại, việc tiếp theo, quy tắc (đọc đầu tiên)
CLAUDE.md         ← hướng dẫn cho Claude Code khi mở thư mục này
vps.py            ← lối tắt: python vps.py run|ls|cat|get|put|sql|info  (SSH vào VPS chính)
secrets.json      ← S3 / Cloudflare / MySQL / WP REST (gitignored, KHÔNG commit)
.claude/skills/
  vps-diaocthongthai/   ← skill VPS chính (gitignored: có mật khẩu)
  vps-claude-control/   ← skill VPS phụ (bản gốc: /root/work/skills/vps-claude-control/ trên VPS phụ)
docs/
  design-server-only.md            ⭐ thiết kế v2 (2 máy, Go), lộ trình giai đoạn 0–8
  rollout-log.md                   ⭐ nhật ký từng giai đoạn: đã làm gì, số đo, việc còn lại
  findings-php-render-2026-11-27.md  đo PHP render, prefork, test nén Cloudflare, nhận định ý tưởng "Go trung gian"
  batch-render-wp-rocket.md        tài liệu đề xuất prefork (từ một hội thoại Claude khác)
  findings-2026-11-25.md           danh sách lỗi F1–F17 của pipeline cũ
  cloudflare.md                    token, KV REST API, Worker
  TODO_VPS.md                      việc tồn đọng của VPS chính (bản sao; bản gốc ở D:\Claude\2026-11-25 Ket Noi VPS)
tools/lab-2026-11-27/   ← script đo/thử (chỉ đọc) đã dùng cho findings
legacy/                 ← tool Python cũ (server/, pc/) + docs cũ, chỉ để tham khảo khi viết bản Go
data/                   ← dữ liệu cục bộ (gitignored)
```
Code Go sẽ nằm ở `cmd/`, `internal/`, `wp/`, `deploy/` (design mục 13). Hiện chưa có.
