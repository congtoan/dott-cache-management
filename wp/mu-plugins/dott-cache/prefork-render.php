<?php
/**
 * prefork-render.php: tạo lại cache WP Rocket cho danh sách URL. WordPress được bootstrap
 * MỘT lần cho mỗi blog, sau đó fork một tiến trình con cho mỗi URL. Tiến trình con render
 * trang rồi để chính WP Rocket ghi file cache (rocket_buffer, Cache::maybe_process_buffer,
 * footer "cached@", file _gzip).
 *
 * Vị trí: wp-content/mu-plugins/dott-cache/prefork-render.php, tức là nằm trong website và được
 * backup cùng website. WordPress chỉ tự nạp file .php nằm ngay trong mu-plugins/, không nạp file
 * trong thư mục con, nên file này không chạy khi có khách truy cập. Nếu ai gọi nó qua HTTP thì
 * nó thoát ngay (kiểm PHP_SAPI).
 *
 * Chạy bằng user của site, qua CLI. Bình thường được gọi từ bộ chuyển lệnh của root,
 * /usr/local/sbin/dott-prefork (nguồn: deploy/vps-main/dott-prefork):
 *   printf '/tttest/abc/\n' | dott-prefork php81.congtoan.net
 * Chạy tay:
 *   runuser -u php81conZSI -- php prefork-render.php --root=<public_html> --host=php81.congtoan.net \
 *       --urls=-|file [--parallel=1] [--readonly] [--no-delete] [--out=log.jsonl] [--limit=N]
 *
 * Đầu vào (--urls, "-" nghĩa là stdin): mỗi dòng một path (/tttest/abc/) hoặc URL https://<host>/...
 * Có thể trộn nhiều blog trong cùng danh sách: path được xếp vào blog có path site dài nhất khớp với nó.
 *
 * Với mỗi URL: xóa file cache cũ (index-https.html, _gzip, bản mobile) NGAY TRƯỚC khi render, rồi
 * render. Trang chỉ mất cache khoảng 70 ms. Bỏ bước xóa bằng --no-delete.
 *
 * stdout, mỗi dòng có một trong 3 dạng:
 *   "R {json}"                    kết quả một URL: path, ok, err, ms, bytes...
 *   "# ..."                       log để người đọc
 *   "DONE ok=N fail=M skip=K"     dòng cuối; thiếu dòng này nghĩa là script dừng giữa chừng
 * Exit code: 0 nếu mọi URL đều ok, 1 nếu có URL lỗi, 2 nếu sai tham số.
 */

if ( PHP_SAPI !== 'cli' ) {
	exit( 1 );
}

$opt = getopt( '', array( 'root:', 'host:', 'urls:', 'parallel::', 'readonly', 'no-delete', 'out::', 'limit::' ) );
foreach ( array( 'root', 'host', 'urls' ) as $k ) {
	if ( empty( $opt[ $k ] ) ) {
		fwrite( STDOUT, "# thiếu --$k\nDONE ok=0 fail=0 skip=0\n" );
		exit( 2 );
	}
}
$pf_root     = rtrim( $opt['root'], '/' );
$pf_host     = strtolower( $opt['host'] );
$pf_parallel = max( 1, (int) ( $opt['parallel'] ?? 1 ) );
$pf_readonly = isset( $opt['readonly'] );
$pf_delete   = ! isset( $opt['no-delete'] );
$pf_limit    = (int) ( $opt['limit'] ?? 0 );
$pf_out      = $opt['out'] ?? '';
$pf_tmp      = sys_get_temp_dir() . '/dott-prefork-' . getmypid();
@mkdir( $pf_tmp, 0700 );

const PF_UA          = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0 Safari/537.36 dott-prefork';
const PF_CACHE_FILES = array( 'index-https.html', 'index-https.html_gzip', 'index-mobile-https.html', 'index-mobile-https.html_gzip' );

function pf_line( $s ) {
	fwrite( STDOUT, $s . "\n" );
}
function pf_result( array $r ) {
	pf_line( 'R ' . json_encode( $r, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE ) );
}

