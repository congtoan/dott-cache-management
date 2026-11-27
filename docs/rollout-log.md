# Nhật ký triển khai Cache System v2

Mỗi giai đoạn trong [design-server-only.md](design-server-only.md) mục 14 có một mục ở đây: làm gì, test gì, số liệu, vấn đề còn lại. Chỉ sang giai đoạn sau khi chủ site đã duyệt.

---

## Giai đoạn 0 – Chuẩn bị (2026-11-27, đang làm)

### Đã xong
- **Repo**
  - Tag `v0-legacy` (commit `701c930`) đánh dấu bộ tool Python cũ.
  - Nhánh `v2` đã push lên `origin`.
  - Thư mục PC `D:\Claude\2026-11-26 Cache System NEW - Server only` là **git worktree** của repo `dott-cache-management`, theo nhánh `v2`. Thư mục cũ `D:\Claude\2026-11-25 Cache Management` vẫn ở nhánh `main`.
  - `secrets.json` không có trong worktree (được gitignore). v2 không dùng file này.
- **Go trên VPS phụ**
  - `apt install golang-go` cho **go1.26.8 linux/amd64**, chiếm 77 MB.
  - Build lần đầu (chỉ thư viện chuẩn: `net/http`, `crypto/tls`) mất **77 giây, RAM tối đa 221 MB**, khi đặt `GOMEMLIMIT=200MiB GOFLAGS=-p=1`. Sau khi build, RAM VPS phụ vẫn còn khoảng 566 MB trống, `claude-rc` không bị ảnh hưởng.
  - Quy ước: luôn build với `GOMEMLIMIT=200MiB GOFLAGS=-p=1`.
- **Xác thực chứng chỉ origin**
  - Dùng **SPKI pin** (SHA-256 của public key, dạng base64) thay cho CA gốc của Cloudflare, nên không cần tải file CA.
  - Pin: `oqhm5KRTcPw6B6FNpgTMxww20FoxF3uoY1oBeD+TSAM=`. Đây là public key, không phải bí mật.
    - Lấy từ file `/etc/nginx/ssl/diaocthongthai.com/cloudflare_cert.pem` trên VPS chính (Cloudflare Origin Certificate, hết hạn **13/11/2038**).
    - Đã đối chiếu: khớp với chứng chỉ mà origin trả về trong handshake TLS.
    - Ghi chú: `cert.pem` (Let's Encrypt, hết hạn từ 01/2024) vẫn còn trong thư mục nhưng đã bị comment trong cấu hình nginx, không dùng nữa.
  - Đã test bằng chương trình Go:
    - Pin đúng: `HTTP/2.0 200`, `Content-Encoding: gzip`, có `Last-Modified`, 19.582 byte, **10 ms**, giống kết quả curl.
    - Pin sai: kết nối bị từ chối (`origin pin mismatch`).
  - Khi Cloudflare Origin Certificate được thay mới thì phải cập nhật pin trong config. Service sẽ báo lỗi rõ ràng và tạm dừng, không âm thầm bỏ qua kiểm tra.
- **Skill `vps-claude-control`** trên VPS phụ đã sửa (có backup `*.bak-2026-11-27`):
  - Ghi đúng là UFW đang bật, chỉ mở 22/tcp.
  - Thêm thông tin Go.
  - Ghi rõ SSH tunnel tới MariaDB không dùng được (`no-port-forwarding`).

### Còn lại của giai đoạn 0
- [ ] **Kill 3 tiến trình treo** `cron-cache-segment-update-v2.py` (PID 4482, 44223, 53896, đã treo 22–37 ngày; F1). **Chờ chủ site đồng ý.**
- [ ] **S3 key mới** chỉ có quyền trên bucket `cdn.diaocthongthai.com` và **Cloudflare token mới** chỉ có quyền Workers KV Edit: chủ site tự tạo trên trang quản trị. Chưa cần ngay, giai đoạn 1 chỉ đọc. Cần có trước giai đoạn 4–5.

### Việc không liên quan nhưng cần biết
- `tmp_update_en_map_of.py` (screen `en-map-of`) lúc 13:47 ngày 27/11 đã chạy tới 30.612/33.079 URL, có 2 lỗi. Không bị đụng tới.
