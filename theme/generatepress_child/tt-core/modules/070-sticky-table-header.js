/* @tt-module id=sticky-table-header device=all ver=1.0.0 order=70 */
/*
 * Sticky header cho bảng trong .thongthai-scrollable
 * Tách nguyên văn từ thongthai-script.js dòng 571-662 (bản 27/11/2026).
 * Sửa có chủ đích khi gộp desktop/mobile:
 *   - mobile: luôn 0px (như bản mobile cũ); desktop: 32px khi có admin bar
 * Code chạy bên trong TT.define(...), biến không lộ ra ngoài (trừ khi gán window.xxx).
 */
//==========================================================
/* Function to create and manage sticky header */
function initStickyHeader(tableContainer) {
    // Find table with sticky-header class inside the container
    const originalTable = tableContainer.querySelector('table.sticky-header');
    if (!originalTable) return;

	const wpAdminBar = document.getElementById('wpadminbar');
	const topOffset = (!TT.mobile && wpAdminBar) ? '32px' : '0px'; // tt-core: bản mobile cũ luôn 0px
	
    // Create sticky header container
    const stickyHeaderContainer = document.createElement('div');
    stickyHeaderContainer.className = 'sticky-header-container';
    stickyHeaderContainer.style.cssText = `
        position: fixed;
        top: ${topOffset};
        left: 0;
        right: 0;
        background: white;
        z-index: 1000;
        display: none;
        overflow-x: hidden;
    `;


    // Extract only <thead> from the original table
    const stickyTable = document.createElement('table');
	stickyTable.className = originalTable.className; // Copy all classes

    stickyTable.style.width = window.getComputedStyle(originalTable).width; // Match width
    const theadClone = originalTable.querySelector('thead').cloneNode(true);
    stickyTable.appendChild(theadClone);
	
	stickyHeaderContainer.appendChild(stickyTable);
    
    // Insert sticky header right after the original table
    originalTable.after(stickyHeaderContainer);

    // Rest of the code remains the same...
    function syncColumnWidths() {
        const originalHeaders = originalTable.querySelectorAll('th');
        const stickyHeaders = stickyTable.querySelectorAll('th');
        
        originalHeaders.forEach((header, index) => {
            if (stickyHeaders[index]) {
                const width = window.getComputedStyle(header).width;
                stickyHeaders[index].style.width = width;
            }
        });
        
        // Match table width
        stickyTable.style.width = window.getComputedStyle(originalTable).width;
    }

    function updateStickyHeader() {
        const tableRect = originalTable.getBoundingClientRect();
        const originalHeaderRect = originalTable.querySelector('thead').getBoundingClientRect();
        const containerRect = tableContainer.getBoundingClientRect();

        if (originalHeaderRect.top < 0 && tableRect.bottom > 0) {
            stickyHeaderContainer.style.display = 'block';
            stickyHeaderContainer.style.left = `${containerRect.left}px`;
            stickyHeaderContainer.style.width = `${containerRect.width}px`;
            
            // Sync horizontal scroll position
            stickyHeaderContainer.scrollLeft = tableContainer.scrollLeft;
        } else {
            stickyHeaderContainer.style.display = 'none';
        }
    }

    // Initial setup
    syncColumnWidths();

    // Event listeners
    window.addEventListener('resize', syncColumnWidths);
    window.addEventListener('scroll', updateStickyHeader);
    tableContainer.addEventListener('scroll', (e) => {
        if (stickyHeaderContainer.style.display === 'block') {
            stickyHeaderContainer.scrollLeft = e.target.scrollLeft;
        }
    });

    // Observe table width changes
    const observer = new ResizeObserver(syncColumnWidths);
    observer.observe(originalTable);
}

// Initialize for all tables with the specified container class
document.querySelectorAll('.thongthai-scrollable').forEach(container => {
    initStickyHeader(container);
});
