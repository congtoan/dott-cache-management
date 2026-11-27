/* @tt-module id=rightclick-cleanup device=desktop ver=1.0.0 order=130 */
/*
 * Xóa dữ liệu right-click tracking cũ trong localStorage
 * Tách nguyên văn từ thongthai-script.js dòng 1321-1323 (bản 27/11/2026).
 * Code chạy bên trong TT.define(...), biến không lộ ra ngoài (trừ khi gán window.xxx).
 */
//==========================================================
// Right-click tracking was removed in 0.6.2. Clear what it left in visitors' browsers.
try { localStorage.removeItem('rightClickData'); localStorage.removeItem('userIP'); } catch (e) {}
