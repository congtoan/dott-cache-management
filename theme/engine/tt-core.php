<?php
/**
 * tt-core: gộp JS của theme và plugin thành MỘT file core.js cho cả desktop lẫn mobile.
 * ==========================================================================
 * Thiết kế: docs/design-tt-core.md (repo dott-cache-management, nhánh v2).
 *
 * File này nằm ở themes/<theme>/tt-core/tt-core.php, được functions.php của theme nạp.
 * Cùng một file cho mọi theme dùng tt-core (chép từ theme/engine/ khi deploy).
 *
 * Frontend chỉ làm 1 việc: enqueue core.js?ver=<ver>. KHÔNG quét file.
 * Build chỉ chạy khi: bấm Generate, WP-CLI, hoặc lần mở admin kế tiếp sau một sự kiện
 * (bật/tắt/cập nhật plugin, plugin gọi tt_core_request_build()).
 *
 * API cho plugin (PHP):
 *   tt_core_api()                         -> 1
 *   add_action( 'tt_core_modules', fn( TT_Core_Registry $reg, string $theme ) )
 *   tt_core_request_build( $theme = null, $reason = '' )
 *   tt_core_has_module( $id, $theme = null )   -> bản build hiện tại có module này không
 *   tt_core_upload_modules_dir( $theme )       -> thư mục plugin được ghi module tự sinh
 */

defined( 'ABSPATH' ) || exit;

if ( defined( 'TT_CORE_VERSION' ) ) {
	return; // đã nạp (ví dụ 2 theme cùng nạp trong một request)
}

define( 'TT_CORE_VERSION', '0.1.0' );
define( 'TT_CORE_API', 1 );

/** Bật minify kể cả khi không có Peast để kiểm tra kết quả (mặc định: không). */
if ( ! defined( 'TT_CORE_MINIFY_UNVERIFIED' ) ) {
	define( 'TT_CORE_MINIFY_UNVERIFIED', false );
}

/* =========================================================================
 * 1. Đường dẫn, option
 * ====================================================================== */

function tt_core_api() {
	return TT_CORE_API;
}

/** Thư mục tt-core của theme (chứa base.js, modules/, core.js). */
function tt_core_dir( $theme = null ) {
	$theme = $theme ? $theme : get_stylesheet();
	return get_theme_root( $theme ) . '/' . $theme . '/tt-core';
}

function tt_core_url( $theme = null ) {
	$theme = $theme ? $theme : get_stylesheet();
	return get_theme_root_uri( $theme ) . '/' . $theme . '/tt-core';
}

/**
 * Thư mục module do plugin tự sinh: wp-content/uploads/tt-core/<theme>/modules
 * (uploads GỐC, kể cả blog con trong multisite, để mọi theme cùng một quy ước).
 */
function tt_core_upload_modules_dir( $theme = null ) {
	$theme = $theme ? $theme : get_stylesheet();
	$dir   = WP_CONTENT_DIR . '/uploads/tt-core/' . $theme . '/modules';
	return apply_filters( 'tt_core_upload_modules_dir', $dir, $theme );
}

/** Các theme có tt-core (có tt-core/base.js). Chỉ gọi lúc có sự kiện, không gọi ở frontend. */
function tt_core_themes() {
	$out = array();
	foreach ( (array) glob( get_theme_root() . '/*/tt-core/base.js' ) as $f ) {
		$out[] = basename( dirname( dirname( $f ) ) );
	}
	return $out;
}

/* Network option (multisite) hoặc option thường (site đơn). Nằm trong object cache. */
function tt_core_builds() {
	$v = get_site_option( 'tt_core_builds', array() );
	return is_array( $v ) ? $v : array();
}

function tt_core_build_info( $theme = null ) {
	$theme = $theme ? $theme : get_stylesheet();
	$all   = tt_core_builds();
	return isset( $all[ $theme ] ) ? $all[ $theme ] : null;
}

function tt_core_settings( $theme = null ) {
	$theme = $theme ? $theme : get_stylesheet();
	$all   = get_site_option( 'tt_core_settings', array() );
	$s     = isset( $all[ $theme ] ) && is_array( $all[ $theme ] ) ? $all[ $theme ] : array();
	return wp_parse_args( $s, array( 'disabled' => array(), 'device' => array() ) );
}

function tt_core_save_settings( $theme, $settings ) {
	$all           = get_site_option( 'tt_core_settings', array() );
	$all           = is_array( $all ) ? $all : array();
	$all[ $theme ] = $settings;
	update_site_option( 'tt_core_settings', $all );
}

function tt_core_has_module( $id, $theme = null ) {
	$b = tt_core_build_info( $theme );
	return $b && isset( $b['modules'][ $id ] );
}

/* =========================================================================
 * 2. Đánh dấu cần build lại (dirty)
 * ====================================================================== */

/**
 * Đánh dấu theme cần build lại. Build thật xảy ra ở lần mở admin kế tiếp của blog
 * dùng theme đó (hoặc bấm Generate / WP-CLI).
 *
 * @param string|null $theme  null = mọi theme có tt-core.
 */
