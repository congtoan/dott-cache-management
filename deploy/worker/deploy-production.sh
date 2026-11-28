#!/bin/bash
# Deploy Worker v2 lên production (script thongthai-cache, route diaocthongthai.com/* giữ nguyên), hoặc quay lại v7.
# Chạy trên VPS phụ. Không in token.
#   bash deploy-production.sh backup     tải mã v7 đang chạy về test/p7/v7-live/worker.js (làm một lần, trước deploy)
#   bash deploy-production.sh deploy     upload v2 (/tmp/dott-cache-worker.js) với binding production
#   bash deploy-production.sh rollback   upload lại v7 đã tải (binding THONGTHAI_CACHE như cũ)
set -euo pipefail
H=/root/tool/cache-system
B=$H/test/p7/v7-live
set -a; . $H/config/secrets/cf.env; . $H/config/secrets/worker-test.env; set +a
HDR=$(mktemp); chmod 600 $HDR; echo "Authorization: Bearer $CF_API_TOKEN" > $HDR
trap 'rm -f $HDR' EXIT
API=https://api.cloudflare.com/client/v4/accounts/$CF_ACCOUNT_ID/workers/scripts/thongthai-cache
NS=cc831ca12e954d0b8a875a3453abacc7
BUCKET=https://us-ord-1.linodeobjects.com/cdn.diaocthongthai.com/_cache_html
ok() { python3 -c "import json,sys;d=json.load(sys.stdin);print('success', d['success'], d.get('errors'))"; }

case "${1:-}" in
backup)
  mkdir -p $B && chmod 700 $B
  curl -s "$API/content/v2" -H @$HDR -o $B/content.bin -D $B/headers.txt
  python3 - $B <<'EOF'
import sys, email
d = sys.argv[1]
hdr = open(d + '/headers.txt').read()
ct = [l.split(':', 1)[1].strip() for l in hdr.splitlines() if l.lower().startswith('content-type')][0]
msg = email.message_from_bytes(b'Content-Type: ' + ct.encode() + b'\r\n\r\n' + open(d + '/content.bin', 'rb').read())
for p in msg.walk():
    if p.get_filename():
        open(d + '/' + p.get_filename(), 'wb').write(p.get_payload(decode=True))
        print('saved', p.get_filename())
EOF
  ls -la $B ;;
deploy)
  [ -s $B/worker.js ] || { echo "chưa có bản v7 để quay lại: chạy backup trước"; exit 1; }
  W=$H/test/p7/v2; mkdir -p $W && chmod 700 $W
  sed 's/\r$//' /tmp/dott-cache-worker.js > $W/worker.js
  python3 - "$NS" "$BUCKET" "$TEST_KEY" > $W/metadata.json <<'EOF'
import json, sys
ns, bucket, key = sys.argv[1:4]
print(json.dumps({
  "main_module": "worker.js",
  "compatibility_date": "2026-11-01",
  "bindings": [
    {"type": "kv_namespace", "name": "CACHE", "namespace_id": ns},
    {"type": "plain_text", "name": "REDIRECTS", "text": "1"},
    {"type": "plain_text", "name": "LEGACY_PREFIXES", "text": json.dumps(["/en/"])},
    {"type": "plain_text", "name": "TARGETS", "text": json.dumps({"NA": bucket, "SA": bucket})},
    {"type": "secret_text", "name": "TEST_KEY", "text": key},
  ]}))
EOF
  chmod 600 $W/metadata.json
  curl -s -X PUT "$API" -H @$HDR -F "metadata=@$W/metadata.json;type=application/json" \
    -F "worker.js=@$W/worker.js;type=application/javascript+module" | ok
  rm -f $W/metadata.json ;;
rollback)
  M=$(mktemp); chmod 600 $M
  printf '%s' '{"main_module":"worker.js","compatibility_date":"2025-09-06","bindings":[{"type":"kv_namespace","name":"THONGTHAI_CACHE","namespace_id":"'$NS'"}]}' > $M
  curl -s -X PUT "$API" -H @$HDR -F "metadata=@$M;type=application/json" \
    -F "worker.js=@$B/worker.js;type=application/javascript+module" | ok
  rm -f $M ;;
*) echo "dùng: $0 backup|deploy|rollback"; exit 2 ;;
esac
