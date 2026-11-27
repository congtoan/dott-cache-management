# Thiết kế tt-core: một file JS cho cả desktop và mobile

> Trạng thái: **bản nháp** (2026-11-27). Code nháp nằm trong [`theme/`](../theme/). Chưa đưa lên server nào.
> Liên quan: [findings-php-render-2026-11-27.md](findings-php-render-2026-11-27.md) mục 5 (desktop và mobile chỉ khác vài dòng), [design-server-only.md](design-server-only.md) mục 15.

## 1. Mục tiêu

- Mỗi trang HTML **giống hệt nhau cho desktop và mobile**. Khi đó WP Rocket không cần "Separate cache for mobile", v2 chỉ render 1 lần mỗi URL, số file trên đĩa và S3 giảm một nửa.
- Thiết bị được nhận biết **trong trình duyệt**, không còn dùng `wp_is_mobile()` lúc PHP render.
- Mỗi theme (`generatepress_child` cho vi, `generatepress_en` cho en) có **một file JS duy nhất** là `tt-core/core.js`. Không có request thứ 2.
- Plugin (ví dụ `ct-multi-sticky-sidebar`) **chèn được code của mình** vào `core.js`, chạy riêng cho desktop hoặc mobile, hoặc tải thêm file JS chỉ cho một loại thiết bị.
- File nguồn viết rõ ràng, nhiều comment. File phát hành (`core.js`) được minify.

Ngoài phạm vi bản nháp này (xử lý sau, chủ site đã chốt):
- `ct-multi-sticky-sidebar` bỏ `wp_is_mobile()` và chuyển sang module tt-core.
- Ảnh preload LCP của trang chủ (WP Rocket in ảnh cover desktop hoặc mobile tùy thiết bị).
- wpDataTables đổi kiểu phân trang khi `wp_is_mobile()` (render bằng JS, ít ảnh hưởng).

## 2. Hiện trạng (đo 2026-11-27, chỉ đọc)

| Theme | Desktop | Mobile | Khác nhau |
|---|---|---|---|
| `generatepress_child` | `thongthai-script.js` 70,8 KB | `thongthai-script-mobile.js` 49,8 KB | Mobile thiếu: chèn widget block 40/42, block 41 "xem nhiều", thanh cuộn ngang, xóa dữ liệu right-click cũ, "Bạn đã xem". Lệch nhỏ: `xemthem` (desktop thêm điều kiện `innerWidth < 480`), `topOffset` của sticky header. **Aff Redirect khác hẳn** (mobile mở link app `shopeevn://`, nghe `touchstart` và `scroll`) |
| `generatepress_en` | `thongthai-desktop.js` 28,0 KB | `thongthai-mobile.js` 20,2 KB | Mobile là bản **cắt bớt** của desktop: thiếu block 28 "xem nhiều" và "Seen posts" |

Server chọn file bằng `wp_is_mobile()` trong `functions.php` của từng theme.

Các nơi khác dùng `wp_is_mobile()` (chỉ ghi lại, xử lý sau): `ct-multi-sticky-sidebar`, preload LCP của WP Rocket, wpDataTables. Hai plugin **không ảnh hưởng**: widget-options (0 widget đặt điều kiện thiết bị) và shortcoder (48 shortcode đều đặt "all").

Không có HTML hay plugin nào gọi tới các hàm toàn cục trong JS theme. Đã kiểm 245 trang cache ngẫu nhiên, `uploads/hot-deal`, `plugins/ct-*` và `mu-plugins`. Ngoại lệ:
- `window.removeViewedPage`: vẫn được giữ là hàm toàn cục.
- `isPaymentInProgress`: do `ct-sepay-integration` đặt, theme chỉ đọc.

Vì vậy bọc từng đoạn code vào hàm riêng là an toàn.

## 3. Cấu trúc file

