# Kiến trúc pool: sự kiện tức thì + một hàng đợi duy nhất (chốt 28/11/2026)

> Thay cho: lịch `sync` 8 giờ/lần, `sync -full` hằng ngày, và ý tưởng "events đọc file mỗi phút" (đã bỏ).
> Chủ site chốt qua thảo luận 28/11. Mục 7 và 11 của [design-server-only.md](design-server-only.md) được thay bằng file này.

## 1. Nguyên tắc
1. **Site là nguồn sự thật** (cần cache những gì). **KV + S3 + đĩa là trạng thái thật** (đang phục vụ những gì). Không giữ bản sao danh sách URL để "cho khớp".
2. **WordPress chỉ báo "cái gì đổi"**, không tính path, không gọi Cloudflare/S3. Mọi quy tắc nằm ở Go.
3. **Chỉ một nơi ghi KV/S3/cache**: service `dott-cache serve` trên VPS phụ, một worker. Không có race giữa nhiều tiến trình.
4. Mọi việc (sự kiện, cập nhật hàng loạt, đối soát) đi qua **một pool có ưu tiên, lưu xuống file** (SQLite), để sự kiện không phải chờ việc hàng loạt và restart không mất việc.

## 2. Luồng
```
WordPress (mu-plugin dott-cache-events.php, mỏng)
  │  POST http://127.0.0.1:8790/event   (loopback VPS chính)
  ▼
SSH reverse tunnel (VPS phụ mở: ssh -N -R 127.0.0.1:8790:127.0.0.1:8790 main)
  ▼
dott-cache serve (VPS phụ, 127.0.0.1:8790, MemoryMax)
  ├─ inbox (SQLite)  ← sự kiện thô, trả 202 ngay
  ├─ resolver: gom 3 s → ssh main dott-prefork <site> --export-urls --ids=...  (trạng thái MỚI do site trả)
  │            so với trạng thái CŨ do plugin gửi → refresh / remove, áp bộ lọc config
  ├─ pool (SQLite): path → action, ưu tiên, generation
  └─ worker (1): lô → render (prefork / HTTP) → lấy HTML → upload S3 (nhiều luồng, mọi đích) → KV → xong
CLI: dott-cache submit ... (gửi việc vào pool), dott-cache pool status
```

## 3. mu-plugin mỏng (`wp/mu-plugins/dott-cache-events.php`)
- Hook: đăng/sửa/gỡ/xóa bài, bình luận (duyệt, bỏ duyệt, sửa, xóa), sửa/xóa term, WP Rocket "clear this cache".
- Trước khi sửa, chụp **trạng thái cũ dạng thô**: URL cũ (`get_permalink`), ID term cũ. Không chuẩn hóa path.
- Cuối request: **một** POST không chờ trả lời (`blocking=false`), body `{"host":..,"events":[{"b":1,"type":"post","id":123,"old_url":"..","old_terms":[5,9]}, {"b":1,"type":"comment","post":123}, {"b":1,"type":"term","tax":"category","id":5,"old_url":".."}]}`.
- URL và token đọc từ `<thư mục site>/dott-cache-hook.json` (ngoài `public_html`). Không có file thì không làm gì.
- Tunnel tắt: cổng loopback đóng, kết nối bị từ chối ngay, ghi `error_log`. Lưới an toàn: đối soát hằng ngày.

## 4. Trạng thái mới: `export-urls.php --ids`
- `--ids=1:p123,1:t:category:5,...`: với bài, trả path, trạng thái (còn publish hay không), path trang 1 và **số trang** của mọi term của bài, trang chủ; với term, trả path trang 1 và số trang.
- Một lần gọi cho cả lô đã gom; chỉ nạp WordPress của các blog có trong lô.
- Chuẩn hóa path chỉ có **một bản** (trong `export-urls.php`).

## 5. Quyết định refresh / remove (Go)
| Sự kiện | Việc |
|---|---|
| Bài còn publish | refresh bài; nếu `old_url` khác path mới thì remove path cũ; refresh trang 1 các term mới và cũ, trang chủ |
| Bài không còn (nháp, thùng rác, xóa) | remove `old_url`; refresh trang 1 các term cũ, trang chủ |
| Bình luận | refresh bài (nếu còn publish) |
| Term sửa / xóa | refresh trang 1 / remove `old_url` |
| Phân trang | với mỗi term bị ảnh hưởng có N trang: thêm `/page/N/` nếu KV chưa có; remove `/page/N+1/` nếu KV còn |

Mọi path đi qua bộ lọc `urls` của config và `write_blogs`.

