O=https://us-ord-1.linodeobjects.com/cdn.diaocthongthai.com/_cache_html/en/about-us/index-https.html
echo "== from $(hostname): GET 1 object S3 Chicago x3 (connect / tls / ttfb / total)"
for i in 1 2 3; do curl -s -o /dev/null -w '%{http_code} conn=%{time_connect} tls=%{time_appconnect} ttfb=%{time_starttransfer} total=%{time_total}\n' "$O"; done
echo "== 4 GETs reusing one connection (like a kept-alive client):"
curl -s -o /dev/null -o /dev/null -o /dev/null -o /dev/null -w 'total=%{time_total}\n' "$O" "$O" "$O" "$O" | tail -1
