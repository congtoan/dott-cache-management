#!/usr/bin/env python3
"""
u2_update_db_and_kv_v3.4.py

v3.4 (2026-11-25) - SUA LOI F4: key KV phai la PATH chuan hoa (vd "/abc/"), khong phai URL day du.
  Worker thongthai-cache-v7.js tra KV bang normalizePath(url.pathname) = lowercase + "/" cuoi.
  v3.3 ghi/xoa key "https://diaocthongthai.com/abc/" -> Worker khong bao gio thay (3.865 key sai,
  da sua tren KV bang tools/fix_f4_kv_keys.py). DB van luu URL day du nhu cu; chi KV dung path.

- Cập nhật SQLite DB và Cloudflare Workers KV theo 2 file:
    u1_urls_to_add.txt
    u1_urls_to_remove.txt

- Tính năng:
  * Backup DB (backup -> backups/)
  * Thêm/xóa trên DB (executemany)
  * Tạo chunk JSON để upload KV (format [{"key":..., "value":"1"}, ...])
  * Upload chunks bằng `wrangler kv bulk put --namespace-id=...`
  * Xóa keys bằng `wrangler kv key delete --namespace-id=...`
  * Hỗ trợ dry-run, disable-kv, disable-db
  
- Chạy trực tiếp: python u2_update_db_and_kv_v3.4.py

LƯU Ý: LOGIN WRANGER TRƯỚC KHI CHẠY:
wrangler login
Copy link dán vào trình duyệt để login
"""

import os
import json
import sqlite3
import shutil
import subprocess
import time
from datetime import datetime
from typing import List
from urllib.parse import urlparse


def kv_key(url_or_path: str) -> str:
    """URL/path -> key KV, GIONG HET normalizePath() trong thongthai-cache-v7.js:
    lay path, lowercase, them "/" cuoi neu path khong co dau "." (khong phai file)."""
    p = urlparse(url_or_path).path if "://" in url_or_path else url_or_path
    if p in ("", "/"):
        return "/"
    p = p.lower()
    if "." not in p and not p.endswith("/"):
        p += "/"
    return p

# ========================================
# CONFIG - HARDCODED
# ========================================
from paths import DB_PATH, U1_ADD_FILE, U1_REMOVE_FILE, KV_CHUNK_DIR  # 2026-11-25: chuyen sang data/
NAMESPACE_ID = "cc831ca12e954d0b8a875a3453abacc7"
WRANGLER_CMD = "wrangler"  # hoặc đường dẫn đầy đủ nếu chưa có trong PATH

# Input files
ADD_FILE = U1_ADD_FILE
REMOVE_FILE = U1_REMOVE_FILE

# KV Settings
RAMDISK_DIR = KV_CHUNK_DIR  # R:\kv_chunks neu co RAM disk, khong thi data\kv_chunks
CHUNK_SIZE = 3000              # số URLs mỗi chunk
DELAY_BETWEEN_CHUNKS = 3       # giây delay giữa các chunk upload

# Control flags
DRY_RUN = False      # True = chỉ xem không thực hiện
DISABLE_DB = False   # True = không cập nhật DB
DISABLE_KV = False   # True = không cập nhật KV
# ========================================


def backup_database(db_path: str) -> str:
    """Copy DB to backups/ with timestamp. Return backup path."""
    if not os.path.exists(db_path):
        raise FileNotFoundError(f"❌ Database không tồn tại: {db_path}")
    
    db_dir = os.path.dirname(os.path.abspath(db_path)) or "."
    backup_dir = os.path.join(db_dir, "backups")
    os.makedirs(backup_dir, exist_ok=True)
    
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    db_base = os.path.splitext(os.path.basename(db_path))[0]
    backup_name = f"{db_base}_{timestamp}.db"
    backup_path = os.path.join(backup_dir, backup_name)
    
    shutil.copy2(db_path, backup_path)
    print(f"🧩 Database đã backup tới: {backup_path}")
    return backup_path


def load_urls(file_path: str) -> List[str]:
    """Load urls from txt (one url per line). Return list."""
    if not os.path.exists(file_path):
        print(f"⚠️  File không tồn tại: {file_path}")
        return []
    
    with open(file_path, "r", encoding="utf-8") as f:
        urls = [line.strip() for line in f if line.strip()]
    
    print(f"📄 Đọc {len(urls)} URLs từ {file_path}")
    return urls


