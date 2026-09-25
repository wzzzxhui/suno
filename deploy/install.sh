#!/usr/bin/env bash
# 首次部署：安装依赖、建库、安装文件、配置 systemd 与 Nginx。可重复执行，不会覆盖已有的 .env 和数据。
# 用法：解压发布包后，改好 deploy/deploy.conf，执行
#   sudo bash suno/deploy/install.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PKG_DIR="$(dirname "$SCRIPT_DIR")"
# shellcheck source=deploy.conf
source "$SCRIPT_DIR/deploy.conf"

log()  { echo -e "\033[32m==> $*\033[0m"; }
warn() { echo -e "\033[33m[!] $*\033[0m"; }
die()  { echo -e "\033[31m[x] $*\033[0m" >&2; exit 1; }

# 在 .env 中设置 KEY=VALUE：已有则替换，没有则追加
set_env() {
  local file=$1 key=$2 val=$3
  if grep -q "^${key}=" "$file"; then
    KEY="$key" VAL="$val" awk 'BEGIN{k=ENVIRON["KEY"]; v=ENVIRON["VAL"]} index($0, k"=")==1 {print k"="v; next} {print}' \
      "$file" > "$file.tmp" && mv "$file.tmp" "$file"
  else
    echo "${key}=${val}" >> "$file"
  fi
}

render() { # 模板 → 目标文件，替换占位符
  sed -e "s#__INSTALL_DIR__#${INSTALL_DIR}#g" \
      -e "s#__RUN_USER__#${RUN_USER}#g" \
      -e "s#__API_DOMAIN__#${API_DOMAIN}#g" \
      -e "s#__ADMIN_DOMAIN__#${ADMIN_DOMAIN}#g" \
      -e "s#__OPEN_DOMAIN__#${OPEN_DOMAIN}#g" \
      "$1" > "$2"
}

# ---------- 0. 检查 ----------
[[ $EUID -eq 0 ]] || die "请用 root 执行：sudo bash $0"
[[ "$PKG_DIR" != "$INSTALL_DIR" ]] || die "请在解压出的发布包目录执行，不要在安装目录 $INSTALL_DIR 里执行"
[[ -f "$PKG_DIR/backend/suno-server" ]] || die "未找到 $PKG_DIR/backend/suno-server，请在解压后的发布包里执行"
[[ "$DB_PASS" != "请改成强密码" && -n "$DB_PASS" ]] || die "请先修改 deploy.conf 里的 DB_PASS"
[[ "$DB_PASS" =~ ^[A-Za-z0-9_]+$ ]] || die "DB_PASS 只能用字母、数字、下划线（会拼进 MySQL DSN，特殊字符会导致解析错误）"
[[ "$API_DOMAIN" != *example.com ]] || die "请先修改 deploy.conf 里的域名"
command -v apt-get >/dev/null || die "本脚本按 Ubuntu/Debian 编写，其他发行版请参照 README 手动安装"

if [[ -n "${TIMEZONE:-}" ]]; then
  log "设置时区 $TIMEZONE"
  timedatectl set-timezone "$TIMEZONE"
fi

# ---------- 1. 系统依赖 ----------
log "安装系统依赖（nginx / mysql / ffmpeg / 中文字体）"
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y nginx mysql-server ffmpeg fonts-noto-cjk curl ca-certificates

if [[ "$ENABLE_OPEN" == "1" ]]; then
  if ! command -v node >/dev/null || [[ $(node -v | sed 's/v\([0-9]*\).*/\1/') -lt 18 ]]; then
    log "安装 Node.js 20"
    curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
    apt-get install -y nodejs
  fi
fi

log "检查 ffmpeg 编码器与滤镜"
ffmpeg -hide_banner -encoders 2>/dev/null | grep -q libx264 || warn "ffmpeg 缺少 libx264，AI MV 合成会失败"
ffmpeg -hide_banner -encoders 2>/dev/null | grep -q libmp3lame || warn "ffmpeg 缺少 libmp3lame，作品无法以 MP3 格式下载"
ffmpeg -hide_banner -filters  2>/dev/null | grep -q drawtext || warn "ffmpeg 缺少 drawtext，MV 字幕不可用"