function tt_core_request_build( $theme = null, $reason = '' ) {
	$themes = $theme ? array( $theme ) : tt_core_themes();
	$dirty  = get_site_option( 'tt_core_dirty', array() );
	$dirty  = is_array( $dirty ) ? $dirty : array();
	foreach ( $themes as $t ) {
		$dirty[ $t ] = array( 'time' => time(), 'reason' => (string) $reason );
	}
	update_site_option( 'tt_core_dirty', $dirty );
}

function tt_core_is_dirty( $theme = null ) {
	$theme = $theme ? $theme : get_stylesheet();
	$dirty = get_site_option( 'tt_core_dirty', array() );
	return is_array( $dirty ) && isset( $dirty[ $theme ] ) ? $dirty[ $theme ] : false;
}

function tt_core_clear_dirty( $theme ) {
	$dirty = get_site_option( 'tt_core_dirty', array() );
	if ( is_array( $dirty ) && isset( $dirty[ $theme ] ) ) {
		unset( $dirty[ $theme ] );
		update_site_option( 'tt_core_dirty', $dirty );
	}
}

add_action( 'activated_plugin', function ( $plugin ) {
	tt_core_request_build( null, 'activated_plugin: ' . $plugin );
} );
add_action( 'deactivated_plugin', function ( $plugin ) {
	tt_core_request_build( null, 'deactivated_plugin: ' . $plugin );
} );
add_action( 'upgrader_process_complete', function ( $upgrader, $extra ) {
	$type = isset( $extra['type'] ) ? $extra['type'] : '';
	if ( 'plugin' === $type || 'theme' === $type ) {
		tt_core_request_build( null, 'upgrade: ' . $type );
	}
}, 10, 2 );
add_action( 'switch_theme', function () {
	tt_core_request_build( get_stylesheet(), 'switch_theme' );
} );

/* Lần mở admin kế tiếp: build nếu theme của blog này đang dirty. */
add_action( 'admin_init', function () {
	if ( wp_doing_ajax() || ! current_user_can( 'manage_options' ) ) {
		return;
	}
	$theme = get_stylesheet();
	if ( ! file_exists( tt_core_dir( $theme ) . '/base.js' ) ) {
		return;
	}
	$dirty = tt_core_is_dirty( $theme );
	if ( ! $dirty && tt_core_build_info( $theme ) ) {
		return;
	}
	$r = tt_core_build( $theme, array( 'reason' => $dirty ? 'auto: ' . $dirty['reason'] : 'auto: first build' ) );
	if ( ! $r['ok'] ) {
		set_site_transient( 'tt_core_notice_' . $theme, $r, DAY_IN_SECONDS );
	}
} );

/* =========================================================================
 * 3. Module: đọc khai báo, gom từ 3 nguồn
 * ====================================================================== */

/** Đăng ký module từ plugin (truyền vào action tt_core_modules). */
class TT_Core_Registry {
	private $items = array();
	public $theme;

	public function __construct( $theme ) {
		$this->theme = $theme;
	}

	/**
	 * @param string $id
	 * @param array  $args ver, device (all|desktop|mobile), order, ready (bool),
	 *                     và MỘT trong: file (đường dẫn), code (chuỗi JS), src (URL tải thêm).
	 */
	public function add( $id, $args ) {
		$args       = is_array( $args ) ? $args : array();
		$args['id'] = $id;
		$this->items[ $id ] = $args;
		return $this;
	}

	public function all() {
		return $this->items;
	}
}

/**
 * Đọc dòng khai báo ở đầu file:  /* @tt-module id=xxx device=mobile ver=1.0.0 order=120 ready=0 *\/
 * Trả về mảng hoặc null nếu không có khai báo.
 */
function tt_core_parse_header( $code ) {
	if ( ! preg_match( '/@tt-module\b([^\r\n]*)/', substr( $code, 0, 1024 ), $m ) ) {
		return null;
	}
	$h = array();
	if ( preg_match_all( '/([a-z]+)=([^\s*]+)/', $m[1], $mm, PREG_SET_ORDER ) ) {
		foreach ( $mm as $kv ) {
			$h[ $kv[1] ] = $kv[2];
		}
	}
	return $h;
}

/** Chuẩn hóa một module về cùng dạng. Trả về mảng hoặc chuỗi lỗi. */
function tt_core_normalize( $raw, $source ) {
	$id = isset( $raw['id'] ) ? (string) $raw['id'] : '';
	if ( ! preg_match( '/^[a-z0-9][a-z0-9-]{0,63}$/', $id ) ) {
		return 'id không hợp lệ: "' . $id . '"';
	}
	$device = isset( $raw['device'] ) ? (string) $raw['device'] : 'all';
	if ( ! in_array( $device, array( 'all', 'desktop', 'mobile' ), true ) ) {
		return $id . ': device không hợp lệ "' . $device . '"';
	}
	$ready = isset( $raw['ready'] ) ? $raw['ready'] : false;
	return array(
		'id'     => $id,
		'device' => $device,
		'ver'    => isset( $raw['ver'] ) ? (string) $raw['ver'] : '',
		'order'  => isset( $raw['order'] ) ? (int) $raw['order'] : 500,
		'ready'  => ( true === $ready || '1' === $ready || 1 === $ready || 'true' === $ready ),
		'source' => $source,
		'file'   => isset( $raw['file'] ) ? (string) $raw['file'] : '',
		'code'   => isset( $raw['code'] ) ? (string) $raw['code'] : null,
		'src'    => isset( $raw['src'] ) ? (string) $raw['src'] : '',
	);
}

