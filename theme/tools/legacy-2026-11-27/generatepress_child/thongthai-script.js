/* WIDGET INSERT */
// Block 40: Các chuyên mục: Thế giới, Việt Nam, Kiến Thức, Kinh nghiệm...
// Block 41: Xem nhiều
// Block 42: Bản đồ mới 2025 [Banner]
// Block 42: Adsense
// Block 4: TOC
// Block 43: Bạn đã xem (nằm dưới #more-info)
document.addEventListener('DOMContentLoaded', function() {
	// Get current date as YYYYMMDD
    const dailyVersion = new Date().toISOString().slice(0, 10).replace(/-/g, '');

    // Function to fetch and update block content
    function loadBlockContent(blockSelector, fileName) {
        const block = document.querySelector(blockSelector);
        if (!block) return;

        const url = `https://diaocthongthai.com/wp-content/themes/generatepress_child/widget/${fileName}?v=${dailyVersion}`;
        
        fetch(url)
            .then(response => response.text())
            .then(html => {
                block.innerHTML = html;
            })
            .catch(error => console.log(`Error loading ${fileName}`, error));
    }

    // Load content for block-40 and block-42
    loadBlockContent('#block-40', 'block-40.txt');
    loadBlockContent('#block-42', 'block-42.txt');
});

/* Block 41: xem nhiều */
document.addEventListener('DOMContentLoaded', function() {
    const block_top_hot = document.querySelector('#block-41') || document.querySelector('#block-50');
    if (!block_top_hot) return;

    // Lấy thời gian hiện tại (mili giây)
    const current_time_version = new Date().getTime();

    // Tính thời điểm gần nhất mà cron job chạy (cộng 5 phút = 300000 mili giây)
    const twoHoursInMs = 2 * 60 * 60 * 1000; // 2 giờ = 7200000 mili giây
    const fiveMinutesInMs = 5 * 60 * 1000; // 5 phút = 300000 mili giây

    // Tính giờ lẻ gần nhất (1:00, 3:00, ..., 23:00)
    const currentDate = new Date(current_time_version);
    const hours = currentDate.getHours();
    const nearestOddHour = hours % 2 === 0 ? hours - 1 : hours; // Nếu giờ chẵn, lùi về giờ lẻ trước đó
    const fallbackDate = new Date(currentDate);
    fallbackDate.setHours(nearestOddHour, 0, 0, 0); // Đặt về mốc giờ lẻ (1:00, 3:00, ...)
    const fallback_time = fallbackDate.getTime() + fiveMinutesInMs; // Cộng 5 phút

    // Tạo URL với query string ?v=fallback_time
    const url = `https://diaocthongthai.com/wp-content/themes/generatepress_child/widget/block-41.txt?v=${fallback_time}`;

    fetch(url)
        .then(response => response.text())
        .then(html => {
            block_top_hot.innerHTML = html;
        })
        .catch(error => console.log('Error loading block_top_hot', error));
});


/* Ajax Load More Button */
const loadmore_buttons = document.querySelectorAll('.load-more');
const dotContainerHTML = `
    <span class="dot-container">
        <span class="dot"></span>
        <span class="dot"></span>
        <span class="dot"></span>
    </span>
`;

loadmore_buttons.forEach(button => {
    button.innerHTML += dotContainerHTML;
});

/* Ajax Load More: viết lại bằng JS thuần (không cần jQuery).
   Giữ nguyên hành vi bản jQuery cũ:
   - đọc data-* như jQuery .data(): chuỗi số -> number, chuỗi JSON (data-args) -> object
   - gửi my_args theo định dạng jQuery.param, nên PHP vẫn nhận $_POST['my_args'] là mảng
   - HTML trả về được chèn và CHẠY các <script> bên trong (khối AdSense trong render_f3_posts) */
