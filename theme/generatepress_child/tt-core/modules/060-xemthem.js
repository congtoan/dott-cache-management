/* @tt-module id=xemthem device=all ver=1.0.0 order=60 */
/*
 * Nút "Mở rộng/Thu gọn" cho .xemthem trên màn hình nhỏ
 * Tách nguyên văn từ thongthai-script.js dòng 542-570 (bản 27/11/2026).
 * Sửa có chủ đích khi gộp desktop/mobile:
 *   - mobile: luôn chạy (như thongthai-script-mobile.js); desktop: chỉ khi < 480px (như thongthai-script.js)
 * Code chạy bên trong TT.define(...), biến không lộ ra ngoài (trừ khi gán window.xxx).
 */
//==========================================================
/* xemthem expand button */
if ((TT.mobile || window.innerWidth < 480) && document.querySelector('.xemthem')) { // tt-core: bản mobile cũ không có điều kiện innerWidth
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