### Trong repo (nguồn)
```
theme/
  README.md                       cách deploy, cách test
  engine/                         phần chung, chép vào <theme>/tt-core/ khi deploy
    base.js                       API window.TT (nguồn, nhiều comment)
    tt-core.php                   build, enqueue, dashboard, WP-CLI
    vendor/                       thư viện PHP đóng kèm (xem mục 6)
  generatepress_child/
    functions.php                 bản mới: nạp tt-core, giữ đường lui cũ
    LEGACY-FILES.md               file cũ còn giữ vì HTML cache cũ
    tt-core/modules/*.js          module của theme vi
  generatepress_en/
    functions.php
    LEGACY-FILES.md
    tt-core/modules/*.js          module của theme en
  tools/                          script Node chạy trên PC: tách JS cũ, kiểm tra
```

### Trên server (sau khi deploy)
```
themes/<theme>/tt-core/
  base.js, tt-core.php, vendor/     (từ engine/)
  modules/*.js                      module của theme
  core.js                           FILE BUILD, không sửa tay, không commit
  core.prev.js                      bản build trước, dùng để quay lui
  core.manifest.json                build lúc nào, gồm module nào, kích thước
uploads/tt-core/<theme>/modules/*.js   module do plugin tự sinh (ví dụ từ cấu hình)
```

`uploads/tt-core/` nằm ở `wp-content/uploads/` gốc, cả blog en cũng dùng thư mục này (không dùng `uploads/sites/2/`). Nhờ vậy 2 theme theo cùng một quy ước.

## 4. Module

### 4.1 Khai báo
Dòng đầu file module:
```js
/* @tt-module id=aff-redirect-mobile device=mobile ver=1.0.0 order=121 */
```
| Khóa | Bắt buộc | Ý nghĩa |
|---|---|---|
| `id` | có | Chữ thường, số, `-`. Duy nhất trong một theme |
| `device` | không (mặc định `all`) | `all`, `desktop` hoặc `mobile` |
| `ver` | không | Phiên bản, hiện trên dashboard |
| `order` | không (mặc định 500) | Thứ tự trong `core.js`, nhỏ chạy trước. Bằng nhau thì xếp theo id |
| `ready` | không (mặc định `0`) | `1`: đợi DOM sẵn sàng mới chạy. `0`: chạy ngay khi `core.js` chạy, giống code cũ |

Phần còn lại của file là code JS bình thường. Code này được bọc trong một hàm, nên biến không bị lộ ra ngoài. Hàm toàn cục nào cần thiết thì gán vào `window.xxx`.

### 4.2 Ba nguồn module
| Nguồn | Khi nào dùng |
|---|---|
| `themes/<theme>/tt-core/modules/*.js` | Module của chính theme |
| Plugin đăng ký qua action `tt_core_modules` (file nằm trong thư mục plugin) | **Cách nên dùng nhất** cho plugin có file JS cố định. Cập nhật plugin là module tự cập nhật, plugin không phải ghi file |
| `uploads/tt-core/<theme>/modules/*.js` | Plugin **tự sinh code** từ cấu hình (giống `lss-1.js` hiện nay). Ghi ra file tạm rồi `rename`, sau đó gọi `tt_core_request_build()` |

Trùng id: module trong uploads thắng plugin, plugin thắng theme. Dashboard có cảnh báo khi trùng.

### 4.3 Plugin đăng ký
```php
add_action( 'tt_core_modules', function ( $reg, $theme ) {
    $reg->add( 'lss', array(
        'ver'    => LSS_VER,
        'device' => 'desktop',                         // all | desktop | mobile
        'file'   => __DIR__ . '/assets/lss-tt.js',     // hoặc 'code' => '...', hoặc 'src' => URL
        'order'  => 600,
    ) );
}, 10, 2 );

// Theme cũ chưa có tt-core: plugin tự enqueue như trước.
if ( ! function_exists( 'tt_core_api' ) || tt_core_api() < 1 ) { /* fallback */ }
```
- `'src' => URL`: `core.js` chỉ chứa một lệnh `TT.load(URL)`, chạy trên đúng thiết bị khai báo. Thiết bị khác không tải file này.
- `$theme` là tên theme đang được build, để plugin chỉ đăng ký cho theme mình muốn.

## 5. API JS: `window.TT` (api 1)

