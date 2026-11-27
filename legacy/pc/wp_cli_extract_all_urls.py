#!/usr/bin/env python3

"""
WP CLI URL Extractor Tool
=========================

### Mô tả tổng quan
Tool này được thiết kế để trích xuất toàn bộ danh sách các URL công khai (public URLs) từ một website WordPress, bao gồm:
- **Single post URLs**: Các URL của bài viết đơn lẻ (cho tất cả public post types).
- **Archive page URLs**: Các URL của trang lưu trữ (archive pages), bao gồm:
  - Post type archives (ví dụ: /posts/, /products/ với pagination).
  - Taxonomy term archives (ví dụ: /category/news/, /tag/tech/ với pagination cho các taxonomy được chỉ định).

**Lý do sử dụng WP-CLI thay vì WP REST API**:
- Phiên bản ban đầu sử dụng WP REST API, nhưng nó rất chậm do phải thực hiện nhiều request HTTP liên tiếp, đặc biệt với các site lớn (hàng nghìn posts/terms). 
- Tool này chuyển sang sử dụng **WP-CLI** (WordPress Command Line Interface) để truy vấn trực tiếp qua lệnh shell (local hoặc qua SSH), giúp tăng tốc độ đáng kể (giảm thời gian từ hàng giờ xuống chỉ vài phút).
- Hỗ trợ chạy local (trên Linux server) hoặc qua SSH (từ Windows dev machine) để tránh phải deploy code lên server.

### Cách hoạt động
1. **Khởi tạo (WPCLIExtractor)**:
   - Nhận settings từ file config (settings.py hoặc settings_en.py), bao gồm SSH credentials, WP path, website URL, subsite URL (nếu multisite).
   - Tự động detect chế độ SSH: Nếu chạy trên Windows → dùng SSH; trên Linux → chạy local.
   - Các tham số tùy chỉnh: 
     - `taxonomies_with_archive`: Danh sách taxonomy cần xử lý archive (ví dụ: category, post_tag).
     - `exclude_patterns`: Regex patterns để loại bỏ URL không mong muốn (ví dụ: /wp-admin/, /?s=search).
     - `exclude_pagination_bases`: Các base URL không tạo pagination (ví dụ: trang tĩnh không cần page/2/).

2. **Kết nối và chạy WP-CLI**:
   - **Kết nối**
     - Nếu `use_ssh=True`: kết nối SSH đến server, chạy các lệnh `wp` từ xa.
     - Nếu `use_ssh=False`: chạy trực tiếp các lệnh WP-CLI trong local environment.
   - Kết nối SSH nếu cần, sau đó chạy các lệnh WP-CLI như `wp post list`, `wp term list`, `wp post-type list`.
   - Lấy `posts_per_page` từ site để tính toán số trang pagination chính xác.
   - Sử dụng batch_size (mặc định 1000) trong `get_single_post_urls` để tránh overload Redis khi query lớn.

3. **Trích xuất dữ liệu**:
   - **Post Type Archives**: Lấy tất cả public post types có `has_archive=true`, tính số trang dựa trên số posts published, build URL base + pagination (page/2/, page/3/...).
   - **Taxonomy Archives**: Với mỗi taxonomy chỉ định, lấy tất cả terms, tính số trang dựa trên `term.count`, build URL từ `term.url` + pagination.
   - **Single Posts**: Lấy tất cả public post types, query từng batch posts published để lấy URL (không pagination vì là single).

4. **Lọc và sắp xếp**:
   - Áp dụng `exclude_patterns` để loại bỏ URL khớp regex (ví dụ: loại bỏ search queries, admin pages).
   - Sắp xếp archive URLs theo thứ tự pagination (base trước, sau đó page/2/, page/3/...).

5. **Lưu file**:
   - Tất cả output lưu vào thư mục `./output/` dưới dạng JSON (dễ parse sau này).
   - Chạy riêng cho từng site (vi/en) với prefix tương ứng.

6. **Chạy tool**:
   - Gọi `run_vi()` cho site tiếng Việt (diaocthongthai.com).
   - Gọi `run_en()` cho site tiếng Anh.
   - Main chạy cả hai.

### Các file xuất ra (trong thư mục ./output/)
Tool chỉ lưu 2 file chính (các file khác bị comment out để tránh dư thừa). Mỗi file là JSON array chứa list các URL string.

1. **`{prefix}_single_post_urls.json`** (ví dụ: `vi_single_post_urls.json`, `en_single_post_urls.json`):
   - **Nội dung**: Danh sách đầy đủ URL của tất cả single posts (bài viết đơn lẻ) sau khi áp dụng exclude_patterns.
   - **Số lượng**: Tùy thuộc site, thường hàng nghìn (ví dụ: ~5000 posts cho site vi).
   - **Cấu trúc**: `["https://example.com/post-1/", "https://example.com/product-abc/", ...]`
   - **Mục đích**: Dùng để crawl nội dung chi tiết từng bài, hoặc import vào sitemap/search engine.

2. **`{prefix}_archive_page_urls.json`** (ví dụ: `vi_archive_page_urls.json`, `en_archive_page_urls.json`):
   - **Nội dung**: Danh sách tất cả archive URLs (post type + taxonomy terms) sau khi filter và sắp xếp.
     - Bao gồm base URLs (ví dụ: /category/news/) và pagination (ví dụ: /category/news/page/2/).
     - Không bao gồm single posts.
   - **Số lượng**: Thấp hơn single posts, thường vài trăm (ví dụ: 200-300 cho site vi, bao gồm tất cả categories/tags/products).
   - **Cấu trúc**: `["https://example.com/blog/", "https://example.com/blog/page/2/", "https://example.com/category/news/", ...]`
   - **Mục đích**: Dùng để crawl danh sách bài viết theo trang (archives), đảm bảo không miss pagination.

**Lưu ý về file**:
- Prefix: "vi" cho site Việt, "en" cho site Anh.
- Nếu cần file riêng lẻ (post_type_urls, taxonomy_urls), uncomment dòng tương ứng trong `run_complete_extraction`.
- File được lưu với encoding UTF-8, indent=2 để dễ đọc.
- Không lưu duplicate URLs (do WP-CLI query unique).

### Cài đặt và chạy
- **Yêu cầu**: Python 3, paramiko (cho SSH), WP-CLI đã cài trên server.
- **Config**: Chỉnh `config/settings.py` và `config/settings_en.py` với SSH/WP info.
- **Chạy**: `python wp_cli_extract_all_urls.py` → Tự động chạy cả vi/en.
- **Debug**: Theo dõi log in console (lệnh WP-CLI, số lượng items).

### Cập nhật gần nhất (2025-09-13)
- Fix hàm `get_single_post_urls`: Sử dụng `batch_size` để query theo lô, tránh lỗi Redis overload khi total posts > 1000.

"""

