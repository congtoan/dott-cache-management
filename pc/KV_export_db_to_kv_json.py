#!/usr/bin/env python3
"""
export_kv_json.py
- Export toàn bộ URL từ cache_urls.db
- Xuất ra JSON format để import vào Cloudflare KV
"""

import sqlite3
import json
import argparse
from urllib.parse import urlparse

def fetch_all_urls(db_path: str):
    conn = sqlite3.connect(db_path)
    cur = conn.cursor()
    cur.execute("SELECT url FROM urls")
    rows = cur.fetchall()
    conn.close()
    return [row[0] for row in rows]

def to_path(url: str) -> str:
    """Chuyển URL đầy đủ thành chỉ path (/abc/, /en/abc/, ...)"""
    parsed = urlparse(url)
    return parsed.path if parsed.path else "/"

def export_to_kv_json(urls, output_file: str):
    # loại bỏ trùng path nếu có
    seen = set()
    data_out = []
    for url in urls:
        path = to_path(url)
        if path not in seen:
            data_out.append({"key": path, "value": "1"})
            seen.add(path)

    with open(output_file, "w", encoding="utf-8") as f:
        json.dump(data_out, f, ensure_ascii=False, indent=2)

    print(f"✅ Exported {len(data_out)} unique URLs to {output_file}")

def main():
    parser = argparse.ArgumentParser()
    from paths import DB_PATH, KV_EXPORT_FILE  # 2026-11-25: mac dinh doc/ghi data/
    parser.add_argument("--db", default=DB_PATH, help="Path to SQLite database file")
    parser.add_argument("--out", default=KV_EXPORT_FILE, help="Output JSON file for KV import")
    args = parser.parse_args()

    urls = fetch_all_urls(args.db)
    export_to_kv_json(urls, args.out)

if __name__ == "__main__":
    main()
