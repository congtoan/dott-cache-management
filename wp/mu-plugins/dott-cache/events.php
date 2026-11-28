<?php
/**
 * events.php: đọc sự kiện do mu-plugin dott-cache-events.php ghi, từ một mốc trở đi. KHÔNG nạp WordPress.
 * Đặt trong wp-content/mu-plugins/dott-cache/ (WordPress không tự nạp file trong thư mục con; script tự
 * thoát nếu không chạy bằng CLI).
 *
 * Bình thường được gọi từ VPS phụ:  ssh main dott-prefork <domain> --events --after=<mốc>
 * Chạy tay (bằng user của site):    php events.php --dir=<thư mục sự kiện> --after=init|YYYYMMDD:OFFSET
 *
 * Mốc = "YYYYMMDD:byte offset" trong file YYYYMMDD.jsonl. "init": không trả sự kiện, chỉ trả mốc hiện tại
 * (cuối file mới nhất), dùng cho lần chạy đầu. Chỉ trả dòng đã ghi trọn (kết thúc bằng \n).
 * Xóa file quá 14 ngày.
 *
 * stdout:
 *   E {json}            một sự kiện
 *   CURSOR YYYYMMDD:N   mốc mới (lần sau gọi với --after=giá trị này)
 *   # log
 *   DONE events=N
 */

if ( PHP_SAPI !== 'cli' ) {
	exit( 1 );
}

$opt   = getopt( '', array( 'dir:', 'after:', 'max:' ) );
$dir   = rtrim( (string) ( $opt['dir'] ?? '' ), '/' );
$after = (string) ( $opt['after'] ?? 'init' );
$max   = max( 1, (int) ( $opt['max'] ?? 20000 ) );

function ev_out( $s ) {
	fwrite( STDOUT, $s . "\n" );
}

if ( '' === $dir ) {
	ev_out( '# thiếu --dir' );
	ev_out( 'DONE events=0' );
	exit( 2 );
}
if ( 'init' !== $after && ! preg_match( '/^\d{8}:\d+$/', $after ) ) {
	ev_out( '# --after phải là init hoặc YYYYMMDD:OFFSET' );
	ev_out( 'DONE events=0' );
	exit( 2 );
}

$files = array();
if ( is_dir( $dir ) ) {
	foreach ( scandir( $dir ) as $f ) {
		if ( preg_match( '/^(\d{8})\.jsonl$/', $f, $m ) ) {
			$files[ $m[1] ] = $dir . '/' . $f;
		}
	}
}
ksort( $files, SORT_STRING );

// Dọn file quá 14 ngày (giữ để soát lỗi).
$old = gmdate( 'Ymd', time() - 14 * 86400 );
foreach ( $files as $d => $f ) {
	if ( strcmp( (string) $d, $old ) < 0 ) {
		@unlink( $f );
		unset( $files[ $d ] );
	}
}

/** Offset sau dòng trọn cuối cùng của file. */
function ev_complete_end( $f ) {
	$size = (int) @filesize( $f );
	if ( $size <= 0 ) {
		return 0;
	}
	$h = fopen( $f, 'rb' );
	$pos = $size;
	while ( $pos > 0 ) {
		$n = min( 4096, $pos );
		fseek( $h, $pos - $n );
		$chunk = fread( $h, $n );
		$i = strrpos( $chunk, "\n" );
		if ( false !== $i ) {
			fclose( $h );
			return $pos - $n + $i + 1;
		}
		$pos -= $n;
	}
	fclose( $h );
	return 0;
}

if ( 'init' === $after ) {
	if ( $files ) {
		$d = array_key_last( $files );
		ev_out( 'CURSOR ' . $d . ':' . ev_complete_end( $files[ $d ] ) );
	} else {
		ev_out( 'CURSOR ' . gmdate( 'Ymd' ) . ':0' );
	}
	ev_out( '# khởi tạo mốc, ' . count( $files ) . ' file' );
	ev_out( 'DONE events=0' );
	exit( 0 );
}

list( $cd, $co ) = explode( ':', $after );
$co   = (int) $co;
$n    = 0;
$curD = $cd;
$curO = $co;
foreach ( $files as $d => $f ) {
	$d = (string) $d; // PHP đổi key "20261128" thành số nguyên
	if ( strcmp( $d, $cd ) < 0 ) {
		continue;
	}
	$off = ( $d === $cd ) ? $co : 0;
	$h   = @fopen( $f, 'rb' );
	if ( ! $h ) {
		ev_out( "# không mở được $f" );
		break;
	}
	if ( $off > (int) filesize( $f ) ) {
		ev_out( "# mốc $after vượt quá cỡ file, đọc lại từ đầu file" );
		$off = 0;
	}
	fseek( $h, $off );
	$curD    = $d;
	$curO    = $off;
	$partial = false;
	while ( $n < $max && false !== ( $l = fgets( $h ) ) ) {
		if ( "\n" !== substr( $l, -1 ) ) {
			$partial = true; // dòng đang ghi dở: dừng ở đây, lần sau đọc tiếp
			break;
		}
		$curO += strlen( $l );
		$l = rtrim( $l, "\r\n" );
		if ( '' !== $l ) {
			ev_out( 'E ' . $l );
			$n++;
		}
	}
	fclose( $h );
	if ( $n >= $max ) {
		ev_out( "# dừng ở $max sự kiện" );
		break;
	}
	if ( $partial ) {
		break;
	}
}
ev_out( 'CURSOR ' . $curD . ':' . $curO );
ev_out( 'DONE events=' . $n );
