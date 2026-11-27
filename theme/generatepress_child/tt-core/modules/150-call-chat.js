/* @tt-module id=call-chat device=all ver=1.0.0 order=150 */
/*
 * Hotline, Messenger, Zalo trên trang dự án và bài bản đồ
 * Tách nguyên văn từ thongthai-script.js dòng 1524-1692 (bản 27/11/2026).
 * Code chạy bên trong TT.define(...), biến không lộ ra ngoài (trừ khi gán window.xxx).
 */
/* CALL AND CHAT */

window.addEventListener("load", function () {

    setTimeout(function () {

        var body = document.body;
        if (!body) return;

        var classes = body.classList;
        var shouldLoad = false;

        var allowed = [
            "single-du_an",
            "single-ban_tin_du_an",
            "single-mat_bang_tong",
            "single-mat_bang_chi_tiet",
            "single-vr_project",
            "single-tien_do_xay_dung",
            "single-bang_gia_du_an"
        ];

        for (var i = 0; i < allowed.length; i++) {
            if (classes.contains(allowed[i])) {
                shouldLoad = true;
                break;
            }
        }

        if (!shouldLoad && classes.contains("single-post")) {

            var h1 = document.querySelector(".entry-title") || document.querySelector("h1");

            if (h1) {
                var title = h1.textContent.toLowerCase();
                if (title.includes("bản đồ")) {
                    shouldLoad = true;
                }
            }

        }

        if (!shouldLoad) return;

        /* ---------------------------
           INSERT CSS
        ---------------------------- */

        if (!document.querySelector("#social-btns-style")) {

            var style = document.createElement("style");
            style.id = "social-btns-style";

            style.innerHTML = `
#social-btns{
position:fixed;
bottom:20px;
right:20px;
z-index:9999;
display:flex;
flex-direction:column;
gap:10px;
align-items:center;
}

.social-btn{
width:60px;
height:60px;
border-radius:50%;
display:flex;
align-items:center;
justify-content:center;
box-shadow:-2px 2px 8px rgba(0,0,0,0.25);
transition:transform .2s,opacity .2s;
}

.social-btn:hover{
transform:scale(1.1);
opacity:.9;
}

.social-btn a{
display:flex;
align-items:center;
justify-content:center;
width:100%;
height:100%;
border-radius:50%;
}

.social-btn img{
width:40px;
height:40px;
}

#btn-messenger{background:#6a4ffe}
#btn-zalo{background:#0068ff}

@media(max-width:768px){
#social-btns{
flex-direction:row;
}
}
            `;

            document.head.appendChild(style);
        }

        /* ---------------------------
           INSERT HOTLINE
        ---------------------------- */

        var footer = document.querySelector(".site-footer");

        if (footer && !document.querySelector(".hp-wrap")) {

            var wrapper = document.createElement("div");

            wrapper.innerHTML = `
<div class="hp-wrap">
<div class="hp-ring">
<div class="hp-ring-img">
<a href="tel:0975868949" class="pps-img">
<img src="https://diaocthongthai.com/wp-content/uploads/imgs/call-button.png" alt="Hotline" width="50">
</a>
</div>
</div>

<div class="hp-bar">
<a href="tel:0975868949">
<span class="t-hotline">0975.868.949</span>
</a>
</div>
</div>
            `;

            footer.parentNode.insertBefore(wrapper.firstElementChild, footer);
        }

        /* ---------------------------
           INSERT SOCIAL BUTTONS
        ---------------------------- */

        if (!document.querySelector("#social-btns")) {

            var social = document.createElement("div");

            social.innerHTML = `
<div id="social-btns">
<div class="social-btn" id="btn-messenger">
<a href="https://m.me/thongthaireal" target="_blank" rel="noopener">
<img src="https://diaocthongthai.com/wp-content/uploads/imgs/messenger.png" alt="Messenger">
</a>
</div>

<div class="social-btn" id="btn-zalo">
<a href="https://zalo.me/0975868949" target="_blank" rel="noopener">
<img src="https://diaocthongthai.com/wp-content/uploads/imgs/zalo.png" alt="Zalo">
</a>
</div>
</div>
            `;

            document.body.appendChild(social.firstElementChild);
        }

    }, 3000);

});