/** Module từ một thư mục *.js (theme hoặc uploads). */
function tt_core_scan_dir( $dir, $source, &$warnings ) {
	$out = array();
	if ( ! is_dir( $dir ) ) {
		return $out;
	}
	$now = time();
	foreach ( (array) glob( $dir . '/*.js' ) as $f ) {
		// File vừa ghi chưa tới 10 giây có thể đang upload dở: bỏ qua lần này.
		// Build sẽ DỪNG (không build thiếu module), xem tt_core_build().
		if ( $now - (int) @filemtime( $f ) < 10 ) {
			$warnings[] = 'PENDING ' . $source . '/' . basename( $f ) . ': vừa thay đổi, thử lại sau 10 giây';
			continue;
		}
		$head = (string) @file_get_contents( $f, false, null, 0, 1024 );
		$h    = tt_core_parse_header( $head );
		if ( null === $h ) {
			$warnings[] = $source . '/' . basename( $f ) . ': thiếu dòng @tt-module, bỏ qua';
			continue;
		}
		$h['file'] = $f;
		$m         = tt_core_normalize( $h, $source );
		if ( is_string( $m ) ) {
			$warnings[] = $source . '/' . basename( $f ) . ': ' . $m;
			continue;
		}
		$out[ $m['id'] ] = $m;
	}
	return $out;
}

/**
 * Gom module của một theme. Ưu tiên khi trùng id: uploads > plugin > theme.
 * @return array { modules: [id => module], warnings: [] }
 */
function tt_core_collect( $theme ) {
	$warnings = array();
	$list     = tt_core_scan_dir( tt_core_dir( $theme ) . '/modules', 'theme', $warnings );

	$reg = new TT_Core_Registry( $theme );
	do_action( 'tt_core_modules', $reg, $theme );
	foreach ( $reg->all() as $raw ) {
		$m = tt_core_normalize( $raw, 'plugin' );
		if ( is_string( $m ) ) {
			$warnings[] = 'plugin: ' . $m;
			continue;
		}
		if ( '' === $m['file'] && null === $m['code'] && '' === $m['src'] ) {
			$warnings[] = 'plugin: ' . $m['id'] . ' thiếu file/code/src';
			continue;
		}
		if ( isset( $list[ $m['id'] ] ) ) {
			$warnings[] = $m['id'] . ': plugin thay module cùng id của theme';
		}
		$list[ $m['id'] ] = $m;
	}

	foreach ( tt_core_scan_dir( tt_core_upload_modules_dir( $theme ), 'uploads', $warnings ) as $id => $m ) {
		if ( isset( $list[ $id ] ) ) {
			$warnings[] = $id . ': uploads thay module cùng id của ' . $list[ $id ]['source'];
		}
		$list[ $id ] = $m;
	}

	// Thiết lập trên dashboard: tắt module, đổi thiết bị.
	$s = tt_core_settings( $theme );
	foreach ( $list as $id => &$m ) {
		$m['enabled']    = ! in_array( $id, (array) $s['disabled'], true );
		$m['device_def'] = $m['device'];
		if ( isset( $s['device'][ $id ] ) && in_array( $s['device'][ $id ], array( 'all', 'desktop', 'mobile' ), true ) ) {
			$m['device'] = $s['device'][ $id ];
		}
	}
	unset( $m );

	uasort( $list, function ( $a, $b ) {
		return $a['order'] === $b['order'] ? strcmp( $a['id'], $b['id'] ) : $a['order'] - $b['order'];
	} );

	return array( 'modules' => $list, 'warnings' => $warnings );
}

/** Đọc code của module. Trả về chuỗi hoặc null nếu lỗi. */
function tt_core_module_code( $m ) {
	if ( '' !== $m['src'] ) {
		return 'TT.load(' . wp_json_encode( $m['src'] ) . ');';
	}
	if ( null !== $m['code'] ) {
		$code = $m['code'];
	} else {
		if ( ! is_readable( $m['file'] ) ) {
			return null;
		}
		$code = file_get_contents( $m['file'] );
	}
	$code = preg_replace( '/^\xEF\xBB\xBF/', '', $code ); // BOM
	return str_replace( array( "\r\n", "\r" ), "\n", $code );
}

/**
 * Chữ ký nguồn: đổi khi bất kỳ module/thiết lập/base.js nào đổi.
 * Dùng để dashboard báo "có thay đổi chưa build". Không dùng ở frontend.
 */
