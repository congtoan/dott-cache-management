<?php
/**
 * replace-cache.php: sửa nhanh nội dung file cache WP Rocket ngay trên đĩa, KHÔNG render lại bằng WordPress.
 * Thay cho replace_cache_content*.py và replace_cache_update_version*.py (2024–2025).
 *
 * Đặt trong wp-content/mu-plugins/dott-cache/ (WordPress không tự nạp file trong thư mục con; script tự
 * thoát nếu không chạy bằng CLI). Gọi từ VPS phụ:  dott-cache replace ...  →  ssh main dott-prefork <domain> --replace
 * Không nạp WordPress, chạy bằng user của site.
 *
 * stdin:
 *   dòng 1: "#job {json}"
 *     mode     "content" | "version"
 *     search, replace, regex (bool)   chế độ content: thay chuỗi thường (mặc định) hoặc regex PCRE
 *     files    ["generatepress_child/style.css", "generatepress_child/tt-core/core.js", ...]   chế độ version:
 *              đường dẫn trong wp-content/themes/. Version mới: core.js → "ver" trong tt-core/core.manifest.json
 *              (hash của bản build tt-core), file khác → filemtime (giống cách theme/GeneratePress in ?ver=)
 *     all      true: mọi file cache của host (lọc theo include/exclude); false: chỉ các path ở các dòng sau
 *     include, exclude   phạm vi blog (vd include "/", exclude ["/en/", "/es/"])
 *     dry      true: chỉ đếm và in vài ví dụ, không ghi
 *     samples  số ví dụ in ra khi dry (mặc định 3)
 *     max_load nghỉ khi load 1 phút vượt mức này (0 = không điều tiết)
 *   các dòng sau: path (/tttest/abc/) khi all = false
 *
 * Với mỗi file index-https.html (và index-mobile-https.html của hệ thống cũ nếu còn) có thay đổi:
 *   ghi HTML mới ra file tạm rồi rename; tạo lại _gzip TỪ HTML mới (gzencode mức 6) rồi rename.
 *   Hai file luôn khớp nhau, và khách không bao giờ nhận file đang ghi dở.
 *
 * stdout:
 *   "# ..."              log
 *   "R {path, n}"        path có index-https.html đã đổi (n = số chỗ thay); VPS phụ upload lại các path này
 *   "SAMPLE {json}"      ví dụ trước/sau (chỉ khi dry)
 *   "REPLACE {json}"     thống kê
 *   "DONE ..."
 */

if ( PHP_SAPI !== 'cli' ) {
	exit( 1 );
}
$opt = getopt( '', array( 'root:', 'host:' ) );
if ( empty( $opt['root'] ) || empty( $opt['host'] ) ) {
	fwrite( STDOUT, "# thiếu --root/--host\nDONE ok=0 fail=0 skip=0\n" );
	exit( 2 );
}
$host = strtolower( $opt['host'] );
$root = rtrim( $opt['root'], '/' );
$base = $root . '/wp-content/cache/wp-rocket/' . $host;

function out( $s ) {
	fwrite( STDOUT, $s . "\n" );
}
function fail( $msg ) {
	out( '# LỖI: ' . $msg );
	out( 'DONE ok=0 fail=1 skip=0' );
	exit( 1 );
}

// ---- Đọc job và danh sách path ----
$job   = null;
$paths = array();
foreach ( (array) @file( 'php://stdin', FILE_IGNORE_NEW_LINES ) as $l ) {
	$l = trim( (string) $l );
	if ( 0 === strpos( $l, '#job ' ) ) {
		$job = json_decode( substr( $l, 5 ), true );
		continue;
	}
	if ( '' === $l || '#' === $l[0] ) {
		continue;
	}
	if ( preg_match( '#^/[A-Za-z0-9/_.%~-]*/$#', $l ) && false === strpos( $l, '..' ) && false === strpos( $l, '//' ) ) {
		$paths[] = $l;
	}
}
if ( ! is_array( $job ) || ! in_array( $job['mode'] ?? '', array( 'content', 'version' ), true ) ) {
	fail( 'thiếu hoặc sai dòng #job' );
}
$dry      = ! empty( $job['dry'] );
$samples  = (int) ( $job['samples'] ?? 3 );
$max_load = (float) ( $job['max_load'] ?? 0 );
$include  = strtolower( (string) ( $job['include'] ?? '/' ) );
$exclude  = array_map( 'strtolower', (array) ( $job['exclude'] ?? array() ) );
$in_scope = function ( $rel ) use ( $include, $exclude ) {
	if ( 0 !== strpos( strtolower( $rel ), $include ) ) {
		return false;
	}
	foreach ( $exclude as $e ) {
		if ( '' !== $e && 0 === strpos( strtolower( $rel ), $e ) ) {
			return false;
		}
	}
	return true;
};

