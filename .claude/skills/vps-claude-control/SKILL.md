---
name: vps-claude-control
description: Kết nối và triển khai công việc trên VPS phụ `claude-control` (Vultr 139.180.158.221, Ubuntu 26.04, 1 GB RAM) từ Claude Code trên PC qua SSH. Dùng khi cần chạy lệnh, chép file, cài runtime (Go/Node/Python), dựng service systemd hoặc endpoint REST trên VPS phụ, chuyển một project (vd Cache Management) từ PC hoặc VPS chính sang VPS phụ, xem bộ giám sát /root/health, hoặc khi cần đi tiếp từ VPS phụ sang VPS chính (`ssh main`).
---

# VPS phụ `claude-control`

Cập nhật: 2026-11-27 (khảo sát trực tiếp trên máy). Bản gốc của skill: `/root/work/skills/vps-claude-control/` trên VPS phụ.

## 1. Máy này là gì
- **Vultr `vc2-1c-1gb`**: 1 vCPU, **1 GB RAM** (950 MB dùng được), swap file 2,4 GB (`/swapfile`, swappiness 10), ổ 23 GB (đang dùng khoảng 7 GB, còn trống 15 GB).
- **Ubuntu 26.04.1 LTS**, kernel 7.0, hostname `claude-control`, múi giờ **Asia/Ho_Chi_Minh**, NTP bật.
- IP: **139.180.158.221** (IPv4 public, `enp1s0`). Máy không có mạng riêng (private network) với VPS chính, mọi kết nối đi qua Internet.
- Vai trò hiện tại:
  1. Chạy Claude Code 24/7 (**Remote Control**) để chủ site quản trị VPS chính từ điện thoại.
  2. Bộ giám sát VPS chính bằng Claude (`/root/health`, cron 23h45).
  3. Từ nay: có thể chạy **service hoặc REST API nhẹ** phục vụ website hoặc VPS chính.
- Máy không chứa dữ liệu quan trọng. Thư mục làm việc: `/root/work`.

## 2. Kết nối từ PC (Windows)
- SSH: `root@139.180.158.221`, **port 22**, chỉ nhận key (đã tắt đăng nhập bằng mật khẩu). Key của PC có comment `pc-to-claude-control`. Đường dẫn file key trên PC xem trong `D:\Claude\2026-11-26 Claude Vultr VPS\ACCESS*`.
- Nên khai báo trong `C:\Users\<user>\.ssh\config` như sau:
  ```
  Host claude-control
      HostName 139.180.158.221
      User root
      Port 22
      IdentityFile ~/.ssh/<key-pc-to-claude-control>
      IdentitiesOnly yes
      ServerAliveInterval 30
  ```
- Cách dùng từ Claude Code trên PC:
  - Chạy lệnh: `ssh claude-control "lệnh"`. Trong vòng lặp hoặc script thì dùng `ssh -n claude-control ...`.
  - Chép file lên: `scp -r .\thu-muc claude-control:/opt/<app>/`
  - Chép file về: `scp claude-control:/root/work/file.md .`
  - Đồng bộ cả project: `rsync` không có sẵn trên Windows. Dùng `scp -r`, hoặc `git push` lên remote rồi `git pull` trên VPS phụ (máy đã có git 2.53).
- **Đi tiếp sang VPS chính**: từ VPS phụ chạy `ssh claude-control "ssh -n main 'lệnh'"`. Key tới VPS chính (`/root/.ssh/id_main_vps`, alias `main` trỏ tới `45.77.32.177:8282`) **chỉ dùng được từ IP của VPS phụ**. Lệnh chỉ đọc soạn sẵn: `ssh claude-control "main-ro help"`.
- Kiến thức về VPS chính nằm ở skill **`vps-diaocthongthai`**.

## 3. Đang chạy gì (không được phá)
| Thành phần | Chi tiết |
|---|---|
| `claude-rc.service` | systemd, mở tmux session `claude` và chạy `/usr/local/bin/claude-rc-loop`, tức là `claude remote-control --capacity 2 --permission-mode default`, tự chạy lại sau 20 giây nếu thoát. Log ở `/root/work/.claude-rc.log`. **Không `systemctl stop/restart claude-rc`, không kill `claude remote-control`**, vì làm vậy sẽ cắt phiên chủ site đang dùng trên điện thoại. |
| Giám sát VPS chính | Cron `45 23 * * * /root/health/run.sh`. Script thu thập số liệu chỉ đọc qua `ssh main`, sau đó `claude -p` (không cho dùng tool nào) phân tích, và chỉ gửi Telegram khi có bất thường. File: `run.sh`, `collect-remote.sh`, `prompt.md`, `telegram.env` (chmod 600, **không in ra**), `history/`. Tài liệu: `/root/work/2026-11-27_vps-log-va-giam-sat.md`. |
| `fail2ban` | Jail `sshd`. |
| `watchdog`, `unattended-upgrades` | Mặc định. |
| Cổng đang mở | Chỉ có **22 (sshd)**. Không có nginx/caddy/docker. |
| Firewall | **UFW đang bật** (`ufw status`: `deny incoming`, chỉ cho phép `22/tcp`; image Vultr bật sẵn, kiểm tra lại ngày 27/11). Service mới lắng nghe trên 0.0.0.0 sẽ bị UFW chặn, nhưng vẫn nên bind `127.0.0.1`. Muốn mở cổng thì phải `ufw allow` (hỏi chủ site). |

