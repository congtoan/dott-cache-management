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

    

    WEBSITE_URL = "https://diaocthongthai.com/en"
    USERNAME = _SECRETS["wp_rest"]["user"]
    PASSWORD = _SECRETS["wp_rest"]["app_password"]    #PythonCloneTool

    # SSH info (doc tu secrets.json o goc project)
    SSH_HOST = _SECRETS["ssh"]["host"]
    SSH_USER = _SECRETS["ssh"]["user"]
    SSH_PASS = _SECRETS["ssh"]["password"]
    WP_PATH = "/home/diaocthDP4f/diaocthongthai.com/public_html"
    SSH_PORT = int(_SECRETS["ssh"]["port"])
    
    SUBSITE_URL = "https://diaocthongthai.com/en"

# Instantiate settings for import
settings = Settings()