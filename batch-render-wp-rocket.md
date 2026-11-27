# Batch render HTML cache WordPress (WP Rocket) bằng PHP prefork

> Tài liệu giao việc cho Claude Code. Mục tiêu: **kiểm chứng và hiện thực hóa** một cơ chế render hàng loạt thay cho việc gửi HTTP request từng URL.
> Mọi con số trong tài liệu này là **giả định chưa đo**. Việc đầu tiên là đo, không phải viết code.

## 0. Quy tắc làm việc (đọc trước)

1. **Giai đoạn 0 và 1 chỉ đọc / chạy thử, không đụng production.** Không sửa file trong `wp-content/cache/`, không xóa cache thật, không chỉnh nginx/php-fpm/MariaDB đang chạy, không cài plugin.
2. Mọi script thử nghiệm đặt trong thư mục riêng (ví dụ `/root/batch-render-lab/`), ghi output ra thư mục tạm riêng, **không ghi đè file cache của WP Rocket** cho tới khi có lệnh rõ ràng.
3. VPS chính chỉ có ~2 GB RAM và ổ đĩa đã dùng ~88%. Chạy mọi thứ với `nice -n 19 ionice -c3`, tối đa 1-2 worker, kiểm tra `free -m` và `df -h` trước khi chạy đợt lớn.
4. Đo trước khi kết luận. Mỗi đề xuất ở dưới cần kèm số liệu thật. Nếu số liệu mâu thuẫn với giả định, **báo lại và đề xuất đổi hướng**, đừng cố làm cho khớp.
5. Ghi lại mọi phát hiện vào `FINDINGS.md` trong thư mục lab (lệnh đã chạy, kết quả, kết luận).
6. Về WP-CLI: **chỉ dùng để đo** (`wp profile ...`). Không dùng làm worker render vì hằng `WP_CLI` làm nhiều plugin đổi hành vi; worker là script PHP thuần `require wp-load.php`.

## 1. Bối cảnh

- Site: `diaocthongthai.com` (WordPress đa ngôn ngữ vi/en/es, khoảng 19.900 bài đăng, ~60k URL cần cache gồm archive, taxonomy, trang map...). Cần xác nhận đa ngôn ngữ là **multisite** hay **plugin** (Polylang/WPML).
- Cache HTML tạo bởi **WP Rocket**. Người dùng thật được phục vụ HTML cache có sẵn (trên server hoặc S3 qua Cloudflare Worker). Vấn đề **không** nằm ở request người dùng.
- Có hệ thống chạy ngầm để rotate/renew cache. Cách làm hiện tại: **gửi HTTP request** tới từng URL như truy cập thật, PHP render, WP Rocket ghi file. Mỗi URL phải làm **2 lần (desktop + mobile)**. Renew toàn site 60k trang rất lâu.
- Sau khi render, có bước upload lên S3 (script Python, đã có sẵn). Giữ nguyên bước này.
- VPS chính: Vultr, Ubuntu 20.04, 2 GB RAM, nginx + PHP 8.1 + MariaDB 10.5 + Redis. Có VPS phụ chưa dùng.
- Có sẵn tool SSH từ xa (`vps.py`) và skill `vps-diaocthongthai`; Claude Code có thể dùng nếu chạy từ PC.

## 2. Bài toán

Tổng thời gian renew ≈ `số URL × 2 × thời gian render / số worker song song`.
Mỗi request HTTP phải trả lại chi phí **bootstrap WordPress + nạp ~34 plugin** dù chi phí này gần như giống nhau mọi lần. Ý tưởng: bootstrap **một lần**, rồi render nhiều URL.

## 3. Hướng tiếp cận đề xuất: master bootstrap một lần + fork mỗi URL

### 3.1 Vì sao không dùng "vòng lặp trong một process"

WordPress không được thiết kế để render nhiều trang trong cùng process. State bị dính giữa các trang: `$wp_query`, `$post`, hook đã fire (`did_action`), enqueue registry, static/singleton của plugin... Reset thủ công luôn có nguy cơ sót, và lỗi rất khó thấy (trang này lẫn nội dung trang kia).

### 3.2 Vì sao không chỉ "restart sau N trang"

Restart worker sau N trang chỉ giới hạn *độ dài* của leak, không loại bỏ leak: trang 2..N vẫn thừa hưởng state của trang trước. Python/Go đứng ngoài không nhìn thấy và không reset được state bên trong PHP, nên chọn Python hay Go không giải quyết vấn đề này.

### 3.3 Cách đề xuất: prefork