import platform
import subprocess
import paramiko
import os
import json
import re
import math
from typing import List, Optional
from config.settings import settings
from config.settings_en import settings as settings_en



class WPCLIExtractor:
    def __init__(
        self,
        settings,
        taxonomies_with_archive: Optional[List[str]] = None,
        exclude_patterns: Optional[List[str]] = None,
        exclude_pagination_bases: Optional[List[str]] = None,
        allow_root: bool = True,
        use_ssh: Optional[bool] = None,
    ):
        
        # Nếu không truyền use_ssh thì auto detect theo hệ điều hành
        if use_ssh is None:
            system_name = platform.system().lower()
            if "windows" in system_name:
                self.use_ssh = True   # chạy trên Windows → dùng SSH
            else:
                self.use_ssh = False  # chạy trên Linux (server) → chạy local
        else:
            self.use_ssh = use_ssh

        # settings object (from config.settings or config.settings_en)
        self.settings = settings
        self.ssh_host = settings.SSH_HOST
        self.ssh_user = settings.SSH_USER
        self.ssh_pass = settings.SSH_PASS
        self.ssh_port = getattr(settings, "SSH_PORT", 22)
        self.wp_path = settings.WP_PATH
        # optional: SUBSITE_URL (for multisite)
        self.subsite_url = getattr(settings, "SUBSITE_URL", None)
        self.website_url = getattr(settings, "WEBSITE_URL", "").rstrip("/")

        # site-specific
        self.taxonomies_with_archive = taxonomies_with_archive or []
        self.exclude_patterns = exclude_patterns or []
        self.exclude_pagination_bases = exclude_pagination_bases or []

        # behavior
        self.allow_root = allow_root

        # state
        self.client = None
        self.posts_per_page = 10

    # ---- SSH / WP-CLI helpers ----
    def connect(self):
        if not self.use_ssh:
            print("Local mode: chạy wp-cli trực tiếp, bỏ qua SSH connect.")
            return
                
        self.client = paramiko.SSHClient()
        self.client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        print(f"Connecting to {self.ssh_host}:{self.ssh_port} as {self.ssh_user} ...")
        self.client.connect(
            self.ssh_host,
            port=self.ssh_port,
            username=self.ssh_user,
            password=self.ssh_pass,
            timeout=30
        )
        print("SSH connected.")

        # read posts_per_page
        out = self.run_wp_cli("option get posts_per_page", quiet=True)
        try:
            self.posts_per_page = int(out.strip())
        except Exception:
            self.posts_per_page = 10
        print(f"posts_per_page = {self.posts_per_page}")

    def close(self):
        if self.client:
            self.client.close()
            self.client = None
            print("SSH connection closed.")

    def run_wp_cli(self, command: str, quiet: bool = False) -> str:
        parts = ["wp", command, f"--path={self.wp_path}"]
        if self.subsite_url:
            parts.append(f"--url={self.subsite_url}")
        if self.allow_root:
            parts.append("--allow-root")
        full_cmd = " ".join(parts)

        if not quiet:
            print(f"Chạy lệnh: {full_cmd}")


        if self.use_ssh:
            # chạy qua SSH
            stdin, stdout, stderr = self.client.exec_command(full_cmd)
            out = stdout.read().decode("utf-8").strip()
            err = stderr.read().decode("utf-8").strip()
            if err and "Warning" not in err:
                print(f"WP-CLI STDERR: {err}")
            return out
        else:
            # chạy local
            proc = subprocess.run(
                full_cmd, shell=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                encoding="utf-8"
            )
            if proc.stderr and "Warning" not in proc.stderr:
                print(f"WP-CLI STDERR: {proc.stderr.strip()}")
            return proc.stdout.strip()

    # ---- Data getters ----
    def build_post_type_archive_urls(self) -> List[str]:
        """
        Build archive URLs (with pagination) for all post types that have has_archive.
        """
        urls = []
        out = self.run_wp_cli("post-type list --fields=name,public,has_archive --format=json")
        post_types = json.loads(out) if out else []

        for pt in post_types:
            if pt.get("public") is True and pt.get("has_archive") not in (False, None, "", "0"):
                post_type = pt["name"]

                # count published posts
                count_out = self.run_wp_cli(
                    f"post list --post_type={post_type} --post_status=publish --fields=ID --format=count"
                )
                try:
                    count = int(count_out.strip())
                except Exception:
                    count = 0

                max_page = max(1, math.ceil(count / self.posts_per_page))

                base = f"{self.website_url}/{pt['has_archive'].strip('/')}/" \
                       if isinstance(pt["has_archive"], str) else f"{self.website_url}/{post_type}/"

                urls.append(base)
                if base not in self.exclude_pagination_bases and max_page > 1:
                    for p in range(2, max_page + 1):
                        urls.append(f"{base}page/{p}/")

                print(f"Archive for {post_type}: {max_page} pages (base: {base})")

        return urls

    def get_taxonomy_terms(self, taxonomy: str) -> List[dict]:
        out = self.run_wp_cli(f"term list {taxonomy} --fields=term_id,slug,url,count --format=json")
        terms = json.loads(out) if out else []
        print(f"Found {len(terms)} terms for {taxonomy}")
        return terms

    def build_taxonomy_urls(self) -> List[str]:
        urls = []
        for taxonomy in self.taxonomies_with_archive:
            print(f"\nProcessing taxonomy: {taxonomy}")
            terms = self.get_taxonomy_terms(taxonomy)
            for term in terms:
                count = term.get("count") or 0
                if isinstance(count, str):
                    try:
                        count = int(count)
                    except ValueError:
                        count = 0
                max_page = max(1, math.ceil(count / self.posts_per_page))

                base = term.get("url")
                if not base:
                    continue

                urls.append(base)
                if base not in self.exclude_pagination_bases and max_page > 1:
                    for p in range(2, max_page + 1):
                        urls.append(f"{base}page/{p}/")

                print(f"  {term.get('slug')}: {max_page} pages (base: {base})")
        return urls

    def get_single_post_urls(self, post_type: str, batch_size: int = 1000) -> List[str]:
        urls = []
        offset = 0

        # Lấy tổng số post trước
        total_count_out = self.run_wp_cli(
            f"post list --post_type={post_type} --post_status=publish --fields=ID --format=count",
            quiet=True
        )
        try:
            total_count = int(total_count_out.strip())
        except Exception:
            total_count = 0

        print(f"Total {total_count} posts for {post_type}")

        while offset < total_count:
            out = self.run_wp_cli(
                f"post list --post_type={post_type} --post_status=publish "
                f"--fields=ID,url --format=json --posts_per_page={batch_size} --offset={offset}"
            )
            batch = json.loads(out) if out else []
            if not batch:
                break

            urls.extend([p["url"] for p in batch if p.get("url")])
            print(f"Fetched {len(batch)} posts (offset {offset}) for {post_type}")

            offset += batch_size

        print(f"Found {len(urls)} published posts for {post_type}")
        return urls




    def get_all_single_post_urls(self) -> List[str]:
        urls = []
        out = self.run_wp_cli("post-type list --fields=name,public --format=json")
        post_types = json.loads(out) if out else []
        for pt in post_types:
            if pt.get("public") is True:
                post_type = pt["name"]
                urls.extend(self.get_single_post_urls(post_type))
        return urls

    # ---- Save / filter ----
    def save_list_to_file(self, data: List, filename: str, as_json: bool = True) -> Optional[str]:
        from paths import JSON_DIR  # 2026-11-25: ghi thang vao data/wp_url_jsons (input cua u1)
        output_dir = JSON_DIR
        os.makedirs(output_dir, exist_ok=True)
        output_file = os.path.join(output_dir, filename)
        with open(output_file, "w", encoding="utf-8") as f:
            if as_json:
                json.dump(data, f, ensure_ascii=False, indent=2)
            else:
                for item in data:
                    f.write(f"{item}\n")
        print(f"Saved {len(data)} items to {output_file}")
        return output_file

    def apply_exclude_patterns(self, url_list: List[str]) -> List[str]:
        filtered = []
        for url in url_list:
            if not any(re.search(pattern, url) for pattern in self.exclude_patterns):
                filtered.append(url)
        print(f"Applied exclude patterns: {len(url_list)-len(filtered)} excluded, {len(filtered)} remain")
        return filtered

    # ---- Orchestrator ----
    def run_complete_extraction(self, prefix: str = "site") -> dict:
        print(f"=== WP-CLI URL EXTRACTION for {prefix} ===\n")
        if self.client is None:
            self.connect()

        def pagination_sort_key(url: str):
            match = re.search(r"/page/(\d+)/", url)
            if match:
                return (1, int(match.group(1)))
            return (0, 0)

        # 1. Post type archive URLs
        print("1. Getting post type archive URLs...")
        post_type_urls = self.build_post_type_archive_urls()
        post_type_urls = sorted(post_type_urls, key=pagination_sort_key)
        # self.save_list_to_file(post_type_urls, f"{prefix}_post_type_urls.json")

        # 2. Taxonomy URLs (full)
        print("\n2. Getting taxonomy URLs (with pagination)...")
        tax_term_urls = self.build_taxonomy_urls()

        # 3. Single post URLs (full)
        print("\n3. Getting single post URLs (all public post types)...")
        single_post_urls = self.get_all_single_post_urls()

        # 4. Apply exclude patterns to taxonomy URLs
        print("\n4. Filtering taxonomy URLs...")
        filtered_taxonomy_urls = self.apply_exclude_patterns(tax_term_urls)
        filtered_taxonomy_urls = sorted(filtered_taxonomy_urls, key=pagination_sort_key)
        # self.save_list_to_file(filtered_taxonomy_urls, f"{prefix}_taxonomy_urls.json")

        # 5. Apply exclude patterns to single post URLs
        print("\n5. Filtering single post URLs...")
        filtered_single_post_urls = self.apply_exclude_patterns(single_post_urls)
        self.save_list_to_file(filtered_single_post_urls, f"{prefix}_single_post_urls.json")

        # 6. Combine post type + taxonomy URLs
        print("\n6. Combining post type + taxonomy URLs...")
        archive_page_urls = post_type_urls + filtered_taxonomy_urls
        archive_page_urls = self.apply_exclude_patterns(archive_page_urls)
        archive_page_urls = sorted(archive_page_urls, key=pagination_sort_key)
        self.save_list_to_file(archive_page_urls, f"{prefix}_archive_page_urls.json")

        self.close()
        return {
            # "post_type_urls": post_type_urls,
            # "taxonomy_urls": filtered_taxonomy_urls,
            "single_post_urls_full": single_post_urls,
            "single_post_urls": filtered_single_post_urls,
            "archive_page_urls": archive_page_urls,
        }