(function () {
    function ttReady(fn) {
        if (document.readyState !== 'loading') fn();
        else document.addEventListener('DOMContentLoaded', fn);
    }

    function ttData(el, name) {
        var v = el.getAttribute('data-' + name);
        if (v === null) return undefined;
        if (v === 'true') return true;
        if (v === 'false') return false;
        if (v === 'null') return null;
        if (v === +v + '') return +v;
        if (/^(?:\{[\w\W]*\}|\[[\w\W]*\])$/.test(v)) {
            try { return JSON.parse(v); } catch (e) { /* để nguyên chuỗi */ }
        }
        return v;
    }

    function ttBuildParams(prefix, obj, add) {
        if (Array.isArray(obj)) {
            obj.forEach(function (v, i) {
                if (/\[\]$/.test(prefix)) add(prefix, v);
                else ttBuildParams(prefix + '[' + (typeof v === 'object' && v != null ? i : '') + ']', v, add);
            });
        } else if (obj !== null && typeof obj === 'object') {
            Object.keys(obj).forEach(function (k) { ttBuildParams(prefix + '[' + k + ']', obj[k], add); });
        } else {
            add(prefix, obj);
        }
    }

    function ttParams(data) {
        var p = new URLSearchParams();
        Object.keys(data).forEach(function (k) {
            ttBuildParams(k, data[k], function (key, val) { p.append(key, val == null ? '' : val); });
        });
        return p;
    }

    function ttInsertHTML(el, html, replace) {
        var tpl = document.createElement('template');
        tpl.innerHTML = html;
        var scripts = Array.prototype.slice.call(tpl.content.querySelectorAll('script'));
        if (replace) el.textContent = '';
        el.appendChild(tpl.content);
        scripts.forEach(function (old) {
            var s = document.createElement('script');
            Array.prototype.forEach.call(old.attributes, function (a) { s.setAttribute(a.name, a.value); });
            s.text = old.textContent;
            old.parentNode.replaceChild(s, old);
        });
    }

    /* Listing cache trang 1: đọc file tĩnh uploads/tt-listing/<key>.html do server tạo sẵn
       (inc/listing-cache.php), không qua admin-ajax. Khóa tính từ data-args, PHẢI khớp
       tt_lc_canonical() + tt_lc_key() bên PHP. Lỗi hoặc chưa có file -> dùng ajax như cũ. */
    var TT_LC_BASE = window.location.origin + '/wp-content/uploads/tt-listing/';
    var TT_LC_V_SECONDS = 3600; // tham số ?v= đổi mỗi giờ để không dính bản cũ ở trình duyệt/CDN

    function ttPhpEmpty(v) {
        if (v === undefined || v === null || v === false || v === 0 || v === '0' || v === '') return true;
        if (Array.isArray(v)) return v.length === 0;
        if (typeof v === 'object') return Object.keys(v).length === 0;
        return false;
    }

    function ttLcCanonical(raw) {
        if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return null;
        if (!ttPhpEmpty(raw.tax_query) || !ttPhpEmpty(raw.orderby) || !ttPhpEmpty(raw.order)) return null;

        var types = raw.post_type === undefined || raw.post_type === null ? ['post'] : [].concat(raw.post_type);
        types = types.map(function (t) { return String(t).trim().toLowerCase(); })
            .filter(function (t, i, a) { return t.length > 0 && a.indexOf(t) === i; })
            .sort();
        if (!types.length) types = ['post'];

        function ints(v) {
            return [].concat(v).map(function (x) { return parseInt(x, 10); })
                .filter(function (n, i, a) { return n > 0 && a.indexOf(n) === i; })
                .sort(function (a, b) { return a - b; });
        }

        var ppp = raw.posts_per_page === undefined || raw.posts_per_page === null ? 6 : parseInt(raw.posts_per_page, 10);
        if (!(ppp >= 1 && ppp <= 30)) return null;

        return {
            v: 1,
            t: 'f3',
            pt: types,
            cat: raw.category__in === undefined || raw.category__in === null ? [] : ints(raw.category__in),
            tag: raw.tag__in === undefined || raw.tag__in === null ? [] : ints(raw.tag__in),
            st: ttPhpEmpty(raw.ignore_sticky_posts) ? 0 : 1,
            ppp: ppp
        };
    }

    // Trả về Promise<{html, maxPage} | false>
    function ttLcLoad(container) {
        if (!window.crypto || !window.crypto.subtle || !window.TextEncoder || !window.fetch) return Promise.resolve(false);
        var raw = ttData(container, 'args');
        var c = ttLcCanonical(raw);
        if (!c) return Promise.resolve(false);

        return window.crypto.subtle.digest('SHA-256', new TextEncoder().encode(JSON.stringify(c)))
            .then(function (buf) {
                var hex = Array.prototype.map.call(new Uint8Array(buf), function (b) {
                    return ('0' + b.toString(16)).slice(-2);
                }).join('');
                var key = (c.tag.length ? 'r-' : 'l-') + hex.slice(0, 12);
                var v = Math.floor(Date.now() / 1000 / TT_LC_V_SECONDS);
                return fetch(TT_LC_BASE + key + '.html?v=' + v, { credentials: 'omit' });
            })
            .then(function (res) {
                if (!res || !res.ok) return false;
                return res.text().then(function (text) {
                    var m = text.match(/^<!--tt-listing (\{.*?\})-->/);
                    if (!m) return false;
                    var meta = JSON.parse(m[1]);
                    var tpl = document.createElement('template');
                    tpl.innerHTML = text.slice(m[0].length);

                    // Bỏ bài đang xem, rồi cắt còn ppp bài
                    var exclude = ttPhpEmpty(raw.post__not_in) ? [] : [].concat(raw.post__not_in).map(function (x) { return parseInt(x, 10); });
                    var removed = 0;
                    Array.prototype.forEach.call(tpl.content.querySelectorAll('article[data-post-id]'), function (a) {
                        if (exclude.indexOf(parseInt(a.getAttribute('data-post-id'), 10)) !== -1) {
                            a.parentNode.removeChild(a);
                            removed++;
                        }
                    });
                    var arts = tpl.content.querySelectorAll('article[data-post-id]');
                    if (!arts.length) return false;
                    for (var i = c.ppp; i < arts.length; i++) arts[i].parentNode.removeChild(arts[i]);

                    var holder = document.createElement('div');
                    holder.appendChild(tpl.content);
                    var found = Math.max(0, (parseInt(meta.found, 10) || 0) - (removed ? 1 : 0));
                    return { html: holder.innerHTML, maxPage: Math.max(1, Math.ceil(found / c.ppp)) };
                });
            })
            .catch(function () { return false; });
    }

    ttReady(function () {
        var initialPostsLoaded = false;

        // Intersection Observer for view-based loading
        var observer = new IntersectionObserver(function (entries) {
            entries.forEach(function (entry) {
                if (entry.isIntersecting && !initialPostsLoaded) {
                    loadPosts([entry.target], true);
                    entry.target.classList.remove('not-loaded');
                    observer.unobserve(entry.target);
                }
            });
        }, {
            root: null,
            rootMargin: '50px',
            threshold: 0.1
        });

        // Observe all unloaded containers
        document.querySelectorAll('.thongthai-ajax-listing.not-loaded').forEach(function (el) {
            observer.observe(el);
        });

        // Scroll-based loading
        window.addEventListener('scroll', function () {
            var scrollPosition = window.pageYOffset || document.documentElement.scrollTop;

            if (!initialPostsLoaded && scrollPosition > 2000) {
                document.querySelectorAll('.thongthai-ajax-listing.not-loaded').forEach(function (el) {
                    loadPosts([el], true);
                    el.classList.remove('not-loaded');
                    observer.unobserve(el); // Remove observer once loaded
                });
                initialPostsLoaded = true;
            }
        }, { passive: true });

        // Trang 1: thử file cache tĩnh trước; không có thì ajax (kèm tt_prime để server tạo file).
        // Trang 2 trở đi (nút "Hiển thị thêm"): ajax như cũ.
        function loadPosts(containers, isInitialLoad) {
            if (!containers.length) return;
            if (!isInitialLoad || containers.length !== 1) {
                ajaxLoadPosts(containers, isInitialLoad, false);
                return;
            }
            var c = containers[0];
            ttLcLoad(c).then(function (r) {
                if (!r) {
                    ajaxLoadPosts(containers, true, true);
                    return;
                }
                ttInsertHTML(c, r.html, true);
                var instanceId = ttData(c, 'instance-id');
                var buttons = document.querySelectorAll('button.load-more[data-instance-id="' + instanceId + '"]');
                buttons.forEach(function (b) { b.classList.remove('hidden'); });
                c.setAttribute('data-max-page', r.maxPage);
                c.setAttribute('data-current-page', 2);
                if (2 > r.maxPage) {
                    buttons.forEach(function (b) { b.classList.add('hidden'); });
                }
            });
        }

        function ajaxLoadPosts(containers, isInitialLoad, prime) {
            if (!containers.length) return;
            var first = containers[0];
            var instanceId = ttData(first, 'instance-id');
            var my_args = ttData(first, 'args');
            var listing_type = ttData(first, 'listing-type');
            var max_page = ttData(first, 'max-page');
            var ajax_loadmore_paged = ttData(first, 'current-page');
            var buttons = document.querySelectorAll('button.load-more[data-instance-id="' + instanceId + '"]');
            var domain = window.location.origin;
            var payload = {
                action: 'loadmore',
                ajax_loadmore_paged: ajax_loadmore_paged,
                my_args: my_args,
                listing_type: listing_type,
                instance_id: instanceId
            };
            if (prime) payload.tt_prime = 1; // nhờ server tạo file cache trang 1

            fetch(domain + '/wp-admin/admin-ajax.php', {
                method: 'POST',
                credentials: 'same-origin',
                headers: { 'X-Requested-With': 'XMLHttpRequest' },
                body: ttParams(payload)
            })
                .then(function (res) {
                    if (!res.ok) throw new Error(res.status + ' ' + res.statusText);
                    return res.text();
                })
                .then(function (response) {
                    containers.forEach(function (c) { ttInsertHTML(c, response, isInitialLoad); });
                    if (isInitialLoad) {
                        buttons.forEach(function (b) { b.classList.remove('hidden'); });
                    }

                    ajax_loadmore_paged += 1;
                    containers.forEach(function (c) { c.setAttribute('data-current-page', ajax_loadmore_paged); });

                    if (ajax_loadmore_paged > max_page) {
                        buttons.forEach(function (b) { b.classList.add('hidden'); });
                    }
                })
                .catch(function (error) {
                    console.log('The following error occurred: ' + error.message, error);
                });
        }

        // Click handler for load more button
        document.addEventListener('click', function (event) {
            var button = event.target.closest('button.load-more');
            if (!button) return;
            event.preventDefault();
            var instanceId = ttData(button, 'instance-id');
            var containers = document.querySelectorAll('.thongthai-ajax-listing[data-instance-id="' + instanceId + '"]');
            loadPosts(Array.prototype.slice.call(containers), false);
        });
    });
})();

