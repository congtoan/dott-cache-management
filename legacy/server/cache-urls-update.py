#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Tool: Cache URLs Update
=======================

Công dụng:
    - Đọc danh sách URL từ file cache-urls-update.txt (mỗi dòng 1 URL).
    - Với từng URL: xoá cache local cũ → trigger cache mới → upload lên S3.

Cách dùng:
    1. Điền các URL vào file cache-urls-update.txt (mỗi dòng 1 URL).
    2. Chạy: python cache-urls-update.py
"""

import os
import time

from utils import (
    trigger_cache,
    url_to_cache_paths,
    upload_files_to_s3,
    check_cpu_usage_and_wait,
    read_urls_from_file,
)

URL_FILE = "cache-urls-update.txt"


def remove_local_cache(url: str):
    """Xoá toàn bộ file cache local tương ứng với URL."""
    for fpath in url_to_cache_paths(url):
        if os.path.exists(fpath):
            os.remove(fpath)
            print(f"  Deleted old cache: {fpath}")


def process_url(url: str, index: int, total: int):
    print(f"\n[{index}/{total}] {url}")
    remove_local_cache(url)
    check_cpu_usage_and_wait(threshold=70)
    trigger_cache(url)
    upload_files_to_s3(url_to_cache_paths(url))
    print(f"  Done.")


def main():
    if not os.path.exists(URL_FILE):
        print(f"File not found: {URL_FILE}")
        return

    urls = read_urls_from_file(URL_FILE)
    if not urls:
        print("No URLs found in file.")
        return

    print(f"Found {len(urls)} URL(s) to update.\n")

    for i, url in enumerate(urls, start=1):
        process_url(url, i, len(urls))
        if i < len(urls):
            time.sleep(2)

    print(f"\nAll done. {len(urls)} URL(s) processed.")


if __name__ == "__main__":
    main()
