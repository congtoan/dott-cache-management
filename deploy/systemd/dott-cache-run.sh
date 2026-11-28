#!/bin/bash
# dott-cache-run.sh: chạy một lệnh dott-cache theo lịch (gọi từ systemd), ghi log, dọn log cũ.
#   dott-cache-run.sh <tên> <tham số dott-cache...>
#   vd: dott-cache-run.sh sync-vi sync -site diaocthongthai.com -blog /
# Log: /root/tool/cache-system/logs/<site>/cron-<tên>-<thời gian>.txt; xóa log quá 30 ngày.
# (Telegram: thêm sau, dùng chung bot với /root/health.)
set -uo pipefail
H=/root/tool/cache-system
name=$1; shift
site=$(printf '%s\n' "$@" | grep -A1 -x -- '-site' | tail -1)
dir=$H/logs/${site:-_}
mkdir -p "$dir"
log=$dir/cron-$name-$(date +%Y%m%d-%H%M%S).txt
{
  echo "# $(date '+%F %T') dott-cache $*"
  # Các lịch của cùng site chạy nối đuôi nhau (chờ tối đa 2 giờ), không bỏ lượt như khóa trong dott-cache.
  mkdir -p "$H/data/${site:-_}"
  flock -w 7200 "$H/data/${site:-_}/.cronlock" $H/bin/dott-cache "$@"
  rc=$?
  echo "# $(date '+%F %T') exit=$rc"
} > "$log" 2>&1
find "$H/logs" -type f -mtime +30 -delete 2>/dev/null
exit 0
