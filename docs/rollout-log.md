# Nhật ký triển khai Cache System v2

Mỗi giai đoạn trong [design-server-only.md](design-server-only.md) mục 14 có một mục ở đây: làm gì, test gì, số liệu, vấn đề còn lại. Chỉ sang giai đoạn sau khi chủ site đã duyệt.

---

## Giai đoạn 0 – Chuẩn bị (2026-11-27, đang làm)

### Đã xong
- **Repo**
  - Tag `v0-legacy` (commit `701c930`) đánh dấu bộ tool Python cũ.
  - Nhánh `v2` đã push lên `origin`.
  - Thư mục PC `D:\Claude\2026-11-26 Cache System NEW - Server only` (từ 27/11 tối đã **chuyển** thành `D:\Claude\2026-11-27 Cache System v2` bằng `git worktree move`) là **git worktree** của repo `dott-cache-management`, theo nhánh `v2`. Thư mục cũ `D:\Claude\2026-11-25 Cache Management` vẫn ở nhánh `main`.
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
- [x] **Kill 3 tiến trình treo** `cron-cache-segment-update-v2.py` (PID 4482, 44223, 53896, đã treo 22–37 ngày; F1). Chủ site đồng ý; làm lúc khoảng 14:00 ngày 27/11.
  - Cả 3 dừng bằng SIGTERM, không cần SIGKILL. Tiến trình cha (cron sh) cũng thoát.
  - `en-map-of` (PID 31442) vẫn chạy bình thường.
  - Chưa sửa timeout trong script, nên cron 3:45/15:45 vẫn có thể treo lại. v2 sẽ thay cron này ở giai đoạn 6.
  - Phát hiện thêm: screen `F1` hiện **chỉ còn một bash rảnh**, `cache_renew_from_db.py` **không còn chạy** (thời điểm dừng chưa rõ; trong log của `cache-tools` chỉ có log en-map-of). Tức là hiện **không có renew nền** nào đang chạy.
- [ ] **S3 key mới** chỉ có quyền trên bucket `cdn.diaocthongthai.com` và **Cloudflare token mới** chỉ có quyền Workers KV Edit: chủ site tự tạo trên trang quản trị. Chưa cần ngay, giai đoạn 1 chỉ đọc. Cần có trước giai đoạn 4–5.

---

## Giai đoạn 1 – Đo và thử (2026-11-27, kiểm tra nhanh xong)

### Kiểm tra: nginx có trả đúng file WP Rocket trên đĩa không
Câu hỏi được kiểm tra: khi VPS phụ gọi một URL đã có cache, nội dung nhận về có **giống hệt file đang nằm trên đĩa** không? Phép kiểm tra không so với bản PHP render mới, cũng không so với S3, nên chuyện cache cũ hơn thực tế không ảnh hưởng.

- **Mẫu:** 20 URL gồm trang chủ vi và en, bài viết vi, page en, archive và archive phân trang (vi `/ban-tin-du-an/page/N/`, en `/maps-of-all-philippine-provinces/page/N/`). Tránh `/en/map-of-*` vì en-map-of đang chạy.
- **Cách làm (chỉ đọc):** VPS phụ gọi 4 request cho mỗi URL: desktop/mobile, có và không có `Accept-Encoding: gzip`. md5 của file trên đĩa được đọc qua `ssh main` **trước và sau** khi gọi, để loại trường hợp file bị thay giữa chừng (không có trường hợp nào).
- **Kết quả:**

| | Kết quả |
|---|---|
| Bản `.html` desktop và mobile (gọi không có gzip) | ✅ **40/40 khớp md5** với `index-https.html` và `index-mobile-https.html` trên đĩa. Có `Last-Modified`, bản mobile khác bản desktop, mỗi request khoảng 8–25 ms (không gọi PHP) |
| Bản gzip (gọi có `Accept-Encoding: gzip`) | ⚠️ 40/40 **lệch md5** với file `_gzip` trên đĩa, nhưng **giải nén ra thì giống hệt file `.html` trên đĩa**. Nghĩa là nginx **không trả file `_gzip`** mà nén file `.html` ngay lúc gửi (`gzip on`, `gzip_comp_level 2` trong `/etc/nginx/extra/gzip.conf`). File HTTP nhận được lớn hơn khoảng 12% so với `_gzip` trên đĩa |

**Kết luận và điều chỉnh thiết kế (design mục 5):**
- Lấy qua HTTP **đúng nội dung** file cache, nên phương án A dùng được.
- Chỉ lấy **2 bản `.html`** (desktop và mobile), **không gửi `Accept-Encoding`**. ~~nginx gửi file tĩnh bằng sendfile~~ **Đính chính 27/11 tối:** không có rocket-nginx. File được `advanced-cache.php` của WP Rocket trả bằng `readfile()` (khoảng 6–8 ms, không bootstrap WordPress). Có tốn một worker PHP-FPM trong vài ms, nhưng vẫn rẻ. Xem design mục 5b.
- VPS phụ **tự tạo `_gzip`** (gzip -9) trước khi upload S3. Đo được: 40 file trung bình 114 KB mất 293 ms tổng cộng, tức khoảng 7 ms mỗi file trên VPS phụ. Bản gzip -9 nhỏ hơn bản nginx level 2 khoảng 13%.
- Băng thông: khoảng 2 × 114 KB = **khoảng 230 KB mỗi URL**. 60 nghìn URL thì khoảng **14 GB mỗi vòng renew**. Với vòng 30 ngày, con số này nhỏ so với hạn mức băng thông của Vultr.
- `_gzip` trên S3 không còn giống từng byte với `_gzip` trên đĩa VPS chính, nhưng **giải nén ra giống hệt nhau**. Worker Cloudflare chỉ cần một file gzip hợp lệ.

**Phát hiện thêm (không thuộc cache system, chỉ ghi lại):**
- rocket-nginx có `location ~ ..._gzip$`, nhưng request gzip thực tế vẫn được nginx nén lại từ file `.html`. Tức là **mỗi lượt khách qua Cloudflare đều tốn CPU nén** trên VPS chính, dù WP Rocket đã có sẵn file `_gzip`.
- Nếu muốn tiết kiệm CPU VPS chính thì có thể xem lại cấu hình rocket-nginx sau. Cần chủ site quyết định; chưa đụng tới.

### Việc không liên quan nhưng cần biết
- `tmp_update_en_map_of.py` (screen `en-map-of`) lúc 13:47 ngày 27/11 đã chạy tới 30.612/33.079 URL, có 2 lỗi. Không bị đụng tới.