| | |
|---|---|
| `TT.api` | `1`. Chỉ thêm, không đổi nghĩa |
| `TT.mobile`, `TT.device` | `true`/`false`, `'mobile'`/`'desktop'` |
| `TT.ready(fn)` | Chạy `fn` khi DOM sẵn sàng. Nếu DOM đã sẵn sàng thì chạy ngay |
| `TT.define(id, opt, fn)` | Mỗi id chỉ chạy 1 lần. Bỏ qua nếu sai thiết bị. Lỗi của module nào chỉ làm hỏng module đó (code cũ: lỗi ở một khối làm dừng cả file) |
| `TT.has(id)`, `TT.ver(id)`, `TT.list()` | Kiểm tra module nào đã chạy, phiên bản nào |
| `TT.load(src, opt)` | Tải thêm script, trả về Promise, không tải trùng |
| `TT.errors` | Danh sách lỗi của các module |
| `TT.build` | Thông tin bản build: hash, theme, thời điểm |
| `window.TTQ.push([id, opt, fn])` | Script chạy trước `core.js` xếp hàng chờ ở đây |

Nhận biết thiết bị (giống `wp_is_mobile()` của WordPress 6.x):
1. Có `navigator.userAgentData.mobile` (tương đương header `Sec-CH-UA-Mobile`) thì dùng giá trị đó.
2. Không có thì dùng regex UA `Mobile|Android|Silk/|Kindle|BlackBerry|Opera Mini|Opera Mobi`.

Như vậy iPad (UA kiểu Mac) vẫn là desktop, đúng như hiện nay.

Để test: thêm `?tt_device=mobile` hoặc `?tt_device=desktop` vào URL. Giá trị được nhớ trong `sessionStorage` của tab đó. `?tt_device=auto` để xóa. Query string lạ cũng giúp bỏ qua cache WP Rocket.

`<html>` được thêm class `tt-mobile` hoặc `tt-desktop`. **Không dùng class này cho bố cục CSS**: `core.js` nạp ở footer nên giao diện sẽ bị nháy. Bố cục vẫn dùng media query.

## 6. Build

### 6.1 Khi nào build
Frontend **không quét file**. Mỗi trang chỉ đọc một network option (đã nằm trong Redis object cache) để lấy `ver`.

| Cách | Ghi chú |
|---|---|
| Nút **Generate** ở Giao diện → TT Core | Thủ công, cách chính |
| `wp tt-core build --url=<site>` | Có `wp tt-core check` (chỉ kiểm tra cú pháp, không ghi gì) |
| Sự kiện: bật, tắt, cập nhật plugin hoặc theme | Chỉ **đánh dấu cần build lại** (dirty). Build thật xảy ra ở lần mở trang admin kế tiếp của blog dùng theme đó |
| Plugin gọi `tt_core_request_build( $theme, $reason )` | Như trên. Dùng sau khi plugin ghi file vào uploads hoặc lưu cấu hình |

Trang TT Core có quét file (chỉ khi mở trang đó) để báo "có module thay đổi chưa build".

### 6.2 Các bước build (chỉ PHP, không cần Node)
1. Gom module từ 3 nguồn, áp dụng thiết lập trên dashboard (bật/tắt, đổi thiết bị), sắp xếp theo `order`.
2. **Kiểm tra cú pháp** từng module bằng thư viện **Peast** (PHP, đọc được ES2022). Module nào lỗi thì **dừng build**, báo đúng file và dòng, giữ nguyên `core.js` đang chạy.
3. Bọc module trong `TT.define("id",{...},function(TT){ ... })`, ghép sau `base.js`.
4. **Minify** bằng `matthiasmullie/minify` (bỏ comment và khoảng trắng, không đổi tên biến). Sau đó **kiểm tra lại bản đã minify** bằng Peast. Nếu bản minify lỗi thì dùng bản chưa minify và ghi cảnh báo. Không có Peast thì mặc định **không minify**, vì không kiểm được kết quả.
5. Ghi ra `core.js.tmp`, chép `core.js` cũ sang `core.prev.js`, rồi `rename`. Ghi `core.manifest.json`. Cập nhật network option `tt_core_builds[<theme>]` gồm `ver` = 10 ký tự đầu của md5 nội dung, thời điểm, người build, danh sách module và kích thước.
6. Có khóa (`flock`) để 2 lần build không chạy cùng lúc.

