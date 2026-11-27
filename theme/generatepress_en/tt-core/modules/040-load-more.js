/* @tt-module id=load-more device=all ver=1.0.0 order=40 */
/*
 * Ajax Load More (JS thuần)
 * Tách nguyên văn từ thongthai-desktop.js dòng 68-180 (bản 27/11/2026).
 * Code chạy bên trong TT.define(...), biến không lộ ra ngoài (trừ khi gán window.xxx).
 */
/* [Common] Ajax Load More Button */
const loadmore_buttons = document.querySelectorAll('.load-more');
const dotContainerHTML = `
    <span class="dot-container">
        <span class="dot"></span>
        <span class="dot"></span>
        <span class="dot"></span>
    </span>
`;

loadmore_buttons.forEach(button => {
    button.innerHTML += dotContainerHTML;
});

/* Ajax Load More: viết lại bằng JS thuần (không cần jQuery).
   Giữ nguyên hành vi bản jQuery cũ:
   - đọc data-* như jQuery .data(): chuỗi số -> number, chuỗi JSON (data-args) -> object
   - gửi my_args theo định dạng jQuery.param, nên PHP vẫn nhận $_POST['my_args'] là mảng
   - HTML trả về được chèn và CHẠY các <script> bên trong (nếu có, giống jQuery .append) */
(function () {
    function ttData(el, name) {
        var v = el.getAttribute('data-' + name);
        if (v === null) return undefined;
        if (v === 'true') return true;
        if (v === 'false') return false;
        if (v === 'null') return null;
        if (v === +v + '') return +v;
        if (/^(?:\{[\w\W]*\}|\[[\w\W]*\])$/.test(v)) {
            try { return JSON.parse(v); } catch (e) { /* để nguyên chuỗi */ }
        }
        return v;
    }

    function ttBuildParams(prefix, obj, add) {
        if (Array.isArray(obj)) {
            obj.forEach(function (v, i) {
                if (/\[\]$/.test(prefix)) add(prefix, v);
                else ttBuildParams(prefix + '[' + (typeof v === 'object' && v != null ? i : '') + ']', v, add);
            });
        } else if (obj !== null && typeof obj === 'object') {
            Object.keys(obj).forEach(function (k) { ttBuildParams(prefix + '[' + k + ']', obj[k], add); });
        } else {
            add(prefix, obj);
        }
    }

    function ttParams(data) {
        var p = new URLSearchParams();
        Object.keys(data).forEach(function (k) {
            ttBuildParams(k, data[k], function (key, val) { p.append(key, val == null ? '' : val); });
        });
        return p;
    }

    function ttInsertHTML(el, html) {
        var tpl = document.createElement('template');
        tpl.innerHTML = html;
        var scripts = Array.prototype.slice.call(tpl.content.querySelectorAll('script'));
        el.appendChild(tpl.content);
        scripts.forEach(function (old) {
            var s = document.createElement('script');
            Array.prototype.forEach.call(old.attributes, function (a) { s.setAttribute(a.name, a.value); });
            s.text = old.textContent;
            old.parentNode.replaceChild(s, old);
        });
    }

    document.addEventListener('click', function (event) {
        var button = event.target.closest('button.load-more');
        if (!button) return;
        event.preventDefault();

        var instanceId = ttData(button, 'instance-id');
        var containers = Array.prototype.slice.call(
            document.querySelectorAll('.thongthai-ajax-listing[data-instance-id="' + instanceId + '"]')
        );
        if (!containers.length) return;

        var my_args = ttData(containers[0], 'args');
        var listing_type = ttData(containers[0], 'listing-type');
        var max_page = ttData(containers[0], 'max-page');
        var ajax_loadmore_paged = ttData(containers[0], 'current-page');

        fetch('https://diaocthongthai.com/en/wp-admin/admin-ajax.php', {
            method: 'POST',
            credentials: 'same-origin',
            headers: { 'X-Requested-With': 'XMLHttpRequest' },
            body: ttParams({
                action: 'loadmore',
                ajax_loadmore_paged: ajax_loadmore_paged,
                my_args: my_args,
                listing_type: listing_type,
                instance_id: instanceId
            })
        })
            .then(function (res) {
                if (!res.ok) throw new Error(res.status + ' ' + res.statusText);
                return res.text();
            })
            .then(function (response) {
                containers.forEach(function (c) { ttInsertHTML(c, response); });
                ajax_loadmore_paged += 1;
                containers.forEach(function (c) { c.setAttribute('data-current-page', ajax_loadmore_paged); });

                if (ajax_loadmore_paged > max_page) {
                    button.classList.add('hidden');
                }
            })
            .catch(function (error) {
                console.log('The following error occurred: ' + error.message, error);
            });
    });
})();
