#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Tool: Cache Segment Update
==========================

Công dụng:
    - Update cache cho archive và/hoặc các bài viết bên trong archive.
    - Cho phép chọn chế độ:
        1. Update Archive only
        2. Update Archive + Posts
        3. Update Posts only

Khác biệt so với bản cũ:
    - Tái sử dụng utils: trigger_cache, url_to_cache_paths, upload_files_to_s3, check_cpu_usage_and_wait.
    - Trigger cache bằng curl hướng về 127.0.0.1 (bypass Cloudflare).
    - Sau khi trigger xong → upload file cache lên S3 buckets.
"""

import re
import time
from bs4 import BeautifulSoup as bs
import requests
import os

from utils import (
    trigger_cache,
    url_to_cache_paths,
    upload_files_to_s3,
    check_cpu_usage_and_wait,
)

HEADERS = {
    "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "
                  "AppleWebKit/537.36 (KHTML, like Gecko) "
                  "Chrome/91.0.4472.124 Safari/537.36"
}

def remove_local_cache(url: str):
    """Xoá toàn bộ file cache local tương ứng với URL."""
    for fpath in url_to_cache_paths(url):
        if os.path.exists(fpath):
            os.remove(fpath)
            print(f"Deleted old local cache: {fpath}")

def get_update_option():
    while True:
        update_option = input(
            """Select one of the below update cache options:
    1. Update Archive only                      
    2. Update both Archive and its Posts, too.
    3. Update Posts only.

My selection: """
        )
        if update_option in ("1", "2", "3"):
            return update_option
        else:
            print("Wrong choice. Please input again")

def extract_articles_from_page(url):
    """Lấy danh sách link bài viết từ 1 archive page."""
    try:
        page = requests.get(url, headers=HEADERS, timeout=30)
        soup = bs(page.content, "lxml")
        articles = soup.find(id="main").find_all("article")
        return [a.find("a")["href"] for a in articles if a.find("a")]
    except Exception as e:
        print(f"Error extracting articles from {url}: {e}")
        return []

def find_last_page_number(archive_url):
    """Tìm số page cuối cùng của archive (sau khi trigger cache)."""
    html = trigger_cache(archive_url, return_content=True)
    if not html:
        return 1
    soup = bs(html, "lxml")
    nav_below = soup.find(id="nav-below")
    if not nav_below:
        return 1
    next_item = nav_below.find("a", class_="next")
    if next_item:
        last_nav = next_item.find_previous_sibling()["href"]
        match = re.search(r"/page/(\d+)/$", last_nav)
        if match:
            return int(match.group(1))
    return 1

def main():
    my_archive_link_list = input(
        "Please input archive/category link \nSplit by comma for many link \n "
    )
    selected_option = get_update_option()

    for archive_link in my_archive_link_list.split(","):
        archive_link = archive_link.strip()
        if not archive_link.endswith("/"):
            archive_link += "/"

        # Xác định số page cuối của archive (fresh HTML sau khi trigger)
        page_number = find_last_page_number(archive_link)
        archive_pages = [
            archive_link.rstrip("/") + f"/page/{p}/"
            for p in range(1, page_number + 1)
        ]

        for archive_page in archive_pages:
            print(f"Processing archive: {archive_page}")

            if selected_option in ("1", "2"):  # update archive
                remove_local_cache(archive_page)             # ⬅️ xoá cache cũ
                trigger_cache(archive_page)
                upload_files_to_s3(url_to_cache_paths(archive_page))
                print(f"Updated + uploaded archive cache: {archive_page}")

            if selected_option in ("2", "3"):  # update posts
                post_links = extract_articles_from_page(archive_page)
                for post_url in post_links:
                    remove_local_cache(post_url)             # ⬅️ xoá cache cũ                     
                    check_cpu_usage_and_wait(threshold=70)
                    trigger_cache(post_url)
                    upload_files_to_s3(url_to_cache_paths(post_url))
                    print(f"Updated + uploaded post cache: {post_url}")
                    time.sleep(2)


if __name__ == "__main__":
    main()
