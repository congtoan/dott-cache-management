# Triển khai service / REST API trên VPS phụ

Áp dụng cho Go (ưu tiên vì nhẹ), Node, Python FastAPI. Mọi lệnh chạy dưới quyền root, qua `ssh claude-control "..."` hoặc bên trong phiên Claude trên VPS phụ.

## 1. Cài runtime (chỉ cài khi cần)

**Go** (khuyên dùng: một file binary, RAM khoảng 10 đến 30 MB):
```bash
# Phương án 1: gói Ubuntu, đơn giản, phiên bản theo bản phân phối
apt-get install -y golang-go && go version
# Phương án 2: bản chính thức mới nhất (thay X.Y.Z bằng bản ổn định trên go.dev/dl)
curl -fsSLO https://go.dev/dl/goX.Y.Z.linux-amd64.tar.gz
rm -rf /usr/local/go && tar -C /usr/local -xzf goX.Y.Z.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' > /etc/profile.d/go.sh
```
Có thể build ngay trên PC rồi chép binary lên: `set GOOS=linux& set GOARCH=amd64& go build -o app` (cmd) hoặc `$env:GOOS="linux"; $env:GOARCH="amd64"; go build -o app` (PowerShell). Build trên PC thì VPS phụ không cần cài Go và không tốn RAM để build.

**Python (FastAPI)**:
```bash
python3 -m venv /opt/<app>/venv
/opt/<app>/venv/bin/pip install fastapi uvicorn
```
Python 3.14 rất mới. Nếu một gói báo lỗi khi build wheel thì cài `uv` rồi dùng `uv venv -p 3.12`.

**Node**: dùng NodeSource hoặc `apt-get install -y nodejs npm`. Lưu ý RAM: một tiến trình Node khoảng 50 đến 100 MB, nên đặt `--max-old-space-size=128`.

**SQLite**: Go dùng `modernc.org/sqlite` (không cần cgo), Python có sẵn `sqlite3`. Nếu cần CLI để xem dữ liệu: `apt-get install -y sqlite3`.

## 2. Bố cục thư mục
```
/opt/<app>/            # code / binary (owner root, chỉ đọc với user service)
  bin/<app>            # binary Go, hoặc venv/ + src/ cho Python
  releases/<ngày>/     # (tùy chọn) bản cũ để rollback
/etc/<app>/env         # biến môi trường + secret, chmod 600
/var/lib/<app>/        # dữ liệu (SQLite...), owner = user service
journald               # log: journalctl -u <app>
```
Tạo user riêng cho service, không chạy service bằng root:
```bash
useradd --system --home /var/lib/<app> --shell /usr/sbin/nologin <app>
install -d -o <app> -g <app> -m 750 /var/lib/<app>
install -d -m 755 /opt/<app>/bin
install -d -m 700 /etc/<app> && touch /etc/<app>/env && chmod 600 /etc/<app>/env
```

## 3. Unit systemd mẫu (`/etc/systemd/system/<app>.service`)
```ini
[Unit]
Description=<app> - <mô tả ngắn>
After=network-online.target
Wants=network-online.target

[Service]
User=<app>
Group=<app>
WorkingDirectory=/var/lib/<app>
EnvironmentFile=/etc/<app>/env
ExecStart=/opt/<app>/bin/<app> -listen 127.0.0.1:8081
# Python: ExecStart=/opt/<app>/venv/bin/uvicorn main:app --host 127.0.0.1 --port 8081 --workers 1
Restart=on-failure
RestartSec=5

# Giới hạn tài nguyên: máy chỉ có 1 GB RAM và Claude Remote Control cần phần lớn
MemoryHigh=120M
MemoryMax=160M
CPUQuota=60%
Nice=5

# Hardening
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/var/lib/<app>

[Install]
WantedBy=multi-user.target
```
```bash
systemctl daemon-reload && systemctl enable --now <app>
systemctl status <app> --no-pager; journalctl -u <app> -n 50 --no-pager
curl -s http://127.0.0.1:8081/health
```
Việc chạy định kỳ: dùng **systemd timer** (`<app>-job.timer` + `.service`, `OnCalendar=*:0/15`) thay cho cron để có log trong journald và giới hạn RAM giống service.

## 4. Mở ra ngoài (chỉ khi cần, phải hỏi chủ site)
Hiện trên máy **không có firewall** và chỉ có port 22 đang mở.

1. **Firewall trước**:
   ```bash
   apt-get install -y ufw
   ufw allow 22/tcp            # BẮT BUỘC trước khi enable, nếu không sẽ tự khóa mình ở ngoài
   ufw allow 80,443/tcp
   ufw enable && ufw status
   ```
   Hoặc dùng Vultr Firewall Group trên trang quản trị. Nếu chỉ VPS chính gọi tới thì chỉ mở cho IP `45.77.32.177`: `ufw allow from 45.77.32.177 to any port 443`.
