<?php
/**
 * GeneratePress child theme functions and definitions.
 *
 * Add your custom PHP in this file.
 * Only edit this file if you have direct access to it on your server (to fix errors if they happen).
 */

/* ADD OTHER PHP FILES */
require_once dirname( __FILE__ ) . '/inc/post_listing.php';
require_once dirname( __FILE__ ) . '/inc/sidebar.php';
require_once dirname( __FILE__ ) . '/inc/post_type.php';
require_once dirname( __FILE__ ) . '/inc/kksr-fresh-nonce.php'; // nonce mới cho kk Star Ratings trên trang cache lâu
require_once dirname( __FILE__ ) . '/inc/listing-cache.php'; // cache file tĩnh trang 1 cho listing tải chậm

/* ADD THONGTHAI-SCRIPT.JS */
function my_enqueue_scripts() {
    // Get the stylesheet directory URI and path
    $stylesheet_dir = get_stylesheet_directory_uri();
    $stylesheet_path = get_stylesheet_directory();

    // Check if the user is on a mobile device
    if (wp_is_mobile()) {
        // Enqueue the mobile script with a version based on file modification time
        $version = filemtime($stylesheet_path . '/thongthai-script-mobile.js');
        // Không phụ thuộc jQuery: WordPress chỉ nạp jQuery khi có plugin khác cần.
        wp_enqueue_script('thongthai-script-mobile-js', $stylesheet_dir . '/thongthai-script-mobile.js', array(), $version, true);

    } else {
        // Enqueue the regular script with a version based on file modification time
        $version = filemtime($stylesheet_path . '/thongthai-script.js');
        // Không phụ thuộc jQuery: WordPress chỉ nạp jQuery khi có plugin khác cần.
        wp_enqueue_script('thongthai-script', $stylesheet_dir . '/thongthai-script.js', array(), $version, true);
    }
}
add_action('wp_enqueue_scripts', 'my_enqueue_scripts');


/* CSE SNIPPET */

// Change Search path in the GeneratePress Theme
add_action('init', function() {
    remove_action('generate_inside_search_modal', 'generate_do_search_fields');
});

add_action( 'generate_inside_search_modal', 'custom_generate_search_fields' );

function custom_generate_search_fields() {
    ?>
    <form role="search" method="get" class="search-modal-form" action="<?php echo esc_url( home_url( '/search/' ) ); ?>">
        <label for="search-modal-input" class="screen-reader-text"><?php echo apply_filters( 'generate_search_label', _x( 'Search for:', 'label', 'generatepress' ) ); ?></label>
        <div class="search-modal-fields">
            <input id="search-modal-input" type="search" class="search-field" placeholder="<?php echo esc_attr( apply_filters( 'generate_search_placeholder', _x( 'Search &hellip;', 'placeholder', 'generatepress' ) ) ); ?>" value="<?php echo get_search_query(); ?>" name="q" />
            <button aria-label="<?php echo esc_attr( apply_filters( 'generate_search_button', _x( 'Search', 'submit button', 'generatepress' ) ) ); ?>"><?php echo generate_get_svg_icon( 'search' ); ?></button>
        </div>
        <?php do_action( 'generate_inside_search_modal_form' ); ?>
    </form>
    <?php
}


/* PHÂN CÁCH TAG*/
add_filter( 'generate_term_separator', function() {
    return ', ';
} );


/* WPFORMS - REGISTER SMART TAG */
// https://wpforms.com/developers/how-to-create-a-smart-tag-from-an-acf-field/

function wpf_dev_register_smarttag( $tags ) {
    // Key is the tag, item is the tag name.
    $tags['link_download'] = 'Link download Quy hoạch'; // Change 'link_download' to the desired tag name
    return $tags;
}

add_filter( 'wpforms_smart_tags', 'wpf_dev_register_smarttag' );


/* WPFORMS - SUPPORT SMART TAG */
function wpf_dev_process_smarttag( $content, $tag ) {
    // Only run if it is our desired tag.
    if ( 'link_download' === $tag ) {
        // Get the field value from ACF
        $my_acf_field = get_field( 'link_download', get_the_ID() ); // Change 'link_download' to your ACF field name
        // Replace the tag with our field value.
        $content = str_replace( '{link_download}', $my_acf_field, $content );
    }
    return $content;
}

add_filter( 'wpforms_smart_tag_process', 'wpf_dev_process_smarttag', 10, 2 );


/* ADD HREFLANG TAG */
function add_custom_hreflang_tag_for_post() {
    if (is_singular('post')) {
        $post_id = get_the_ID();
        $lang_en = get_post_meta($post_id, 'lang_en', true);

        if ($lang_en) {
            $lang_vi = get_permalink($post_id);
            echo '<link rel="alternate" hreflang="vi" href="' . esc_url($lang_vi) . '" />' . PHP_EOL;
            echo '<link rel="alternate" hreflang="en" href="' . esc_url($lang_en) . '" />' . PHP_EOL;
        }
    }
}
add_action('wp_head', 'add_custom_hreflang_tag_for_post', 2);