`ver` tính theo nội dung, nên nội dung không đổi thì `ver` cũng không đổi.

Số đo minify trên server (2026-11-27): `thongthai-script.js` từ 70,8 KB còn 43,4 KB (gzip từ 19,3 KB còn 12,9 KB), mất 12 ms. `thongthai-desktop.js` từ 28,0 KB còn 16,3 KB (gzip từ 8,6 KB còn 5,6 KB). Không cần tạo sẵn file `.gz`: nginx gzip lúc gửi, Cloudflare nén brotli rồi cache ở edge.

Node trên VPS chính (`/usr/bin/node` v10.19, gói apt của Ubuntu 20.04, không có npm) **không được dùng**: quá cũ, không hiểu cú pháp như `?.` (code hiện tại có dùng), và PHP-FPM đã chặn `exec`.

### 6.3 Thư viện đóng kèm (`engine/vendor/`)
- `matthiasmullie/minify` và `matthiasmullie/path-converter` (MIT).
- `mck89/peast` (BSD-3).

Đã tải ngày 27/11 (chủ site đồng ý): Peast `v.1.18.2`, minify `1.3.76`, path-converter `1.1.3`. Phiên bản và SHA-256 ghi trong `theme/engine/vendor/README.md`. Thiếu thư viện thì build vẫn chạy nhưng không kiểm tra cú pháp và không minify, và dashboard hiện cảnh báo.

## 7. Enqueue và đường lui
- Có bản build: `wp_enqueue_script( 'tt-core', <theme>/tt-core/core.js, [], <ver>, true )`. HTML chỉ có **một thẻ** `<script id="tt-core-js">`.
- Chưa có bản build (mới deploy, build lỗi lần đầu): `functions.php` quay về cách cũ (`wp_is_mobile()` và 2 file JS cũ). Site không bao giờ bị mất JS.

## 8. Cache và trang HTML cũ
- Tên file mới (`tt-core/core.js`), nên HTML cache cũ vẫn nạp `thongthai-script*.js` cũ. **Không sửa và không xóa** các file cũ (xem `LEGACY-FILES.md`).
- Cloudflare cache JS **theo cả query string** với `max-age=31536000, immutable` (đã đo: cùng `?ver=` thì HIT, `?ver=` lạ thì MISS). Vì vậy HTML cũ giữ `core.js?ver=<cũ>` cho tới khi Cloudflare xóa bản đó khỏi cache. Sau đó request về origin và nhận bản mới nhất. Quy tắc bắt buộc: **module phải chạy được với HTML cũ** (luôn kiểm tra selector tồn tại rồi mới chạy) và `TT.api` chỉ được thêm.
- Muốn bản sửa tới mọi trang ngay lập tức: purge các URL `core.js?ver=...` đã dùng (lịch sử `ver` có trong manifest). Làm sau, khi đã có token Cloudflare mới.

## 9. Dashboard: Giao diện → TT Core
- Bản build hiện tại: `ver`, thời điểm, kích thước (gốc và đã minify), thư viện nào đang có.
- Bảng module: id, nguồn, thiết bị, phiên bản, kích thước, trạng thái cú pháp, **bật/tắt**, **đổi thiết bị**. Thiết lập lưu trong network option `tt_core_settings[<theme>]`.
- Nút **Generate**, **Quay về bản trước**, **Kiểm tra cú pháp**.
- **Không có ô soạn code JS**: code luôn nằm trong file, có lịch sử git.
- Quyền: `manage_options`, có nonce.

## 10. Tách module từ JS hiện tại

