// dott-cache-worker.js: Cloudflare Worker phục vụ HTML cache từ S3 theo châu lục (v2). Thay cho thongthai-cache-v7.js.
//
// Giữ nguyên từ v7:
//   - POST đi thẳng origin;
//   - redirect file tĩnh sang CDN (handleStaticRedirect) và redirect/410 theo trang (handlePageRedirect),
//     chỉ bật khi REDIRECTS = "1" (các quy tắc này viết riêng cho diaocthongthai.com);
//   - danh sách bỏ qua (shouldBypass);
//   - chuẩn hóa URL (viết thường, "/" cuối) bằng 301.
// Khác v7:
//   - MỘT bản HTML cho mọi thiết bị: chỉ lấy index-https.html (Cloudflare tự nén response của Worker);
//   - nhiều đích S3: châu lục → bucket (TARGETS); châu lục không có trong TARGETS thì vào origin;
//   - LEGACY_PREFIXES: path của blog CHƯA chuyển sang một bản HTML (vd ["/en/"] trong lúc chuyển từng blog)
//     được phục vụ như v7: chọn bản mobile/desktop theo User-Agent (chỉ lấy .html, Cloudflare tự nén);
//   - bỏ qua khi có cookie đăng nhập / bình luận / giỏ hàng (v7 chưa có);
//   - wp-admin, wp-json... khớp cả khi có tiền tố blog (/en/wp-admin/), v7 chỉ khớp ở gốc;
//   - header X-Dott-Cache cho biết đi đường nào (S3-<châu lục>[-legacy] | ORIGIN-<lý do>);
//   - test: X-Dott-Continent chỉ có hiệu lực khi kèm X-Dott-Test-Key đúng secret TEST_KEY.
//
// Binding:
//   CACHE           KV namespace: key = path chuẩn hóa; có key = có bản HTML trên S3
//   TARGETS         JSON {"NA": "https://us-ord-1.linodeobjects.com/<bucket>/_cache_html", "SA": ..., "EU": ...}
//   LEGACY_PREFIXES JSON ["/en/"] (tùy chọn)
//   REDIRECTS       "1" để bật quy tắc redirect của diaocthongthai.com
//   ORIGIN          chỉ bản test trên workers.dev (vd "https://php81.congtoan.net"); bản chạy trên route: để trống
//   TEST_KEY        secret (tùy chọn)

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    const toOrigin = (why) => originFetch(request, url, env, why);

    if (request.method === 'POST') return toOrigin('method');

    if (env.REDIRECTS === '1') {
      const cdn = handleStaticRedirect(url);
      if (cdn) return Response.redirect(cdn, 301);
      const pr = handlePageRedirect(url);
      if (pr) {
        if (pr.type === 410) return new Response('Gone', { status: 410 });
        return Response.redirect(pr.location, 301);
      }
    }

    if (request.method !== 'GET' && request.method !== 'HEAD') return toOrigin('method');
    if (shouldBypass(url.pathname, url.search)) return toOrigin('bypass');

    // Chuyển về URL chuẩn (viết thường, "/" cuối) giống v7, để key KV và key S3 chỉ có một dạng
    const path = normalizePath(url.pathname);
    if (path !== url.pathname) return Response.redirect(url.origin + path + url.search, 301);

    if (url.search) return toOrigin('query'); // WP Rocket không cache URL có query string
    const cookie = request.headers.get('cookie') || '';
    if (/(wordpress_logged_in_|wp-postpass_|comment_author_|woocommerce_items_in_cart|edd_items_in_cart)/.test(cookie)) {
      return toOrigin('cookie');
    }

    let continent = request.cf?.continent || '';
    const forced = request.headers.get('x-dott-continent');
    if (forced && env.TEST_KEY && request.headers.get('x-dott-test-key') === env.TEST_KEY) continent = forced.toUpperCase();

    const targets = parseJSON(env.TARGETS, {});
    const base = targets[continent];
    if (!base) return toOrigin('region-' + (continent || 'none'));

    if (!(await env.CACHE.get(path))) return toOrigin('kv-miss');

    // Blog chưa chuyển sang một bản HTML: chọn bản mobile/desktop theo User-Agent như v7. Chỉ lấy .html
    // (không lấy _gzip như v7): Cloudflare tự nén response, khỏi phụ thuộc header Content-Encoding trên S3.
    const legacy = parseJSON(env.LEGACY_PREFIXES, []).some((p) => path.startsWith(p));
    let file = 'index-https.html';
    if (legacy && /Android|iPhone|iPad|iPod|BlackBerry|IEMobile|Opera Mini/i.test(request.headers.get('user-agent') || '')) {
      file = 'index-mobile-https.html';
    }

    try {
      const s3 = await fetch(base + path + file, { cf: { cacheTtl: 60, cacheEverything: true } });
      if (s3.status === 200) {
        return new Response(request.method === 'HEAD' ? null : s3.body, {
          status: 200,
          headers: {
            'Content-Type': 'text/html; charset=utf-8',
            'X-Dott-Cache': 'S3-' + continent + (legacy ? (file.includes('mobile') ? '-legacy-mobile' : '-legacy') : ''),
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

function parseJSON(s, def) {
  try {
    return s ? JSON.parse(s) : def;
  } catch (e) {
    return def;
  }
}

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

// Những đường dẫn luôn đi thẳng origin (từ v7, thêm tiền tố blog cho wp-admin/wp-json/...).
function shouldBypass(path, search = '') {
  if (path === '/en/' && search.startsWith('?s=')) return true;
  if (search && (search.includes('?s=') || search.includes('&s='))) return true;
  const patterns = [
    /^\/search\//i,
    /^(\/[^/]+)?\/(wp-admin|wp-includes|wp-json|wp-content)\//,
    /\.php(?:[\/\?]|$)/i,
    /^\/admin-ajax\.php(?:[\/\?]|$)/i,
    /^\/secretlogin(\?.*)?$/,
    /^\/xmlrpc\.php/,
    /\.(jpg|jpeg|png|gif|webp|svg|ico)(\?.*)?$/i,
    /\.(css|js)(\?.*)?$/i,
    /\.(woff|woff2|ttf|eot)(\?.*)?$/i,
    /\.(mp4|mp3|avi|mov|pdf|zip|doc|docx)(\?.*)?$/i,
    /\.(json|xml|txt)(\?.*)?$/i,
    /^\/sepay(?:[\/\?]|$)/i,
    /^\/admin-post\.php(?:[\/\?]|$)/i,
    /^\/sitemap/i,
    /^\/robots\.txt$/,
    /^\/favicon\.ico$/,
    /^\/trackback\//,
    /^\/wp-cron\.php/,
    /^\/wp-signup\.php/,
    /^\/wp-activate\.php/,
    /^\/(en\/)?checkout\//,
  ];
  return patterns.some((re) => re.test(path));
}

// ---- Quy tắc riêng của diaocthongthai.com (chép nguyên từ v7, origin lấy theo host của request) ----

function handleStaticRedirect(url) {
  const path = url.pathname;
  // 1. /wp-content/uploads/(map|2014-2019|2020-2029)/*
  let m = path.match(/^\/wp-content\/uploads\/(map|201[4-9]|202[0-9])\/(.*)$/);
  if (m) return `https://cdn.diaocthongthai.com/${m[1]}/${m[2]}`;
  // 2. /map/*
  m = path.match(/^\/map\/(.*)$/);
  if (m) return `https://cdn.diaocthongthai.com/map/${m[1]}`;
  // 3. /docs/2019/*
  m = path.match(/^\/docs\/2019\/(.*)$/);
  if (m) return `https://cdn.diaocthongthai.com/2019/${m[1]}`;
  // 4. /docs/mau-hop-dong/*
  m = path.match(/^\/docs\/mau-hop-dong\/(.*)$/);
  if (m) return `https://cdn.diaocthongthai.com/docs/mau-hop-dong/${m[1]}`;
  // 5. /pano360/*.html
  m = path.match(/^\/(pano360\/.*\.html)$/i);
  if (m) return `https://cdn.diaocthongthai.com/${m[1]}`;
  return null;
}

function handlePageRedirect(url) {
  const path = url.pathname;
  const origin = url.origin;
  // 1. /ma-zip-code-*/link_ds_(huyen_thi|xa_phuong) → 410
  if (/^\/ma-zip-code-.*\/link_ds_(huyen_thi|xa_phuong)$/.test(path)) return { type: 410 };
  // 2. /ban-tin/tin-du-an/* → /ban-tin-du-an/*
  let m = path.match(/^\/ban-tin\/tin-du-an\/(.*)$/);
  if (m) return { type: 301, location: `${origin}/ban-tin-du-an/${m[1]}` };
  // 3. /feed/ (exact) → /
  if (path === '/feed/') return { type: 301, location: `${origin}/` };
  // 4. any .../feed/ → strip /feed/
  m = path.match(/^(.*)\/feed\/$/i);
  if (m) return { type: 301, location: `${origin}${m[1]}` };
  // 5. /tag/* → /tags/*
  m = path.match(/^\/tag\/(.*)$/);
  if (m) return { type: 301, location: `${origin}/tags/${m[1]}` };
  // 6. /en/(ban-do.*) → /$1
  m = path.match(/^\/en\/(ban-do.*)$/);
  if (m) return { type: 301, location: `${origin}/${m[1]}` };
  // 7. /en/page/* → /en/
  m = path.match(/^\/en\/page\/(.*)$/);
  if (m) return { type: 301, location: `${origin}/en/` };
  // 8. /blog/(.*?/)?([a-z].*) → /$2
  m = path.match(/^\/blog\/(?:.*?\/)?([a-z].*)$/);
  if (m) return { type: 301, location: `${origin}/${m[1]}` };
  // 9. /map-(.*) → /en/map-$1
  m = path.match(/^\/map-(.*)$/);
  if (m) return { type: 301, location: `${origin}/en/map-${m[1]}` };
  // 10. /<slug>,_... → /<slug>/
  m = path.match(/^\/([^/]+)\/,_.*$/);
  if (m) return { type: 301, location: `${origin}/${m[1]}/` };
  // 11. /<slug>_(...) → /<slug>/
  m = path.match(/^\/([^/]+)\/?_\(.+$/);
  if (m) return { type: 301, location: `${origin}/${m[1]}/` };
  return null;
}
