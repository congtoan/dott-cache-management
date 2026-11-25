#!/usr/bin/env python3
"""
u1_compare_json_and_db.py (FIXED - NO NORMALIZATION)

- Đọc tất cả file .json trong JSON_DIR (xuất từ wp_cli_extract_all_urls.py)
- So sánh với danh sách URL hiện có trong SQLite DB (cache_urls.db)
- Sinh 2 file text (mỗi dòng 1 URL, GIỮ NGUYÊN format từ nguồn):
    - u1_urls_to_add.txt      (có trong JSON mà chưa có trong DB)
    - u1_urls_to_remove.txt   (có trong DB mà không còn trong JSON)

FIX: Loại bỏ normalize_url() - giữ nguyên URLs từ JSON vì:
  - URLs từ WordPress đã chuẩn (lowercase + trailing slash)
  - Worker đã chuẩn hóa tất cả requests
  - DB/KV đều dùng format đã chuẩn hóa
"""

import os
import json
import sqlite3
from typing import Set, List

# =============== CONFIG ===============
from paths import DB_PATH, JSON_DIR, U1_ADD_FILE as OUT_ADD, U1_REMOVE_FILE as OUT_REMOVE  # 2026-11-25: chuyen sang data/
# ======================================


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
            # Chỉ strip whitespace, GIỮ NGUYÊN trailing slash
            urls.extend([x.strip() for x in data if x.strip()])
            return urls
        # list of objects
        for item in data:
            if isinstance(item, str):
                urls.append(item.strip())
            elif isinstance(item, dict):
                # common key names
                for k in ("url", "link", "href"):
                    if k in item and isinstance(item[k], str):
                        urls.append(item[k].strip())
                        break
                else:
                    # fallback: try values that look like urls
                    for v in item.values():
                        if isinstance(v, str) and (v.startswith("http") or v.startswith("/")):
                            urls.append(v.strip())
                            break
    elif isinstance(data, dict):
        # If dict: maybe {"urls": [...]} or mapping
        # try common keys
        for k in ("urls", "data", "items"):
            if k in data and isinstance(data[k], list):
                # recursively extract from that list
                for itm in data[k]:
                    if isinstance(itm, str):
                        urls.append(itm.strip())
                    elif isinstance(itm, dict):
                        if "url" in itm and isinstance(itm["url"], str):
                            urls.append(itm["url"].strip())
                if urls:
                    return urls
        # fallback: take keys or string values that look like urls
        for key, val in data.items():
            if isinstance(val, str) and (val.startswith("http") or val.startswith("/")):
                urls.append(val.strip())
            elif isinstance(key, str) and (key.startswith("http") or key.startswith("/")):
                urls.append(key.strip())
    
    # Filter out empty strings
    return [u for u in urls if u]


def gather_urls_from_json_dir(json_dir: str) -> Set[str]:
    """Scan json_dir for .json files and extract URLs.
    NO normalization - keep original format from JSON.
    """
    if not os.path.isdir(json_dir):
        raise FileNotFoundError(f"JSON_DIR not found: {json_dir}")

    files = [
        os.path.join(json_dir, f)
        for f in os.listdir(json_dir)
        if f.lower().endswith(".json")
    ]
    files.sort()
    print(f"📂 Found {len(files)} JSON files in {json_dir}")

    urls_set = set()
    total_extracted = 0
    
    for i, fp in enumerate(files, start=1):
        urls = load_urls_from_json_file(fp)
        # Chỉ filter empty, KHÔNG normalize
        valid_urls = {u for u in urls if u}
        urls_set.update(valid_urls)
        total_extracted += len(valid_urls)
        
        if i % 10 == 0 or i == len(files):
            print(f"   [Progress] {i}/{len(files)} files | {len(urls_set)} unique URLs")

    print(f"✅ Extracted {len(urls_set)} unique URLs from JSON (total: {total_extracted})")
    return urls_set


def load_urls_from_db(db_path: str) -> Set[str]:
    """Load all URLs from DB table 'urls' (SELECT url FROM urls).
    Keep original format - NO normalization.
    """
    if not os.path.exists(db_path):
        raise FileNotFoundError(f"DB not found: {db_path}")

    conn = sqlite3.connect(db_path)
    cur = conn.cursor()
    try:
        rows = cur.execute("SELECT url FROM urls").fetchall()
    finally:
        conn.close()
    
    # Chỉ strip whitespace, giữ nguyên trailing slash
    urls = {r[0].strip() for r in rows if r and r[0]}
    print(f"📊 Loaded {len(urls)} URLs from DB")
    return urls


def write_list_to_file(lines: List[str], out_path: str):
    """Write list to file, one line per item."""
    with open(out_path, "w", encoding="utf-8") as f:
        for line in lines:
            f.write(line + "\n")
    print(f"   💾 Saved: {out_path}")


def main():
    print("=" * 70)
    print("🔍 u1_compare_json_and_db - Compare JSON vs DB (NO normalization)")
    print("=" * 70)
    print(f"📍 DB: {DB_PATH}")
    print(f"📍 JSON_DIR: {JSON_DIR}")
    print(f"📍 Output: {OUT_ADD}, {OUT_REMOVE}")
    print("=" * 70)

    # Load URLs from both sources
    try:
        json_urls = gather_urls_from_json_dir(JSON_DIR)
    except Exception as e:
        print(f"❌ Cannot gather JSON urls: {e}")
        return 2

    try:
        db_urls = load_urls_from_db(DB_PATH)
    except Exception as e:
        print(f"❌ Cannot load DB urls: {e}")
        return 3

    # Compute differences (NO normalization - direct comparison)
    to_add = sorted(json_urls - db_urls)
    to_remove = sorted(db_urls - json_urls)

    print("\n" + "=" * 70)
    print("📊 COMPARISON RESULTS")
    print("=" * 70)
    print(f"➕ URLs to ADD: {len(to_add)}")
    print(f"➖ URLs to REMOVE: {len(to_remove)}")
    print(f"✅ URLs in sync: {len(json_urls & db_urls)}")

    # Show samples if any
    if to_add:
        print(f"\n📋 Sample URLs to add (first 5):")
        for url in to_add[:5]:
            print(f"   + {url}")
    
    if to_remove:
        print(f"\n📋 Sample URLs to remove (first 5):")
        for url in to_remove[:5]:
            print(f"   - {url}")

    # Write outputs
    print("\n" + "=" * 70)
    print("💾 WRITING OUTPUT FILES")
    print("=" * 70)
    write_list_to_file(to_add, OUT_ADD)
    write_list_to_file(to_remove, OUT_REMOVE)

    print("\n" + "=" * 70)
    print("✅ DONE!")
    print("=" * 70)
    print("⚠️  IMPORTANT: URLs are kept in original format (with trailing slash)")
    print("⚠️  This matches Worker normalization and DB/KV format")
    
    return 0


if __name__ == "__main__":
    raise SystemExit(main())