2. **Caddy làm reverse proxy + HTTPS tự động** (nhẹ, khoảng 30 MB):
   ```bash
   apt-get install -y caddy
   # /etc/caddy/Caddyfile
   api.example.com {
       reverse_proxy 127.0.0.1:8081
   }
   systemctl reload caddy
   ```
   Cần bản ghi DNS `api.<domain>` trỏ về `139.180.158.221`. Nếu để proxy Cloudflare (đám mây cam) thì đặt SSL mode là Full (strict).
3. **Xác thực**: API dùng nội bộ thì đặt header `Authorization: Bearer <token>` (lưu trong `/etc/<app>/env`), hoặc chỉ cho phép IP VPS chính. Không để endpoint ghi dữ liệu mà không có xác thực.

## 5. Cập nhật / rollback
```bash
# từ PC: build -> chép bản mới
scp app claude-control:/opt/<app>/releases/$(date +%F)/app
# trên VPS phụ
cp /opt/<app>/bin/<app> /opt/<app>/bin/<app>.prev
install -m 755 /opt/<app>/releases/<ngày>/app /opt/<app>/bin/<app>
systemctl restart <app> && sleep 2 && curl -sf http://127.0.0.1:8081/health || {
  mv /opt/<app>/bin/<app>.prev /opt/<app>/bin/<app>; systemctl restart <app>; echo "ROLLBACK"; }
```
Mỗi service nên có endpoint `GET /health` trả về `200 ok` để kiểm tra nhanh.

## 6. Ghi nhật ký thay đổi
Mỗi lần triển khai, ghi một file `/root/work/YYYY-MM-DD_<app>.md` với các mục: mục đích, file và đường dẫn, unit systemd, cổng, secret ở đâu (không ghi giá trị), cách kiểm tra, cách hoàn tác. Làm giống `/root/work/2026-11-27_vps-log-va-giam-sat.md`.

## 7. Checklist: chuyển project Cache Management sang VPS phụ
Trả lời các câu dưới đây trước khi chuyển. Pipeline cache hiện chạy trên VPS chính và **không được dừng** (screen `F1`: `cache_renew_from_db.py`, `en-map-of`; không xóa `cache_urls.db`).

1. **Dữ liệu nằm ở đâu?** `cache_urls.db` và các thư mục `wp-content/cache/wp-rocket` (14 GB) nằm trên VPS chính.
   - Nếu project cần đọc hoặc ghi trực tiếp file cache thì **không thể** chạy ở VPS phụ, chỉ có thể điều khiển từ xa.
   - Có thể tách theo hướng: VPS phụ làm phần điều phối, lập lịch, lưu DB và giao diện quản lý; VPS chính chỉ làm phần thao tác với file cache.
2. **Ai sinh ra trang cache?** WP Rocket tạo cache khi có request tới VPS chính. Hiện pipeline gọi `127.0.0.1`. Nếu gọi từ VPS phụ thì request đi qua Internet hoặc Cloudflare, nên cần gọi thẳng origin (`--resolve diaocthongthai.com:443:45.77.32.177`) và không để bị rate limit hoặc fail2ban (`nginx-404`) chặn IP `139.180.158.221`.
3. **Tải**: VPS phụ có 1 vCPU và 1 GB RAM. Việc render trang vẫn tốn CPU và RAM của **VPS chính**, nên chuyển project không làm giảm phần tải render.
4. **Bí mật**: S3 (Linode `cdn.diaocthongthai.com`), Cloudflare token (`/var/hostvn/.cloudflare.conf` trên VPS chính), DB WordPress. Hiện MariaDB của VPS chính chỉ nghe local và **không được mở port MySQL ra Internet**. **SSH tunnel cũng không dùng được**: key `claude-control` trên VPS chính có `no-port-forwarding`, nên `ssh -L` bị từ chối (`administratively prohibited`). Cache System v2 lấy dữ liệu qua REST endpoint của mu-plugin (xem `docs/design-server-only.md` trong repo `dott-cache-management`).
5. **Đường gọi giữa 2 máy**:
   - VPS phụ ra lệnh cho VPS chính: dùng `ssh main` (đã có key, chỉ dùng từ IP VPS phụ).
   - VPS chính gọi API trên VPS phụ: dùng HTTPS + token, hoặc chỉ mở port cho IP `45.77.32.177`.
6. **Khi VPS phụ sập**: cache vẫn phải được phục vụ bình thường. VPS phụ chỉ nên là phần "bộ não", không nằm trên đường phục vụ request của website.
7. Làm song song: chạy bản trên VPS phụ ở chế độ **dry-run** (chỉ đọc và báo cáo) trước, so sánh kết quả với pipeline hiện tại, rồi mới hỏi chủ site để chuyển dần.
