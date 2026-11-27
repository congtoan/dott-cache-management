/* @tt-module id=viewed-history device=desktop ver=1.0.0 order=60 */
/*
 * Lịch sử "Seen posts" (block 29)
 * Tách nguyên văn từ thongthai-desktop.js dòng 374-570 (bản 27/11/2026).
 * Code chạy bên trong TT.define(...), biến không lộ ra ngoài (trừ khi gán window.xxx).
 */
//==========================================================
// Viewed History Tracking Script - Production Version
// Block 29
(function() {
    'use strict';
    
    const STORAGE_KEY = 'viewed_pages_history_en';
    const MAX_ITEMS = 10;
    const SITE_NAME = ' - Thong Thai Real';
    
    // Lấy dữ liệu từ localStorage
    function getViewedPages() {
        try {
            const data = localStorage.getItem(STORAGE_KEY);
            return data ? JSON.parse(data) : {};
        } catch (e) {
            return {};
        }
    }
    
    // Lưu dữ liệu vào localStorage
    function saveViewedPages(data) {
        try {
            localStorage.setItem(STORAGE_KEY, JSON.stringify(data));
        } catch (e) {
            // Silently fail
        }
    }
    
    // Tính toán điểm ưu tiên (kết hợp views và thời gian)
    function calculatePriority(viewCount, lastVisited) {
        const now = Date.now();
        const daysSinceVisit = (now - lastVisited) / (1000 * 60 * 60 * 24);
        const viewScore = Math.min(viewCount * 10, 100);
        const timeScore = Math.max(0, 50 - daysSinceVisit * 2);
        return viewScore + timeScore;
    }
    
    // Lấy thông tin trang hiện tại
    function getCurrentPageInfo() {
        const url = window.location.href;
        const body = document.body;
        
        if (!body.classList.contains('archive') && !body.classList.contains('single')) {
            return null;
        }
        
        if (url.includes('/page/')) {
            return null;
        }
        
        let title = '';
        const h1 = document.querySelector('h1');
        if (h1) {
            title = h1.textContent.trim();
        } else {
            const titleTag = document.querySelector('title');
            if (titleTag) {
                title = titleTag.textContent.trim();
            }
        }
        
        if (title.endsWith(SITE_NAME)) {
            title = title.substring(0, title.length - SITE_NAME.length).trim();
        }
        
        let image = '';
        const ogImage = document.querySelector('meta[property="og:image"]');
        if (ogImage) {
            image = ogImage.getAttribute('content') || '';
        }
        
        // Không lưu nếu thiếu title hoặc image
        if (!title || !image) {
            return null;
        }
        
        return {
            url: url,
            title: title,
            image: image,
            timestamp: Date.now()
        };
    }
    
    // Lưu trang hiện tại vào lịch sử
    function saveCurrentPage() {
        const pageInfo = getCurrentPageInfo();
        if (!pageInfo) return;
        
        const viewedPages = getViewedPages();
        const url = pageInfo.url;
        
        if (viewedPages[url]) {
            viewedPages[url].viewCount += 1;
            viewedPages[url].lastVisited = pageInfo.timestamp;
        } else {
            viewedPages[url] = {
                title: pageInfo.title,
                image: pageInfo.image,
                viewCount: 1,
                lastVisited: pageInfo.timestamp,
                firstVisited: pageInfo.timestamp
            };
        }
        
        saveViewedPages(viewedPages);
    }
    
    // Xóa một trang khỏi lịch sử
    function removePageFromHistory(url) {
        const viewedPages = getViewedPages();
        if (viewedPages[url]) {
            delete viewedPages[url];
            saveViewedPages(viewedPages);
            renderViewedHistory();
        }
    }
    
    // Tạo HTML cho một item
    function createViewedItem(url, data) {
        const item = document.createElement('a');
        item.href = url;
        item.className = 'ct-sidebar-suggest-item';
        
        item.innerHTML = `
            <div class="ct-close-viewed-item" onclick="event.preventDefault(); removeViewedPage('${url}')"></div>
            <div class="ct-sidebar-suggest-thumbnail">
                <img src="${data.image}" alt="${data.title}" loading="lazy" onerror="this.style.display='none'">
            </div>
            <div class="ct-sidebar-suggest-content">
                <h4 class="ct-sidebar-suggest-post-title">${data.title}</h4>
            </div>
        `;
        
        return item;
    }
    
    // Render lịch sử đã xem
    function renderViewedHistory() {
        let widget = document.getElementById('block-29');
        if (!widget) return;
        
        const viewedPages = getViewedPages();
        const currentUrl = window.location.href;
        
        const sortedPages = Object.entries(viewedPages)
            .filter(([url, data]) => url !== currentUrl)
            .map(([url, data]) => ({
                url,
                data,
                priority: calculatePriority(data.viewCount, data.lastVisited)
            }))
            .sort((a, b) => b.priority - a.priority)
            .slice(0, MAX_ITEMS);
        
        if (sortedPages.length === 0) {
            widget.innerHTML = '';
            return;
        }
        
        const container = document.createElement('div');
        container.innerHTML = `
            <h3>Your Recent Views</h3>
            <div class="ct-sidebar-suggest-grid"></div>
        `;
        
        const grid = container.querySelector('.ct-sidebar-suggest-grid');
        
        sortedPages.forEach(({url, data}) => {
            const item = createViewedItem(url, data);
            grid.appendChild(item);
        });
        
        widget.innerHTML = '';
        widget.appendChild(container);
    }
    
    // Hàm global để xóa trang
    window.removeViewedPage = function(url) {
        removePageFromHistory(url);
    };
    
    // Khởi tạo
    function init() {
        saveCurrentPage();
        renderViewedHistory();
    }
    
    // Chạy khi DOM ready
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
    
})();
