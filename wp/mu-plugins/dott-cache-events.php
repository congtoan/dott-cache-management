<?php
/**
 * Plugin Name: dott-cache events
 * Description: Báo cho dott-cache (VPS phụ) biết bài, bình luận, term nào vừa đổi, để cache được cập nhật ngay.
 * Version: 3.0.0
 *
 * Cài: wp-content/mu-plugins/dott-cache-events.php (nguồn: wp/mu-plugins/ trong repo cache-system v2).
 * Thiết kế: docs/design-pool.md.
 *
 * Plugin MỎNG: chỉ báo "cái gì đổi" và chụp trạng thái CŨ dạng thô (URL cũ, ID term cũ) trước khi sửa.
 * Không chuẩn hóa path, không quyết định gì: dott-cache hỏi lại site trạng thái mới (export-urls.php --ids)
 * rồi tự tính URL nào cần làm mới / gỡ.
 *
 * Cuối request, một POST không chờ trả lời tới URL trong <thư mục site>/dott-cache-hook.json
 *   {"url":"http://127.0.0.1:8790/event","token":"..."}
 * (cổng loopback của VPS chính; SSH reverse tunnel chuyển về "dott-cache serve" trên VPS phụ).
 * Không có file đó thì plugin không làm gì.
 *
 * Body: {"host":"<domain>","events":[
 *   {"b":1,"type":"post","id":123,"old_url":"https://.../slug-cu/","old_terms":["category:5","post_tag:9"]},
 *   {"b":1,"type":"comment","post":123},
 *   {"b":1,"type":"touch","post":123},                       (WP Rocket "clear this cache")
 *   {"b":1,"type":"term","tax":"category","id":5,"old_url":"..."} ]}
 *
 * Lỗi chỉ ghi error_log, không bao giờ làm hỏng thao tác của biên tập viên.
 * Tắt nhanh: xóa dott-cache-hook.json, hoặc define( 'DOTT_CACHE_EVENTS_OFF', true ), hoặc xóa file này.
 */

defined( 'ABSPATH' ) || exit;

if ( defined( 'DOTT_CACHE_EVENTS_OFF' ) && DOTT_CACHE_EVENTS_OFF ) {
	return;
}

final class Dott_Cache_Events {
	/** @var array key => event */
	private static $ev = array();
	/** @var array "blog:post_id" => array( old_url, old_terms ) */
	private static $old = array();
	private static $hooked = false;

	public static function init() {
		add_action( 'pre_post_update', array( __CLASS__, 'snapshot' ), 10, 1 );
		add_action( 'wp_trash_post', array( __CLASS__, 'snapshot' ), 1, 1 );       // trước khi slug bị thêm __trashed
		add_action( 'before_delete_post', array( __CLASS__, 'snapshot' ), 1, 1 );
		add_action( 'transition_post_status', array( __CLASS__, 'transition' ), 10, 3 );

		add_action( 'comment_post', array( __CLASS__, 'comment_post' ), 10, 2 );
		// wp_insert_comment (WP-CLI, import, plugin tự thêm bình luận) không kích comment_post.
		add_action( 'wp_insert_comment', array( __CLASS__, 'insert_comment' ), 10, 2 );
		add_action( 'transition_comment_status', array( __CLASS__, 'comment_status' ), 10, 3 );
		add_action( 'edit_comment', array( __CLASS__, 'comment_changed' ), 10, 1 );
		add_action( 'delete_comment', array( __CLASS__, 'comment_changed' ), 10, 1 );

		add_action( 'edited_term', array( __CLASS__, 'edited_term' ), 10, 3 );
		add_action( 'pre_delete_term', array( __CLASS__, 'pre_delete_term' ), 10, 2 );

		add_action( 'after_rocket_clean_post', array( __CLASS__, 'rocket_clean_post' ), 10, 1 );
	}

	private static function public_post( $post ) {
		if ( ! $post || 'attachment' === $post->post_type || wp_is_post_revision( $post ) || wp_is_post_autosave( $post ) ) {
			return false;
		}
		$pto = get_post_type_object( $post->post_type );
		return $pto && $pto->public;
	}

	private static function add( $key, array $e ) {
		$e = array( 'b' => get_current_blog_id() ) + $e;
		$k = $e['b'] . ':' . $key;
		// Sự kiện "post" bao trùm "comment"/"touch" của cùng bài.
		if ( isset( self::$ev[ $k ] ) && 'post' === self::$ev[ $k ]['type'] && 'post' !== $e['type'] ) {
			return;
		}
		self::$ev[ $k ] = isset( self::$ev[ $k ] ) ? array_merge( self::$ev[ $k ], $e ) : $e;
		if ( ! self::$hooked ) {
			self::$hooked = true;
			add_action( 'shutdown', array( __CLASS__, 'flush' ), 1 );
		}
	}

	// ---------- Bài ----------

