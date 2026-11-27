/* @tt-module id=aff-flyicon device=all ver=1.0.0 order=110 */
/*
 * Nạp uploads/hot-deal/deal_script.js (icon deal)
 * Tách nguyên văn từ thongthai-script.js dòng 1083-1101 (bản 27/11/2026).
 * Code chạy bên trong TT.define(...), biến không lộ ra ngoài (trừ khi gán window.xxx).
 */
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
