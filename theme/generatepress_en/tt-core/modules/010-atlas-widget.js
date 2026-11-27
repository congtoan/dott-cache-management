/* @tt-module id=atlas-widget device=all ver=1.0.0 order=10 */
/*
 * Quick Atlas (block 23): tải widget/atlas-widget.txt
 * Tách nguyên văn từ thongthai-desktop.js dòng 8-22 (bản 27/11/2026).
 * Code chạy bên trong TT.define(...), biến không lộ ra ngoài (trừ khi gán window.xxx).
 */
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