Ngân sách RAM: Remote Control cộng mỗi phiên Claude chiếm khoảng 250 đến 400 MB, tối đa 2 phiên. Khi lúc rảnh, phần "available" còn khoảng 540 MB. **Tổng RAM của các service mới nên giữ dưới khoảng 200 MB** và luôn đặt `MemoryMax=` trong unit systemd.

## 4. Phần mềm có sẵn
- Có sẵn: `python3` 3.14 + pip, `git`, `gcc`, `make`, `tmux`, `screen`, `jq`, `rsync`, `curl`, Claude Code (`/root/.local/bin/claude`).
- **Go 1.26.8** (`apt install golang-go`, cài ngày 27/11 cho Cache System v2). GOROOT `/usr/lib/go-1.26`, cache build ở `/root/.cache/go-build`. **Build lần đầu tốn khoảng 220 MB RAM và 77 giây**, nên build bằng `GOMEMLIMIT=200MiB GOFLAGS=-p=1 go build ...`; các lần sau nhanh hơn nhờ có cache.
- **Chưa cài**: Node, uv, sqlite3 CLI, docker, nginx/caddy, s3cmd, redis.
- Python 3.14 rất mới, một số wheel (thư viện) có thể chưa có bản dựng sẵn. Dùng `python3 -m venv /opt/<app>/venv` để cách ly, đừng `pip install` vào hệ thống.

## 5. Quy tắc an toàn
1. **VPS chính là production.** Chỉ đọc thì cứ làm. Ghi, xóa, restart, sửa cấu hình hay chạy SQL ghi trên VPS chính thì **phải hỏi chủ site trước**. Chi tiết ở skill `vps-diaocthongthai`.
2. VPS phụ: được cài và thử nghiệm, nhưng **không đụng `claude-rc`**, không sửa `/root/.claude/settings.json` để nới quyền, không chạy Claude với `--dangerously-skip-permissions`.
3. **Không sửa dòng `pc-to-claude-control` trong `/root/.ssh/authorized_keys`**, vì đó là đường vào từ PC. Không đụng `/root/.ssh/id_main_vps*`. Trên VPS chính không sửa dòng `claude-control` trong authorized_keys.
4. Service mới mặc định **bind `127.0.0.1`**. Nếu cần mở ra ngoài thì đặt reverse proxy (Caddy) hoặc firewall phía trước, và hỏi chủ site trước.
5. Không in mật khẩu, key hay token ra hội thoại. Secret đặt trong file `chmod 600` (ví dụ `/etc/<app>/env`) rồi nạp bằng `EnvironmentFile=`.
6. Trước khi sửa file cấu hình: `cp file file.bak-$(date +%F)`.
7. Thử nghiệm làm trong `/tmp/<tên>` và dọn sau khi xong.

## 6. Triển khai một service mới
Xem đầy đủ trong [references/deploy-service.md](references/deploy-service.md): cài Go/Node/Python, bố cục thư mục `/opt/<app>`, mẫu unit systemd có giới hạn RAM và CPU, log, mở cổng an toàn (Caddy + HTTPS, firewall), cách cập nhật và rollback, cùng checklist cho việc **chuyển project Cache Management** sang đây.

## 7. Lệnh nhanh (chạy qua `ssh claude-control "..."`)
```
free -m; df -h /; uptime                 # tài nguyên
systemctl --failed; systemctl status <app>
journalctl -u <app> -n 100 --no-pager    # log service
ss -tlnp                                  # cổng đang nghe
tail -20 /root/health/run.log             # lần giám sát gần nhất
cat /root/health/history/$(date +%F).answer
main-ro status                            # VPS chính, chỉ đọc
```

## 8. Tài liệu liên quan
- PC: `D:\Claude\2026-11-26 Claude Vultr VPS\` (README, ACCESS, SETUP-PLAN, INSTALL-LOG) ghi cách dựng máy này. Bản gốc của `claude-rc-loop`, `main-ro`, `CLAUDE.md` nằm ở `deploy\`.
- VPS phụ: `/root/.claude/CLAUDE.md` (hướng dẫn chung), `/root/work/*.md` (nhật ký các thay đổi).
