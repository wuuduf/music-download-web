#!/usr/bin/env bash
# =============================================================================
# musicweb 迁移备份脚本 —— 在【原 VPS】上运行
#
# 打包成一个自包含的归档，供 migrate-deploy.sh 在新 VPS 上还原：
#   - 二进制、config.ini、前端资源 webui/dist
#   - SQLite 数据库（用 sqlite3 .backup 做一致性快照，避免热拷贝损坏）
#   - 平台凭据 data/（WVD 等）
#   - systemd unit、nginx / Caddy 站点配置、Let's Encrypt 证书
#   - 一份 manifest.env，记录域名、端口、用户等，供部署端自动识别
#
# 默认不含 cache/（可重新下载的音频缓存，通常几十 MB~几 GB）；
# 需要连缓存一起搬时加 --with-cache。
#
# 用法:
#   sudo ./migrate-backup.sh                    # 输出到 /root/musicweb-migration-<时间>.tar.gz
#   sudo ./migrate-backup.sh --with-cache
#   sudo ./migrate-backup.sh -o /tmp/mw.tar.gz
# =============================================================================
set -euo pipefail

APP_DIR="${APP_DIR:-/opt/musicweb/app}"
SERVICE_NAME="${SERVICE_NAME:-musicweb}"
WITH_CACHE=0
OUT=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --with-cache) WITH_CACHE=1; shift ;;
    -o|--output)  OUT="$2"; shift 2 ;;
    -h|--help)    sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

[[ $EUID -eq 0 ]] || { echo "请用 root 运行（需要读取 config.ini / 证书）"; exit 1; }
[[ -d "$APP_DIR" ]] || { echo "找不到 $APP_DIR，请用 APP_DIR=... 指定"; exit 1; }

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="${OUT:-/root/musicweb-migration-$stamp.tar.gz}"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
ROOT="$STAGE/musicweb-migration"
mkdir -p "$ROOT"/{app,system}

echo "==> 1/6 读取当前配置"
cfg_get() { grep -E "^[[:space:]]*$1[[:space:]]*=" "$APP_DIR/config.ini" 2>/dev/null | head -1 | sed 's/^[^=]*=[[:space:]]*//' | tr -d '\r'; }
LISTEN_ADDR="$(cfg_get WebListenAddr)"; LISTEN_ADDR="${LISTEN_ADDR:-127.0.0.1:8080}"
APP_PORT="${LISTEN_ADDR##*:}"
PUBLIC_URL="$(cfg_get WebPublicBaseURL)"
DOMAIN="$(printf '%s' "$PUBLIC_URL" | sed -E 's#^https?://##; s#/.*$##')"
SVC_USER="$(systemctl show "$SERVICE_NAME" -p User --value 2>/dev/null || echo musicweb)"
SVC_USER="${SVC_USER:-musicweb}"
echo "    域名=${DOMAIN:-<未配置>}  端口=$APP_PORT  服务用户=$SVC_USER"

echo "==> 2/6 停止服务以取得一致快照"
RESTART_AFTER=0
if systemctl is-active --quiet "$SERVICE_NAME"; then
  systemctl stop "$SERVICE_NAME"; RESTART_AFTER=1
fi