```
php master (bootstrap WP 1 lần, KHÔNG gọi wp())
   ├─ đóng kết nối MySQL ($wpdb->close())
   ├─ pcntl_fork() → con 1: render URL A → ghi file → tự kết thúc
   ├─ pcntl_fork() → con 2: render URL B → ghi file → tự kết thúc
   └─ ... tối đa N con song song, có timeout, có báo cáo
```

- Mỗi con thừa hưởng trạng thái "sạch" ngay sau bootstrap (copy-on-write) → **không có state leak giữa các trang**, không phải tự reset.
- Plugin gọi `exit`/`die`/`wp_die` chỉ giết một con. Memory leak không tích lũy.
- Master có thể **prime dùng chung** trước khi fork (alloptions, terms, nav menu, widget...), mọi con thừa hưởng, các query dùng chung chỉ chạy một lần cho cả đợt.
- Chi phí đánh đổi: vài ms mỗi lần fork + kết nối DB/Redis mới cho mỗi con.
- **Phụ thuộc:** extension `pcntl` và `posix`. Nếu thiếu, fallback là restart sau N nhỏ (xem 3.6).

### 3.4 Phân vai

| Thành phần | Việc |
|---|---|
| **PHP master + con fork** | Bootstrap, render, chạy bước xử lý output của WP Rocket, ghi file nguyên tử (`rename()`), báo kết quả `{url, bytes, ms, status}` |
| **Python (orchestrator)** | Đọc hàng đợi, sắp xếp job theo template/term (để cache dùng chung luôn nóng), điều chỉnh số worker theo load, so hash với HTTP, retry, log, đánh dấu `needs_upload` |
| **Script upload S3 hiện có** | Giữ nguyên, đọc các file vừa ghi |

Python **không** nhận HTML qua pipe (100-500 KB/trang là thừa), PHP ghi thẳng ra đĩa.

### 3.5 Khung mã minh họa (chưa phải bản chạy được, cần kiểm chứng)

```php
<?php
// warm-master.php  --host=diaocthongthai.com --ua=desktop --workers=2
$opt    = getopt('', ['host:', 'ua:', 'workers:']);
$mobile = ($opt['ua'] ?? 'desktop') === 'mobile';

$_SERVER['HTTP_HOST']       = $opt['host'];
$_SERVER['HTTPS']           = 'on';
$_SERVER['SERVER_PORT']     = 443;
$_SERVER['REQUEST_METHOD']  = 'GET';
// URL không thể có cache: tránh advanced-cache.php của WP Rocket phục vụ file rồi exit() ngay lúc bootstrap
$_SERVER['REQUEST_URI']     = '/?djt-warm-boot=1';
$_SERVER['HTTP_USER_AGENT'] = $mobile ? MOBILE_UA : DESKTOP_UA;  // UA phải đặt TRƯỚC bootstrap
define('WP_USE_THEMES', true);
define('DJT_WARM', true);

require '/var/www/<site>/wp-load.php';   // bootstrap 1 lần. KHÔNG gọi wp() ở đây.

// (tuỳ chọn) prime dùng chung ở master: terms, nav menu, widget...

global $wpdb;
$wpdb->close();                          // master không giữ kết nối khi fork

// vòng điều phối: lấy job (Redis list / SQLite, kết nối riêng), fork tối đa N con,
// pcntl_waitpid(-1, WNOHANG), SIGKILL con quá timeout, ghi kết quả.

function render_child(array $job, bool $mobile): void {
    global $wpdb;
    $wpdb->db_connect();
    $wpdb->query('SET SESSION TRANSACTION READ ONLY');   // lưới an toàn: plugin lỡ ghi DB sẽ lỗi thay vì ghi thật
    // kết nối lại Redis object cache (tuỳ drop-in; thường wp_cache_init())

    $p = parse_url($job['url']);
    $_SERVER['REQUEST_URI']  = $p['path'] . (isset($p['query']) ? '?' . $p['query'] : '');
    $_SERVER['QUERY_STRING'] = $p['query'] ?? '';
    $_GET = []; parse_str($p['query'] ?? '', $_GET);
    $_POST = $_COOKIE = []; $_REQUEST = $_GET;

    // chặn đường thoát: redirect canonical → ghi nhận rồi kết thúc con
    add_filter('wp_redirect', function ($loc, $status) use ($job) {
        report($job, 'redirect', $loc, $status); kill_self();
    }, 0, 2);

    ob_start();
    wp();                                                 // parse_request, query, handle_404...
    require ABSPATH . WPINC . '/template-loader.php';     // template_redirect + render
    $html = ob_get_clean();

    if (is_404() || is_search() || !$html) { report($job, 'skip'); kill_self(); }
    $html = djt_post_process($html);                      // bước WP Rocket, xem mục 3.7
    djt_write_cache($job['url'], $html, $mobile);         // ghi file tạm + rename() nguyên tử
    report($job, 'ok');
    kill_self();   // posix_kill(getmypid(), SIGKILL): bỏ qua shutdown hook plugin và destructor mysqli
}
```