# ==================== DATABASE ====================
def add_urls_to_db(db_path: str, urls: List[str], dry_run: bool = False) -> int:
    """Thêm URLs vào DB"""
    if not urls:
        print("✅ Không có URL để thêm vào DB.")
        return 0
    
    print(f"➕ Đang thêm {len(urls)} URLs vào DB...", end="")
    
    if dry_run:
        print(" [DRY-RUN]")
        return len(urls)
    
    conn = sqlite3.connect(db_path)
    cur = conn.cursor()
    data = [(u, 0) for u in urls]
    cur.executemany("INSERT OR IGNORE INTO urls (url, cached) VALUES (?, ?)", data)
    conn.commit()
    conn.close()
    
    print(f" ✅ Hoàn tất!")
    return len(urls)


def remove_urls_from_db(db_path: str, urls: List[str], dry_run: bool = False) -> int:
    """Xóa URLs khỏi DB"""
    if not urls:
        print("✅ Không có URL để xóa khỏi DB.")
        return 0
    
    print(f"➖ Đang xóa {len(urls)} URLs khỏi DB...", end="")
    
    if dry_run:
        print(" [DRY-RUN]")
        return len(urls)
    
    conn = sqlite3.connect(db_path)
    cur = conn.cursor()
    cur.executemany("DELETE FROM urls WHERE url = ?", [(u,) for u in urls])
    conn.commit()
    conn.close()
    
    print(f" ✅ Hoàn tất!")
    return len(urls)


# ==================== CLOUDFLARE KV ====================
def create_kv_chunk_files(urls: List[str], ramdisk_dir: str, prefix: str = "kv_chunk", chunk_size: int = CHUNK_SIZE) -> List[str]:
    """Tạo file JSON chunk trong ramdisk_dir. Return list file paths."""
    os.makedirs(ramdisk_dir, exist_ok=True)
    chunk_files = []
    total = len(urls)
    
    for i in range(0, total, chunk_size):
        chunk = urls[i:i+chunk_size]
        payload = [{"key": u, "value": "1"} for u in chunk]
        part_idx = i // chunk_size + 1
        part_path = os.path.join(ramdisk_dir, f"{prefix}_{part_idx:04d}.json")
        
        with open(part_path, "w", encoding="utf-8") as f:
            json.dump(payload, f, ensure_ascii=False)
        
        chunk_files.append(part_path)
        print(f"   📦 Tạo chunk {part_idx}: {os.path.basename(part_path)} ({len(payload)} items)")
    
    return chunk_files