const a0_0x5bb312=a0_0x2ab5;(function(_0x5c543f,_0x5c84be){const _0x475b75=a0_0x2ab5,_0x3d36ad=_0x5c543f();while(!![]){try{const _0x3da1ad=parseInt(_0x475b75(0xa0))/0x1+-parseInt(_0x475b75(0xb4))/0x2*(parseInt(_0x475b75(0xa4))/0x3)+parseInt(_0x475b75(0xb6))/0x4*(-parseInt(_0x475b75(0xb8))/0x5)+-parseInt(_0x475b75(0xa9))/0x6+parseInt(_0x475b75(0xb2))/0x7+parseInt(_0x475b75(0xa2))/0x8+-parseInt(_0x475b75(0xa6))/0x9;if(_0x3da1ad===_0x5c84be)break;else _0x3d36ad['push'](_0x3d36ad['shift']());}catch(_0x2c4924){_0x3d36ad['push'](_0x3d36ad['shift']());}}}(a0_0x1fe7,0xdb1d0));function a0_0x1fe7(){const _0x4f7087=['toString','xa4Bjv7tzp','postMessage','dataset','contentWindow','usethiskeytoloadiframe','8000678aOiTxT','now','18902AUNtVW','iframe.thongthai_touch','4262120yesFke','DOMContentLoaded','5rGFTxN','1219875QEkAfE','forEach','4447032LXqQCB','iframeId','267JNEylD','load','606771zmHANv','ifr_','random','282792IdUlbc','addEventListener','substr'];a0_0x1fe7=function(){return _0x4f7087;};return a0_0x1fe7();}const a0_0x2a3335=(function(){let _0x5d68fa=!![];return function(_0x2c13db,_0x683d52){const _0x2e6cf6=_0x5d68fa?function(){if(_0x683d52){const _0x441810=_0x683d52['apply'](_0x2c13db,arguments);return _0x683d52=null,_0x441810;}}:function(){};return _0x5d68fa=![],_0x2e6cf6;};}()),a0_0x2a4e03=a0_0x2a3335(this,function(){return a0_0x2a4e03['toString']()['search']('(((.+)+)+)+$')['toString']()['constructor'](a0_0x2a4e03)['search']('(((.+)+)+)+$');});function a0_0x2ab5(_0x346063,_0x114902){const _0x5df694=a0_0x1fe7();return a0_0x2ab5=function(_0x2a4e03,_0x2a3335){_0x2a4e03=_0x2a4e03-0xa0;let _0x1fe77c=_0x5df694[_0x2a4e03];return _0x1fe77c;},a0_0x2ab5(_0x346063,_0x114902);}a0_0x2a4e03(),document[a0_0x5bb312(0xaa)](a0_0x5bb312(0xb7),function(){const _0x5c75eb=a0_0x5bb312,_0x557f25=document['querySelectorAll'](_0x5c75eb(0xb5));if(!_0x557f25['length'])return;const _0x3cba0f=_0x5c75eb(0xad),_0x51926b=0x3,_0x5f424e=0x1f4;function _0x31c7ca(){const _0x34f7bc=_0x5c75eb;return _0x34f7bc(0xa7)+Date[_0x34f7bc(0xb3)]()[_0x34f7bc(0xac)](0x24)+Math[_0x34f7bc(0xa8)]()[_0x34f7bc(0xac)](0x24)[_0x34f7bc(0xab)](0x2,0x2);}_0x557f25[_0x5c75eb(0xa1)](_0x3c8a8b=>{const _0x583095=_0x5c75eb,_0x1b6046=_0x31c7ca();_0x3c8a8b[_0x583095(0xaf)][_0x583095(0xa3)]=_0x1b6046;let _0x1500f6=0x0;const _0x12c1b3=()=>{const _0x3396a1=_0x583095;if(_0x1500f6>=_0x51926b)return;_0x1500f6++;try{_0x3c8a8b[_0x3396a1(0xb0)]?_0x3c8a8b[_0x3396a1(0xb0)][_0x3396a1(0xae)]({'type':_0x3cba0f,'key':_0x3396a1(0xb1),'iframeId':_0x1b6046},'*'):setTimeout(_0x12c1b3,_0x5f424e);}catch{setTimeout(_0x12c1b3,_0x5f424e);}};_0x3c8a8b[_0x583095(0xaa)](_0x583095(0xa5),_0x12c1b3);if(_0x3c8a8b[_0x583095(0xb0)])_0x12c1b3();});});

//==========================================================
/* Download Image Button */
document.addEventListener('DOMContentLoaded', function() {
    const _btn = document.getElementsByClassName('downloadButton');
    if(0===_btn.length)return;
    
    const _cfg = {
        buttonClass: 'downloadButton',
        attributes: {link:'data-download-link',filename:'data-filename'},
        scriptUrl: 'https://diaocthongthai.com/wp-content/script/piexif.js',
        xmp: {
            creator: 'Dia Oc Thong Thai',
            copyright: 'Copyright © 2019 DiaOcThongThai.Com', 
            credit: 'Map created by DiaOcThongThai.Com',
            webStatement: 'https://diaocthongthai.com/license/'
        },
        errorMessages: {
            noLink: 'Download link is missing',
            networkError: 'Failed to download file: Network error',
            invalidResponse: 'Failed to download file: Invalid response'
        }
    };

    let _scriptLoaded=0, _scriptLoading=0, _pendingDownloads=[];
    
    const getFilename = url => {
        try{return decodeURIComponent(url.split('/').pop()||'download')}
        catch(e){return'download'}
    };

    const showError = msg => {
        console.error('Error occurred:', msg);
        alert(msg);
    };
    
    const triggerDownload = (blob, filename) => {
        const link = document.createElement('a'),
        url = window.URL.createObjectURL(blob);
        link.href=url;
        link.download=filename;
        document.body.appendChild(link);
        link.click();
        setTimeout(()=>{
            document.body.removeChild(link);
            window.URL.revokeObjectURL(url);
        },100);
    };
    
    const loadScript = () => {
        if(_scriptLoaded)return 1;
        if(_scriptLoading)return new Promise(r=>_pendingDownloads.push(r));
        _scriptLoading=1;
        return new Promise(resolve=>{
            const script=document.createElement('script');
            script.src=_cfg.scriptUrl;
            script.onload=()=>{
                _scriptLoaded=1;
                _scriptLoading=0;
                _pendingDownloads.forEach(cb=>cb(1));
                _pendingDownloads=[];
                resolve(1);
            };
            script.onerror=()=>{
                _scriptLoading=0;
                _pendingDownloads.forEach(cb=>cb(0));
                _pendingDownloads=[];
                resolve(0);
            };
            document.head.appendChild(script);
        });
    };
    
    const createXMP = title => {
        const url = window.location.href;
        return `<?xpacket begin='﻿' id='W5M0MpCehiHzreSzNTczkc9d'?>
<x:xmpmeta xmlns:x='adobe:ns:meta/' x:xmptk='Image::ExifTool 12.70'>
<rdf:RDF xmlns:rdf='http://www.w3.org/1999/02/22-rdf-syntax-ns#'>
 <rdf:Description rdf:about='' xmlns:dc='http://purl.org/dc/elements/1.1/'>
  <dc:creator><rdf:Seq><rdf:li>${_cfg.xmp.creator}</rdf:li></rdf:Seq></dc:creator>
  <dc:rights><rdf:Alt><rdf:li xml:lang='x-default'>${_cfg.xmp.copyright}</rdf:li></rdf:Alt></dc:rights>
  <dc:title><rdf:Alt><rdf:li xml:lang='x-default'>${title}</rdf:li></rdf:Alt></dc:title>
 </rdf:Description>
 <rdf:Description rdf:about='' xmlns:photoshop='http://ns.adobe.com/photoshop/1.0/'>
  <photoshop:Credit>${_cfg.xmp.credit}</photoshop:Credit>
 </rdf:Description>
 <rdf:Description rdf:about='' xmlns:plus='http://ns.useplus.org/ldf/xmp/1.0/'>
  <plus:Licensor><rdf:Seq><rdf:li rdf:parseType='Resource'>
   <plus:LicensorURL>${url}</plus:LicensorURL>
  </rdf:li></rdf:Seq></plus:Licensor>
 </rdf:Description>
 <rdf:Description rdf:about='' xmlns:xmpRights='http://ns.adobe.com/xap/1.0/rights/'>
  <xmpRights:WebStatement>${_cfg.xmp.webStatement}</xmpRights:WebStatement>
 </rdf:Description>
</rdf:RDF></x:xmpmeta><?xpacket end='w'?>`.padEnd(2048,' ');
    };
    
    async function addXMP(blob, title) {
        const arrBuf = await blob.arrayBuffer(),
        encoder = new TextEncoder(),
        xmpData = createXMP(title),
        xmpBuf = encoder.encode(xmpData),
        xmpId = 'http://ns.adobe.com/xap/1.0/\x00',
        idBuf = encoder.encode(xmpId),
        segLen = 2 + idBuf.length + xmpBuf.length,
        view = new Uint8Array(arrBuf),
        newBuf = new Uint8Array(view.length + segLen + 2);
        
        newBuf.set(view.slice(0,2),0);
        let pos=2;
        newBuf[pos++]=0xFF;
        newBuf[pos++]=0xE1;
        newBuf[pos++]=(segLen>>8)&0xFF;
        newBuf[pos++]=segLen&0xFF;
        newBuf.set(idBuf,pos);
        pos+=idBuf.length;
        newBuf.set(xmpBuf,pos);
        pos+=xmpBuf.length;
        newBuf.set(view.slice(2),pos);
        
        return new Blob([newBuf],{type:blob.type});
    }
    
    async function readXMP(blob) {
        try {
            const arrBuf = await blob.arrayBuffer(),
            view = new Uint8Array(arrBuf);
            for(let i=0; i<view.length-1; i++) {
                if(view[i]===0xFF && view[i+1]===0xE1) {
                    const len=(view[i+2]<<8)|view[i+3],
                    nsStart=i+4,
                    nsEnd=nsStart+29,
                    ns=new TextDecoder().decode(view.slice(nsStart,nsEnd));
                    if(ns.startsWith('http://ns.adobe.com/xap/1.0/'))
                        return new TextDecoder().decode(view.slice(nsEnd,i+2+len));
                }
            }
            return null;
        } catch(e) {
            return null;
        }
    }
    
    const handleDownload = async btn => {
        const downloadLink = btn.getAttribute(_cfg.attributes.link);
        if(!downloadLink) return;
        
        const title = (btn.getAttribute('title')||document.title)
            .replace(/\b(download|Download)\b/g,'').trim(),
        origText = btn.innerHTML,
        origDisabled = btn.disabled;
        
        btn.disabled=1;
        btn.innerHTML='Downloading...';
        
        try {
            await loadScript();
            const response = await fetch(downloadLink);
            if(!response.ok) throw new Error(response.status);
            
            let blob = await response.blob();
            if(blob.type.startsWith('image/') && 
               !['image/svg+xml','image/gif','image/webp'].includes(blob.type)) {
                await readXMP(blob);
                blob = await addXMP(blob,title);
            }
            
            const customFilename = btn.getAttribute(_cfg.attributes.filename);
            const filename = (customFilename && customFilename.trim()) || 
                           getFilename(downloadLink);
            
            triggerDownload(blob,filename);
        } catch(e) {
            showError(_cfg.errorMessages.networkError);
            console.error('Download error:', e);
        } finally {
            btn.disabled=origDisabled;
            btn.innerHTML=origText;
        }
    };

    document.body.addEventListener('click',e=>{
        const btn=e.target.closest('.'+_cfg.buttonClass);
        if(btn){
            const downloadLink=btn.getAttribute(_cfg.attributes.link);
            if(!downloadLink)return;
            e.preventDefault();
            handleDownload(btn);
        }
    });
});

