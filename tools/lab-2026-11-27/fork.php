<?php
// Prefork spike (read-only): bootstrap WP once, fork one child per URL, child renders and saves HTML to /tmp.
// Usage: php fork.php <blogprefix: "" or "/en"> <desktop|mobile> <path1> <path2> ...
$djt_args   = array_slice($argv, 1);
$djt_prefix = array_shift($djt_args);
$djt_mobile = array_shift($djt_args) === 'mobile';
$djt_urls   = $djt_args;
$djt_ua = $djt_mobile
  ? 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148 Safari/604.1'
  : 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/130 Safari/537.36';
function djt_server($uri, $ua) {
  $_SERVER['HTTP_HOST'] = $_SERVER['SERVER_NAME'] = 'diaocthongthai.com';
  $_SERVER['HTTPS'] = 'on'; $_SERVER['SERVER_PORT'] = 443; $_SERVER['REQUEST_METHOD'] = 'GET';
  $_SERVER['REQUEST_URI'] = $uri; $_SERVER['QUERY_STRING'] = (string) parse_url($uri, PHP_URL_QUERY);
  $_SERVER['REMOTE_ADDR'] = '127.0.0.1'; $_SERVER['HTTP_USER_AGENT'] = $ua; $_SERVER['HTTP_ACCEPT'] = 'text/html';
  $_GET = []; parse_str($_SERVER['QUERY_STRING'], $_GET); $_REQUEST = $_GET; $_POST = $_COOKIE = [];
}
djt_server($djt_prefix . '/?djtboot=1', $djt_ua);
define('DONOTCACHEPAGE', true);
define('SAVEQUERIES', true);
define('WP_USE_THEMES', true);
$t0 = microtime(true);
ob_start();
require '/home/diaocthDP4f/diaocthongthai.com/public_html/wp-load.php';
ob_end_clean();
$boot_ms = (microtime(true) - $t0) * 1000;
fwrite(STDOUT, sprintf("master boot (cold compile in CLI) = %d ms, mem=%d MB\n", $boot_ms, memory_get_usage(true) / 1048576));
global $wpdb;
$wpdb->close();
@mkdir('/tmp/djt-fork', 0777, true);

foreach ($djt_urls as $i => $u) {
  $pid = pcntl_fork();
  if ($pid === 0) {
    $c0 = microtime(true);
    $wpdb->db_connect();
    $wpdb->query('SET SESSION TRANSACTION READ ONLY');
    wp_cache_init();                                   // fresh Redis connection for the child
    $nq0 = count($wpdb->queries);
    djt_server($u . '?djtprobe=1', $djt_ua);
    ob_start();
    wp();
    $c1 = microtime(true);
    require ABSPATH . WPINC . '/template-loader.php';
    $html = '';
    while (ob_get_level() > 0) { $html = ob_get_clean() . $html; }
    $c2 = microtime(true);
    $db = 0; $n = 0; foreach (array_slice($wpdb->queries, $nq0) as $q) { $db += $q[1]; $n++; }
    file_put_contents('/tmp/djt-fork/' . md5($u . ($djt_mobile ? 'm' : 'd')) . '.html', $html);
    fwrite(STDOUT, sprintf("  child %-50s %s total=%4dms (reconnect+query=%3dms, template=%4dms) db=%3dms/%3dq html=%dKB 404=%d\n",
      substr($u, 0, 50), $djt_mobile ? 'm' : 'd', ($c2 - $c0) * 1000, ($c1 - $c0) * 1000, ($c2 - $c1) * 1000,
      $db * 1000, $n, strlen($html) / 1024, is_404() ? 1 : 0));
    posix_kill(getmypid(), 9);                          // skip shutdown hooks
  }
  pcntl_waitpid($pid, $st);
}
