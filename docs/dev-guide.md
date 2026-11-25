# Hướng dẫn phát triển tool mới

Quy ước để các tool mới ăn khớp với hệ thống hiện có.

## Chọn nơi đặt
| Tool cần... | Đặt ở | Lý do |
|---|---|---|
| Đọc/ghi file cache trên disk, trigger cache, upload S3 | `server/` | Cache WP Rocket chỉ có trên VPS; `trigger_cache` gọi `127.0.0.1` |
| Sửa DB gốc `cache_urls.db` | `server/` | DB gốc nằm trên VPS (xem pipeline mục 4) |
| Cloudflare KV / wrangler, xử lý dữ liệu nặng, liệt kê S3 | `pc/` | wrangler đã login trên PC |
| Chỉ đọc DB để thống kê | `pc/` (dùng bản sao trong `data/`) hoặc `server/` | |

## Quy ước tên
- Tiền tố theo giai đoạn: `a*`/`u*` (danh sách URL), `m*` (kiểm kê/vá thiếu), `cache_*` (vận hành).
- Hậu tố nơi chạy: `_sv_` (server) hoặc `_pc_`. Có phiên bản thì thêm `_v2`. Bản cũ chuyển vào `_archive/` thay vì để chung.
- Tool chạy một lần: đặt tên có `once_`, ví dụ `once_fix_null_lang.py`, và ghi ngày chạy vào docstring.

## Tool server
```python
#!/usr/bin/env python3
"""
<tên>.py – <làm gì>
Chạy: cd /root/tool/diaocthongthai.com/cache-tools && python3 <tên>.py --db output/cache_urls.db [--dry-run]
"""
import argparse, sqlite3, time
from utils import url_to_cache_paths, trigger_cache, upload_files_to_s3, check_cpu_usage_and_wait, TARGET_DIR
```
- **Mở DB như renew đang làm**: `sqlite3.connect(db, timeout=30)` + `PRAGMA journal_mode=WAL`. Connection chỉ đọc thì thêm `PRAGMA query_only=1`. **Bỏ qua URL có `in_progress=1`** (đang bị renew khóa).
- Vòng lặp nặng phải gọi `check_cpu_usage_and_wait()` trước `trigger_cache`, vì VPS chỉ có 2 GB RAM.
- Mọi request HTTP phải có `timeout=` (xem findings F1).
- Thao tác xóa (disk/S3/DB) phải hỗ trợ `--dry-run`, và mặc định nên in số lượng trước khi chạy thật.
- Chạy lâu thì dùng `screen -S <tên>`, đừng chạy trong session SSH thường. Cũng đừng dùng screen F1 (của renew).

## Tool PC
```python
from paths import DB_PATH, DATA_DIR, JSON_DIR, MANIFEST_ROOT   # pc/paths.py
```
- Không viết đường dẫn tuyệt đối; thêm hằng số mới vào `pc/paths.py`.
- Cần SSH/WP-CLI thì dùng `pc/config/settings.py`, hoặc gọi tool `vps.py` ở `D:\Claude\2026-11-25 Ket Noi VPS`.
- Trước khi dùng `data/cache_urls.db`, hãy tải bản chụp mới từ VPS (README).

## Deploy và kiểm tra
1. Sửa trong `server/`, chạy thử cú pháp trên PC: `python -m py_compile server/<file>.py`
2. Backup bản trên VPS: `cp x.py x.py.bak-$(date +%F)`
3. `python vps.py put server/<file>.py /root/tool/diaocthongthai.com/cache-tools/<file>.py`
4. Chạy thử với `--dry-run` hoặc `--limit 5` trước.
5. Cập nhật `docs/pipeline.md` (bảng tool) nếu thêm tool mới.
