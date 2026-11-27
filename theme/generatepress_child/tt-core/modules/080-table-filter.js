/* @tt-module id=table-filter device=all ver=1.0.0 order=80 */
/*
 * Ô lọc .table-filter cho bảng ngay sau nó
 * Tách nguyên văn từ thongthai-script.js dòng 664-689 (bản 27/11/2026).
 * Code chạy bên trong TT.define(...), biến không lộ ra ngoài (trừ khi gán window.xxx).
 */
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
