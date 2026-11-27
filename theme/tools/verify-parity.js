#!/usr/bin/env node
/*
 * verify-parity.js – kiểm tra module tt-core so với JS cũ (chạy trên PC, Node 18+).
 *
 *   node theme/tools/verify-parity.js
 *
 * 1. Tương đương mã nguồn: với mỗi theme và mỗi thiết bị, ghép các module chạy trên thiết bị đó
 *    (theo order), bỏ phần header, rồi so với file JS cũ của thiết bị đó.
 *    So theo dòng, bỏ qua dòng trống và khoảng trắng cuối dòng. Chỉ được lệch đúng các dòng
 *    đã sửa có chủ đích (EXPECTED bên dưới).
 * 2. Cú pháp: từng module sau khi bọc TT.define(...) phải parse được (vm.Script).
 * 3. Chạy thử: base.js + bundle mô phỏng (bọc giống tt-core.php) trong môi trường DOM giả,
 *    cho desktop và mobile. Kiểm tra TT.list() đúng tập module theo thiết bị.
 * Thoát với mã 1 nếu có lỗi.
 */
'use strict';
const fs = require('fs');
const path = require('path');
const vm = require('vm');

const ROOT = path.resolve(__dirname, '..');
const LEGACY = path.join(__dirname, 'legacy-2026-11-27');
const FILES = {
    generatepress_child: { desktop: 'thongthai-script.js', mobile: 'thongthai-script-mobile.js' },
    generatepress_en: { desktop: 'thongthai-desktop.js', mobile: 'thongthai-mobile.js' },
};
// Dòng được phép lệch: [theme, device, dòng trong module (sau trim), dòng trong file cũ (sau trim)]
const EXPECTED = [
    ['generatepress_child', 'desktop',
        "if ((TT.mobile || window.innerWidth < 480) && document.querySelector('.xemthem')) { // tt-core: bản mobile cũ không có điều kiện innerWidth",
        "if (window.innerWidth < 480 && document.querySelector('.xemthem')) {"],
    ['generatepress_child', 'mobile',
        "if ((TT.mobile || window.innerWidth < 480) && document.querySelector('.xemthem')) { // tt-core: bản mobile cũ không có điều kiện innerWidth",
        "if (document.querySelector('.xemthem')) {"],
    ['generatepress_child', 'desktop',
        "const topOffset = (!TT.mobile && wpAdminBar) ? '32px' : '0px'; // tt-core: bản mobile cũ luôn 0px",
        "const topOffset = wpAdminBar ? '32px' : '0px';"],
    ['generatepress_child', 'mobile',
        "const topOffset = (!TT.mobile && wpAdminBar) ? '32px' : '0px'; // tt-core: bản mobile cũ luôn 0px",
        "const topOffset = '0px';"],
];

let failed = 0;
const fail = (msg) => { failed++; console.log('  ❌ ' + msg); };
const ok = (msg) => console.log('  ✅ ' + msg);

const norm = (t) => t.replace(/^﻿/, '').replace(/\r\n?/g, '\n');
const nonBlank = (t) => norm(t).split('\n').map((l) => l.replace(/\s+$/, '')).filter((l) => l.trim() !== '');

function parseHeader(code) {
    const m = /@tt-module\b([^\n]*)/.exec(code.slice(0, 1024));
    if (!m) return null;
    const h = { device: 'all', order: 500, ready: false };
    for (const [, k, v] of m[1].matchAll(/([a-z]+)=([^\s*]+)/g)) h[k] = v;
    h.order = parseInt(h.order, 10);
    h.ready = h.ready === '1' || h.ready === 'true';
    return h;
}

/* Bỏ header của module: dòng @tt-module và khối comment mô tả ngay sau nó. */
function stripHeader(code) {
    const ls = norm(code).split('\n');
    let i = 1;
    if (ls[i] && ls[i].trim() === '/*') {
        while (i < ls.length && ls[i].trim() !== '*/') i++;
        i++;
    }
    return ls.slice(i).join('\n');
}

function loadModules(theme) {
    const dir = path.join(ROOT, theme, 'tt-core', 'modules');
    return fs.readdirSync(dir).filter((f) => f.endsWith('.js')).map((f) => {
        const code = fs.readFileSync(path.join(dir, f), 'utf8');
        const h = parseHeader(code);
        if (!h) throw new Error(f + ': thiếu @tt-module');
        return { file: f, ...h, code: norm(code) };
    }).sort((a, b) => a.order - b.order || a.id.localeCompare(b.id));
}

