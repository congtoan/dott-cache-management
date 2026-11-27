#!/bin/bash
# renew.sh: tạo lại cache WP Rocket cho danh sách path, bằng prefork. Chạy trên VPS chính (root).
# Bản thử (28/11): CHỈ cho php81.congtoan.net (site test).
#
# VPS phụ gọi:  printf '/tttest/abc/\n' | ssh main 'bash /home/php81conZSI/prefork/renew.sh'
#
# stdin: mỗi dòng một path (/tttest/abc/) hoặc URL đầy đủ của php81.
# Các bước:
#   1. lọc path hợp lệ;
#   2. xóa file cache cũ (index*-https.html và _gzip) của đúng thư mục path, không xóa thư mục con;
#   3. chạy prefork-render.php cho từng blog (prefork chỉ nạp một blog mỗi lần).
# stdout:
#   - dòng "R {json}": kết quả từng path;
#   - dòng "# ...": log của người đọc;
#   - dòng cuối "DONE ok=N fail=M skip=K".
# Có flock: mỗi lúc chỉ một lô chạy. Chạy với nice và ionice để nhường tài nguyên cho khách.
set -uo pipefail
HOST=php81.congtoan.net
USR=php81conZSI
Q=/home/$USR/php81.congtoan.net/public_html
PF=/home/$USR/prefork
CACHE=$Q/wp-content/cache/wp-rocket/$HOST

exec 9>/tmp/dott-renew.lock
flock -w 900 9 || { echo "# đang có lô khác chạy quá 15 phút, bỏ"; echo "DONE ok=0 fail=0 skip=0"; exit 3; }

TMP=$(mktemp -d /tmp/dott-renew.XXXXXX)
trap 'rm -rf "$TMP"' EXIT
chown $USR "$TMP"

skip=0
: > "$TMP/all.txt"
while IFS= read -r l || [ -n "$l" ]; do
  l=$(printf '%s' "$l" | tr -d '\r' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
  [ -z "$l" ] && continue
  case "$l" in \#*) continue ;; esac
  case "$l" in
    https://$HOST/*) l=${l#https://$HOST} ;;
    http*://*) echo "R {\"path\":\"$l\",\"ok\":false,\"err\":\"không phải host $HOST\"}"; skip=$((skip+1)); continue ;;
  esac
  case "$l" in */) ;; *) l="$l/" ;; esac
  # Chỉ ký tự an toàn, không có "..", không có "?" (cache không lưu query string).
  if ! printf '%s' "$l" | grep -Eq '^/[A-Za-z0-9/_.%~-]*$' || printf '%s' "$l" | grep -Eq '\.\.|//'; then
    echo "R {\"path\":\"$l\",\"ok\":false,\"err\":\"path không hợp lệ\"}"; skip=$((skip+1)); continue
  fi
  echo "$l" >> "$TMP/all.txt"
done
sort -u -o "$TMP/all.txt" "$TMP/all.txt"
n=$(wc -l < "$TMP/all.txt")
echo "# $n path hợp lệ, $skip bị bỏ"
[ "$n" -eq 0 ] && { echo "DONE ok=0 fail=0 skip=$skip"; exit 0; }

# 2. Xóa file cache cũ: chỉ file trong đúng thư mục path, không đụng thư mục con.
del=0
while IFS= read -r p; do
  d="$CACHE$p"
  for f in "$d"index-https.html "$d"index-https.html_gzip "$d"index-mobile-https.html "$d"index-mobile-https.html_gzip; do
    [ -f "$f" ] && rm -f "$f" && del=$((del+1))
  done
done < "$TMP/all.txt"
echo "# đã xóa $del file cache cũ"

# 3. Chia path theo blog: path thuộc blog có tiền tố (path site) dài nhất khớp với nó.
#    Danh sách blog lấy từ WordPress (vd "/", "/en/", "/tttest/", "/tttest-en/").
#    Mỗi blog bootstrap một lần.
runuser -u $USR -- wp --path=$Q --url=https://$HOST/ --skip-plugins --skip-themes site list --field=path 2>/dev/null \
  | awk '{print length($0) " " $0}' | sort -rn | cut -d' ' -f2 > "$TMP/sites.txt"
awk 'NR==FNR { s[++n]=$0; next } { for (i=1;i<=n;i++) if (index($0, s[i])==1) { print s[i] "\t" $0; next } }' \
  "$TMP/sites.txt" "$TMP/all.txt" > "$TMP/grouped.tsv"
ok=0; fail=0; gi=0
for site in $(cut -f1 "$TMP/grouped.tsv" | sort -u); do
  gi=$((gi+1))
  awk -F'\t' -v s="$site" '$1==s {print $2}' "$TMP/grouped.tsv" > "$TMP/g$gi.txt"
  chown $USR "$TMP/g$gi.txt"
  t0=$(date +%s.%N)
  # prefork lấy blog theo đoạn đầu của path ĐẦU TIÊN; với blog "/" thì path bất kỳ của blog gốc
  # cũng ra blog 1, nên thứ tự trong file không quan trọng.
  nice -n 10 ionice -c2 -n7 runuser -u $USR -- php "$PF/prefork-render.php" --root=$Q --host=$HOST \
      --urls="$TMP/g$gi.txt" --parallel=1 --readonly --out="$TMP/g$gi.jsonl" 2>&1 \
    | grep -v 'wpdb must set' | grep -v '^\s*$' | sed 's/^/# /'
  echo "# blog $site: $(wc -l < "$TMP/g$gi.txt") path, $(awk -v a="$t0" -v b="$(date +%s.%N)" 'BEGIN{printf "%.1f", b-a}') s"
  if [ -f "$TMP/g$gi.jsonl" ]; then
    while IFS= read -r j; do
      echo "R $j"
      case "$j" in *'"ok":true'*) ok=$((ok+1)) ;; *) fail=$((fail+1)) ;; esac
    done < "$TMP/g$gi.jsonl"
  fi
done
# Path không có kết quả (không thuộc blog nào, hoặc prefork dừng giữa chừng): báo lỗi, không bỏ im lặng.
cat "$TMP"/g*.jsonl 2>/dev/null | grep -o '"path":"[^"]*"' | cut -d'"' -f4 | sort -u > "$TMP/reported.txt"
while IFS= read -r p; do
  echo "R {\"path\":\"$p\",\"ok\":false,\"err\":\"prefork không trả kết quả\"}"; fail=$((fail+1))
done < <(comm -23 "$TMP/all.txt" "$TMP/reported.txt")
echo "DONE ok=$ok fail=$fail skip=$skip"