Lưu ý thiết kế:
- Kết thúc con bằng `SIGKILL` thay vì `exit`, để không chạy shutdown action của plugin (có thể ghi DB) và destructor mysqli (có thể gửi `COM_QUIT` làm hỏng socket nếu dùng chung). Con báo kết quả qua kênh riêng (file/Redis kết nối mới).
- **Không dùng `wp` (WP-CLI) cho worker**: WP-CLI định nghĩa hằng `WP_CLI`, nhiều plugin thấy hằng này sẽ bỏ qua phần frontend hoặc đổi hành vi. Dùng script PHP thuần `require wp-load.php`. Plugin kiểm tra `PHP_SAPI === 'cli'` vẫn có thể hành xử khác → cần so hash (mục 6).
- **Desktop và mobile chạy hai master riêng.** Nhiều theme gán `wp_is_mobile()` rất sớm (trước `init`), đổi UA sau bootstrap không có tác dụng.
- Nếu là **multisite**: mỗi site một master, **không** `switch_to_blog()` trong vòng lặp. Nếu là Polylang/WPML trên một site: xem lại chỗ xác định ngôn ngữ (thường ở `init`/`parse_request`), có thể cần một master mỗi ngôn ngữ.

### 3.6 Phương án dự phòng (nếu thiếu pcntl hoặc fork gặp vấn đề)

Một process PHP render N URL liên tiếp (N = 10-50) rồi tự thoát, Python spawn lại. Chấp nhận leak trong N trang, bù bằng sample check hash (mục 6). Kém an toàn hơn prefork, chỉ dùng khi bắt buộc.

### 3.7 Điểm khó nhất: tạo file cache giống hệt WP Rocket

WP Rocket không lưu HTML thô. Nó xử lý trong output buffer callback (minify/combine, lazyload, delay JS, RUCSS, CDN rewrite, chú thích cuối trang), rồi ghi file (kèm bản gzip, tên file khác nhau cho mobile/HTTPS...). Hai cách:

1. **Gọi lại callback buffer của chính WP Rocket** (class kiểu `Buffer\Cache`, hàm kiểu `maybe_init_process` / `maybe_process_buffer`). Độ giống cao nhất nhưng phụ thuộc cấu trúc nội bộ từng phiên bản.
2. **Tự áp `apply_filters('rocket_buffer', $html)`** rồi tự ghi file đúng đường dẫn và quy ước tên.

> Tên class/hàm/hook chính xác **chưa được xác minh**. Phải đọc source phiên bản đang cài:
> `grep -rn "rocket_buffer" wp-content/plugins/wp-rocket/inc | head` và `ls wp-content/plugins/wp-rocket/inc/classes/Buffer/`.

Cảnh báo:
- **RUCSS (Remove Unused CSS)**: nếu bật, mỗi URL chưa có dữ liệu có thể kích hoạt job gửi tới dịch vụ của WP Rocket. Render 60k URL có thể bắn hàng chục nghìn job. Phải xác định có bật không và hành vi ra sao **trước khi** chạy đợt lớn.
- Ghi file bằng `rename()` nguyên tử (ghi tạm rồi đổi tên), không xóa file cũ trước, để không có khoảng trống cache.

## 4. Các giả định cần kiểm chứng (đừng tin, hãy đo)

