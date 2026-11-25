# Cloudflare – token, KV và Worker

## API token
| Mục | Giá trị |
|---|---|
| Tên | `Cho phep python truy cap Workers KV` |
| Tạo ngày | 2026-11-25 (User settings → API tokens của mr.nctoan) |
| **Giá trị token** | `secrets.json` → `cloudflare.api_token` (gitignored, **không ghi ở đây**) |
| Account | `Mr.NcToan`, id `fac6a871fcd9e4c6213a66ba9d3f4688` |
| KV namespace | `cc831ca12e954d0b8a875a3453abacc7` (binding `THONGTHAI_CACHE` trong Worker) |
| Hết hạn | Không hết hạn |
| Lọc IP | **Chưa đặt** |
| Xác minh | `GET /user/tokens/verify` → `active` (2026-11-25, từ VPS) |

**Quyền đã cấp** (theo màn hình tạo token):
- Zone `diaocthongthai.com`: Workers Routes Write
- Account `Mr.NcToan`: Workers KV Storage Write, Workers Scripts Write, Account Settings Read, cùng 7 quyền khác (tổng 10 permission group)
- User: User Details Read, Memberships Read

> ⚠️ Token có **nhiều quyền hơn mức hệ thống cache cần** (cache chỉ cần *Workers KV Storage: Edit*). Với Workers Scripts Write và Workers Routes Write, ai có token có thể sửa hoặc gỡ Worker của site.
> Nên làm: (1) thêm **Client IP filtering** `45.77.32.177` cộng IP của PC; (2) khi rảnh thì tạo token mới chỉ có quyền KV và thu hồi token này.
> Token này đã từng được dán vào phiên chat với Claude ngày 2026-11-25.

Token cũ **không dùng được**: Global API Key trong `/var/hostvn/.cloudflare.conf` (`CF_Key`/`CF_Email`) bị API từ chối ngày 2026-11-25.

## Kết quả test KV REST API (2026-11-25, chạy trên VPS, Python 3.8 + requests 2.22)
| Bước | Thời gian | Kết quả |
|---|---|---|
| Verify token | 0.77s | active |
| GET key thật `/` | 0.78s | `'1'` |
| PUT / GET / DELETE 1 key | 0.39 / 0.06 / 0.40s | OK |
| Bulk PUT 1.000 key (1 request) | 0.63s | OK |
| List theo prefix (1.000 key) | 1.05s | đủ 1.000 |
| Bulk DELETE 1.000 key | 1.61s | HTTP 200, nhưng **3 key KHÔNG bị xóa** |

### Bài học cho code
1. **Bulk delete có thể thất bại một phần mà vẫn trả 200.** Sau khi xóa phải đọc `result.unsuccessful_keys` hoặc list lại theo prefix, rồi **thử xóa lại** các key còn sót. Áp dụng tương tự cho bulk put.
2. **KV đồng bộ chậm khoảng 60 giây**: `GET` một key vừa xóa vẫn trả giá trị cũ, còn `list` cập nhật nhanh hơn. Gỡ một trang thì khách Mỹ có thể còn thấy nó thêm khoảng 1 phút.
3. Một request ghi hoặc xóa tối đa **10.000 key**. Toàn bộ 60 nghìn key chỉ cần khoảng 7 request.
4. Key phải được URL-encode khi nằm trong path: `/values/%2Fabc%2F`.
5. Không cần wrangler hay Node trên server; chỉ cần `requests`.

### Trên PC: lỗi SSL do Avast
Python trên PC báo `CERTIFICATE_VERIFY_FAILED` vì **Avast Web/Mail Shield** chặn HTTPS và tự cấp chứng chỉ (`CN=Avast Web/Mail Shield Root`). Windows và wrangler tin chứng chỉ này, còn `certifi` của Python thì không.
Cách xử lý nếu cần gọi API từ PC (**không** tắt kiểm tra SSL):
- `pip install truststore`, rồi đặt `import truststore; truststore.inject_into_ssl()` ở đầu script để Python dùng kho chứng chỉ của Windows; hoặc
- thêm ngoại lệ `api.cloudflare.com` trong Avast → Web Shield.

## Worker `thongthai-cache-v7.js` (nguồn: `D:\Draft\2025-09-07 CF WORKER.JS\`)
- Chỉ khách **châu Mỹ (NA/SA)** mới được phục vụ từ S3 Chicago; khách ở nơi khác đi thẳng origin.
- `KV.get(normalizedPath)`: key = path viết thường, có `/` ở cuối (ví dụ `/en/map-of-oh-grandview/`). Có key thì fetch `us-ord-1.linodeobjects.com/cdn.diaocthongthai.com/_cache_html<path>index[-mobile]-https.html[_gzip]`; S3 trả 200 thì phục vụ, còn lại về origin.
- Bỏ qua (về origin): POST, `?s=`, `/search/`, wp-admin, `.php`, static, sitemap...
- `thongthai-cdn-v2.js`: phục vụ static cho `cdn.diaocthongthai.com`; châu Mỹ lấy từ bucket US, lỗi thì lấy bucket AP.

## Trạng thái KV (2026-11-25)
- 60.688 key: 56.823 dạng path (đúng) và **3.865 dạng URL đầy đủ (sai, do u2 ghi)**. Vì vậy 3.864 URL trong DB không được Worker phục vụ từ S3 (findings F4).
- 0 key path mồ côi (không có key nào trỏ tới URL đã bị xóa khỏi DB).