function tt_core_signature( $theme, $modules ) {
	$parts = array( TT_CORE_VERSION );
	$base  = tt_core_dir( $theme ) . '/base.js';
	$parts[] = @filemtime( $base ) . ':' . @filesize( $base );
	foreach ( $modules as $m ) {
		$p = array( $m['id'], $m['device'], $m['order'], (int) $m['ready'], $m['ver'], $m['source'], (int) $m['enabled'] );
		if ( '' !== $m['src'] ) {
			$p[] = $m['src'];
		} elseif ( null !== $m['code'] ) {
			$p[] = md5( $m['code'] );
		} else {
			$p[] = $m['file'] . ':' . @filemtime( $m['file'] ) . ':' . @filesize( $m['file'] );
		}
		$parts[] = implode( '|', $p );
	}
	return md5( implode( "\n", $parts ) );
}

/* =========================================================================
 * 4. Thư viện: Peast (kiểm cú pháp), matthiasmullie/minify
 *    Chỉ nạp lúc build. Không đụng tới frontend.
 * ====================================================================== */

function tt_core_vendor_autoload() {
	static $done = false;
	if ( $done ) {
		return;
	}
	$done = true;
	$vendor = __DIR__ . '/vendor';
	$map    = array(
		'Peast\\'                      => $vendor . '/peast/lib/Peast/',
		'MatthiasMullie\\Minify\\'        => $vendor . '/matthiasmullie/minify/src/',
		'MatthiasMullie\\PathConverter\\' => $vendor . '/matthiasmullie/path-converter/src/',
	);
	spl_autoload_register( function ( $class ) use ( $map ) {
		foreach ( $map as $prefix => $dir ) {
			if ( 0 === strpos( $class, $prefix ) ) {
				$f = $dir . str_replace( '\\', '/', substr( $class, strlen( $prefix ) ) ) . '.php';
				if ( is_file( $f ) ) {
					require $f;
				}
				return;
			}
		}
	} );
}

function tt_core_has_peast() {
	tt_core_vendor_autoload();
	return class_exists( 'Peast\\Peast' );
}

function tt_core_has_minify() {
	tt_core_vendor_autoload();
	return class_exists( 'MatthiasMullie\\Minify\\JS' );
}

/**
 * Kiểm tra cú pháp JS (dạng script, không phải module ES).
 * @return true|array|null  true = hợp lệ; array{line,col,msg} = lỗi; null = không có Peast.
 */
function tt_core_syntax_check( $code ) {
	if ( ! tt_core_has_peast() ) {
		return null;
	}
	try {
		\Peast\Peast::latest( $code, array( 'sourceType' => \Peast\Peast::SOURCE_TYPE_SCRIPT ) )->parse();
		return true;
	} catch ( \Peast\Syntax\Exception $e ) {
		$pos = $e->getPosition();
		return array(
			'line' => $pos ? $pos->getLine() : 0,
			'col'  => $pos ? $pos->getColumn() : 0,
			'msg'  => $e->getMessage(),
		);
	} catch ( \Exception $e ) {
		return array( 'line' => 0, 'col' => 0, 'msg' => get_class( $e ) . ': ' . $e->getMessage() );
	}
}

/** Minify. Trả về chuỗi, hoặc null nếu không có thư viện / lỗi. */
function tt_core_minify( $code ) {
	if ( ! tt_core_has_minify() ) {
		return null;
	}
	try {
		$min = new \MatthiasMullie\Minify\JS();
		$min->add( $code );
		return $min->minify();
	} catch ( \Exception $e ) {
		return null;
	}
}

/* =========================================================================
 * 5. Build
 * ====================================================================== */

/** Bọc code module trong TT.define(...). */
function tt_core_wrap( $m, $code ) {
	$opt = array( 'ver' => $m['ver'], 'device' => $m['device'] );
	if ( $m['ready'] ) {
		$opt['ready'] = true;
	}
	// Xuống dòng trước "}" để code kết thúc bằng comment // vẫn đúng.
	return "/* --- module " . $m['id'] . " (" . $m['source'] . ") --- */\n"
		. 'TT.define(' . wp_json_encode( $m['id'] ) . ',' . wp_json_encode( $opt ) . ",function(TT){\n"
		. rtrim( $code ) . "\n});\n";
}

/**
 * Build core.js cho một theme.
 *
 * @param string $theme
 * @param array  $args  dry (bool): chỉ kiểm tra, không ghi; reason (string)
 * @return array ok, ver, errors[], warnings[], size, size_raw, minified, checked, modules
 */
