/* @tt-module id=expand-collapse device=all ver=1.0.0 order=30 */
/*
 * Nút Show more/Show less cho .collapse-container
 * Tách nguyên văn từ thongthai-desktop.js dòng 58-66 (bản 27/11/2026).
 * Code chạy bên trong TT.define(...), biến không lộ ra ngoài (trừ khi gán window.xxx).
 */
/* [Common] Expand collapse container */
document.querySelectorAll('.expand-button').forEach(button => {
    button.addEventListener('click', function () {
        var instanceId = this.getAttribute('data-instance-id');
        var container = document.querySelector('.collapse-container[data-instance-id="' + instanceId + '"]');
        container.classList.toggle('expanded');
        this.textContent = container.classList.contains('expanded') ? 'Show less' : 'Show more';
    });
});