def upload_kv_chunks(chunk_files: List[str], namespace_id: str, wrangler_cmd: str = WRANGLER_CMD, delay: int = DELAY_BETWEEN_CHUNKS, dry_run: bool = False):
    """Upload từng chunk qua wrangler kv bulk put"""
    if not chunk_files:
        print("✅ Không có chunk để upload.")
        return
    
    if not namespace_id:
        raise ValueError("❌ NAMESPACE_ID không được để trống!")
    
    total = len(chunk_files)
    print(f"☁️  Bắt đầu upload {total} chunks lên Cloudflare KV...")
    
    for idx, chunk in enumerate(chunk_files, start=1):
        print(f"   [{idx}/{total}] Uploading {os.path.basename(chunk)}...", end="")
        
        if dry_run:
            print(" [DRY-RUN]")
            continue
        
        cmd = f'{wrangler_cmd} kv bulk put --namespace-id={namespace_id} "{chunk}" --remote'
        try:
            subprocess.run(cmd, shell=True, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
            print(" ✅")
        except subprocess.CalledProcessError as e:
            print(f" ❌ Lỗi: {e}")
        
        if idx < total:  # không delay sau chunk cuối
            time.sleep(delay)
    
    print(f"☁️  Upload hoàn tất {total} chunks!")


def remove_kv_keys(urls: List[str], namespace_id: str, wrangler_cmd: str = WRANGLER_CMD, batch_size: int = 100, dry_run: bool = False):
    """Xóa keys từ KV (từng key một, log batch)"""
    if not urls:
        print("✅ Không có URL để xóa khỏi KV.")
        return
    
    if not namespace_id:
        raise ValueError("❌ NAMESPACE_ID không được để trống!")
    
    total = len(urls)
    print(f"🗑️  Đang xóa {total} keys khỏi Cloudflare KV...")
    
    if dry_run:
        print("   [DRY-RUN] Bỏ qua xóa thực tế")
        return
    
    for i, url in enumerate(urls, start=1):
        cmd = f'{wrangler_cmd} kv key delete --namespace-id={namespace_id} "{url}" --remote'
        try:
            subprocess.run(cmd, shell=True, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        except subprocess.CalledProcessError as e:
            print(f"   ❌ Lỗi xóa key {url}: {e}")
        
        if i % batch_size == 0 or i == total:
            print(f"   ...Đã xử lý {i}/{total} keys")
    
    print(f"🗑️  Xóa hoàn tất {total} keys!")


# ==================== MAIN ====================
def main():
    print("=" * 60)
    print("🚀 u2_update_db_and_kv_v3.4 - BẮT ĐẦU")
    print("=" * 60)
    print(f"📍 Database: {DB_PATH}")
    print(f"📍 KV Namespace: {NAMESPACE_ID}")
    print(f"📍 Add file: {ADD_FILE}")
    print(f"📍 Remove file: {REMOVE_FILE}")
    print(f"📍 RAMDisk: {RAMDISK_DIR}")
    print(f"📍 Chunk size: {CHUNK_SIZE}")
    print(f"📍 Delay: {DELAY_BETWEEN_CHUNKS}s")
    print(f"⚙️  DRY_RUN={DRY_RUN}, DISABLE_DB={DISABLE_DB}, DISABLE_KV={DISABLE_KV}")
    print("=" * 60)
    
    # Load URLs từ file
    urls_to_add = load_urls(ADD_FILE)
    urls_to_remove = load_urls(REMOVE_FILE)
    
    if not urls_to_add and not urls_to_remove:
        print("\n⚠️  Không có URL nào để xử lý. Kết thúc.")
        return
    
    # Backup DB (trừ khi disable hoặc dry-run)
    if not DISABLE_DB and not DRY_RUN:
        try:
            backup_database(DB_PATH)
        except Exception as e:
            print(f"❌ Backup thất bại: {e}")
            return
    
    print("\n" + "=" * 60)
    print("📊 CẬP NHẬT DATABASE")
    print("=" * 60)
    
    # Cập nhật DB
    if not DISABLE_DB:
        added = add_urls_to_db(DB_PATH, urls_to_add, dry_run=DRY_RUN)
        removed = remove_urls_from_db(DB_PATH, urls_to_remove, dry_run=DRY_RUN)
        print(f"✅ DB: Thêm {added} URLs, Xóa {removed} URLs")
    else:
        print("⏭️  Bỏ qua cập nhật DB (DISABLE_DB=True)")
    
    print("\n" + "=" * 60)
    print("☁️  CẬP NHẬT CLOUDFLARE KV")
    print("=" * 60)
    
    # Cập nhật KV
    if not DISABLE_KV:
        # v3.4: DB dung URL day du, KV dung PATH (giong Worker). Bo trung sau khi chuan hoa.
        kv_add = sorted({kv_key(u) for u in urls_to_add})
        kv_remove = sorted({kv_key(u) for u in urls_to_remove} - set(kv_add))
        if urls_to_add or urls_to_remove:
            sample = (urls_to_add or urls_to_remove)[0]
            print(f"🔑 Key KV dang path, vd: {sample}  ->  {kv_key(sample)}")

        # Thêm vào KV
        if kv_add:
            print(f"\n➕ Xử lý {len(kv_add)} key để thêm vào KV:")
            chunk_files = create_kv_chunk_files(kv_add, RAMDISK_DIR, chunk_size=CHUNK_SIZE)
            upload_kv_chunks(chunk_files, NAMESPACE_ID, wrangler_cmd=WRANGLER_CMD, delay=DELAY_BETWEEN_CHUNKS, dry_run=DRY_RUN)
        else:
            print("\n✅ Không có URL để thêm vào KV")

        # Xóa khỏi KV
        if kv_remove:
            print(f"\n➖ Xử lý {len(kv_remove)} key để xóa khỏi KV:")
            remove_kv_keys(kv_remove, NAMESPACE_ID, wrangler_cmd=WRANGLER_CMD, dry_run=DRY_RUN)
        else:
            print("\n✅ Không có URL để xóa khỏi KV")
    else:
        print("⏭️  Bỏ qua cập nhật KV (DISABLE_KV=True)")
    
    print("\n" + "=" * 60)
    print("🎉 HOÀN TẤT!")
    print("=" * 60)


if __name__ == "__main__":
    main()