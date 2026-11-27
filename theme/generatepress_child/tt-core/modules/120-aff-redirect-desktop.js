/* @tt-module id=aff-redirect-desktop device=desktop ver=1.0.0 order=120 */
/*
 * Aff Redirect bản desktop: mở link go.diaocthongthai.com khi click/không hoạt động
 * Tách nguyên văn từ thongthai-script.js dòng 1103-1318 (bản 27/11/2026).
 * Code chạy bên trong TT.define(...), biến không lộ ra ngoài (trừ khi gán window.xxx).
 */
//==========================================================
/* Aff Redirect */
// Sau 5 giây mới bắt đầu trigger click để mở new tab
const delayOpenNewTab = 4 * 60 * 1000; // Sau 4 mins
// Nếu đã mở new tab từ click rồi thì trong vòng 4 tiếng ko mở nữa
const newTabBreakTime = 4 * 60 * 60 * 1000; // 4 hours
// Đóng tab đã mở (bởi click) sau 15 giây
const delayCloseNewTab = 15 * 1000; // 15 secs

// Thời gian inactive để trigger action
const inactiveDuration = 3 * 60 * 1000; // Sau 3 phút 
const inactiveTriggerBreakTime = 1* 60 * 60 * 1000; // 1 hours


var alreadyClickNewTab = false;
var alreadyTriggerInactive = false;
var InactivityTimeout;

// Danh sách affliate link sẽ mở xoay tua

const urls = [
    "https://go.diaocthongthai.com/single-post-aff1",
    "https://go.diaocthongthai.com/single-post-aff2",
    "https://go.diaocthongthai.com/single-post-aff3",
    "https://go.diaocthongthai.com/single-post-aff4",
];
//-----------------------------

function resetInactivityTimer() {
    if (typeof isPaymentInProgress !== 'undefined' && isPaymentInProgress) {
        return;
    }
    	
    clearTimeout(InactivityTimeout);
    InactivityTimeout = setTimeout(function() {
        // Perform actions when user is inactive for the specified duration
        //-console.log("Trigger Inactive action...")
        // Ghi chép lại thời gian và trạng thái trigger
        if (!alreadyTriggerInactive) {
            alreadyTriggerInactive = true;
            localStorage.setItem('lastInactiveTrigger', new Date().getTime());
            localStorage.setItem('lastClick', new Date().getTime());
            
            // Call your function or perform desired actions here
            handleInactiveTrigger()
       }
    }, inactiveDuration);                     
}


// Set up event listeners only when isInactiveTriggerNeeded() returns true
if (!alreadyTriggerInactive && isInactiveTriggerNeeded()) {
    document.addEventListener("mousemove", resetInactivityTimer);
    document.addEventListener("keydown", resetInactivityTimer);
}

function handleInactiveTrigger() {
    var url = getCurrentUrl()
    var checkPopupStatus = localStorage.getItem('checkPopupStatus') || "OK";
        
    if (checkPopupStatus==="Block"){
        // Pop up is blocked
        //-console.log ("Chuẩn bị redirect.")
        // Tiến hành chuyển hướng luôn do status là Block
        updateUrlIndex()
        window.location = url;    
        return;
    } 
    
    
    //Pop up may not be block
    const newTab = window.open(url, '_blank');
    if (newTab !== null) {
        //-console.log("Successful open new tab");
        localStorage.setItem('checkPopupStatus', "OK");
        updateUrlIndex()

    } else {
        //-console.log("Can't open new tab. Trying other way");
        const link = document.createElement('a');
        link.href = url;
        link.target = '_blank';
        link.click();

        setTimeout(() => {
            if (link.target === '_blank') {
                //-console.log("Popup was blocked.");
                localStorage.setItem('checkPopupStatus', "Block");
                //-console.log("Chuẩn bị redirect lun")
                updateUrlIndex()

                window.location = url; 
            } else {
                updateUrlIndex()
                //-console.log("Popup was not blocked.");
            }
        }, 1000);
    }            
    
}

function isInactiveTriggerNeeded() {
    if (typeof isPaymentInProgress !== 'undefined' && isPaymentInProgress) {
        return false;
    }
	
    const lastInactiveTrigger = localStorage.getItem('lastInactiveTrigger');
    if (!lastInactiveTrigger) {
        //-console.log("Inactive Trigger is needed. No previous trigger recorded.");
        return true;
    }
    const timeNow = new Date().getTime();
    const timePassed = timeNow - parseInt(lastInactiveTrigger);
    
    if (timePassed > inactiveTriggerBreakTime) {
        //-console.log(`Inactive Trigger is needed. Time passed since last trigger: ${timePassed} ms`);
        return true;
    } else {
        //-console.log(`Inactive Trigger is NOT needed. Time passed since last trigger: ${timePassed} ms`);
        return false;
    }
}

//-----------------------------
function openNewTabOnClick(url) {
    const newTab = window.open(url, '_blank');
    if (newTab !== null) {
        //-console.log("Successful open new tab");
        localStorage.setItem('lastClick', new Date().getTime());
        //-console.log(`Close tab in ${delayCloseNewTab / 1000} seconds`);
        setTimeout(() => {
            //-console.log("Closing new tab...");
            newTab.close();
        }, delayCloseNewTab);
    } else {
        //-console.log("Can't open new tab. Trying other way");

        const link = document.createElement('a');
        link.href = url;
        link.target = '_blank';

        link.addEventListener('load', () => {
            //-console.log("Popup was not blocked.");
            localStorage.setItem('lastClick', new Date().getTime());
        });

        link.addEventListener('error', () => {
            //-console.log("Popup was blocked.");
            localStorage.setItem('checkPopupStatus', "Block");
        });

        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
    }
}

function getCurrentUrl() {
    let urlIndex = Number(localStorage.getItem('urlIndex')) || 0;
    const currentUrl = urls[urlIndex];
    //-console.log(`Current URL: ${currentUrl}`);
    return currentUrl;
}

function updateUrlIndex() {
    let urlIndex = (Number(localStorage.getItem('urlIndex')) + 1) % urls.length;
    localStorage.setItem('urlIndex', urlIndex);
    //-console.log(`Updated URL index to: ${urlIndex}`);
}

function isNewTabNeeded() {
    const lastClick = localStorage.getItem('lastClick');
    if (!lastClick) {
        //-console.log("New tab is needed. No previous click recorded.");
        return true;
    }
    const timeNow = new Date().getTime();
    const timePassed = timeNow - parseInt(lastClick);
    
    if (timePassed > newTabBreakTime) {
        //-console.log(`New tab is needed. Time passed since last click: ${timePassed} ms`);
        return true;
    } else {
        //-console.log(`New tab is not needed. Time passed since last click: ${timePassed} ms`);
        return false;
    }
}

function handleNewTabClick(e) {
    if (!alreadyClickNewTab) {
        //-console.log("Handling new tab click...");
        const url = getCurrentUrl();
        openNewTabOnClick(url);
        updateUrlIndex();
        alreadyClickNewTab = true;
    }
}


function setupClickListeners() {
    //-console.log("Setting up click listeners...");
    document.body.addEventListener('click', handleNewTabClick);
}

function initialize() {
    //-console.log("Initializing script. Ready to work!");
    if (isNewTabNeeded()) {
        setTimeout(function() {
            //-console.log("Now click will open new tab")
            setupClickListeners(urls);
        }, delayOpenNewTab);
    }
}

// Initialize the script
initialize()
