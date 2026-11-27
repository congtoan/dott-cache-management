#!/usr/bin/env python3
"""
u1_compare_json_and_db.py

- Đọc tất cả file .json trong JSON_DIR (xuất từ wp_cli_extract_all_urls.py)
- So sánh với danh sách URL hiện có trong SQLite DB (cache_urls.db)
- Sinh 2 file text (mỗi dòng 1 URL, normalized bằng rstrip("/")):
    - u1_urls_to_add.txt      (có trong JSON mà chưa có trong DB)
    - u1_urls_to_remove.txt   (có trong DB mà không còn trong JSON)
- Hỗ trợ CLI overrides cho paths
"""

import os
import json
import sqlite3
import argparse
from typing import Set, List

# =============== CONFIG (mặc định) ===============
DB_PATH = r"D:\Real\Python\2025-01-18 s3 sync with boto3\2025-09-05_S3_Sync_Object_S3_to_S3_v6\cache-tools-new\cache_urls.db"
JSON_DIR = r"D:\Real\Python\2025-01-18 s3 sync with boto3\2025-09-05_S3_Sync_Object_S3_to_S3_v6\cache-tools-new\wp_url_jsons"
OUT_ADD = "u1_urls_to_add.txt"
OUT_REMOVE = "u1_urls_to_remove.txt"
# ================================================


def normalize_url(u: str) -> str:
    """Normalize URL for comparison: strip whitespace and trailing slash.
    Return empty string for invalid inputs."""
    if not u:
        return ""
    return u.strip().rstrip("/")


def load_urls_from_json_file(path: str) -> List[str]:
    """Load URLs from a single JSON file.
    Handles common shapes:
      - a JSON list of strings: ["https://...","..."]
      - a JSON list of objects with 'url' key: [{"url": "..."}, ...]
      - a JSON object/dict where keys or values are urls (best-effort)
    Returns list of strings (may be empty).
    """
    urls = []
    try:
        with open(path, "r", encoding="utf-8") as f:
            data = json.load(f)
    except Exception as e:
        print(f"[WARN] Failed to load JSON {path}: {e}")
        return urls

    # Case 1: list of strings
    if isinstance(data, list):
        if data and all(isinstance(x, str) for x in data):
            urls.extend([x for x in data])
            return urls
        # list of objects
        for item in data:
            if isinstance(item, str):
                urls.append(item)
            elif isinstance(item, dict):
                # common key names
                for k in ("url", "link", "href"):
                    if k in item and isinstance(item[k], str):
                        urls.append(item[k])
                        break
                else:
                    # fallback: try values that look like urls
                    for v in item.values():
                        if isinstance(v, str) and (v.startswith("http") or v.startswith("/")):
                            urls.append(v)
                            break
    elif isinstance(data, dict):
        # If dict: maybe {"urls": [...]} or mapping
        # try common keys
        for k in ("urls", "data", "items"):
            if k in data and isinstance(data[k], list):
                # recursively extract from that list
                for itm in data[k]:
                    if isinstance(itm, str):
                        urls.append(itm)
                    elif isinstance(itm, dict):
                        if "url" in itm and isinstance(itm["url"], str):
                            urls.append(itm["url"])
                if urls:
                    return urls
        # fallback: take keys or string values that look like urls
        for key, val in data.items():
            if isinstance(val, str) and (val.startswith("http") or val.startswith("/")):
                urls.append(val)
            elif isinstance(key, str) and (key.startswith("http") or key.startswith("/")):
                urls.append(key)
    else:
        # unknown type
        pass

    return urls


def gather_urls_from_json_dir(json_dir: str) -> Set[str]:
    """Scan json_dir for .json files and extract URLs."""
    if not os.path.isdir(json_dir):
        raise FileNotFoundError(f"JSON_DIR not found: {json_dir}")

    files = [
        os.path.join(json_dir, f)
        for f in os.listdir(json_dir)
        if f.lower().endswith(".json")
    ]
    files.sort()
    print(f"[INFO] Found {len(files)} JSON files in {json_dir}")

    urls_set = set()
    total_extracted = 0
    for i, fp in enumerate(files, start=1):
        urls = load_urls_from_json_file(fp)
        # normalize and filter
        normalized = {normalize_url(u) for u in urls if normalize_url(u)}
        urls_set.update(normalized)
        total_extracted += len(normalized)
        if i % 10 == 0 or i == len(files):
            print(f"  [Progress] processed {i}/{len(files)} files; unique urls so far: {len(urls_set)}")

    print(f"[DONE] Extracted {len(urls_set)} unique URLs from JSON files (total items seen: {total_extracted})")
    return urls_set


def load_urls_from_db(db_path: str) -> Set[str]:
    """Load all URLs from DB table 'urls' (SELECT url FROM urls).
    Normalize same as JSON side.
    """
    if not os.path.exists(db_path):
        raise FileNotFoundError(f"DB not found: {db_path}")

    conn = sqlite3.connect(db_path)
    cur = conn.cursor()
    try:
        rows = cur.execute("SELECT url FROM urls").fetchall()
    finally:
        conn.close()
    urls = {normalize_url(r[0]) for r in rows if r and r[0]}
    print(f"[INFO] Loaded {len(urls)} URLs from DB ({db_path})")
    return urls


def write_list_to_file(lines: List[str], out_path: str):
    with open(out_path, "w", encoding="utf-8") as f:
        for line in lines:
            f.write(line + "\n")


def main(argv=None):
    parser = argparse.ArgumentParser(description="u1_compare_json_and_db: produce u1_urls_to_add.txt and u1_urls_to_remove.txt")
    parser.add_argument("--db", default=DB_PATH, help="Path to cache_urls.db")
    parser.add_argument("--json-dir", default=JSON_DIR, help="Directory containing JSON files exported from WP-CLI")
    parser.add_argument("--out-add", default=OUT_ADD, help="Output file for URLs to add")
    parser.add_argument("--out-remove", default=OUT_REMOVE, help="Output file for URLs to remove")
    args = parser.parse_args(argv)

    try:
        json_urls = gather_urls_from_json_dir(args.json_dir)
    except Exception as e:
        print(f"[ERROR] Cannot gather JSON urls: {e}")
        return 2

    try:
        db_urls = load_urls_from_db(args.db)
    except Exception as e:
        print(f"[ERROR] Cannot load DB urls: {e}")
        return 3

    # Compute diffs (normalized, no trailing slash)
    to_add = sorted(json_urls - db_urls)
    to_remove = sorted(db_urls - json_urls)

    print(f"[RESULT] URLs to add: {len(to_add)}")
    print(f"[RESULT] URLs to remove: {len(to_remove)}")

    # write outputs
    write_list_to_file(to_add, args.out_add)
    write_list_to_file(to_remove, args.out_remove)

    print(f"[DONE] Wrote add-file: {args.out_add}")
    print(f"[DONE] Wrote remove-file: {args.out_remove}")
    print("Note: files contain normalized URLs (no trailing slash). You can edit them by hand if needed before running u2_update_db_and_kv.py")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
