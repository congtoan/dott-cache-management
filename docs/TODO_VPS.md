# TODO – VPS DiaOcThongThai.com

> Các vấn đề phát hiện khi rà soát VPS ngày 2026-11-25. **Chưa sửa gì.** Đánh dấu `[x]` khi xử lý xong.
> Mỗi mục ghi: hiện trạng → rủi ro → cách xử lý đề xuất → lệnh kiểm tra.

## 🔴 Ưu tiên cao

### [ ] 1. Ba tiến trình `cron-cache-segment-update-v2.py` bị treo
- **Hiện trạng:** PID 4482, 44223, 53896 đã chạy 20 đến 35 ngày, mỗi tiến trình kẹt một kết nối ESTAB tới Cloudflare (443).
- **Nguyên nhân:** script gọi `requests.get(...)` mà không có `timeout=`, nên khi kết nối treo thì chờ mãi. Nhánh `except` cũng lại gọi `requests.get` không timeout.
- **Rủi ro:** các lượt cập nhật tag/archive bị bỏ dở. Mỗi ngày có thể thêm 2 tiến trình treo (cron 3:45 và 15:45).
- **Xử lý:**
  1. `kill 4482 44223 53896`
  2. Sửa `/root/tool/diaocthongthai.com/cron-cache-segment-update-v2.py`: thêm `timeout=30` vào mọi `requests.get`. Có thể thêm cơ chế chống chạy chồng bằng `flock` trong crontab:
     `45 3,15 * * * flock -n /tmp/segment.lock python3 /root/tool/.../cron-cache-segment-update-v2.py`
- **Kiểm tra:** `ps -eo pid,etime,args | grep cron-cache-segment | grep -v grep`
- **Cập nhật 2026-11-27 ~14:00:** ✅ bước 1 đã xong (chủ site đồng ý). 3 PID dừng bằng SIGTERM, tiến trình cha (cron sh) cũng đã thoát. ⏳ Bước 2 (timeout/flock) **chưa làm**, nên cron 3:45/15:45 vẫn có thể sinh tiến trình treo mới. Cache System v2 sẽ thay hẳn cron này (giai đoạn 6).