| # | Giả định | Cách kiểm tra |
|---|---|---|
| A1 | Bootstrap + nạp 34 plugin chiếm phần đáng kể (ước đoán 30-50%) thời gian một trang | `wp profile stage --all --url=...` trên 5-10 URL đại diện |
| A2 | Phần "riêng từng trang" (query + template) không chiếm phần lớn | Cũng từ `wp profile stage` / `wp profile hook` |
| A3 | Extension `pcntl`, `posix`, `redis` có sẵn ở PHP CLI | `php -m \| grep -E "pcntl\|posix\|redis"` (CLI và FPM có thể khác php.ini) |
| A4 | Render qua script PHP thuần cho HTML giống render qua HTTP | So hash, mục 6 |
| A5 | WP Rocket tách cache mobile/desktop | Đọc setting "Separate cache files for mobile devices" + xem thư mục cache |
| A6 | Có thể bỏ render mobile cho phần lớn URL | Xem theme có khác biệt HTML thật giữa mobile/desktop không (so hash hai bản) |
| A7 | Thời gian chờ MariaDB không chiếm phần lớn một trang | Query Monitor / slow query log / `wp profile` cho thấy tổng thời gian DB trên mỗi loại trang; xem `innodb_buffer_pool_size` và tỉ lệ đọc đĩa |
| A8 | Bước xử lý output của WP Rocket chiếm một phần đáng kể | Đo riêng thời gian callback buffer của WP Rocket (hook thời gian trước/sau `rocket_buffer`, hoặc render có/không có bước này trên cùng HTML) |
| A9 | 60k URL thuộc ít loại template | Phân loại toàn bộ URL theo post type, taxonomy, archive, ngôn ngữ, và đếm số lượng mỗi loại (nguồn: DB + sitemap) |

**Nếu A1 sai** (bootstrap chỉ chiếm ~20% hoặc ít hơn), toàn bộ hướng prefork chỉ lời tương ứng. Khi đó đổi ưu tiên sang: bỏ/gộp render mobile (nếu A6 đúng), "warm mode" tắt plugin không ảnh hưởng HTML, prime dùng chung, sắp xếp hàng đợi theo term. **Báo cáo kết quả này trước khi làm tiếp.**

## 5. Kế hoạch theo giai đoạn

### Giai đoạn 0: Khảo sát và đo (chỉ đọc, ~1-2 giờ)

- [ ] Thu thập môi trường: phiên bản WP, PHP CLI/FPM, WP Rocket (phiên bản + toàn bộ setting liên quan), danh sách plugin active, `php -m`, RAM/disk còn trống.
- [ ] Xác định đa ngôn ngữ: multisite hay Polylang/WPML? Đường dẫn cache và tên file của WP Rocket cho desktop/mobile/HTTPS.
- [ ] Xác định cách renew hiện tại: script nào gửi request, tuần tự hay song song, dùng preload của WP Rocket hay script riêng; đọc `cache_renew_from_db.py` và bước upload S3.
- [ ] Đo baseline: 100 URL đại diện (bài, category, tag, trang map, phân trang sâu; đủ 3 ngôn ngữ) qua HTTP, ghi `time_total` desktop và mobile.
- [ ] `wp profile stage` / `wp profile hook` cho 5-10 URL → tách **bootstrap / main query / template**. Đo thời gian `time wp option get siteurl` làm tham chiếu bootstrap.
- [ ] Ước tính tổng thời gian renew hiện tại và dung lượng cache (HTML + gzip) cho 60k × 2 URL.
- [ ] Kiểm tra RUCSS, Delay JS, các tính năng WP Rocket làm thay đổi output.
- [ ] Liệt kê plugin "nhạy cảm" với CLI/state: kiểm tra `WP_CLI`, `PHP_SAPI`, `static`, singleton, `$GLOBALS`, ghi DB ở frontend (đếm view, thống kê, bảo mật).
- [ ] Tách thời gian một trang thành 4 phần: **bootstrap / chờ DB / template+hook / bước xử lý WP Rocket** (A1, A2, A7, A8). Lấy số trung bình và p95 theo từng loại trang.
- [ ] Kiểm tra cấu hình MariaDB: `innodb_buffer_pool_size`, kích thước DB, slow query log, `EXPLAIN` các query nặng nhất, kích thước autoload trong `wp_options`.
- [ ] Phân loại 60k URL theo loại template/post type/taxonomy/ngôn ngữ và đếm số lượng (A9); nêu rõ phần riêng của từng loại gồm những gì và plugin nào chèn vào đó.
- [ ] Viết `FINDINGS.md`: tỉ lệ 4 phần ở trên, bảng phân loại template, khuyến nghị theo cổng quyết định ở mục 8 (Mức 1, Mức 2, hay tune DB trước).

**Cổng quyết định:** nếu tiết kiệm kỳ vọng từ bỏ bootstrap quá nhỏ, dừng và đề xuất hướng khác.

### Giai đoạn 1: Spike một URL (không ghi vào cache thật)