/* Bọc giống tt_core_wrap() trong tt-core.php */
function wrap(m) {
    const opt = { ver: m.ver, device: m.device };
    if (m.ready) opt.ready = true;
    return `/* --- module ${m.id} (theme) --- */\nTT.define(${JSON.stringify(m.id)},${JSON.stringify(opt)},function(TT){\n${m.code.replace(/\s+$/, '')}\n});\n`;
}

/* ---- DOM giả: mọi thuộc tính/hàm đều trả về một stub, không bao giờ ném lỗi ---- */
function makeStub(name) {
    const fn = function () { return makeStub(name + '()'); };
    return new Proxy(fn, {
        get(t, k) {
            if (k === Symbol.toPrimitive) return () => '';
            if (k === Symbol.iterator) return function* () { };
            if (k === 'length') return 0;
            if (k === 'then') return undefined; // không phải Promise
            if (k === 'forEach') return () => { };
            return makeStub(name + '.' + String(k));
        },
        apply() { return makeStub(name + '()'); },
        construct() { return makeStub('new ' + name); },
        set() { return true; },
        has() { return true; },
    });
}

function runBundle(bundle, ua, uaMobile) {
    const listeners = [];
    const classes = new Set();
    const storage = () => { const s = {}; return { getItem: (k) => (k in s ? s[k] : null), setItem: (k, v) => { s[k] = String(v); }, removeItem: (k) => { delete s[k]; } }; };
    const document = new Proxy({
        readyState: 'loading',
        addEventListener: (t) => listeners.push(t),
        documentElement: { classList: { add: (c) => classes.add(c) } },
        querySelector: () => null,
        querySelectorAll: () => [],
        getElementsByClassName: () => [],
        getElementById: () => null,
        createElement: () => makeStub('el'),
        head: makeStub('head'),
        body: makeStub('body'),
    }, { get: (t, k) => (k in t ? t[k] : makeStub('document.' + String(k))) });
    const errors = [];
    const ctx = {
        document,
        navigator: { userAgent: ua, userAgentData: uaMobile === undefined ? undefined : { mobile: uaMobile } },
        location: { search: '', href: 'https://diaocthongthai.com/x/', origin: 'https://diaocthongthai.com' },
        localStorage: storage(),
        sessionStorage: storage(),
        console: { log() { }, error: (...a) => errors.push(a.map(String).join(' ')), warn() { } },
        setTimeout: () => 0, clearTimeout: () => { },
        addEventListener: () => { },
        IntersectionObserver: function () { return makeStub('io'); },
        MutationObserver: function () { return makeStub('mo'); },
        ResizeObserver: function () { return makeStub('ro'); },
        fetch: () => new Promise(() => { }),
        innerWidth: 1280, innerHeight: 800, pageYOffset: 0,
        Promise, JSON, Math, Date, Object, Array, String, Number, RegExp, Error, Map, Set, URLSearchParams,
        parseInt, parseFloat, isNaN, encodeURIComponent, decodeURIComponent,
    };
    ctx.window = ctx;
    vm.createContext(ctx);
    new vm.Script(bundle, { filename: 'core.js' }).runInContext(ctx);
    return { TT: ctx.TT, classes: [...classes], listeners, errors };
}

const base = fs.readFileSync(path.join(ROOT, 'engine', 'base.js'), 'utf8');
try { new vm.Script(base); ok('engine/base.js: cú pháp hợp lệ'); } catch (e) { fail('engine/base.js: ' + e.message); }

