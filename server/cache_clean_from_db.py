#!/usr/bin/env python3
"""
Tool: Cache Clean
=================

Công dụng:
    - Dọn dẹp các thư mục/file cache trên disk (TARGET_DIR) không còn tương ứng với URL trong database.
    - Đồng bộ xóa cả trên S3 bucket nếu file/thư mục cache đã được upload.

Cách hoạt động:
    1. Đọc danh sách URL từ DB và chuyển đổi sang tập hợp các thư mục cache hợp lệ (normalize path).
    2. Quét toàn bộ TARGET_DIR:
       - Nếu thư mục không nằm trong danh sách hợp lệ:
           + Nếu có sub-folder: chỉ xóa file cache trực tiếp (index-https.html, gzip, mobile...).
           + Nếu không có sub-folder: xóa cả thư mục.
    3. Với mỗi file/thư mục bị xóa → xóa object tương ứng trên S3 (nếu tồn tại).

Điểm kỹ thuật:
    - Path được normalize bằng `os.path.normpath()` để tránh bug `/` cuối → không xoá nhầm cache hợp lệ.
    - Không sử dụng batch size vì tool quét toàn bộ thư mục một lần.
    - Cột in_progress trong DB không tham gia (tool chỉ đọc danh sách URL).
    - Thận trọng: nếu thư mục có sub-folder → không xoá cả thư mục, chỉ xoá file cache trực tiếp trong thư mục đó.

Cách sử dụng:
    python cache_clean_from_db.py --db output/cache_urls.db --bucket thongthai_chicago
    python cache_clean_from_db.py --db output/cache_urls.db --bucket thongthai_chicago thongthai_singapore
"""

import os
import sqlite3
import shutil
from urllib.parse import urlparse
from utils import TARGET_DIR, BUCKET_CONFIGS, _get_s3_client_from_bucket_name, S3_PREFIX


def pre_clean_search_dir(bucket_names):
    """Xóa toàn bộ thư mục /search/ trên disk và S3 trước khi clean chính thức."""
    search_dir = os.path.join(TARGET_DIR, "search")
    if os.path.isdir(search_dir):
        # Xóa toàn bộ search local
        shutil.rmtree(search_dir)
        print(f"Pre-clean: Deleted local directory (rm -rf): {search_dir}")


def get_valid_cache_dirs(db_path):
    """Lấy tất cả đường dẫn hợp lệ từ database (normalize path để tránh mismatch)."""
    conn = sqlite3.connect(db_path, timeout=30.0)
    cur = conn.cursor()
    cur.execute("SELECT url FROM urls")
    urls = [row[0] for row in cur.fetchall()]
    conn.close()

    valid_dirs = set()
    for url in urls:
        parsed = urlparse(url)
        rel_path = parsed.path.lstrip("/")
        # normalize để loại bỏ "/" dư thừa
        dir_path = os.path.normpath(os.path.join(TARGET_DIR, rel_path))
        valid_dirs.add(dir_path)
    return valid_dirs


def delete_files_in_dir(dir_path, bucket_names):
    """Chỉ xóa file cache trong thư mục, giữ nguyên sub-folder."""
    variants = [
        "index-https.html",
        "index-https.html_gzip",
        "index-mobile-https.html",
        "index-mobile-https.html_gzip",
    ]
    for fname in variants:
        fpath = os.path.join(dir_path, fname)
        if os.path.exists(fpath):
            os.remove(fpath)
            print(f"Deleted local file: {fpath}")
            # Xóa trên S3
            delete_from_s3(fpath, bucket_names)


def delete_entire_dir(dir_path, bucket_names):
    """Xóa cả thư mục (nếu không có sub-folder)."""
    if os.path.isdir(dir_path):
        shutil.rmtree(dir_path)
        print(f"Deleted local directory: {dir_path}")
        # Xóa prefix trên S3
        rel = os.path.relpath(dir_path, start=TARGET_DIR).replace("\\", "/")
        s3_prefix = f"{S3_PREFIX}/{rel}"
        for bucket in bucket_names:
            s3_client, bucket_domain = _get_s3_client_from_bucket_name(bucket)
            try:
                resp = s3_client.list_objects_v2(Bucket=bucket_domain, Prefix=s3_prefix)
                if "Contents" in resp:
                    for obj in resp["Contents"]:
                        s3_client.delete_object(Bucket=bucket_domain, Key=obj["Key"])
                        print(f"[{bucket}] Deleted s3://{bucket_domain}/{obj['Key']}")
            except Exception as e:
                print(f"[{bucket}] Error deleting prefix {s3_prefix}: {e}")


def delete_from_s3(local_path, bucket_names):
    """Xóa file cache trên S3 nếu có."""
    rel = os.path.relpath(local_path, start=TARGET_DIR).replace("\\", "/")
    if rel.startswith(".."):
        return
    if not rel.startswith(f"{S3_PREFIX}/"):
        rel = f"{S3_PREFIX}/{rel}"
    s3_key = rel
    for bucket in bucket_names:
        s3_client, bucket_domain = _get_s3_client_from_bucket_name(bucket)
        try:
            s3_client.head_object(Bucket=bucket_domain, Key=s3_key)  # check tồn tại
            s3_client.delete_object(Bucket=bucket_domain, Key=s3_key)
            print(f"[{bucket}] Deleted s3://{bucket_domain}/{s3_key}")
        except Exception:
            # Có thể key chưa tồn tại → bỏ qua
            pass


def clean_cache(db_path="output/cache_urls.db", bucket_names=None):
    if bucket_names is None:
        bucket_names = ["thongthai_chicago"]  # chỉnh ở đây nếu muốn nhiều bucket

    # bước pre-clean search
    pre_clean_search_dir(bucket_names)
    
    valid_dirs = get_valid_cache_dirs(db_path)
    print(f"Found {len(valid_dirs)} valid cache dirs from DB")

    for root, dirs, files in os.walk(TARGET_DIR, topdown=True):
        root_norm = os.path.normpath(root)
        if root_norm == os.path.normpath(TARGET_DIR):
            continue
        if root_norm not in valid_dirs:
            # Nếu có sub-folder thì chỉ xóa file cache trong root
            if dirs:
                delete_files_in_dir(root_norm, bucket_names)
            else:
                delete_entire_dir(root_norm, bucket_names)


def main():
    import argparse
    parser = argparse.ArgumentParser(description="Clean orphan cache directories not in DB")
    parser.add_argument("--db", default="output/cache_urls.db", help="SQLite DB path")
    parser.add_argument("--bucket", nargs="+", default=["thongthai_chicago"], help="List bucket names")
    args = parser.parse_args()
    clean_cache(db_path=args.db, bucket_names=args.bucket)


if __name__ == "__main__":
    main()
