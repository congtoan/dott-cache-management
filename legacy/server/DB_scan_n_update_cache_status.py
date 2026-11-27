#!/usr/bin/env python3
"""
cache_sync_from_files.py
- Kiểm tra các file cache hiện có trong thư mục
- Cập nhật cột cached và last_update trong database dựa trên file thực tế
"""
import os
import sqlite3
import time
from urllib.parse import urlparse
from utils import TARGET_DIR

def get_cache_file_path(url: str) -> str:
    """Trả về đường dẫn tới file cache chính (index-https.html)"""
    parsed = urlparse(url)
    rel_path = parsed.path.lstrip("/")
    base = os.path.join(TARGET_DIR, rel_path) if rel_path else TARGET_DIR
    return os.path.join(base, "index-https.html")

def sync_cache_status(db_path="output/cache_urls.db", lang="all", page_type="all", tag=None):
    """
    Kiểm tra file cache và cập nhật database
    """
    conn = sqlite3.connect(db_path)
    cur = conn.cursor()
    select_cur = conn.cursor()

    # Tạo query
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

    total_checked = 0
    found_cached = 0
    found_not_cached = 0

    print("Starting cache sync from existing files...")
    print(f"Cache directory: {TARGET_DIR}")
    print("-" * 60)

    for url_id, url in select_cur.execute(q, qparams):
        total_checked += 1
        cache_file = get_cache_file_path(url)
        
        if os.path.exists(cache_file):
            # File tồn tại - lấy mtime làm last_update
            mtime = int(os.path.getmtime(cache_file))
            cur.execute(
                "UPDATE urls SET cached=1, last_update=? WHERE id=?", 
                (mtime, url_id)
            )
            found_cached += 1
            print(f"CACHED: {url}")
            print(f"  File: {cache_file}")
            print(f"  Modified: {time.strftime('%Y-%m-%d %H:%M:%S', time.localtime(mtime))}")
        else:
            # File không tồn tại
            cur.execute(
                "UPDATE urls SET cached=0, last_update=NULL WHERE id=?", 
                (url_id,)
            )
            found_not_cached += 1
            print(f"NOT CACHED: {url}")
    
        # Commit mỗi 100 records để tránh mất dữ liệu
        if total_checked % 100 == 0:
            conn.commit()
            print(f"INFO: Processed {total_checked} URLs...")

    # Final commit
    conn.commit()
    conn.close()

    print("-" * 60)
    print(f"SUMMARY:")
    print(f"  Total URLs checked: {total_checked}")
    print(f"  Found cached: {found_cached}")
    print(f"  Found not cached: {found_not_cached}")
    print(f"Database updated successfully!")

def show_cache_stats(db_path="output/cache_urls.db"):
    """
    Hiển thị thống kê cache trong database
    """
    conn = sqlite3.connect(db_path)
    cur = conn.cursor()

    print("\nCACHE STATISTICS:")
    print("-" * 40)
    
    # Tổng số URLs
    cur.execute("SELECT COUNT(*) FROM urls")
    total = cur.fetchone()[0]
    print(f"Total URLs: {total}")
    
    # Số URLs đã cache
    cur.execute("SELECT COUNT(*) FROM urls WHERE cached=1")
    cached_count = cur.fetchone()[0]
    print(f"Cached URLs: {cached_count}")
    
    # Số URLs chưa cache
    not_cached = total - cached_count
    print(f"Not cached URLs: {not_cached}")
    
    # Phần trăm
    if total > 0:
        cached_percent = (cached_count / total) * 100
        print(f"Cache ratio: {cached_percent:.1f}%")
    
    # Thống kê theo ngôn ngữ
    print("\nBy Language:")
    cur.execute("""
        SELECT lang, 
               COUNT(*) as total,
               SUM(CASE WHEN cached=1 THEN 1 ELSE 0 END) as cached
        FROM urls 
        GROUP BY lang
    """)
    for lang, total_lang, cached_lang in cur.fetchall():
        percent = (cached_lang / total_lang * 100) if total_lang > 0 else 0
        print(f"  {lang}: {cached_lang}/{total_lang} ({percent:.1f}%)")
    
    # Thống kê theo page_type
    print("\nBy Page Type:")
    cur.execute("""
        SELECT page_type, 
               COUNT(*) as total,
               SUM(CASE WHEN cached=1 THEN 1 ELSE 0 END) as cached
        FROM urls 
        GROUP BY page_type
    """)
    for ptype, total_type, cached_type in cur.fetchall():
        percent = (cached_type / total_type * 100) if total_type > 0 else 0
        print(f"  {ptype}: {cached_type}/{total_type} ({percent:.1f}%)")

    conn.close()

def main():
    import argparse
    parser = argparse.ArgumentParser(
        description="Sync cache status from existing files to database"
    )
    parser.add_argument("--db", default="output/cache_urls.db", 
                       help="Path to SQLite database file")
    parser.add_argument("--lang", default="all", 
                       help="Language filter (en, vi, all)")
    parser.add_argument("--page_type", default="all", 
                       help="Page type filter (single, archive, all)")
    parser.add_argument("--tag", 
                       help="Tag filter")
    parser.add_argument("--stats-only", action="store_true", 
                       help="Only show statistics, don't sync")
    args = parser.parse_args()

    if not os.path.exists(args.db):
        print(f"ERROR: Database file not found: {args.db}")
        print("Please run db_init_and_import.py first to create the database.")
        return

    if args.stats_only:
        show_cache_stats(args.db)
    else:
        sync_cache_status(
            db_path=args.db,
            lang=args.lang,
            page_type=args.page_type,
            tag=args.tag
        )
        # Show stats after sync
        show_cache_stats(args.db)

if __name__ == "__main__":
    main()