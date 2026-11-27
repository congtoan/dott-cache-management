# vendor/: thư viện PHP đóng kèm tt-core

Thư viện chỉ được nạp **lúc build** (`tt_core_vendor_autoload()` trong `tt-core.php`). Frontend không nạp.
Mỗi thư viện giữ nguyên mã nguồn gốc và kèm theo `LICENSE`, `composer.json`. Không sửa gì bên trong.

| Thư mục | Thư viện | Phiên bản (tag) | Ngày tag | Giấy phép | Dùng để |
|---|---|---|---|---|---|
| `peast/lib/` | [mck89/peast](https://github.com/mck89/peast) | `v.1.18.2` | 2026-09-29 | BSD-3-Clause | Kiểm tra cú pháp JS (ES2022, dạng script) |
| `matthiasmullie/minify/src/`, `data/` | [matthiasmullie/minify](https://github.com/matthiasmullie/minify) | `1.3.76` | 2026-11-03 | MIT | Minify JS. Thư mục `data/js/` bắt buộc phải có |
| `matthiasmullie/path-converter/src/` | [matthiasmullie/path-converter](https://github.com/matthiasmullie/path-converter) | `1.1.3` | 2019-02-05 | MIT | minify cần |

Tải ngày 2026-11-27 từ `https://codeload.github.com/<repo>/zip/refs/tags/<tag>`. SHA-256 của file zip:
```
peast           e2a69c225d101badc41a9765df736241edcfd29c586ddede026ee941f9737bac
minify          2c66c93c2ea906acea11604d6937cb05332341287b39dec5706eb8a9a766c3cc
path-converter  a0d1377a9958ef533a7cca1d3b8b44c5971eef61e2bf5f2fc6b3cdb5a6fde9e6
```
Peast `v1.18.3` (2026-11-25) mới ra 2 ngày lúc tải, nên mình chọn bản trước đó.
Muốn nâng cấp: tải tag mới, thay nguyên thư mục, sửa bảng này, rồi build thử bằng `wp tt-core check`.
