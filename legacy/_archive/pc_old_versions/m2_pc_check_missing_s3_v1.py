'''
Cần chạy trước tool "D:/Real/Python/2025-01-18 s3 sync with boto3/2025-09-05_S3_Sync_Object_S3_to_S3_v6/cache-segment-update-v2.py"
Để xuất dánh ách file cache vào thư mục manifest
'''

import os
import json
import sqlite3
from collections import Counter
from concurrent.futures import ThreadPoolExecutor, as_completed

# ========================
# Config tuyệt đối (absolute path)
MANIFEST_DIR = r"D:\Real\Python\2025-01-18 s3 sync with boto3\2025-09-05_S3_Sync_Object_S3_to_S3_v6\manifest-v2-20250917_015220"
DB_PATH = r"D:\Real\Python\2025-01-18 s3 sync with boto3\2025-09-05_S3_Sync_Object_S3_to_S3_v6\cache-tools-new\cache_urls.db"
OUT_FILE = "missing_s3.txt"
# missing = [url for url in db_urls if url not in s3_complete]
# → missing là các URL không có cache đầy đủ trên S3,
# tức là hoàn toàn không có cache, hoặc chỉ có 1 phần.

OUT_FILE_PLUS = "missing_s3_plus.txt" 
# cái missing_s3_plus này không cần dùng tới, đã nằm hết trong missing_s3.txt rồi

MERGED_FILE = "missing_s3_all.txt"
DEFAULT_WORKERS = 8
# ========================

def process_manifest_file(filepath: str) -> list:
    urls = []
    try:
        with open(filepath, "r", encoding="utf-8") as f:
            data = json.load(f)
            for obj in data.get("files", []):
                key = obj["file_key"]
                # Bỏ qua file gzip
                if key.endswith("_gzip"):
                    continue
                # Bỏ file html -> lấy thư mục
                if key.endswith("index-https.html"):
                    key = key.replace("index-https.html", "")
                elif key.endswith("index-mobile-https.html"):
                    key = key.replace("index-mobile-https.html", "")
                else:
                    continue
                url = key.replace("_cache_html", "https://diaocthongthai.com").rstrip("/")
                urls.append(url)
    except Exception as e:
        print(f"[ERROR] Failed to process {filepath}: {e}")
    return urls

def load_urls_from_manifest(manifest_dir: str, workers: int = DEFAULT_WORKERS) -> (set, set):
    files = [
        os.path.join(manifest_dir, f)
        for f in os.listdir(manifest_dir)
        if f.startswith("file_manifest_") and f.endswith(".json")
    ]
    total = len(files)
    print(f"Found {total} manifest files")

    all_urls = []
    with ThreadPoolExecutor(max_workers=workers) as executor:
        future_to_file = {executor.submit(process_manifest_file, f): f for f in files}
        for i, fut in enumerate(as_completed(future_to_file), start=1):
            try:
                all_urls.extend(fut.result())
            except Exception as e:
                print(f"[ERROR] {future_to_file[fut]}: {e}")
            if i % 5 == 0 or i == total:
                print(f"[Progress] Processed {i}/{total} manifest files")

    counts = Counter(all_urls)

    # Nhóm 1: thiếu (chỉ có 1 phiên bản)
    missing_plus = {url for url, c in counts.items() if c == 1}

    # Nhóm 2: đủ (có cả desktop + mobile)
    complete = {url for url, c in counts.items() if c >= 2}

    return complete, missing_plus

def get_urls_from_db(db_path=DB_PATH) -> list:
    conn = sqlite3.connect(db_path)
    cur = conn.cursor()
    urls = [row[0].rstrip("/") for row in cur.execute("SELECT url FROM urls")]
    conn.close()
    return urls

def main(manifest_dir=MANIFEST_DIR, db_path=DB_PATH,
         out_file=OUT_FILE, out_file_plus=OUT_FILE_PLUS,
         merged_file=MERGED_FILE, workers=DEFAULT_WORKERS):
    print("Loading cached URLs from manifest...")
    s3_complete, s3_missing_plus = load_urls_from_manifest(manifest_dir, workers=workers)
    print(f"Found {len(s3_complete)} complete URLs in manifest")
    print(f"Found {len(s3_missing_plus)} half-missing URLs in manifest")

    print("Loading URLs from DB...")
    db_urls = get_urls_from_db(db_path)
    print(f"Loaded {len(db_urls)} URLs from DB")

    # Bình thường: so sánh DB với complete set
    missing = [url for url in db_urls if url not in s3_complete]

    print(f"Missing {len(missing)} URLs on S3 (completely missing)")
    print(f"Missing {len(s3_missing_plus)} URLs on S3 (half-missing)")

    # Xuất file
    with open(out_file, "w", encoding="utf-8") as f:
        for url in missing:
            f.write(url + "\n")
    with open(out_file_plus, "w", encoding="utf-8") as f:
        for url in sorted(s3_missing_plus):
            f.write(url + "\n")

    # Merge chung
    with open(merged_file, "w", encoding="utf-8") as f:
        for url in missing:
            f.write(url + "\n")
        for url in sorted(s3_missing_plus):
            f.write(url + "\n")

    print(f"Done. Missing URLs saved to {out_file}, {out_file_plus}, and merged into {merged_file}")

if __name__ == "__main__":
    main()