### generatepress_child (vi)
| order | id | device | Nguồn (dòng trong `thongthai-script.js`) |
|---|---|---|---|
| 010 | widget-insert | desktop | 1–30 |
| 020 | top-hot | desktop | 32–61 |
| 030 | load-more | all | 64–347 |
| 040 | iframe-touch | all | 349 (code đã obfuscate, giữ nguyên) |
| 050 | download-image | all | 351–540 |
| 060 | xemthem | all | 542–570. Điều kiện `innerWidth < 480` đổi thành `(TT.mobile \|\| innerWidth < 480)` |
| 070 | sticky-table-header | all | 571–662. `topOffset`: mobile luôn `0px` |
| 080 | table-filter | all | 664–689 |
| 090 | sortable-table | all | 691–829 |
| 100 | scrollbar | desktop | 831–1080 |
| 110 | aff-flyicon | all | 1083–1101 |
| 120 | aff-redirect-desktop | desktop | 1103–1318 |
| 121 | aff-redirect-mobile | mobile | `thongthai-script-mobile.js` 788–934 |
| 130 | rightclick-cleanup | desktop | 1321–1323 |
| 140 | viewed-history | desktop | 1326–1521 |
| 150 | call-chat | all | 1524–1692 |
| 160 | fancybox | all | 1694–1780 |
| 170 | kksr-nonce | all | 1782–1849 |

### generatepress_en
| order | id | device | Nguồn (dòng trong `thongthai-desktop.js`) |
|---|---|---|---|
| 010 | atlas-widget | all | 8–22 |
| 020 | top-hot | desktop | 24–53 |
| 030 | expand-collapse | all | 58–66 |
| 040 | load-more | all | 68–180 |
| 050 | download-image | all | 183–371 |
| 060 | viewed-history | desktop | 374–570 |
| 070 | fancybox | all | 572–658 |
| 080 | kksr-nonce | all | 660–727 |

Kiểm tra tương đương bằng `theme/tools/verify-parity.js`: ghép lại module theo từng thiết bị rồi so với file gốc. Chỉ được lệch đúng những dòng đã ghi ở bảng trên.

## 11. Lộ trình
1. Bản nháp trong repo, kiểm tra trên PC (Node 22): cú pháp, tương đương với JS cũ, API `TT`. ← **đang ở đây**
2. Chủ site đồng ý tải thư viện vendor.
3. Test trên **php81.congtoan.net** (multisite, theme `generatepress-child`, 15 bài): chép 2 theme mới sang, build, kiểm tra trên desktop, mobile và `?tt_device=`. Mỗi lần ghi lên server đều hỏi trước.
4. Đưa lên diaocthongthai.com: deploy theme, Generate, kiểm tra vài URL bằng `?djtprobe=` (không qua cache). Lúc này **vẫn còn 2 bản cache** desktop và mobile, nhưng nội dung đã gần như giống nhau.
5. Sau khi chạy ổn (xử lý sticky sidebar, preload LCP, wpDataTables), tắt "Separate cache for mobile", sửa Worker và v2 chỉ render 1 bản.

## 11b. Kết quả test trên php81.congtoan.net (2026-11-27, chủ site đồng ý)

**Đã thiết lập để test:**
- Chép `generatepress_child` và `generatepress_en` từ diaocthongthai.com (production chỉ đọc), rồi phủ `dist/` lên.
- Tạo 2 site con: **site 3** `/tttest/` (theme vi) và **site 4** `/tttest-en/` (theme en). Site 1 và site 2 cùng theme `generatepress-child` của php81 không bị đụng tới.

