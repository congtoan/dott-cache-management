// Temporary test worker: does Cloudflare compress a Worker-built response?
const S3 = 'https://us-ord-1.linodeobjects.com/cdn.diaocthongthai.com/_cache_html/en/about-us/';
export default {
  async fetch(request) {
    const p = new URL(request.url).pathname;
    if (p === '/plain') {            // like Worker fetching index-https.html and returning it uncompressed
      const r = await fetch(S3 + 'index-https.html');
      return new Response(r.body, { headers: { 'Content-Type': 'text/html; charset=utf-8' } });
    }
    if (p === '/gz') {               // current thongthai-cache-v7 behaviour with _gzip
      const r = await fetch(S3 + 'index-https.html_gzip');
      return new Response(r.body, { headers: { 'Content-Type': 'text/html; charset=utf-8', 'Content-Encoding': 'gzip', 'Vary': 'Accept-Encoding' } });
    }
    if (p === '/s3hdr') {            // what S3 itself returns for the .html object
      const r = await fetch(S3 + 'index-https.html');
      return new Response(JSON.stringify(Object.fromEntries(r.headers)), { headers: { 'Content-Type': 'application/json' } });
    }
    return new Response('ok');
  }
};
