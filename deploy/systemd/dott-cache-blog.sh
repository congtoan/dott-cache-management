#!/bin/bash
# dott-cache-blog.sh <instance> <lệnh>: đổi tên instance systemd (vi, en) thành -blog và chạy qua dott-cache-run.sh.
#   lệnh: sync | sync-full | update-all
set -uo pipefail
H=/root/tool/cache-system
SITE=diaocthongthai.com
case "$1" in
  vi) blog=/ ;;
  en) blog=/en/ ;;
  *) echo "instance lạ: $1"; exit 2 ;;
esac
case "$2" in
  sync)       exec $H/bin/dott-cache-run.sh "sync-$1"       sync -site $SITE -blog "$blog" ;;
  sync-full)  exec $H/bin/dott-cache-run.sh "sync-full-$1"  sync -site $SITE -blog "$blog" -full ;;
  update-all) exec $H/bin/dott-cache-run.sh "update-all-$1" update -site $SITE -all -blog "$blog" -yes ;;
  *) echo "lệnh lạ: $2"; exit 2 ;;
esac
