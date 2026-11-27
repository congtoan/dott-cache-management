/* @tt-module id=kksr-nonce device=all ver=1.0.0 order=170 */
/*
 * kk Star Ratings: lấy nonce mới trước khi chấm sao
 * Tách nguyên văn từ thongthai-script.js dòng 1782-1849 (bản 27/11/2026).
 * Code chạy bên trong TT.define(...), biến không lộ ra ngoài (trừ khi gán window.xxx).
 */
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
