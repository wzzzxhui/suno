#!/usr/bin/env bash
# 发版更新：先备份数据库，再替换后端/后台/公开站并重启。保留 .env 与数据，旧版本留作回滚。
# 用法：上传并解压新发布包后执行
#   sudo bash suno/deploy/upgrade.sh
# 部署参数读取已安装的 /opt/suno/deploy/deploy.conf（不是新包里的），装在别处用 INSTALL_DIR=/xxx 指定
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PKG_DIR="$(dirname "$SCRIPT_DIR")"
INSTALL_DIR="${INSTALL_DIR:-/opt/suno}"
[[ -f "$INSTALL_DIR/deploy/deploy.conf" ]] || { echo "未找到 $INSTALL_DIR/deploy/deploy.conf，请先执行 install.sh"; exit 1; }
# shellcheck source=deploy.conf
source "$INSTALL_DIR/deploy/deploy.conf"

log()  { echo -e "\033[32m==> $*\033[0m"; }
warn() { echo -e "\033[33m[!] $*\033[0m"; }
die()  { echo -e "\033[31m[x] $*\033[0m" >&2; exit 1; }

[[ $EUID -eq 0 ]] || die "请用 root 执行：sudo bash $0"
[[ "$PKG_DIR" != "$INSTALL_DIR" ]] || die "请在新发布包目录执行，不要在安装目录里执行"
[[ -f "$PKG_DIR/backend/suno-server" ]] || die "未找到 $PKG_DIR/backend/suno-server"

log "升级前备份数据库"
"$INSTALL_DIR/deploy/backup.sh"

log "保存当前版本用于回滚"
PREV="$INSTALL_DIR/prev"
rm -rf "$PREV" && mkdir -p "$PREV"
cp -a "$INSTALL_DIR/backend/suno-server" "$PREV/"
cp -a "$INSTALL_DIR/admin" "$PREV/admin"
[[ -d "$INSTALL_DIR/open/.output" ]] && cp -a "$INSTALL_DIR/open/.output" "$PREV/open-output"

log "替换后端"
install -m 755 "$PKG_DIR/backend/suno-server" "$INSTALL_DIR/backend/suno-server.new"
mv -f "$INSTALL_DIR/backend/suno-server.new" "$INSTALL_DIR/backend/suno-server"
install -m 644 "$PKG_DIR/backend/internal/storage/schema.sql" "$INSTALL_DIR/backend/internal/storage/schema.sql"
install -m 644 "$PKG_DIR/backend/.env.example" "$INSTALL_DIR/backend/.env.example"
chown -R "$RUN_USER:$RUN_USER" "$INSTALL_DIR/backend"

# 新版本若在 .env.example 里加了配置项，提示一下（程序对缺失项使用默认值，不影响启动）
missing=$(comm -23 \
  <(grep -oE '^[A-Z_]+=' "$INSTALL_DIR/backend/.env.example" | sort -u) \
  <(grep -oE '^[A-Z_]+=' "$INSTALL_DIR/backend/.env" | sort -u) | tr -d '=' | xargs || true)
[[ -n "$missing" ]] && warn ".env 中缺少新配置项（使用默认值）：$missing"
# 创作证明的核验二维码需要公开站地址，老 .env 里没有时自动补上
if [[ "$ENABLE_OPEN" == "1" ]] && ! grep -q '^CERT_VERIFY_URL=.' "$INSTALL_DIR/backend/.env"; then
  sed -i '/^CERT_VERIFY_URL=/d' "$INSTALL_DIR/backend/.env"
  echo "CERT_VERIFY_URL=https://${OPEN_DOMAIN}/verify" >> "$INSTALL_DIR/backend/.env"
  log "已写入 CERT_VERIFY_URL=https://${OPEN_DOMAIN}/verify"
fi

log "建表（仅补新表，已有表不动）"
(cd "$INSTALL_DIR/backend" && sudo -u "$RUN_USER" ./suno-server -migrate)

log "替换运营后台"
rm -rf "$INSTALL_DIR/admin.new" && cp -r "$PKG_DIR/admin" "$INSTALL_DIR/admin.new"
rm -rf "$INSTALL_DIR/admin" && mv "$INSTALL_DIR/admin.new" "$INSTALL_DIR/admin"

if [[ "$ENABLE_OPEN" == "1" && -d "$PKG_DIR/open/.output" ]]; then
  log "替换公开站"
  rm -rf "$INSTALL_DIR/open/.output"
  cp -r "$PKG_DIR/open/.output" "$INSTALL_DIR/open/.output"
  chown -R "$RUN_USER:$RUN_USER" "$INSTALL_DIR/open"
fi

log "更新运维脚本（保留已安装的 deploy.conf）"
find "$SCRIPT_DIR" -maxdepth 1 -name '*.sh' -exec cp {} "$INSTALL_DIR/deploy/" \;
cp -r "$SCRIPT_DIR/templates" "$INSTALL_DIR/deploy/"
cp "$SCRIPT_DIR/README.md" "$INSTALL_DIR/deploy/" 2>/dev/null || true
chmod +x "$INSTALL_DIR"/deploy/*.sh

log "重启服务"
systemctl restart suno
[[ "$ENABLE_OPEN" == "1" ]] && systemctl restart suno-open

sleep 2
if curl -fsS http://127.0.0.1:8080/healthz >/dev/null; then
  log "升级完成，/healthz 正常"
else
  warn "/healthz 无响应。查看日志：journalctl -u suno -n 100 --no-pager"
  warn "需要回滚：sudo $INSTALL_DIR/deploy/rollback.sh"
  exit 1
fi