echo "==> 3/6 复制应用文件"
# 二进制与配置
[[ -f "$APP_DIR/musicweb" ]] && cp -a "$APP_DIR/musicweb" "$ROOT/app/"
[[ -f "$APP_DIR/config.ini" ]] && cp -a "$APP_DIR/config.ini" "$ROOT/app/"
# 前端资源
[[ -d "$APP_DIR/webui" ]] && cp -a "$APP_DIR/webui" "$ROOT/app/"
# 平台凭据（WVD 等）
[[ -d "$APP_DIR/data" ]] && cp -a "$APP_DIR/data" "$ROOT/app/"
# 数据库：优先用 sqlite3 .backup 取一致快照；没有该命令时服务已停止，直接拷贝也安全
for db in "$APP_DIR"/*.db; do
  [[ -f "$db" ]] || continue
  if command -v sqlite3 >/dev/null 2>&1; then
    sqlite3 "$db" ".backup '$ROOT/app/$(basename "$db")'"
  else
    cp -a "$db" "$ROOT/app/"
  fi
done
if [[ $WITH_CACHE -eq 1 && -d "$APP_DIR/cache" ]]; then
  echo "    含音频缓存（$(du -sh "$APP_DIR/cache" | cut -f1)）"
  cp -a "$APP_DIR/cache" "$ROOT/app/"
fi
# macOS 传输过来的 AppleDouble 垃圾文件不要带进新机
find "$ROOT/app" -name '._*' -delete 2>/dev/null || true

echo "==> 4/6 复制系统配置"
cp -a "/etc/systemd/system/$SERVICE_NAME.service" "$ROOT/system/" 2>/dev/null || true
cp -a "/etc/systemd/system/$SERVICE_NAME-backup.service" "$ROOT/system/" 2>/dev/null || true
cp -a "/etc/systemd/system/$SERVICE_NAME-backup.timer" "$ROOT/system/" 2>/dev/null || true
REVERSE_PROXY="none"
if [[ -n "$DOMAIN" ]]; then
  for f in "/etc/nginx/sites-available/$DOMAIN.conf" "/etc/nginx/conf.d/$DOMAIN.conf"; do
    [[ -f "$f" ]] && { mkdir -p "$ROOT/system/nginx"; cp -a "$f" "$ROOT/system/nginx/"; REVERSE_PROXY="nginx"; }
  done
  if [[ -f /etc/caddy/Caddyfile ]] && grep -q "$DOMAIN" /etc/caddy/Caddyfile 2>/dev/null; then
    mkdir -p "$ROOT/system/caddy"
    # 只截取该域名的 site block，避免把别的站点配置也搬过去
    awk -v d="$DOMAIN" '$0 ~ d"[ ,{]" {f=1} f {print} f && /^}/ {f=0}' /etc/caddy/Caddyfile > "$ROOT/system/caddy/site.caddy" || true
    REVERSE_PROXY="caddy"
  fi
  if [[ -d "/etc/letsencrypt/live/$DOMAIN" ]]; then
    mkdir -p "$ROOT/system/letsencrypt"
    # -L 展开符号链接，否则新机上会得到断链
    tar -C /etc/letsencrypt -chzf "$ROOT/system/letsencrypt/certs.tar.gz" "live/$DOMAIN" "archive/$DOMAIN" 2>/dev/null || true
  fi
fi
echo "    反向代理: $REVERSE_PROXY"

echo "==> 5/6 写入 manifest"
cat > "$ROOT/manifest.env" <<EOF
# musicweb 迁移清单（由 migrate-backup.sh 生成）
BACKUP_TIME=$stamp
SOURCE_HOST=$(hostname)
APP_DIR=$APP_DIR
SERVICE_NAME=$SERVICE_NAME
SERVICE_USER=$SVC_USER
LISTEN_ADDR=$LISTEN_ADDR
APP_PORT=$APP_PORT
DOMAIN=$DOMAIN
PUBLIC_URL=$PUBLIC_URL
REVERSE_PROXY=$REVERSE_PROXY
WITH_CACHE=$WITH_CACHE
EOF

if [[ $RESTART_AFTER -eq 1 ]]; then
  systemctl start "$SERVICE_NAME"
  echo "    原服务已重新启动"
fi

echo "==> 6/6 打包"
tar -C "$STAGE" -czf "$OUT" musicweb-migration
chmod 600 "$OUT"

cat <<EOF

✅ 备份完成
   文件: $OUT  ($(du -h "$OUT" | cut -f1))
   注意: 内含 config.ini（管理员密码哈希、平台 Cookie、会话密钥），权限已设为 600，请勿公开传播。

下一步，在新 VPS 上执行:
   scp -P <端口> $OUT root@<新VPS>:/root/
   scp -P <端口> migrate-deploy.sh root@<新VPS>:/root/
   ssh -p <端口> root@<新VPS> 'bash /root/migrate-deploy.sh /root/$(basename "$OUT")'
EOF
