/* @tt-module id=fancybox device=all ver=1.0.0 order=70 */
/*
 * Fancybox tải khi cần
 * Tách nguyên văn từ thongthai-desktop.js dòng 572-658 (bản 27/11/2026).
 * Code chạy bên trong TT.define(...), biến không lộ ra ngoài (trừ khi gán window.xxx).
 */
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
