#!/usr/bin/env bash
# 备份 suno_open 库到 /opt/suno/backup，按 deploy.conf 的 BACKUP_KEEP_DAYS 清理旧备份。
# install.sh 已配置每日 03:30 自动执行（/etc/cron.d/suno-backup），也可手动执行：
#   sudo /opt/suno/deploy/backup.sh
set -euo pipefail

INSTALL_DIR="${INSTALL_DIR:-/opt/suno}"
# shellcheck source=deploy.conf
source "$INSTALL_DIR/deploy/deploy.conf"
DIR="$INSTALL_DIR/backup"
mkdir -p "$DIR"
chmod 700 "$DIR"

file="$DIR/suno_open_$(date +%Y%m%d_%H%M%S).sql.gz"
# 用 root 的 socket 认证导出，不在命令行暴露密码
mysqldump --single-transaction --routines --triggers --databases suno_open | gzip > "$file"

# 空文件说明导出失败
[[ $(stat -c %s "$file") -gt 1024 ]] || { echo "$(date '+%F %T') 备份异常：$file 过小"; exit 1; }

find "$DIR" -name 'suno_open_*.sql.gz' -mtime +"${BACKUP_KEEP_DAYS:-14}" -delete
echo "$(date '+%F %T') 备份完成：$file ($(du -h "$file" | cut -f1))"
