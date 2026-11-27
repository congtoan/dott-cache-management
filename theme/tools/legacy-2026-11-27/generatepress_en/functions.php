<?php
/**
 * GeneratePress child theme functions and definitions.
 *
 * Add your custom PHP in this file.
 * Only edit this file if you have direct access to it on your server (to fix errors if they happen).
 */

/* ADD OTHER PHP FILES */
require_once dirname( __FILE__ ) . '/inc/post_listing.php';
require_once dirname( __FILE__ ) . '/inc/post_type.php';
require_once dirname( __FILE__ ) . '/inc/kksr-fresh-nonce.php'; // nonce mới cho kk Star Ratings trên trang cache lâu

/* ADD THONGTHAI-SCRIPT.JS */
function my_enqueue_scripts() {
    // Get the stylesheet directory URI and path
    $stylesheet_dir = get_stylesheet_directory_uri();
    $stylesheet_path = get_stylesheet_directory();

    if (wp_is_mobile()) {
        // Enqueue mobile script with a version based on file modification time
        $version = filemtime($stylesheet_path . '/thongthai-mobile.js');
        // Không phụ thuộc jQuery: WordPress chỉ nạp jQuery khi có plugin khác cần.
        wp_enqueue_script('thongthai-mobile', $stylesheet_dir . '/thongthai-mobile.js', array(), $version, true);
    } else {
        // Enqueue desktop script with a version based on file modification time
        $version = filemtime($stylesheet_path . '/thongthai-desktop.js');
        // Không phụ thuộc jQuery: WordPress chỉ nạp jQuery khi có plugin khác cần.
        wp_enqueue_script('thongthai-desktop', $stylesheet_dir . '/thongthai-desktop.js', array(), $version, true);
    }
}
add_action('wp_enqueue_scripts', 'my_enqueue_scripts');

/* CSE SNIPPET */
// Change Search path in the GeneratePress Theme
add_filter( 'generate_navigation_search_output', function() {
printf( 
'<form method="get" class="search-form navigation-search" action="%1$s">
<input type="search" placeholder=" Search" class="search-field" value="%2$s" name="q" title="%3$s" />
</form>', 
esc_url( home_url( '/search/' ) ), 
esc_attr( get_search_query() ), 
esc_attr_x( 'Search', 'label', 'generatepress' ) 
); 
} );
// Other theme? Use Real Time Find and Replace Plugin


/* ADD HREFLANG TAG */
function add_custom_hreflang_tag_for_post() {
    $post_types = array('post', 'map_0', 'map_1', 'map_2', 'map_3');

    if (is_singular($post_types)) {
        $post_id = get_the_ID();
        $lang_vi = get_post_meta($post_id, 'lang_vi', true);

        if ($lang_vi) {
            $lang_en = get_permalink($post_id);
            echo '<link rel="alternate" hreflang="vi" href="' . esc_url($lang_vi) . '" />' . PHP_EOL;
            echo '<link rel="alternate" hreflang="en" href="' . esc_url($lang_en) . '" />' . PHP_EOL;
        }
    }
}
add_action('wp_head', 'add_custom_hreflang_tag_for_post', 2);


  