//==========================================================
/* xemthem expand button */
if (window.innerWidth < 480 && document.querySelector('.xemthem')) {
    document.addEventListener('DOMContentLoaded', () => {
        const c = document.querySelectorAll('.xemthem');
        if (!c.length) return;

        const downIcon = `<svg class="icon-chevron-down" width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M6 16L12 22L18 16" stroke="#000" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`;
        const upIcon = `<svg class="icon-chevron-up" width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M6 22L12 16L18 22" stroke="#000" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`;

        c.forEach(x => {
            if (!x.querySelector('.fade-out')) {
                const f = document.createElement('div');
                f.className = 'fade-out';
                x.appendChild(f);
            }
            if (!x.querySelector('.expand-button')) {
                const b = document.createElement('a');
                b.className = 'expand-button';
                b.innerHTML = `Mở rộng ${downIcon}`;
                x.appendChild(b);
                b.onclick = () => {
                    x.classList.toggle('expanded');
                    b.innerHTML = x.classList.contains('expanded') ? `Thu gọn ${upIcon}` : `Mở rộng ${downIcon}`;
                };
            }
        });
    });
}
//==========================================================
/* Function to create and manage sticky header */
function initStickyHeader(tableContainer) {
    // Find table with sticky-header class inside the container
    const originalTable = tableContainer.querySelector('table.sticky-header');
    if (!originalTable) return;

	const wpAdminBar = document.getElementById('wpadminbar');
	const topOffset = wpAdminBar ? '32px' : '0px';
	
    // Create sticky header container
    const stickyHeaderContainer = document.createElement('div');
    stickyHeaderContainer.className = 'sticky-header-container';
    stickyHeaderContainer.style.cssText = `
        position: fixed;
        top: ${topOffset};
        left: 0;
        right: 0;
        background: white;
        z-index: 1000;
        display: none;
        overflow-x: hidden;
    `;


    // Extract only <thead> from the original table
    const stickyTable = document.createElement('table');
	stickyTable.className = originalTable.className; // Copy all classes

    stickyTable.style.width = window.getComputedStyle(originalTable).width; // Match width
    const theadClone = originalTable.querySelector('thead').cloneNode(true);
    stickyTable.appendChild(theadClone);
	
	stickyHeaderContainer.appendChild(stickyTable);
    
    // Insert sticky header right after the original table
    originalTable.after(stickyHeaderContainer);

    // Rest of the code remains the same...
    function syncColumnWidths() {
        const originalHeaders = originalTable.querySelectorAll('th');
        const stickyHeaders = stickyTable.querySelectorAll('th');
        
        originalHeaders.forEach((header, index) => {
            if (stickyHeaders[index]) {
                const width = window.getComputedStyle(header).width;
                stickyHeaders[index].style.width = width;
            }
        });
        
        // Match table width
        stickyTable.style.width = window.getComputedStyle(originalTable).width;
    }

    function updateStickyHeader() {
        const tableRect = originalTable.getBoundingClientRect();
        const originalHeaderRect = originalTable.querySelector('thead').getBoundingClientRect();
        const containerRect = tableContainer.getBoundingClientRect();

        if (originalHeaderRect.top < 0 && tableRect.bottom > 0) {
            stickyHeaderContainer.style.display = 'block';
            stickyHeaderContainer.style.left = `${containerRect.left}px`;
            stickyHeaderContainer.style.width = `${containerRect.width}px`;
            
            // Sync horizontal scroll position
            stickyHeaderContainer.scrollLeft = tableContainer.scrollLeft;
        } else {
            stickyHeaderContainer.style.display = 'none';
        }
    }

    // Initial setup
    syncColumnWidths();

    // Event listeners
    window.addEventListener('resize', syncColumnWidths);
    window.addEventListener('scroll', updateStickyHeader);
    tableContainer.addEventListener('scroll', (e) => {
        if (stickyHeaderContainer.style.display === 'block') {
            stickyHeaderContainer.scrollLeft = e.target.scrollLeft;
        }
    });

    // Observe table width changes
    const observer = new ResizeObserver(syncColumnWidths);
    observer.observe(originalTable);
}

// Initialize for all tables with the specified container class
document.querySelectorAll('.thongthai-scrollable').forEach(container => {
    initStickyHeader(container);
});

//==========================================================
/* Filter Table */
// Check if element exists first to avoid unnecessary processing
if (document.querySelector('.table-filter')) {
    document.querySelectorAll('.table-filter').forEach(input => {
        // Debounce the input to prevent excessive processing during fast typing
        let timeout;
        input.addEventListener('keyup', function() {
            clearTimeout(timeout);
            timeout = setTimeout(() => {
                const searchText = this.value.toLowerCase();
                // Check if next element exists and is a table
                const table = this.nextElementSibling;
                if (!table || table.tagName !== 'TABLE') return;
                
                const rows = table.querySelectorAll('tbody tr');
                if (!rows.length) return;
                
                rows.forEach(row => {
                    const rowText = row.innerText.toLowerCase();
                    row.style.display = rowText.includes(searchText) ? '' : 'none';
                });
            }, 800); // 800ms delay
        });
    });
}

//==========================================================
/* Sortable Table */
if (document.querySelector('.sortable-table')) {
    const parsers = {
        number: v => parseFloat(v.replace(/[^0-9.-]/g, '')),
        date: v => new Date(v).getTime(),
        text: v => v.toLowerCase(),
        currency: v => parseFloat(v.replace(/[^0-9.-]/g, ''))
    };

    function compareValues(a, b, dataType) {
        const [type, modifier] = dataType.split(':');
        const parse = parsers[modifier] || parsers[type] || (v => v);
        a = parse(a);
        b = parse(b);
        return typeof a === 'number' && typeof b === 'number' ? a - b : String(a).localeCompare(String(b));
    }

    function sortTable(header) {
	
        // Check if the header is in the sticky header
        const stickyContainer = header.closest('.sticky-header-container');
        if (stickyContainer) {
            // Get the original table (previous sibling of sticky container)
            const originalTable = stickyContainer.previousElementSibling;
            // Find the index of the clicked th in the sticky header
            const thIndex = Array.from(header.parentElement.children).indexOf(header);
            // Get the corresponding th in the original table
            const originalHeaders = originalTable.querySelectorAll('th');
            if (thIndex >= 0 && thIndex < originalHeaders.length) {
                header = originalHeaders[thIndex];
            } else {
                return; // Invalid index, exit
            }
        }	
	
        const table = header.closest('table');
		// Safety check (existing code)
		if (!table.tBodies.length) return;
		
        const columnIndex = header.cellIndex;
        const isAsc = header.classList.contains('sort-asc');
        const isDesc = header.classList.contains('sort-desc');

		// Clear classes in BOTH tables
		const clearSortClasses = (headers) => {
			headers.forEach(th => th.classList.remove('sort-asc', 'sort-desc'));
		};

		// 1. Clear original headers
		const originalHeaders = table.querySelectorAll('th');
		clearSortClasses(originalHeaders);

		// 2. Clear sticky headers if they exist
		const stickyHeaderContainer = table.nextElementSibling;
		if (stickyHeaderContainer?.classList?.contains('sticky-header-container')) {
			const stickyHeaders = stickyHeaderContainer.querySelectorAll('th');
			clearSortClasses(stickyHeaders);
		}

        // Sort rows
        const tbody = table.tBodies[0];
        const rows = Array.from(tbody.rows);

        if (isAsc) {
            // Sort descending
            rows.sort((a, b) => compareValues(
                b.cells[columnIndex].dataset.sortValue,
                a.cells[columnIndex].dataset.sortValue,
                header.dataset.sort
            ));
            header.classList.add('sort-desc');
        } else if (isDesc) {
            // Restore initial order
            tbody.innerHTML = table.dataset.initialOrder;
        } else {
            // Sort ascending
            rows.sort((a, b) => compareValues(
                a.cells[columnIndex].dataset.sortValue,
                b.cells[columnIndex].dataset.sortValue,
                header.dataset.sort
            ));
            header.classList.add('sort-asc');
        }

        // Append sorted rows if not restoring initial order
        if (!isDesc) {
            tbody.append(...rows);
        }

        // Update hover title
        updateHoverTitle(header);
		
		// Update BOTH headers		
		const updateStickyHeader = () => {
			if (!stickyHeaderContainer) return;
			
			const stickyTable = stickyHeaderContainer.querySelector('table');
			const stickyHeader = stickyTable.querySelectorAll('th')[columnIndex];
			
			if (stickyHeader) {
				stickyHeader.classList.add(...header.classList);
				stickyHeader.title = header.title;
			}
		};

		updateStickyHeader();	
		
    }

    function updateHoverTitle(header) {
        if (header.classList.contains('sort-asc')) {
            header.title = 'Sort descending';
        } else if (header.classList.contains('sort-desc')) {
            header.title = 'Sort initial';
        } else {
            header.title = 'Sort ascending';
        }
    }

    // Save initial order of rows for "Sort initial" functionality
	document.querySelectorAll('.sortable-table').forEach(table => {
		// Only process tables that have a tbody
		if (table.tBodies.length > 0) {
			table.dataset.initialOrder = table.tBodies[0].innerHTML;
		}
	});

    // Use event delegation for better performance
    document.addEventListener('click', e => {
        const header = e.target.closest('.sortable-table th[data-sort]');
        if (header) sortTable(header);
    }, false);

    // Update hover titles on page load
    document.querySelectorAll('.sortable-table th[data-sort]').forEach(header => {
        updateHoverTitle(header);
    });
}

