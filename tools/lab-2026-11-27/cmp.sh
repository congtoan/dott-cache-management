cd /tmp/djt-fork || exit 1
UA_D="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/130 Safari/537.36"
UA_M="Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148 Safari/604.1"
norm() { sed -E 's/(nonce|_wpnonce|"nonce")([^0-9a-f]{1,6})[0-9a-f]{10}/\1\2NONCE/g; s/ver=[0-9]{10}/ver=TS/g; s/[0-9]{10}\.[0-9]+/TIME/g' "$1"; }
for spec in "/:d" "/ban-tin-du-an/:d" "/ban-tin-du-an/page/5/:d" "/13-dieu-kien-cho-nguoi-mua-nha-o-xa-hoi/:d" "/3171-can-ho-glory-heights-da-hoan-thien-phap-ly-du-dieu-kien-dua-vao-kinh-doanh/:d" "/en/:d" "/en/about-us/:d" "/en/afghanistan-maps/:d" "/en/maps-of-all-philippine-provinces/page/2/:d" "/en/map-of-ho-chi-minh-city/:d" "/en/map-of-ho-chi-minh-city/:m" "/en/about-us/:m"; do
  u=${spec%:*}; v=${spec##*:}; ua=$UA_D; [ $v = m ] && ua=$UA_M
  f=$(printf '%s' "$u$v" | md5sum | cut -c1-32).html
  curl -sk -A "$ua" -H "Host: diaocthongthai.com" "https://127.0.0.1${u}?djtprobe=1" -o /tmp/djt-http.html
  raw=$(cmp -s $f /tmp/djt-http.html && echo SAME || echo diff)
  n=$(diff <(norm $f) <(norm /tmp/djt-http.html) | grep -c '^[<>]')
  printf "%-62s %s raw=%-4s changed-lines-after-normalize=%s\n" "${u:0:62}" $v $raw $n
  [ "$n" -gt 0 ] && diff <(norm $f) <(norm /tmp/djt-http.html) | grep '^[<>]' | cut -c1-220 | head -4
done
