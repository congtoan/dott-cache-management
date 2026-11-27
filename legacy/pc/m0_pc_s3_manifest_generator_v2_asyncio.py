"""
S3 File Manifest Generator v2 (High Performance Async Version)
- Sử dụng asyncio + aiobotocore cho tốc độ tối đa
- Song song hàng trăm request bất đồng bộ
- Tối ưu cho máy cấu hình mạnh với RAM 64GB
- Batch processing thông minh
"""

import asyncio
import aiobotocore.session
import json
import os
from datetime import datetime
import logging
import sys
from typing import Dict, Any, List, Set
from botocore.exceptions import ClientError
import re
from concurrent.futures import ThreadPoolExecutor
import aiofiles
from collections import deque
import time

# Performance Constants - Tối ưu cho máy mạnh
MAX_ITEMS_PER_MANIFEST = 2000
MAX_CONCURRENT_REQUESTS = 200  # Số request đồng thời
BATCH_SIZE = 1000  # Kích thước batch xử lý
PREFETCH_SIZE = 10000  # Số items prefetch
MAX_RETRIES = 3
RETRY_DELAY = 0.5

# Patterns to exclude
EXCLUDED_PATTERNS = [
    ".DS_Store", "Thumbs.db", ".vscode", ".env", ".gitignore", "_gsdata_"
]

