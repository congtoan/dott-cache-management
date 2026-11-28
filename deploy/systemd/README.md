# Service pool (docs/design-pool.md)

| Unit | Việc |
|---|---|
| `dott-cache-serve.service` | `dott-cache serve -sites …`: nhận sự kiện trên `127.0.0.1:8790`, pool, worker. `MemoryMax=250M` (đo: 23 MB) |
| `dott-cache-tunnel.service` | `ssh -N -R 127.0.0.1:8790:127.0.0.1:8790 main`: cổng loopback VPS chính → serve. Cần key riêng có `permitlisten` (design-pool mục 9) |

Log: `journalctl -u dott-cache-serve`, kết quả từng URL ở `logs/<site>/serve-YYYYMMDD.jsonl`. Xem pool: `dott-cache pool status`.

# Lịch chạy dott-cache (systemd trên VPS phụ), **chưa bật**

Bật ở bước P8, sau khi mỗi blog đã chuyển xong. Mỗi lịch là một cặp `.service` và `.timer`. Lệnh được bọc trong `dott-cache-run.sh`, có ghi log và dọn log quá 30 ngày.

| Timer | Lịch | Lệnh |
|---|---|---|
| `dott-cache-sync@<blog>.timer` | 8 giờ/lần (02:15, 10:15, 18:15) | `sync -site diaocthongthai.com -blog <blog>` |
| `dott-cache-sync-full@<blog>.timer` | 1 lần/ngày (03:30) | `sync -site diaocthongthai.com -blog <blog> -full` |
| `dott-cache-update-all@<blog>.timer` | Hằng tháng (ngày 1, 04:00) | `update -site diaocthongthai.com -all -blog <blog> -yes` |
`<blog>` là tên instance: `vi` → `-blog /`, `en` → `-blog /en/`. Bảng ánh xạ nằm trong `dott-cache-blog.sh`.

Mọi service đều có `MemoryMax=300M`, `Nice=10`, `CPUQuota=50%`. Khóa theo site (`data/<site>/.lock`) bảo đảm các lịch không chạy chồng nhau: lịch đến sau sẽ báo "đang có lệnh khác" và thoát.

Cài (khi được duyệt):
```
cp deploy/systemd/*.service deploy/systemd/*.timer /etc/systemd/system/
install -m 755 deploy/systemd/dott-cache-run.sh deploy/systemd/dott-cache-blog.sh /root/tool/cache-system/bin/
systemctl daemon-reload
systemctl enable --now dott-cache-sync@vi.timer dott-cache-sync-full@vi.timer dott-cache-update-all@vi.timer
```
