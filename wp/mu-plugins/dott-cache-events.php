<?php
/**
 * Plugin Name: dott-cache events
 * Description: Ghi sự kiện (sửa/đăng/gỡ bài, bình luận, sửa term) ra file để dott-cache trên VPS phụ cập nhật cache gần như ngay.
 * Version: 1.0.0
 *
 * Cài: wp-content/mu-plugins/dott-cache-events.php (nguồn: wp/mu-plugins/ trong repo cache-system v2).
 *
 * Plugin chỉ GHI FILE, không gọi mạng, không render, không xóa cache:
 *   <thư mục site>/dott-cache-events/YYYYMMDD.jsonl   (ngoài public_html, nằm trong open_basedir của site)
 * Mỗi dòng là một sự kiện đã tính sẵn path (cùng cách chuẩn hóa với export-urls.php):
 *   {"t":unix,"b":blog_id,"bp":"/","r":"post_updated","id":123,
 *    "refresh":[{"p":"/bai/","k":"single","t":"post","id":123}, {"p":"/cat/","k":"term","t":"category","id":5,"pg":1}, {"p":"/","k":"home"}],
 *    "remove":[{"p":"/slug-cu/","k":"single","t":"post","id":123}]}
 * VPS phụ đọc file qua "dott-prefork <domain> --events --after=<mốc>" (dott-cache/events.php), lọc theo config,
 * rồi chạy update (refresh) hoặc gỡ KV → server → S3 (remove).
 *
 * Path được tính ở cuối request (shutdown), khi term và slug đã lưu xong (Gutenberg lưu term sau
 * wp_update_post). Path cũ (trước khi đổi slug, gỡ bài, xóa term) được chụp lại trước khi thay đổi.
 *
 * Lỗi chỉ ghi error_log, không bao giờ làm hỏng thao tác của biên tập viên.
 * Tắt nhanh: define( 'DOTT_CACHE_EVENTS_OFF', true ) trong wp-config.php, hoặc xóa file này.
 */

defined( 'ABSPATH' ) || exit;

if ( defined( 'DOTT_CACHE_EVENTS_OFF' ) && DOTT_CACHE_EVENTS_OFF ) {
	return;
}

final class Dott_Cache_Events {
	/** @var array blog_id => post_id => 'full' | 'comment' */
	private static $posts = array();
	/** @var array blog_id => post_id => array( 'remove' => [...], 'arch' => [...], 'r' => reason ) chụp trước khi đổi */
	private static $before = array();
	/** @var array các sự kiện term đã tính xong: list of event arrays */
	private static $done = array();
	private static $hooked = false;

	public static function init() {
		add_action( 'pre_post_update', array( __CLASS__, 'pre_post_update' ), 10, 1 );
		add_action( 'wp_trash_post', array( __CLASS__, 'snapshot_hook' ), 1, 1 );
		add_action( 'before_delete_post', array( __CLASS__, 'snapshot_hook' ), 1, 1 );
		add_action( 'transition_post_status', array( __CLASS__, 'transition_post_status' ), 10, 3 );

		add_action( 'comment_post', array( __CLASS__, 'comment_post' ), 10, 2 );
		add_action( 'transition_comment_status', array( __CLASS__, 'transition_comment_status' ), 10, 3 );
		add_action( 'edit_comment', array( __CLASS__, 'comment_changed' ), 10, 1 );
		add_action( 'delete_comment', array( __CLASS__, 'comment_changed' ), 10, 1 );

		add_action( 'edited_term', array( __CLASS__, 'edited_term' ), 10, 3 );
		add_action( 'pre_delete_term', array( __CLASS__, 'pre_delete_term' ), 10, 2 );

		// Xóa cache một bài trong admin WP Rocket ("Clear this cache"): origin mất cache, S3 vẫn bản cũ.
		add_action( 'after_rocket_clean_post', array( __CLASS__, 'rocket_clean_post' ), 10, 1 );
	}

	// ---------- Chuẩn hóa path: giống export-urls.php ----------