# ---------- 2. 运行用户与目录 ----------
id "$RUN_USER" &>/dev/null || { log "创建运行用户 $RUN_USER"; useradd -r -s /usr/sbin/nologin "$RUN_USER"; }
mkdir -p "$INSTALL_DIR"/{backend/internal/storage,admin,deploy,backup}

# ---------- 3. 安装文件 ----------
log "安装后端到 $INSTALL_DIR/backend"
install -m 755 "$PKG_DIR/backend/suno-server" "$INSTALL_DIR/backend/suno-server"
install -m 644 "$PKG_DIR/backend/internal/storage/schema.sql" "$INSTALL_DIR/backend/internal/storage/schema.sql"
install -m 644 "$PKG_DIR/backend/.env.example" "$INSTALL_DIR/backend/.env.example"

ENV_FILE="$INSTALL_DIR/backend/.env"
if [[ ! -f "$ENV_FILE" ]]; then
  log "生成 .env（基于 .env.example）"
  cp "$INSTALL_DIR/backend/.env.example" "$ENV_FILE"
  set_env "$ENV_FILE" SUNO_HTTP_ADDR "127.0.0.1:8080"
  set_env "$ENV_FILE" SUNO_MYSQL_DSN "${DB_USER}:${DB_PASS}@tcp(127.0.0.1:3306)/suno_open?parseTime=true&charset=utf8mb4&loc=Local"
  origins="https://${ADMIN_DOMAIN}"
  [[ "$ENABLE_OPEN" == "1" ]] && origins="${origins},https://${OPEN_DOMAIN}"
  set_env "$ENV_FILE" SUNO_ALLOWED_ORIGINS "$origins"
  set_env "$ENV_FILE" FFMPEG_PATH "$(command -v ffmpeg)"
  font=/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc
  [[ -f $font ]] && set_env "$ENV_FILE" MV_FONT_FILE "$font"
  # 创作证明上的二维码指向公开站核验页
  [[ "$ENABLE_OPEN" == "1" ]] && set_env "$ENV_FILE" CERT_VERIFY_URL "https://${OPEN_DOMAIN}/verify"
  NEW_ENV=1
else
  log ".env 已存在，保持不变"
  NEW_ENV=0
fi
chown -R "$RUN_USER:$RUN_USER" "$INSTALL_DIR/backend"
chmod 600 "$ENV_FILE"

log "安装运营后台到 $INSTALL_DIR/admin"
rm -rf "$INSTALL_DIR/admin" && mkdir -p "$INSTALL_DIR/admin"
cp -r "$PKG_DIR/admin/." "$INSTALL_DIR/admin/"

if [[ "$ENABLE_OPEN" == "1" ]]; then
  [[ -d "$PKG_DIR/open/.output" ]] || die "ENABLE_OPEN=1 但发布包里没有 open/.output，打包时去掉 -SkipOpen"
  log "安装公开站到 $INSTALL_DIR/open"
  rm -rf "$INSTALL_DIR/open" && mkdir -p "$INSTALL_DIR/open"
  cp -r "$PKG_DIR/open/.output" "$INSTALL_DIR/open/"
  chown -R "$RUN_USER:$RUN_USER" "$INSTALL_DIR/open"
fi

