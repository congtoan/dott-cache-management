// dott-cache-worker.js: Cloudflare Worker phục vụ HTML cache từ S3 theo châu lục (v2).
// Bản test cho php81 (28/11), chạy trên workers.dev. Dựa trên thongthai-cache-v7.js, khác ở:
//   - MỘT bản HTML cho mọi thiết bị: chỉ lấy index-https.html, không có bản mobile, không có _gzip
//     (Cloudflare tự nén response của Worker);
//   - nhiều đích S3: châu lục → bucket (TARGETS), châu lục không có trong TARGETS thì vào origin;
//   - bỏ qua khi khách có cookie đăng nhập / bình luận / mật khẩu bài viết (v7 chưa có);
//   - header X-Dott-Cache cho biết response đi đường nào (S3-<châu lục> | ORIGIN-<lý do>);
//   - test: header X-Dott-Continent chỉ có hiệu lực khi kèm X-Dott-Test-Key đúng secret TEST_KEY.
//     Nhờ vậy giả lập được khách châu Mỹ/Âu mà không cần ở đó.
//
// Biến môi trường (binding):
//   CACHE    KV namespace: key = path chuẩn hóa (viết thường, "/" cuối), có key = có bản HTML trên S3
//   ORIGIN   vd "https://php81.congtoan.net" (bản chạy thật trên route của site: để trống, dùng fetch(request))
//   TARGETS  JSON {"NA": "https://us-ord-1.linodeobjects.com/<bucket>/_cache_html", "SA": ..., "EU": ...}
//   TEST_KEY secret (chỉ bản test)

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    const toOrigin = (why) => originFetch(request, url, env, why);

    if (request.method !== 'GET' && request.method !== 'HEAD') return toOrigin('method');
    if (shouldBypass(url.pathname, url.search)) return toOrigin('bypass');
    if (url.search) return toOrigin('query'); // WP Rocket không cache URL có query string
    const cookie = request.headers.get('cookie') || '';
    if (/(wordpress_logged_in_|wp-postpass_|comment_author_|woocommerce_items_in_cart|edd_items_in_cart)/.test(cookie)) {
      return toOrigin('cookie');
    }

    // Chuyển về URL chuẩn (viết thường, "/" cuối) giống v7, để key KV và key S3 chỉ có một dạng
    const path = normalizePath(url.pathname);
    if (path !== url.pathname) return Response.redirect(url.origin + path, 301);

    let continent = request.cf?.continent || '';
    const forced = request.headers.get('x-dott-continent');
    if (forced && env.TEST_KEY && request.headers.get('x-dott-test-key') === env.TEST_KEY) continent = forced.toUpperCase();

    let targets = {};
    try { targets = JSON.parse(env.TARGETS || '{}'); } catch (e) {}
    const base = targets[continent];
    if (!base) return toOrigin('region-' + (continent || 'none'));

    if (!(await env.CACHE.get(path))) return toOrigin('kv-miss');

    try {
      const s3 = await fetch(base + path + 'index-https.html', { cf: { cacheTtl: 60, cacheEverything: true } });
      if (s3.status === 200) {
        return new Response(request.method === 'HEAD' ? null : s3.body, {
          status: 200,
          headers: {
            'Content-Type': 'text/html; charset=utf-8',
            'X-Dott-Cache': 'S3-' + continent,
            'Last-Modified': s3.headers.get('last-modified') || '',
          },
        });
      }
      return toOrigin('s3-' + s3.status);
    } catch (e) {
      return toOrigin('s3-error');
    }
  },
};

async function originFetch(request, url, env, why) {
  let resp;
  if (env.ORIGIN) {
    // Bản test trên workers.dev: chuyển request sang origin thật.
    const o = new URL(env.ORIGIN);
    o.pathname = url.pathname;
    o.search = url.search;
    resp = await fetch(new Request(o.toString(), request), { redirect: 'manual' });
  } else {
    resp = await fetch(request);
  }
  const r = new Response(resp.body, resp);
  r.headers.set('X-Dott-Cache', 'ORIGIN-' + why);
  return r;
}

function normalizePath(path) {
  if (path === '/') return '/';
  let p = path.toLowerCase();
  if (!p.includes('.') && !p.endsWith('/')) p += '/';
  return p;
}

// Giống v7: những đường dẫn luôn đi thẳng origin. Khác v7: wp-admin/wp-json/... khớp cả khi có tiền tố
// blog của multisite (/en/wp-admin/, /tttest/wp-json/), v7 chỉ khớp ở gốc.
function shouldBypass(path, search = '') {
  if (search && (search.includes('?s=') || search.includes('&s='))) return true;
  const patterns = [
    /^\/search\//i, /^(\/[^/]+)?\/(wp-admin|wp-includes|wp-json|wp-content)\//,
    /\.php(?:[\/\?]|$)/i, /^\/secretlogin(\?.*)?$/, /^\/sepay(?:[\/\?]|$)/i,
    /\.(jpg|jpeg|png|gif|webp|svg|ico|css|js|woff|woff2|ttf|eot|mp4|mp3|avi|mov|pdf|zip|doc|docx|json|xml|txt)$/i,
    /^\/sitemap/i, /^\/robots\.txt$/, /^\/favicon\.ico$/, /^\/trackback\//,
    /^\/(en\/)?checkout\//,
  ];
  return patterns.some((re) => re.test(path));
}