- [ ] Script PHP thuần: bootstrap bằng URL không cache được, fork một con, con render một URL qua `wp()` + template-loader, lưu HTML vào thư mục tạm.
- [ ] So với HTML lấy qua HTTP thật cho cùng URL (chuẩn hóa: bỏ nonce, timestamp, chú thích WP Rocket). Giải thích **mọi** khác biệt.
- [ ] Thử đủ loại URL: bài, page, category, tag, trang map, phân trang, trang chủ từng ngôn ngữ, URL redirect, URL 404.
- [ ] Thử đủ desktop và mobile (hai master).
- [ ] Đưa HTML qua bước xử lý của WP Rocket (3.7) và so với file WP Rocket đang lưu. Ghi lại tên class/hàm/hook thực tế tìm được.
- [ ] Xác nhận cơ chế an toàn: `SET SESSION TRANSACTION READ ONLY` chặn được ghi; plugin nào cố ghi DB (log lại, đây là danh sách plugin cần tắt trong warm mode).

**Tiêu chí qua cổng:** ≥95% URL mẫu khớp sau chuẩn hóa, mọi khác biệt còn lại có giải thích. Nếu không, báo cáo nguyên nhân (thường là plugin nhạy cảm CLI/UA).

### Giai đoạn 2: Prefork nhiều worker

- [ ] Master + pool N con (bắt đầu N=1-2), timeout cứng mỗi con (ví dụ 60s, SIGKILL), thu hoạch con bằng `pcntl_waitpid`.
- [ ] Mỗi con kết nối DB/Redis mới; kiểm tra không dùng chung socket với master.
- [ ] Ghi file nguyên tử vào **thư mục thử riêng**, chưa phải cache thật.
- [ ] Đo: trang/giây, p50/p95, CPU, RAM đỉnh, so với baseline HTTP. Chạy 1.000 URL.
- [ ] Chạy thử với `nice/ionice` và cgroup/systemd `MemoryMax` để bảo vệ site thật.

### Giai đoạn 3: Tối ưu

- [ ] **Prime ở master** trước khi fork: alloptions, terms, nav menu, widget, các query dùng chung.
- [ ] **Warm mode** (mu-plugin nhận header/hằng `DJT_WARM`): tắt plugin không ảnh hưởng HTML (bảo mật, analytics, đếm view), tắt cron/heartbeat/pingback; xác nhận bằng so hash.
- [ ] **Sắp xếp job** theo template/term để cache và buffer pool nóng.
- [ ] **Giảm render mobile** nếu A6 cho thấy HTML mobile ≈ desktop (hoặc khác biệt chuyển được sang CSS/JS).
- [ ] Chuyển từ "xóa rồi dựng lại" sang "render đè lên" để không có khoảng trống cache.

### Giai đoạn 4: Tích hợp pipeline

- [ ] Python orchestrator: đọc queue (SQLite/Redis), khóa chống trùng URL, dead-letter cho URL lỗi, điều chỉnh số worker theo load average.
- [ ] Nối với hook purge của WP Rocket (tên hook cần xác minh theo phiên bản) để đẩy URL cần render lại vào queue; tắt preload mặc định nếu thay thế. Đây vẫn là Nấc 1 (mục 9): giữ WP Rocket làm bước xử lý output và phục vụ file.
- [ ] Đánh dấu `needs_upload`, giữ script upload S3 hiện có.
- [ ] systemd unit, log, metric (hit, lỗi, fallback, thời gian).

### Giai đoạn 5: Chạy song song và cutover

- [ ] Chạy song song một thời gian: batch render cho tập con, HTTP cho phần còn lại, so hash định kỳ.
- [ ] Chỉ chuyển hẳn khi tỉ lệ khớp đạt ngưỡng đã thống nhất. Luôn giữ HTTP làm fallback; tự động quay về HTTP nếu tỉ lệ lệch vượt ngưỡng.

## 6. Kiểm chứng chất lượng (bắt buộc, không bỏ qua)

1. Chọn tập mẫu cố định ~300 URL đại diện, cộng thêm lấy mẫu ngẫu nhiên mỗi lần chạy lớn.
2. Với mỗi URL: HTML từ **HTTP thật** và HTML từ **worker mới**, desktop và mobile.
3. Chuẩn hóa (bỏ nonce, timestamp, chú thích WP Rocket, thứ tự attribute không quan trọng) rồi so hash. Lưu diff cho mọi URL lệch.
4. Phân loại nguyên nhân lệch: plugin nhạy CLI, UA/mobile, thứ tự hook, dữ liệu động hợp lệ.
5. Chỉ khi khớp mới tính tốc độ và so với baseline.