	private static function path( $url ) {
		if ( ! is_string( $url ) || '' === $url ) {
			return null;
		}
		$u    = wp_parse_url( $url );
		$host = wp_parse_url( home_url( '/' ), PHP_URL_HOST );
		if ( empty( $u['host'] ) || strtolower( $u['host'] ) !== strtolower( (string) $host ) || ! empty( $u['query'] ) ) {
			return null;
		}
		$p = isset( $u['path'] ) ? $u['path'] : '/';
		return '/' === substr( $p, -1 ) ? $p : $p . '/';
	}

	private static function cacheable( $post ) {
		if ( ! $post || ! is_object( $post ) ) {
			return false;
		}
		if ( 'attachment' === $post->post_type || wp_is_post_revision( $post ) || wp_is_post_autosave( $post ) ) {
			return false;
		}
		$pto = get_post_type_object( $post->post_type );
		return $pto && $pto->public;
	}

	/** URL của bài (khi đang publish). */
	private static function post_item( $post ) {
		$p = self::path( get_permalink( $post ) );
		return $p ? array( 'p' => $p, 'k' => 'single', 't' => $post->post_type, 'id' => (int) $post->ID ) : null;
	}

	/** Trang 1 archive của mọi term public của bài, cộng trang chủ. */
	private static function archive_items( $post ) {
		$out = array();
		$taxes = get_object_taxonomies( $post->post_type, 'objects' );
		foreach ( $taxes as $tax ) {
			if ( empty( $tax->public ) || empty( $tax->rewrite ) ) {
				continue;
			}
			$terms = get_the_terms( $post->ID, $tax->name );
			if ( ! $terms || is_wp_error( $terms ) ) {
				continue;
			}
			foreach ( $terms as $t ) {
				$it = self::term_item( $t );
				if ( $it ) {
					$out[ $it['p'] ] = $it;
				}
			}
		}
		$hp = self::path( home_url( '/' ) );
		if ( $hp ) {
			$out[ $hp ] = array( 'p' => $hp, 'k' => 'home', 't' => '', 'id' => 0, 'pg' => 1 );
		}
		return $out;
	}

	private static function term_item( $t ) {
		$link = get_term_link( $t );
		if ( is_wp_error( $link ) ) {
			return null;
		}
		$p = self::path( $link );
		return $p ? array( 'p' => $p, 'k' => 'term', 't' => $t->taxonomy, 'id' => (int) $t->term_id, 'pg' => 1 ) : null;
	}

	private static function hook_shutdown() {
		if ( ! self::$hooked ) {
			self::$hooked = true;
			add_action( 'shutdown', array( __CLASS__, 'flush' ), 1 );
		}
	}

	// ---------- Bài viết ----------

	/** Chụp path và archive hiện tại của bài đang publish, trước khi bị sửa / gỡ / xóa (chỉ lần đầu). */
	private static function snapshot( $post_id, $reason ) {
		$b = get_current_blog_id();
		if ( isset( self::$before[ $b ][ $post_id ] ) ) {
			return;
		}
		$post = get_post( $post_id );
		if ( ! $post || 'publish' !== $post->post_status || ! self::cacheable( $post ) ) {
			return;
		}
		$it = self::post_item( $post );
		self::$before[ $b ][ $post_id ] = array(
			'item' => $it,
			'arch' => self::archive_items( $post ),
			'r'    => $reason,
		);
		self::$posts[ $b ][ $post_id ] = 'full';
		self::hook_shutdown();
	}

	public static function pre_post_update( $post_id ) {
		self::safe( function () use ( $post_id ) {
			self::snapshot( (int) $post_id, 'post_updated' );
		} );
	}

	public static function snapshot_hook( $post_id ) {
		self::safe( function () use ( $post_id ) {
			self::snapshot( (int) $post_id, current_filter() );
		} );
	}

