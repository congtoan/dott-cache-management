/* @tt-module id=widget-insert device=desktop ver=1.0.0 order=10 */
/*
 * Chèn nội dung widget block 40 (chuyên mục) và block 42 (banner bản đồ 2025) từ widget/*.txt
 * Tách nguyên văn từ thongthai-script.js dòng 1-30 (bản 27/11/2026).
 * Code chạy bên trong TT.define(...), biến không lộ ra ngoài (trừ khi gán window.xxx).
 */
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