// ---- Bước 0 (chưa nạp WordPress): đọc và lọc danh sách ----
$pf_skip  = 0;
$pf_paths = array();
$src      = '-' === $opt['urls'] ? 'php://stdin' : $opt['urls'];
foreach ( (array) @file( $src, FILE_IGNORE_NEW_LINES ) as $l ) {
	$l = trim( (string) $l );
	if ( '' === $l || '#' === $l[0] ) {
		continue;
	}
	$p = $l;
	if ( preg_match( '#^https?://([^/]+)(/.*)?$#i', $l, $m ) ) {
		if ( strtolower( $m[1] ) !== $pf_host ) {
			pf_result( array( 'path' => $l, 'ok' => false, 'err' => "không phải host $pf_host" ) );
			$pf_skip++;
			continue;
		}
		$p = isset( $m[2] ) && '' !== $m[2] ? $m[2] : '/';
	}
	if ( '/' !== substr( $p, -1 ) ) {
		$p .= '/';
	}
	// Chỉ ký tự an toàn; không có "..", "//", query string. Path này dùng để dựng đường dẫn file cần xóa.
	if ( ! preg_match( '#^/[A-Za-z0-9/_.%~-]*$#', $p ) || false !== strpos( $p, '..' ) || false !== strpos( $p, '//' ) ) {
		pf_result( array( 'path' => $l, 'ok' => false, 'err' => 'path không hợp lệ' ) );
		$pf_skip++;
		continue;
	}
	$pf_paths[ $p ] = true;
	if ( $pf_limit && count( $pf_paths ) >= $pf_limit ) {
		break;
	}
}
$pf_paths = array_keys( $pf_paths );
pf_line( sprintf( '# %d path hợp lệ, %d bị bỏ', count( $pf_paths ), $pf_skip ) );
if ( ! $pf_paths ) {
	pf_line( "DONE ok=0 fail=0 skip=$pf_skip" );
	exit( $pf_skip ? 1 : 0 );
}

/* Danh sách path site của multisite (vd "/", "/en/", "/tttest/"). Lấy bằng cách bootstrap
 * WordPress trong một tiến trình con riêng (mất khoảng 0,4 s), rồi lưu tạm 10 phút. */
$pf_sites_cache = sys_get_temp_dir() . '/dott-prefork-sites-' . md5( $pf_root . '|' . $pf_host ) . '.json';
$pf_sites       = null;
if ( is_file( $pf_sites_cache ) && filemtime( $pf_sites_cache ) > time() - 600 ) {
	$pf_sites = json_decode( (string) file_get_contents( $pf_sites_cache ), true );
}

// Phần dưới đây phải chạy ở phạm vi toàn cục (WordPress cần biến toàn cục), nên các tiến trình
// con đi tiếp xuống phần bootstrap, còn tiến trình cha chờ ở vòng điều phối.
$pf_role  = 'parent'; // parent | discover | group
$pf_group = null;     // [site path, paths] của tiến trình con 'group'

if ( ! is_array( $pf_sites ) ) {
	$pid = pcntl_fork();
	if ( 0 === $pid ) {
		$pf_role = 'discover';
		$pf_boot_site = '/';
	} else {
		pcntl_waitpid( $pid, $st );
		$pf_sites = is_file( $pf_sites_cache ) ? json_decode( (string) file_get_contents( $pf_sites_cache ), true ) : null;
		if ( ! is_array( $pf_sites ) || ! $pf_sites ) {
			pf_line( '# không lấy được danh sách site, coi như chỉ có "/"' );
			$pf_sites = array( '/' );
		}
	}
}

if ( 'parent' === $pf_role ) {
	// Xếp path theo blog: site có path dài nhất là tiền tố của path.
	usort( $pf_sites, function ( $a, $b ) {
		return strlen( $b ) - strlen( $a );
	} );
	$groups = array();
	foreach ( $pf_paths as $p ) {
		foreach ( $pf_sites as $s ) {
			if ( 0 === strpos( $p, $s ) ) {
				$groups[ $s ][] = $p;
				continue 2;
			}
		}
		$groups['/'][] = $p; // không khớp site nào: thuộc blog gốc
	}
	// Mỗi blog một tiến trình con, chạy lần lượt (VPS chính có 1 vCPU).
	foreach ( $groups as $site => $paths ) {
		$pid = pcntl_fork();
		if ( 0 === $pid ) {
			$pf_role  = 'group';
			$pf_group = array( $site, $paths );
			break;
		}
		pcntl_waitpid( $pid, $st );
	}
}

