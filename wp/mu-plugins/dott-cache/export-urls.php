<?php
/**
 * export-urls.php: in ra mọi URL cần cache của một website (mọi blog của multisite trên cùng host).
 * Đặt cạnh prefork-render.php trong wp-content/mu-plugins/dott-cache/. WordPress không tự nạp file trong
 * thư mục con, và script tự thoát nếu không chạy bằng CLI.
 *
 * Bình thường được gọi từ VPS phụ:   ssh main dott-prefork <domain> --export-urls [--since=UNIX]
 * Chạy tay (bằng user của site):     php export-urls.php --root=<public_html> --host=<domain> [--since=UNIX]
 *
 * Vì sao không dùng nhiều lệnh `wp post list` như tool cũ: mỗi lệnh WP-CLI phải nạp lại WordPress
 * (khoảng 0,5–1 s với 50 plugin). Script này nạp WordPress MỘT lần cho mỗi blog, lấy ID bằng một
 * query SQL, rồi gọi get_permalink() theo lô (có nạp sẵn cache của post).
 * Mỗi blog chạy trong một tiến trình PHP riêng, vì plugin bật theo từng blog (custom post type,
 * permalink): switch_to_blog() không nạp plugin của blog kia nên permalink có thể sai.
 *
 * Xuất ra:
 *   - bài đơn (mọi post type public, trừ attachment) đang publish;
 *   - archive của mọi taxonomy public, kèm phân trang /page/N/ (N = ceil(count / posts_per_page));
 *   - archive của post type có has_archive, kèm phân trang;
 *   - trang chủ của blog.
 * Lọc (taxonomy nào, exclude_patterns) làm ở VPS phụ theo config, để đổi quy tắc không phải sửa site.
 * --since=UNIX: chỉ xuất bài đơn có post_modified_gmt >= since (chạy định kỳ), kèm id các term của
 * bài, để VPS phụ biết archive nào cần làm mới. Không xuất archive và trang chủ.
 *
 * stdout, mỗi dòng có một trong 3 dạng:
 *   U {"p":path,"b":blog_id,"bp":blog_path,"k":kind,"t":type,"id":id,"pg":page,"m":modified_unix,"terms":[...]}
 *     kind: single | term | pt_archive | home
 *   # log
 *   DONE blogs=N urls=M
 */

if ( PHP_SAPI !== 'cli' ) {
	exit( 1 );
}

$opt = getopt( '', array( 'root:', 'host:', 'since::', 'blog-path::', 'terms::' ) );
if ( empty( $opt['root'] ) || empty( $opt['host'] ) ) {
	fwrite( STDOUT, "# thiếu --root/--host\nDONE blogs=0 urls=0\n" );
	exit( 2 );
}
$ex_root  = rtrim( $opt['root'], '/' );
$ex_host  = strtolower( $opt['host'] );
$ex_since = isset( $opt['since'] ) ? max( 0, (int) $opt['since'] ) : 0;
$ex_blog  = $opt['blog-path'] ?? null; // có giá trị: tiến trình con, chỉ xuất một blog
// --terms=BLOG:TAXONOMY:TERM_ID,...: chỉ xuất bài đơn thuộc các term này (gồm term con, giống trang
// archive), dùng cho "update -segment". Không xuất archive và trang chủ.
$ex_terms = array();
foreach ( array_filter( explode( ',', (string) ( $opt['terms'] ?? '' ) ) ) as $t ) {
	if ( preg_match( '/^(\d+):([a-z0-9_-]+):(\d+)$/', $t, $m ) ) {
		$ex_terms[] = array( (int) $m[1], $m[2], (int) $m[3] );
	}
}

// Request giả để nạp WordPress đúng blog (multisite theo thư mục con), không có file cache.
$boot = ( null === $ex_blog ? '/' : rtrim( $ex_blog, '/' ) . '/' ) . 'dott-export-urls-' . getmypid() . '/';
$_SERVER['HTTP_HOST']      = $ex_host;
$_SERVER['SERVER_NAME']    = $ex_host;
$_SERVER['HTTPS']          = 'on';
$_SERVER['SERVER_PORT']    = 443;
$_SERVER['REQUEST_METHOD'] = 'GET';
$_SERVER['REQUEST_URI']    = $boot;
$_SERVER['REMOTE_ADDR']    = '127.0.0.1';
$_SERVER['HTTP_USER_AGENT'] = 'dott-export-urls';
define( 'DONOTCACHEPAGE', true );
$t0 = microtime( true );
ob_start();
require $ex_root . '/wp-load.php';
while ( ob_get_level() > 0 ) {
	ob_end_clean();
}
$boot_ms = (int) ( ( microtime( true ) - $t0 ) * 1000 );

