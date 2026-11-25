# cache_manifest_generator.py
import os
import json
import shutil
from datetime import datetime
import time

# Config
CACHE_DIR = "/home/diaocthDP4f/diaocthongthai.com/public_html/wp-content/cache/wp-rocket/diaocthongthai.com/"
MANIFEST_DIR = "manifest"
MAX_ITEMS_PER_MANIFEST = 2000

# Clear manifest dir before use
if os.path.exists(MANIFEST_DIR):
    shutil.rmtree(MANIFEST_DIR)
os.makedirs(MANIFEST_DIR, exist_ok=True)

def save_manifest(batch, index):
    filename = f"file_manifest_{index:04d}.json"
    path = os.path.join(MANIFEST_DIR, filename)
    data = {
        "generated_at": datetime.now().isoformat(),
        "file_number": index,
        "total_files": len(batch),
        "files": batch
    }
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, indent=2, ensure_ascii=False)
    print(f"Saved {path} ({len(batch)} files)")

def main():
    manifest_count = 0
    batch = []
    total = 0
    skipped = []

    for root, _, files in os.walk(CACHE_DIR):
        # Skip any directory containing "search"
        if "/search/" in root or root.endswith("/search"):
            continue

        for f in files:
            if f.endswith(".html") or f.endswith(".html_gzip"):
                file_path = os.path.join(root, f)
                rel_path = os.path.relpath(file_path, CACHE_DIR)

                # Skip any path containing "." or "//"
                url_path = "/" + os.path.dirname(rel_path) + "/"
                url_path = url_path.replace("\\", "/")
                if "." in url_path or "//" in url_path:
                    skipped.append(rel_path)
                    continue

                batch.append({
                    "file_path": file_path,
                    "relative_path": rel_path,
                    "mtime": datetime.fromtimestamp(os.path.getmtime(file_path)).isoformat()
                })
                total += 1

                if len(batch) >= MAX_ITEMS_PER_MANIFEST:
                    manifest_count += 1
                    save_manifest(batch, manifest_count)
                    batch = []
                    time.sleep(0.05)  # nghỉ 50ms mỗi batch

    if batch:
        manifest_count += 1
        save_manifest(batch, manifest_count)

    summary = {
        "generated_at": datetime.now().isoformat(),
        "total_files": total,
        "total_manifests": manifest_count
    }
    with open(os.path.join(MANIFEST_DIR, "manifest_summary.json"), "w", encoding="utf-8") as f:
        json.dump(summary, f, indent=2, ensure_ascii=False)
    print("Done. Summary saved.")

    if skipped:
        print(f"Skipped {len(skipped)} paths (contained '.' or '//')")    

if __name__ == "__main__":
    main()