### [ ] 2. Netdata đang mở công khai, không cần đăng nhập, phiên bản rất cũ
- **Hiện trạng:** Netdata v1.19.0 (2019) có `bind socket to IP = 45.77.32.177` trong `/etc/netdata/netdata.conf`, UFW rule `19999 ALLOW IN Anywhere` (#20, #40). Ai cũng xem được http://45.77.32.177:19999/
- **Rủi ro:** lộ tiến trình, dịch vụ, tải hệ thống và cấu trúc server. Bản cũ có thể còn lỗ hổng.
- **Xử lý (chọn 1):**
  - (Khuyến nghị) đổi bind sang `127.0.0.1`, xóa rule UFW 19999, rồi xem qua SSH tunnel: `ssh -p 8282 -L 19999:127.0.0.1:19999 root@45.77.32.177`, sau đó mở http://localhost:19999
  - Hoặc giới hạn UFW chỉ cho IP nhà: `ufw delete <số>`, rồi `ufw allow from <IP> to any port 19999`
  - Cân nhắc nâng cấp Netdata.

### [ ] 3. RAM/swap ở mức CRITICAL
- **Hiện trạng:** RAM 1.9 GB, swap dùng khoảng 1.1 GB, tức bằng 68% RAM (Netdata báo CRITICAL `system.swap.ram_in_swap`).
- **Nguyên nhân có thể:** `cache_renew_from_db.py` chạy liên tục (screen F1), các tiến trình treo ở mục 1, MariaDB, PHP-FPM và Redis cùng chạy trên 2 GB RAM.
- **Xử lý:** xem `ps aux --sort=-rss | head`. Cân nhắc giảm `innodb_buffer_pool_size`, giới hạn `pm.max_children` của PHP-FPM, hoặc nâng VPS lên 4 GB.

### [ ] 4. Ổ đĩa đầy 88% (còn khoảng 5.7 GB)
- **Hiện trạng:** `/dev/vda2` 47G, dùng 39G. Riêng `/home/diaocthDP4f` chiếm 21G.
- **Ứng viên dọn:**
  - `/home/diaocthDP4f/diaocthongthai.com/public_html/coastlines-split-4326.zip` (857 MB, root, nằm ngay trong web root nên **ai cũng tải được qua URL**)
  - Bảng `thai_2_blc_links` (621 MB) và `thai_blc_links` (262 MB) của Broken Link Checker, `thai_defender_lockout_log` (106 MB)
  - `/root/13909383-720p.mp4` (30 MB), `/root/tool/diaocthongthai.com/import_done/` (khoảng 100 MB xlsx), `/root/tool/diaocthongthai.com/s3_sync/manifest-*`
- **Kiểm tra:** `du -xh / --max-depth=3 2>/dev/null | sort -h | tail -30`
- **Cập nhật 2026-11-26 (một phần):** ổ từ 85% xuống **81%** (trống 8,6 GB). Đã xóa `coastlines-split-4326.zip`, vacuum journal (648 MB), làm rỗng `/var/log/btmp` và `/var/log/nginx/error.log`, sửa cron `clean_logs.sh` (trước đây chưa từng chạy, dự kiến giải phóng thêm ~1,9 GB), tạo logrotate cho nginx. Chi tiết: `references/disk-cleanup-2026-11-26.md` trong skill. **Còn lại:**
  - [ ] Bảng Broken Link Checker và `thai_defender_lockout_log` (xem ở trên)
  - [ ] `wp-content/uploads/wpallimport` (357 MB), `wpallexport` (137 MB): file nhập/xuất cũ, cần xem trước khi xóa
  - [ ] Giới hạn journal vĩnh viễn: `SystemMaxUse=200M` trong `/etc/systemd/journald.conf`, rồi `systemctl restart systemd-journald`
  - [ ] `/root/tool/clean_logs.sh`: biến `$log_file` không được định nghĩa (`tee -a` rỗng, vô hại), nên khai báo hoặc bỏ
  - [ ] Sau lần chạy `clean_logs.sh` đầu tiên (3:30, 2 ngày/lần), kiểm tra `du -sh /home/*/*/logs` đã giảm

## 🟠 Bảo mật

### [ ] 5. SSH root đăng nhập bằng mật khẩu, bị brute-force liên tục
- **Hiện trạng:** `PermitRootLogin yes`, đăng nhập bằng mật khẩu. fail2ban đang ban 272 IP (tổng cộng 4174). sshd thỉnh thoảng từ chối kết nối mới vì `MaxStartups`.
- **Xử lý:** tạo SSH key trên PC, thêm vào `/root/.ssh/authorized_keys`, kiểm tra đăng nhập bằng key OK rồi mới đặt `PasswordAuthentication no` (hoặc `PermitRootLogin prohibit-password`). **Giữ phiên cũ mở khi đổi** để không bị khóa ngoài.
- **Cập nhật 2026-11-26:** `authorized_keys` đã có 1 key của **VPS phụ `claude-control`** (139.180.158.221, giới hạn `from=` IP, không port-forward), xem SKILL.md mục 2.1. **Đừng xóa key này.** PC vẫn vào bằng mật khẩu, nên trước khi tắt mật khẩu phải thêm key cho PC (và cho Bitvise), đồng thời sửa `vps.py` (đang dùng `password=`; `sql` dùng tunnel bằng mật khẩu). Cách an toàn khi đổi: đặt hẹn giờ rollback `systemd-run --on-active=5min ...` (đã dùng cho VPS phụ, xem `D:\Claude\2026-11-26 Claude Vultr VPS\ACCESS.md` mục 3).

### [ ] 6. `wp-config.php` của cả 3 site ở quyền 644
- **Rủi ro:** user Linux của site này đọc được mật khẩu DB của site khác. Nếu một site bị hack thì cả 3 cùng lộ.
- **Xử lý:** `chmod 640` (owner = user site, group = nhóm của php-fpm) hoặc `600`, rồi kiểm tra site vẫn chạy.
  Các file: `/home/{diaocthDP4f/diaocthongthai.com, congtoaTdvz/congtoan.net, php81conZSI/php81.congtoan.net}/wp-config.php`

### [ ] 7. Credential nằm rải rác ở dạng plain text, quyền 644
- `/root/tool/MegaIO/accounts.json` (18 tài khoản Mega), `/root/tool/diaocthongthai.com/ga4_report/{client_secret*.json, token*.pickle}`: nên `chmod 600`.
- 18 script trong `/root/tool` viết thẳng mật khẩu/key trong code (backup_*.sh, utils.py, s3_*.py...). Nên chuyển sang đọc từ `/var/hostvn/.hostvn.conf` hoặc một file `.env` quyền 600.
- Access key Linode trong `utils.py` có cả ở PC và trong ghi chú `.docx`. Nếu từng chia sẻ thì nên **rotate key**.
- `backup_choice.sh` upload cả `/root/tool` (gồm các file trên) lên S3 `_private/`. Đã kiểm tra là private (403), vẫn ổn nhưng cần biết.

### [ ] 8. File lạ hoặc thừa trong web root
- ~~`public_html/coastlines-split-4326.zip`~~ (đã xóa 26/11/2026), `public_html/test/` (đang chứa demo test `more.js`/`more_admin_2025.js` cho bản đồ Leaflet, tạo 26/11/2026; trang /test2/ nhúng các demo này) (root, tạo 13/11), `public_html/vinq9.html` (469 KB), `56b1ad12699c2133d05ea42a834ce5dd.txt`. Xác nhận file nào còn cần, còn lại thì xóa.

### [ ] 8b. fail2ban vẫn đang chạy, jail `nginx-404` có thể ban nhầm IP Cloudflare
- **Hiện trạng (kiểm tra 2026-11-27):** chủ site nghĩ fail2ban đã tắt, nhưng thực tế service đang `active` + `enabled` (từ lần boot 31/08/2026), có 3 jail:
  - `sshd`: **đang có tác dụng** (đã ban tổng cộng 4.226 IP, đang ban 3). SSH 8282 vẫn mở đăng nhập bằng mật khẩu nên **nên giữ** jail này cho tới khi làm xong mục 5.
  - `nginx-404`: đọc `congtoan.net/logs/access.log`, đã ban **1.118 lần**. IP trong log **là IP thật của khách**: `nginx.conf` dòng 79 include `/etc/nginx/extra/cloudflare.conf`, file này đặt `real_ip_header X-Forwarded-For` và `set_real_ip_from` đủ các dải IPv4/IPv6 của Cloudflare. Nhưng ban vẫn **không có tác dụng**, vì fail2ban chặn bằng iptables trên VPS chính, mà kẻ tấn công không kết nối thẳng vào VPS chính. Request đi tới VPS chính từ IP của Cloudflare, nên IP bị ban không bao giờ khớp. Muốn chặn thật thì phải chặn ở Cloudflare (WAF rule / Cloudflare API), không phải iptables.
  - `nginx-http-auth`: chưa ban lần nào.
- **Xử lý đề xuất (cần chủ site quyết):** tắt jail `nginx-404` (`enabled = false` trong `/etc/fail2ban/jail.d/nginx-404.conf`, rồi `fail2ban-client reload`), giữ jail `sshd`. Không cần thêm `ignoreip` cho VPS phụ (VPS phụ gọi cổng 443 và log diaocthongthai.com, không thuộc phạm vi jail nào).

## 🟡 Cache pipeline (xem `.claude/skills/vps-diaocthongthai/references/cache-pipeline.md`)

### [ ] 9. Cron segment-update và cache-clean không đẩy cache lên S3
- `cron-cache-clean-v2.py` và `cron-cache-segment-update-v2.py` curl qua domain public, chỉ làm mới cache trên disk và không gọi `upload_files_to_s3`.
- **Cần xác nhận:** Cloudflare Worker phục vụ từ S3 hay từ origin? Nếu từ S3 thì các trang này trên S3 sẽ cũ hơn bản trên disk. Hướng sửa: viết lại bằng `utils.trigger_cache` + `upload_files_to_s3`, hoặc đưa các URL vào `cache-urls-update.txt`.

### [ ] 10. 3.865 URL trong `cache_urls.db` bị NULL `lang`/`page_type`
- Do `u2_update_db_and_kv_v3.3.py` chỉ INSERT `url, cached`. Các tool chạy với `--lang`/`--page_type` sẽ bỏ qua những URL này.
- **Sửa dữ liệu:** `UPDATE urls SET lang=CASE WHEN url LIKE 'https://diaocthongthai.com/en/%' THEN 'en' ELSE 'vi' END, page_type=CASE WHEN url LIKE '%/page/%' THEN 'archive' ELSE 'single' END WHERE lang IS NULL;` (cần xem lại quy tắc archive/single trước khi chạy).
- **Sửa code u2:** INSERT đủ `lang, page_type, page_number` (dùng `utils.compute_page_number`).

### [ ] 11. Phiên bản tool lệch giữa PC và VPS
- `cache_create_from_db.py` trên **PC** vẫn còn bug `are_cache_file_exists` (VPS đã sửa). Nên copy bản VPS về PC.
- `m1`/`m2` trên VPS (`m1_check_missing_disk.py`, `m2_check_missing_s3.py`) là bản cũ, khác `m1_sv_*`/`m2_pc_*_v2` trên PC.
- Quy trình đồng bộ `cache_urls.db` giữa PC (u2 ghi) và VPS (m1/m4/renew ghi) chưa được ghi rõ, dễ ghi đè lẫn nhau. Nên chốt: VPS là bản gốc, PC luôn tải về trước khi chạy u1/u2 rồi đẩy lại.

### [ ] 12. Các điểm còn mở trong `_Note_cache_tools_pipeline.md`
- Key KV không thống nhất: `KV_export` ghi **path** (`/abc/`), `u2` ghi **URL đầy đủ**. Cần xác nhận Worker tra theo dạng nào.
- `m2_pc_check_missing_s3_v2.py` coi URL là "có trên S3" khi chỉ có 1 trong 2 bản (desktop/mobile).
- `pre_clean_search_dir` chỉ xóa `/search/` trên disk, không xóa trên S3.

## ⚪ Dọn dẹp nhỏ
- [ ] 13. `backup_choice.sh`: dòng `rm -rf "$backup/$up"` đang bị comment, nên `/home/backup/<domain>/` sẽ tích thư mục rỗng.
- [ ] 14. `/root/tool/vinhomecangio.info/`: site không còn trên VPS, có thể lưu trữ hoặc xóa.
- [ ] 15. File rác: `/root/python` (0 byte), `/root/2`, `/root/tool/wget-log`, `/root/tool/diaocthongthai.com/cache-tools/-cli -h 127.0.0.1 -p 6379 ping` (file tên lỗi do gõ nhầm lệnh).