	public static function transition_post_status( $new, $old, $post ) {
		self::safe( function () use ( $new, $old, $post ) {
			if ( ( 'publish' !== $new && 'publish' !== $old ) || ! self::cacheable( $post ) ) {
				return;
			}
			self::$posts[ get_current_blog_id() ][ (int) $post->ID ] = 'full';
			self::hook_shutdown();
		} );
	}

	public static function rocket_clean_post( $post ) {
		self::safe( function () use ( $post ) {
			if ( $post && 'publish' === $post->post_status && self::cacheable( $post ) ) {
				self::mark_comment( (int) $post->ID ); // chỉ làm mới URL bài
			}
		} );
	}

	// ---------- Bình luận: chỉ làm mới URL bài ----------

	private static function mark_comment( $post_id ) {
		if ( $post_id <= 0 ) {
			return;
		}
		$b = get_current_blog_id();
		if ( ! isset( self::$posts[ $b ][ $post_id ] ) ) {
			self::$posts[ $b ][ $post_id ] = 'comment';
		}
		self::hook_shutdown();
	}

	public static function comment_post( $comment_id, $approved ) {
		self::safe( function () use ( $comment_id, $approved ) {
			if ( 1 === $approved || '1' === $approved ) {
				$c = get_comment( $comment_id );
				if ( $c ) {
					self::mark_comment( (int) $c->comment_post_ID );
				}
			}
		} );
	}

	public static function transition_comment_status( $new, $old, $comment ) {
		self::safe( function () use ( $new, $old, $comment ) {
			if ( ( 'approved' === $new || 'approved' === $old ) && $comment ) {
				self::mark_comment( (int) $comment->comment_post_ID );
			}
		} );
	}

	public static function comment_changed( $comment_id ) {
		self::safe( function () use ( $comment_id ) {
			$c = get_comment( $comment_id );
			if ( $c && '1' === (string) $c->comment_approved ) {
				self::mark_comment( (int) $c->comment_post_ID );
			}
		} );
	}

	// ---------- Term ----------

	public static function edited_term( $term_id, $tt_id, $taxonomy ) {
		self::safe( function () use ( $term_id, $taxonomy ) {
			$tax = get_taxonomy( $taxonomy );
			if ( ! $tax || empty( $tax->public ) || empty( $tax->rewrite ) ) {
				return;
			}
			$t  = get_term( (int) $term_id, $taxonomy );
			$it = ( $t && ! is_wp_error( $t ) && $t->count > 0 ) ? self::term_item( $t ) : null;
			if ( $it ) {
				self::$done[] = self::event( 'edited_term', (int) $term_id, array( $it ), array() );
				self::hook_shutdown();
			}
		} );
	}

	public static function pre_delete_term( $term_id, $taxonomy ) {
		self::safe( function () use ( $term_id, $taxonomy ) {
			$tax = get_taxonomy( $taxonomy );
			if ( ! $tax || empty( $tax->public ) || empty( $tax->rewrite ) ) {
				return;
			}
			$t  = get_term( (int) $term_id, $taxonomy );
			$it = ( $t && ! is_wp_error( $t ) ) ? self::term_item( $t ) : null;
			if ( $it ) {
				// Chỉ trang 1; các trang /page/N/ được "sync -full" hằng ngày gỡ.
				self::$done[] = self::event( 'delete_term', (int) $term_id, array(), array( $it ) );
				self::hook_shutdown();
			}
		} );
	}

	// ---------- Ghi ra file ----------

	private static function event( $reason, $id, array $refresh, array $remove ) {
		$bp = '/';
		if ( is_multisite() ) {
			$d  = get_blog_details( get_current_blog_id() );
			$bp = $d ? $d->path : '/';
		}
		$b = get_current_blog_id();
		$fix = function ( array $items ) use ( $b, $bp ) {
			$out = array();
			foreach ( $items as $it ) {
				if ( $it ) {
					$it['b']  = $b;
					$it['bp'] = $bp;
					$out[]    = $it;
				}
			}
			return $out;
		};
		return array(
			't'       => time(),
			'b'       => $b,
			'bp'      => $bp,
			'r'       => $reason,
			'id'      => $id,
			'refresh' => $fix( array_values( $refresh ) ),
			'remove'  => $fix( array_values( $remove ) ),
		);
	}

