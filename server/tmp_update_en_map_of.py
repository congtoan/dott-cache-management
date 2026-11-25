#!/usr/bin/env python3
"""
tmp_update_en_map_of.py - TOOL TAM (2026-11-26): lam moi cache cho cac bai /en/map-of-*/ (khach chau My).

Moi URL:  xoa 4 file cache cu tren disk -> trigger desktop + mobile (curl 127.0.0.1, co timeout)
          -> kiem du 4 file moi -> upload S3 Chicago (utils.upload_files_to_s3) -> cap nhat sqlite.
Log moi URL: [da_xong/tong] trang thai, thoi gian, ETA. Ghi them ra log/tmp_update_en_map_of_<ngay>.log

Chay (tu thu muc cache-tools):
  screen -S en-map-of
  python3 -u tmp_update_en_map_of.py              # chay het
  python3 -u tmp_update_en_map_of.py --limit 1    # thu 1 URL
Dung an toan: Ctrl+C (xong URL hien tai roi thoat). Chay lai se TIEP TUC tu cho dung
(bo qua URL da lam moi tu luc bat dau dot nay - moc thoi gian luu trong .tmp_update_en_map_of.start).
"""
import argparse, os, signal, sqlite3, subprocess, sys, time
from datetime import datetime
from urllib.parse import urlparse

from utils import url_to_cache_paths, upload_files_to_s3, check_cpu_usage_and_wait, DOMAIN

HERE = os.path.dirname(os.path.abspath(__file__))
DB = os.path.join(HERE, "output", "cache_urls.db")
START_FILE = os.path.join(HERE, ".tmp_update_en_map_of.start")
LOG = os.path.join(HERE, "log", f"tmp_update_en_map_of_{datetime.now():%Y-%m-%d}.log")
PAT = "https://diaocthongthai.com/en/map-of-%/"
PAT_DEEP = "https://diaocthongthai.com/en/map-of-%/%/"   # loai /page/N/ va cap sau hon
MOBILE_UA = "Mozilla/5.0 (iPhone; CPU iPhone OS 10_3 like Mac OS X) AppleWebKit/602.1 Safari/602.1"

stop = False
def _stop(sig, frm):
    global stop
    stop = True
    log("!! Nhan tin hieu dung - se thoat sau URL hien tai")
signal.signal(signal.SIGINT, _stop)
signal.signal(signal.SIGTERM, _stop)

os.makedirs(os.path.dirname(LOG), exist_ok=True)
_logf = open(LOG, "a", encoding="utf-8")
def log(msg):
    line = f"{datetime.now():%Y-%m-%d %H:%M:%S} {msg}"
    print(line, flush=True)
    _logf.write(line + "\n"); _logf.flush()


def db():
    c = sqlite3.connect(DB, timeout=30)
    c.execute("PRAGMA journal_mode=WAL")
    c.execute("PRAGMA busy_timeout=30000")
    return c


def trigger(path, mobile):
    cmd = ["curl", "-k", "-s", "-o", "/dev/null", "--max-time", "60", "-w", "%{http_code}",
           "-H", f"Host: {DOMAIN}", f"https://127.0.0.1{path}"]
    if mobile:
        cmd[1:1] = ["-A", MOBILE_UA]
    try:
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=90)
        return r.stdout.strip() or "000"
    except subprocess.TimeoutExpired:
        return "timeout"


def process(url):
    files = url_to_cache_paths(url)          # [desktop, desktop_gzip, mobile, mobile_gzip]
    for f in files:
        if os.path.exists(f):
            os.remove(f)
    path = urlparse(url).path
    check_cpu_usage_and_wait()
    t0 = time.time()
    cd = trigger(path, mobile=False)
    cm = trigger(path, mobile=True)
    missing = [os.path.basename(f) for f in files if not (os.path.exists(f) and os.path.getmtime(f) >= t0 - 1)]
    if missing:
        return False, f"http d={cd} m={cm}, thieu file: {','.join(missing)} (KHONG upload)"
    res = upload_files_to_s3(files)
    bad = [r for r in res if not r.get("ok")]
    if bad:
        return False, f"upload loi {len(bad)}/{len(res)}: {bad[0].get('error')}"
    return True, f"http d={cd} m={cm}, upload {len(res)} file"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--limit", type=int, default=0, help="chi xu ly N URL (de thu)")
    ap.add_argument("--sleep", type=float, default=0.5, help="nghi giua cac URL (giay)")
    a = ap.parse_args()

    if not os.path.exists(START_FILE):
        open(START_FILE, "w").write(str(int(time.time())))
    start_ts = int(open(START_FILE).read().strip())

    c = db()
    total = c.execute("SELECT count(*) FROM urls WHERE url LIKE ? AND url NOT LIKE ?", (PAT, PAT_DEEP)).fetchone()[0]
    todo = [r for r in c.execute(
        "SELECT id, url FROM urls WHERE url LIKE ? AND url NOT LIKE ? AND (last_update IS NULL OR last_update < ?) "
        "ORDER BY last_update ASC", (PAT, PAT_DEEP, start_ts))]
    c.close()
    done = total - len(todo)
    if a.limit:
        todo = todo[:a.limit]
    log(f"=== BAT DAU: tong {total} URL /en/map-of-*/ | da xong tu dot {datetime.fromtimestamp(start_ts):%Y-%m-%d %H:%M}: {done} "
        f"| lan nay xu ly: {len(todo)} | log: {LOG}")

    ok_n = fail_n = 0
    t_run = time.time()
    for i, (uid, url) in enumerate(todo, 1):
        if stop:
            break
        t = time.time()
        try:
            ok, note = process(url)
        except Exception as e:  # noqa
            ok, note = False, f"loi: {e}"
        if ok:
            ok_n += 1
            done += 1
            for _ in range(5):
                try:
                    c = db(); c.execute("UPDATE urls SET cached=1, last_update=? WHERE id=?", (int(time.time()), uid)); c.commit(); c.close()
                    break
                except sqlite3.OperationalError as e:
                    log(f"   (db ban, thu lai: {e})"); time.sleep(2)
        else:
            fail_n += 1
        avg = (time.time() - t_run) / i
        eta = avg * (len(todo) - i)
        log(f"[{done}/{total}] {'OK  ' if ok else 'FAIL'} {time.time()-t:4.1f}s  {urlparse(url).path}  ({note})"
            f"  | lan nay ok={ok_n} loi={fail_n} | ETA {int(eta//3600)}h{int(eta%3600//60):02d}m")
        time.sleep(a.sleep)

    log(f"=== KET THUC: lan nay ok={ok_n} loi={fail_n} | tien do {done}/{total}"
        + (" | DA XONG TOAN BO - co the xoa file .tmp_update_en_map_of.start" if done >= total else ""))


if __name__ == "__main__":
    main()
