<?php
/**
 * prefork-render.php: render hàng loạt URL, bootstrap WordPress MỘT lần, fork mỗi URL.
 * Mỗi tiến trình con render một trang rồi để chính WP Rocket ghi file cache
 * (Cache::maybe_process_buffer: rocket_buffer, footer "cached@", file _gzip).
 *
 * Chạy dưới user của site (CLI), ví dụ trên php81:
 *   runuser -u php81conZSI -- php prefork-render.php --root=/home/php81conZSI/php81.congtoan.net/public_html \
 *       --host=php81.congtoan.net --urls=/tmp/paths.txt --parallel=1 [--readonly] [--out=/tmp/log.jsonl]
 *
 * File --urls: mỗi dòng một path (/tttest/abc/) hoặc URL đầy đủ, cùng một blog (multisite).
 * Bản thử nghiệm (27/11). Thiết kế: docs/design-server-only.md mục "prefork".
 */

if ( PHP_SAPI !== 'cli' ) {
	exit( 1 );
}

$opt = getopt( '', array( 'root:', 'host:', 'urls:', 'parallel::', 'readonly', 'out::', 'limit::' ) );
foreach ( array( 'root', 'host', 'urls' ) as $k ) {
	if ( empty( $opt[ $k ] ) ) {
		fwrite( STDERR, "thiếu --$k\n" );
		exit( 2 );
	}
}
$pf_root     = rtrim( $opt['root'], '/' );
$pf_host     = $opt['host'];
$pf_parallel = max( 1, (int) ( $opt['parallel'] ?? 1 ) );
$pf_readonly = isset( $opt['readonly'] );
$pf_limit    = (int) ( $opt['limit'] ?? 0 );
$pf_out      = $opt['out'] ?? '';

$pf_paths = array();
foreach ( file( $opt['urls'], FILE_IGNORE_NEW_LINES | FILE_SKIP_EMPTY_LINES ) as $l ) {
	$l = trim( $l );
	if ( '' === $l || '#' === $l[0] ) {
		continue;
	}
	if ( false !== ( $i = strpos( $l, '://' ) ) ) {
		$j = strpos( $l, '/', $i + 3 );
		$l = false === $j ? '/' : substr( $l, $j );
	}
	$pf_paths[] = $l;
	if ( $pf_limit && count( $pf_paths ) >= $pf_limit ) {
		break;
	}
}
if ( ! $pf_paths ) {
	fwrite( STDERR, "không có URL\n" );
	exit( 2 );
}

const PF_UA = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0 Safari/537.36 dott-prefork';

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

// ---- Master: bootstrap một lần ----
// Bootstrap bằng GET (không dùng POST): WP Rocket chỉ đăng ký các bộ lọc tối ưu (script LCP,
// xử lý Google Fonts...) cho request GET; bootstrap bằng POST thì HTML thiếu các phần đó (đã gặp 27/11).
// Dùng một path chắc chắn KHÔNG có file cache, để advanced-cache.php không trả file rồi exit.
// Bộ đệm mà advanced-cache.php mở lúc bootstrap được đóng hết sau wp-load (nội dung rỗng nên
// WP Rocket không ghi gì).
$pf_blog     = preg_match( '#^(/[^/]+/)#', $pf_paths[0], $m ) && '/' !== $pf_paths[0] ? $m[1] : '/';
$pf_boot_uri = rtrim( $pf_blog, '/' ) . '/dott-prefork-boot-' . getmypid() . '/';
pf_server( $pf_host, $pf_boot_uri, 'GET' );
define( 'WP_USE_THEMES', true );
$t0 = microtime( true );
ob_start();
require $pf_root . '/wp-load.php';
while ( ob_get_level() > 0 ) {
	ob_end_clean();
}
$boot_ms = ( microtime( true ) - $t0 ) * 1000;
global $wpdb;
$wpdb->close();
fwrite( STDOUT, sprintf( "master: boot %d ms, RAM %d MB, blog %d (%s), %d URL, parallel %d, readonly %d\n",
	$boot_ms, memory_get_usage( true ) / 1048576, get_current_blog_id(), home_url( '/' ), count( $pf_paths ), $pf_parallel, $pf_readonly ) );

