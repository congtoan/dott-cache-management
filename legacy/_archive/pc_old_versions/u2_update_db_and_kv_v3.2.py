#!/usr/bin/env python3
"""
u2_update_db_and_kv_v3.py

- Cập nhật SQLite DB và Cloudflare Workers KV theo 2 file:
    u1_urls_to_add.txt
    u1_urls_to_remove.txt

- Tính năng:
  * Backup DB (backup -> backups/)
  * Thêm/xóa trên DB (executemany)
  * Tạo chunk JSON để upload KV (format [{"key":..., "value":"1"}, ...])
  * Upload chunks bằng `wrangler kv bulk put --namespace-id=...`
  * Xóa keys bằng `wrangler kv key delete --namespace-id=...`
  * Hỗ trợ dry-run, disable-kv, disable-db, ramdisk dir, chunk size, delay
"""

import os
import json
import sqlite3
import shutil
import subprocess
import time
from datetime import datetime
import argparse
from typing import List

# -------------------------
# Defaults (can be overridden by CLI)
# -------------------------
DEFAULT_DB_PATH = r"cache_urls.db"
DEFAULT_ADD_FILE = "u1_urls_to_add.txt"
DEFAULT_REMOVE_FILE = "u1_urls_to_remove.txt"
DEFAULT_RAMDISK_DIR = r"R:\kv_chunks"  # change if no ramdisk; will be created
DEFAULT_CHUNK_SIZE = 3000
DEFAULT_DELAY = 3
DEFAULT_WRANGLER_CMD = "wrangler"
DEFAULT_NAMESPACE_ID = None  # must set via CLI or config
# -------------------------


def backup_database(db_path: str) -> str:
    """Copy DB to backups/ with timestamp. Return backup path."""
    if not os.path.exists(db_path):
        raise FileNotFoundError(f"DB not found: {db_path}")
    db_dir = os.path.dirname(os.path.abspath(db_path)) or "."
    backup_dir = os.path.join(db_dir, "backups")
    os.makedirs(backup_dir, exist_ok=True)
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    db_base = os.path.splitext(os.path.basename(db_path))[0]
    backup_name = f"{db_base}_{timestamp}.db"
    backup_path = os.path.join(backup_dir, backup_name)
    shutil.copy2(db_path, backup_path)
    print(f"[BACKUP] Database copied to: {backup_path}")
    return backup_path


def load_urls(file_path: str) -> List[str]:
    """Load urls from txt (one url per line). Return list."""
    if not os.path.exists(file_path):
        print(f"[WARN] File not found: {file_path} -> returning empty list.")
        return []
    with open(file_path, "r", encoding="utf-8") as f:
        return [line.strip() for line in f if line.strip()]


# ---------- DB operations ----------
def add_urls_to_db(db_path: str, urls: List[str], dry_run: bool = False):
    if not urls:
        print("[DB] No URLs to add.")
        return 0
    print(f"[DB] Adding {len(urls)} URLs to DB (dry_run={dry_run})")
    if dry_run:
        return len(urls)
    conn = sqlite3.connect(db_path)
    cur = conn.cursor()
    data = [(u, 0) for u in urls]
    cur.executemany("INSERT OR IGNORE INTO urls (url, cached) VALUES (?, ?)", data)
    conn.commit()
    conn.close()
    return len(urls)


def remove_urls_from_db(db_path: str, urls: List[str], dry_run: bool = False):
    if not urls:
        print("[DB] No URLs to remove.")
        return 0
    print(f"[DB] Removing {len(urls)} URLs from DB (dry_run={dry_run})")
    if dry_run:
        return len(urls)
    conn = sqlite3.connect(db_path)
    cur = conn.cursor()
    cur.executemany("DELETE FROM urls WHERE url = ?", [(u,) for u in urls])
    conn.commit()
    conn.close()
    return len(urls)


# ---------- KV chunking and upload ----------
def create_kv_chunk_files(urls: List[str], ramdisk_dir: str, prefix: str = "kv_chunk", chunk_size: int = DEFAULT_CHUNK_SIZE) -> List[str]:
    """Create JSON chunk files in ramdisk_dir. Return list of file paths."""
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
        print(f"[KV] Created chunk {part_idx}: {part_path} ({len(payload)} items)")
    return chunk_files


def upload_kv_chunks(chunk_files: List[str], namespace_id: str, wrangler_cmd: str = DEFAULT_WRANGLER_CMD, delay: int = DEFAULT_DELAY, dry_run: bool = False):
    """Upload each chunk via wrangler kv bulk put."""
    if not chunk_files:
        print("[KV] No chunk files to upload.")
        return
    if not namespace_id:
        raise ValueError("namespace_id is required to upload KV chunks.")
    total = len(chunk_files)
    for idx, chunk in enumerate(chunk_files, start=1):
        print(f"[KV] Uploading chunk {idx}/{total}: {chunk} (dry_run={dry_run})")
        if dry_run:
            continue
        cmd = f'{wrangler_cmd} kv bulk put --namespace-id={namespace_id} "{chunk}" --remote'
        try:
            subprocess.run(cmd, shell=True, check=True)
            print(f"[KV] Uploaded chunk {idx}/{total} OK")
        except subprocess.CalledProcessError as e:
            print(f"[KV][ERROR] Upload failed for {chunk}: {e}")
        time.sleep(delay)


