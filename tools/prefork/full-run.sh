#!/bin/bash
# Prefork toàn bộ URL của php81 /tttest/ (xóa cache trước), đo thời gian + tải VPS chính.
# Dùng: bash full-run.sh <parallel>   (chạy bằng root trên VPS chính, chỉ đụng php81)
set -uo pipefail
P=${1:-1}
Q=/home/php81conZSI/php81.congtoan.net/public_html
PF=/home/php81conZSI/prefork
W="runuser -u php81conZSI -- wp --path=$Q --url=https://php81.congtoan.net/tttest/ --no-color"
{ echo /tttest/; $W post list --post_type=post --posts_per_page=-1 --field=url 2>/dev/null; $W term list category --field=url 2>/dev/null; } \
  | sed 's#https://php81.congtoan.net##' > $PF/all.txt
$W eval 'rocket_clean_domain();' 2>/dev/null
echo "URL: $(wc -l < $PF/all.txt), file cache còn lại sau khi xóa: $(find $Q/wp-content/cache/wp-rocket/php81.congtoan.net/tttest -name 'index*.html' 2>/dev/null | wc -l)"
( while true; do cut -d' ' -f1 /proc/loadavg; sleep 5; done ) > /tmp/pf-load.txt &
S=$!
/usr/bin/time -f "tiến trình master: %e s, RAM tối đa %M KB, CPU %P" \
  runuser -u php81conZSI -- php $PF/prefork-render.php --root=$Q --host=php81.congtoan.net --urls=$PF/all.txt \
    --parallel=$P --readonly --out=$PF/all-p$P.jsonl 2>&1 | grep -v 'wpdb must set' | grep -v '^\s*$'
kill $S
echo "load 1 phút trong lúc chạy: max $(sort -n /tmp/pf-load.txt | tail -1), trung bình $(awk '{s+=$1} END {printf "%.2f", s/NR}' /tmp/pf-load.txt) ($(wc -l < /tmp/pf-load.txt) mẫu)"
rm -f /tmp/pf-load.txt
echo "file cache sau khi chạy: $(find $Q/wp-content/cache/wp-rocket/php81.congtoan.net/tttest -name 'index-https.html' | wc -l) html, $(find $Q/wp-content/cache/wp-rocket/php81.congtoan.net/tttest -name 'index-https.html_gzip' | wc -l) gzip, mobile: $(find $Q/wp-content/cache/wp-rocket/php81.congtoan.net/tttest -name 'index-mobile*' | wc -l)"
echo "lỗi: $(grep -c '"ok":false' $PF/all-p$P.jsonl)"; grep '"ok":false' $PF/all-p$P.jsonl | head -5
