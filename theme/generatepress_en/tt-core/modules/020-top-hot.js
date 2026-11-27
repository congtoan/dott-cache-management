/* @tt-module id=top-hot device=desktop ver=1.0.0 order=20 */
/*
 * Block 28 "Most viewed": tải widget/block-28.txt, đổi ?v= theo mốc cron 2 giờ
 * Tách nguyên văn từ thongthai-desktop.js dòng 24-53 (bản 27/11/2026).
 * Code chạy bên trong TT.define(...), biến không lộ ra ngoài (trừ khi gán window.xxx).
 */
/* Block 28: xem nhiều */
document.addEventListener('DOMContentLoaded', function() {
    const block_top_hot = document.querySelector('#block-28') ;
    if (!block_top_hot) return;

    // Lấy thời gian hiện tại (mili giây)
    const current_time_version = new Date().getTime();

    // Tính thời điểm gần nhất mà cron job chạy (cộng 15 phút)
    const twoHoursInMs = 2 * 60 * 60 * 1000; // 2 giờ = 7200000 mili giây
    const fifteenMinutesInMs = 15 * 60 * 1000; // 15 phút

    // Tính giờ lẻ gần nhất (1:00, 3:00, ..., 23:00)
    const currentDate = new Date(current_time_version);
    const hours = currentDate.getHours();
    const nearestOddHour = hours % 2 === 0 ? hours - 1 : hours; // Nếu giờ chẵn, lùi về giờ lẻ trước đó
    const fallbackDate = new Date(currentDate);
    fallbackDate.setHours(nearestOddHour, 0, 0, 0); // Đặt về mốc giờ lẻ (1:00, 3:00, ...)
    const fallback_time = fallbackDate.getTime() + fifteenMinutesInMs; // Cộng 15 phút

    // Tạo URL với query string ?v=fallback_time
    const url = `https://diaocthongthai.com/wp-content/themes/generatepress_en/widget/block-28.txt?v=${fallback_time}`;

    fetch(url)
        .then(response => response.text())
        .then(html => {
            block_top_hot.innerHTML = html;
        })
        .catch(error => console.log('Error loading block_top_hot', error));
});