//==========================================================
/* Scroll Bar */
(function() {
    let globalListenersInitialized = false;
    const scrollbars = new Map();
    let observer = null;

    // Cleanup function to remove all scrollbars and event listeners
    function cleanup() {
        scrollbars.forEach(data => {
            data.scrollbar.remove();
            removeScrollbarListeners(data);
        });
        scrollbars.clear();
        
        if (globalListenersInitialized) {
            document.removeEventListener('mousemove', onDocumentMouseMove);
            document.removeEventListener('mouseup', onDocumentMouseUp);
            window.removeEventListener('resize', onWindowResize);
            document.removeEventListener('scroll', updateAllScrollbarVisibility);
            globalListenersInitialized = false;
        }
        
        if (observer) {
            observer.disconnect();
            observer = null;
        }
    }

    // Initialize global event listeners only when needed
    function initGlobalListeners() {
        if (globalListenersInitialized) return;
        
        document.addEventListener('mousemove', onDocumentMouseMove);
        document.addEventListener('mouseup', onDocumentMouseUp);
        window.addEventListener('resize', onWindowResize);
        document.addEventListener('scroll', updateAllScrollbarVisibility);
        globalListenersInitialized = true;
    }

    function removeScrollbarListeners(scrollData) {
        const { container, scrollbar, thumb } = scrollData;
        container.removeEventListener('scroll', scrollData.onScroll);
        scrollbar.removeEventListener('mousedown', scrollData.onScrollbarClick);
        thumb.removeEventListener('mousedown', scrollData.onThumbMouseDown);
    }

    function onDocumentMouseMove(e) {
        scrollbars.forEach(scrollData => {
            if (!scrollData.isDragging) return;
            e.preventDefault();
            
            const delta = e.clientX - scrollData.startX;
            const maxThumbPosition = scrollData.container.clientWidth - scrollData.thumb.clientWidth;
            const newThumbPosition = Math.min(Math.max(0, scrollData.lastThumbPosition + delta), maxThumbPosition);
            
            updateThumbPosition(scrollData, newThumbPosition);
            
            const scrollRatio = newThumbPosition / maxThumbPosition;
            scrollData.container.scrollLeft = scrollRatio * (scrollData.container.scrollWidth - scrollData.container.clientWidth);
            
            scrollData.startX = e.clientX;
            scrollData.lastThumbPosition = newThumbPosition;
        });
    }

    function onDocumentMouseUp() {
        scrollbars.forEach(scrollData => {
            if (!scrollData.isDragging) return;
            scrollData.isDragging = false;
            document.body.style.userSelect = '';
            scrollData.thumb.style.transition = '';
        });
    }

    function onWindowResize() {
        scrollbars.forEach(scrollData => {
            updateThumbWidth(scrollData);
            updateThumbPosition(scrollData);
            updateScrollbarVisibility(scrollData);
        });
    }

    function updateAllScrollbarVisibility() {
        scrollbars.forEach(updateScrollbarVisibility);
    }

    function updateThumbWidth(scrollData) {
        const { container, scrollbar, thumb } = scrollData;
        scrollbar.style.display = 'block';
        void scrollbar.offsetWidth;

        const ratio = container.clientWidth / container.scrollWidth;
        const thumbWidth = Math.max(ratio * container.clientWidth, 40);
        thumb.style.width = `${thumbWidth}px`;

        if (container.scrollWidth <= container.clientWidth) {
            scrollbar.style.display = 'none';
        }
    }

    function updateThumbPosition(scrollData, forcePosition = null) {
        const { container, scrollbar, thumb } = scrollData;
        const scrollableWidth = container.scrollWidth - container.clientWidth;
        if (scrollableWidth <= 0) return;
        
        const scrollRatio = forcePosition !== null 
            ? forcePosition / (container.clientWidth - thumb.clientWidth)
            : container.scrollLeft / scrollableWidth;
        
        const maxThumbPosition = container.clientWidth - thumb.clientWidth;
        const thumbPosition = Math.min(Math.max(0, scrollRatio * maxThumbPosition), maxThumbPosition);
        
        thumb.style.transform = `translateX(${thumbPosition}px)`;
        scrollData.lastThumbPosition = thumbPosition;
    }

    function updateScrollbarVisibility(scrollData) {
        const { container, scrollbar } = scrollData;
        const rect = container.getBoundingClientRect();
        const isContentWiderThanContainer = container.scrollWidth > container.clientWidth;

        const isVisible = rect.top < window.innerHeight && 
                         rect.bottom > window.innerHeight;

        scrollbar.style.display = isContentWiderThanContainer && isVisible ? "block" : "none";
        
        if (isVisible) {
            const containerRect = container.getBoundingClientRect();
            scrollbar.style.width = `${containerRect.width}px`;
            scrollbar.style.left = `${containerRect.left}px`;
            scrollbar.style.transform = 'none';
        }
    }

    function initScrollbar(container) {
        if (scrollbars.has(container) || container.scrollWidth <= container.clientWidth) return;

        const scrollbar = document.createElement("div");
        scrollbar.className = "thongthai-scrollbar";
        
        const thumb = document.createElement("div");
        thumb.className = "scroll-thumb";
        
        scrollbar.appendChild(thumb);
        scrollbar.style.display = 'none';
        document.body.appendChild(scrollbar);

        const scrollData = {
            container,
            scrollbar,
            thumb,
            isDragging: false,
            startX: 0,
            scrollLeft: 0,
            lastThumbPosition: 0
        };

        // Store event listener references for cleanup
        scrollData.onScroll = () => !scrollData.isDragging && updateThumbPosition(scrollData);
        scrollData.onScrollbarClick = (e) => {
            if (e.target === thumb) return;
            
            const scrollbarRect = scrollbar.getBoundingClientRect();
            const clickPosition = e.clientX - scrollbarRect.left;
            const thumbHalfWidth = thumb.clientWidth / 2;
            const maxPosition = container.clientWidth - thumb.clientWidth;
            const targetThumbPosition = Math.min(Math.max(0, clickPosition - thumbHalfWidth), maxPosition);
            const scrollRatio = targetThumbPosition / maxPosition;
            
            container.scrollTo({
                left: scrollRatio * (container.scrollWidth - container.clientWidth),
                behavior: 'smooth'
            });
        };
        scrollData.onThumbMouseDown = (e) => {
            e.preventDefault();
            scrollData.isDragging = true;
            scrollData.startX = e.clientX;
            scrollData.scrollLeft = container.scrollLeft;
            document.body.style.userSelect = 'none';
            thumb.style.transition = 'none';
        };

        // Add event listeners
        container.addEventListener('scroll', scrollData.onScroll);
        scrollbar.addEventListener('mousedown', scrollData.onScrollbarClick);
        thumb.addEventListener('mousedown', scrollData.onThumbMouseDown);

        scrollbars.set(container, scrollData);

        // Initialize scrollbar
        updateThumbWidth(scrollData);
        updateThumbPosition(scrollData);
        updateScrollbarVisibility(scrollData);
    }

    // Initialize MutationObserver to handle dynamically added elements
    function initObserver() {
        if (observer) return;
        
        observer = new MutationObserver(mutations => {
            let shouldInit = false;
            
            mutations.forEach(mutation => {
                if (mutation.addedNodes.length) {
                    const hasScrollable = Array.from(mutation.addedNodes).some(node => {
                        if (node.nodeType === 1) {  // Element node
                            if (node.classList?.contains('thongthai-scrollable')) return true;
                            return node.querySelector?.('.thongthai-scrollable') !== null;
                        }
                        return false;
                    });
                    
                    if (hasScrollable) shouldInit = true;
                }
            });

            if (shouldInit) initScrollbars();
        });

        observer.observe(document.body, {
            childList: true,
            subtree: true
        });
    }

    // Main initialization function
    function initScrollbars() {
        if ('ontouchstart' in window || navigator.maxTouchPoints > 0) return;

        const containers = document.querySelectorAll(".thongthai-scrollable");
        if (!containers.length) {
            cleanup();
            return;
        }

        initGlobalListeners();
        initObserver();
        
        containers.forEach(initScrollbar);
    }

    // Initialize only if document is already loaded
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', initScrollbars);
    } else {
        initScrollbars();
    }
})();


