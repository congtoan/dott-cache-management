#!/bin/bash
# Deploy Worker test lên workers.dev (VPS phụ). Không in token.
set -uo pipefail
H=/root/tool/cache-system
W=$H/test/worker
mkdir -p $W && chmod 700 $W
sed -i 's/\r$//' /tmp/dott-cache-worker.js && mv /tmp/dott-cache-worker.js $W/worker.js
set -a; . $H/config/secrets/cf.env; set +a
TE=$H/config/secrets/worker-test.env
[ -f $TE ] || { umask 077; echo "TEST_KEY=$(openssl rand -hex 20)" > $TE; }
set -a; . $TE; set +a
NS=$(python3 -c "import json;print(json.load(open('$H/config/sites/php81.congtoan.net.json'))['kv']['namespace_id'])")
HDR=$(mktemp); chmod 600 $HDR; echo "Authorization: Bearer $CF_API_TOKEN" > $HDR
API=https://api.cloudflare.com/client/v4/accounts/$CF_ACCOUNT_ID
NAME=dott-cache-php81-test
python3 - "$NS" "$TEST_KEY" > $W/metadata.json <<'EOF'
import json, sys
print(json.dumps({
  "main_module": "worker.js",
  "compatibility_date": "2026-11-01",
  "bindings": [
    {"type": "kv_namespace", "name": "CACHE", "namespace_id": sys.argv[1]},
    {"type": "plain_text", "name": "ORIGIN", "text": "https://php81.congtoan.net"},
    {"type": "plain_text", "name": "TARGETS", "text": json.dumps({
        "NA": "https://us-ord-1.linodeobjects.com/congtoanphp/_cache_html",
        "SA": "https://us-ord-1.linodeobjects.com/congtoanphp/_cache_html",
        "EU": "https://eu-central-1.linodeobjects.com/congtoanphp/_cache_html"})},
    {"type": "secret_text", "name": "TEST_KEY", "text": sys.argv[2]},
  ]}))
EOF
chmod 600 $W/metadata.json
echo "--- upload script"
curl -s -X PUT "$API/workers/scripts/$NAME" -H @$HDR \
  -F "metadata=@$W/metadata.json;type=application/json" \
  -F "worker.js=@$W/worker.js;type=application/javascript+module" | python3 -c "import json,sys;d=json.load(sys.stdin);print('success', d['success'], d.get('errors'))"
rm -f $W/metadata.json
echo "--- bật workers.dev"
curl -s -X POST "$API/workers/scripts/$NAME/subdomain" -H @$HDR -H 'Content-Type: application/json' -d '{"enabled":true,"previews_enabled":false}' | python3 -c "import json,sys;d=json.load(sys.stdin);print('success', d['success'], d.get('errors'))"
SUB=$(curl -s "$API/workers/subdomain" -H @$HDR | python3 -c "import json,sys;print(json.load(sys.stdin)['result']['subdomain'])")
rm -f $HDR
echo "https://$NAME.$SUB.workers.dev" > $W/url.txt
echo "URL: $(cat $W/url.txt)"