## 7. Rủi ro chính

| Rủi ro | Giảm thiểu |
|---|---|
| Plugin kiểm tra `PHP_SAPI`/`WP_CLI` nên đổi output | Script thuần (không WP-CLI), so hash; nếu cần, chạy master qua php-fpm/cgi thay vì cli |
| `wp_is_mobile()` gán sớm | Hai master riêng theo UA |
| `advanced-cache.php` phục vụ file cũ và `exit` lúc bootstrap | Bootstrap bằng URL không cache được |
| Con dùng chung socket DB/Redis với master | Đóng kết nối ở master trước khi fork, mở mới ở con |
| Plugin ghi DB khi render | `READ ONLY` session + warm mode tắt plugin đó |
| RUCSS bắn job hàng loạt | Xác nhận setting trước khi chạy đợt lớn |
| OOM (2 GB RAM) | 1-2 worker, `MemoryMax`, `nice/ionice`; hoặc chuyển sang máy phụ |
| Ổ đĩa đầy (đang 88%) | Tính dung lượng trước; dọn dẹp hoặc chuyển cache sang ổ khác |
| File cache sai định dạng làm hỏng trải nghiệm người dùng | Ghi thư mục thử trước, so với file WP Rocket thật, cutover từng phần |

## 8. Hướng thoát khỏi PHP (Mức 2: lai), chỉ cân nhắc sau khi có số đo

Giả thuyết của chủ dự án: PHP là bottleneck chính, nếu thoát được xử lý bằng PHP sẽ nhanh hơn nhiều. **Giả thuyết này chưa được chứng minh.** Thời gian một trang có thể nằm ở bốn chỗ:

| Chỗ tốn thời gian | Bỏ PHP có giúp không |
|---|---|
| Bootstrap + nạp ~34 plugin (CPU) | Có |
| Template + hook (CPU) | Có |
| Chờ MariaDB (DB ~2,6 GB, RAM 2 GB, buffer pool nhỏ) | **Không**, Go/Python vẫn chờ đúng các query đó |
| Bước xử lý output của WP Rocket (minify, lazyload, CDN rewrite... bằng regex PHP trên HTML) | Có, thường bị đánh giá thấp |

Ba mức, theo thứ tự thử:

1. **Mức 1: prefork (mục 3).** Vẫn PHP, bỏ chi phí bootstrap lặp lại. Rủi ro thấp. Làm trước.
2. **Mức 2: lai.** PHP chỉ dựng "khung", Go/Python điền phần riêng từng trang:
   - Render bằng PHP vài trang mẫu cho mỗi *loại template × ngôn ngữ × desktop/mobile*. Phần giống nhau giữa các trang (header, footer, sidebar, menu, CSS/JS) tách thành khung có placeholder.
   - Go/Python đọc DB (tiêu đề, nội dung, meta SEO, canonical, schema, breadcrumb, bài liên quan, phân trang) rồi điền vào khung và ghi file.
   - Bước xử lý kiểu WP Rocket chạy trên khung **một lần** thay vì trên ~120k trang.
   - Giá phải trả: phải tự cài lại mọi thứ plugin chèn vào phần riêng của trang (SEO plugin, schema, quảng cáo, related posts...). Sót một thứ là HTML khác bản PHP. Bắt buộc so hash, và giữ PHP prefork làm đường dự phòng cho URL lạ.
   - Chỉ đáng làm nếu ~90% URL thuộc vài loại template với phần riêng đơn giản (ví dụ bài map do engine Python sinh ra, archive, taxonomy).
3. **Mức 3: viết lại toàn bộ render bằng Go.** Phải tái hiện theme và mọi plugin ảnh hưởng HTML, bảo trì song song với WordPress. **Không khuyến nghị.**

**Cổng quyết định (điền sau Giai đoạn 0):**
- Chờ DB chiếm phần lớn → ưu tiên tune MariaDB (buffer pool, index postmeta/term_relationships), Mức 2 không đáng.
- Bootstrap chiếm phần lớn → Mức 1.
- Bước WP Rocket chiếm phần lớn → xem xét tự xử lý bước này ngoài PHP (ví dụ trên khung dùng chung).
- Template phân loại gọn (ít loại, phần riêng đơn giản) và CPU PHP chiếm phần lớn sau Mức 1 → thử Mức 2 bằng spike trên **một** loại template.

## 9. Lộ trình thay thế WP Rocket (ba nấc)

Cách tạo cache mới **không** đồng nghĩa với việc bỏ WP Rocket. WP Rocket làm ba việc khác nhau, và chỉ việc 1 bị thay thế bởi cơ chế mới:

| Vai trò của WP Rocket | Cơ chế mới có thay thế không |
|---|---|
| 1. Kích hoạt tạo cache (preload gửi request, hook purge và renew) | **Có**: master prefork + hàng đợi |
| 2. Xử lý output (minify/combine, lazyload, delay JS, RUCSS, CDN rewrite, chú thích cuối trang) | **Không**: worker phải gọi lại bước này (mục 3.7) |
| 3. Phục vụ và quản lý (`advanced-cache.php`, quy tắc nginx, purge từ wp-admin, tương thích plugin) | Không đổi nếu không muốn đổi |

Ba nấc, **làm lần lượt, chỉ sang nấc sau khi có số đo và được chủ dự án đồng ý**:

1. **Nấc 1: giữ WP Rocket, chỉ thay cách kích hoạt.** Worker gọi lại bước xử lý của WP Rocket và ghi file đúng quy ước của nó. Script upload S3 và Cloudflare Worker không phải sửa. Rủi ro thấp nhất. **Đây là phạm vi của Giai đoạn 1-5 ở mục 5.**
2. **Nấc 2: tách dần.** Khi cache chủ yếu do worker tạo, WP Rocket chỉ còn tối ưu output. Có thể tự xử lý bước này bên ngoài PHP (đặc biệt nếu đi Mức 2 ở mục 8: xử lý trên "khung" dùng chung một lần thay vì ~120k trang), rồi tắt dần từng tính năng của WP Rocket (mỗi lần một tính năng, so hash sau mỗi lần tắt).
3. **Nấc 3: bỏ hẳn, tự xây.** Phải tự có: bước tối ưu output; quy ước tên file và đường dẫn; hook purge khi đăng/sửa/xóa bài, đổi menu/widget/theme; quy tắc loại trừ (user đăng nhập, cookie, query string, URL không cache); tương thích plugin khác; phục vụ file (nginx/`advanced-cache.php` thay thế). RUCSS và delay JS là phần khó nhất vì WP Rocket dựa vào dịch vụ đám mây và nhiều năm xử lý ngoại lệ. **Không làm nếu chưa có lý do đo được** (ví dụ WP Rocket chiếm phần lớn bootstrap của request không cache, hoặc chi phí bước tối ưu output là nút thắt).

Lợi ích của việc bỏ hẳn: bớt một plugin nặng khỏi bootstrap của mọi request không cache (admin, AJAX, REST), hết phụ thuộc cấu trúc nội bộ từng phiên bản. Cái giá: tự bảo trì toàn bộ, trong khi vấn đề hiện tại chủ yếu là tốc độ render chứ không phải thiếu tính năng.

### Việc cần khảo sát (thêm vào Giai đoạn 0)

- [ ] **Quy ước file của WP Rocket**: đường dẫn thư mục (theo host/path), tên file (`index.html`, `index-https.html`, biến thể mobile, `.html_gzip`, `.webp`...), quyền file, có file `.htaccess`/quy tắc nginx đọc trực tiếp không. Ghi chính xác bằng cách liệt kê vài thư mục cache thật.
- [ ] **Ai đọc file cache**: nginx (quy tắc `try_files`/map), `advanced-cache.php`, script `cache_renew_from_db.py`, Cloudflare Worker, S3. Với mỗi bên, ghi rõ nó phụ thuộc vào đường dẫn/tên file/định dạng nào, để biết sửa quy ước thì hỏng chỗ nào.
- [ ] **Hook purge của WP Rocket**: liệt kê hook và hàm thực tế trong phiên bản đang cài (ví dụ các hook sau khi xóa cache bài/domain/term). Xác định điểm nối để đẩy URL vào hàng đợi mới mà không phá hành vi hiện có. **Tên chính xác chưa được xác minh, đọc source.**
- [ ] **Tính năng WP Rocket đang bật**: minify/combine CSS-JS, lazyload, delay JS, RUCSS, preload, CDN rewrite, tối ưu font, các quy tắc loại trừ tùy chỉnh. Với từng tính năng: có thể gọi lại qua filter `rocket_buffer` không, tốn bao nhiêu thời gian trên mỗi trang, có phụ thuộc dịch vụ ngoài không.
- [ ] **Mức đóng góp của WP Rocket vào bootstrap**: đo thời gian bootstrap có và không nạp WP Rocket (và các tính năng nặng của nó) để biết bỏ nó tiết kiệm bao nhiêu cho request không cache.
- [ ] **Danh sách tính năng Nấc 3 phải tự làm**: với mỗi tính năng ở trên, ước lượng công sức và rủi ro tự cài lại, kèm khuyến nghị giữ/bỏ.

