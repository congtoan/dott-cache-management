/* @tt-module id=load-more device=all ver=1.0.0 order=30 */
/*
 * Ajax Load More (JS thuần) + listing cache trang 1 (uploads/tt-listing)
 * Tách nguyên văn từ thongthai-script.js dòng 64-347 (bản 27/11/2026).
 * Code chạy bên trong TT.define(...), biến không lộ ra ngoài (trừ khi gán window.xxx).
 */
/* Ajax Load More Button */
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
   - HTML trả về được chèn và CHẠY các <script> bên trong (khối AdSense trong render_f3_posts) */
(function () {
    function ttReady(fn) {
        if (document.readyState !== 'loading') fn();
        else document.addEventListener('DOMContentLoaded', fn);
    }

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

    function ttInsertHTML(el, html, replace) {
        var tpl = document.createElement('template');
        tpl.innerHTML = html;
        var scripts = Array.prototype.slice.call(tpl.content.querySelectorAll('script'));
        if (replace) el.textContent = '';
        el.appendChild(tpl.content);
        scripts.forEach(function (old) {
            var s = document.createElement('script');
            Array.prototype.forEach.call(old.attributes, function (a) { s.setAttribute(a.name, a.value); });
            s.text = old.textContent;
            old.parentNode.replaceChild(s, old);
        });
    }

    /* Listing cache trang 1: đọc file tĩnh uploads/tt-listing/<key>.html do server tạo sẵn
       (inc/listing-cache.php), không qua admin-ajax. Khóa tính từ data-args, PHẢI khớp
       tt_lc_canonical() + tt_lc_key() bên PHP. Lỗi hoặc chưa có file -> dùng ajax như cũ. */
    var TT_LC_BASE = window.location.origin + '/wp-content/uploads/tt-listing/';
    var TT_LC_V_SECONDS = 3600; // tham số ?v= đổi mỗi giờ để không dính bản cũ ở trình duyệt/CDN

    function ttPhpEmpty(v) {
        if (v === undefined || v === null || v === false || v === 0 || v === '0' || v === '') return true;
        if (Array.isArray(v)) return v.length === 0;
        if (typeof v === 'object') return Object.keys(v).length === 0;
        return false;
    }

    function ttLcCanonical(raw) {
        if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return null;
        if (!ttPhpEmpty(raw.tax_query) || !ttPhpEmpty(raw.orderby) || !ttPhpEmpty(raw.order)) return null;

        var types = raw.post_type === undefined || raw.post_type === null ? ['post'] : [].concat(raw.post_type);
        types = types.map(function (t) { return String(t).trim().toLowerCase(); })
            .filter(function (t, i, a) { return t.length > 0 && a.indexOf(t) === i; })
            .sort();
        if (!types.length) types = ['post'];

        function ints(v) {
            return [].concat(v).map(function (x) { return parseInt(x, 10); })
                .filter(function (n, i, a) { return n > 0 && a.indexOf(n) === i; })
                .sort(function (a, b) { return a - b; });
        }

        var ppp = raw.posts_per_page === undefined || raw.posts_per_page === null ? 6 : parseInt(raw.posts_per_page, 10);
        if (!(ppp >= 1 && ppp <= 30)) return null;

        return {
            v: 1,
            t: 'f3',
            pt: types,
            cat: raw.category__in === undefined || raw.category__in === null ? [] : ints(raw.category__in),
            tag: raw.tag__in === undefined || raw.tag__in === null ? [] : ints(raw.tag__in),
            st: ttPhpEmpty(raw.ignore_sticky_posts) ? 0 : 1,
            ppp: ppp
        };
    }

    // Trả về Promise<{html, maxPage} | false>
    function ttLcLoad(container) {
        if (!window.crypto || !window.crypto.subtle || !window.TextEncoder || !window.fetch) return Promise.resolve(false);
        var raw = ttData(container, 'args');
        var c = ttLcCanonical(raw);
        if (!c) return Promise.resolve(false);

        return window.crypto.subtle.digest('SHA-256', new TextEncoder().encode(JSON.stringify(c)))
            .then(function (buf) {
                var hex = Array.prototype.map.call(new Uint8Array(buf), function (b) {
                    return ('0' + b.toString(16)).slice(-2);
                }).join('');
                var key = (c.tag.length ? 'r-' : 'l-') + hex.slice(0, 12);
                var v = Math.floor(Date.now() / 1000 / TT_LC_V_SECONDS);
                return fetch(TT_LC_BASE + key + '.html?v=' + v, { credentials: 'omit' });
            })
            .then(function (res) {
                if (!res || !res.ok) return false;
                return res.text().then(function (text) {
                    var m = text.match(/^<!--tt-listing (\{.*?\})-->/);
                    if (!m) return false;
                    var meta = JSON.parse(m[1]);
                    var tpl = document.createElement('template');
                    tpl.innerHTML = text.slice(m[0].length);

                    // Bỏ bài đang xem, rồi cắt còn ppp bài
                    var exclude = ttPhpEmpty(raw.post__not_in) ? [] : [].concat(raw.post__not_in).map(function (x) { return parseInt(x, 10); });
                    var removed = 0;
                    Array.prototype.forEach.call(tpl.content.querySelectorAll('article[data-post-id]'), function (a) {
                        if (exclude.indexOf(parseInt(a.getAttribute('data-post-id'), 10)) !== -1) {
                            a.parentNode.removeChild(a);
                            removed++;
                        }
                    });
                    var arts = tpl.content.querySelectorAll('article[data-post-id]');
                    if (!arts.length) return false;
                    for (var i = c.ppp; i < arts.length; i++) arts[i].parentNode.removeChild(arts[i]);

                    var holder = document.createElement('div');
                    holder.appendChild(tpl.content);
                    var found = Math.max(0, (parseInt(meta.found, 10) || 0) - (removed ? 1 : 0));
                    return { html: holder.innerHTML, maxPage: Math.max(1, Math.ceil(found / c.ppp)) };
                });
            })
            .catch(function () { return false; });
    }

    ttReady(function () {
        var initialPostsLoaded = false;

        // Intersection Observer for view-based loading
        var observer = new IntersectionObserver(function (entries) {
            entries.forEach(function (entry) {
                if (entry.isIntersecting && !initialPostsLoaded) {
                    loadPosts([entry.target], true);
                    entry.target.classList.remove('not-loaded');
                    observer.unobserve(entry.target);
                }
            });
        }, {
            root: null,
            rootMargin: '50px',
            threshold: 0.1
        });

        // Observe all unloaded containers
        document.querySelectorAll('.thongthai-ajax-listing.not-loaded').forEach(function (el) {
            observer.observe(el);
        });

        // Scroll-based loading
        window.addEventListener('scroll', function () {
            var scrollPosition = window.pageYOffset || document.documentElement.scrollTop;

            if (!initialPostsLoaded && scrollPosition > 2000) {
                document.querySelectorAll('.thongthai-ajax-listing.not-loaded').forEach(function (el) {
                    loadPosts([el], true);
                    el.classList.remove('not-loaded');
                    observer.unobserve(el); // Remove observer once loaded
                });
                initialPostsLoaded = true;
            }
        }, { passive: true });

        // Trang 1: thử file cache tĩnh trước; không có thì ajax (kèm tt_prime để server tạo file).
        // Trang 2 trở đi (nút "Hiển thị thêm"): ajax như cũ.
        function loadPosts(containers, isInitialLoad) {
            if (!containers.length) return;
            if (!isInitialLoad || containers.length !== 1) {
                ajaxLoadPosts(containers, isInitialLoad, false);
                return;
            }
            var c = containers[0];
            ttLcLoad(c).then(function (r) {
                if (!r) {
                    ajaxLoadPosts(containers, true, true);
                    return;
                }
                ttInsertHTML(c, r.html, true);
                var instanceId = ttData(c, 'instance-id');
                var buttons = document.querySelectorAll('button.load-more[data-instance-id="' + instanceId + '"]');
                buttons.forEach(function (b) { b.classList.remove('hidden'); });
                c.setAttribute('data-max-page', r.maxPage);
                c.setAttribute('data-current-page', 2);
                if (2 > r.maxPage) {
                    buttons.forEach(function (b) { b.classList.add('hidden'); });
                }
            });
        }

        function ajaxLoadPosts(containers, isInitialLoad, prime) {
            if (!containers.length) return;
            var first = containers[0];
            var instanceId = ttData(first, 'instance-id');
            var my_args = ttData(first, 'args');
            var listing_type = ttData(first, 'listing-type');
            var max_page = ttData(first, 'max-page');
            var ajax_loadmore_paged = ttData(first, 'current-page');
            var buttons = document.querySelectorAll('button.load-more[data-instance-id="' + instanceId + '"]');
            var domain = window.location.origin;
            var payload = {
                action: 'loadmore',
                ajax_loadmore_paged: ajax_loadmore_paged,
                my_args: my_args,
                listing_type: listing_type,
                instance_id: instanceId
            };
            if (prime) payload.tt_prime = 1; // nhờ server tạo file cache trang 1

            fetch(domain + '/wp-admin/admin-ajax.php', {
                method: 'POST',
                credentials: 'same-origin',
                headers: { 'X-Requested-With': 'XMLHttpRequest' },
                body: ttParams(payload)
            })
                .then(function (res) {
                    if (!res.ok) throw new Error(res.status + ' ' + res.statusText);
                    return res.text();
                })
                .then(function (response) {
                    containers.forEach(function (c) { ttInsertHTML(c, response, isInitialLoad); });
                    if (isInitialLoad) {
                        buttons.forEach(function (b) { b.classList.remove('hidden'); });
                    }

                    ajax_loadmore_paged += 1;
                    containers.forEach(function (c) { c.setAttribute('data-current-page', ajax_loadmore_paged); });

                    if (ajax_loadmore_paged > max_page) {
                        buttons.forEach(function (b) { b.classList.add('hidden'); });
                    }
                })
                .catch(function (error) {
                    console.log('The following error occurred: ' + error.message, error);
                });
        }

        // Click handler for load more button
        document.addEventListener('click', function (event) {
            var button = event.target.closest('button.load-more');
            if (!button) return;
            event.preventDefault();
            var instanceId = ttData(button, 'instance-id');
            var containers = document.querySelectorAll('.thongthai-ajax-listing[data-instance-id="' + instanceId + '"]');
            loadPosts(Array.prototype.slice.call(containers), false);
        });
    });
})();
