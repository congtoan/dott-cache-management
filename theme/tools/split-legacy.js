#!/usr/bin/env node
/*
 * split-legacy.js – tách JS cũ của 2 theme thành module tt-core (chạy MỘT lần).
 *
 *   node theme/tools/split-legacy.js            ghi theme/<theme>/tt-core/modules/*.js
 *   node theme/tools/split-legacy.js --dry      chỉ in ra danh sách
 *
 * Nguồn: theme/tools/legacy-2026-11-27/ (bản chụp từ VPS chính ngày 27/11/2026).
 * Bảng tách: docs/design-tt-core.md mục 10. Code được chép NGUYÊN VĂN, chỉ có 2 chỗ sửa
 * có chủ đích ở theme vi (xemthem, sticky header), ghi trong PATCHES bên dưới.
 * Sau khi tách, kiểm bằng verify-parity.js.
 */
'use strict';
const fs = require('fs');
const path = require('path');

const ROOT = path.resolve(__dirname, '..');
const LEGACY = path.join(__dirname, 'legacy-2026-11-27');
const dry = process.argv.includes('--dry');

function lines(file) {
    return fs.readFileSync(file, 'utf8').replace(/^﻿/, '').replace(/\r\n?/g, '\n').split('\n');
}

// [order, id, device, file('D'|'M'), from, to, mô tả]
const PLAN = {
    generatepress_child: {
        D: 'thongthai-script.js',
        M: 'thongthai-script-mobile.js',
        mods: [
            [10, 'widget-insert', 'desktop', 'D', 1, 30, 'Chèn nội dung widget block 40 (chuyên mục) và block 42 (banner bản đồ 2025) từ widget/*.txt'],
            [20, 'top-hot', 'desktop', 'D', 32, 61, 'Block 41/50 "Xem nhiều": tải widget/block-41.txt, đổi ?v= theo mốc cron 2 giờ'],
            [30, 'load-more', 'all', 'D', 64, 347, 'Ajax Load More (JS thuần) + listing cache trang 1 (uploads/tt-listing)'],
            [40, 'iframe-touch', 'all', 'D', 349, 349, 'Gửi khóa postMessage cho iframe.thongthai_touch (code đã obfuscate từ trước, giữ nguyên)'],
            [50, 'download-image', 'all', 'D', 351, 540, 'Nút tải ảnh .downloadButton: tải ảnh, chèn XMP bản quyền'],
            [60, 'xemthem', 'all', 'D', 542, 570, 'Nút "Mở rộng/Thu gọn" cho .xemthem trên màn hình nhỏ'],
            [70, 'sticky-table-header', 'all', 'D', 571, 662, 'Sticky header cho bảng trong .thongthai-scrollable'],
            [80, 'table-filter', 'all', 'D', 664, 689, 'Ô lọc .table-filter cho bảng ngay sau nó'],
            [90, 'sortable-table', 'all', 'D', 691, 829, 'Sắp xếp cột .sortable-table (đồng bộ với sticky header)'],
            [100, 'scrollbar', 'desktop', 'D', 831, 1080, 'Thanh cuộn ngang nổi cho .thongthai-scrollable (bỏ qua máy cảm ứng)'],
            [110, 'aff-flyicon', 'all', 'D', 1083, 1101, 'Nạp uploads/hot-deal/deal_script.js (icon deal)'],
            [120, 'aff-redirect-desktop', 'desktop', 'D', 1103, 1318, 'Aff Redirect bản desktop: mở link go.diaocthongthai.com khi click/không hoạt động'],
            [121, 'aff-redirect-mobile', 'mobile', 'M', 788, 934, 'Aff Redirect bản mobile: mở link app Shopee khi chạm, link web khi không hoạt động'],
            [130, 'rightclick-cleanup', 'desktop', 'D', 1321, 1323, 'Xóa dữ liệu right-click tracking cũ trong localStorage'],
            [140, 'viewed-history', 'desktop', 'D', 1326, 1521, 'Lịch sử "Bạn đã xem" (block 43/51)'],
            [150, 'call-chat', 'all', 'D', 1524, 1692, 'Hotline, Messenger, Zalo trên trang dự án và bài bản đồ'],
            [160, 'fancybox', 'all', 'D', 1694, 1780, 'Fancybox tải khi cần'],
            [170, 'kksr-nonce', 'all', 'D', 1782, 1849, 'kk Star Ratings: lấy nonce mới trước khi chấm sao'],
        ],
    },
    generatepress_en: {
        D: 'thongthai-desktop.js',
        M: 'thongthai-mobile.js',
        mods: [
            [10, 'atlas-widget', 'all', 'D', 8, 22, 'Quick Atlas (block 23): tải widget/atlas-widget.txt'],
            [20, 'top-hot', 'desktop', 'D', 24, 53, 'Block 28 "Most viewed": tải widget/block-28.txt, đổi ?v= theo mốc cron 2 giờ'],
            [30, 'expand-collapse', 'all', 'D', 58, 66, 'Nút Show more/Show less cho .collapse-container'],
            [40, 'load-more', 'all', 'D', 68, 180, 'Ajax Load More (JS thuần)'],
            [50, 'download-image', 'all', 'D', 183, 371, 'Nút tải ảnh .downloadButton: tải ảnh, chèn XMP bản quyền'],
            [60, 'viewed-history', 'desktop', 'D', 374, 570, 'Lịch sử "Seen posts" (block 29)'],
            [70, 'fancybox', 'all', 'D', 572, 658, 'Fancybox tải khi cần'],
            [80, 'kksr-nonce', 'all', 'D', 660, 727, 'kk Star Ratings: lấy nonce mới trước khi chấm sao'],
        ],
    },
};

