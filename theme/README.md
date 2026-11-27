# theme/: tt-core cho generatepress_child và generatepress_en

Thiết kế: [docs/design-tt-core.md](../docs/design-tt-core.md). Trạng thái: **bản nháp, chưa deploy**.

| Thư mục | Nội dung |
|---|---|
| `engine/` | `base.js` (API `window.TT`), `tt-core.php` (build, enqueue, dashboard, WP-CLI), `vendor/` (thư viện PHP, chưa có) |
| `generatepress_child/`, `generatepress_en/` | `functions.php` mới, `LEGACY-FILES.md`, `tt-core/modules/*.js` |
| `tools/legacy-2026-11-27/` | Bản chụp JS và `functions.php` cũ từ VPS chính, dùng để so sánh |
| `tools/split-legacy.js` | Tách JS cũ thành module (đã chạy một lần, chạy lại sẽ ghi đè module) |
| `tools/verify-parity.js` | Kiểm tra module so với JS cũ, cú pháp, chạy thử desktop/mobile |
| `tools/package.js` | Gom file deploy vào `dist/<theme>/` (`dist/` không commit) |

## Sửa module
1. Sửa `generatepress_*/tt-core/modules/NNN-id.js`. Dòng đầu là khai báo `@tt-module` (id, device, ver, order).
2. Tăng `ver` của module.
3. `node theme/tools/verify-parity.js`. Sau khi đã cố ý đổi code thì phần so với JS cũ sẽ báo lệch: đó là chuyện bình thường. Phần cú pháp và chạy thử phải đạt.
4. Deploy file module đó, rồi vào **Giao diện → TT Core → Generate** (hoặc `wp tt-core build --url=...`).

**Không chạy lại** `split-legacy.js` sau khi đã sửa module: nó tạo lại module từ bản chụp cũ.

## Thư viện vendor
Đã có trong `engine/vendor/`: Peast (kiểm cú pháp), matthiasmullie/minify và path-converter. Phiên bản và SHA-256 xem [engine/vendor/README.md](engine/vendor/README.md).
Thiếu thư viện thì build vẫn chạy, nhưng không kiểm cú pháp và không minify (dashboard báo).

## Deploy (khi được duyệt, từng bước)
1. `node theme/tools/package.js`
2. Backup thư mục theme trên server (`cp -a <theme> <theme>.bak-$(date +%F)`).
3. Chép `dist/<theme>/*` đè lên thư mục theme. Chỉ thêm `tt-core/` và thay `functions.php`, file cũ giữ nguyên.
   Sau đó sửa chủ sở hữu và quyền: `chown -R <user>:<user> <theme>`, `find <theme>/tt-core -type d -exec chmod 755 {} +`, `find <theme>/tt-core -type f -exec chmod 644 {} +`. Lý do: tar tạo từ Windows mang quyền 666/777 (đã gặp khi test trên php81).
4. Mở **Giao diện → TT Core**, bấm **Generate**. Trước khi Generate, site vẫn chạy JS cũ (đường lui).
5. Kiểm tra: `<url>?djtprobe=N`, `?tt_device=mobile`, `?tt_device=desktop`. Trong console gõ `TT.list()`.

Test trước trên **php81.congtoan.net** (multisite). Mỗi lần ghi lên server đều phải hỏi chủ site.
