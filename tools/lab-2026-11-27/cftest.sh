B=https://djt-compress-test.congtoan-real.workers.dev
sleep 5
echo "S3 headers of .html object:"; curl -s $B/s3hdr; echo
for p in plain gz; do
  for ae in "gzip, deflate, br" "gzip" "identity"; do
    curl -s -o /tmp/cf.bin -D /tmp/cf.h -H "Accept-Encoding: $ae" -w "%{http_code} size=%{size_download} t=%{time_total}" "$B/$p"
    printf "  /%-5s AE=%-18s -> %s  content-encoding=%s\n" $p "$ae" "" "$(grep -i '^content-encoding' /tmp/cf.h | tr -d '\r' | awk '{print $2}')"
  done
done
echo "check decoded body ok (gz path, AE=gzip):"; curl -s --compressed -H 'Accept-Encoding: gzip' $B/gz | tail -c 120; echo
echo "check decoded body ok (plain path, AE=br):"; curl -s --compressed -H 'Accept-Encoding: br' $B/plain | tail -c 120; echo
