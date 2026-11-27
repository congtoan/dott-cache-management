#!/usr/bin/env python3
# simple_s3_cache_upload.py
import boto3
import json
import os
import glob
from concurrent.futures import ThreadPoolExecutor, as_completed
import logging
from datetime import datetime
import sys
from botocore.exceptions import ClientError
from typing import List, Dict, Any
import time
import re

# ==================== CONFIG ====================
# S3 destination
S3_ENDPOINT = "https://us-ord-1.linodeobjects.com"
ACCESS_KEY = "<REDACTED - xem secrets.json>"   # tool da bo, chi luu tham khao
SECRET_KEY = "<REDACTED - xem secrets.json>"
BUCKET = "cdn.diaocthongthai.com"
S3_PREFIX = "_cache_html"  # Thư mục đích trong S3

# Threading config
MAX_WORKERS = 20
MAX_RETRIES = 3

# Manifest directory
MANIFEST_DIR = "manifest"

# ==================== Logging ====================
def setup_logging() -> logging.Logger:
    logger = logging.getLogger('s3_cache_upload')
    logger.setLevel(logging.INFO)
    logger.handlers.clear()

    timestamp = datetime.now().strftime('%Y%m%d_%H%M%S')
    fh = logging.FileHandler(f's3_cache_upload_log_{timestamp}.txt')
    fh.setFormatter(logging.Formatter('%(asctime)s - %(levelname)s - %(message)s'))
    logger.addHandler(fh)

    ch = logging.StreamHandler()
    ch.setFormatter(logging.Formatter('%(asctime)s - %(levelname)s - %(message)s'))
    logger.addHandler(ch)

    return logger

# ==================== Progress Tracking ====================
def save_progress(completed_manifests: List[str]):
    """Lưu progress vào file JSON"""
    progress_data = {
        "completed_manifests": completed_manifests,
        "total_completed": len(completed_manifests),
        "last_updated": datetime.now().isoformat(),
        "last_manifest": completed_manifests[-1] if completed_manifests else None
    }
    
    try:
        with open("upload_progress.json", 'w', encoding='utf-8') as f:
            json.dump(progress_data, f, indent=2, ensure_ascii=False)
        print(f"Progress saved: {len(completed_manifests)} manifests completed")
    except Exception as e:
        logger.error(f"Error saving progress: {str(e)}")

def load_progress() -> List[str]:
    """Load progress từ file JSON"""
    if not os.path.exists("upload_progress.json"):
        print("No previous progress found, starting fresh")
        return []
    
    try:
        with open("upload_progress.json", 'r', encoding='utf-8') as f:
            progress_data = json.load(f)
        
        completed = progress_data.get("completed_manifests", [])
        print(f"Resuming: {len(completed)} manifests already completed")
        if completed:
            print(f"Last completed: {os.path.basename(completed[-1])}")
        
        return completed
    except Exception as e:
        logger.error(f"Error loading progress: {str(e)}, starting fresh")
        return []

def clear_progress():
    """Xóa file progress khi hoàn thành"""
    try:
        if os.path.exists("upload_progress.json"):
            os.remove("upload_progress.json")
            print("Progress file cleared")
    except Exception as e:
        logger.error(f"Error clearing progress file: {str(e)}")

# ==================== Helper Functions ====================
def is_supported_file(file_path: str) -> bool:
    """Kiểm tra xem file có được hỗ trợ sync không (chỉ .html và .html_gzip)"""
    return file_path.endswith('.html') or file_path.endswith('.html_gzip')

def get_content_type(file_path: str) -> str:
    """Xác định content type cho file .html và .html_gzip"""
    if file_path.endswith('.html_gzip'):
        return "text/html; charset=utf-8"
    elif file_path.endswith('.html'):
        return "text/html"
    else:
        return "application/octet-stream"  # Fallback (không nên xảy ra)

def get_s3_key(relative_path: str) -> str:
    """Tạo S3 key từ relative path"""
    # Chuyển đổi path separator cho Windows
    relative_path = relative_path.replace("\\", "/")
    
    # Tạo key với prefix
    s3_key = f"{S3_PREFIX}/{relative_path}"
    
    # Normalize path (loại bỏ double slashes)
    s3_key = re.sub(r'/+', '/', s3_key)
    
    return s3_key

