"""
u2_update_db_and_kv_v3.py
--------------------------------------------
- Cập nhật database và Cloudflare KV theo danh sách URL thay đổi.
- Dữ liệu KV upload thực tế lên Cloudflare thông qua wrangler CLI.
- Hỗ trợ chia chunk, RAMDisk, backup DB an toàn.
"""

import os
import json
import sqlite3
import shutil
import subprocess
import time
from datetime import datetime

# ==================== CONFIG ====================
DB_PATH = r"D:\Real\Python\cache-tools-new\cache_urls.db"

# Cloudflare KV Settings
NAMESPACE_ID = "cc831ca12e954d0b8a875a3453abacc7"
WRANGLER_CMD = "wrangler"  # nếu wrangler chưa trong PATH, dùng đường dẫn đầy đủ
RAMDISK_DIR = r"R:\kv_chunks"
CHUNK_SIZE = 3000
DELAY_BETWEEN_CHUNKS = 3  # giây

# Input files
ADD_FILE = "u1_urls_to_add.txt"
REMOVE_FILE = "u1_urls_to_remove.txt"
# =================================================


# ==================== UTILS ====================
def backup_database(db_path):
    """Tạo backup DB an toàn trước khi update"""
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    db_dir = os.path.dirname(db_path)
    backup_dir = os.path.join(db_dir, "backups")
    os.makedirs(backup_dir, exist_ok=True)
    backup_name = f"{os.path.splitext(os.path.basename(db_path))[0]}_{timestamp}.db"
    backup_path = os.path.join(backup_dir, backup_name)
    shutil.copy2(db_path, backup_path)
    print(f"🧩 Database backed up to: {backup_path}")


def load_urls(file_path):
    """Đọc danh sách URL từ file txt"""
    if not os.path.exists(file_path):
        print(f"⚠️ File not found: {file_path}")
        return []
    with open(file_path, "r", encoding="utf-8") as f:
        return [line.strip() for line in f if line.strip()]


# ==================== DATABASE ====================
def add_urls_to_db(db_path, urls):
    """Thêm URL mới vào DB"""
    if not urls:
        print("✅ Không có URL mới để thêm vào DB.")
        return
    conn = sqlite3.connect(db_path)
    cur = conn.cursor()
    data = [(url, 0) for url in urls]
    cur.executemany("INSERT OR IGNORE INTO urls (url, cached) VALUES (?, ?)", data)
    conn.commit()
    conn.close()
    print(f"✅ Đã thêm {len(urls)} URL vào DB.")


def remove_urls_from_db(db_path, urls):
    """Xóa URL khỏi DB"""
    if not urls:
        print("✅ Không có URL để xóa khỏi DB.")
        return
    conn = sqlite3.connect(db_path)
    cur = conn.cursor()
    cur.executemany("DELETE FROM urls WHERE url = ?", [(u,) for u in urls])
    conn.commit()
    conn.close()
    print(f"🗑️ Đã xóa {len(urls)} URL khỏi DB.")


# ==================== CLOUDFLARE KV ====================
def create_kv_chunk_files(urls, ramdisk_dir, prefix="kv_chunk", chunk_size=CHUNK_SIZE):
    """Tạo file JSON chia nhỏ để upload lên KV"""
    os.makedirs(ramdisk_dir, exist_ok=True)
    chunk_files = []
    for i in range(0, len(urls), chunk_size):
        chunk = [{"key": url, "value": "1"} for url in urls[i:i+chunk_size]]
        chunk_file = os.path.join(ramdisk_dir, f"{prefix}_{i//chunk_size + 1}.json")
        with open(chunk_file, "w", encoding="utf-8") as cf:
            json.dump(chunk, cf, ensure_ascii=False)
        chunk_files.append(chunk_file)
        print(f"[+] Created chunk: {chunk_file} ({len(chunk)} items)")
    return chunk_files


def upload_kv_chunks(chunk_files, namespace_id, delay=3):
    """Upload từng chunk lên Cloudflare KV qua wrangler CLI"""
    total = len(chunk_files)
    for idx, chunk_file in enumerate(chunk_files, start=1):
        print(f"\n[*] Uploading chunk {idx}/{total}: {chunk_file}")
        cmd = f'{WRANGLER_CMD} kv bulk put --namespace-id={namespace_id} "{chunk_file}" --remote'
        try:
            subprocess.run(cmd, shell=True, check=True)
            print(f"[OK] Uploaded chunk {idx}/{total}")
        except subprocess.CalledProcessError as e:
            print(f"[!] Error uploading {chunk_file}: {e}")
        time.sleep(delay)


def remove_kv_keys(urls, namespace_id):
    """Xóa các key cụ thể khỏi KV (dùng wrangler kv key delete)"""
    if not urls:
        print("✅ Không có URL để xóa khỏi KV.")
        return

    total = len(urls)
    print(f"🗑️ Đang xóa {total} URL khỏi KV...")
    for i, url in enumerate(urls, start=1):
        cmd = f'{WRANGLER_CMD} kv key delete --namespace-id={namespace_id} "{url}" --remote'
        try:
            subprocess.run(cmd, shell=True, check=True)
        except subprocess.CalledProcessError as e:
            print(f"[!] Error deleting key {url}: {e}")
        if i % 100 == 0:
            print(f"   ...Đã xử lý {i}/{total}")
    print(f"✅ Đã xóa xong {total} URL khỏi KV.")


# ==================== MAIN LOGIC ====================
def main():
    print("🚀 Bắt đầu chạy u2_update_db_and_kv_v3...")
    backup_database(DB_PATH)

    urls_to_add = load_urls(ADD_FILE)
    urls_to_remove = load_urls(REMOVE_FILE)

    # --- Update DB ---
    add_urls_to_db(DB_PATH, urls_to_add)
    remove_urls_from_db(DB_PATH, urls_to_remove)

    # --- Add to KV ---
    if urls_to_add:
        print("\n⚙️ Bắt đầu thêm URL vào KV...")
        chunk_files = create_kv_chunk_files(urls_to_add, RAMDISK_DIR)
        upload_kv_chunks(chunk_files, NAMESPACE_ID, delay=DELAY_BETWEEN_CHUNKS)

    # --- Remove from KV ---
    if urls_to_remove:
        print("\n⚙️ Bắt đầu xóa URL khỏi KV...")
        remove_kv_keys(urls_to_remove, NAMESPACE_ID)

    print("\n✅ Hoàn tất cập nhật DB và Cloudflare KV!")


if __name__ == "__main__":
    main()