class AsyncS3ManifestGenerator:
    def __init__(self, endpoint_url: str, access_key: str, secret_key: str):
        self.session = aiobotocore.session.get_session()
        self.config = {
            'region_name': 'us-east-1',
            'endpoint_url': endpoint_url,
            'aws_access_key_id': access_key,
            'aws_secret_access_key': secret_key
        }
        self.semaphore = asyncio.Semaphore(MAX_CONCURRENT_REQUESTS)
        self.logger = self.setup_logging()
        self.stats = {
            'total_files': 0,
            'total_directories': 0,
            'start_time': time.time(),
            'requests_made': 0,
            'errors': 0
        }
        
    def setup_logging(self) -> logging.Logger:
        logger = logging.getLogger('s3_manifest_v2')
        logger.setLevel(logging.INFO)
        logger.handlers.clear()

        timestamp = datetime.now().strftime('%Y%m%d_%H%M%S')
        
        # File handler with UTF-8 encoding
        fh = logging.FileHandler(f's3_manifest_v2_log_{timestamp}.txt', encoding='utf-8')
        fh.setFormatter(logging.Formatter('%(asctime)s - %(levelname)s - %(message)s'))
        logger.addHandler(fh)

        # Console handler - simple text for Windows compatibility
        ch = logging.StreamHandler()
        ch.setLevel(logging.INFO)
        
        # Custom formatter to remove emojis for console on Windows
        import platform
        if platform.system() == 'Windows':
            console_formatter = logging.Formatter('%(asctime)s - %(levelname)s - %(message)s')
        else:
            console_formatter = logging.Formatter('%(asctime)s - %(levelname)s - %(message)s')
        
        ch.setFormatter(console_formatter)
        logger.addHandler(ch)

        return logger

    @staticmethod
    def should_skip_path(path: str) -> bool:
        return any(pattern in path for pattern in EXCLUDED_PATTERNS)

    @staticmethod
    def get_file_extension(file_key: str) -> str:
        return file_key.split('.')[-1].lower() if '.' in file_key else ''

    @staticmethod
    def get_content_type(file_key: str) -> str:
        mime_types = {
            'html': 'text/html', 'htm': 'text/html',
            'js': 'application/javascript', 'mjs': 'application/javascript',
            'css': 'text/css',
            'png': 'image/png', 'jpg': 'image/jpeg', 'jpeg': 'image/jpeg',
            'gif': 'image/gif', 'webp': 'image/webp', 'svg': 'image/svg+xml', 'ico': 'image/x-icon',
            'woff': 'font/woff', 'woff2': 'font/woff2', 'ttf': 'font/ttf', 'otf': 'font/otf',
            'pdf': 'application/pdf', 'json': 'application/json', 'xml': 'application/xml',
            'txt': 'text/plain',
            'zip': 'application/zip', 'gz': 'application/gzip',
        }
        ext = AsyncS3ManifestGenerator.get_file_extension(file_key)
        return mime_types.get(ext, 'application/octet-stream')

    async def save_manifest_file_async(self, manifest_data: List[Dict[str, Any]], 
                                     manifest_dir: str, file_number: int) -> str:
        """Async file writing để không block main thread"""
        filename = f"file_manifest_{file_number:04d}.json"
        filepath = os.path.join(manifest_dir, filename)

        manifest_info = {
            "generated_at": datetime.now().isoformat(),
            "file_number": file_number,
            "total_files": len(manifest_data),
            "files": manifest_data
        }

        async with aiofiles.open(filepath, 'w', encoding='utf-8') as f:
            await f.write(json.dumps(manifest_info, indent=2, ensure_ascii=False))

        self.logger.info(f"Saved manifest file: {filename} with {len(manifest_data)} files")
        return filepath

    async def list_objects_with_retry(self, s3_client, bucket: str, prefix: str, 
                                    continuation_token: str = None) -> Dict:
        """List objects với retry mechanism"""
        for attempt in range(MAX_RETRIES):
            try:
                async with self.semaphore:
                    params = {
                        'Bucket': bucket,
                        'Prefix': prefix,
                        'Delimiter': '/',
                        'MaxKeys': 1000  # AWS S3 max
                    }
                    if continuation_token:
                        params['ContinuationToken'] = continuation_token
                    
                    self.stats['requests_made'] += 1
                    response = await s3_client.list_objects_v2(**params)
                    return response
                    
            except Exception as e:
                self.stats['errors'] += 1
                if attempt < MAX_RETRIES - 1:
                    self.logger.warning(f"Request failed (attempt {attempt + 1}), retrying: {e}")
                    await asyncio.sleep(RETRY_DELAY * (2 ** attempt))  # Exponential backoff
                else:
                    self.logger.error(f"Request failed after {MAX_RETRIES} attempts: {e}")
                    raise

    async def process_directory_async(self, s3_client, bucket: str, prefix: str) -> List[Dict[str, Any]]:
        """Xử lý một directory bất đồng bộ"""
        files = []
        subdirs = []
        continuation_token = None
        
        while True:
            try:
                response = await self.list_objects_with_retry(s3_client, bucket, prefix, continuation_token)
                
                # Process files
                if 'Contents' in response:
                    for obj in response['Contents']:
                        file_key = obj['Key']
                        
                        if self.should_skip_path(file_key) or file_key.endswith('/'):
                            continue
                            
                        # Uncomment và modify filter nếu cần
                        # pattern = r'^_cache_html/quy-hoach-huyen-[^/]+/index-https\.html$'
                        # if not re.match(pattern, file_key):
                        #     continue
                        
                        file_info = {
                            "file_key": file_key,
                            "file_size": obj['Size'],
                            "last_modified": obj['LastModified'].isoformat(),
                            "etag": obj['ETag'].strip('"'),
                            "extension": self.get_file_extension(file_key),
                            "content_type": self.get_content_type(file_key),
                            "is_large_file": obj['Size'] > 50 * 1024 * 1024
                        }
                        files.append(file_info)
                
                # Collect subdirectories
                if 'CommonPrefixes' in response:
                    for prefix_obj in response['CommonPrefixes']:
                        dir_prefix = prefix_obj['Prefix']
                        if not self.should_skip_path(dir_prefix):
                            subdirs.append(dir_prefix)
                
                # Check if more results available
                if response.get('IsTruncated'):
                    continuation_token = response.get('NextContinuationToken')
                else:
                    break
                    
            except Exception as e:
                self.logger.error(f"Error processing directory {prefix}: {e}")
                break
        
        self.stats['total_files'] += len(files)
        self.stats['total_directories'] += len(subdirs)
        
        return files, subdirs



    async def generate_manifests_async(self, bucket: str, prefix: str, manifest_dir: str) -> int:
        """Main async function với TRUE STREAMING - xử lý xong clear RAM ngay"""
        os.makedirs(manifest_dir, exist_ok=True)
        
        async with self.session.create_client('s3', **self.config) as s3_client:
            self.logger.info("Starting TRUE STREAMING async file collection...")
            
            # Streaming batch processor - KHÔNG GIỮ DATA TRONG RAM
            manifest_count = 0
            current_batch = []
            total_files_count = 0  # Chỉ đếm, không lưu data
            
            async def flush_current_batch():
                """Save và clear batch ngay lập tức - như tool gốc"""
                nonlocal manifest_count, current_batch
                if current_batch:
                    manifest_count += 1
                    self.logger.info(f"[SAVE] manifest {manifest_count} with {len(current_batch)} files - then CLEARING RAM")
                    
                    # SAVE NGAY LẬP TỨC (await để đảm bảo file được ghi)
                    await self.save_manifest_file_async(
                        current_batch,  # Dùng trực tiếp, không copy
                        manifest_dir, 
                        manifest_count
                    )
                    
                    # CLEAR RAM NGAY SAU KHI SAVE
                    current_batch.clear()
                    self.logger.info(f"[OK] Manifest {manifest_count} saved & RAM cleared")
            
            # Stream collection với TRUE streaming
            directories_queue = deque([prefix])
            processed_dirs = set()
            
            while directories_queue:
                # Lấy batch directories để xử lý song song  
                current_dir_batch = []
                batch_size = min(MAX_CONCURRENT_REQUESTS // 4, len(directories_queue))
                
                for _ in range(batch_size):
                    if directories_queue:
                        dir_prefix = directories_queue.popleft()
                        if dir_prefix not in processed_dirs:
                            current_dir_batch.append(dir_prefix)
                            processed_dirs.add(dir_prefix)
                
                if not current_dir_batch:
                    break
                
                # Xử lý song song batch directories
                tasks = [
                    self.process_directory_async(s3_client, bucket, dir_prefix)
                    for dir_prefix in current_dir_batch
                ]
                
                self.logger.info(f"[PROCESS] Processing {len(current_dir_batch)} directories in parallel...")
                batch_results = await asyncio.gather(*tasks, return_exceptions=True)
                
                # Xử lý kết quả - STREAMING MODE
                for i, result in enumerate(batch_results):
                    if isinstance(result, Exception):
                        self.logger.error(f"[ERROR] Error processing {current_dir_batch[i]}: {result}")
                        continue
                        
                    files, subdirs = result
                    directories_queue.extend(subdirs)
                    
                    # STREAMING: Thêm file và kiểm tra flush ngay
                    for file_info in files:
                        current_batch.append(file_info)
                        total_files_count += 1
                        
                        # FLUSH NGAY KHI BATCH ĐẦY
                        if len(current_batch) >= MAX_ITEMS_PER_MANIFEST:
                            await flush_current_batch()
                    
                    # Progress report (chỉ count, không data)
                    if total_files_count % 5000 == 0:
                        elapsed = time.time() - self.stats['start_time']
                        rate = total_files_count / elapsed if elapsed > 0 else 0
                        self.logger.info(f"[STATS] Processed {total_files_count} files | "
                                       f"Rate: {rate:.1f} files/sec | "
                                       f"Manifests saved: {manifest_count} | "
                                       f"Current batch: {len(current_batch)} files")
            
            # Flush batch cuối cùng (nếu còn)
            await flush_current_batch()
                
            self.logger.info(f"[COMPLETE] STREAMING COLLECTION COMPLETED!")
            self.logger.info(f"[STATS] Total files processed: {total_files_count}")
            self.logger.info(f"[STATS] Total manifests created: {manifest_count}")
            
            # Save summary (chỉ với stats, không data)
            await self.save_summary_async(bucket, prefix, total_files_count, manifest_count, manifest_dir)
            
            return manifest_count

    async def save_summary_async(self, bucket: str, prefix: str, total_files: int, 
                               manifest_count: int, manifest_dir: str):
        """Save summary file async"""
        elapsed_time = time.time() - self.stats['start_time']
        
        summary = {
            "generated_at": datetime.now().isoformat(),
            "source_bucket": bucket,
            "source_prefix": prefix,
            "total_files": total_files,
            "total_manifests": manifest_count,
            "max_items_per_manifest": MAX_ITEMS_PER_MANIFEST,
            "excluded_patterns": EXCLUDED_PATTERNS,
            "performance_stats": {
                "elapsed_time_seconds": round(elapsed_time, 2),
                "files_per_second": round(total_files / elapsed_time, 2) if elapsed_time > 0 else 0,
                "total_requests": self.stats['requests_made'],
                "total_errors": self.stats['errors'],
                "max_concurrent_requests": MAX_CONCURRENT_REQUESTS,
                "total_directories_processed": self.stats['total_directories']
            }
        }
        
        summary_path = os.path.join(manifest_dir, "manifest_summary.json")
        async with aiofiles.open(summary_path, 'w', encoding='utf-8') as f:
            await f.write(json.dumps(summary, indent=2, ensure_ascii=False))
        
        self.logger.info(f"Summary saved to: {summary_path}")
        self.logger.info(f"Performance: {summary['performance_stats']['files_per_second']:.1f} files/sec")

async def main():
    """Main async function"""
    from paths import load_secrets
    _s3 = load_secrets()["s3"]
    generator = AsyncS3ManifestGenerator(
        endpoint_url='https://us-ord-1.linodeobjects.com',
        access_key=_s3["access_key"],
        secret_key=_s3["secret_key"]
    )
    
    source_bucket = "cdn.diaocthongthai.com"
    prefix = "_cache_html/"  # thay đổi nếu cần
    
    timestamp = datetime.now().strftime('%Y%m%d_%H%M%S')
    from paths import MANIFEST_ROOT  # 2026-11-25: ghi vao data/manifests de m2 tu tim thay
    manifest_dir = os.path.join(MANIFEST_ROOT, f"manifest-s3-cache-{timestamp}")
    
    try:
        generator.logger.info("Starting S3 File Manifest Generation v2 (High Performance Async)")
        generator.logger.info(f"Source bucket: {source_bucket}")
        generator.logger.info(f"Prefix: {prefix}")
        generator.logger.info(f"Manifest directory: {manifest_dir}")
        generator.logger.info(f"Max concurrent requests: {MAX_CONCURRENT_REQUESTS}")
        generator.logger.info(f"Max items per manifest: {MAX_ITEMS_PER_MANIFEST}")
        
        total_manifests = await generator.generate_manifests_async(source_bucket, prefix, manifest_dir)
        
        if total_manifests > 0:
            generator.logger.info("Manifest generation completed successfully!")
            print(f"\n[SUCCESS] HIGH PERFORMANCE RESULTS:")
            print(f"Manifest directory: {manifest_dir}")
            print(f"Total manifests: {total_manifests}")
            print(f"Total files: {generator.stats['total_files']}")
            
            elapsed = time.time() - generator.stats['start_time']
            print(f"Elapsed time: {elapsed:.2f} seconds")
            print(f"Performance: {generator.stats['total_files']/elapsed:.1f} files/sec")
            print(f"Total requests: {generator.stats['requests_made']}")
            print(f"Errors: {generator.stats['errors']}")
        else:
            generator.logger.warning("No manifest files generated")
            
    except Exception as e:
        generator.logger.error(f"Manifest generation failed: {e}")
        sys.exit(1)

if __name__ == "__main__":
    # Chạy với asyncio
    asyncio.run(main())