function tt_core_build( $theme = null, $args = array() ) {
	$theme = $theme ? $theme : get_stylesheet();
	$args  = wp_parse_args( $args, array( 'dry' => false, 'reason' => 'manual' ) );
	$res   = array( 'ok' => false, 'theme' => $theme, 'errors' => array(), 'warnings' => array() );
	$dir   = tt_core_dir( $theme );

	$base = @file_get_contents( $dir . '/base.js' );
	if ( false === $base ) {
		$res['errors'][] = 'Không đọc được ' . $dir . '/base.js';
		return $res;
	}

	$lock = null;
	if ( ! $args['dry'] ) {
		$lock = @fopen( $dir . '/.build.lock', 'c' );
		if ( ! $lock || ! flock( $lock, LOCK_EX | LOCK_NB ) ) {
			$res['errors'][] = 'Đang có một lần build khác chạy.';
			return $res;
		}
	}

	$c                = tt_core_collect( $theme );
	$res['warnings']  = $c['warnings'];
	foreach ( $c['warnings'] as $w ) {
		if ( 0 === strpos( $w, 'PENDING ' ) ) {
			$res['errors'][] = substr( $w, 8 );
		}
	}
	$checked          = tt_core_has_peast();
	$res['checked']   = $checked;
	if ( ! $checked ) {
		$res['warnings'][] = 'Không có Peast: không kiểm tra cú pháp được.';
	}

	$parts   = array();
	$summary = array();
	foreach ( $c['modules'] as $id => $m ) {
		if ( ! $m['enabled'] ) {
			continue;
		}
		$code = tt_core_module_code( $m );
		if ( null === $code ) {
			$res['errors'][] = $id . ': không đọc được ' . $m['file'];
			continue;
		}
		if ( $checked && '' === $m['src'] ) {
			$chk = tt_core_syntax_check( $code );
			if ( true !== $chk ) {
				$where           = '' !== $m['file'] ? basename( $m['file'] ) : $id;
				$res['errors'][] = sprintf( '%s: lỗi cú pháp dòng %d cột %d: %s', $where, $chk['line'], $chk['col'], $chk['msg'] );
				continue;
			}
		}
		$parts[]        = tt_core_wrap( $m, $code );
		$summary[ $id ] = array(
			'ver'    => $m['ver'],
			'device' => $m['device'],
			'source' => $m['source'],
			'size'   => strlen( $code ),
		);
	}

	if ( $res['errors'] ) {
		if ( $lock ) {
			flock( $lock, LOCK_UN );
			fclose( $lock );
		}
		return $res; // giữ nguyên core.js đang chạy
	}

	$body = rtrim( $base ) . "\n" . implode( '', $parts );
	$ver  = substr( md5( $body ), 0, 10 );
	$info = array( 'ver' => $ver, 'theme' => $theme, 'built' => gmdate( 'c' ) );
	// Thông tin build đặt ngay sau base.js để module nào cũng đọc được TT.build.
	$body = rtrim( $base ) . "\nwindow.TT&&(window.TT.build=" . wp_json_encode( $info ) . ");\n" . implode( '', $parts );

	$out      = $body;
	$minified = false;
	if ( $checked || TT_CORE_MINIFY_UNVERIFIED ) {
		$min = tt_core_minify( $body );
		if ( null === $min ) {
			$res['warnings'][] = 'Không minify (thiếu thư viện matthiasmullie/minify hoặc lỗi).';
		} elseif ( $checked && true !== ( $chk = tt_core_syntax_check( $min ) ) ) {
			$res['warnings'][] = 'Bản minify lỗi cú pháp (dòng ' . $chk['line'] . '): dùng bản chưa minify.';
		} else {
			$out      = $min;
			$minified = true;
		}
	} else {
		$res['warnings'][] = 'Không minify vì không kiểm tra được kết quả (thiếu Peast).';
	}

	$header = sprintf(
		"/*! tt-core %s | theme %s | ver %s | %s | %d module | FILE BUILD - KHONG SUA TAY, sua trong tt-core/modules roi Generate */\n",
		TT_CORE_VERSION, $theme, $ver, $info['built'], count( $summary )
	);
	$out = $header . $out;

	$res['ver']      = $ver;
	$res['size']     = strlen( $out );
	$res['size_raw'] = strlen( $body );
	$res['minified'] = $minified;
	$res['modules']  = $summary;

	if ( $args['dry'] ) {
		$res['ok'] = true;
		return $res;
	}

	// Ghi an toàn: tmp -> (core.js cũ thành core.prev.js) -> rename.
	$file = $dir . '/core.js';
	$tmp  = $file . '.tmp';
	if ( false === @file_put_contents( $tmp, $out ) ) {
		$res['errors'][] = 'Không ghi được ' . $tmp;
	} else {
		if ( is_file( $file ) ) {
			@copy( $file, $dir . '/core.prev.js' );
		}
		if ( ! @rename( $tmp, $file ) ) {
			@unlink( $tmp );
			$res['errors'][] = 'Không đổi tên được ' . $tmp;
		}
	}

	if ( ! $res['errors'] ) {
		$all  = tt_core_builds();
		$prev = isset( $all[ $theme ] ) ? $all[ $theme ] : array();
		$hist = isset( $prev['history'] ) ? (array) $prev['history'] : array();
		if ( ! $hist || end( $hist ) !== $ver ) {
			$hist[] = $ver;
		}
		$all[ $theme ] = array(
			'ver'       => $ver,
			'built'     => $info['built'],
			'by'        => function_exists( 'wp_get_current_user' ) && get_current_user_id() ? wp_get_current_user()->user_login : ( defined( 'WP_CLI' ) ? 'wp-cli' : 'system' ),
			'reason'    => $args['reason'],
			'size'      => $res['size'],
			'size_raw'  => $res['size_raw'],
			'minified'  => $minified,
			'checked'   => $checked,
			'modules'   => $summary,
			'signature' => tt_core_signature( $theme, $c['modules'] ),
			'history'   => array_slice( $hist, -30 ), // để purge Cloudflare sau này
		);
		update_site_option( 'tt_core_builds', $all );
		@file_put_contents( $dir . '/core.manifest.json', wp_json_encode( $all[ $theme ], JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE ) );
		tt_core_clear_dirty( $theme );
		delete_site_transient( 'tt_core_notice_' . $theme );
		$res['ok'] = true;
	}

	if ( $lock ) {
		flock( $lock, LOCK_UN );
		fclose( $lock );
	}
	return $res;
}

