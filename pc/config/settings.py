import os
import json
from dataclasses import dataclass

# secrets.json o goc project (gitignored) - mau: secrets.example.json
with open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "secrets.json"), encoding="utf-8") as _f:
    _SECRETS = json.load(_f)

@dataclass
class Settings:
    BASE_DIR = "D:/Real/Python/_module/"
    CONFIG_DIR = os.path.join(BASE_DIR, "config/")
    OUTPUT_DIR = os.path.join(BASE_DIR, "output/")
    ROOT_DOWNLOAD_DIR = "D:/Images/"
    
    # Config file paths
    DOWNLOAD_CONFIG_FILE = os.path.join(CONFIG_DIR, "download_image_config.json")
    CLONE_POST_CONFIG_FILE = os.path.join(CONFIG_DIR, "clone_post.json")
    
    # Default values
    DEFAULT_IMG_MAP_JSON_FILE = os.path.join(OUTPUT_DIR, "download_image_map_{time_now}.json")
    DEFAULT_DIR_MODE = '1. fixed_dir'
    DEFAULT_DOWNLOAD_MODE = 'download_from_url'
    DEFAULT_PAGE_URL = 'https://diaocthongthai.com/cap-nhat-hinh-anh-14-nha-ga-tuyen-metro-so-1-thang-4-2024/'
    DEFAULT_IMG_SRC = 'https://cdn.diaocthongthai.com/project/2022/lumiere-boulevard/toan-canh-1-1024x654.jpg'
    DEFAULT_DOWNLOAD_DIR = 'D:/Images'
    DEFAULT_FILE_NAME = ''
    DEFAULT_IMAGE_PREFIX = ''
    
    CSS_SELECTORS = [
        '.sevenPostWrap',
        'div.entry-content',
        'div.inner-post-entry',
        'div.post-body',
        '#ftwp-postcontent',
        '#content',
        '.fl-node-5c9b8c86a7fad',
        'div.entry',
        '.post_content'
    ]

    # Common selectors for post date extraction
    POST_DATE_SELECTORS = [
        # Highest priority: <meta> tag in <head>
        {
            "tag": "meta",
            "attrs": {"property": "article:published_time"},
            "location": "head",
            "extract": lambda tag: tag.get("content", ""),
        },
        # <time> tag with class "entry-date" or "published" in body
        {
            "tag": "time",
            "attrs": {"class": ["entry-date", "published"]},  # Match either class
            "location": "body",
            "extract": lambda tag: tag.get("datetime", tag.get_text(strip=True)),
        },
        # <span> with class "posted-on" containing <time> in body
        {
            "tag": "span",
            "attrs": {"class": "posted-on"},
            "location": "body",
            "extract": lambda tag: (
                tag.find("time").get("datetime", tag.find("time").get_text(strip=True))
                if tag.find("time")
                else ""
            ),
        },
        # <span> with class "meta-date" in body
        {
            "tag": "span",
            "attrs": {"class": "meta-date"},
            "location": "body",
            "extract": lambda tag: tag.get_text(strip=True),
        },
    ]    

    HEADERS = {'User-Agent': 'Mozilla/5.0'}
    TIMEOUT_SECONDS = 5

    # WordPress REST API credentials (doc tu secrets.json o goc project)
    WEBSITE_URL = "https://diaocthongthai.com"
    USERNAME = _SECRETS["wp_rest"]["user"]
    PASSWORD = _SECRETS["wp_rest"]["app_password"]    #PythonCloneTool

    # SSH info
    SSH_HOST = _SECRETS["ssh"]["host"]
    SSH_USER = _SECRETS["ssh"]["user"]
    SSH_PASS = _SECRETS["ssh"]["password"]
    WP_PATH = "/home/diaocthDP4f/diaocthongthai.com/public_html"
    SSH_PORT = int(_SECRETS["ssh"]["port"])

    # 🛢️ MySQL info
    DB_HOST = _SECRETS["mysql"]["host"]
    DB_PORT = int(_SECRETS["mysql"]["port"])
    DB_USER = _SECRETS["mysql"]["user"]
    DB_PASSWORD = _SECRETS["mysql"]["password"]
    DB_NAME = _SECRETS["mysql"]["db_name"]


# Instantiate settings for import
settings = Settings()