// ---- Hàm biến đổi nội dung ----
if ( 'content' === $job['mode'] ) {
	$search  = (string) ( $job['search'] ?? '' );
	$replace = (string) ( $job['replace'] ?? '' );
	if ( '' === $search ) {
		fail( 'chuỗi cần tìm rỗng' );
	}
	if ( ! empty( $job['regex'] ) ) {
		if ( false === @preg_match( $search, '' ) ) {
			fail( 'regex không hợp lệ: ' . $search );
		}
		$apply = function ( $s ) use ( $search, $replace ) {
			$n = 0;
			$r = preg_replace( $search, $replace, $s, -1, $n );
			return array( null === $r ? $s : $r, $n );
		};
	} else {
		$apply = function ( $s ) use ( $search, $replace ) {
			$n = substr_count( $s, $search );
			return array( $n ? str_replace( $search, $replace, $s ) : $s, $n );
		};
	}
	out( sprintf( '# chế độ content: %s %s → %s', empty( $job['regex'] ) ? 'chuỗi' : 'regex', json_encode( $search, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES ), json_encode( $replace, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES ) ) );
} else {
	$themes = $root . '/wp-content/themes/';
	$rules  = array();
	foreach ( (array) ( $job['files'] ?? array() ) as $rel ) {
		$rel = ltrim( (string) $rel, '/' );
		if ( '' === $rel || false !== strpos( $rel, '..' ) ) {
			continue;
		}
		$full = $themes . $rel;
		if ( ! is_file( $full ) ) {
			out( "# bỏ $rel: không có file $full" );
			continue;
		}
		if ( 'core.js' === basename( $rel ) ) {
			$man = json_decode( (string) @file_get_contents( dirname( $full ) . '/core.manifest.json' ), true );
			$ver = isset( $man['ver'] ) ? (string) $man['ver'] : '';
			$src = 'tt-core/core.manifest.json';
		} else {
			$ver = (string) filemtime( $full );
			$src = 'filemtime';
		}
		if ( '' === $ver || ! preg_match( '/^[A-Za-z0-9._-]+$/', $ver ) ) {
			out( "# bỏ $rel: không xác định được version" );
			continue;
		}
		out( "# version $rel = $ver ($src)" );
		// Khớp ".../themes/<rel>?ver=<cũ>" bất kể tiền tố blog (/en/wp-content/themes/...) và kiểu nháy.
		$rules[] = array( '#(/themes/' . preg_quote( $rel, '#' ) . ')\?ver=[^"\'&\s<>]*#', '${1}?ver=' . $ver );
	}
	if ( ! $rules ) {
		fail( 'không có file nào để cập nhật version' );
	}
	$apply = function ( $s ) use ( $rules ) {
		$total = 0;
		foreach ( $rules as $r ) {
			$n = 0;
			$s = preg_replace( $r[0], $r[1], $s, -1, $n );
			$total += $n;
		}
		return array( $s, $total );
	};
}

