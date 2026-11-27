# cache_kv_export.py
import os
import json

MANIFEST_DIR = "manifest"
OUTPUT_FILE = "cache_kv_urls_bulk.json"

def main():
    manifest_files = sorted([f for f in os.listdir(MANIFEST_DIR) if f.startswith("file_manifest_")])
    url_paths = set()

    for mf in manifest_files:
        with open(os.path.join(MANIFEST_DIR, mf), "r", encoding="utf-8") as f:
            data = json.load(f)
            for file in data["files"]:
                rel_path = file["relative_path"]
                url_path = "/" + os.path.dirname(rel_path) + "/"
                url_path = url_path.replace("\\", "/")
                url_paths.add(url_path)

    data_out = [{"key": url, "value": "1"} for url in sorted(url_paths)]

    with open(OUTPUT_FILE, "w", encoding="utf-8") as f:
        json.dump(data_out, f, indent=2, ensure_ascii=False)

    print(f"Exported {len(url_paths)} url paths to {OUTPUT_FILE} (JSON array format, ready for wrangler kv bulk put)")

if __name__ == "__main__":
    main()