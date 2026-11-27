#!/bin/bash
# So file cache do prefork ghi với file cache do HTTP render ghi (php81 /tttest/), 20 URL.
# Chạy trên VPS chính bằng root. Chỉ đụng php81.
set -euo pipefail
Q=/home/php81conZSI/php81.congtoan.net/public_html
C=$Q/wp-content/cache/wp-rocket/php81.congtoan.net
PF=/home/php81conZSI/prefork
W="runuser -u php81conZSI -- wp --path=$Q --url=https://php81.congtoan.net/tttest/ --no-color"
D="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/131 Safari/537.36"
N=${1:-20}

{ echo /tttest/; $W post list --post_type=post --posts_per_page=$((N-2)) --orderby=rand --field=url 2>/dev/null; $W term list category --number=1 --field=url 2>/dev/null; } \
  | sed 's#https://php81.congtoan.net##' > $PF/sample.txt
rm -rf /tmp/pf-http && mkdir -p /tmp/pf-http
# 1) HTTP render: xóa cache các URL mẫu, gọi 1 lần (render + ghi), lưu bản sao file cache
while read p; do rm -rf "$C$p"index-https.html* ; done < $PF/sample.txt
while read p; do curl -sk --resolve php81.congtoan.net:443:127.0.0.1 -A "$D" -o /dev/null "https://php81.congtoan.net$p"; \
  cp "$C${p}index-https.html" "/tmp/pf-http/$(echo $p | md5sum | cut -c1-12).html"; done < $PF/sample.txt
# 2) Xóa lại, prefork render
while read p; do rm -rf "$C$p"index-https.html* ; done < $PF/sample.txt
runuser -u php81conZSI -- php $PF/prefork-render.php --root=$Q --host=php81.congtoan.net --urls=$PF/sample.txt --parallel=1 --readonly --out=$PF/sample.jsonl
# 3) So sánh (bỏ qua dòng cached@ / honeypot / nonce LCP)
norm() { sed -E 's/cached@[0-9]+/cached@X/; s/wpa_field_info = JSON.parse\(atob\("[^"]*"\)\)/wpa_field_info=X/; s/"nonce":"[0-9a-f]+"/"nonce":"X"/g; s/data-token-time="[0-9]+"/data-token-time="X"/g' "$1"; }
same=0; diffc=0
while read p; do
  h="/tmp/pf-http/$(echo $p | md5sum | cut -c1-12).html"; f="$C${p}index-https.html"
  if [ ! -f "$f" ]; then echo "THIẾU file prefork: $p"; diffc=$((diffc+1)); continue; fi
  d=$(diff <(norm "$h") <(norm "$f") | grep -c '^[<>]' || true)
  if [ "$d" = "0" ]; then same=$((same+1)); else diffc=$((diffc+1)); echo "LỆCH $d dòng: $p"; { diff <(norm "$h") <(norm "$f") | grep '^[<>]' | cut -c1-160 | head -6; } || true; fi
  [ -f "${f}_gzip" ] || echo "thiếu _gzip: $p"
done < $PF/sample.txt
echo "giống hệt (sau chuẩn hóa): $same / $(wc -l < $PF/sample.txt), lệch: $diffc"
tail -c 120 "$C/tttest/index-https.html"; echo
