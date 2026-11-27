# File cũ còn giữ – generatepress_en

Các file dưới đây **không còn được theme nạp**, nhưng HTML cache cũ (trên đĩa WP Rocket, S3 `_cache_html/en/`, Cloudflare) vẫn có thể còn tham chiếu tới chúng.
**Không sửa, không xóa** cho tới khi chắc chắn không còn HTML nào dùng tới.

| File | Thay bằng | Ngừng nạp từ | Ghi chú |
|---|---|---|---|
| `thongthai-desktop.js` | `tt-core/core.js` (module `tt-core/modules/*.js`) | (ngày deploy tt-core) | JS desktop cũ. Đường lui trong `functions.php` vẫn dùng khi chưa có bản build |
| `thongthai-mobile.js` | `tt-core/core.js` | (ngày deploy tt-core) | JS mobile cũ. Đường lui như trên |
| `thongthai-script.js` | – | trước 11/2026 | Bản cũ 2024-11 (trước khi tách desktop/mobile). Không thấy enqueue ở đâu, giữ phòng HTML rất cũ |

`home.js` **không phải file cũ**: hiệu ứng trang chủ `/en/`, không do `functions.php` nạp (kiểm tra lại nơi nạp trước khi đụng tới).

Bản build tt-core cũ: chỉ có `tt-core/core.prev.js` (bản liền trước). Các `?ver=` đã dùng nằm trong `tt-core/core.manifest.json` → `history`.

## Khi nào được xóa
Khi cả 3 nơi đều không còn tham chiếu:
1. Đĩa VPS chính: `grep -rl "generatepress_en/thongthai-" wp-content/cache/wp-rocket/diaocthongthai.com/en --include=index*.html | head` không trả gì.
2. S3 `cdn.diaocthongthai.com/_cache_html/en/`: v2 đã renew hết một vòng sau ngày deploy.
3. Đã qua ít nhất 30 ngày kể từ lúc 2 bước trên xong.

Xóa xong thì ghi lại ở đây (ngày, người xóa).