log "安装运维脚本到 $INSTALL_DIR/deploy"
cp -r "$SCRIPT_DIR/." "$INSTALL_DIR/deploy/"
chmod +x "$INSTALL_DIR"/deploy/*.sh

# ---------- 4. 数据库 ----------
log "创建 MySQL 库与账号"
systemctl enable --now mysql
mysql <<SQL
CREATE DATABASE IF NOT EXISTS \`suno_open\` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER IF NOT EXISTS '${DB_USER}'@'127.0.0.1' IDENTIFIED BY '${DB_PASS}';
CREATE USER IF NOT EXISTS '${DB_USER}'@'localhost' IDENTIFIED BY '${DB_PASS}';
GRANT ALL PRIVILEGES ON \`suno_open\`.* TO '${DB_USER}'@'127.0.0.1';
GRANT ALL PRIVILEGES ON \`suno_open\`.* TO '${DB_USER}'@'localhost';
FLUSH PRIVILEGES;
SQL

log "建表（-migrate，可重复执行）"
(cd "$INSTALL_DIR/backend" && sudo -u "$RUN_USER" ./suno-server -migrate)

# ---------- 5. systemd ----------
log "配置 systemd 服务"
render "$SCRIPT_DIR/templates/suno.service" /etc/systemd/system/suno.service
if [[ "$ENABLE_OPEN" == "1" ]]; then
  render "$SCRIPT_DIR/templates/suno-open.service" /etc/systemd/system/suno-open.service
fi
systemctl daemon-reload
systemctl enable suno
systemctl restart suno
if [[ "$ENABLE_OPEN" == "1" ]]; then
  systemctl enable suno-open
  systemctl restart suno-open
fi

# ---------- 6. Nginx ----------
log "配置 Nginx"
if [[ -f /etc/nginx/conf.d/suno.conf ]] && grep -q "managed by Certbot" /etc/nginx/conf.d/suno.conf; then
  warn "suno.conf 已由 certbot 配过 HTTPS，不覆盖；模板有改动请手动合并"
else
  render "$SCRIPT_DIR/templates/nginx-suno.conf" /etc/nginx/conf.d/suno.conf
fi
if [[ "$ENABLE_OPEN" == "1" ]]; then
  if [[ -f /etc/nginx/conf.d/suno-open.conf ]] && grep -q "managed by Certbot" /etc/nginx/conf.d/suno-open.conf; then
    warn "suno-open.conf 已由 certbot 配过 HTTPS，不覆盖"
  else
    render "$SCRIPT_DIR/templates/nginx-open.conf" /etc/nginx/conf.d/suno-open.conf
  fi
fi
nginx -t
systemctl enable nginx
systemctl reload nginx

# ---------- 7. 每日备份 ----------
log "配置每日 03:30 数据库备份"
cat > /etc/cron.d/suno-backup <<CRON
30 3 * * * root $INSTALL_DIR/deploy/backup.sh >> /var/log/suno-backup.log 2>&1
CRON

# ---------- 8. 健康检查 ----------
sleep 2
if curl -fsS http://127.0.0.1:8080/healthz >/dev/null; then
  log "后端已启动：/healthz 正常"
else
  warn "后端 /healthz 无响应，查看日志：journalctl -u suno -n 100 --no-pager"
fi

cat <<EOF

========================================================
 部署完成。接下来：

 1. 填写上游与第三方配置（SUNO_PROVIDER / SUNO_UPSTREAM_KEY / TME_* / MV_LLM_* 等）：
      sudo vi $ENV_FILE
      sudo systemctl restart suno
$( [[ $NEW_ENV == 1 ]] && echo "    （当前 SUNO_PROVIDER 仍是 mock，只会返回假数据）" )
 2. 创建运营后台管理员与商户：
      sudo $INSTALL_DIR/deploy/suno-cli.sh -create-admin admin -admin-password '至少8位密码'
      sudo $INSTALL_DIR/deploy/suno-cli.sh -create-merchant "商户名"

 3. 申请 HTTPS 证书：
      sudo apt install -y certbot python3-certbot-nginx
      sudo certbot --nginx -d $API_DOMAIN -d $ADMIN_DOMAIN$( [[ "$ENABLE_OPEN" == "1" ]] && echo " -d $OPEN_DOMAIN" )

 4. 云服务器安全组只放行 22 / 80 / 443
========================================================
EOF
