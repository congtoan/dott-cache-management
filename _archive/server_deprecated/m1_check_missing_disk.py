#!/usr/bin/env python3
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