def run_vi():
    taxonomies_with_archive = ["category", "post_tag", "cdt", "khu-vuc", "san-pham"]
    exclude_patterns = [
        r'/danh-sach-xa-phuong-viet-nam/',
        r'/danh-sach-phuong-xa-(.*)/.*',
        r'/test-page/',
        r'/danh-sach-quan-huyen-viet-nam/',
        r'/test2/',
        r'/huy-dang-ky-email/',
        r'/danh-sach-xa-phuong-viet-nam-2025-new/',
        r'/wp-admin/.*',
        r'/wp-login.*',
        r'/.*\?.*',
        r'/feed/.*',
        r'/.*/feed/.*',
        r'/.*sitemap.*',
        r'/\?s=.*'
    ]
    exclude_pagination_bases = [
        "https://diaocthongthai.com/ban-do-34-tinh-thanh-viet-nam-2025/",
    ]

    extractor = WPCLIExtractor(
        settings=settings,
        taxonomies_with_archive=taxonomies_with_archive,
        exclude_patterns=exclude_patterns,
        exclude_pagination_bases=exclude_pagination_bases,
        allow_root=True,
    )
    return extractor.run_complete_extraction(prefix="vi")


def run_en():
    taxonomies_with_archive = ["category", "post_tag"]
    exclude_patterns = [
        r'/en/test-post/',
        r'/en/list-of-all-countries-in-the-world/',
        r'/en/list-of-all-us-states/',
        r'/en/list-of-all-us-counties/',
        r'/en/list-of-all-us-cities-and-places/',
        r'/wp-admin/.*',
        r'/wp-login.*',
        r'/.*\?.*',
        r'/feed/.*',
        r'/.*/feed/.*',
        r'/.*sitemap.*',
        r'/\?s=.*'
    ]
    exclude_pagination_bases = []

    extractor = WPCLIExtractor(
        settings=settings_en,
        taxonomies_with_archive=taxonomies_with_archive,
        exclude_patterns=exclude_patterns,
        exclude_pagination_bases=exclude_pagination_bases,
        allow_root=True,
    )
    return extractor.run_complete_extraction(prefix="en")


def main():
    # Bạn có thể chọn chạy cả hai hoặc chỉ một
    run_vi()
    run_en()


if __name__ == "__main__":
    main()
