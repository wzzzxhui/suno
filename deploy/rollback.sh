#!/usr/bin/env bash
# 回滚到上一次 upgrade.sh 之前的版本（只回滚程序文件，数据库不动）。
# 如需同时回滚数据，从 /opt/suno/backup 里挑备份手动导入，见 README。
#   sudo /opt/suno/deploy/rollback.sh
set -euo pipefail

INSTALL_DIR="${INSTALL_DIR:-/opt/suno}"
# shellcheck source=deploy.conf
source "$INSTALL_DIR/deploy/deploy.conf"
PREV="$INSTALL_DIR/prev"

[[ $EUID -eq 0 ]] || { echo "请用 root 执行"; exit 1; }
[[ -f "$PREV/suno-server" ]] || { echo "没有可回滚的版本（$PREV 为空）"; exit 1; }

echo "==> 回滚后端"
install -m 755 "$PREV/suno-server" "$INSTALL_DIR/backend/suno-server"
chown "$RUN_USER:$RUN_USER" "$INSTALL_DIR/backend/suno-server"

echo "==> 回滚运营后台"
rm -rf "$INSTALL_DIR/admin" && cp -a "$PREV/admin" "$INSTALL_DIR/admin"

if [[ -d "$PREV/open-output" ]]; then
  echo "==> 回滚公开站"
  rm -rf "$INSTALL_DIR/open/.output" && cp -a "$PREV/open-output" "$INSTALL_DIR/open/.output"
fi

systemctl restart suno
[[ "$ENABLE_OPEN" == "1" ]] && systemctl restart suno-open
sleep 2
curl -fsS http://127.0.0.1:8080/healthz >/dev/null && echo "==> 回滚完成，/healthz 正常" \
  || echo "[!] /healthz 无响应：journalctl -u suno -n 100 --no-pager"
