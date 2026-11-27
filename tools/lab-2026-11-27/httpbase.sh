#!/bin/bash
# Baseline: real PHP render time over HTTP, cache bypassed via unknown query string (WP Rocket won't cache it)
URLS="/ /en/ /ban-tin-du-an/ /ban-tin-du-an/page/5/ /13-dieu-kien-cho-nguoi-mua-nha-o-xa-hoi/
/3171-can-ho-glory-heights-da-hoan-thien-phap-ly-du-dieu-kien-dua-vao-kinh-doanh/
/en/about-us/ /en/afghanistan-maps/ /en/maps-of-all-philippine-provinces/page/2/ /en/map-of-ho-chi-minh-city/"
UA_D="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/130 Safari/537.36"
UA_M="Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148 Safari/604.1"
i=0
printf "%-58s %-22s %-22s\n" URL "desktop ttfb/total KB" "mobile ttfb/total KB"
for u in $URLS; do
  row=""
  for ua in "$UA_D" "$UA_M"; do
    i=$((i+1))
    r=$(curl -sk -o /dev/null -w '%{http_code} %{time_starttransfer} %{time_total} %{size_download}' -A "$ua" \
        --resolve diaocthongthai.com:443:45.77.32.177 "https://diaocthongthai.com${u}?djtprobe=$RANDOM$i")
    set -- $r; row="$row $(printf '%s %5.2fs/%5.2fs %4dKB' $1 $2 $3 $(( $4/1024 )))"
    sleep 1
  done
  printf "%-58s%s\n" "${u:0:58}" "$row"
done