/** Quay về core.prev.js. */
function tt_core_rollback( $theme = null ) {
	$theme = $theme ? $theme : get_stylesheet();
	$dir   = tt_core_dir( $theme );
	$prev  = $dir . '/core.prev.js';
	if ( ! is_file( $prev ) ) {
		return array( 'ok' => false, 'errors' => array( 'Không có core.prev.js' ) );
	}
	$code = file_get_contents( $prev );
	if ( ! preg_match( '/\| ver ([0-9a-f]{10}) \|/', $code, $m ) ) {
		return array( 'ok' => false, 'errors' => array( 'core.prev.js không có dòng ver' ) );
	}
	$cur = $dir . '/core.js';
	if ( is_file( $cur ) ) {
		@copy( $cur, $dir . '/core.rolledback.js' );
	}
	if ( ! @copy( $prev, $cur ) ) {
		return array( 'ok' => false, 'errors' => array( 'Không chép được core.prev.js' ) );
	}
	$all = tt_core_builds();
	if ( isset( $all[ $theme ] ) ) {
		$all[ $theme ]['ver']       = $m[1];
		$all[ $theme ]['reason']    = 'rollback';
		$all[ $theme ]['signature'] = ''; // buộc dashboard báo "cần build"
		$all[ $theme ]['history'][] = $m[1];
		update_site_option( 'tt_core_builds', $all );
	}
	return array( 'ok' => true, 'ver' => $m[1] );
}

/* =========================================================================
 * 6. Frontend: enqueue
 * ====================================================================== */

/**
 * Enqueue core.js của theme hiện tại. Trả về false nếu chưa có bản build
 * (functions.php khi đó dùng cách cũ).
 */
function tt_core_enqueue() {
	$theme = get_stylesheet();
	$b     = tt_core_build_info( $theme );
	if ( ! $b || empty( $b['ver'] ) ) {
		return false;
	}
	wp_enqueue_script( 'tt-core', tt_core_url( $theme ) . '/core.js', array(), $b['ver'], true );
	return true;
}

do_action( 'tt_core_init' );

/* =========================================================================
 * 7. Dashboard: Giao diện → TT Core
 * ====================================================================== */

add_action( 'admin_menu', function () {
	add_theme_page( 'TT Core', 'TT Core', 'manage_options', 'tt-core', 'tt_core_admin_page' );
} );

add_action( 'admin_notices', function () {
	if ( ! current_user_can( 'manage_options' ) ) {
		return;
	}
	$theme = get_stylesheet();
	$n     = get_site_transient( 'tt_core_notice_' . $theme );
	if ( $n && ! empty( $n['errors'] ) ) {
		echo '<div class="notice notice-error"><p><strong>TT Core:</strong> build tự động thất bại, đang dùng bản cũ. ';
		echo esc_html( implode( ' | ', $n['errors'] ) );
		echo ' <a href="' . esc_url( admin_url( 'themes.php?page=tt-core' ) ) . '">Mở TT Core</a></p></div>';
	}
} );