// Sửa có chủ đích: [theme, id, chuỗi cũ, chuỗi mới, lý do]
const PATCHES = [
    ['generatepress_child', 'xemthem',
        "if (window.innerWidth < 480 && document.querySelector('.xemthem')) {",
        "if ((TT.mobile || window.innerWidth < 480) && document.querySelector('.xemthem')) { // tt-core: bản mobile cũ không có điều kiện innerWidth",
        'mobile: luôn chạy (như thongthai-script-mobile.js); desktop: chỉ khi < 480px (như thongthai-script.js)'],
    ['generatepress_child', 'sticky-table-header',
        "\tconst topOffset = wpAdminBar ? '32px' : '0px';",
        "\tconst topOffset = (!TT.mobile && wpAdminBar) ? '32px' : '0px'; // tt-core: bản mobile cũ luôn 0px",
        'mobile: luôn 0px (như bản mobile cũ); desktop: 32px khi có admin bar'],
];

for (const theme of Object.keys(PLAN)) {
    const p = PLAN[theme];
    const src = { D: lines(path.join(LEGACY, theme, p.D)), M: lines(path.join(LEGACY, theme, p.M)) };
    const outDir = path.join(ROOT, theme, 'tt-core', 'modules');
    if (!dry) fs.mkdirSync(outDir, { recursive: true });

    for (const [order, id, device, f, from, to, desc] of p.mods) {
        let body = src[f].slice(from - 1, to).join('\n');
        for (const [t, mid, a, b] of PATCHES) {
            if (t !== theme || mid !== id) continue;
            if (!body.includes(a)) throw new Error(`${theme}/${id}: không thấy chuỗi cần sửa: ${a}`);
            body = body.replace(a, b);
        }
        const patches = PATCHES.filter(x => x[0] === theme && x[1] === id).map(x => ' *   - ' + x[4]);
        const header = [
            `/* @tt-module id=${id} device=${device} ver=1.0.0 order=${order} */`,
            '/*',
            ` * ${desc}`,
            ` * Tách nguyên văn từ ${p[f]} dòng ${from}-${to} (bản 27/11/2026).`,
            ...(patches.length ? [' * Sửa có chủ đích khi gộp desktop/mobile:', ...patches] : []),
            ' * Code chạy bên trong TT.define(...), biến không lộ ra ngoài (trừ khi gán window.xxx).',
            ' */',
        ].join('\n');
        const file = path.join(outDir, `${String(order).padStart(3, '0')}-${id}.js`);
        const text = header + '\n' + body.replace(/\s+$/, '') + '\n';
        if (dry) console.log(theme, path.basename(file), device, `${f}:${from}-${to}`, text.length);
        else fs.writeFileSync(file, text);
    }
    if (!dry) console.log(theme + ': ' + p.mods.length + ' module -> ' + path.relative(ROOT, outDir));
}
