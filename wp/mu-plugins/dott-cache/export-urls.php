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
 *   - archive của mọi taxonomy public, kèm phân trang /page/N/ (N = max_num_pages của truy vấn chính, xem
 *     ex_term_pages(): gồm bài của term con và post type do theme chèn vào, không dùng term->count);
 *   - archive của post type có has_archive, kèm phân trang;
 *   - trang chủ của blog.
 * Lọc (taxonomy nào, exclude_patterns) làm ở VPS phụ theo config, để đổi quy tắc không phải sửa site.
 * --ids=BLOG:pID,BLOG:t:TAX:ID: trạng thái hiện tại của các bài / term vừa đổi (xem chú thích ở dưới).
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

$opt = getopt( '', array( 'root:', 'host:', 'since::', 'blog-path::', 'terms::', 'ids::' ) );
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
// --ids=BLOG:pID,BLOG:t:TAXONOMY:ID,...: trạng thái HIỆN TẠI của các bài / term vừa đổi (dott-cache serve).
// Mỗi mục trả các dòng U có thêm "q" (mục được hỏi):
//   bài còn publish: bài (k=single), trang 1 của mọi term public của bài VÀ term tổ tiên (chuyên mục cha)
//     (k=term, "pages"=số trang), trang chủ;
//   term còn (count > 0): trang 1 (k=term, "pages");
//   không còn: {"k":"gone"}.
$ex_ids = array(); // blog_id => list of [kind, tax, id, q]
foreach ( array_filter( explode( ',', (string) ( $opt['ids'] ?? '' ) ) ) as $t ) {
	if ( preg_match( '/^(\d+):p(\d+)$/', $t, $m ) ) {
		$ex_ids[ (int) $m[1] ][] = array( 'p', '', (int) $m[2], 'p' . $m[2] );
	} elseif ( preg_match( '/^(\d+):t:([a-z0-9_-]+):(\d+)$/', $t, $m ) ) {
		$ex_ids[ (int) $m[1] ][] = array( 't', $m[2], (int) $m[3], 't:' . $m[2] . ':' . $m[3] );
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
	if ( $ex_ids ) {
		// Map blog_id → path của mọi blog trên host, để VPS phụ biết path nào thuộc blog nào.
		$map = array( '1' => '/' );
		if ( is_multisite() ) {
			$map = array();
			foreach ( get_sites( array( 'number' => 1000, 'deleted' => 0, 'archived' => 0, 'spam' => 0 ) ) as $s ) {
				if ( strtolower( $s->domain ) === $ex_host ) {
					$map[ (string) $s->blog_id ] = $s->path;
				}
			}
		}
		ex_line( 'BLOGS ' . json_encode( $map, JSON_UNESCAPED_SLASHES ) );
	}
	if ( $ex_ids && is_multisite() ) {
		// Chế độ --ids: chỉ nạp các blog có trong danh sách.
		$want = array();
		foreach ( array_keys( $ex_ids ) as $bid ) {
			$d = get_blog_details( $bid );
			if ( $d && strtolower( $d->domain ) === $ex_host ) {
				$want[] = $d->path;
			}
		}
		$paths = array_values( array_intersect( $paths, $want ) );
	}
	foreach ( $paths as $bp ) {
		$cmd = escapeshellarg( PHP_BINARY ) . ' ' . escapeshellarg( __FILE__ )
			. ' --root=' . escapeshellarg( $ex_root ) . ' --host=' . escapeshellarg( $ex_host )
			. ' --blog-path=' . escapeshellarg( $bp ) . ( $ex_since ? ' --since=' . $ex_since : '' )
			. ( $ex_terms ? ' --terms=' . escapeshellarg( (string) $opt['terms'] ) : '' )
			. ( $ex_ids ? ' --ids=' . escapeshellarg( (string) $opt['ids'] ) : '' ) . ' 2>/dev/null';
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

/**
 * Số trang của archive một term, đúng như WordPress phục vụ cho khách.
 *
 * KHÔNG dùng ceil(term->count / posts_per_page): term->count chỉ đếm bài gắn TRỰC TIẾP vào term, còn archive
 * chuyên mục cha hiện cả bài của chuyên mục con, và theme có hook pre_get_posts (chỉ cho truy vấn chính) chèn
 * thêm post type vào archive. Đo 28/11 trên production: en map-of-us-cities-and-places count 0 → export 1 trang,
 * thật 2.465 trang; vi tu-lieu 1 → 67; vi ban-tin 1 → 10 (cách "include_children + post_type any" ra 11, sai).
 *
 * Cách làm: dựng truy vấn CHÍNH như WordPress làm cho URL archive (đặt tạm $wp_the_query, nên is_main_query()
 * = true và mọi hook của theme/plugin chạy như khi khách mở trang), lấy max_num_pages, rồi trả lại như cũ.
 * Chỉ lấy ID (một query LIMIT + FOUND_ROWS). Đo: 788 term vi 1,6 s, 68 term en 0,8 s.
 */
$ex_pages_diff = array(); // term có số trang khác ceil(count/ppp), để in ra log
function ex_term_pages( $t, $ppp ) {
	global $wp_the_query, $wp_query, $ex_pages_diff;
	$naive = max( 1, (int) ceil( $t->count / $ppp ) );
	$tax   = get_taxonomy( $t->taxonomy );
	if ( 'category' === $t->taxonomy ) {
		$vars = array( 'cat' => (int) $t->term_id );
	} elseif ( 'post_tag' === $t->taxonomy ) {
		$vars = array( 'tag_id' => (int) $t->term_id );
	} elseif ( $tax && ! empty( $tax->query_var ) ) {
		$vars = array( $tax->query_var => $t->slug );
	} else {
		return $naive;
	}
	$saved_the = $wp_the_query;
	$saved     = $wp_query;
	$q         = new WP_Query();
	$wp_the_query = $wp_query = $q; // phpcs:ignore
	try {
		$q->query( $vars + array( 'fields' => 'ids', 'update_post_meta_cache' => false, 'update_post_term_cache' => false ) );
		$pages = max( 1, (int) $q->max_num_pages );
	} catch ( \Throwable $e ) {
		$pages = $naive;
	} finally {
		$wp_the_query = $saved_the; // phpcs:ignore
		$wp_query     = $saved; // phpcs:ignore
	}
	if ( $pages !== $naive ) {
		$ex_pages_diff[ $t->taxonomy . ':' . $t->slug ] = "$naive→$pages";
	}
	return $pages;
}
function ex_pages_diff_line() {
	global $ex_pages_diff, $blog_id;
	if ( $ex_pages_diff ) {
		$s = array();
		foreach ( $ex_pages_diff as $k => $v ) {
			$s[] = "$k $v";
		}
		ex_line( sprintf( '# blog %d: %d term có số trang khác ceil(count/ppp): %s', $blog_id, count( $s ), implode( ', ', array_slice( $s, 0, 20 ) ) ) );
	}
}

// ---- Chế độ --ids ----
if ( $ex_ids ) {
	$t_seen = array(); // "q|term_id": mỗi term một dòng cho mỗi mục được hỏi
	$t_item = function ( $t, $q ) use ( $blog_id, $ppp, &$t_seen ) {
		if ( isset( $t_seen[ $q . '|' . $t->term_id ] ) ) {
			return;
		}
		$t_seen[ $q . '|' . $t->term_id ] = true;
		$base = ex_path( get_term_link( $t ) );
		if ( null === $base ) {
			return;
		}
		ex_out( array( 'p' => $base, 'b' => $blog_id, 'k' => 'term', 't' => $t->taxonomy, 'id' => (int) $t->term_id, 'pg' => 1,
			'pages' => ex_term_pages( $t, $ppp ), 'q' => $q ) );
	};
	// Term của bài + mọi term tổ tiên: archive chuyên mục cha hiện cả bài của chuyên mục con, nên cũng đổi.
	$with_ancestors = function ( array $ts ) {
		$out = array();
		foreach ( $ts as $t ) {
			$out[ $t->term_id ] = $t;
			if ( is_taxonomy_hierarchical( $t->taxonomy ) ) {
				foreach ( get_ancestors( $t->term_id, $t->taxonomy, 'taxonomy' ) as $aid ) {
					$a = get_term( $aid, $t->taxonomy );
					if ( $a && ! is_wp_error( $a ) ) {
						$out[ $a->term_id ] = $a;
					}
				}
			}
		}
		return array_values( $out );
	};
	$home = false;
	foreach ( $ex_ids[ $blog_id ] ?? array() as list( $kind, $tax, $id, $q ) ) {
		if ( 'p' === $kind ) {
			$home = true; // bài đăng, sửa hay gỡ đều làm đổi trang chủ
			$post = $id > 0 ? get_post( $id ) : null; // get_post(0) trả về bài toàn cục, không phải "không có"
			$pto  = $post ? get_post_type_object( $post->post_type ) : null;
			$path = ( $post && 'publish' === $post->post_status && $pto && $pto->public && 'attachment' !== $post->post_type )
				? ex_path( get_permalink( $post ) ) : null;
			if ( null === $path ) {
				ex_out( array( 'p' => '', 'b' => $blog_id, 'k' => 'gone', 'id' => $id, 'q' => $q ) );
				continue;
			}
			ex_out( array( 'p' => $path, 'b' => $blog_id, 'k' => 'single', 't' => $post->post_type, 'id' => (int) $id, 'q' => $q,
				'm' => (int) strtotime( $post->post_modified_gmt . ' UTC' ) ) );
			foreach ( get_object_taxonomies( $post->post_type, 'objects' ) as $to ) {
				if ( empty( $to->public ) || empty( $to->rewrite ) ) {
					continue;
				}
				$ts = get_the_terms( $post, $to->name );
				foreach ( $with_ancestors( ( $ts && ! is_wp_error( $ts ) ) ? $ts : array() ) as $t ) {
					$t_item( $t, $q );
				}
			}
		} else {
			$to = get_taxonomy( $tax );
			$t  = ( $to && ! empty( $to->public ) && ! empty( $to->rewrite ) ) ? get_term( $id, $tax ) : null;
			if ( ! $t || is_wp_error( $t ) || $t->count < 1 ) {
				// Term rỗng (không còn trong export vì hide_empty) vẫn có link: trả kèm để gỡ được.
				$gp = ( $t && ! is_wp_error( $t ) ) ? (string) ex_path( get_term_link( $t ) ) : '';
				ex_out( array( 'p' => $gp, 'b' => $blog_id, 'k' => 'gone', 't' => $tax, 'id' => $id, 'q' => $q ) );
				continue;
			}
			$t_item( $t, $q );
		}
	}
	if ( $home && ( $hp = ex_path( home_url( '/' ) ) ) ) {
		ex_out( array( 'p' => $hp, 'b' => $blog_id, 'k' => 'home', 't' => '', 'id' => 0, 'pg' => 1, 'q' => 'home' ) );
	}
	ex_pages_diff_line();
	ex_line( sprintf( '# blog %d: --ids %d mục, %d dòng, boot %d ms, %.2f s',
		$blog_id, count( $ex_ids[ $blog_id ] ?? array() ), $n, $boot_ms, microtime( true ) - $t1 ) );
	fflush( STDOUT );
	posix_kill( getmypid(), 9 );
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
	// Chế độ --since: bài sửa sau mốc, HOẶC bài có bình luận đã duyệt viết trong 48 giờ trước mốc trở đi.
	// Bình luận mới không làm đổi post_modified; bình luận có thể được duyệt trễ (DB không lưu giờ duyệt),
	// nên lùi thêm 48 giờ. Đổi lại, các bài này được làm mới vài lần liên tiếp.
	$cond = $ex_since ? $wpdb->prepare(
		" AND ( post_modified_gmt >= FROM_UNIXTIME(%d) OR ID IN ( SELECT comment_post_ID FROM {$wpdb->comments}
			WHERE comment_approved = '1' AND comment_date_gmt >= FROM_UNIXTIME(%d) ) )",
		$ex_since, $ex_since - 172800 ) : '';
	$sql = "SELECT ID, post_type, UNIX_TIMESTAMP(post_modified_gmt) AS m FROM {$wpdb->posts}
		WHERE post_status = 'publish' AND post_type IN ($in)" . $cond . ' ORDER BY ID';
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
			$pages = ex_term_pages( $t, $ppp );
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
ex_pages_diff_line();
ex_line( sprintf( '# blog %d (%s): %d URL, boot %d ms, xuất %.1f s, RAM tối đa %d MB',
	$blog_id, home_url( '/' ), $n, $boot_ms, microtime( true ) - $t1, memory_get_peak_usage( true ) / 1048576 ) );
fflush( STDOUT );
posix_kill( getmypid(), 9 ); // bỏ qua shutdown hook của plugin
