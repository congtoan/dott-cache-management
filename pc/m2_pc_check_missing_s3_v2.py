'''
Cần chạy trước tool "D:/Real/Python/2025-01-18 s3 sync with boto3/2025-09-05_S3_Sync_Object_S3_to_S3_v6/cache-segment-update-v2.py"
Để xuất dánh ách file cache vào thư mục manifest
'''

'''
2025-10-23. Viết lại cho rõ ràng dễ hiểu hơn, loại bỏ mấy cái plus vì ko cần thiết.
Trích xuất tất cả url có đủ 2 version cache trên s3: s3_complete
Lấy tất cả url trong db: db_urls
db_urls trừ cho s3_complete: ra danh sách missing_url trên s3 cần upload, gọi là missing_s3.txt

'''

import os
import json
import sqlite3
from concurrent.futures import ThreadPoolExecutor, as_completed

# ========================
# Config tuyệt đối (absolute path)
from paths import DB_PATH, MISSING_S3_FILE as OUT_FILE, latest_manifest_dir  # 2026-11-25: chuyen sang data/
MANIFEST_DIR = latest_manifest_dir()  # tu lay manifest moi nhat trong data/manifests (output cua m0)
DEFAULT_WORKERS = 8
# ========================


def process_manifest_file(filepath: str) -> list:
    """Đọc một file manifest và trả về list URL tương ứng."""
    urls = []
    try:
        with open(filepath, "r", encoding="utf-8") as f:
            data = json.load(f)
            for obj in data.get("files", []):
                key = obj["file_key"]
                # Bỏ qua gzip
                if key.endswith("_gzip"):
                    continue

                # Chỉ lấy file HTML chính
                if key.endswith("index-https.html") or key.endswith("index-mobile-https.html"):
                    key = key.rsplit("/", 1)[0]  # Lấy thư mục chứa file
                    url = key.replace("_cache_html", "https://diaocthongthai.com").rstrip("/")
                    urls.append(url)
    except Exception as e:
        print(f"[ERROR] Failed to process {filepath}: {e}")
    return urls


def load_urls_from_manifest(manifest_dir: str, workers: int = DEFAULT_WORKERS) -> set:
    """Đọc toàn bộ manifest, gom tất cả URL có cache trên S3."""
    manifest_files = [
        os.path.join(manifest_dir, f)
        for f in os.listdir(manifest_dir)
        if f.startswith("file_manifest_") and f.endswith(".json")
    ]
    total = len(manifest_files)
    print(f"Found {total} manifest files")

    all_urls = []
    with ThreadPoolExecutor(max_workers=workers) as executor:
        futures = {executor.submit(process_manifest_file, f): f for f in manifest_files}
        for i, future in enumerate(as_completed(futures), start=1):
            try:
                all_urls.extend(future.result())
            except Exception as e:
                print(f"[ERROR] {futures[future]}: {e}")
            if i % 5 == 0 or i == total:
                print(f"[Progress] Processed {i}/{total} manifest files")

    s3_urls_set = set(all_urls)
    print(f"Total unique URLs found on S3: {len(s3_urls_set)}")
    return s3_urls_set


def get_urls_from_db(db_path=DB_PATH) -> list:
    """Lấy danh sách URL từ database."""
    conn = sqlite3.connect(db_path)
    cur = conn.cursor()
    urls = [row[0].rstrip("/") for row in cur.execute("SELECT url FROM urls")]
    conn.close()
    return urls


def main(manifest_dir=MANIFEST_DIR, db_path=DB_PATH, out_file=OUT_FILE, workers=DEFAULT_WORKERS):
    print("=== Checking missing cache on S3 ===")
    if not manifest_dir or not os.path.isdir(manifest_dir):
        raise SystemExit("Chua co manifest trong data/manifests. Chay m0_pc_s3_manifest_generator_v2_asyncio.py truoc.")
    print(f"Manifest: {manifest_dir}")

    # Load danh sách cache có trên S3
    s3_urls = load_urls_from_manifest(manifest_dir, workers)

    # Load danh sách URL từ DB
    db_urls = get_urls_from_db(db_path)
    print(f"Total URLs in DB: {len(db_urls)}")

    # Tính toán missing
    missing_urls = [url for url in db_urls if url not in s3_urls]
    print(f"Missing {len(missing_urls)} URLs on S3")

    # Ghi ra file
    with open(out_file, "w", encoding="utf-8") as f:
        for url in missing_urls:
            f.write(url + "\n")

    print(f"✅ Done. Missing URLs saved to: {out_file}")


if __name__ == "__main__":
    main()
