#!/usr/bin/env python3
"""
Tool: Cache Renew (Infinity Loop)
=================================

Công dụng:
    - Liên tục quét database và làm mới (renew) cache cho các URL cũ nhất.
    - Đảm bảo cache luôn cập nhật, tránh tình trạng cache quá hạn.
    - Upload các file cache mới lên S3 bucket sau khi tạo lại.

Cách hoạt động:
    1. Vòng lặp vô hạn:
       - Lấy một batch URL cũ nhất từ DB (ORDER BY last_update ASC).
       - Lock batch bằng cách set cột in_progress=1.
       - Với mỗi URL trong batch:
           + Xóa file cache cũ trên disk.
           + Trigger curl để tạo lại cache mới.
           + Upload file cache mới lên S3.
           + Update trạng thái cached=1, last_update=now, in_progress=0.
       - Nếu tool bị dừng giữa chừng → unlock các URL chưa xử lý (set in_progress=0).
    2. Nghỉ một khoảng thời gian (sleep) rồi tiếp tục batch kế tiếp.

Điểm kỹ thuật:
    - Batch size: số lượng URL xử lý mỗi lần (giúp kiểm soát tải hệ thống).
    - Dùng 2 connection riêng biệt (SELECT và UPDATE) để tránh lock.
    - Cursor SELECT chỉ đọc, cursor UPDATE chỉ ghi → cho phép chạy song song nhiều tool/screen.
    - Cột in_progress:
        + Đảm bảo một URL chỉ bị một process xử lý tại một thời điểm.
        + Tránh tình trạng nhiều worker cùng xử lý một URL.
    - Cơ chế graceful shutdown: nhận signal (Ctrl+C), tool hoàn thành batch hiện tại rồi mới thoát.

Cách sử dụng:
    python cache_renew_from_db.py --db output/cache_urls.db --lang vi --page_type single --batch_size 50 --sleep 5

Tham số:
    --db          Đường dẫn file SQLite DB.
    --lang        Lọc theo ngôn ngữ.
    --page_type   Lọc theo loại trang.
    --tag         Lọc theo tag cụ thể.
    --batch_size  Số lượng URL xử lý mỗi batch (default: 50).
    --sleep       Thời gian nghỉ giữa các batch (giây).

Lưu ý:
    - Rất phù hợp để chạy dài hạn dưới dạng service hoặc screen.
    - Cột in_progress là chìa khóa để tránh race condition giữa nhiều tool.
"""

import os
import sqlite3
import time
import signal
import sys
from utils import (
    trigger_cache,
    url_to_cache_paths,
    upload_files_to_s3,
    check_cpu_usage_and_wait,
)

shutdown_requested = False

def signal_handler(signum, frame):
    global shutdown_requested
    print(f"\nINFO: Received signal {signum}. Finishing current batch and shutting down...")
    shutdown_requested = True

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

class DatabaseManager:
    def __init__(self, db_path):
        self.db_path = db_path
        self.conn_select = None
        self.conn_update = None
    
    def __enter__(self):
        self.conn_select = get_select_connection(self.db_path)
        self.conn_update = get_update_connection(self.db_path)
        return self
    
    def __exit__(self, exc_type, exc_val, exc_tb):
        if self.conn_select:
            self.conn_select.close()
        if self.conn_update:
            self.conn_update.close()
    
    def get_batch_urls(self, lang="all", page_type="all", tag=None, batch_size=50):
        cur = self.conn_select.cursor()
        q = """
        SELECT id, url 
        FROM urls 
        WHERE in_progress=0
        """
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
        q += " ORDER BY last_update ASC NULLS FIRST"
        q += f" LIMIT {batch_size}"
        return cur.execute(q, qparams).fetchall()
    
    def lock_batch_urls(self, url_ids):
        cur = self.conn_update.cursor()
        placeholders = ','.join(['?' for _ in url_ids])
        cur.execute(f"UPDATE urls SET in_progress=1 WHERE id IN ({placeholders})", url_ids)
        self.conn_update.commit()
        print(f"INFO: Locked {len(url_ids)} URLs for processing")
    
    def unlock_batch_urls(self, url_ids):
        cur = self.conn_update.cursor()
        placeholders = ','.join(['?' for _ in url_ids])
        cur.execute(f"UPDATE urls SET in_progress=0 WHERE id IN ({placeholders})", url_ids)
        self.conn_update.commit()
        print(f"INFO: Unlocked {len(url_ids)} URLs")
    
    def update_url_success(self, url_id):
        cur = self.conn_update.cursor()
        ts = int(time.time())
        cur.execute("UPDATE urls SET cached=1, last_update=?, in_progress=0 WHERE id=?", (ts, url_id))
        self.conn_update.commit()
    
    def update_url_failed(self, url_id):
        cur = self.conn_update.cursor()
        cur.execute("UPDATE urls SET in_progress=0 WHERE id=?", (url_id,))
        self.conn_update.commit()

