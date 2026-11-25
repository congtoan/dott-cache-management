#!/usr/bin/env python3
import sqlite3
from concurrent.futures import ThreadPoolExecutor, as_completed
from utils import url_to_cache_paths, are_cache_files_on_s3, write_urls_to_file

def check_url_s3(url):
    files = url_to_cache_paths(url)
    ok = are_cache_files_on_s3(files[0])
    return None if ok else url

def main(db_path="output/cache_urls.db", out_file="missing_s3.txt", workers=10):
    conn = sqlite3.connect(db_path)
    cur = conn.cursor()
    urls = [row[0] for row in cur.execute("SELECT url FROM urls")]
    conn.close()

    total = len(urls)
    missing = []
    processed = 0

    print(f"Checking {total} urls on S3 using {workers} threads...")

    with ThreadPoolExecutor(max_workers=workers) as ex:
        futures = {ex.submit(check_url_s3, url): url for url in urls}
        for fut in as_completed(futures):
            processed += 1
            url = futures[fut]
            res = fut.result()
            if res:
                missing.append(res)
                print(f"[{processed}/{total}] ❌ Missing on S3: {res}")
            else:
                if processed % 100 == 0:
                    print(f"[{processed}/{total}] ✓ checked...")

    write_urls_to_file(missing, out_file)
    print(f"\nDone. Missing cache on S3: {len(missing)} urls → {out_file}")

if __name__ == "__main__":
    main()