## 6. Pool
- Bảng `pool`: mỗi path một dòng: `action` (refresh | remove), `prio`, `ord` (thứ tự trong cùng mức), `gen` (tăng mỗi khi dòng đổi), `state`, `tries`, `next_at`, `last_err`, `src`.
- Mức ưu tiên: 0 gỡ; 1 sự kiện: bài; 2 sự kiện: archive, trang chủ, phân trang; 3 thủ công, đối soát; 4 cập nhật hàng loạt (`submit -all`).
- **Gộp theo path:** việc mới thay hành động cũ và giữ mức ưu tiên cao hơn.
- **Generation:** worker ghi nhớ `gen` lúc lấy lô. Xong lô, chỉ PUT KV và xóa dòng nếu `gen` không đổi. Nếu path đã chuyển sang remove trong lúc render, không PUT KV; lượt remove sẽ dọn S3.
- **Lô:** việc mức 0–2 lấy tối đa 20 URL (≤ 4 thì HTTP, nhiều hơn thì prefork); mức 3–4 lấy 40 URL prefork. Sự kiện chờ tối đa một lô hàng loạt (khoảng 10–30 s).
- **Lỗi:** thử lại có giãn cách (1, 5, 30 phút), quá 5 lần thì ghi `failed` vào log và bỏ khỏi pool.
- Điều tiết theo load như cũ (`max_load` 1,5) trước mỗi lô.

## 7. Các tầng của worker
| Tầng | Số luồng | Ghi chú |
|---|---|---|
| Render | 1 (cho mọi site, vì chung 1 vCPU) | prefork qua `dott-prefork`, hoặc xóa file + HTTP |
| Lấy HTML từ VPS chính | `origin.workers` (2–4) | |
| Upload S3 | `workers` của từng đích, các đích song song | hàm `runUpload` có sẵn |
| KV | GET từng key (song song) rồi PUT key còn thiếu | gói miễn phí chỉ 1.000 lượt ghi/ngày, nên không PUT key đã có |

Upload của lô trước chạy gối đầu với render của lô sau.

## 8. Đối soát và cập nhật hàng loạt
- `dott-cache submit -all -blog /`: export toàn bộ, đẩy vào pool mức 4 theo thứ tự trang chủ → bài → archive trang 1 → phân trang.
- Đối soát hằng ngày (export + list KV + list S3: thêm cái thiếu, gỡ mồ côi, chặn khi > 2%) và gỡ `state.db`: **đợt sau**, khi pool đã chạy ổn.
- Trong lúc `serve` chạy, nó giữ khóa site: các lệnh ghi CLI cũ (`update`, `sync`, `replace`) bị từ chối, phải dùng `submit`.

## 8b. Làm mới theo tuổi cache: `submit -stale` (chốt 28/11, làm sau P8)
Mục tiêu: biết bản cache nào cũ (so với thời gian, hoặc so với nội dung), rồi làm mới từ cũ nhất tới mới nhất, mỗi lần một phần.
Có thể thay lượt `submit -all` hằng tháng (3–4 giờ mỗi blog) bằng lượt nhỏ hằng đêm.

**Nguồn thời gian: `cached@<unix>` ở cuối file, không dùng mtime.**
- WP Rocket ghi `cached@` đúng lúc render. `replace-cache.php` (`dott-cache replace`, đổi `?ver=`) ghi đè file mà không render lại:
  mtime thành mới nhưng nội dung bài vẫn cũ; `cached@` thì không đổi. Mỗi URL còn có 2 file (`.html`, `_gzip`), restore/copy
  cũng đổi mtime.
- Không dùng KV metadata (gói miễn phí 1.000 lượt ghi/ngày, tool không PUT key đã có). `Last-Modified` trên S3 là giờ upload,
  chỉ có ở 1 đích: hợp để đối chiếu S3 với đĩa, không hợp để đo tuổi bản render.

**Đo trên production 28/11 20:06** (63.636 `index-https.html` của vi + en + es, `nice -n 19 ionice -c3`, chỉ đọc, P6 đang chạy):

| | Lần 1 (ổ đĩa nguội) | Lần 2 |
|---|---|---|
| PHP: `RecursiveDirectoryIterator` qua mọi file, `fseek` đọc 300 byte cuối | 22,3 s | 9,6 s |
| Shell: `find -name index-https.html -exec tail -q -c 300 {} +` | 10,4 s (sau PHP, đã ấm một phần) | 4,3 s |

- Cả hai đọc đủ 63.636 file, file nào cũng có `cached@`. Load gần như không đổi (1,25 → 1,28).
- Bản cache cũ nhất: 2025-10-23 (chủ yếu là blog chưa từng được làm mới).

**Chọn PHP** (dù chậm hơn shell khoảng 2 lần): chênh 5 s không đáng kể với việc chạy hằng đêm; đúng mô hình `dott-prefork`
(chỉ chạy `prefork-render.php` bằng user của site, không nới quyền bộ chuyển lệnh root); in kết quả có cấu trúc dễ hơn
tách các dòng `==> file <==` của `tail`. Bản thật duyệt **thư mục** rồi mở thẳng `index-https.html` (như `--purge-cache`),
không lặp qua mọi file, nên dự kiến còn khoảng 5 s.

