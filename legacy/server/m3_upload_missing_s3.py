#!/usr/bin/env python3
from concurrent.futures import ThreadPoolExecutor, as_completed
from utils import url_to_cache_paths, upload_files_to_s3, read_urls_from_file

def upload_url(url):
    files = url_to_cache_paths(url)
    upload_files_to_s3(files)
    return url

def main(missing_s3_file="missing_s3.txt", missing_disk_file="missing_disk.txt", workers=10):
    missing_s3 = set(read_urls_from_file(missing_s3_file))
    missing_disk = set(read_urls_from_file(missing_disk_file))
    upload_candidates = list(missing_s3 - missing_disk)

    print(f"Need to upload {len(upload_candidates)} urls to S3")

    with ThreadPoolExecutor(max_workers=workers) as ex:
        futures = {ex.submit(upload_url, url): url for url in upload_candidates}
        for fut in as_completed(futures):
            url = fut.result()
            print(f"Uploaded: {url}")

if __name__ == "__main__":
    main()