function ex_line( $s ) {
	fwrite( STDOUT, $s . "\n" );
}

// ---- Tiến trình cha: tìm các blog của host, chạy mỗi blog một tiến trình PHP mới ----
if ( null === $ex_blog ) {
	$paths = array( '/' );
	if ( is_multisite() ) {
		$paths = array();
		foreach ( get_sites( array( 'number' => 1000, 'deleted' => 0, 'archived' => 0, 'spam' => 0 ) ) as $s ) {
			if ( strtolower( $s->domain ) === $ex_host ) {
				$paths[] = $s->path;
			}
		}
	}
	ex_line( sprintf( '# %d blog: %s (boot %d ms)', count( $paths ), implode( ' ', $paths ), $boot_ms ) );
	$total = 0;
	foreach ( $paths as $bp ) {
		$cmd = escapeshellarg( PHP_BINARY ) . ' ' . escapeshellarg( __FILE__ )
			. ' --root=' . escapeshellarg( $ex_root ) . ' --host=' . escapeshellarg( $ex_host )
			. ' --blog-path=' . escapeshellarg( $bp ) . ( $ex_since ? ' --since=' . $ex_since : '' )
			. ( $ex_terms ? ' --terms=' . escapeshellarg( (string) $opt['terms'] ) : '' ) . ' 2>/dev/null';
		$p = popen( $cmd, 'r' );
		while ( false !== ( $l = fgets( $p ) ) ) {
			$l = rtrim( $l, "\n" );
			if ( 0 === strpos( $l, 'U ' ) ) {
				$total++;
				ex_line( $l );
			} elseif ( 0 === strpos( $l, '# ' ) ) {
				ex_line( $l );
			}
		}
		$st = pclose( $p );
		if ( 0 !== $st && 137 !== $st ) { // 137 = con tự kết thúc bằng SIGKILL như dự kiến
			ex_line( "# blog $bp: tiến trình con thoát với mã $st" );
		}
	}
	ex_line( sprintf( 'DONE blogs=%d urls=%d', count( $paths ), $total ) );
	fflush( STDOUT );
	posix_kill( getmypid(), 9 ); // tiến trình cha cũng đã nạp WordPress: bỏ qua shutdown hook của plugin
}

// ---- Tiến trình con: một blog ----
global $wpdb;
$blog_id = get_current_blog_id();
$ex_bp   = is_multisite() ? get_blog_details( $blog_id )->path : '/';
$ppp     = max( 1, (int) get_option( 'posts_per_page', 10 ) );
$n       = 0;
$t1      = microtime( true );

function ex_path( $url ) {
	global $ex_host;
	$u = wp_parse_url( $url );
	if ( empty( $u['host'] ) || strtolower( $u['host'] ) !== $ex_host || ! empty( $u['query'] ) ) {
		return null; // link ra host khác hoặc có query string (?p=123): không cache được
	}
	$p = $u['path'] ?? '/';
	return '/' === substr( $p, -1 ) ? $p : $p . '/';
}
function ex_out( array $r ) {
	global $n, $ex_bp;
	$n++;
	$r['bp'] = $ex_bp; // path của blog (vd "/en/"), để VPS phụ áp quy tắc lọc theo blog
	ex_line( 'U ' . json_encode( $r, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE ) );
}