if ( 'parent' === $pf_role ) {
	// Tổng hợp kết quả: path nào không có kết quả thì báo lỗi, không bỏ qua im lặng.
	$ok = 0;
	$fail = 0;
	$seen = array();
	foreach ( glob( "$pf_tmp/group-*.jsonl" ) ?: array() as $f ) {
		foreach ( file( $f, FILE_IGNORE_NEW_LINES | FILE_SKIP_EMPTY_LINES ) as $j ) {
			$r = json_decode( $j, true );
			if ( ! $r ) {
				continue;
			}
			$seen[ $r['path'] ] = true;
			$r['ok'] ? $ok++ : $fail++;
		}
		@unlink( $f );
	}
	foreach ( $pf_paths as $p ) {
		if ( empty( $seen[ $p ] ) ) {
			pf_result( array( 'path' => $p, 'ok' => false, 'err' => 'không có kết quả (tiến trình blog dừng giữa chừng?)' ) );
			$fail++;
		}
	}
	@rmdir( $pf_tmp );
	pf_line( "DONE ok=$ok fail=$fail skip=$pf_skip" );
	exit( $fail || $pf_skip ? 1 : 0 );
}

// ======== Từ đây: tiến trình con 'discover' hoặc 'group', bootstrap WordPress ========

function pf_server( $host, $uri, $method ) {
	$_SERVER['HTTP_HOST']       = $host;
	$_SERVER['SERVER_NAME']     = $host;
	$_SERVER['HTTPS']           = 'on';
	$_SERVER['SERVER_PORT']     = 443;
	$_SERVER['REQUEST_METHOD']  = $method;
	$_SERVER['REQUEST_URI']     = $uri;
	$_SERVER['QUERY_STRING']    = (string) parse_url( $uri, PHP_URL_QUERY );
	$_SERVER['REMOTE_ADDR']     = '127.0.0.1';
	$_SERVER['HTTP_USER_AGENT'] = PF_UA;
	$_SERVER['HTTP_ACCEPT']     = 'text/html';
	$_SERVER['SCRIPT_NAME']     = '/index.php';
	$_SERVER['PHP_SELF']        = '/index.php';
	$_SERVER['SCRIPT_FILENAME'] = ABSPATH_GUESS . '/index.php';
	$_GET = array();
	parse_str( $_SERVER['QUERY_STRING'], $_GET );
	$_REQUEST = $_GET;
	$_POST    = array();
	$_COOKIE  = array();
}
define( 'ABSPATH_GUESS', $pf_root );

/* Xóa kết quả ghi nhớ (Memoize, biến static) của các class Buffer của WP Rocket.
 * Tiến trình con thừa hưởng bộ nhớ của master, nên nếu không xóa thì kết quả của request
 * bootstrap sẽ bị dùng lại. */
function pf_reset_memo() {
	foreach ( array( 'WP_Rocket\\Buffer\\Tests', 'WP_Rocket\\Buffer\\Config', 'WP_Rocket\\Buffer\\Cache' ) as $cls ) {
		if ( ! class_exists( $cls, false ) ) {
			continue;
		}
		$r = new ReflectionClass( $cls );
		foreach ( $r->getProperties( ReflectionProperty::IS_STATIC ) as $p ) {
			$p->setAccessible( true );
			$v = $p->getValue();
			$p->setValue( null, is_array( $v ) ? array() : null );
		}
	}
}

// Bootstrap bằng GET (không dùng POST): WP Rocket chỉ đăng ký các bộ lọc tối ưu (script LCP,
// xử lý Google Fonts...) cho request GET; bootstrap bằng POST thì HTML thiếu các phần đó (đã gặp 27/11).
// Dùng một path chắc chắn KHÔNG có file cache, để advanced-cache.php không trả file rồi exit.
// Bộ đệm mà advanced-cache.php mở lúc bootstrap được đóng hết sau wp-load (nội dung rỗng nên
// WP Rocket không ghi gì).
$pf_boot_site = 'group' === $pf_role ? $pf_group[0] : '/';
pf_server( $pf_host, rtrim( $pf_boot_site, '/' ) . '/dott-prefork-boot-' . getmypid() . '/', 'GET' );
define( 'WP_USE_THEMES', true );
$t0 = microtime( true );
ob_start();
require $pf_root . '/wp-load.php';
while ( ob_get_level() > 0 ) {
	ob_end_clean();
}
$boot_ms = ( microtime( true ) - $t0 ) * 1000;

if ( 'discover' === $pf_role ) {
	$sites = array( '/' );
	if ( function_exists( 'is_multisite' ) && is_multisite() ) {
		$sites = array();
		foreach ( get_sites( array( 'number' => 1000, 'deleted' => 0, 'archived' => 0 ) ) as $s ) {
			if ( strtolower( $s->domain ) === $pf_host ) {
				$sites[] = $s->path;
			}
		}
	}
	file_put_contents( $pf_sites_cache . '.tmp', json_encode( array_values( array_unique( $sites ) ) ) );
	rename( $pf_sites_cache . '.tmp', $pf_sites_cache );
	posix_kill( getmypid(), 9 );
}