// ---- Danh sách file HTML cần xem ----
$t0    = microtime( true );
$files = array();
if ( ! empty( $job['all'] ) ) {
	if ( is_dir( $base ) ) {
		$it = new RecursiveIteratorIterator( new RecursiveDirectoryIterator( $base, FilesystemIterator::SKIP_DOTS ) );
		foreach ( $it as $f ) {
			$n = $f->getFilename();
			if ( ( 'index-https.html' === $n || 'index-mobile-https.html' === $n ) && $f->isFile() ) {
				$rel = substr( $f->getPath(), strlen( $base ) ) . '/';
				if ( $in_scope( $rel ) ) {
					$files[] = array( $rel, $f->getPathname() );
				}
			}
		}
	}
} else {
	foreach ( array_unique( $paths ) as $p ) {
		if ( ! $in_scope( $p ) ) {
			continue;
		}
		foreach ( array( 'index-https.html', 'index-mobile-https.html' ) as $n ) {
			if ( is_file( $base . $p . $n ) ) {
				$files[] = array( $p, $base . $p . $n );
			}
		}
	}
}
out( sprintf( '# %d file HTML trong phạm vi (%.1f s)%s', count( $files ), microtime( true ) - $t0, $dry ? ', DRY-RUN: không ghi' : '' ) );

// ---- Xử lý ----
$st = array( 'files' => count( $files ), 'changed' => 0, 'replacements' => 0, 'errors' => 0, 'mobile_changed' => 0, 'pauses' => 0, 'waited' => 0, 'dry' => $dry );
$shown = 0;
$i     = 0;
foreach ( $files as list( $rel, $file ) ) {
	// Điều tiết chỉ khi ghi thật (chạy thử chỉ đọc file, rất nhẹ).
	if ( ! $dry && $max_load > 0 && 0 === ( $i++ % 50 ) && sys_getloadavg()[0] > $max_load ) {
		$st['pauses']++;
		$w = 0;
		while ( sys_getloadavg()[0] > $max_load * 0.7 && $w < 60 ) {
			sleep( 2 );
			$w += 2;
		}
		$st['waited'] += $w;
	}
	$old = @file_get_contents( $file );
	if ( false === $old ) {
		$st['errors']++;
		continue;
	}
	list( $new, $n ) = $apply( $old );
	if ( $new === $old ) {
		continue;
	}
	$mobile = 'index-mobile-https.html' === basename( $file );
	$st['changed']++;
	$st['replacements'] += $n;
	if ( $mobile ) {
		$st['mobile_changed']++;
	}
	if ( $dry ) {
		if ( $shown < $samples && ! $mobile ) {
			// Đoạn quanh chỗ khác đầu tiên
			$k = 0;
			$max = min( strlen( $old ), strlen( $new ) );
			while ( $k < $max && $old[ $k ] === $new[ $k ] ) {
				$k++;
			}
			$a = max( 0, $k - 60 );
			out( 'SAMPLE ' . json_encode( array( 'path' => $rel, 'n' => $n, 'before' => substr( $old, $a, 160 ), 'after' => substr( $new, $a, 160 ) ), JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES | JSON_INVALID_UTF8_SUBSTITUTE ) );
			$shown++;
		}
	} else {
		$tmp = $file . '.dott-tmp';
		$gz  = $file . '_gzip';
		$ok  = false !== file_put_contents( $tmp, $new ) && rename( $tmp, $file );
		$ok  = $ok && false !== file_put_contents( $gz . '.dott-tmp', gzencode( $new, 6 ) ) && rename( $gz . '.dott-tmp', $gz );
		if ( ! $ok ) {
			@unlink( $tmp );
			@unlink( $gz . '.dott-tmp' );
			$st['errors']++;
			out( 'R ' . json_encode( array( 'path' => $rel, 'ok' => false, 'err' => 'ghi file lỗi' ), JSON_UNESCAPED_SLASHES ) );
			continue;
		}
	}
	if ( ! $mobile ) {
		out( 'R ' . json_encode( array( 'path' => $rel, 'ok' => true, 'n' => $n ), JSON_UNESCAPED_SLASHES ) );
	}
}
$st['seconds'] = round( microtime( true ) - $t0, 1 );
out( 'REPLACE ' . json_encode( $st ) );
out( sprintf( 'DONE ok=%d fail=%d skip=0', $st['changed'] - $st['mobile_changed'], $st['errors'] ) );
