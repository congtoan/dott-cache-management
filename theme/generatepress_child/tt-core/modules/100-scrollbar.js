/* @tt-module id=scrollbar device=desktop ver=1.0.0 order=100 */
/*
 * Thanh cuộn ngang nổi cho .thongthai-scrollable (bỏ qua máy cảm ứng)
 * Tách nguyên văn từ thongthai-script.js dòng 831-1080 (bản 27/11/2026).
 * Code chạy bên trong TT.define(...), biến không lộ ra ngoài (trừ khi gán window.xxx).
 */
//==========================================================
/* Scroll Bar */
(function() {
    let globalListenersInitialized = false;
    const scrollbars = new Map();
    let observer = null;

    // Cleanup function to remove all scrollbars and event listeners
    function cleanup() {
        scrollbars.forEach(data => {
            data.scrollbar.remove();
            removeScrollbarListeners(data);
        });
        scrollbars.clear();
        
        if (globalListenersInitialized) {
            document.removeEventListener('mousemove', onDocumentMouseMove);
            document.removeEventListener('mouseup', onDocumentMouseUp);
            window.removeEventListener('resize', onWindowResize);
            document.removeEventListener('scroll', updateAllScrollbarVisibility);
            globalListenersInitialized = false;
        }
        
        if (observer) {
            observer.disconnect();
            observer = null;
        }
    }

    // Initialize global event listeners only when needed
    function initGlobalListeners() {
        if (globalListenersInitialized) return;
        
        document.addEventListener('mousemove', onDocumentMouseMove);
        document.addEventListener('mouseup', onDocumentMouseUp);
        window.addEventListener('resize', onWindowResize);
        document.addEventListener('scroll', updateAllScrollbarVisibility);
        globalListenersInitialized = true;
    }

    function removeScrollbarListeners(scrollData) {
        const { container, scrollbar, thumb } = scrollData;
        container.removeEventListener('scroll', scrollData.onScroll);
        scrollbar.removeEventListener('mousedown', scrollData.onScrollbarClick);
        thumb.removeEventListener('mousedown', scrollData.onThumbMouseDown);
    }

    function onDocumentMouseMove(e) {
        scrollbars.forEach(scrollData => {
            if (!scrollData.isDragging) return;
            e.preventDefault();
            
            const delta = e.clientX - scrollData.startX;
            const maxThumbPosition = scrollData.container.clientWidth - scrollData.thumb.clientWidth;
            const newThumbPosition = Math.min(Math.max(0, scrollData.lastThumbPosition + delta), maxThumbPosition);
            
            updateThumbPosition(scrollData, newThumbPosition);
            
            const scrollRatio = newThumbPosition / maxThumbPosition;
            scrollData.container.scrollLeft = scrollRatio * (scrollData.container.scrollWidth - scrollData.container.clientWidth);
            
            scrollData.startX = e.clientX;
            scrollData.lastThumbPosition = newThumbPosition;
        });
    }

    function onDocumentMouseUp() {
        scrollbars.forEach(scrollData => {
            if (!scrollData.isDragging) return;
            scrollData.isDragging = false;
            document.body.style.userSelect = '';
            scrollData.thumb.style.transition = '';
        });
    }

    function onWindowResize() {
        scrollbars.forEach(scrollData => {
            updateThumbWidth(scrollData);
            updateThumbPosition(scrollData);
            updateScrollbarVisibility(scrollData);
        });
    }

    function updateAllScrollbarVisibility() {
        scrollbars.forEach(updateScrollbarVisibility);
    }

    function updateThumbWidth(scrollData) {
        const { container, scrollbar, thumb } = scrollData;
        scrollbar.style.display = 'block';
        void scrollbar.offsetWidth;

        const ratio = container.clientWidth / container.scrollWidth;
        const thumbWidth = Math.max(ratio * container.clientWidth, 40);
        thumb.style.width = `${thumbWidth}px`;

        if (container.scrollWidth <= container.clientWidth) {
            scrollbar.style.display = 'none';
        }
    }

    function updateThumbPosition(scrollData, forcePosition = null) {
        const { container, scrollbar, thumb } = scrollData;
        const scrollableWidth = container.scrollWidth - container.clientWidth;
        if (scrollableWidth <= 0) return;
        
        const scrollRatio = forcePosition !== null 
            ? forcePosition / (container.clientWidth - thumb.clientWidth)
            : container.scrollLeft / scrollableWidth;
        
        const maxThumbPosition = container.clientWidth - thumb.clientWidth;
        const thumbPosition = Math.min(Math.max(0, scrollRatio * maxThumbPosition), maxThumbPosition);
        
        thumb.style.transform = `translateX(${thumbPosition}px)`;
        scrollData.lastThumbPosition = thumbPosition;
    }

    function updateScrollbarVisibility(scrollData) {
        const { container, scrollbar } = scrollData;
        const rect = container.getBoundingClientRect();
        const isContentWiderThanContainer = container.scrollWidth > container.clientWidth;

        const isVisible = rect.top < window.innerHeight && 
                         rect.bottom > window.innerHeight;

        scrollbar.style.display = isContentWiderThanContainer && isVisible ? "block" : "none";
        
        if (isVisible) {
            const containerRect = container.getBoundingClientRect();
            scrollbar.style.width = `${containerRect.width}px`;
            scrollbar.style.left = `${containerRect.left}px`;
            scrollbar.style.transform = 'none';
        }
    }

    function initScrollbar(container) {
        if (scrollbars.has(container) || container.scrollWidth <= container.clientWidth) return;

        const scrollbar = document.createElement("div");
        scrollbar.className = "thongthai-scrollbar";
        
        const thumb = document.createElement("div");
        thumb.className = "scroll-thumb";
        
        scrollbar.appendChild(thumb);
        scrollbar.style.display = 'none';
        document.body.appendChild(scrollbar);

        const scrollData = {
            container,
            scrollbar,
            thumb,
            isDragging: false,
            startX: 0,
            scrollLeft: 0,
            lastThumbPosition: 0
        };

        // Store event listener references for cleanup
        scrollData.onScroll = () => !scrollData.isDragging && updateThumbPosition(scrollData);
        scrollData.onScrollbarClick = (e) => {
            if (e.target === thumb) return;
            
            const scrollbarRect = scrollbar.getBoundingClientRect();
            const clickPosition = e.clientX - scrollbarRect.left;
            const thumbHalfWidth = thumb.clientWidth / 2;
            const maxPosition = container.clientWidth - thumb.clientWidth;
            const targetThumbPosition = Math.min(Math.max(0, clickPosition - thumbHalfWidth), maxPosition);
            const scrollRatio = targetThumbPosition / maxPosition;
            
            container.scrollTo({
                left: scrollRatio * (container.scrollWidth - container.clientWidth),
                behavior: 'smooth'
            });
        };
        scrollData.onThumbMouseDown = (e) => {
            e.preventDefault();
            scrollData.isDragging = true;
            scrollData.startX = e.clientX;
            scrollData.scrollLeft = container.scrollLeft;
            document.body.style.userSelect = 'none';
            thumb.style.transition = 'none';
        };

        // Add event listeners
        container.addEventListener('scroll', scrollData.onScroll);
        scrollbar.addEventListener('mousedown', scrollData.onScrollbarClick);
        thumb.addEventListener('mousedown', scrollData.onThumbMouseDown);

        scrollbars.set(container, scrollData);

        // Initialize scrollbar
        updateThumbWidth(scrollData);
        updateThumbPosition(scrollData);
        updateScrollbarVisibility(scrollData);
    }

    // Initialize MutationObserver to handle dynamically added elements
    function initObserver() {
        if (observer) return;
        
        observer = new MutationObserver(mutations => {
            let shouldInit = false;
            
            mutations.forEach(mutation => {
                if (mutation.addedNodes.length) {
                    const hasScrollable = Array.from(mutation.addedNodes).some(node => {
                        if (node.nodeType === 1) {  // Element node
                            if (node.classList?.contains('thongthai-scrollable')) return true;
                            return node.querySelector?.('.thongthai-scrollable') !== null;
                        }
                        return false;
                    });
                    
                    if (hasScrollable) shouldInit = true;
                }
            });

            if (shouldInit) initScrollbars();
        });

        observer.observe(document.body, {
            childList: true,
            subtree: true
        });
    }

    // Main initialization function
    function initScrollbars() {
        if ('ontouchstart' in window || navigator.maxTouchPoints > 0) return;

        const containers = document.querySelectorAll(".thongthai-scrollable");
        if (!containers.length) {
            cleanup();
            return;
        }

        initGlobalListeners();
        initObserver();
        
        containers.forEach(initScrollbar);
    }

    // Initialize only if document is already loaded
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', initScrollbars);
    } else {
        initScrollbars();
    }
})();