$rocket_config_path = WP_CONTENT_DIR . '/wp-rocket-config/';
$rocket_cache_path  = WP_CONTENT_DIR . '/cache/wp-rocket/';
if ( ! class_exists( 'WP_Rocket\\Buffer\\Cache' ) ) {
	foreach ( array( 'class-abstract-buffer', 'class-config', 'class-tests', 'class-cache' ) as $f ) {
		require_once WP_PLUGIN_DIR . '/wp-rocket/inc/classes/Buffer/' . $f . '.php';
	}
}

$out = $pf_out ? fopen( $pf_out, 'w' ) : null;
$tmp = sys_get_temp_dir() . '/pf-' . getmypid();
@mkdir( $tmp, 0700 );

// ---- Fork từng URL, tối đa $pf_parallel con chạy cùng lúc ----
$running = array();
$t_all   = microtime( true );
$stats   = array( 'ok' => 0, 'fail' => 0, 'ms' => array() );

$collect = function ( $pid, $status ) use ( &$running, $tmp, $out, &$stats ) {
	$p    = $running[ $pid ];
	unset( $running[ $pid ] );
	$f    = "$tmp/$pid.json";
	$info = is_file( $f ) ? json_decode( file_get_contents( $f ), true ) : null;
	@unlink( $f );
	if ( ! $info ) {
		$info = array( 'path' => $p, 'ok' => false, 'err' => 'con thoát không có kết quả, status ' . $status );
	}
	$info['ok'] ? $stats['ok']++ : $stats['fail']++;
	if ( $info['ok'] ) {
		$stats['ms'][] = $info['ms'];
	}
	if ( $out ) {
		fwrite( $out, json_encode( $info, JSON_UNESCAPED_SLASHES ) . "\n" );
	}
	if ( ! $info['ok'] ) {
		fwrite( STDOUT, "  LỖI {$info['path']}: {$info['err']}\n" );
	}
};

foreach ( $pf_paths as $path ) {
	while ( count( $running ) >= $pf_parallel ) {
		$pid = pcntl_wait( $st );
		if ( $pid > 0 ) {
			$collect( $pid, $st );
		}
	}
	$pid = pcntl_fork();
	if ( -1 === $pid ) {
		fwrite( STDERR, "fork lỗi\n" );
		exit( 1 );
	}
	if ( 0 === $pid ) {
		// ---- Con ----
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
		file_put_contents( "$tmp/" . getmypid() . '.json', json_encode( $info, JSON_UNESCAPED_SLASHES ) );
		posix_kill( getmypid(), 9 ); // bỏ qua shutdown hook của plugin
	}
	$running[ $pid ] = $path;
}
while ( $running ) {
	$pid = pcntl_wait( $st );
	if ( $pid > 0 ) {
		$collect( $pid, $st );
	}
}
@rmdir( $tmp );
$total = microtime( true ) - $t_all;
sort( $stats['ms'] );
$n   = count( $stats['ms'] );
$p50 = $n ? $stats['ms'][ (int) ( 0.5 * ( $n - 1 ) ) ] : 0;
$p95 = $n ? $stats['ms'][ (int) ( 0.95 * ( $n - 1 ) ) ] : 0;
fwrite( STDOUT, sprintf( "xong: %d ok, %d lỗi, %.1f s (%.2f URL/s), mỗi URL p50 %.0f ms, p95 %.0f ms (+ boot %d ms một lần)\n",
	$stats['ok'], $stats['fail'], $total, $stats['ok'] / max( $total, 0.001 ), $p50, $p95, $boot_ms ) );