for (const theme of Object.keys(FILES)) {
    console.log('\n=== ' + theme);
    const mods = loadModules(theme);
    console.log(`  ${mods.length} module: ` + mods.map((m) => `${m.id}(${m.device})`).join(', '));

    // 1. Tương đương mã nguồn
    for (const dev of ['desktop', 'mobile']) {
        const mine = nonBlank(mods.filter((m) => m.device === 'all' || m.device === dev).map((m) => stripHeader(m.code)).join('\n'));
        const orig = nonBlank(fs.readFileSync(path.join(LEGACY, theme, FILES[theme][dev]), 'utf8'));
        const exp = EXPECTED.filter((e) => e[0] === theme && e[1] === dev);
        // Diff theo dãy con chung dài nhất (LCS). Dòng sửa có chủ đích được coi là khớp.
        const same = (a, b) => a === b || exp.some((x) => a.trim() === x[2] && b.trim() === x[3]);
        const n = mine.length, m2 = orig.length, W = m2 + 1;
        const L = new Uint16Array((n + 1) * W);
        for (let i = n - 1; i >= 0; i--) for (let j = m2 - 1; j >= 0; j--) {
            L[i * W + j] = same(mine[i], orig[j]) ? L[(i + 1) * W + j + 1] + 1 : Math.max(L[(i + 1) * W + j], L[i * W + j + 1]);
        }
        const diffs = [];
        let i = 0, j = 0;
        while (i < n || j < m2) {
            if (i < n && j < m2 && same(mine[i], orig[j])) {
                const e = exp.find((x) => mine[i].trim() === x[2] && orig[j].trim() === x[3]);
                if (e) e.used = true;
                i++; j++;
            } else if (j < m2 && (i >= n || L[i * W + j + 1] >= L[(i + 1) * W + j])) {
                diffs.push(`chỉ có trong file cũ, dòng ${j + 1}: ${orig[j].trim()}`); j++;
            } else {
                diffs.push(`chỉ có trong module: ${mine[i].trim()}`); i++;
            }
        }
        const unused = exp.filter((e) => !e.used);
        const onlyHeaderComments = diffs.every((d) => /^chỉ có trong file cũ, dòng \d+: (\/\*|\/\/)/.test(d));
        if (!diffs.length && !unused.length) ok(`${dev}: khớp ${orig.length} dòng (không tính dòng trống), ${exp.length} dòng sửa có chủ đích`);
        else if (onlyHeaderComments && !unused.length) {
            ok(`${dev}: khớp, ${exp.length} dòng sửa có chủ đích; ${diffs.length} dòng comment đầu file cũ không đưa vào module:`);
            diffs.forEach((d) => console.log('       · ' + d));
        } else {
            fail(`${dev}: ${diffs.length} chỗ lệch` + (unused.length ? `, ${unused.length} dòng sửa dự kiến không thấy` : ''));
            diffs.slice(0, 15).forEach((d) => console.log('       · ' + d));
        }
    }

    // 2. Cú pháp từng module sau khi bọc
    let synOk = 0;
    for (const m of mods) {
        try { new vm.Script(wrap(m), { filename: m.file }); synOk++; } catch (e) { fail(`${m.file}: ${e.message}`); }
    }
    if (synOk === mods.length) ok(`cú pháp: ${synOk}/${mods.length} module hợp lệ sau khi bọc TT.define`);

    // 3. Chạy thử bundle
    const bundle = base + '\nwindow.TT&&(window.TT.build={"ver":"test","theme":"' + theme + '"});\n' + mods.map(wrap).join('');
    const cases = [
        ['desktop (Chrome Windows)', 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/131 Safari/537.36', undefined, 'desktop'],
        ['mobile (iPhone Safari)', 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Version/17.0 Mobile/15E148 Safari/604.1', undefined, 'mobile'],
        ['mobile (Android Chrome, UA-CH)', 'Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 Chrome/131 Mobile Safari/537.36', true, 'mobile'],
        ['iPad (UA kiểu Mac) = desktop', 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/17.0 Safari/605.1.15', undefined, 'desktop'],
    ];
    for (const [label, ua, ch, dev] of cases) {
        try {
            const r = runBundle(bundle, ua, ch);
            const list = r.TT.list();
            const want = mods.filter((m) => m.device === 'all' || m.device === dev).map((m) => m.id).sort();
            const got = Object.keys(list.ran).sort();
            const good = r.TT.device === dev && JSON.stringify(want) === JSON.stringify(got) && r.classes.includes('tt-' + dev);
            if (good && !r.TT.errors.length) ok(`chạy thử ${label}: ${got.length} module chạy, ${Object.keys(list.skipped).length} bỏ qua, 0 lỗi`);
            else fail(`chạy thử ${label}: device=${r.TT.device} chạy=[${got}] cần=[${want}] lỗi=${r.TT.errors.map((e) => e.id + ': ' + e.error).join('; ')}`);
        } catch (e) {
            fail(`chạy thử ${label}: ${e.stack}`);
        }
    }
}

console.log(failed ? `\n${failed} lỗi.` : '\nTất cả đều đạt.');
process.exit(failed ? 1 : 0);