def upload_single_file(file_info: Dict[str, Any], s3_client) -> bool:
    """Upload một file lên S3"""
    file_path = file_info['file_path']
    relative_path = file_info['relative_path']
    s3_key = get_s3_key(relative_path)
    
    # Kiểm tra file có tồn tại không
    if not os.path.exists(file_path):
        logger.error(f"File not found: {file_path}")
        return False
    
    # Kiểm tra file có được hỗ trợ không
    if not is_supported_file(file_path):
        logger.info(f"SKIPPED (unsupported format): {relative_path}")
        return True  # Coi như thành công để không đánh dấu failed
    
    # Xác định xem có phải .html_gzip không
    is_html_gzip = file_path.endswith('.html_gzip')
    
    for attempt in range(MAX_RETRIES):
        try:
            print(f"Uploading: {relative_path} (attempt {attempt+1}/{MAX_RETRIES})")
            
            # Tính ETag local (MD5)
            import hashlib
            with open(file_path, 'rb') as f:
                local_md5 = hashlib.md5(f.read()).hexdigest()
            
            # Kiểm tra file đã tồn tại chưa
            try:
                s3_object = s3_client.head_object(Bucket=BUCKET, Key=s3_key)
                s3_etag = s3_object.get('ETag', '').strip('"')
                
                if s3_etag == local_md5:
                    print(f"File {s3_key} already exists with same ETag, skipping")
                    return True
            except ClientError as e:
                if e.response['Error']['Code'] != '404':
                    raise
            
            # Đọc file content
            with open(file_path, 'rb') as f:
                file_content = f.read()
            
            # Chuẩn bị upload parameters
            upload_params = {
                'Bucket': BUCKET,
                'Key': s3_key,
                'Body': file_content,
                'ContentType': get_content_type(file_path),
                'CacheControl': 'public, max-age=31536000',
                'ACL': 'public-read'
            }
            
            # Đặc biệt xử lý .html_gzip - thêm Content-Encoding
            if is_html_gzip:
                upload_params['ContentEncoding'] = 'gzip'
            
            # Upload
            s3_client.put_object(**upload_params)
            print(f"✓ Successfully uploaded: {relative_path}")
            return True
            
        except Exception as e:
            if attempt < MAX_RETRIES - 1:
                print(f"Attempt {attempt+1} failed for {s3_key}: {str(e)}")
                time.sleep(2 ** attempt)  # Exponential backoff
                continue
            logger.error(f"Error uploading file {s3_key}: {str(e)}")
            return False

# ==================== Manifest Processing ====================
def get_manifest_files() -> List[str]:
    """Lấy danh sách manifest files và sort theo số thứ tự"""
    pattern = os.path.join(MANIFEST_DIR, "file_manifest_*.json")
    manifest_files = glob.glob(pattern)
    
    if not manifest_files:
        return []
    
    # Sort theo số thứ tự trong tên file
    manifest_files.sort(key=lambda x: int(re.search(r'(\d+)', os.path.basename(x)).group(1)))
    return manifest_files

def load_manifest_file(manifest_path: str) -> Dict[str, Any]:
    """Load một manifest file"""
    try:
        with open(manifest_path, 'r', encoding='utf-8') as f:
            return json.load(f)
    except Exception as e:
        logger.error(f"Error loading manifest {manifest_path}: {str(e)}")
        return {}

def process_manifest_files(files: List[Dict[str, Any]], s3_client) -> Dict[str, int]:
    """Xử lý danh sách files với multi-threading"""
    # Lọc ra chỉ các file được hỗ trợ và log các file bị skip
    supported_files = []
    skipped_files = []
    
    for file_info in files:
        file_path = file_info['file_path']
        if is_supported_file(file_path):
            supported_files.append(file_info)
        else:
            skipped_files.append(file_info['relative_path'])
    
    # Log các file bị skip
    if skipped_files:
        for skipped in skipped_files:
            logger.info(f"SKIPPED (unsupported format): {skipped}")
    
    print(f"Processing {len(supported_files)} supported files (.html/.html_gzip), skipped {len(skipped_files)} unsupported files")
    
    if not supported_files:
        return {"successful": 0, "failed": 0}
    
    successful, failed = 0, 0
    
    with ThreadPoolExecutor(max_workers=MAX_WORKERS) as executor:
        # Submit all tasks
        future_to_file = {
            executor.submit(upload_single_file, file_info, s3_client): file_info['relative_path'] 
            for file_info in supported_files
        }
        
        # Process completed tasks
        for future in as_completed(future_to_file):
            try:
                if future.result():
                    successful += 1
                else:
                    failed += 1
            except Exception as e:
                logger.error(f"Error processing {future_to_file[future]}: {e}")
                failed += 1
    
    return {"successful": successful, "failed": failed}

