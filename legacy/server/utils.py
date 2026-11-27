#!/usr/bin/env python3
"""
utils.py - các hàm dùng chung
"""
import os
import re
import time
import psutil
import subprocess
from urllib.parse import urlparse
import boto3
from botocore.exceptions import ClientError
from concurrent.futures import ThreadPoolExecutor, as_completed

# Fixed config
DOMAIN = "diaocthongthai.com"
USER = "diaocthDP4f"
TARGET_DIR = f"/home/{USER}/{DOMAIN}/public_html/wp-content/cache/wp-rocket/{DOMAIN}/"

def _load_secrets():
    """Doc secrets.json: cung thu muc utils.py (VPS) hoac thu muc cha (repo). Khong commit file nay."""
    import json
    here = os.path.dirname(os.path.abspath(__file__))
    for p in (os.path.join(here, "secrets.json"), os.path.join(os.path.dirname(here), "secrets.json")):
        if os.path.exists(p):
            with open(p, encoding="utf-8") as f:
                return json.load(f)
    raise FileNotFoundError("Khong tim thay secrets.json (xem secrets.example.json)")

_S3 = _load_secrets()["s3"]

# Cấu hình bucket S3
BUCKET_CONFIGS = {
    "thongthai_chicago": {
        "S3_ENDPOINT": "https://us-ord-1.linodeobjects.com",
        "ACCESS_KEY": _S3["access_key"],
        "SECRET_KEY": _S3["secret_key"],
        "BUCKET_DOMAIN": "cdn.diaocthongthai.com"
    },
    "thongthai_singapore": {
        "S3_ENDPOINT": "https://ap-south-1.linodeobjects.com",
        "ACCESS_KEY": _S3["access_key"],
        "SECRET_KEY": _S3["secret_key"],
        "BUCKET_DOMAIN": "cdn.diaocthongthai.com"
    }
}

# Prefix trên S3
S3_PREFIX = "_cache_html"

PAGE_RE = re.compile(r"/page/(\d+)/?$")

def compute_page_number(url: str) -> int:
    m = PAGE_RE.search(urlparse(url).path)
    return int(m.group(1)) if m else 1

def url_to_cache_paths(url: str):
    """Trả về list các đường dẫn file cache."""
    parsed = urlparse(url)
    rel_path = parsed.path.lstrip("/")
    base = os.path.join(TARGET_DIR, rel_path) if rel_path else TARGET_DIR
    desktop = os.path.join(base, "index-https.html")
    mobile = os.path.join(base, "index-mobile-https.html")
    return [desktop, desktop + "_gzip", mobile, mobile + "_gzip"]

def is_cache_file_exists(cache_file_html: str) -> bool:
    """Kiểm tra đủ cả 4 file cache (desktop + mobile, html + gzip)."""
    variants = [
        cache_file_html,
        cache_file_html.replace("index-https.html", "index-https.html_gzip"),
        cache_file_html.replace("index-https.html", "index-mobile-https.html"),
        cache_file_html.replace("index-https.html", "index-mobile-https.html_gzip"),
    ]
    return all(os.path.exists(f) for f in variants)

def check_cpu_usage_and_wait(threshold=70, interval=10):
    """Nếu CPU load > threshold thì chờ cho đến khi xuống thấp hơn."""
    while True:
        cpu_percent = psutil.cpu_percent(interval=1)
        if cpu_percent <= threshold:
            break
        print(f"CPU usage {cpu_percent}%, waiting {interval}s...")
        time.sleep(interval)

def trigger_cache(url: str, return_content=False):
    path = urlparse(url).path
    if not path.endswith("/"):
        path += "/"

    # Desktop: lấy cả stdout nếu cần
    cmd1 = f'curl -H "Host: {DOMAIN}" https://127.0.0.1{path} -k -s'
    desktop_res = subprocess.run(cmd1, shell=True, capture_output=True, text=True)

    # Mobile: chỉ trigger, không cần stdout
    ua = '"Mozilla/5.0 (iPhone; CPU iPhone OS 10_3 like Mac OS X) AppleWebKit/602.1 Safari/602.1"'
    cmd2 = f'curl -H "Host: {DOMAIN}" -A {ua} https://127.0.0.1{path} -k -s -o /dev/null'
    subprocess.call(cmd2, shell=True)

    if return_content:
        if desktop_res.returncode == 0:
            return desktop_res.stdout
        else:
            print(f"Error triggering cache for {url}: {desktop_res.stderr}")
            return None
    return None

