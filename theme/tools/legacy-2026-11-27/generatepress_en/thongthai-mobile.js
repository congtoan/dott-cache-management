/* [Common] Atlas Sidebar Widget */
document.addEventListener('DOMContentLoaded', function() {
    const menuContainer = document.querySelector('#block-23 .quick-atlas-menu');
    if (!menuContainer) return;

    // Simple way to get YYYYMMDD
    const dailyVersion = new Date().toISOString().slice(0, 10).replace(/-/g, '');
    
    fetch('https://diaocthongthai.com/en/wp-content/themes/generatepress_en/widget/atlas-widget.txt?ver=' + dailyVersion)
        .then(response => response.text())
        .then(html => {
            menuContainer.innerHTML = html;
        })
        .catch(error => console.log('Error loading Quick Atlas menu:', error));
});

/* [Common] Expand collapse container */
document.querySelectorAll('.expand-button').forEach(button => {
    button.addEventListener('click', function () {
        var instanceId = this.getAttribute('data-instance-id');
        var container = document.querySelector('.collapse-container[data-instance-id="' + instanceId + '"]');
        container.classList.toggle('expanded');
        this.textContent = container.classList.contains('expanded') ? 'Show less' : 'Show more';
    });
});

/* [Common] Ajax Load More Button */
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
   - HTML trả về được chèn và CHẠY các <script> bên trong (nếu có, giống jQuery .append) */
(function () {
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

    function ttInsertHTML(el, html) {
        var tpl = document.createElement('template');
        tpl.innerHTML = html;
        var scripts = Array.prototype.slice.call(tpl.content.querySelectorAll('script'));
        el.appendChild(tpl.content);
        scripts.forEach(function (old) {
            var s = document.createElement('script');
            Array.prototype.forEach.call(old.attributes, function (a) { s.setAttribute(a.name, a.value); });
            s.text = old.textContent;
            old.parentNode.replaceChild(s, old);
        });
    }

    document.addEventListener('click', function (event) {
        var button = event.target.closest('button.load-more');
        if (!button) return;
        event.preventDefault();

        var instanceId = ttData(button, 'instance-id');
        var containers = Array.prototype.slice.call(
            document.querySelectorAll('.thongthai-ajax-listing[data-instance-id="' + instanceId + '"]')
        );
        if (!containers.length) return;

        var my_args = ttData(containers[0], 'args');
        var listing_type = ttData(containers[0], 'listing-type');
        var max_page = ttData(containers[0], 'max-page');
        var ajax_loadmore_paged = ttData(containers[0], 'current-page');

        fetch('https://diaocthongthai.com/en/wp-admin/admin-ajax.php', {
            method: 'POST',
            credentials: 'same-origin',
            headers: { 'X-Requested-With': 'XMLHttpRequest' },
            body: ttParams({
                action: 'loadmore',
                ajax_loadmore_paged: ajax_loadmore_paged,
                my_args: my_args,
                listing_type: listing_type,
                instance_id: instanceId
            })
        })
            .then(function (res) {
                if (!res.ok) throw new Error(res.status + ' ' + res.statusText);
                return res.text();
            })
            .then(function (response) {
                containers.forEach(function (c) { ttInsertHTML(c, response); });
                ajax_loadmore_paged += 1;
                containers.forEach(function (c) { c.setAttribute('data-current-page', ajax_loadmore_paged); });

                if (ajax_loadmore_paged > max_page) {
                    button.classList.add('hidden');
                }
            })
            .catch(function (error) {
                console.log('The following error occurred: ' + error.message, error);
            });
    });
})();

/* [Common] Download Image Button */
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
            webStatement: 'https://diaocthongthai.com/en/license/'
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