function tt_core_admin_page() {
	if ( ! current_user_can( 'manage_options' ) ) {
		return;
	}
	$theme  = get_stylesheet();
	$result = null;

	if ( isset( $_POST['tt_core_action'] ) ) {
		check_admin_referer( 'tt_core_admin' );
		$action = sanitize_key( wp_unslash( $_POST['tt_core_action'] ) );

		if ( 'save' === $action || 'save_build' === $action ) {
			$disabled = array();
			$device   = array();
			$posted   = isset( $_POST['m'] ) && is_array( $_POST['m'] ) ? wp_unslash( $_POST['m'] ) : array();
			foreach ( $posted as $id => $row ) {
				$id = sanitize_key( $id );
				if ( empty( $row['enabled'] ) ) {
					$disabled[] = $id;
				}
				$dev = isset( $row['device'] ) ? sanitize_key( $row['device'] ) : '';
				$def = isset( $row['device_def'] ) ? sanitize_key( $row['device_def'] ) : '';
				if ( in_array( $dev, array( 'all', 'desktop', 'mobile' ), true ) && $dev !== $def ) {
					$device[ $id ] = $dev;
				}
			}
			tt_core_save_settings( $theme, array( 'disabled' => $disabled, 'device' => $device ) );
			$result = array( 'ok' => true, 'msg' => 'Đã lưu thiết lập.' );
			if ( 'save_build' === $action ) {
				$result = tt_core_build( $theme, array( 'reason' => 'dashboard' ) );
			}
		} elseif ( 'build' === $action ) {
			$result = tt_core_build( $theme, array( 'reason' => 'dashboard' ) );
		} elseif ( 'check' === $action ) {
			$result = tt_core_build( $theme, array( 'dry' => true ) );
			$result['msg'] = 'Kiểm tra xong (không ghi file).';
		} elseif ( 'rollback' === $action ) {
			$result = tt_core_rollback( $theme );
		}
	}

	$c     = tt_core_collect( $theme );
	$b     = tt_core_build_info( $theme );
	$sig   = tt_core_signature( $theme, $c['modules'] );
	$stale = ! $b || empty( $b['signature'] ) || $b['signature'] !== $sig;
	$dirty = tt_core_is_dirty( $theme );

	echo '<div class="wrap"><h1>TT Core <small style="font-weight:normal">(' . esc_html( $theme ) . ', engine ' . esc_html( TT_CORE_VERSION ) . ')</small></h1>';

	if ( $result ) {
		$ok  = ! empty( $result['ok'] );
		$cls = $ok ? 'notice-success' : 'notice-error';
		echo '<div class="notice ' . esc_attr( $cls ) . '"><p>';
		if ( ! empty( $result['msg'] ) ) {
			echo esc_html( $result['msg'] ) . ' ';
		}
		if ( $ok && ! empty( $result['ver'] ) ) {
			printf( 'ver <code>%s</code>, %s byte%s.', esc_html( $result['ver'] ), number_format_i18n( isset( $result['size'] ) ? $result['size'] : 0 ), ! empty( $result['minified'] ) ? ' (đã minify)' : '' );
		}
		foreach ( (array) ( isset( $result['errors'] ) ? $result['errors'] : array() ) as $e ) {
			echo '<br>❌ ' . esc_html( $e );
		}
		foreach ( (array) ( isset( $result['warnings'] ) ? $result['warnings'] : array() ) as $w ) {
			echo '<br>⚠️ ' . esc_html( $w );
		}
		echo '</p></div>';
	}

	// Bản build hiện tại
	echo '<h2>Bản build hiện tại</h2>';
	if ( $b ) {
		printf(
			'<p>ver <code>%s</code> · %s · bởi %s (%s) · %s byte (gốc %s)%s%s</p>',
			esc_html( $b['ver'] ), esc_html( $b['built'] ), esc_html( $b['by'] ), esc_html( $b['reason'] ),
			number_format_i18n( $b['size'] ), number_format_i18n( $b['size_raw'] ),
			$b['minified'] ? ' · đã minify' : ' · <strong>chưa minify</strong>',
			$b['checked'] ? ' · đã kiểm cú pháp' : ' · <strong>chưa kiểm cú pháp</strong>'
		);
		echo '<p>URL: <code>' . esc_html( tt_core_url( $theme ) . '/core.js?ver=' . $b['ver'] ) . '</code></p>';
	} else {
		echo '<p><strong>Chưa có bản build.</strong> Theme đang dùng JS cũ (đường lui trong functions.php).</p>';
	}
	if ( $stale ) {
		echo '<p style="color:#b32d2e"><strong>Có thay đổi chưa build</strong>' . ( $dirty ? ' (' . esc_html( $dirty['reason'] ) . ')' : '' ) . '. Bấm Generate để áp dụng.</p>';
	}
	printf(
		'<p>Thư viện: Peast %s · minify %s</p>',
		tt_core_has_peast() ? '✅' : '❌ (không kiểm cú pháp được)',
		tt_core_has_minify() ? '✅' : '❌'
	);
	echo '<p>Module tự sinh của plugin: <code>' . esc_html( tt_core_upload_modules_dir( $theme ) ) . '</code></p>';

	foreach ( $c['warnings'] as $w ) {
		echo '<p>⚠️ ' . esc_html( $w ) . '</p>';
	}

	// Bảng module
	echo '<form method="post">';
	wp_nonce_field( 'tt_core_admin' );
	echo '<table class="widefat striped" style="max-width:1100px"><thead><tr>';
	echo '<th>Bật</th><th>order</th><th>id</th><th>Thiết bị</th><th>ver</th><th>Nguồn</th><th>Kích thước</th><th>Trong bản build</th>';
	echo '</tr></thead><tbody>';
	foreach ( $c['modules'] as $id => $m ) {
		$size = '' !== $m['src'] ? 'tải: ' . $m['src'] : ( null !== $m['code'] ? strlen( $m['code'] ) . ' B' : number_format_i18n( (int) @filesize( $m['file'] ) ) . ' B' );
		$where = '' !== $m['file'] ? str_replace( WP_CONTENT_DIR, 'wp-content', $m['file'] ) : $m['source'];
		echo '<tr>';
		printf( '<td><input type="checkbox" name="m[%1$s][enabled]" value="1" %2$s></td>', esc_attr( $id ), checked( $m['enabled'], true, false ) );
		echo '<td>' . (int) $m['order'] . '</td>';
		echo '<td><code>' . esc_html( $id ) . '</code></td>';
		echo '<td><select name="m[' . esc_attr( $id ) . '][device]">';
		foreach ( array( 'all', 'desktop', 'mobile' ) as $d ) {
			printf( '<option value="%1$s" %2$s>%1$s%3$s</option>', esc_attr( $d ), selected( $m['device'], $d, false ), $d === $m['device_def'] ? ' (mặc định)' : '' );
		}
		echo '</select><input type="hidden" name="m[' . esc_attr( $id ) . '][device_def]" value="' . esc_attr( $m['device_def'] ) . '"></td>';
		echo '<td>' . esc_html( $m['ver'] ) . '</td>';
		echo '<td title="' . esc_attr( $where ) . '">' . esc_html( $m['source'] ) . '</td>';
		echo '<td>' . esc_html( $size ) . '</td>';
		echo '<td>' . ( $b && isset( $b['modules'][ $id ] ) ? '✅' : '—' ) . '</td>';
		echo '</tr>';
	}
	echo '</tbody></table><p>';
	echo '<button class="button" name="tt_core_action" value="save">Lưu thiết lập</button> ';
	echo '<button class="button button-primary" name="tt_core_action" value="save_build">Lưu và Generate</button> ';
	echo '<button class="button" name="tt_core_action" value="check">Kiểm tra cú pháp</button> ';
	echo '<button class="button" name="tt_core_action" value="rollback" onclick="return confirm(\'Quay về bản build trước?\')">Quay về bản trước</button>';
	echo '</p></form>';
	echo '<p style="color:#666">Code module sửa trong file (<code>tt-core/modules/*.js</code> của theme, hoặc thư mục uploads ở trên), không sửa ở đây. Sửa xong bấm Generate.</p>';
	echo '</div>';
}

