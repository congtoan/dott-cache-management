import os

# 📌 Thư mục gốc cache WP Rocket
BASE_DIR = '/home/diaocthDP4f/diaocthongthai.com/public_html/wp-content/cache/wp-rocket/diaocthongthai.com/'

# 📄 File output chứa danh sách trang lỗi
OUTPUT_FILE = 'redis_error_pages.txt'

# 📝 Danh sách file HTML cần kiểm tra cho mỗi thư mục con
TARGET_FILES = [
    'index-https.html',
    'index-mobile-https.html',
]

# 🔍 Từ khoá để phát hiện trang lỗi Redis
KEYWORDS = [
    'Error establishing a Redis connection',
    'Redis'
]

def file_contains_keywords(filepath):
    """Kiểm tra file có chứa từ khoá liên quan tới lỗi Redis hay không."""
    try:
        with open(filepath, 'r', encoding='utf-8', errors='ignore') as f:
            content = f.read()
            for keyword in KEYWORDS:
                if keyword.lower() in content.lower():
                    return True
    except Exception as e:
        print(f"⚠️ Không thể đọc file {filepath}: {e}")
    return False

def scan_wp_rocket_cache():
    """Duyệt qua toàn bộ thư mục con cấp 1 để kiểm tra các file HTML cache."""
    error_files = []

    for entry in os.scandir(BASE_DIR):
        if entry.is_dir(follow_symlinks=False):
            subdir = entry.path
            for filename in TARGET_FILES:
                filepath = os.path.join(subdir, filename)
                if os.path.isfile(filepath):
                    if file_contains_keywords(filepath):
                        error_files.append(filepath)

    return error_files

def save_results(file_list):
    """Ghi danh sách file lỗi Redis ra file txt."""
    if not file_list:
        print("✅ Không phát hiện file HTML nào chứa lỗi Redis.")
        return

    with open(OUTPUT_FILE, 'w', encoding='utf-8') as f:
        for path in file_list:
            f.write(path + '\n')

    print(f"📄 Đã ghi {len(file_list)} file chứa lỗi Redis vào: {OUTPUT_FILE}")

if __name__ == "__main__":
    print(f"🔍 Đang quét cache WP Rocket trong: {BASE_DIR}\n")
    error_files = scan_wp_rocket_cache()
    save_results(error_files)
