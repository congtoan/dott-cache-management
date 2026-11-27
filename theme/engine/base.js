/*
 * tt-core base.js – API window.TT (api 1)
 * ==========================================================================
 * Đây là FILE NGUỒN. Trình build (tt-core.php) ghép file này với các module
 * rồi ghi ra core.js. Đừng enqueue file này trực tiếp.
 *
 * Nguyên tắc:
 *  - Chỉ THÊM vào API, không đổi nghĩa cái đã có. HTML cache cũ có thể chạy
 *    với core.js mới (xem docs/design-tt-core.md mục 8).
 *  - Không phụ thuộc thư viện nào (không jQuery).
 *  - Code ES5 để chạy được trên trình duyệt cũ. Module thì được dùng ES2020.
 */
(function (w, d) {
    'use strict';

    // Nạp 2 lần (ví dụ có thẻ script trùng): giữ bản đầu, module vẫn được
    // chống chạy trùng theo id.
    if (w.TT && w.TT.api) return;

    var API = 1;
    var TT = {};
    var modules = {};       // id -> ver (module đã chạy)
    var skipped = {};       // id -> lý do bỏ qua (sai thiết bị...)
    var loaded = {};        // src -> Promise (TT.load)

    /* ---------------------------------------------------------------
     * Nhận biết thiết bị: giống wp_is_mobile() của WordPress 6.x
     * 1. Có User-Agent Client Hints (tương đương header Sec-CH-UA-Mobile)
     *    thì dùng giá trị đó.
     * 2. Không có thì so User-Agent với cùng danh sách từ khóa của WordPress.
     * iPad gửi UA kiểu Mac nên vẫn là desktop, giống hiện nay.
     * --------------------------------------------------------------- */
    function detectMobile() {
        var nav = w.navigator || {};
        if (nav.userAgentData && typeof nav.userAgentData.mobile === 'boolean') {
            return nav.userAgentData.mobile;
        }
        return /Mobile|Android|Silk\/|Kindle|BlackBerry|Opera Mini|Opera Mobi/.test(nav.userAgent || '');
    }

    /* Ép thiết bị khi test: ?tt_device=mobile | desktop | auto
     * Giá trị nhớ trong sessionStorage của tab (đóng tab là mất). */
    function forcedDevice() {
        var KEY = 'tt_device';
        var v = null;
        try {
            var m = /[?&]tt_device=(mobile|desktop|auto)\b/.exec(w.location.search);
            if (m) {
                if (m[1] === 'auto') w.sessionStorage.removeItem(KEY);
                else w.sessionStorage.setItem(KEY, m[1]);
            }
            v = w.sessionStorage.getItem(KEY);
        } catch (e) { /* chặn storage: bỏ qua */ }
        return v === 'mobile' || v === 'desktop' ? v : null;
    }

    var forced = forcedDevice();
    TT.api = API;
    TT.forced = !!forced;
    TT.mobile = forced ? forced === 'mobile' : detectMobile();
    TT.device = TT.mobile ? 'mobile' : 'desktop';
    TT.errors = [];
    TT.build = null;        // trình build gán { ver, theme, built }

    // Class trên <html> chỉ để JS/CSS phụ dùng. KHÔNG dùng cho bố cục
    // (core.js nạp ở footer nên sẽ nháy). Bố cục dùng media query.
    try { d.documentElement.classList.add('tt-' + TT.device); } catch (e) { }

    /* TT.ready(fn): chạy khi DOM sẵn sàng, hoặc chạy ngay nếu đã sẵn sàng.
     * Khác document.addEventListener('DOMContentLoaded'): không bị lỡ sự kiện
     * khi script được nạp muộn (TT.load, script async). */
    TT.ready = function (fn) {
        if (d.readyState !== 'loading') fn();
        else d.addEventListener('DOMContentLoaded', fn);
    };

    function report(id, err) {
        TT.errors.push({ id: id, error: err });
        if (w.console && w.console.error) w.console.error('[tt-core] module "' + id + '":', err);
    }

    /* TT.define(id, opt, fn)
     *  opt.ver    : phiên bản (chuỗi)
     *  opt.device : 'all' | 'desktop' | 'mobile'
     *  opt.ready  : true = đợi DOM sẵn sàng; mặc định chạy ngay (giống code cũ,
     *               lúc core.js chạy ở footer thì DOM đã parse xong phần thân).
     * Trả về true nếu module được chạy (hoặc đã xếp lịch chạy). */
    TT.define = function (id, opt, fn) {
        opt = opt || {};
        if (typeof fn !== 'function') return false;
        if (modules.hasOwnProperty(id)) return false;          // chống chạy trùng
        var dev = opt.device || 'all';
        if (dev !== 'all' && dev !== TT.device) {
            skipped[id] = dev;
            return false;
        }
        modules[id] = opt.ver || '';
        var run = function () {
            try { fn.call(w, TT); } catch (e) { report(id, e); }
        };
        if (opt.ready) TT.ready(run); else run();
        return true;
    };

    TT.has = function (id) { return modules.hasOwnProperty(id); };
    TT.ver = function (id) { return modules.hasOwnProperty(id) ? modules[id] : null; };
    TT.list = function () {
        var out = {};
        for (var k in modules) if (modules.hasOwnProperty(k)) out[k] = modules[k];
        return { ran: out, skipped: skipped, device: TT.device, forced: TT.forced };
    };

    /* TT.load(src, opt): tải thêm 1 script, trả về Promise.
     *  opt.attrs: thuộc tính thêm cho thẻ script, ví dụ { crossorigin: 'anonymous' }
     *  Cùng src chỉ tải 1 lần. Script tải kiểu này chạy SAU DOMContentLoaded,
     *  nên bên trong nó dùng TT.ready() thay cho DOMContentLoaded. */
    TT.load = function (src, opt) {
        if (loaded[src]) return loaded[src];
        opt = opt || {};
        loaded[src] = new Promise(function (resolve, reject) {
            var s = d.createElement('script');
            s.src = src;
            s.async = true;
            var a = opt.attrs || {};
            for (var k in a) if (a.hasOwnProperty(k)) s.setAttribute(k, a[k]);
            s.onload = function () { resolve(s); };
            s.onerror = function () { delete loaded[src]; reject(new Error('TT.load failed: ' + src)); };
            (d.head || d.documentElement).appendChild(s);
        });
        return loaded[src];
    };

    w.TT = TT;

    /* Hàng đợi cho script chạy TRƯỚC core.js:
     *   (window.TTQ = window.TTQ || []).push(['id', {device:'mobile'}, function (TT) { ... }]);
     * Sau khi core.js chạy, TTQ.push gọi thẳng TT.define. */
    var q = w.TTQ;
    w.TTQ = { push: function (args) { return TT.define.apply(null, args); } };
    if (q && q.length) {
        for (var i = 0; i < q.length; i++) w.TTQ.push(q[i]);
    }
})(window, document);