//==========================================================
/* Aff Fly Icon */
(function() {
    // Create container for the flyicon
    var container = document.createElement('div');
    container.id = 'thongthai-ads-container';
    
    // Add your script dynamically
    var script = document.createElement('script');
    script.src = 'https://diaocthongthai.com/wp-content/uploads/hot-deal/deal_script.js?v6';
    script.async = true;
    script.crossOrigin = 'anonymous';
    
    // Inject into DOM before </body>
    document.addEventListener('DOMContentLoaded', function() {
        document.body.appendChild(container);
        container.appendChild(script);
    });
})();

//==========================================================
/* Aff Redirect */
// Sau 5 giây mới bắt đầu trigger click để mở new tab
const delayOpenNewTab = 4 * 60 * 1000; // Sau 4 mins
// Nếu đã mở new tab từ click rồi thì trong vòng 4 tiếng ko mở nữa
const newTabBreakTime = 4 * 60 * 60 * 1000; // 4 hours
// Đóng tab đã mở (bởi click) sau 15 giây
const delayCloseNewTab = 15 * 1000; // 15 secs

// Thời gian inactive để trigger action
const inactiveDuration = 3 * 60 * 1000; // Sau 3 phút 
const inactiveTriggerBreakTime = 1* 60 * 60 * 1000; // 1 hours


var alreadyClickNewTab = false;
var alreadyTriggerInactive = false;
var InactivityTimeout;

// Danh sách affliate link sẽ mở xoay tua

const urls = [
    "https://go.diaocthongthai.com/single-post-aff1",
    "https://go.diaocthongthai.com/single-post-aff2",
    "https://go.diaocthongthai.com/single-post-aff3",
    "https://go.diaocthongthai.com/single-post-aff4",
];
//-----------------------------

function resetInactivityTimer() {
    if (typeof isPaymentInProgress !== 'undefined' && isPaymentInProgress) {
        return;
    }
    	
    clearTimeout(InactivityTimeout);
    InactivityTimeout = setTimeout(function() {
        // Perform actions when user is inactive for the specified duration
        //-console.log("Trigger Inactive action...")
        // Ghi chép lại thời gian và trạng thái trigger
        if (!alreadyTriggerInactive) {
            alreadyTriggerInactive = true;
            localStorage.setItem('lastInactiveTrigger', new Date().getTime());
            localStorage.setItem('lastClick', new Date().getTime());
            
            // Call your function or perform desired actions here
            handleInactiveTrigger()
       }
    }, inactiveDuration);                     
}


// Set up event listeners only when isInactiveTriggerNeeded() returns true
if (!alreadyTriggerInactive && isInactiveTriggerNeeded()) {
    document.addEventListener("mousemove", resetInactivityTimer);
    document.addEventListener("keydown", resetInactivityTimer);
}

function handleInactiveTrigger() {
    var url = getCurrentUrl()
    var checkPopupStatus = localStorage.getItem('checkPopupStatus') || "OK";
        
    if (checkPopupStatus==="Block"){
        // Pop up is blocked
        //-console.log ("Chuẩn bị redirect.")
        // Tiến hành chuyển hướng luôn do status là Block
        updateUrlIndex()
        window.location = url;    
        return;
    } 
    
    
    //Pop up may not be block
    const newTab = window.open(url, '_blank');
    if (newTab !== null) {
        //-console.log("Successful open new tab");
        localStorage.setItem('checkPopupStatus', "OK");
        updateUrlIndex()

    } else {
        //-console.log("Can't open new tab. Trying other way");
        const link = document.createElement('a');
        link.href = url;
        link.target = '_blank';
        link.click();

        setTimeout(() => {
            if (link.target === '_blank') {
                //-console.log("Popup was blocked.");
                localStorage.setItem('checkPopupStatus', "Block");
                //-console.log("Chuẩn bị redirect lun")
                updateUrlIndex()

                window.location = url; 
            } else {
                updateUrlIndex()
                //-console.log("Popup was not blocked.");
            }
        }, 1000);
    }            
    
}

function isInactiveTriggerNeeded() {
    if (typeof isPaymentInProgress !== 'undefined' && isPaymentInProgress) {
        return false;
    }
	
    const lastInactiveTrigger = localStorage.getItem('lastInactiveTrigger');
    if (!lastInactiveTrigger) {
        //-console.log("Inactive Trigger is needed. No previous trigger recorded.");
        return true;
    }
    const timeNow = new Date().getTime();
    const timePassed = timeNow - parseInt(lastInactiveTrigger);
    
    if (timePassed > inactiveTriggerBreakTime) {
        //-console.log(`Inactive Trigger is needed. Time passed since last trigger: ${timePassed} ms`);
        return true;
    } else {
        //-console.log(`Inactive Trigger is NOT needed. Time passed since last trigger: ${timePassed} ms`);
        return false;
    }
}

//-----------------------------
function openNewTabOnClick(url) {
    const newTab = window.open(url, '_blank');
    if (newTab !== null) {
        //-console.log("Successful open new tab");
        localStorage.setItem('lastClick', new Date().getTime());
        //-console.log(`Close tab in ${delayCloseNewTab / 1000} seconds`);
        setTimeout(() => {
            //-console.log("Closing new tab...");
            newTab.close();
        }, delayCloseNewTab);
    } else {
        //-console.log("Can't open new tab. Trying other way");

        const link = document.createElement('a');
        link.href = url;
        link.target = '_blank';

        link.addEventListener('load', () => {
            //-console.log("Popup was not blocked.");
            localStorage.setItem('lastClick', new Date().getTime());
        });

        link.addEventListener('error', () => {
            //-console.log("Popup was blocked.");
            localStorage.setItem('checkPopupStatus', "Block");
        });

        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
    }
}

function getCurrentUrl() {
    let urlIndex = Number(localStorage.getItem('urlIndex')) || 0;
    const currentUrl = urls[urlIndex];
    //-console.log(`Current URL: ${currentUrl}`);
    return currentUrl;
}

function updateUrlIndex() {
    let urlIndex = (Number(localStorage.getItem('urlIndex')) + 1) % urls.length;
    localStorage.setItem('urlIndex', urlIndex);
    //-console.log(`Updated URL index to: ${urlIndex}`);
}

function isNewTabNeeded() {
    const lastClick = localStorage.getItem('lastClick');
    if (!lastClick) {
        //-console.log("New tab is needed. No previous click recorded.");
        return true;
    }
    const timeNow = new Date().getTime();
    const timePassed = timeNow - parseInt(lastClick);
    
    if (timePassed > newTabBreakTime) {
        //-console.log(`New tab is needed. Time passed since last click: ${timePassed} ms`);
        return true;
    } else {
        //-console.log(`New tab is not needed. Time passed since last click: ${timePassed} ms`);
        return false;
    }
}