// ---- Tiến trình 'group': một blog, fork mỗi URL ----
$pf_paths = $pf_group[1];
global $wpdb;
$wpdb->close();
pf_line( sprintf( '# blog %s: boot %d ms, RAM %d MB, blog_id %d (%s), %d URL, parallel %d, readonly %s, xóa cache cũ: %s',
	$pf_group[0], $boot_ms, memory_get_usage( true ) / 1048576, get_current_blog_id(), home_url( '/' ), count( $pf_paths ), $pf_parallel,
	$pf_readonly ? 'có' : 'không', $pf_delete ? 'có' : 'không' ) );

$rocket_config_path = WP_CONTENT_DIR . '/wp-rocket-config/';
$rocket_cache_path  = WP_CONTENT_DIR . '/cache/wp-rocket/';
if ( ! class_exists( 'WP_Rocket\\Buffer\\Cache' ) ) {
	if ( ! is_file( WP_PLUGIN_DIR . '/wp-rocket/inc/classes/Buffer/class-cache.php' ) ) {
		foreach ( $pf_paths as $p ) {
			$r = array( 'path' => $p, 'ok' => false, 'err' => 'blog này không có WP Rocket' );
			pf_result( $r );
			file_put_contents( "$pf_tmp/group-" . getmypid() . '.jsonl', json_encode( $r, JSON_UNESCAPED_SLASHES ) . "\n", FILE_APPEND );
		}
		posix_kill( getmypid(), 9 );
	}
	foreach ( array( 'class-abstract-buffer', 'class-config', 'class-tests', 'class-cache' ) as $f ) {
		require_once WP_PLUGIN_DIR . '/wp-rocket/inc/classes/Buffer/' . $f . '.php';
	}
}

$out       = $pf_out ? fopen( $pf_out, 'a' ) : null;
$group_log = fopen( "$pf_tmp/group-" . getmypid() . '.jsonl', 'a' );
$running   = array();
$t_all     = microtime( true );
$stats     = array( 'ok' => 0, 'fail' => 0, 'ms' => array() );

$collect = function ( $pid, $status ) use ( &$running, $pf_tmp, $out, $group_log, &$stats ) {
	$p    = $running[ $pid ];
	unset( $running[ $pid ] );
	$f    = "$pf_tmp/$pid.json";
	$info = is_file( $f ) ? json_decode( file_get_contents( $f ), true ) : null;
	@unlink( $f );
	if ( ! $info ) {
		$info = array( 'path' => $p['path'], 'ok' => false, 'err' => 'tiến trình con thoát không có kết quả, status ' . $status );
	}
	$info['deleted'] = $p['deleted'];
	$info['ok'] ? $stats['ok']++ : $stats['fail']++;
	if ( $info['ok'] ) {
		$stats['ms'][] = $info['ms'];
	}
	$j = json_encode( $info, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE );
	pf_line( 'R ' . $j );
	fwrite( $group_log, $j . "\n" );
	if ( $out ) {
		fwrite( $out, $j . "\n" );
	}
};