	/** Tính sự kiện của một bài (đang ở đúng blog). */
	private static function post_event( $post_id, $kind ) {
		$before = isset( self::$before[ get_current_blog_id() ][ $post_id ] ) ? self::$before[ get_current_blog_id() ][ $post_id ] : null;
		// Không gọi clean_post_cache(): WP Rocket gắn rocket_clean_post vào hook đó. WordPress đã tự xóa
		// cache của bài khi lưu.
		$post    = get_post( $post_id );
		$live    = $post && 'publish' === $post->post_status && self::cacheable( $post );
		$refresh = array();
		$remove  = array();
		$reason  = $before ? $before['r'] : ( 'comment' === $kind ? 'comment' : 'publish' );

		if ( $live ) {
			$it = self::post_item( $post );
			if ( $it ) {
				$refresh[ $it['p'] ] = $it;
			}
			if ( 'full' === $kind ) {
				$refresh += self::archive_items( $post );
				if ( $before ) {
					$refresh += $before['arch']; // term cũ (bài bị bỏ khỏi chuyên mục)
				}
			}
			if ( $before && $before['item'] && ( ! $it || $before['item']['p'] !== $it['p'] ) ) {
				$remove[] = $before['item']; // đổi slug / parent
				$reason   = 'slug_changed';
			}
		} elseif ( $before ) {
			// Bài đã gỡ (nháp, riêng tư, thùng rác, xóa hẳn).
			if ( $before['item'] ) {
				$remove[] = $before['item'];
			}
			$refresh = $before['arch'];
			$reason  = 'unpublished';
		} else {
			return null;
		}
		if ( ! $refresh && ! $remove ) {
			return null;
		}
		foreach ( $remove as $r ) {
			unset( $refresh[ $r['p'] ] );
		}
		return self::event( $reason, (int) $post_id, $refresh, $remove );
	}

	public static function flush() {
		self::safe( function () {
			$events = self::$done;
			foreach ( self::$posts as $b => $ids ) {
				$switched = false;
				if ( is_multisite() && (int) $b !== get_current_blog_id() ) {
					switch_to_blog( (int) $b );
					$switched = true;
				}
				foreach ( $ids as $id => $kind ) {
					$e = self::post_event( (int) $id, $kind );
					if ( $e ) {
						$events[] = $e;
					}
				}
				if ( $switched ) {
					restore_current_blog();
				}
			}
			self::$posts  = array();
			self::$before = array();
			self::$done   = array();
			if ( ! $events ) {
				return;
			}
			$buf = '';
			foreach ( $events as $e ) {
				$buf .= wp_json_encode( $e, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE ) . "\n";
			}
			$dir = self::dir();
			if ( ! is_dir( $dir ) && ! @mkdir( $dir, 0750, true ) && ! is_dir( $dir ) ) {
				error_log( 'dott-cache-events: không tạo được ' . $dir );
				return;
			}
			$f = $dir . '/' . gmdate( 'Ymd' ) . '.jsonl';
			if ( false === @file_put_contents( $f, $buf, FILE_APPEND | LOCK_EX ) ) {
				error_log( 'dott-cache-events: không ghi được ' . $f );
			}
		} );
	}

	public static function dir() {
		if ( defined( 'DOTT_CACHE_EVENTS_DIR' ) ) {
			return rtrim( DOTT_CACHE_EVENTS_DIR, '/' );
		}
		return dirname( rtrim( ABSPATH, '/' ) ) . '/dott-cache-events';
	}

	private static function safe( $fn ) {
		try {
			$fn();
		} catch ( \Throwable $e ) {
			error_log( 'dott-cache-events: ' . $e->getMessage() . ' @ ' . $e->getFile() . ':' . $e->getLine() );
		}
	}
}

Dott_Cache_Events::init();
