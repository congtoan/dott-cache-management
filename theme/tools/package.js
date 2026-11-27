#!/usr/bin/env node
/*
 * package.js – gom file cần deploy cho từng theme vào theme/dist/<theme>/ (chạy trên PC).
 *
 *   node theme/tools/package.js
 *
 * Kết quả (chỉ là file MỚI/ĐỔI, chép đè lên thư mục theme trên server):
 *   dist/<theme>/functions.php
 *   dist/<theme>/LEGACY-FILES.md
 *   dist/<theme>/tt-core/{base.js, tt-core.php, vendor/, modules/}
 * KHÔNG có core.js: file build do server tự tạo (Generate). Đừng chép core.js từ nơi khác.
 * Trước khi gom: chạy verify-parity.js và node --check cho base.js, dừng nếu lỗi.
 */
'use strict';
const fs = require('fs');
const path = require('path');
const { execFileSync } = require('child_process');

const ROOT = path.resolve(__dirname, '..');
const DIST = path.join(ROOT, 'dist');
const THEMES = ['generatepress_child', 'generatepress_en'];

execFileSync(process.execPath, [path.join(__dirname, 'verify-parity.js')], { stdio: 'inherit' });

function copyDir(src, dst) {
    fs.mkdirSync(dst, { recursive: true });
    for (const e of fs.readdirSync(src, { withFileTypes: true })) {
        const s = path.join(src, e.name), d = path.join(dst, e.name);
        if (e.isDirectory()) copyDir(s, d);
        else fs.writeFileSync(d, fs.readFileSync(s));
    }
}

fs.rmSync(DIST, { recursive: true, force: true });
for (const t of THEMES) {
    const out = path.join(DIST, t);
    copyDir(path.join(ROOT, t), out);                       // functions.php, LEGACY-FILES.md, tt-core/modules
    copyDir(path.join(ROOT, 'engine'), path.join(out, 'tt-core'));  // base.js, tt-core.php, vendor/
    const hasVendor = fs.existsSync(path.join(out, 'tt-core', 'vendor', 'peast'));
    console.log(`${t}: ${path.relative(ROOT, out)}${hasVendor ? '' : '  (CHƯA có vendor/: build sẽ không kiểm cú pháp, không minify)'}`);
}