foreach ( $pf_paths as $path ) {
	while ( count( $running ) >= $pf_parallel ) {
		$pid = pcntl_wait( $st );
		if ( $pid > 0 ) {
			$collect( $pid, $st );
		}
	}
	// Xóa cache cũ của đúng URL này ngay trước khi render (path đã được lọc ở bước 0).
	$deleted = 0;
	if ( $pf_delete ) {
		$dir = $rocket_cache_path . $pf_host . $path;
		foreach ( PF_CACHE_FILES as $f ) {
			if ( @unlink( $dir . $f ) ) {
				$deleted++;
			}
		}
	}
	$pid = pcntl_fork();
	if ( -1 === $pid ) {
		pf_line( '# fork lỗi' );
		break;
	}
	if ( 0 === $pid ) {
		// ---- Con: render một URL ----
		$c0   = microtime( true );
		$info = array( 'path' => $path, 'ok' => false );
		try {
			$wpdb->db_connect();
			if ( $pf_readonly ) {
				$wpdb->query( 'SET SESSION TRANSACTION READ ONLY' );
			}
			wp_cache_init(); // kết nối cache riêng (Redis nếu có)
			pf_server( $pf_host, $path, 'GET' );
			pf_reset_memo();
			// Bộ đệm riêng ở ngoài cùng. WordPress (6.9+: chèn CSS block vào placeholder) và plugin
			// mở thêm bộ đệm có callback. Phải đóng chúng bằng ob_end_flush() để callback chạy và đẩy
			// kết quả xuống bộ đệm của mình. ob_get_clean() KHÔNG chạy callback (đã gặp 27/11).
			$pf_level = ob_get_level();
			ob_start();
			$pf_mine = ob_get_level();
			wp();
			require ABSPATH . WPINC . '/template-loader.php';
			if ( getenv( 'PF_DEBUG' ) ) {
				$info['ob_handlers']   = ob_list_handlers();
				$info['rocket_buffer'] = isset( $GLOBALS['wp_filter']['rocket_buffer'] ) ? count( $GLOBALS['wp_filter']['rocket_buffer']->callbacks ) : 0;
			}
			while ( ob_get_level() > $pf_mine ) {
				ob_end_flush();
			}
			$html = ob_get_clean();
			while ( ob_get_level() > $pf_level ) {
				ob_end_clean();
			}
			$c1    = microtime( true );
			$cfg   = new WP_Rocket\Buffer\Config( array( 'config_dir_path' => $rocket_config_path ) );
			$tests = new WP_Rocket\Buffer\Tests( $cfg );
			// Bước tối ưu của WP Rocket (Optimization::maybe_process_buffer): khi chạy qua HTTP, bộ đệm
			// này mở ở template_redirect. Trong tiến trình con, can_init_process() của instance tạo lúc
			// master bootstrap trả false nên bộ đệm không mở. Vì vậy gọi lại đúng 2 bước đó ở đây,
			// dùng Tests mới tạo theo request của con.
			if ( $tests->can_init_process() && $tests->can_process_buffer( $html ) ) {
				do_action( 'rocket_before_maybe_process_buffer', $html );
				$html = (string) apply_filters( 'rocket_buffer', $html );
				do_action( 'rocket_after_process_buffer' );
				$info['optimized'] = true;
			} else {
				$info['optimized']   = false;
				$info['test_failed'] = method_exists( $tests, 'get_last_error' ) ? $tests->get_last_error() : '?';
			}
			$c = new WP_Rocket\Buffer\Cache( $tests, $cfg, array( 'cache_dir_path' => $rocket_cache_path ) );
			$c->maybe_process_buffer( $html );
			$file = $c->get_cache_path();
			clearstatcache();
			$info['render_ms'] = round( ( $c1 - $c0 ) * 1000, 1 );
			$info['ms']        = round( ( microtime( true ) - $c0 ) * 1000, 1 );
			$info['bytes']     = strlen( $html );
			$info['is_404']    = is_404();
			$info['file']      = str_replace( $rocket_cache_path, '', $file );
			$info['written']   = is_file( $file ) && filemtime( $file ) >= (int) $c0 - 1;
			$info['ok']        = $info['written'] && ! $info['is_404'];
			if ( ! $info['ok'] ) {
				$info['err'] = $info['is_404'] ? '404' : 'WP Rocket không ghi file cache';
			}
		} catch ( Throwable $e ) {
			$info['err'] = get_class( $e ) . ': ' . $e->getMessage();
		}
		file_put_contents( "$pf_tmp/" . getmypid() . '.json', json_encode( $info, JSON_UNESCAPED_SLASHES ) );
		posix_kill( getmypid(), 9 ); // bỏ qua shutdown hook của plugin
	}
	$running[ $pid ] = array( 'path' => $path, 'deleted' => $deleted );
}
while ( $running ) {
	$pid = pcntl_wait( $st );
	if ( $pid > 0 ) {
		$collect( $pid, $st );
	}
}
$total = microtime( true ) - $t_all;
sort( $stats['ms'] );
$n   = count( $stats['ms'] );
$p50 = $n ? $stats['ms'][ (int) ( 0.5 * ( $n - 1 ) ) ] : 0;
$p95 = $n ? $stats['ms'][ (int) ( 0.95 * ( $n - 1 ) ) ] : 0;
pf_line( sprintf( '# blog %s xong: %d ok, %d lỗi, %.1f s (%.2f URL/s), mỗi URL p50 %.0f ms, p95 %.0f ms (+ boot %d ms một lần)',
	$pf_group[0], $stats['ok'], $stats['fail'], $total, $stats['ok'] / max( $total, 0.001 ), $p50, $p95, $boot_ms ) );
fclose( $group_log );
if ( $out ) {
	fclose( $out );
}
fflush( STDOUT );
posix_kill( getmypid(), 9 ); // tiến trình 'group' cũng bỏ qua shutdown hook