| Phép thử | Kết quả |
|---|---|
| `wp tt-core check`, rồi `build` | ✅ vi: 18 module, gốc 90.355 byte, **minify 50.910 byte** (gzip 14,7 KB, brotli 13,1 KB). en: 8 module, gốc 37.085 byte, minify 19.562 byte (gzip 6,9 KB). Mỗi lệnh mất khoảng 0,4 giây và 115 MB RAM, tính cả bootstrap WP-CLI |
| `core.js` minify, kiểm bằng `node --check` (Node 22) trên PC | ✅ cả 2 file |
| Chuỗi nhạy cảm sau minify: template literal (XMP, CSS call-chat), regex, link app Shopee, `onclick` "Bạn đã xem" | ✅ còn nguyên. Chỉ mất 7 template literal nằm trong dòng `//-console.log`, tức comment, nên mất là đúng |
| Build lại khi không có thay đổi | ✅ cùng `ver` (`ver` tính theo nội dung) |
| Trình duyệt desktop, `?tt_device=desktop` | ✅ vi: 17 module chạy, `aff-redirect-mobile` bị bỏ qua, 0 lỗi. en: 8/8 |
| Trình duyệt mobile (Android, có UA-CH), `?tt_device=auto` | ✅ vi: 12 module chạy, 6 module desktop bị bỏ qua, `<html class="tt-mobile">`, 0 lỗi. en: 6 chạy, 2 bỏ qua |
| HTML chỉ có một script của theme | ✅ `tt-core/core.js?ver=...`, không còn `thongthai-*.js` |
| HTML desktop và HTML mobile (curl với 2 UA) | ✅ phần theme giống hệt. Còn lệch: `lss-3.js` của **ct-multi-sticky-sidebar** (đã biết, để sau) và jQuery chỉ nạp ở desktop (do một plugin của php81, production không có chỗ lệch này) |
| Module trong `uploads/tt-core/<theme>/modules/` (giả lập plugin) | ✅ file vừa ghi chưa tới 10 giây thì build **dừng** và báo. Hết 10 giây thì build có thêm module (19 module, `device=mobile`) |
| Module lỗi cú pháp | ✅ build dừng: `901-bad-demo.js: lỗi cú pháp dòng 2 cột 6`, `core.js` giữ nguyên bản cũ |
| `wp tt-core rollback` | ✅ quay về `core.prev.js`, dashboard báo cần build |
| Đường lui khi chưa có bản build | ✅ desktop nạp `thongthai-desktop.js`, mobile nạp `thongthai-mobile.js` (cách cũ). Generate xong thì chuyển sang `core.js` |
| Trang Giao diện → TT Core trên trình duyệt (chủ site đăng nhập, Claude bấm thử ở `/tttest/`) | ✅ **Kiểm tra cú pháp**: báo ver, không ghi file. ✅ **Lưu và Generate** sau khi tắt `call-chat` và đổi `iframe-touch` sang desktop: ra ver mới `85f37cbed7` (48.101 byte). Frontend mobile không còn 2 module đó, 0 lỗi. ✅ **Quay về bản trước**: về `6ed98f93d4`, trang báo "có thay đổi chưa build". ✅ Trả thiết lập về mặc định rồi Generate: đúng ver ban đầu `6ed98f93d4` |

**Lỗi tìm thấy và đã sửa:**
1. `wp tt-core` không đăng ký được, vì class nằm trong khối `if` được khai báo sau `add_command`.
2. Rollback không cập nhật kích thước và thời điểm build.
3. tar tạo từ Windows làm file mang quyền 666/777. Đã thêm bước `chmod` vào hướng dẫn deploy.
4. Sau khi rollback, dashboard hiện "0 byte", "gốc 0" và người thực hiện cũ. Ngoài ra, lúc sửa lỗi này, kết quả rollback mới làm phần báo cáo của `wp tt-core rollback` bị lỗi PHP fatal (chỉ ở bước in kết quả, rollback vẫn chạy xong). Đã sửa cả hai và test lại.

**Còn lại trên php81** (dọn khi test xong): site 3, site 4, thư mục theme `generatepress_child`, `generatepress_en`, và `wp-content/uploads/tt-core/`.

## 12. Còn mở
- Bản mobile cũ của Aff Redirect lấy `appUrls[urlIndex]` với `urlIndex` chạy từ 0 tới 3 (tính theo độ dài `webUrls`), trong khi `appUrls` chỉ có 2 link. Vì vậy 2 trên 4 lượt sẽ mở `undefined`. Module mới **giữ nguyên hành vi** này. Muốn sửa thì đổi thành `appUrls[urlIndex % appUrls.length]`, chủ site quyết định.
- `rightclick-cleanup` hiện chỉ chạy trên desktop, giống code cũ. Có thể cho chạy ở mọi thiết bị vì vô hại.
