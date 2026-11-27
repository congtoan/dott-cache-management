# Cache Management – diaocthongthai.com

> **Hướng mới (từ 2026-11-27): nhánh [`v2`](https://github.com/congtoan/dott-cache-management/tree/v2).** Hệ thống chia 2 máy, service Go chạy trên VPS phụ.
> Mọi việc mới đều làm ở nhánh `v2`. Đọc `HANDOFF.md` và `docs/HISTORY.md` trên nhánh đó.
> Nhánh `main` này là **pipeline Python cũ (v0)**, vẫn đang chạy cron trên production (tag `v0-legacy`). Nhánh chỉ giữ để tham khảo và vận hành tạm, không phát triển thêm.

Bộ tool quản lý **cache HTML WP Rocket** của diaocthongthai.com: đồng bộ cache lên **S3 Linode** và danh sách URL hợp lệ lên **Cloudflare KV**.

Thư mục này **thay cho** thư mục cũ `D:\Real\Python\2025-01-18 s3 sync with boto3\2025-09-05_S3_Sync_Object_S3_to_S3_v6\cache-tools-new`. Thư mục cũ vẫn giữ nguyên, không xóa.
Tạo ngày 2026-11-25 từ bản mới nhất trên VPS và trên PC.

## Cấu trúc

```
2026-11-25 Cache Management/
├── README.md                 ← file này
├── server/                   ← tool chạy TRÊN VPS (bản chuẩn, deploy lên /root/tool/diaocthongthai.com/cache-tools/)
│   ├── utils.py              ← thư viện lõi: BUCKET_CONFIGS (S3 Chicago + Singapore), trigger_cache, upload_files_to_s3...
│   ├── cache_renew_from_db.py        (C) vòng lặp vô hạn renew cache cũ nhất, đang chạy trong screen F1
│   ├── cache-urls-update.py + .txt   (C) làm mới thủ công danh sách URL
│   ├── cache-segment-update.py       (C) làm mới theo category/archive (hỏi khi chạy)
│   ├── cache_clean_from_db.py        (C) dọn cache mồ côi trên disk + S3 ⚠️
│   ├── cache_create_from_db.py       (C) tạo cache cho URL cached=0 (bản đã sửa bug)
│   ├── DB_init_and_import.py         (A2) tạo DB lần đầu
│   ├── DB_scan_n_update_cache_status.py   quét disk, cập nhật cached/last_update, --stats-only
│   ├── m1_sv_check_missing_disk.py   (B1) kiểm tra thiếu trên disk
│   ├── m3_upload_missing_s3.py       (B3) upload phần thiếu trên S3
│   ├── m4_sv_create_missing_disk.py  (B4) tạo cache còn thiếu
│   ├── scan_wp_rocket_cache_for_redis_error.py   tìm trang cache dính lỗi Redis
│   └── legacy_cron/          ← 2 script cron cũ (nằm ngoài cache-tools trên VPS) + url_list/
├── pc/                       ← tool chạy TRÊN PC (Windows)
│   ├── paths.py              ← nơi khai báo đường dẫn chung: mọi tool PC đọc/ghi vào ../data/
│   ├── config/settings*.py   ← SSH + WP config cho wp_cli_extract_all_urls.py
│   ├── wp_cli_extract_all_urls.py          (A1) lấy toàn bộ URL bằng WP-CLI qua SSH
│   ├── u1_compare_json_and_db_v2.py        (A4a) so JSON với DB, ra u1_urls_to_add/remove
│   ├── u2_update_db_and_kv_v3.4.py         (A4b) cập nhật DB + KV (v3.4: key KV dạng path)
│   ├── KV_export_db_to_kv_json.py / KV_import_json_into_KV.py   (A3) đẩy KV lần đầu
│   ├── m0_pc_s3_manifest_generator_v2_asyncio.py   (B0) liệt kê object trên S3
│   └── m2_pc_check_missing_s3_v2.py        (B2) tìm URL thiếu trên S3
├── data/                     ← dữ liệu (KHÔNG deploy)
│   ├── cache_urls.db         ← bản chụp từ VPS ngày 2026-11-25 12:36 (60.687 URL)
│   ├── wp_url_jsons/         ← 4 JSON URL (2025-11-01, nên chạy lại A1 trước khi dùng u1)
│   ├── manifests/            ← output của m0 (trống)
│   └── backups/              ← u2 backup DB vào đây
├── docs/
│   ├── pipeline.md           ← ⭐ tài liệu chính: kiến trúc, từng tool, thứ tự chạy, trạng thái đã kiểm tra
│   ├── findings-2026-11-25.md← danh sách lỗi F1–F17 (đã sửa / còn mở) + bảng rà tool
│   ├── design-server-only.md ← ⭐ kế hoạch pipeline mới chạy hoàn toàn trên server
│   ├── cloudflare.md         ← token, KV API, Worker
│   ├── dev-guide.md          ← quy ước khi viết tool mới
│   ├── _Note_cache_tools_pipeline_ORIGINAL.md   bản ghi chú gốc (2026-10-20)
│   └── _Note_cache_Tools.docx                   ghi chú gốc dạng Word (⚠️ có S3 key)
└── _archive/                 ← bản cũ, chỉ để tham khảo
    ├── pc_old_versions/      (m2 v1, u1 v1, u2 v3.1/v3.2, cache_create_from_db_PC_BUG.py)
    └── server_deprecated/    (1_, 2_, 3_ đã bỏ; m1/m2 bản cũ đang nằm trên VPS; export_kv_json.py trùng với KV_export)
```

## Thay đổi so với thư mục cũ
- **Tool PC không còn đường dẫn tuyệt đối trỏ về thư mục cũ.** Tất cả đọc qua `pc/paths.py` vào `data/`:
  - `wp_cli_extract_all_urls.py` ghi thẳng vào `data/wp_url_jsons/`, nên không phải copy tay sang cho u1 nữa.
  - `m0` ghi manifest vào `data/manifests/`; `m2` **tự lấy manifest mới nhất**, không phải sửa đường dẫn mỗi lần chạy.
  - `u1`, `u2`, `KV_*` dùng `data/cache_urls.db`, `data/u1_urls_to_*.txt`, `data/kv_export.json`.
  - Chunk KV ghi vào `R:\kv_chunks` nếu có RAM disk, không có thì dùng `data/kv_chunks`.
- `server/` lấy **bản đang chạy trên VPS**. Riêng `cache_create_from_db.py` trên VPS đã sửa bug, bản PC cũ chuyển vào `_archive`.
- Đổi tên cho rõ nơi chạy: `m1_check_missing_disk.py` thành `m1_sv_check_missing_disk.py` (dùng bản PC, có docstring), `m4_create_missing_disk.py` thành `m4_sv_create_missing_disk.py` (nội dung giống hệt). **Trên VPS hiện vẫn mang tên cũ.**
- Không copy: manifest S3 cũ, các file `missing_*.txt` cũ, `kv_export.json` cũ, log, `_backup/*.zip`, DB cũ trên PC (2025-11-01). Các file này vẫn nằm trong thư mục cũ.

## Chạy nhanh

**Yêu cầu PC:** Python 3.10 (`C:\Portable\WinPython\...`), `paramiko`, `boto3`, `aiobotocore`, `aiofiles`, `wrangler` (đã `wrangler login`).
**Yêu cầu VPS:** python3 với `boto3`, `psutil`, `requests`, `beautifulsoup4`, `lxml` (đã cài sẵn).

Truy cập VPS từ PC bằng tool `vps.py` ở dự án `D:\Claude\2026-11-25 Ket Noi VPS` (skill `vps-diaocthongthai`). Dưới đây `$V` là đường dẫn tới tool đó:
```powershell
$V = "D:\Claude\2026-11-25 Ket Noi VPS\vps.py"
```

**Tải DB mới nhất từ VPS về PC** (dùng `.backup` cho an toàn vì renew đang ghi liên tục):
```powershell
"cd /root/tool/diaocthongthai.com/cache-tools && python3 -c `"import sqlite3; s=sqlite3.connect('file:output/cache_urls.db?mode=ro',uri=True); d=sqlite3.connect('/tmp/cu.db'); s.backup(d); d.close()`"" | python $V run -
python $V get /tmp/cu.db "D:\Claude\2026-11-25 Cache Management\data\cache_urls.db"
```

**Deploy 1 tool server lên VPS** (backup bản cũ trên VPS trước):
```powershell
"cp /root/tool/diaocthongthai.com/cache-tools/utils.py /root/tool/diaocthongthai.com/cache-tools/utils.py.bak-$(date +%F)" | python $V run -
python $V put "D:\Claude\2026-11-25 Cache Management\server\utils.py" /root/tool/diaocthongthai.com/cache-tools/utils.py
```

Quy trình chi tiết (A1 → A4, B0 → B4, C) xem [docs/pipeline.md](docs/pipeline.md).

## ⚠️ Lưu ý quan trọng
- **Bản DB gốc là bản trên VPS** (`/root/tool/diaocthongthai.com/cache-tools/output/cache_urls.db`): renew, m1 và m4 ghi liên tục vào đó. `data/cache_urls.db` chỉ là bản sao. Nếu chạy u2 trên PC rồi đẩy DB lên VPS thì sẽ **ghi đè** những gì VPS cập nhật trong lúc đó. Xem mục "Đồng bộ DB" trong pipeline.md.
- Không chạy `cache_clean_from_db.py` khi DB chưa đồng bộ với WordPress, vì nó sẽ xóa nhầm cache hợp lệ trên disk và S3.
- **Secrets:** toàn bộ mật khẩu và key (S3, SSH, MySQL, WP REST) nằm trong `secrets.json` ở thư mục gốc. File này **gitignore**, có bản mẫu `secrets.example.json`. `server/utils.py` tìm `secrets.json` cùng thư mục với nó (khi đã deploy lên VPS) hoặc ở thư mục cha (khi chạy trong repo). Khi deploy bản `utils.py` này lên VPS thì phải copy `secrets.json` lên cùng (`chmod 600`).
- `docs/_Note_cache_Tools.docx` có S3 key dạng chữ thường nên không đưa lên git; `data/` cũng không đưa lên git (tải lại theo hướng dẫn ở trên).