/* =========================================================================
 * 8. WP-CLI: wp tt-core status|check|build|rollback  (dùng --url=<site> trong multisite)
 * ====================================================================== */

if ( defined( 'WP_CLI' ) && WP_CLI ) {
	WP_CLI::add_command( 'tt-core', 'TT_Core_CLI' );

	class TT_Core_CLI {
		/** Trạng thái bản build và module. */
		public function status() {
			$theme = get_stylesheet();
			$b     = tt_core_build_info( $theme );
			$c     = tt_core_collect( $theme );
			WP_CLI::log( 'theme: ' . $theme . '  engine ' . TT_CORE_VERSION );
			WP_CLI::log( $b ? sprintf( 'build: ver %s, %s, %d byte, minified=%d, checked=%d', $b['ver'], $b['built'], $b['size'], $b['minified'], $b['checked'] ) : 'build: (chưa có)' );
			$sig = tt_core_signature( $theme, $c['modules'] );
			WP_CLI::log( 'stale: ' . ( ! $b || $b['signature'] !== $sig ? 'YES' : 'no' ) );
			WP_CLI::log( 'peast: ' . ( tt_core_has_peast() ? 'yes' : 'NO' ) . '  minify: ' . ( tt_core_has_minify() ? 'yes' : 'NO' ) );
			$rows = array();
			foreach ( $c['modules'] as $m ) {
				$rows[] = array(
					'order'   => $m['order'],
					'id'      => $m['id'],
					'device'  => $m['device'],
					'ver'     => $m['ver'],
					'source'  => $m['source'],
					'enabled' => $m['enabled'] ? 'yes' : 'no',
				);
			}
			WP_CLI\Utils\format_items( 'table', $rows, array( 'order', 'id', 'device', 'ver', 'source', 'enabled' ) );
			foreach ( $c['warnings'] as $w ) {
				WP_CLI::warning( $w );
			}
		}

		/** Kiểm tra cú pháp và build thử, không ghi file. */
		public function check() {
			$this->report( tt_core_build( get_stylesheet(), array( 'dry' => true ) ) );
		}

		/** Build core.js. */
		public function build() {
			$this->report( tt_core_build( get_stylesheet(), array( 'reason' => 'wp-cli' ) ) );
		}

		/** Quay về core.prev.js. */
		public function rollback() {
			$this->report( tt_core_rollback( get_stylesheet() ) );
		}

		private function report( $r ) {
			foreach ( (array) ( isset( $r['warnings'] ) ? $r['warnings'] : array() ) as $w ) {
				WP_CLI::warning( $w );
			}
			if ( empty( $r['ok'] ) ) {
				WP_CLI::error( implode( "\n", (array) $r['errors'] ) );
			}
			WP_CLI::success( sprintf(
				'ver %s%s',
				isset( $r['ver'] ) ? $r['ver'] : '?',
				isset( $r['size'] ) ? sprintf( ', %d byte (gốc %d), minified=%d, %d module', $r['size'], $r['size_raw'], $r['minified'], count( $r['modules'] ) ) : ''
			) );
		}
	}
}
