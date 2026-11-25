#!/usr/bin/env python3
import sqlite3
import time
from utils import (
    trigger_cache,
    url_to_cache_paths,
    upload_files_to_s3,
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

def create_missing_cache(
    db_path="output/cache_urls.db",
    lang="all",
    page_type="all",
    tag=None,
    limit=None,
):
    """Tạo cache cho các URL chưa có (cached=0)."""
    conn_select = get_select_connection(db_path)
    conn_update = get_update_connection(db_path)
    cur_update = conn_update.cursor()
    cur_select = conn_select.cursor()

    q = "SELECT id, url FROM urls WHERE cached=0"
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

        check_cpu_usage_and_wait()
        trigger_cache(url)
        upload_files_to_s3(files)

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
        description="Create cache for URLs with cached=0"
    )
    parser.add_argument("--db", default="output/cache_urls.db")
    parser.add_argument("--lang", default="all")
    parser.add_argument("--page_type", default="all")
    parser.add_argument("--tag")
    parser.add_argument("--limit", type=int)
    args = parser.parse_args()

    create_missing_cache(
        db_path=args.db,
        lang=args.lang,
        page_type=args.page_type,
        tag=args.tag,
        limit=args.limit,
    )

if __name__ == "__main__":
    main()
