#!/usr/bin/env python3
"""
==========================================================
🧩 Tool Name: m1_sv_check_missing_disk.py
----------------------------------------------------------
Chức năng chính:
- Kiểm tra toàn bộ danh sách URL trong database `cache_urls.db`
  để xác định các URL nào đã có đủ cache file trên ổ đĩa (disk),
  và URL nào đang thiếu cache (cache bị lỗi, bị xóa, hoặc chưa tạo).

Cơ chế hoạt động:
1️⃣ Đọc toàn bộ danh sách URL từ bảng `urls` trong DB.
2️⃣ Với mỗi URL, kiểm tra sự tồn tại của 4 file cache:
    - index-https.html
    - index-https.html_gzip
    - index-mobile-https.html
    - index-mobile-https.html_gzip
   (dựa vào các hàm kiểm tra trong `utils.py`)
3️⃣ Nếu đủ cả 4 file → cập nhật trong DB:
        cached = 1
    Ngược lại, nếu thiếu → cập nhật trong DB:
        cached = 0
    Đồng thời ghi URL đó vào file `missing_disk.txt` để tiện theo dõi.
4️⃣ Sau khi tool chạy xong, DB được đồng bộ hoàn toàn với tình trạng thực tế
   của cache trên ổ đĩa, phục vụ cho các tool tiếp theo (như m4).

Output:
- File log chứa danh sách URL bị thiếu cache: `missing_disk.txt`
- Cột `cached` trong DB `urls` được cập nhật chính xác (0 = thiếu, 1 = đủ)

Lưu ý:
- Tool này **vừa kiểm tra, vừa cập nhật database**,
  nên không cần phải sửa DB thủ công.
- Tool m4 sẽ dựa vào kết quả `cached=0` trong DB để tạo cache mới.

Tác giả: Địa Ốc Thông Thái Dev Team
Phiên bản: v1.0
==========================================================
"""

import sqlite3
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from utils import url_to_cache_paths, is_cache_file_exists, write_urls_to_file

def check_url_disk(url):
    files = url_to_cache_paths(url)
    return url if not is_cache_file_exists(files[0]) else None

def main(db_path="output/cache_urls.db", out_file="missing_disk.txt", workers=10):
    conn = sqlite3.connect(db_path)
    conn.execute("PRAGMA journal_mode = WAL")
    cur_select = conn.cursor()
    cur_update = conn.cursor()

    urls = [(row[0], row[1]) for row in cur_select.execute("SELECT id, url FROM urls")]
    missing = []

    with ThreadPoolExecutor(max_workers=workers) as ex:
        futures = {ex.submit(check_url_disk, url): (url_id, url) for url_id, url in urls}
        for fut in as_completed(futures):
            url_id, url = futures[fut]
            res = fut.result()
            ts = int(time.time())
            if res:  # thiếu trên disk
                missing.append(res)
                cur_update.execute("UPDATE urls SET cached=0, last_update=? WHERE id=?", (ts, url_id))
            else:  # có cache
                cur_update.execute("UPDATE urls SET cached=1, last_update=? WHERE id=?", (ts, url_id))
            conn.commit()

    write_urls_to_file(missing, out_file)
    conn.close()
    print(f"Done. Missing cache on disk: {len(missing)} urls → {out_file}")

if __name__ == "__main__":
    main()