def remove_kv_keys(urls: List[str], namespace_id: str, wrangler_cmd: str = DEFAULT_WRANGLER_CMD, batch_size: int = 100, dry_run: bool = False):
    """Remove keys individually (batched logging)."""
    if not urls:
        print("[KV] No keys to remove.")
        return
    if not namespace_id:
        raise ValueError("namespace_id is required to delete KV keys.")
    total = len(urls)
    print(f"[KV] Removing {total} keys from KV (dry_run={dry_run})")
    if dry_run:
        return
    for i, url in enumerate(urls, start=1):
        cmd = f'{wrangler_cmd} kv key delete --namespace-id={namespace_id} "{url}" --remote'
        try:
            subprocess.run(cmd, shell=True, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        except subprocess.CalledProcessError as e:
            print(f"[KV][ERROR] Failed to delete key {url}: {e}")
        if i % batch_size == 0 or i == total:
            print(f"[KV] Deleted {i}/{total} keys")


# ---------------- CLI / main ----------------
def parse_args():
    p = argparse.ArgumentParser(description="u2_update_db_and_kv_v3 - update DB and Cloudflare KV from u1 lists")
    p.add_argument("--db", default=DEFAULT_DB_PATH, help="Path to cache_urls.db")
    p.add_argument("--add-file", default=DEFAULT_ADD_FILE, help="u1_urls_to_add.txt")
    p.add_argument("--remove-file", default=DEFAULT_REMOVE_FILE, help="u1_urls_to_remove.txt")
    p.add_argument("--namespace-id", default=DEFAULT_NAMESPACE_ID, help="Cloudflare KV namespace id")
    p.add_argument("--wrangler-cmd", default=DEFAULT_WRANGLER_CMD, help="wrangler command (path or 'wrangler')")
    p.add_argument("--ramdisk-dir", default=DEFAULT_RAMDISK_DIR, help="dir to write KV chunk files (ramdisk recommended)")
    p.add_argument("--chunk-size", type=int, default=DEFAULT_CHUNK_SIZE, help="items per KV chunk")
    p.add_argument("--delay", type=int, default=DEFAULT_DELAY, help="delay seconds between chunk uploads")
    p.add_argument("--no-db", action="store_true", help="Do not modify DB (only KV)")
    p.add_argument("--no-kv", action="store_true", help="Do not modify KV (only DB)")
    p.add_argument("--dry-run", action="store_true", help="Dry run: do not perform writes or wrangler calls")
    return p.parse_args()


def main():
    args = parse_args()
    db_path = args.db
    add_file = args.add_file
    remove_file = args.remove_file
    namespace_id = args.namespace_id
    wrangler_cmd = args.wrangler_cmd
    ramdisk_dir = args.ramdisk_dir
    chunk_size = args.chunk_size
    delay = args.delay
    no_db = args.no_db
    no_kv = args.no_kv
    dry_run = args.dry_run

    print("=== u2_update_db_and_kv_v3 starting ===")
    print(f"DB: {db_path}")
    print(f"Add-file: {add_file}, Remove-file: {remove_file}")
    print(f"KV namespace: {namespace_id}, wrangler: {wrangler_cmd}")
    print(f"ramdisk-dir: {ramdisk_dir}, chunk_size: {chunk_size}, delay: {delay}")
    print(f"no-db: {no_db}, no-kv: {no_kv}, dry-run: {dry_run}")
    print("=======================================")

    # load lists
    urls_to_add = load_urls(add_file)
    urls_to_remove = load_urls(remove_file)

    # backup DB unless --no-db
    if not no_db and not dry_run:
        try:
            backup_database(db_path)
        except Exception as e:
            print(f"[ERROR] backup failed: {e}")
            return 2

    # DB updates
    if not no_db:
        added = add_urls_to_db(db_path, urls_to_add, dry_run=dry_run)
        removed = remove_urls_from_db(db_path, urls_to_remove, dry_run=dry_run)
        print(f"[DB] Done: added={added}, removed={removed}")
    else:
        print("[DB] Skipped (no-db)")

    # KV updates
    if not no_kv:
        if urls_to_add:
            print(f"[KV] Preparing to add {len(urls_to_add)} keys")
            chunk_files = create_kv_chunk_files(urls_to_add, ramdisk_dir, chunk_size=chunk_size)
            upload_kv_chunks(chunk_files, namespace_id, wrangler_cmd=wrangler_cmd, delay=delay, dry_run=dry_run)
        else:
            print("[KV] No keys to add")

        if urls_to_remove:
            print(f"[KV] Preparing to remove {len(urls_to_remove)} keys")
            remove_kv_keys(urls_to_remove, namespace_id, wrangler_cmd=wrangler_cmd, dry_run=dry_run)
        else:
            print("[KV] No keys to remove")
    else:
        print("[KV] Skipped (no-kv)")

    print("=== u2_update_db_and_kv_v3 finished ===")


if __name__ == "__main__":
    main()