def process_single_url(db_manager, url_id, url):
    try:
        print(f"Renewing cache: {url}")
        files = url_to_cache_paths(url)

        removed_count = 0
        for file_path in files:
            if os.path.exists(file_path):
                os.remove(file_path)
                removed_count += 1
        if removed_count > 0:
            print(f"INFO: Removed {removed_count} old cache files")

        check_cpu_usage_and_wait()
        trigger_cache(url)
        upload_files_to_s3(files)  # upload lên tất cả bucket
        db_manager.update_url_success(url_id)
        
        print(f"SUCCESS: Renewed cache for {url}")
        return True
    except Exception as e:
        print(f"ERROR: Failed to renew cache for {url}: {e}")
        try:
            db_manager.update_url_failed(url_id)
        except Exception as db_error:
            print(f"ERROR: Failed to unlock URL {url_id} in database: {db_error}")
        return False

def cache_renew_infinity(db_path="output/cache_urls.db", lang="all", page_type="all", tag=None, batch_size=50, sleep_between_batches=5):
    global shutdown_requested
    print("="*60)
    print("CACHE RENEW INFINITY LOOP STARTED")
    print("="*60)
    print(f"Database: {db_path}")
    print(f"Language: {lang}")
    print(f"Page type: {page_type}")
    print(f"Tag: {tag}")
    print(f"Batch size: {batch_size}")
    print(f"Sleep between batches: {sleep_between_batches}s")
    print("INFO: Using separate SELECT and UPDATE connections")
    print("Press Ctrl+C to stop gracefully...")
    print("="*60)
    
    total_processed = 0
    total_batches = 0
    
    while not shutdown_requested:
        try:
            with DatabaseManager(db_path) as db_manager:
                batch_urls = db_manager.get_batch_urls(lang, page_type, tag, batch_size)
                if not batch_urls:
                    print("INFO: No URLs found for processing. Waiting...")
                    time.sleep(sleep_between_batches * 2)
                    continue
                
                total_batches += 1
                batch_ids = [url_id for url_id, url in batch_urls]
                print(f"\nBATCH #{total_batches}: Processing {len(batch_urls)} URLs")
                print("-" * 40)
                db_manager.lock_batch_urls(batch_ids)
                
                batch_success = 0
                batch_failed = 0
                processed_ids = []
                
                for url_id, url in batch_urls:
                    if shutdown_requested:
                        print("INFO: Shutdown requested, stopping current batch...")
                        break
                    success = process_single_url(db_manager, url_id, url)
                    processed_ids.append(url_id)
                    if success:
                        batch_success += 1
                    else:
                        batch_failed += 1
                    total_processed += 1
                
                unprocessed_ids = [url_id for url_id, url in batch_urls if url_id not in processed_ids]
                if unprocessed_ids:
                    db_manager.unlock_batch_urls(unprocessed_ids)
                
                print("-" * 40)
                print(f"BATCH #{total_batches} COMPLETED:")
                print(f"  Success: {batch_success}")
                print(f"  Failed: {batch_failed}")
                print(f"  Total processed: {total_processed}")
                
                if not shutdown_requested:
                    print(f"INFO: Sleeping {sleep_between_batches}s before next batch...")
                    time.sleep(sleep_between_batches)
                    
        except KeyboardInterrupt:
            print("\nINFO: Keyboard interrupt received")
            shutdown_requested = True
        except Exception as e:
            print(f"ERROR: Unexpected error in main loop: {e}")
            print("INFO: Sleeping 30s before retry...")
            time.sleep(30)
    
    print("\n" + "="*60)
    print("CACHE RENEW INFINITY LOOP STOPPED")
    print(f"Total batches processed: {total_batches}")
    print(f"Total URLs processed: {total_processed}")
    print("="*60)

def main():
    import argparse
    parser = argparse.ArgumentParser(
        description="Tool Cache Renew (Infinity Loop) - làm mới cache liên tục từ DB"
    )
    parser.add_argument("--db", default="output/cache_urls.db", help="Đường dẫn file SQLite DB")
    parser.add_argument("--lang", default="all", help="Ngôn ngữ (vd: vi, en, hoặc 'all')")
    parser.add_argument("--page_type", default="all", help="Loại trang (vd: single, archive, hoặc 'all')")
    parser.add_argument("--tag", default=None, help="Tag để lọc URL cụ thể")
    parser.add_argument("--batch_size", type=int, default=50, help="Số lượng URL xử lý mỗi batch")
    parser.add_argument("--sleep", type=int, default=5, help="Thời gian nghỉ giữa các batch (giây)")
    
    args = parser.parse_args()

    # Gắn signal handler để shutdown an toàn
    signal.signal(signal.SIGINT, signal_handler)
    signal.signal(signal.SIGTERM, signal_handler)

    cache_renew_infinity(
        db_path=args.db,
        lang=args.lang,
        page_type=args.page_type,
        tag=args.tag,
        batch_size=args.batch_size,
        sleep_between_batches=args.sleep,
    )

if __name__ == "__main__":
    main()