### Tiêu chí chuyển nấc

- Nấc 1 → Nấc 2: file do worker tạo khớp file WP Rocket tạo (so hash, mục 6) và chạy ổn định qua nhiều đợt renew; đo được bước tối ưu output là phần chiếm thời gian đáng kể.
- Nấc 2 → Nấc 3: từng tính năng WP Rocket còn lại đã có thay thế được kiểm chứng bằng so hash; có đường lui (bật lại WP Rocket) trong vài phút.
- Ở mọi nấc: luôn giữ khả năng quay về cách cũ, và không chuyển nấc cho toàn site cùng lúc (làm theo tập con URL trước).

## 10. Hướng mở rộng (làm sau, chỉ khi có lý do đo được)

- **Render farm trên VPS phụ** (hoặc PC có RAM lớn): cần bản sao code WP, **MariaDB replica chỉ đọc + Redis cục bộ** trên chính máy đó (không truy vấn DB của VPS chính qua mạng, độ trễ sẽ làm chậm hơn). Render xong đẩy file về/ lên S3. Cần xử lý độ trễ replication sau khi đăng bài.
- **Giảm số trang phải render lại**: tách phần dùng chung (menu/footer) khỏi HTML (SSI hoặc ghép ở Cloudflare Worker), purge theo phụ thuộc thay vì render lại cả site. Thường là đòn bẩy lớn nhất nếu "update toàn site" xảy ra vì thay đổi dùng chung.
- Đổi orchestrator sang Go chỉ khi Python trở thành nghẽn (khó xảy ra với 60k URL).

## 11. Câu hỏi cần trả lời (điền vào FINDINGS.md)

1. Đa ngôn ngữ: multisite hay plugin nào?
2. Phiên bản WP Rocket; "Separate cache files for mobile" bật hay không; đường dẫn và tên file cache; RUCSS/Delay JS bật không?
3. `pcntl`, `posix`, `redis` có ở PHP CLI không?
4. Tỉ lệ bootstrap / main query / template trên tổng thời gian một trang?
5. Hệ thống renew hiện tại chạy thế nào (script nào, song song bao nhiêu, mất bao lâu cho 60k URL)?
6. HTML mobile có khác desktop thật sự không?
7. Plugin nào nhạy cảm với CLI/state hoặc ghi DB ở frontend?
8. Ngưỡng khớp hash chấp nhận được là bao nhiêu (đề xuất ban đầu ≥99% trước khi cutover)?
9. Thời gian một trang chia thế nào giữa bootstrap, chờ DB, template+hook và bước WP Rocket? PHP CPU thật sự chiếm bao nhiêu phần trăm?
10. 60k URL thuộc bao nhiêu loại template, mỗi loại bao nhiêu URL, và phần riêng của mỗi loại phức tạp đến đâu (Mức 2 có khả thi không)?
11. Những thành phần nào (nginx, `advanced-cache.php`, script renew/upload S3, Cloudflare Worker) phụ thuộc vào quy ước tên file/đường dẫn của WP Rocket? Hook purge thực tế là gì?
12. WP Rocket đóng góp bao nhiêu vào bootstrap và vào thời gian xử lý mỗi trang? Có lý do đo được để đi Nấc 2 hoặc 3 không, hay Nấc 1 là đủ?

## 12. Deliverable mong đợi sau giai đoạn 0-1

- `FINDINGS.md` với số liệu thật và khuyến nghị tiếp tục/đổi hướng.
- Script spike chạy được cho một URL, kèm báo cáo so hash.
- Danh sách plugin nhạy cảm và danh sách plugin có thể tắt trong warm mode.
- Ước tính thời gian renew toàn site theo từng phương án (hiện tại, prefork 1-2 worker, prefork + warm mode + bỏ mobile).
- Bảng chia thời gian một trang (bootstrap / DB / template / WP Rocket) và bảng phân loại 60k URL theo template, làm cơ sở quyết định Mức 1 hay Mức 2.
- Bản mô tả quy ước file cache của WP Rocket, danh sách thành phần phụ thuộc vào nó, danh sách hook purge, và bảng tính năng WP Rocket (gọi lại được không, tốn bao nhiêu, thay thế thế nào), làm cơ sở quyết định Nấc 1, 2 hay 3 (mục 9).