// Bài đơn
$types = array_values( array_diff( get_post_types( array( 'public' => true ) ), array( 'attachment' ) ) );
if ( $ex_terms ) {
	// Chế độ --terms: bài thuộc term (của blog này), rồi dừng.
	foreach ( $ex_terms as list( $tb, $tax, $tid ) ) {
		if ( $tb !== $blog_id || ! taxonomy_exists( $tax ) ) {
			continue;
		}
		$ids = get_posts( array(
			'post_type' => $types, 'post_status' => 'publish', 'posts_per_page' => -1, 'fields' => 'ids',
			'no_found_rows' => true, 'orderby' => 'ID', 'order' => 'ASC',
			'tax_query' => array( array( 'taxonomy' => $tax, 'field' => 'term_id', 'terms' => $tid, 'include_children' => true ) ),
		) );
		foreach ( array_chunk( $ids, 500 ) as $chunk ) {
			_prime_post_caches( $chunk, false, false );
			foreach ( $chunk as $id ) {
				$path = ex_path( get_permalink( $id ) );
				if ( null !== $path ) {
					ex_out( array( 'p' => $path, 'b' => $blog_id, 'k' => 'single', 't' => get_post_type( $id ), 'id' => (int) $id, 'seg' => "$tax:$tid" ) );
				}
			}
		}
	}
	$types = array(); // không xuất phần còn lại
	$ex_since = 1;    // bỏ qua archive và trang chủ ở dưới
}
if ( $types ) {
	$in  = implode( ',', array_map( function ( $t ) use ( $wpdb ) {
		return $wpdb->prepare( '%s', $t );
	}, $types ) );
	$sql = "SELECT ID, post_type, UNIX_TIMESTAMP(post_modified_gmt) AS m FROM {$wpdb->posts}
		WHERE post_status = 'publish' AND post_type IN ($in)" . ( $ex_since ? $wpdb->prepare( ' AND post_modified_gmt >= FROM_UNIXTIME(%d)', $ex_since ) : '' ) . ' ORDER BY ID';
	$rows = $wpdb->get_results( $sql );
	$taxes = get_taxonomies( array( 'public' => true ) );
	foreach ( array_chunk( $rows, 500 ) as $chunk ) {
		$ids = wp_list_pluck( $chunk, 'ID' );
		_prime_post_caches( $ids, true, false ); // nạp post và term của cả lô một lần
		foreach ( $chunk as $r ) {
			$path = ex_path( get_permalink( (int) $r->ID ) );
			if ( null === $path ) {
				continue;
			}
			$o = array( 'p' => $path, 'b' => $blog_id, 'k' => 'single', 't' => $r->post_type, 'id' => (int) $r->ID, 'm' => (int) $r->m );
			if ( $ex_since ) {
				$o['terms'] = wp_get_object_terms( (int) $r->ID, $taxes, array( 'fields' => 'ids' ) );
			}
			ex_out( $o );
		}
		wp_cache_flush_runtime();
	}
}

if ( ! $ex_since ) {
	// Trang chủ
	$hp = ex_path( home_url( '/' ) );
	if ( $hp ) {
		ex_out( array( 'p' => $hp, 'b' => $blog_id, 'k' => 'home', 't' => '', 'id' => 0, 'pg' => 1 ) );
	}
	// Archive của taxonomy, có phân trang
	foreach ( get_taxonomies( array( 'public' => true ), 'objects' ) as $tax ) {
		if ( empty( $tax->rewrite ) ) {
			continue;
		}
		$terms = get_terms( array( 'taxonomy' => $tax->name, 'hide_empty' => true ) );
		if ( is_wp_error( $terms ) ) {
			continue;
		}
		foreach ( $terms as $t ) {
			$base = ex_path( get_term_link( $t ) );
			if ( null === $base ) {
				continue;
			}
			$pages = max( 1, (int) ceil( $t->count / $ppp ) );
			for ( $pg = 1; $pg <= $pages; $pg++ ) {
				ex_out( array( 'p' => 1 === $pg ? $base : $base . "page/$pg/", 'b' => $blog_id, 'k' => 'term', 't' => $tax->name, 'id' => (int) $t->term_id, 'pg' => $pg ) );
			}
		}
	}
	// Archive của post type
	foreach ( get_post_types( array( 'public' => true, 'has_archive' => true ), 'objects' ) as $pt ) {
		$link = get_post_type_archive_link( $pt->name );
		$base = $link ? ex_path( $link ) : null;
		if ( null === $base ) {
			continue;
		}
		$cnt   = (int) ( wp_count_posts( $pt->name )->publish ?? 0 );
		$pages = max( 1, (int) ceil( $cnt / $ppp ) );
		for ( $pg = 1; $pg <= $pages; $pg++ ) {
			ex_out( array( 'p' => 1 === $pg ? $base : $base . "page/$pg/", 'b' => $blog_id, 'k' => 'pt_archive', 't' => $pt->name, 'id' => 0, 'pg' => $pg ) );
		}
	}
}
ex_line( sprintf( '# blog %d (%s): %d URL, boot %d ms, xuất %.1f s, RAM tối đa %d MB',
	$blog_id, home_url( '/' ), $n, $boot_ms, microtime( true ) - $t1, memory_get_peak_usage( true ) / 1048576 ) );
fflush( STDOUT );
posix_kill( getmypid(), 9 ); // bỏ qua shutdown hook của plugin