def _get_s3_client_from_bucket_name(bucket_name: str):
    if bucket_name not in BUCKET_CONFIGS:
        raise ValueError(f"Unknown bucket_name: {bucket_name}")
    cfg = BUCKET_CONFIGS[bucket_name]
    return boto3.client(
        "s3",
        endpoint_url=cfg["S3_ENDPOINT"],
        aws_access_key_id=cfg["ACCESS_KEY"],
        aws_secret_access_key=cfg["SECRET_KEY"]
    ), cfg["BUCKET_DOMAIN"]


def _upload_to_single_bucket(file_paths, bucket_name, prefix=S3_PREFIX, dry_run=False):
    """Upload file_paths lên 1 bucket cụ thể"""
    s3_client, bucket_domain = _get_s3_client_from_bucket_name(bucket_name)
    results = []

    for local_path in file_paths:
        entry = {"path": local_path, "s3_key": None, "bucket": bucket_name, "ok": False, "error": None}
        try:
            if not os.path.exists(local_path):
                entry["error"] = "local file not found"
                print(f"[{bucket_name}] File not found: {local_path}")
                results.append(entry)
                continue

            rel = os.path.relpath(local_path, start=TARGET_DIR).replace("\\", "/")
            if rel.startswith(".."):
                rel = os.path.basename(local_path)
            if not rel.startswith(f"{prefix}/"):
                rel = f"{prefix}/{rel}"
            s3_key = rel
            entry["s3_key"] = s3_key

            if dry_run:
                print(f"[{bucket_name}] [dry-run] -> s3://{bucket_domain}/{s3_key}")
                entry["ok"] = True
                results.append(entry)
                continue

            with open(local_path, "rb") as fh:
                file_bytes = fh.read()

            params = {
                "Bucket": bucket_domain,
                "Key": s3_key,
                "Body": file_bytes,
                "ACL": "public-read",
                "CacheControl": "public, max-age=31536000",
                "ContentType": "text/html",
            }
            if local_path.endswith(".html_gzip") or local_path.endswith(".gz"):
                params["ContentEncoding"] = "gzip"

            s3_client.put_object(**params)
            print(f"[{bucket_name}] Uploaded: s3://{bucket_domain}/{s3_key}")
            entry["ok"] = True

        except ClientError as e:
            entry["error"] = str(e)
            print(f"[{bucket_name}] Upload failed {local_path} -> {s3_key}: {e}")
        except Exception as e:
            entry["error"] = str(e)
            print(f"[{bucket_name}] Upload failed {local_path} -> {s3_key}: {e}")

        results.append(entry)

    return results


def upload_files_to_s3(file_paths, prefix=S3_PREFIX, dry_run=False):
    """Upload file_paths lên tất cả bucket trong BUCKET_CONFIGS"""
    all_results = []
    # chỉnh sửa list bucket_name cần upload tại đây
    for bucket_name in ["thongthai_chicago"]:
        res = _upload_to_single_bucket(file_paths, bucket_name, prefix=prefix, dry_run=dry_run)
        all_results.extend(res)
    return all_results


def write_urls_to_file(urls, filepath):
    with open(filepath, "w", encoding="utf-8") as f:
        for url in urls:
            f.write(url.strip() + "\n")

def read_urls_from_file(filepath):
    with open(filepath, "r", encoding="utf-8") as f:
        return [line.strip() for line in f if line.strip()]

def are_cache_files_on_s3(cache_file_html, bucket_name="thongthai_chicago", prefix=S3_PREFIX):
    """Kiểm tra cả desktop (index-https.html) và mobile (index-mobile-https.html) đã có trên S3 chưa"""
    s3_client, bucket_domain = _get_s3_client_from_bucket_name(bucket_name)

    base_variants = [
        cache_file_html,
        cache_file_html.replace("index-https.html", "index-mobile-https.html"),
    ]

    for local_html_file in base_variants:
        rel = os.path.relpath(local_html_file, start=TARGET_DIR).replace("\\", "/")
        if rel.startswith(".."):
            rel = os.path.basename(local_html_file)
        if not rel.startswith(f"{prefix}/"):
            rel = f"{prefix}/{rel}"
        s3_key = rel

        try:
            s3_client.head_object(Bucket=bucket_domain, Key=s3_key)
        except ClientError as e:
            if e.response["Error"]["Code"] == "404":
                return False
            raise
    return True
