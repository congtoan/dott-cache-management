import json
import os
import subprocess
import time
from pathlib import Path

# ==================== CONFIG ====================
from paths import KV_EXPORT_FILE as INPUT_JSON, KV_CHUNK_DIR as RAMDISK_DIR  # 2026-11-25: chuyen sang data/
NAMESPACE_ID = "cc831ca12e954d0b8a875a3453abacc7"
CHUNK_SIZE = 3000
DELAY_BETWEEN_CHUNKS = 3  # giây
WRANGLER_CMD = "wrangler"  # Nếu wrangler không có trong PATH, gõ đường dẫn đầy đủ
# =================================================

def chunk_json(input_file, output_dir, chunk_size):
    os.makedirs(output_dir, exist_ok=True)
    with open(input_file, "r", encoding="utf-8") as f:
        data = json.load(f)
    chunk_files = []
    for i in range(0, len(data), chunk_size):
        chunk = data[i:i + chunk_size]
        chunk_file = os.path.join(output_dir, f"kv_chunk_{i//chunk_size + 1}.json")
        with open(chunk_file, "w", encoding="utf-8") as cf:
            json.dump(chunk, cf, ensure_ascii=False)
        chunk_files.append(chunk_file)
        print(f"[+] Created chunk: {chunk_file} ({len(chunk)} items)")
    return chunk_files

def upload_chunks(chunk_files):
    total = len(chunk_files)
    for idx, chunk_file in enumerate(chunk_files, start=1):
        print(f"\n[*] Uploading chunk {idx}/{total}: {chunk_file}")
        cmd = f'{WRANGLER_CMD} kv bulk put --namespace-id={NAMESPACE_ID} "{chunk_file}" --remote'
        try:
            result = subprocess.run(cmd, shell=True, check=True)
        except subprocess.CalledProcessError as e:
            print(f"[!] Error uploading {chunk_file}: {e}")
        print(f"[*] Sleeping {DELAY_BETWEEN_CHUNKS}s before next chunk...")
        time.sleep(DELAY_BETWEEN_CHUNKS)

def main():
    print("[*] Starting KV bulk upload tool...")
    chunk_files = chunk_json(INPUT_JSON, RAMDISK_DIR, CHUNK_SIZE)
    upload_chunks(chunk_files)
    print("[*] All chunks uploaded!")

if __name__ == "__main__":
    main()
