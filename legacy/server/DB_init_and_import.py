#!/usr/bin/env python3
"""
db_init_and_import.py
- Khởi tạo database cache_index.db
- Import URLs từ JSON
"""
import os
import sqlite3
import json
import argparse
import re

# ===== Helpers =====
def detect_page_number(url: str) -> int:
    """Trích xuất số trang từ URL archive. Không có /page/ thì trả về 1."""
    match = re.search(r"/page/(\d+)/", url)
    return int(match.group(1)) if match else 1

def detect_lang_from_filename(filename: str) -> str:
    """Xác định ngôn ngữ từ tên file JSON."""
    if filename.startswith("en_"):
        return "en"
    elif filename.startswith("vi_"):
        return "vi"
    return "unknown"

def detect_page_type_from_filename(filename: str) -> str:
    """Xác định page_type từ tên file JSON."""
    if "archive" in filename:
        return "archive"
    elif "single" in filename:
        return "single"
    return "unknown"

# ===== DB init =====
def init_db(db_path: str):
    conn = sqlite3.connect(db_path)
    cur = conn.cursor()

    # Bảng chính chứa URLs
    cur.execute("""
    CREATE TABLE IF NOT EXISTS urls (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        url TEXT UNIQUE,
        lang TEXT,
        page_type TEXT,
        cached INTEGER DEFAULT 0,
        last_update INTEGER,
        page_number INTEGER,
        in_progress INTEGER DEFAULT 0
    )
    """)

    # Bảng phụ chứa tags
    cur.execute("""
    CREATE TABLE IF NOT EXISTS url_tags (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        url_id INTEGER,
        tag TEXT,
        FOREIGN KEY(url_id) REFERENCES urls(id)
    )
    """)

    conn.commit()
    return conn, cur

# ===== Import JSON =====
def import_json_to_db(conn, cur, json_file: str):
    if not os.path.exists(json_file):
        print(f"⚠️ File not found: {json_file}")
        return

    lang = detect_lang_from_filename(os.path.basename(json_file))
    page_type = detect_page_type_from_filename(os.path.basename(json_file))

    with open(json_file, "r", encoding="utf-8") as f:
        urls = json.load(f)

    inserted, skipped = 0, 0
    for url in urls:
        page_number = detect_page_number(url)
        try:
            cur.execute("""
                INSERT OR IGNORE INTO urls (url, lang, page_type, cached, page_number)
                VALUES (?, ?, ?, 0, ?)
            """, (url, lang, page_type, page_number))
            if cur.rowcount > 0:
                inserted += 1
            else:
                skipped += 1
        except sqlite3.Error as e:
            print(f"✗ Error inserting {url}: {e}")
    conn.commit()
    print(f"✓ Imported {inserted} URLs from {json_file} (skipped {skipped})")

# ===== Main =====
def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--db", default="cache_urls.db", help="Path to SQLite database file")
    args = parser.parse_args()

    conn, cur = init_db(args.db)

    # Import từ 4 file JSON chuẩn
    json_files = [
        "en_archive_page_urls.json",
        "en_single_post_urls.json",
        "vi_archive_page_urls.json",
        "vi_single_post_urls.json",
    ]

    for jf in json_files:
        import_json_to_db(conn, cur, jf)

    conn.close()
    print(f"\n🎉 Database initialized and data imported into {args.db}")

if __name__ == "__main__":
    main()