def upload_from_manifests(s3_client):
    """Upload files từ manifests với resume capability"""
    # Lấy danh sách manifest files
    manifest_files = get_manifest_files()
    total_manifests = len(manifest_files)
    
    if not manifest_files:
        logger.error(f"No manifest files found in {MANIFEST_DIR}")
        return
    
    print(f"Found {total_manifests} manifest files to process")
    
    # Load progress
    completed_manifests = load_progress()
    
    # Lọc ra những manifest chưa xử lý
    remaining_manifests = [mf for mf in manifest_files if mf not in completed_manifests]
    
    if not remaining_manifests:
        print("All manifests have been processed! Upload is already complete.")
        clear_progress()
        return
    
    print(f"Resume status: {len(completed_manifests)}/{total_manifests} completed, {len(remaining_manifests)} remaining")
    
    # Stats tracking
    total_stats = {"successful": 0, "failed": 0, "skipped": 0}
    
    # Xử lý từng manifest file
    for i, manifest_file in enumerate(remaining_manifests, 1):
        print(f"\n📁 Processing manifest {len(completed_manifests) + i}/{total_manifests}: {os.path.basename(manifest_file)}")
        
        try:
            # Load manifest data
            data = load_manifest_file(manifest_file)
            files = data.get("files", [])
            
            if not files:
                print(f"No files found in manifest {manifest_file}")
                completed_manifests.append(manifest_file)
                save_progress(completed_manifests)
                continue
            
            # Count skipped files for this manifest
            skipped_count = len([f for f in files if not is_supported_file(f['file_path'])])
            total_stats["skipped"] += skipped_count
            
            # Process files in this manifest
            stats = process_manifest_files(files, s3_client)
            total_stats["successful"] += stats["successful"]
            total_stats["failed"] += stats["failed"]
            
            # Cập nhật progress
            completed_manifests.append(manifest_file)
            save_progress(completed_manifests)
            
            print(f"✅ Completed manifest {len(completed_manifests)}/{total_manifests}: "
                  f"Success={stats['successful']}, Failed={stats['failed']}, Skipped={skipped_count}")
            
        except Exception as e:
            logger.error(f"Error processing manifest {manifest_file}: {str(e)}")
            continue
    
    # Hoàn thành
    print(f"\n🎉 UPLOAD COMPLETED!")
    print(f"📊 Final Stats: Success={total_stats['successful']}, Failed={total_stats['failed']}, Skipped={total_stats['skipped']}")
    print(f"📝 Check log file for details of skipped files")
    clear_progress()

# ==================== Main ====================
def main():
    global logger
    logger = setup_logging()
    
    # Kiểm tra manifest directory
    if not os.path.exists(MANIFEST_DIR):
        logger.error(f"Manifest directory '{MANIFEST_DIR}' not found!")
        sys.exit(1)
    
    # Kiểm tra manifest files
    manifest_files = get_manifest_files()
    if not manifest_files:
        logger.error(f"No manifest files found in {MANIFEST_DIR}")
        sys.exit(1)
    
    print(f"📋 Found {len(manifest_files)} manifest files in {MANIFEST_DIR}")
    
    # Khởi tạo S3 client
    try:
        s3_client = boto3.client(
            's3',
            endpoint_url=S3_ENDPOINT,
            aws_access_key_id=ACCESS_KEY,
            aws_secret_access_key=SECRET_KEY
        )
        
        # Test connectivity
        s3_client.head_bucket(Bucket=BUCKET)
        print(f"✓ Connected to S3: {BUCKET}")
        
    except Exception as e:
        logger.error(f"Error connecting to S3: {str(e)}")
        sys.exit(1)
    
    # Thông báo về file types được hỗ trợ
    print(f"🎯 Only .html and .html_gzip files will be synced")
    print(f"📝 Other file types will be skipped and logged")
    
    # Confirm before start
    print(f"\n🎯 Upload destination: {S3_ENDPOINT}/{BUCKET}/{S3_PREFIX}/")
    confirm = input("Start upload? (y/N): ").strip().lower()
    if confirm != 'y':
        print("Upload cancelled.")
        sys.exit(0)
    
    # Start upload
    print("\n🚀 Starting S3 upload with resume capability")
    upload_from_manifests(s3_client)

if __name__ == "__main__":
    main()