#!/usr/bin/env python3
"""
cf_kv_api_test.py - Kiem tra Cloudflare KV REST API bang API token (khong can wrangler).
Chay duoc tren PC (py3.10) va VPS (py3.8), chi can `requests`.

  python cf_kv_api_test.py <duong_dan_secrets.json> [--bulk 1000]

Chi ghi/xoa cac key test co prefix /__claude_api_test__/ ; key that chi DOC.
Khong in token ra man hinh.
"""
import json, sys, time
from urllib.parse import quote
import requests

PREFIX = "/__claude_api_test__/"
API = "https://api.cloudflare.com/client/v4"


def main():
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    cfg = json.load(open(sys.argv[1], encoding="utf-8"))["cloudflare"]
    n_bulk = int(sys.argv[sys.argv.index("--bulk") + 1]) if "--bulk" in sys.argv else 1000
    tok, acc, ns = cfg["api_token"].strip(), cfg["account_id"], cfg["kv_namespace_id"]
    if not tok:
        sys.exit("secrets.json: cloudflare.api_token dang trong")
    s = requests.Session()
    s.headers["Authorization"] = f"Bearer {tok}"
    base = f"{API}/accounts/{acc}/storage/kv/namespaces/{ns}"
    k = lambda key: quote(key, safe="")
    ok_all = True

    def step(name, fn):
        nonlocal ok_all
        t = time.time()
        try:
            msg = fn()
            print(f"[OK ] {name:<42} {time.time()-t:5.2f}s  {msg or ''}")
        except Exception as e:  # noqa
            ok_all = False
            print(f"[ERR] {name:<42} {time.time()-t:5.2f}s  {e}")

    def chk(r):
        if r.status_code >= 400:
            raise RuntimeError(f"HTTP {r.status_code}: {r.text[:300]}")
        return r

    # 1. Token hop le? (account token va user token co endpoint verify khac nhau)
    def verify():
        for url in (f"{API}/accounts/{acc}/tokens/verify", f"{API}/user/tokens/verify"):
            r = s.get(url, timeout=20)
            if r.ok and r.json().get("success"):
                return f"status={r.json()['result'].get('status')} ({url.split('/v4/')[1]})"
        raise RuntimeError(r.text[:300])
    step("1. verify token", verify)

    # 2. Doc key that (chi doc)
    def get_val(key):
        return chk(s.get(base + "/values/" + k(key), timeout=20)).text

    step("2. GET key that '/'", lambda: "value=%r" % get_val("/")[:10])

    # 3. Ghi / doc / xoa 1 key test
    key1 = PREFIX + "single/"
    step("3a. PUT 1 key test", lambda: chk(s.put(f"{base}/values/{k(key1)}", data="1", timeout=20)) and None)
    step("3b. GET lai key test", lambda: "value=%r" % get_val(key1))
    step("3c. DELETE key test", lambda: chk(s.delete(f"{base}/values/{k(key1)}", timeout=20)) and None)

    # 4. Bulk ghi n key test (1 request <= 10.000 key)
    keys = [f"{PREFIX}bulk-{i:05d}/" for i in range(n_bulk)]
    step(f"4a. bulk PUT {n_bulk} key", lambda: chk(s.put(f"{base}/bulk", json=[{"key": x, "value": "1"} for x in keys], timeout=120)) and None)

    def count():
        n, cursor = 0, None
        while True:
            p = {"prefix": PREFIX, "limit": 1000}
            if cursor:
                p["cursor"] = cursor
            d = chk(s.get(f"{base}/keys", params=p, timeout=60)).json()
            n += len(d["result"])
            cursor = d.get("result_info", {}).get("cursor")
            if not cursor:
                return n
    step("4b. list prefix test (dem key)", lambda: f"{count()} key")

    def bulk_delete():
        r = s.post(f"{base}/bulk/delete", json=keys, timeout=120)
        if r.status_code == 404:  # API cu
            r = s.delete(f"{base}/bulk", json=keys, timeout=120)
        chk(r)
    step(f"4c. bulk DELETE {n_bulk} key", bulk_delete)
    step("4d. list prefix test sau xoa", lambda: f"{count()} key (mong doi 0)")

    print("\nKET QUA:", "TAT CA OK" if ok_all else "CO LOI - xem dong [ERR]")
    sys.exit(0 if ok_all else 1)


if __name__ == "__main__":
    main()