	/** Chụp URL và term cũ của bài đang publish, trước khi sửa / gỡ / xóa (chỉ lần đầu trong request). */
	public static function snapshot( $post_id ) {
		self::safe( function () use ( $post_id ) {
			$k = get_current_blog_id() . ':' . (int) $post_id;
			if ( isset( self::$old[ $k ] ) ) {
				return;
			}
			$post = get_post( $post_id );
			if ( ! $post || 'publish' !== $post->post_status || ! self::public_post( $post ) ) {
				return;
			}
			$terms = array();
			$taxes = get_object_taxonomies( $post->post_type, 'names' );
			if ( $taxes ) {
				$ts = wp_get_object_terms( $post->ID, $taxes );
				if ( ! is_wp_error( $ts ) ) {
					foreach ( $ts as $t ) {
						$terms[] = $t->taxonomy . ':' . $t->term_id;
					}
				}
			}
			self::$old[ $k ] = array( 'old_url' => get_permalink( $post ), 'old_terms' => $terms );
			self::add( 'p' . $post->ID, array( 'type' => 'post', 'id' => (int) $post->ID ) + self::$old[ $k ] );
		} );
	}

	public static function transition( $new, $old, $post ) {
		self::safe( function () use ( $new, $old, $post ) {
			if ( ( 'publish' === $new || 'publish' === $old ) && self::public_post( $post ) ) {
				self::add( 'p' . $post->ID, array( 'type' => 'post', 'id' => (int) $post->ID ) );
			}
		} );
	}

	public static function rocket_clean_post( $post ) {
		self::safe( function () use ( $post ) {
			if ( $post && 'publish' === $post->post_status && self::public_post( $post ) ) {
				self::add( 'p' . $post->ID, array( 'type' => 'touch', 'post' => (int) $post->ID ) );
			}
		} );
	}

	// ---------- Bình luận ----------

	private static function comment_on( $post_id ) {
		if ( $post_id > 0 ) {
			self::add( 'p' . $post_id, array( 'type' => 'comment', 'post' => (int) $post_id ) );
		}
	}

	public static function comment_post( $comment_id, $approved ) {
		self::safe( function () use ( $comment_id, $approved ) {
			$c = get_comment( $comment_id );
			if ( $c && 1 === (int) $approved ) {
				self::comment_on( (int) $c->comment_post_ID );
			}
		} );
	}

	public static function insert_comment( $comment_id, $comment ) {
		self::safe( function () use ( $comment ) {
			if ( $comment && '1' === (string) $comment->comment_approved ) {
				self::comment_on( (int) $comment->comment_post_ID );
			}
		} );
	}

	public static function comment_status( $new, $old, $comment ) {
		self::safe( function () use ( $new, $old, $comment ) {
			if ( $comment && ( 'approved' === $new || 'approved' === $old ) ) {
				self::comment_on( (int) $comment->comment_post_ID );
			}
		} );
	}

	public static function comment_changed( $comment_id ) {
		self::safe( function () use ( $comment_id ) {
			$c = get_comment( $comment_id );
			if ( $c && '1' === (string) $c->comment_approved ) {
				self::comment_on( (int) $c->comment_post_ID );
			}
		} );
	}

	// ---------- Term ----------

	public static function edited_term( $term_id, $tt_id, $taxonomy ) {
		self::safe( function () use ( $term_id, $taxonomy ) {
			self::add( 't' . $taxonomy . ':' . $term_id, array( 'type' => 'term', 'tax' => $taxonomy, 'id' => (int) $term_id ) );
		} );
	}

	public static function pre_delete_term( $term_id, $taxonomy ) {
		self::safe( function () use ( $term_id, $taxonomy ) {
			$link = get_term_link( (int) $term_id, $taxonomy );
			self::add( 't' . $taxonomy . ':' . $term_id, array(
				'type'    => 'term',
				'tax'     => $taxonomy,
				'id'      => (int) $term_id,
				'old_url' => is_wp_error( $link ) ? '' : $link,
			) );
		} );
	}

	// ---------- Gửi ----------

	public static function flush() {
		self::safe( function () {
			$events     = array_values( self::$ev );
			self::$ev   = array();
			self::$old  = array();
			if ( ! $events ) {
				return;
			}
			$hook = self::hook();
			if ( ! $hook ) {
				return;
			}
			$body = wp_json_encode( array(
				'host'   => strtolower( (string) wp_parse_url( network_home_url( '/' ), PHP_URL_HOST ) ),
				'events' => $events,
			), JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE );
			$r = wp_remote_post( $hook['url'], array(
				// Không chờ trả lời. Tunnel tắt: bị từ chối ngay. Bên kia nhận mà không trả lời: tối đa 1 s.
				'blocking' => false,
				'timeout'  => 1,
				'headers'  => array(
					'Content-Type'  => 'application/json',
					'Authorization' => 'Bearer ' . $hook['token'],
				),
				'body'     => $body,
			) );
			if ( is_wp_error( $r ) ) {
				error_log( 'dott-cache-events: POST lỗi (' . $r->get_error_message() . '), mất ' . count( $events ) . ' sự kiện' );
			}
		} );
	}

	/** {"url","token"} từ <thư mục site>/dott-cache-hook.json; null nếu chưa bật. */
	private static function hook() {
		$f = defined( 'DOTT_CACHE_HOOK_FILE' ) ? DOTT_CACHE_HOOK_FILE : dirname( rtrim( ABSPATH, '/' ) ) . '/dott-cache-hook.json';
		if ( ! is_readable( $f ) ) {
			return null;
		}
		$h = json_decode( (string) file_get_contents( $f ), true );
		if ( empty( $h['url'] ) || empty( $h['token'] ) || ! preg_match( '#^http://127\.0\.0\.1:\d+/#', $h['url'] ) ) {
			error_log( 'dott-cache-events: ' . $f . ' thiếu url/token, hoặc url không phải http://127.0.0.1:<port>/' );
			return null;
		}
		return $h;
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
