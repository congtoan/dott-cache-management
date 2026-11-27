#!/usr/bin/env python3

"""
Tool: Cache Create
===================

Công dụng:
    - Tạo mới cache cho các URL trong database mà hiện tại chưa có cache trên disk.
    - Sau khi tạo cache, upload các file cache lên S3 bucket (có thể nhiều bucket nếu cấu hình trong utils).

Cách hoạt động:
    1. Đọc danh sách URL từ database `urls` thông qua cursor SELECT (read-only).
    2. Với mỗi URL:
       - Kiểm tra file cache có tồn tại trên disk không.
         + Nếu có → cập nhật trạng thái cached=1 và bỏ qua.
         + Nếu chưa có → trigger curl để tạo cache, sau đó upload lên S3.
    3. Cập nhật trạng thái trong DB (cursor UPDATE/WRITE riêng biệt).
    4. Commit từng URL để giảm thiểu lock và đảm bảo an toàn khi chạy song song với tool khác.

Điểm kỹ thuật:
    - Tool này không dùng batch size (mặc định chạy hết hoặc tới khi đủ `--limit`).
    - Sử dụng 2 connection: 
        + 1 connection chỉ để SELECT (PRAGMA query_only=1).
        + 1 connection chỉ để UPDATE/INSERT/DELETE.
      Điều này giúp giảm nguy cơ lock khi nhiều tool cùng truy cập DB.

Cách sử dụng:
    python cache_create_from_db.py --db output/cache_urls.db --lang vi --page_type single --tag hot --limit 100

Tham số:
    --db        Đường dẫn file SQLite DB (mặc định: output/cache_urls.db).
    --lang      Lọc theo ngôn ngữ (en, vi, all).
    --page_type Lọc theo loại trang (single, archive, all).
    --tag       Lọc theo tag cụ thể.
    --limit     Giới hạn số lượng URL xử lý.

Lưu ý:
    - Tool chạy một lần (one-shot).
    - Không có cột in_progress, vì tool xử lý tuần tự và commit từng URL, giảm thiểu xung đột với tool khác.
"""

import sqlite3
import time
from utils import (
    trigger_cache,
    url_to_cache_paths,
    upload_files_to_s3,
    is_cache_file_exists,
    check_cpu_usage_and_wait,
)


def get_select_connection(db_path):
    conn = sqlite3.connect(db_path, timeout=30.0)
    conn.execute("PRAGMA journal_mode = WAL")
    conn.execute("PRAGMA synchronous = NORMAL")
    conn.execute("PRAGMA cache_size = 1000")
    conn.execute("PRAGMA temp_store = memory")
    conn.execute("PRAGMA query_only = 1")
    return conn

def get_update_connection(db_path):
    conn = sqlite3.connect(db_path, timeout=30.0)
    conn.execute("PRAGMA journal_mode = WAL")
    conn.execute("PRAGMA synchronous = NORMAL")
    conn.execute("PRAGMA cache_size = 1000")
    conn.execute("PRAGMA temp_store = memory")
    return conn


def cache_create(
    db_path="output/cache_urls.db",
    lang="all",
    page_type="all",
    tag=None,
    limit=None,
):
    """Chạy 1 lần: tạo cache cho các URL chưa có cache trên disk."""
    conn_select = get_select_connection(db_path)
    conn_update = get_update_connection(db_path)
    cur_update = conn_update.cursor()
    cur_select = conn_select.cursor()

    q = "SELECT id, url FROM urls WHERE 1=1"
    qparams = []
    if lang != "all":
        q += " AND lang=?"
        qparams.append(lang)
    if page_type != "all":
        q += " AND page_type=?"
        qparams.append(page_type)
    if tag:
        q += " AND id IN (SELECT url_id FROM url_tags WHERE tag=?)"
        qparams.append(tag)

    processed = 0
    for url_id, url in cur_select.execute(q, qparams):
        print("Creating cache:", url)
        files = url_to_cache_paths(url)

        if is_cache_file_exists(files[0]):
            print("✓ Cache exists on disk")

            from utils import is_file_on_s3
            if not is_file_on_s3(files[0]):
                print("⏫ Missing on S3 → uploading...")
                upload_files_to_s3(files)
            else:
                print("✓ Already on S3")

            ts = int(time.time())
            cur_update.execute(
                "UPDATE urls SET cached=1, last_update=? WHERE id=?",
                (ts, url_id),
            )
            conn_update.commit()
            continue


        check_cpu_usage_and_wait()
        trigger_cache(url)
        upload_files_to_s3(files)  # upload lên tất cả bucket

        ts = int(time.time())
        cur_update.execute(
            "UPDATE urls SET cached=1, last_update=? WHERE id=?",
            (ts, url_id),
        )
        conn_update.commit()

        processed += 1
        if limit and processed >= limit:
            break

    conn_select.close()
    conn_update.close()
    print(f"Finished. Total processed: {processed}")


def main():
    import argparse

    parser = argparse.ArgumentParser(
        description="Cache create - generate cache for missing URLs"
    )
    parser.add_argument("--db", default="output/cache_urls.db")
    parser.add_argument("--lang", default="all")
    parser.add_argument("--page_type", default="all")
    parser.add_argument("--tag")
    parser.add_argument("--limit", type=int)
    args = parser.parse_args()

    cache_create(
        db_path=args.db,
        lang=args.lang,
        page_type=args.page_type,
        tag=args.tag,
        limit=args.limit,
    )


if __name__ == "__main__":
    main()
