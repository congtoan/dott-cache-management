/* @tt-module id=sortable-table device=all ver=1.0.0 order=90 */
/*
 * Sắp xếp cột .sortable-table (đồng bộ với sticky header)
 * Tách nguyên văn từ thongthai-script.js dòng 691-829 (bản 27/11/2026).
 * Code chạy bên trong TT.define(...), biến không lộ ra ngoài (trừ khi gán window.xxx).
 */
//==========================================================
/* Sortable Table */
if (document.querySelector('.sortable-table')) {
    const parsers = {
        number: v => parseFloat(v.replace(/[^0-9.-]/g, '')),
        date: v => new Date(v).getTime(),
        text: v => v.toLowerCase(),
        currency: v => parseFloat(v.replace(/[^0-9.-]/g, ''))
    };

    function compareValues(a, b, dataType) {
        const [type, modifier] = dataType.split(':');
        const parse = parsers[modifier] || parsers[type] || (v => v);
        a = parse(a);
        b = parse(b);
        return typeof a === 'number' && typeof b === 'number' ? a - b : String(a).localeCompare(String(b));
    }

    function sortTable(header) {
	
        // Check if the header is in the sticky header
        const stickyContainer = header.closest('.sticky-header-container');
        if (stickyContainer) {
            // Get the original table (previous sibling of sticky container)
            const originalTable = stickyContainer.previousElementSibling;
            // Find the index of the clicked th in the sticky header
            const thIndex = Array.from(header.parentElement.children).indexOf(header);
            // Get the corresponding th in the original table
            const originalHeaders = originalTable.querySelectorAll('th');
            if (thIndex >= 0 && thIndex < originalHeaders.length) {
                header = originalHeaders[thIndex];
            } else {
                return; // Invalid index, exit
            }
        }	
	
        const table = header.closest('table');
		// Safety check (existing code)
		if (!table.tBodies.length) return;
		
        const columnIndex = header.cellIndex;
        const isAsc = header.classList.contains('sort-asc');
        const isDesc = header.classList.contains('sort-desc');

		// Clear classes in BOTH tables
		const clearSortClasses = (headers) => {
			headers.forEach(th => th.classList.remove('sort-asc', 'sort-desc'));
		};

		// 1. Clear original headers
		const originalHeaders = table.querySelectorAll('th');
		clearSortClasses(originalHeaders);

		// 2. Clear sticky headers if they exist
		const stickyHeaderContainer = table.nextElementSibling;
		if (stickyHeaderContainer?.classList?.contains('sticky-header-container')) {
			const stickyHeaders = stickyHeaderContainer.querySelectorAll('th');
			clearSortClasses(stickyHeaders);
		}

        // Sort rows
        const tbody = table.tBodies[0];
        const rows = Array.from(tbody.rows);

        if (isAsc) {
            // Sort descending
            rows.sort((a, b) => compareValues(
                b.cells[columnIndex].dataset.sortValue,
                a.cells[columnIndex].dataset.sortValue,
                header.dataset.sort
            ));
            header.classList.add('sort-desc');
        } else if (isDesc) {
            // Restore initial order
            tbody.innerHTML = table.dataset.initialOrder;
        } else {
            // Sort ascending
            rows.sort((a, b) => compareValues(
                a.cells[columnIndex].dataset.sortValue,
                b.cells[columnIndex].dataset.sortValue,
                header.dataset.sort
            ));
            header.classList.add('sort-asc');
        }

        // Append sorted rows if not restoring initial order
        if (!isDesc) {
            tbody.append(...rows);
        }

        // Update hover title
        updateHoverTitle(header);
		
		// Update BOTH headers		
		const updateStickyHeader = () => {
			if (!stickyHeaderContainer) return;
			
			const stickyTable = stickyHeaderContainer.querySelector('table');
			const stickyHeader = stickyTable.querySelectorAll('th')[columnIndex];
			
			if (stickyHeader) {
				stickyHeader.classList.add(...header.classList);
				stickyHeader.title = header.title;
			}
		};

		updateStickyHeader();	
		
    }

    function updateHoverTitle(header) {
        if (header.classList.contains('sort-asc')) {
            header.title = 'Sort descending';
        } else if (header.classList.contains('sort-desc')) {
            header.title = 'Sort initial';
        } else {
            header.title = 'Sort ascending';
        }
    }

    // Save initial order of rows for "Sort initial" functionality
	document.querySelectorAll('.sortable-table').forEach(table => {
		// Only process tables that have a tbody
		if (table.tBodies.length > 0) {
			table.dataset.initialOrder = table.tBodies[0].innerHTML;
		}
	});

    // Use event delegation for better performance
    document.addEventListener('click', e => {
        const header = e.target.closest('.sortable-table th[data-sort]');
        if (header) sortTable(header);
    }, false);

    // Update hover titles on page load
    document.querySelectorAll('.sortable-table th[data-sort]').forEach(header => {
        updateHoverTitle(header);
    });
}
