#!/usr/bin/env bash
# 以运行用户、在正确的工作目录（读取 .env）执行 suno-server 的管理命令。
# 例：
#   sudo /opt/suno/deploy/suno-cli.sh -create-admin admin -admin-password '至少8位密码'
#   sudo /opt/suno/deploy/suno-cli.sh -create-merchant "商户名" -points 1000
#   sudo /opt/suno/deploy/suno-cli.sh -tme-check
#   sudo /opt/suno/deploy/suno-cli.sh -migrate
set -euo pipefail

INSTALL_DIR="${INSTALL_DIR:-/opt/suno}"
# shellcheck source=deploy.conf
source "$INSTALL_DIR/deploy/deploy.conf"

[[ $# -gt 0 ]] || { "$INSTALL_DIR/backend/suno-server" -h 2>&1 || true; exit 0; }
cd "$INSTALL_DIR/backend"
exec sudo -u "$RUN_USER" ./suno-server "$@"