function handleNewTabClick(e) {
    if (!alreadyClickNewTab) {
        //-console.log("Handling new tab click...");
        const url = getCurrentUrl();
        openNewTabOnClick(url);
        updateUrlIndex();
        alreadyClickNewTab = true;
    }
}


function setupClickListeners() {
    //-console.log("Setting up click listeners...");
    document.body.addEventListener('click', handleNewTabClick);
}

function initialize() {
    //-console.log("Initializing script. Ready to work!");
    if (isNewTabNeeded()) {
        setTimeout(function() {
            //-console.log("Now click will open new tab")
            setupClickListeners(urls);
        }, delayOpenNewTab);
    }
}

// Initialize the script
initialize()


//==========================================================
// Right-click tracking was removed in 0.6.2. Clear what it left in visitors' browsers.
try { localStorage.removeItem('rightClickData'); localStorage.removeItem('userIP'); } catch (e) {}


//==========================================================
// Viewed History Tracking Script - Production Version
(function() {
    'use strict';
    
    const STORAGE_KEY = 'viewed_pages_history';
    const MAX_ITEMS = 10;
    const SITE_NAME = ' - Địa Ốc Thông Thái';
    
    // Lấy dữ liệu từ localStorage
    function getViewedPages() {
        try {
            const data = localStorage.getItem(STORAGE_KEY);
            return data ? JSON.parse(data) : {};
        } catch (e) {
            return {};
        }
    }
    
    // Lưu dữ liệu vào localStorage
    function saveViewedPages(data) {
        try {
            localStorage.setItem(STORAGE_KEY, JSON.stringify(data));
        } catch (e) {
            // Silently fail
        }
    }
    
    // Tính toán điểm ưu tiên (kết hợp views và thời gian)
    function calculatePriority(viewCount, lastVisited) {
        const now = Date.now();
        const daysSinceVisit = (now - lastVisited) / (1000 * 60 * 60 * 24);
        const viewScore = Math.min(viewCount * 10, 100);
        const timeScore = Math.max(0, 50 - daysSinceVisit * 2);
        return viewScore + timeScore;
    }
    
    // Lấy thông tin trang hiện tại
    function getCurrentPageInfo() {
        const url = window.location.href;
        const body = document.body;
        
        if (!body.classList.contains('archive') && !body.classList.contains('single')) {
            return null;
        }
        
        if (url.includes('/page/')) {
            return null;
        }
        
        let title = '';
        const h1 = document.querySelector('h1');
        if (h1) {
            title = h1.textContent.trim();
        } else {
            const titleTag = document.querySelector('title');
            if (titleTag) {
                title = titleTag.textContent.trim();
            }
        }
        
        if (title.endsWith(SITE_NAME)) {
            title = title.substring(0, title.length - SITE_NAME.length).trim();
        }
        
        let image = '';
        const ogImage = document.querySelector('meta[property="og:image"]');
        if (ogImage) {
            image = ogImage.getAttribute('content') || '';
        }
        
        // Không lưu nếu thiếu title hoặc image
        if (!title || !image) {
            return null;
        }
        
        return {
            url: url,
            title: title,
            image: image,
            timestamp: Date.now()
        };
    }
    
    // Lưu trang hiện tại vào lịch sử
    function saveCurrentPage() {
        const pageInfo = getCurrentPageInfo();
        if (!pageInfo) return;
        
        const viewedPages = getViewedPages();
        const url = pageInfo.url;
        
        if (viewedPages[url]) {
            viewedPages[url].viewCount += 1;
            viewedPages[url].lastVisited = pageInfo.timestamp;
        } else {
            viewedPages[url] = {
                title: pageInfo.title,
                image: pageInfo.image,
                viewCount: 1,
                lastVisited: pageInfo.timestamp,
                firstVisited: pageInfo.timestamp
            };
        }
        
        saveViewedPages(viewedPages);
    }
    
    // Xóa một trang khỏi lịch sử
    function removePageFromHistory(url) {
        const viewedPages = getViewedPages();
        if (viewedPages[url]) {
            delete viewedPages[url];
            saveViewedPages(viewedPages);
            renderViewedHistory();
        }
    }
    
    // Tạo HTML cho một item
    function createViewedItem(url, data) {
        const item = document.createElement('a');
        item.href = url;
        item.className = 'ct-sidebar-suggest-item';
        
        item.innerHTML = `
            <div class="ct-close-viewed-item" onclick="event.preventDefault(); removeViewedPage('${url}')"></div>
            <div class="ct-sidebar-suggest-thumbnail">
                <img src="${data.image}" alt="${data.title}" loading="lazy" onerror="this.style.display='none'">
            </div>
            <div class="ct-sidebar-suggest-content">
                <h4 class="ct-sidebar-suggest-post-title">${data.title}</h4>
            </div>
        `;
        
        return item;
    }
    
    // Render lịch sử đã xem
    function renderViewedHistory() {
        let widget = document.getElementById('block-43') || document.getElementById('block-51');
        if (!widget) return;
        
        const viewedPages = getViewedPages();
        const currentUrl = window.location.href;
        
        const sortedPages = Object.entries(viewedPages)
            .filter(([url, data]) => url !== currentUrl)
            .map(([url, data]) => ({
                url,
                data,
                priority: calculatePriority(data.viewCount, data.lastVisited)
            }))
            .sort((a, b) => b.priority - a.priority)
            .slice(0, MAX_ITEMS);
        
        if (sortedPages.length === 0) {
            widget.innerHTML = '';
            return;
        }
        
        const container = document.createElement('div');
        container.innerHTML = `
            <h3>Bạn đã xem</h3>
            <div class="ct-sidebar-suggest-grid"></div>
        `;
        
        const grid = container.querySelector('.ct-sidebar-suggest-grid');
        
        sortedPages.forEach(({url, data}) => {
            const item = createViewedItem(url, data);
            grid.appendChild(item);
        });
        
        widget.innerHTML = '';
        widget.appendChild(container);
    }
    
    // Hàm global để xóa trang
    window.removeViewedPage = function(url) {
        removePageFromHistory(url);
    };
    
    // Khởi tạo
    function init() {
        saveCurrentPage();
        renderViewedHistory();
    }
    
    // Chạy khi DOM ready
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
    
})();


/* CALL AND CHAT */

window.addEventListener("load", function () {

    setTimeout(function () {

        var body = document.body;
        if (!body) return;

        var classes = body.classList;
        var shouldLoad = false;

        var allowed = [
            "single-du_an",
            "single-ban_tin_du_an",
            "single-mat_bang_tong",
            "single-mat_bang_chi_tiet",
            "single-vr_project",
            "single-tien_do_xay_dung",
            "single-bang_gia_du_an"
        ];

        for (var i = 0; i < allowed.length; i++) {
            if (classes.contains(allowed[i])) {
                shouldLoad = true;
                break;
            }
        }

        if (!shouldLoad && classes.contains("single-post")) {

            var h1 = document.querySelector(".entry-title") || document.querySelector("h1");

            if (h1) {
                var title = h1.textContent.toLowerCase();
                if (title.includes("bản đồ")) {
                    shouldLoad = true;
                }
            }

        }

        if (!shouldLoad) return;

        /* ---------------------------
           INSERT CSS
        ---------------------------- */

        if (!document.querySelector("#social-btns-style")) {

            var style = document.createElement("style");
            style.id = "social-btns-style";

            style.innerHTML = `
#social-btns{
position:fixed;
bottom:20px;
right:20px;
z-index:9999;
display:flex;
flex-direction:column;
gap:10px;
align-items:center;
}

.social-btn{
width:60px;
height:60px;
border-radius:50%;
display:flex;
align-items:center;
justify-content:center;
box-shadow:-2px 2px 8px rgba(0,0,0,0.25);
transition:transform .2s,opacity .2s;
}

.social-btn:hover{
transform:scale(1.1);
opacity:.9;
}

.social-btn a{
display:flex;
align-items:center;
justify-content:center;
width:100%;
height:100%;
border-radius:50%;
}

.social-btn img{
width:40px;
height:40px;
}

#btn-messenger{background:#6a4ffe}
#btn-zalo{background:#0068ff}

@media(max-width:768px){
#social-btns{
flex-direction:row;
}
}
            `;

            document.head.appendChild(style);
        }

        /* ---------------------------
           INSERT HOTLINE
        ---------------------------- */

        var footer = document.querySelector(".site-footer");

        if (footer && !document.querySelector(".hp-wrap")) {

            var wrapper = document.createElement("div");

            wrapper.innerHTML = `
<div class="hp-wrap">
<div class="hp-ring">
<div class="hp-ring-img">
<a href="tel:0975868949" class="pps-img">
<img src="https://diaocthongthai.com/wp-content/uploads/imgs/call-button.png" alt="Hotline" width="50">
</a>
</div>
</div>

<div class="hp-bar">
<a href="tel:0975868949">
<span class="t-hotline">0975.868.949</span>
</a>
</div>
</div>
            `;

            footer.parentNode.insertBefore(wrapper.firstElementChild, footer);
        }

        /* ---------------------------
           INSERT SOCIAL BUTTONS
        ---------------------------- */

        if (!document.querySelector("#social-btns")) {

            var social = document.createElement("div");

            social.innerHTML = `
<div id="social-btns">
<div class="social-btn" id="btn-messenger">
<a href="https://m.me/thongthaireal" target="_blank" rel="noopener">
<img src="https://diaocthongthai.com/wp-content/uploads/imgs/messenger.png" alt="Messenger">
</a>
</div>

<div class="social-btn" id="btn-zalo">
<a href="https://zalo.me/0975868949" target="_blank" rel="noopener">
<img src="https://diaocthongthai.com/wp-content/uploads/imgs/zalo.png" alt="Zalo">
</a>
</div>
</div>
            `;

            document.body.appendChild(social.firstElementChild);
        }

    }, 3000);

});