**Thiết kế:**
1. `prefork-render.php --scan-cache [--blog=/en/]`: mỗi dòng `S {"p":"/en/x/","c":<cached_at>,"m":<mtime>,"v":"<ver core.js>"}`,
   cuối `DONE n=…`. Chỉ đọc. Gọi qua `dott-prefork <domain> --scan-cache` (thêm vào danh sách tham số được phép; giữ khóa chung
   như `--export-urls` toàn bộ).
2. `export-urls.php` in thêm `post_modified_gmt` của bài và giờ bình luận được duyệt mới nhất (`m`), và với term: giờ bài mới
   nhất của term (để biết archive và phân trang có lỗi thời không).
3. `dott-cache submit -site <d> -stale -blog / [-older 30d] [-before <ngày>] [-ver-not <hash>] [-limit 2000] [-dry-run]`:
   - quét `cached@` + export, lọc theo một trong các điều kiện:
     - **bài:** `cached_at < max(post_modified, bình luận mới nhất)`, tức cache cũ hơn nội dung;
     - **archive, trang chủ, phân trang:** `cached_at <` giờ bài mới nhất của term (lấp chỗ hở hiện tại: trang 2..N chỉ được
       làm mới ở lượt hằng tháng);
     - **theo tuổi:** `-older 30d` hoặc `-before <ngày>` (vd mốc đổi menu/widget/theme: production bật
       `wp-rocket-no-cache-auto-purge`, nên các thay đổi này không sinh sự kiện);
     - **theo bản JS:** `-ver-not <hash>`, tức trang còn trỏ bản `core.js` cũ, chưa render lại sau lần build theme;
   - URL có trong export nhưng chưa có file cache: coi như cũ nhất;
   - đẩy vào pool **mức 4** với `ord = cached_at`, nên pool tự làm **từ cũ nhất tới mới nhất**; sự kiện (mức 1–2) vẫn chen trước;
   - `-limit` chặn số URL mỗi lần; `-dry-run` in thống kê (số URL theo từng điều kiện, phân bố tuổi theo tháng) và không gửi gì.
4. Lịch: `dott-cache-stale@<blog>.timer` hằng đêm (vd 02:00, `-limit 2000`, khoảng 15–20 phút prefork). Giữ `submit -all`
   hằng tháng làm lưới an toàn cho tới khi `-stale` chạy ổn.
5. Test trước trên php81 (`/tttest/`): tạo bài sửa sau khi render, `replace` để đổi mtime mà không đổi `cached@`, term có bài mới.

## 9. Bảo mật
- `serve` bind `127.0.0.1` (quy tắc CLAUDE.md), `MemoryMax`. Không mở cổng public.
- Tunnel dùng `GatewayPorts no` của VPS chính: cổng 8790 chỉ mở trên loopback VPS chính.
- Mọi process trên VPS chính gọi được cổng loopback, nên vẫn cần token: mỗi site một token (`config/secrets/hook-<site>.token` trên VPS phụ, `dott-cache-hook.json` trên site), so bằng `subtle.ConstantTimeCompare`; host trong body phải khớp token.
- Tunnel dùng **key riêng** (key root `main` có `no-port-forwarding`). Đã cài 28/11, dòng trong `/root/.ssh/authorized_keys` của VPS chính:
  `from="139.180.158.221",restrict,port-forwarding,permitlisten="127.0.0.1:8790",permitopen="127.0.0.1:1",command="/bin/false" ssh-ed25519 … dott-cache-tunnel`
  (`restrict` tắt mọi thứ, `port-forwarding` bật lại forward, `permitlisten` chỉ cho mở đúng cổng loopback đó, `permitopen` chỉ cho `-L` tới cổng 1 không có gì nghe (OpenSSH 8.2 không nhận `"none"`), `command="/bin/false"` cấm chạy lệnh). Trên VPS phụ: key `/root/.ssh/id_dott_tunnel`, alias `main-tunnel`.
- Lô HTTP (≤ 4 URL) không chờ load (đo 28/11: chờ 47 s cho một lần sửa bài là ngược mục tiêu); lô prefork vẫn điều tiết.

## 10. Thứ tự triển khai
1. php81: plugin mỏng, `--ids`, `serve` + pool + tunnel; test (sự kiện, đổi slug, gỡ, bình luận, phân trang, sự kiện giữa lúc `submit -all`, restart giữa chừng, tunnel tắt).
2. Production: P6–P7 như kế hoạch bằng CLI hiện tại (chưa có `serve`).
3. Production: bật `serve` + tunnel + plugin (thay P8).
4. Đối soát hằng ngày qua pool, gỡ `state.db` và `urls sync`.
