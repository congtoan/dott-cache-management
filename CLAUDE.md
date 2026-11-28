# Hướng dẫn cho Claude Code – Cache System v2

- Trả lời chủ site bằng **tiếng Việt**.
- Bắt đầu mỗi hội thoại: đọc `HANDOFF.md` và `docs/INVENTORY-2026-11-28.md`. Các tài liệu khác (`docs/rollout-log.md` rất dài, `docs/design-server-only.md`, `docs/HISTORY.md`) chỉ đọc phần cần tra cứu.
- Skill có sẵn trong `.claude/skills/`: `vps-diaocthongthai` (VPS chính, có mật khẩu) và `vps-claude-control` (VPS phụ).

## Quy tắc an toàn (bắt buộc)
1. **VPS chính 45.77.32.177 là production.** Chỉ đọc thì cứ làm. Ghi, xóa, restart, sửa cấu hình, SQL ghi, kill process, sửa cron, cài/tắt plugin thì **phải hỏi chủ site trước**, và backup trước khi sửa (`cp f f.bak-$(date +%F)`).
2. Triển khai v2 **từng giai đoạn** (P2–P8 trong HANDOFF mục 4; chạy cho site vi trước, en sau). Mỗi giai đoạn test kỹ, ghi kết quả vào `docs/rollout-log.md`, commit, rồi báo chủ site duyệt trước khi sang giai đoạn sau. Bản mới chạy chỉ đọc hoặc dry-run trước.
3. VPS phụ: được cài và thử nghiệm. **Không đụng `claude-rc.service`/tmux `claude`**, không nới quyền trong `/root/.claude/settings.json`, service mới bind `127.0.0.1` và luôn có `MemoryMax`.
4. Không in mật khẩu, key hay token ra hội thoại. Không commit `secrets.json`, `credentials.json`, `.claude/skills/vps-diaocthongthai/`.
5. Không chạy Claude với `--dangerously-skip-permissions` trên máy có quyền SSH root vào production.
6. Đo trước khi kết luận. Khi số đo mâu thuẫn với giả định thì báo lại, không cố làm cho khớp.

## Git
- Nhánh `v2` (thư mục này là git worktree; nhánh `main` ở `D:\Claude\2026-11-25 Cache Management`).
- Repo private này commit theo git config sẵn có của máy. Tên `Cong Toan` và email noreply `10488568+congtoan@users.noreply.github.com` là quy ước cho **2 repo plugin public**.
- Push lên `origin` (repo private) sau mỗi giai đoạn.