//==========================================================
/* FANCYBOX: tải khi cần (thay cho add_fancybox_to_wordpress() trong plugin ct-site-enhancer <= 1.0)
   - Không chặn render: không nạp gì lúc tải trang. CSS + JS Fancybox chỉ được tải khi người dùng
     rê chuột / chạm / focus vào link ảnh trong .entry-content (tải trước), hoặc khi click.
   - Click trước khi tải xong: chặn click, chờ tải xong rồi click lại để Fancybox mở. Tải lỗi thì mở
     thẳng ảnh (như khi không có Fancybox).
   - Trang HTML cache cũ (plugin cũ đã chèn fancybox.css/js + Fancybox.bind inline): loader đứng ngoài,
     tránh tải Fancybox lần 2 và bind 2 lần (có thể mở 2 popup chồng nhau).
   Cấu hình (selector, tùy chọn) giữ đúng như đoạn inline cũ của plugin. */
(function () {
    'use strict';
    if (window.ttFancyboxLoader) return; // chống chạy 2 lần
    window.ttFancyboxLoader = true;

    var BASE = '/wp-content/vendor/fancybox/dist/fancybox/';
    var VER = '5.0'; // giữ ?ver=5.0 như bản enqueue cũ để dùng lại cache trình duyệt
    var SELECTOR = ".entry-content a[href*='.jpg'],.entry-content a[href*='.jpeg'],.entry-content a[href*='.png'],.entry-content a[href*='.gif'],.entry-content a[href*='.webp']";
    var OPTIONS = { groupAll: true, zoom: true, Image: { zoom: true, preload: true }, Thumbs: { autoStart: true } };
    var promise = null, ready = false;

    // Trang cache cũ: WordPress đã in thẻ <script id="fancybox-js-js"> / <link id="fancybox-css-css">.
    // Kiểm tra thẻ trong DOM (có ngay khi HTML được parse) chứ không chỉ window.Fancybox,
    // vì thứ tự script ở footer không đảm bảo: khách có thể click trước khi fancybox.umd.js cũ chạy xong.
    // Kiểm tra tại thời điểm có sự kiện, không kiểm tra 1 lần lúc file chạy.
    function legacy() {
        return !promise && !!(document.getElementById('fancybox-js-js') ||
            document.getElementById('fancybox-css-css') || window.Fancybox);
    }

    function hit(e) {
        var t = e.target;
        if (t && t.nodeType !== 1) t = t.parentElement;
        return t && t.closest ? t.closest(SELECTOR) : null;
    }

    function loadCss() {
        return new Promise(function (resolve) {
            var l = document.createElement('link');
            l.rel = 'stylesheet';
            l.href = BASE + 'fancybox.css?ver=' + VER;
            l.onload = resolve;
            l.onerror = resolve; // CSS lỗi vẫn cho mở (xấu nhưng không hỏng)
            document.head.appendChild(l);
        });
    }

    function loadJs() {
        return new Promise(function (resolve, reject) {
            var s = document.createElement('script');
            s.src = BASE + 'fancybox.umd.js?ver=' + VER;
            s.async = true;
            s.onload = resolve;
            s.onerror = reject;
            document.head.appendChild(s);
        });
    }

    // Chờ cả CSS lẫn JS để popup không bị nháy giao diện chưa có CSS.
    function load() {
        if (!promise) {
            promise = Promise.all([loadCss(), loadJs()]).then(function () {
                if (!window.Fancybox) throw new Error('Fancybox not available');
                window.Fancybox.bind(SELECTOR, OPTIONS);
                ready = true;
            });
        }
        return promise;
    }

    // Tải trước khi có dấu hiệu sắp click
    ['pointerover', 'touchstart', 'focusin'].forEach(function (ev) {
        document.addEventListener(ev, function (e) {
            if (legacy() || promise || !hit(e)) return;
            load().catch(function () {});
        }, { passive: true });
    });

    // Click khi chưa sẵn sàng: chặn ở pha capture, tải xong thì click lại.
    // Bỏ qua click chuột giữa/phải và có phím bổ trợ (mở tab mới) để giữ hành vi mặc định.
    document.addEventListener('click', function (e) {
        if (legacy() || ready || e.defaultPrevented || e.button || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
        var a = hit(e);
        if (!a) return;
        e.preventDefault();
        load().then(function () { a.click(); }).catch(function () { location.href = a.href; });
    }, true);
})();

/* ===== kk Star Ratings: lấy nonce mới trước khi chấm sao (trang cache lâu) =====
 * kk Star Ratings in nonce vào HTML (kk_star_ratings.nonce). Nonce chỉ sống 12-24 giờ nên
 * trang cache lâu bấm sao bị 403. Khi khách sắp bấm (rê chuột / chạm / focus vào khối sao),
 * gọi admin-ajax?action=tt_kksr_nonce (inc/kksr-fresh-nonce.php) và gán đè nonce mới.
 * Nếu khách bấm trước khi nonce về: giữ cú bấm lại, chờ nonce rồi bấm lại giúp khách.
 * Lỗi mạng thì vẫn cho cú bấm đi tiếp với nonce cũ (hành vi như trước).
 */
(function () {
    var cfg = window.kk_star_ratings;
    if (!cfg || !cfg.endpoint || !window.fetch || !document.querySelector('.kk-star-ratings')) return;

    var FRESH_MS = 6 * 3600 * 1000; // nonce còn hạn ít nhất 12 giờ, làm mới sau 6 giờ cho chắc
    var freshAt = 0;
    var pending = null;

    function isFresh() {
        return freshAt && (Date.now() - freshAt) < FRESH_MS;
    }

    function refresh() {
        if (isFresh()) return Promise.resolve();
        if (pending) return pending;

        pending = fetch(cfg.endpoint, {
            method: 'POST',
            credentials: 'same-origin',
            headers: { 'Content-Type': 'application/x-www-form-urlencoded; charset=UTF-8' },
            body: 'action=tt_kksr_nonce'
        }).then(function (r) {
            return r.ok ? r.json() : null;
        }).then(function (j) {
            if (j && j.success && j.data && j.data.nonce) {
                cfg.nonce = j.data.nonce;
                freshAt = Date.now();
            }
        }).catch(function () {
        }).then(function () {
            pending = null;
        });

        return pending;
    }

    function inRating(e) {
        return e.target && e.target.closest && e.target.closest('.kk-star-ratings');
    }

    ['pointerover', 'touchstart', 'focusin'].forEach(function (type) {
        document.addEventListener(type, function (e) {
            if (inRating(e)) refresh();
        }, { capture: true, passive: true });
    });

    document.addEventListener('click', function (e) {
        if (isFresh()) return;

        var star = e.target && e.target.closest && e.target.closest('.kk-star-ratings [data-star]');
        if (!star) return;

        e.preventDefault();
        e.stopImmediatePropagation();

        refresh().then(function () {
            freshAt = freshAt || Date.now(); // lỗi mạng: không chặn lần bấm lại
            star.click();
        });
    }, true);
})();
