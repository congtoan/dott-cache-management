"""
paths.py - Duong dan dung chung cho cac tool chay tren PC.
Moi tool PC import tu day de doc/ghi vao thu muc data/ (khong phu thuoc thu muc dang dung).
"""
import os
import glob

ROOT_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))  # D:\Claude\2026-11-25 Cache Management
DATA_DIR = os.path.join(ROOT_DIR, "data")

DB_PATH = os.path.join(DATA_DIR, "cache_urls.db")          # ban sao DB (tai tu VPS truoc khi dung)
JSON_DIR = os.path.join(DATA_DIR, "wp_url_jsons")          # output cua wp_cli_extract_all_urls.py, input cua u1
MANIFEST_ROOT = os.path.join(DATA_DIR, "manifests")        # output cua m0, input cua m2
BACKUP_DIR = os.path.join(DATA_DIR, "backups")             # u2 backup DB vao day

U1_ADD_FILE = os.path.join(DATA_DIR, "u1_urls_to_add.txt")
U1_REMOVE_FILE = os.path.join(DATA_DIR, "u1_urls_to_remove.txt")
MISSING_S3_FILE = os.path.join(DATA_DIR, "missing_s3.txt")
KV_EXPORT_FILE = os.path.join(DATA_DIR, "kv_export.json")

# Thu muc tam cho chunk KV (RAM disk R:\ neu co, khong thi dung data/kv_chunks)
KV_CHUNK_DIR = r"R:\kv_chunks" if os.path.isdir("R:\\") else os.path.join(DATA_DIR, "kv_chunks")

# Duong dan tren VPS
VPS_CACHE_TOOLS = "/root/tool/diaocthongthai.com/cache-tools"
VPS_DB_PATH = VPS_CACHE_TOOLS + "/output/cache_urls.db"


SECRETS_FILE = os.path.join(ROOT_DIR, "secrets.json")  # gitignored, mau: secrets.example.json


def load_secrets():
    """Doc secrets.json o thu muc goc project."""
    import json
    if not os.path.exists(SECRETS_FILE):
        raise FileNotFoundError(f"Thieu {SECRETS_FILE} (copy tu secrets.example.json)")
    with open(SECRETS_FILE, encoding="utf-8") as f:
        return json.load(f)


def latest_manifest_dir():
    """Thu muc manifest-s3-cache-* moi nhat trong data/manifests (None neu chua co)."""
    dirs = sorted(glob.glob(os.path.join(MANIFEST_ROOT, "manifest-s3-cache-*")))
    return dirs[-1] if dirs else None
