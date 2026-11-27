/* @tt-module id=aff-redirect-mobile device=mobile ver=1.0.0 order=121 */
/*
 * Aff Redirect bản mobile: mở link app Shopee khi chạm, link web khi không hoạt động
 * Tách nguyên văn từ thongthai-script-mobile.js dòng 788-934 (bản 27/11/2026).
 * Code chạy bên trong TT.define(...), biến không lộ ra ngoài (trừ khi gán window.xxx).
 */
//==========================================================
/* Aff Redirect */
const delayOpenNewTab = 4* 60 * 1000; // Sau 4 mins
const newTabBreakTime = 4 * 60 * 60 * 1000; // 4 hours
const delayCloseNewTab = 10 * 1000; // 10 seconds
const inactiveDuration = 3 * 60 * 1000; // 3 minutes 
const inactiveTriggerBreakTime = 1 * 60 * 60 * 1000; // 1 hour

let alreadyClickNewTab = false;
let alreadyTriggerInactive = false;
let inactivityTimeout;

const appUrls = [
    "shopeevn://reactPath?navigate_url=https%3A%2F%2Fshopee.vn%2F%3Futm_campaign%3D-%26utm_content%3D----%26utm_medium%3Daffiliates%26utm_source%3Dan_17316780106%26utm_term%3Dbnoqfe2emsmy",
    "shopeevn://reactPath?navigate_url=https%3A%2F%2Fshopee.vn%2Ftop_products%3Futm_campaign%3D-%26utm_content%3D----%26utm_medium%3Daffiliates%26utm_source%3Dan_17316780106%26utm_term%3Dbnotfvqx2927"
];

const webUrls = [
    "https://go.diaocthongthai.com/single-post-aff1",
    "https://go.diaocthongthai.com/single-post-aff2",
    "https://go.diaocthongthai.com/single-post-aff3",
    "https://go.diaocthongthai.com/single-post-aff4",
];

function resetInactivityTimer() {
    if (typeof isPaymentInProgress !== 'undefined' && isPaymentInProgress) {
        return;
    }
    	
    clearTimeout(inactivityTimeout);
    inactivityTimeout = setTimeout(() => {
        if (!alreadyTriggerInactive) {
            alreadyTriggerInactive = true;
            localStorage.setItem('lastInactiveTrigger', new Date().getTime());
            localStorage.setItem('lastClick', new Date().getTime());
            handleInactiveTrigger();
        }
    }, inactiveDuration);                     
}

if (!alreadyTriggerInactive && isInactiveTriggerNeeded()) {
    document.addEventListener('touchstart', resetInactivityTimer);
    document.addEventListener('scroll', resetInactivityTimer);
}

function openUrl(url) {
    const newTab = window.open(url, '_blank');
    if (newTab !== null) {
        localStorage.setItem('lastClick', new Date().getTime());
        setTimeout(() => newTab.close(), delayCloseNewTab);
        return true;
    } else {
        return false;
    }
}

function handleInactiveTrigger() {
    const url = getCurrentWebUrl();
    const checkPopupStatus = localStorage.getItem('checkPopupStatus') || "OK";
    
    if (checkPopupStatus === "Block") {
        window.location = url;    
        return;
    }
    
    if (!openUrl(url)) {
        const link = document.createElement('a');
        link.href = url;
        link.target = '_blank';
        link.click();

        setTimeout(() => {
            if (link.target === '_blank') {
                localStorage.setItem('checkPopupStatus', "Block");
                window.location = url; 
            }
        }, 1000);
    }
    updateUrlIndex();
}

function isInactiveTriggerNeeded() {
    if (typeof isPaymentInProgress !== 'undefined' && isPaymentInProgress) {
        return false;
    }	
	
    const lastInactiveTrigger = localStorage.getItem('lastInactiveTrigger');
    if (!lastInactiveTrigger) return true;
    
    const timePassed = new Date().getTime() - parseInt(lastInactiveTrigger);
    return timePassed > inactiveTriggerBreakTime;
}

function openNewTabOnClick(url) {
    if (!openUrl(url)) {
        const link = document.createElement('a');
        link.href = url;
        link.target = '_blank';
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
    }
}

function getCurrentWebUrl() {
    const urlIndex = Number(localStorage.getItem('urlIndex')) || 0;
    return webUrls[urlIndex];
}

function updateUrlIndex() {
    const urlIndex = (Number(localStorage.getItem('urlIndex')) + 1) % webUrls.length;
    localStorage.setItem('urlIndex', urlIndex);
}

function isNewTabNeeded() {
    const lastClick = localStorage.getItem('lastClick');
    if (!lastClick) return true;
    
    const timePassed = new Date().getTime() - parseInt(lastClick);
    return timePassed > newTabBreakTime;
}

function handleNewTabClick(e) {
    if (!alreadyClickNewTab) {
        const url = getCurrentAppUrl();
        openNewTabOnClick(url);
        updateUrlIndex();
        alreadyClickNewTab = true;
    }
}

function getCurrentAppUrl() {
    const urlIndex = Number(localStorage.getItem('urlIndex')) || 0;
    return appUrls[urlIndex];
}

function setupClickListeners() {
    document.body.addEventListener('click', handleNewTabClick);
}

function initialize() {
    if (isNewTabNeeded()) {
        setTimeout(() => setupClickListeners(), delayOpenNewTab);
    }
}

initialize();
