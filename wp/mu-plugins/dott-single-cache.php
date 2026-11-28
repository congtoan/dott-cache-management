<?php
/**
 * Plugin Name: DOTT single HTML cache (WP Rocket)
 * Description: Một bản cache HTML cho cả desktop và mobile. Giữ "Mobile cache" (cache_mobile = 1) để khách mobile vẫn được phục vụ cache, nhưng ép "Separate cache files for mobile" (do_caching_mobile_files = 0).
 * Version:     1.1.0
 *
 * Vì sao cần mu-plugin (WP Rocket 3.16, đọc code 27/11):
 *   - Giao diện không còn nút tắt. 2 trường này là trường ẩn, giữ nguyên giá trị khi lưu.
 *   - inc/admin/admin.php: khi lưu thiết lập, nếu cache_mobile = 1 mà do_caching_mobile_files = 0
 *     thì WP Rocket TỰ BẬT LẠI do_caching_mobile_files = 1.
 *   - Settings\Page::enable_separate_cache_files_mobile() (lúc nâng cấp) cũng bật lại.
 *   - Đặt cache_mobile = 0 thì Buffer\Tests::can_process_mobile() trả false với UA mobile:
 *     khách mobile KHÔNG được phục vụ cache qua PHP và request mobile không ghi cache.
 *   => Cách đúng là cache_mobile = 1 + do_caching_mobile_files = 0, và chặn mọi lần ghi
 *      do_caching_mobile_files = 1 bằng filter bên dưới.
 *
 * Cùng với theme tt-core (HTML giống nhau cho mọi thiết bị) và rocket-nginx
 * (/etc/nginx/extra/rocket-page-cache.conf). Repo: dott-cache-management/wp/mu-plugins/.
 */

defined( 'ABSPATH' ) || exit;

/*
 * Bật theo từng blog (1.1.0, để chuyển production từng blog: vi trước, en sau).
 *   null           : mọi blog (php81 test).
 *   array( 1 )     : chỉ blog 1 (diaocthongthai.com/); blog khác giữ tách mobile như cũ.
 *   array( 1, 2 )  : thêm blog 2 (/en/) khi chuyển xong.
 * Sau khi đổi danh sách: lưu lại thiết lập WP Rocket của blog đó (hoặc chạy rocket_generate_config_file())
 * để file wp-rocket-config/<host>[.<blog>].php được tạo lại với do_caching_mobile_files = 0.
 */
if ( ! defined( 'DOTT_SINGLE_CACHE_BLOGS' ) ) {
	define( 'DOTT_SINGLE_CACHE_BLOGS', null );
}

/** Ép giá trị trước khi WP Rocket ghi option (UI, nâng cấp, AJAX, WP-CLI). */
function dott_single_cache_force( $value ) {
	$blogs = DOTT_SINGLE_CACHE_BLOGS;
	if ( is_array( $blogs ) && ! in_array( get_current_blog_id(), $blogs, true ) ) {
		return $value; // blog chưa chuyển: giữ nguyên
	}
	if ( is_array( $value ) ) {
		$value['cache_mobile']            = 1;
		$value['do_caching_mobile_files'] = 0;
	}
	return $value;
}
add_filter( 'pre_update_option_wp_rocket_settings', 'dott_single_cache_force', PHP_INT_MAX );

/** Đọc option cũng trả về giá trị đã ép, phòng khi DB còn giá trị cũ. */
add_filter( 'option_wp_rocket_settings', 'dott_single_cache_force', PHP_INT_MAX );
