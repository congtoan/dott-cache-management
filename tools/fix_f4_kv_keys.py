#!/usr/bin/env python3
"""
fix_f4_kv_keys.py - Sua loi F4: key KV dang URL day du -> dang path (Worker tra theo path).

  python3 fix_f4_kv_keys.py <secrets.json>            # dry-run: chi liet ke, khong ghi
  python3 fix_f4_kv_keys.py <secrets.json> --apply    # ghi key path, xac minh, roi xoa key URL day du

Quy trinh (an toan: GHI truoc, XOA sau, chi xoa khi key path tuong ung da co):
  1. List toan bo key co prefix "https://" (key sai).
  2. Doi moi key sang path chuan hoa giong Worker (normalizePath: lowercase + "/" cuoi).
  3. Bulk PUT cac key path (value "1"), toi da 10.000 key/request.
  4. List lai toan bo KV, xac nhan moi key path da ton tai.
  5. Bulk DELETE key URL day du (chi nhung key co key path da xac nhan), kiem unsuccessful_keys, thu lai.
  6. List lai prefix "https://" -> mong doi 0.
Chay duoc tren py3.8, chi can requests.
"""
import json, sys, time
from urllib.parse import urlparse
import requests

API = "https://api.cloudflare.com/client/v4"
BATCH = 10000


def normalize_path(url_or_path):
    """Giong normalizePath() trong thongthai-cache-v7.js."""
    p = urlparse(url_or_path).path if url_or_path.startswith("http") else url_or_path
    if p == "/" or p == "":
        return "/"
    p = p.lower()
    if "." not in p and not p.endswith("/"):
        p += "/"
    return p


class KV:
    def __init__(self, cfg):
        self.s = requests.Session()
        self.s.headers["Authorization"] = "Bearer " + cfg["api_token"].strip()
        self.base = f"{API}/accounts/{cfg['account_id']}/storage/kv/namespaces/{cfg['kv_namespace_id']}"

    def _chk(self, r):
        if r.status_code >= 400:
            raise RuntimeError(f"HTTP {r.status_code}: {r.text[:300]}")
        d = r.json()
        if not d.get("success", True):
            raise RuntimeError(str(d.get("errors")))
        return d

    def list_keys(self, prefix=None):
        out, cursor = [], None
        while True:
            p = {"limit": 1000}
            if prefix:
                p["prefix"] = prefix
            if cursor:
                p["cursor"] = cursor
            d = self._chk(self.s.get(self.base + "/keys", params=p, timeout=60))
            out += [k["name"] for k in d["result"]]
            cursor = (d.get("result_info") or {}).get("cursor")
            if not cursor:
                return out

    def bulk_put(self, keys):
        for i in range(0, len(keys), BATCH):
            chunk = keys[i:i + BATCH]
            d = self._chk(self.s.put(self.base + "/bulk", json=[{"key": k, "value": "1"} for k in chunk], timeout=180))
            bad = (d.get("result") or {}).get("unsuccessful_keys") or []
            print(f"   bulk PUT {len(chunk)} key -> loi: {len(bad)}")
            if bad:
                raise RuntimeError(f"PUT that bai {len(bad)} key, vd: {bad[:3]}")

    def bulk_delete(self, keys):
        failed = []
        for i in range(0, len(keys), BATCH):
            chunk = keys[i:i + BATCH]
            r = self.s.post(self.base + "/bulk/delete", json=chunk, timeout=180)
            d = self._chk(r)
            bad = (d.get("result") or {}).get("unsuccessful_keys") or []
            print(f"   bulk DELETE {len(chunk)} key -> API bao loi: {len(bad)}")
            failed += bad
        return failed


def main():
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    apply = "--apply" in sys.argv
    kv = KV(json.load(open(sys.argv[1], encoding="utf-8"))["cloudflare"])

    t = time.time()
    all_keys = kv.list_keys()
    kset = set(all_keys)
    wrong = [k for k in all_keys if k.startswith("http")]
    print(f"[1] KV co {len(all_keys):,} key; key sai (URL day du): {len(wrong):,}  ({time.time()-t:.1f}s)")
    if not wrong:
        print("Khong con key sai. Xong.")
        return

    mapping = {w: normalize_path(w) for w in wrong}
    hosts = {urlparse(w).netloc for w in wrong}
    print(f"[2] Host trong key sai: {hosts}")
    need_put = sorted({p for p in mapping.values() if p not in kset})
    print(f"    Key path can them: {len(need_put):,} (da co san: {len(set(mapping.values())) - len(need_put):,})")
    for w in wrong[:5]:
        print(f"      {w}\n        -> {mapping[w]}  {'(da co)' if mapping[w] in kset else '(se them)'}")
    if not apply:
        print("\nDRY-RUN: khong ghi gi. Chay lai voi --apply de sua.")
        return

    print(f"[3] Ghi {len(need_put):,} key path ...")
    if need_put:
        kv.bulk_put(need_put)

    print("[4] List lai de xac nhan key path ...")
    time.sleep(5)
    kset = set(kv.list_keys())
    missing = [p for p in set(mapping.values()) if p not in kset]
    print(f"    Key path con thieu: {len(missing)}")
    if missing:
        sys.exit(f"DUNG: con {len(missing)} key path chua thay, KHONG xoa key cu. vd {missing[:3]}")

    to_delete = [w for w in wrong if mapping[w] in kset]
    print(f"[5] Xoa {len(to_delete):,} key URL day du ...")
    # Bai hoc 2026-11-25: lan xoa dau sot 99/3865 key du API bao 0 loi, va list theo prefix "https://"
    # KHONG thay 99 key do -> phai kiem bang list TOAN BO, cho ~60s giua cac lan.
    for attempt in range(1, 5):
        kv.bulk_delete(to_delete)
        time.sleep(60)
        left = [k for k in kv.list_keys() if k.startswith("http")]
        print(f"    Lan {attempt}: con {len(left)} key URL day du (list toan bo)")
        if not left:
            break
        to_delete = left
    print(f"\n[6] KET QUA: con {len(left)} key sai; tong key hien tai {len(kv.list_keys()):,}")


if __name__ == "__main__":
    main()
