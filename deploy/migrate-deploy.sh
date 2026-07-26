#!/usr/bin/env bash
# =============================================================================
# musicweb 迁移部署脚本 —— 在【新 VPS】上运行
#
# 还原 migrate-backup.sh 生成的归档，并在动手前做冲突检查：
#   1) 应用端口冲突 —— 自动挑选空闲端口，或（占用方是 Docker 时）提供改容器端口
#   2) 反向代理冲突 —— 检测 nginx / Caddy 里是否已有同域名站点，给出具体解决办法
#   3) 证书 —— 沿用备份中的 Let's Encrypt 证书；没有则提示如何签发
#
# 全程只新增本服务相关的文件，不改动新机上已有站点的配置。
#
# 用法:
#   sudo ./migrate-deploy.sh /root/musicweb-migration-xxx.tar.gz
#   sudo ./migrate-deploy.sh <包> --port 18081     # 直接指定端口，跳过交互
#   sudo ./migrate-deploy.sh <包> --yes            # 全部用推荐值，无人值守
# =============================================================================
set -euo pipefail

ARCHIVE="${1:-}"; shift || true
FORCE_PORT=""; ASSUME_YES=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --port) FORCE_PORT="$2"; shift 2 ;;
    --yes|-y) ASSUME_YES=1; shift ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

[[ $EUID -eq 0 ]] || { echo "请用 root 运行"; exit 1; }
[[ -f "$ARCHIVE" ]] || { echo "用法: $0 <migration.tar.gz> [--port N] [--yes]"; exit 1; }

RED=$'\e[31m'; GRN=$'\e[32m'; YEL=$'\e[33m'; BLD=$'\e[1m'; RST=$'\e[0m'
info() { echo "${GRN}==>${RST} $*"; }
warn() { echo "${YEL}[!]${RST} $*"; }
err()  { echo "${RED}[×]${RST} $*"; }
ask()  { # ask <提示> <默认y|n> -> 0=yes
  [[ $ASSUME_YES -eq 1 ]] && return 0
  local p="$1" d="${2:-y}" a
  read -r -p "$p [$( [[ $d == y ]] && echo 'Y/n' || echo 'y/N' )] " a </dev/tty || a=""
  a="${a:-$d}"; [[ "${a,,}" == y* ]]
}

STAGE="$(mktemp -d)"; trap 'rm -rf "$STAGE"' EXIT
info "解包 $ARCHIVE"
tar -C "$STAGE" -xzf "$ARCHIVE"
ROOT="$STAGE/musicweb-migration"
[[ -f "$ROOT/manifest.env" ]] || { err "归档格式不对（缺 manifest.env）"; exit 1; }
# shellcheck disable=SC1091
source "$ROOT/manifest.env"
APP_DIR="${APP_DIR:-/opt/musicweb/app}"
SERVICE_NAME="${SERVICE_NAME:-musicweb}"
SERVICE_USER="${SERVICE_USER:-musicweb}"

echo
echo "${BLD}迁移内容${RST}"
echo "  来源主机 : ${SOURCE_HOST:-未知}（备份于 ${BACKUP_TIME:-未知}）"
echo "  域名     : ${DOMAIN:-<未配置>}"
echo "  原端口   : ${APP_PORT}"
echo "  反向代理 : ${REVERSE_PROXY:-none}"
echo

# ---------------------------------------------------------------------------
# 检查 1：端口冲突
# ---------------------------------------------------------------------------
info "检查 1/3：端口占用"
port_user() { ss -tlnp 2>/dev/null | awk -v p=":$1\$" '$4 ~ p {print $NF; exit}'; }
port_busy() { ss -tln 2>/dev/null | awk -v p=":$1\$" '$4 ~ p {found=1} END {exit !found}'; }
docker_on_port() { # 输出占用该端口的容器名
  command -v docker >/dev/null 2>&1 || return 1
  docker ps --format '{{.Names}}\t{{.Ports}}' 2>/dev/null \
    | awk -v p=":$1->" '$0 ~ p {print $1; exit}'
}

NEW_PORT="${FORCE_PORT:-$APP_PORT}"
if port_busy "$NEW_PORT"; then
  holder="$(port_user "$NEW_PORT" || true)"
  warn "端口 ${BLD}$NEW_PORT${RST} 已被占用： ${holder:-未知进程}"
  CONTAINER="$(docker_on_port "$NEW_PORT" || true)"

  if [[ -n "$CONTAINER" ]]; then
    echo "    占用方是 Docker 容器：${BLD}$CONTAINER${RST}"
    echo "    可选方案："
    echo "      A) 让 musicweb 换一个空闲端口（推荐，不动你的容器）"
    echo "      B) 把该容器改到其它端口，musicweb 沿用 $NEW_PORT"
    if [[ $ASSUME_YES -eq 0 ]] && ask "    选 B（改 Docker 容器端口）吗？选 N 则走 A" n; then
      read -r -p "    容器 $CONTAINER 的新宿主机端口: " CPORT </dev/tty
      if port_busy "$CPORT"; then err "端口 $CPORT 同样被占用，已取消"; exit 1; fi
      cimg="$(docker inspect -f '{{.Config.Image}}' "$CONTAINER")"
      cintl="$(docker inspect -f '{{range $p,$c := .NetworkSettings.Ports}}{{$p}}{{end}}' "$CONTAINER" | head -1)"
      echo
      echo "    ${BLD}Docker 端口无法在线修改，需要用新端口重建容器。${RST}"
      echo "    该容器由 docker compose 管理时，请改 compose 文件的 ports 后 'docker compose up -d'。"
      echo "    独立容器可参考："
      echo "      docker stop $CONTAINER && docker rename $CONTAINER ${CONTAINER}-old"
      echo "      docker run -d --name $CONTAINER -p ${CPORT}:${cintl%%/*} $cimg   # 其余参数按原容器补齐"
      echo "      # 确认新容器正常后： docker rm ${CONTAINER}-old"
      echo
      warn "改完容器后重新运行本脚本（musicweb 将使用 $NEW_PORT）。"
      exit 0
    fi
  fi

  # 方案 A：自动选一个空闲端口
  cand=$((NEW_PORT + 1))
  while port_busy "$cand" && [[ $cand -lt 65000 ]]; do cand=$((cand + 1)); done
  if [[ $ASSUME_YES -eq 1 ]] || ask "    改用空闲端口 ${BLD}$cand${RST}？" y; then
    NEW_PORT="$cand"
  else
    read -r -p "    手动指定端口: " NEW_PORT </dev/tty
    port_busy "$NEW_PORT" && { err "端口 $NEW_PORT 仍被占用"; exit 1; }
  fi
fi
echo "    musicweb 将监听 127.0.0.1:${BLD}$NEW_PORT${RST}"

# ---------------------------------------------------------------------------
# 检查 2：反向代理冲突
# ---------------------------------------------------------------------------
info "检查 2/3：反向代理与域名"
HAVE_NGINX=0; HAVE_CADDY=0; DOMAIN_TAKEN=""
command -v nginx >/dev/null 2>&1 && HAVE_NGINX=1
command -v caddy >/dev/null 2>&1 && HAVE_CADDY=1

if [[ -n "${DOMAIN:-}" ]]; then
  if [[ $HAVE_NGINX -eq 1 ]]; then
    # 找出已存在、且不是本域名自己配置文件的 server_name 冲突
    hit="$(grep -rlE "server_name[^;]*\b${DOMAIN//./\\.}\b" /etc/nginx/sites-enabled/ /etc/nginx/conf.d/ 2>/dev/null | grep -v "$DOMAIN.conf" || true)"
    [[ -n "$hit" ]] && DOMAIN_TAKEN="nginx:$hit"
  fi
  if [[ $HAVE_CADDY -eq 1 && -f /etc/caddy/Caddyfile ]]; then
    grep -qE "^\s*(https?://)?${DOMAIN//./\\.}[\s,{:]" /etc/caddy/Caddyfile 2>/dev/null && DOMAIN_TAKEN="caddy:/etc/caddy/Caddyfile"
  fi
fi

if [[ -n "$DOMAIN_TAKEN" ]]; then
  echo
  err "域名 ${BLD}$DOMAIN${RST} 在新机上已被占用：${DOMAIN_TAKEN#*:}"
  cat <<EOF

  ${BLD}如何解决（三选一）${RST}
  1. 该配置已废弃 → 删除或停用它，再重跑本脚本：
       nginx: rm /etc/nginx/sites-enabled/<冲突文件> && nginx -t && systemctl reload nginx
       caddy: 编辑 /etc/caddy/Caddyfile 删掉该站点块 && systemctl reload caddy
  2. 该配置还要用 → 给 musicweb 换个域名（例如 music2.example.com）：
       改备份里的 config.ini 中 WebPublicBaseURL，并把站点配置里的 server_name 一并改掉。
  3. 想让两者共存 → 用不同路径前缀，把 musicweb 挂到已有站点的子路径下
       （需自行调整 nginx location / Caddy handle_path，注意 musicweb 目前按根路径提供服务）。

  本脚本不会覆盖新机上已有的站点配置，请先处理后重试。
EOF
  exit 1
fi

if [[ $HAVE_NGINX -eq 0 && $HAVE_CADDY -eq 0 ]]; then
  warn "新机没有检测到 nginx 或 Caddy。应用仍会部署在 127.0.0.1:$NEW_PORT，"
  warn "但需要你自行装反代并配 HTTPS 才能用域名访问。"
else
  echo "    未发现域名冲突（nginx=$HAVE_NGINX caddy=$HAVE_CADDY）"
fi

# ---------------------------------------------------------------------------
# 检查 3：依赖与证书
# ---------------------------------------------------------------------------
info "检查 3/3：依赖与证书"
CERT_OK=0
[[ -f "$ROOT/system/letsencrypt/certs.tar.gz" ]] && CERT_OK=1
[[ -d "/etc/letsencrypt/live/${DOMAIN:-__none__}" ]] && CERT_OK=1
[[ $CERT_OK -eq 1 ]] && echo "    证书：可用（备份内含或本机已有）" || warn "证书：缺失，稍后需用 certbot 签发"

# ---------------------------------------------------------------------------
# 开始部署
# ---------------------------------------------------------------------------
echo
ask "以上检查完成，开始部署？" y || { echo "已取消"; exit 0; }

info "创建服务用户与目录"
id "$SERVICE_USER" >/dev/null 2>&1 || useradd -r -s /usr/sbin/nologin -d "$(dirname "$APP_DIR")" "$SERVICE_USER"
mkdir -p "$APP_DIR"
if [[ -f "$APP_DIR/config.ini" ]]; then
  cp -a "$APP_DIR/config.ini" "$APP_DIR/config.ini.bak.$(date +%s)"
  warn "已备份新机上原有的 config.ini"
fi

info "还原应用文件"
cp -a "$ROOT/app/." "$APP_DIR/"
mkdir -p "$APP_DIR"/{cache/web,data/credentials,log}

info "按新端口调整配置"
sed -i -E "s#^([[:space:]]*WebListenAddr[[:space:]]*=).*#\1 127.0.0.1:$NEW_PORT#" "$APP_DIR/config.ini"
grep -qE '^[[:space:]]*WebListenAddr' "$APP_DIR/config.ini" || echo "WebListenAddr = 127.0.0.1:$NEW_PORT" >> "$APP_DIR/config.ini"

info "设置权限"
chown -R "$SERVICE_USER:$SERVICE_USER" "$(dirname "$APP_DIR")"
chmod 750 "$(dirname "$APP_DIR")" "$APP_DIR"
chmod 600 "$APP_DIR/config.ini"
chmod 700 "$APP_DIR/data" "$APP_DIR/cache" "$APP_DIR/log" 2>/dev/null || true
find "$APP_DIR" -maxdepth 1 -name '*.db*' -exec chmod 600 {} + 2>/dev/null || true
# 反代要以 www-data 直接读前端静态文件，故放开这一部分的读权限
chmod o+x "$(dirname "$APP_DIR")" "$APP_DIR"
chmod -R o+rX "$APP_DIR/webui" 2>/dev/null || true

info "安装 systemd 服务"
if [[ -f "$ROOT/system/$SERVICE_NAME.service" ]]; then
  cp -a "$ROOT/system/$SERVICE_NAME.service" /etc/systemd/system/
else
  cat > "/etc/systemd/system/$SERVICE_NAME.service" <<EOF
[Unit]
Description=MusicWeb
After=network-online.target
Wants=network-online.target
[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_USER
WorkingDirectory=$APP_DIR
ExecStart=$APP_DIR/musicweb -c $APP_DIR/config.ini
Restart=on-failure
RestartSec=5s
Environment=GOMAXPROCS=1
Environment=GOMEMLIMIT=300MiB
MemoryMax=400M
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true
ReadWritePaths=$APP_DIR
[Install]
WantedBy=multi-user.target
EOF
fi
systemctl daemon-reload
systemctl enable "$SERVICE_NAME" >/dev/null 2>&1 || true
systemctl restart "$SERVICE_NAME"

info "等待服务就绪"
ok=0
for _ in $(seq 1 20); do
  sleep 1
  if curl -fsS "http://127.0.0.1:$NEW_PORT/api/v1/health" >/dev/null 2>&1; then ok=1; break; fi
done
if [[ $ok -eq 1 ]]; then
  echo "    健康检查通过：$(curl -fsS "http://127.0.0.1:$NEW_PORT/api/v1/health")"
else
  err "服务未就绪，请查看： journalctl -u $SERVICE_NAME -n 50 --no-pager"
  exit 1
fi

# ---------------------------------------------------------------------------
# 反向代理
# ---------------------------------------------------------------------------
if [[ -n "${DOMAIN:-}" ]]; then
  info "配置反向代理"
  if [[ -f "$ROOT/system/letsencrypt/certs.tar.gz" && ! -d "/etc/letsencrypt/live/$DOMAIN" ]]; then
    mkdir -p /etc/letsencrypt
    tar -C /etc/letsencrypt -xzf "$ROOT/system/letsencrypt/certs.tar.gz"
    echo "    已还原 $DOMAIN 的证书"
  fi

  if [[ $HAVE_NGINX -eq 1 ]]; then
    src="$(ls "$ROOT"/system/nginx/*.conf 2>/dev/null | head -1 || true)"
    dst="/etc/nginx/sites-available/$DOMAIN.conf"
    [[ -d /etc/nginx/sites-available ]] || { mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled; }
    if [[ -n "$src" ]]; then
      cp -a "$src" "$dst"
      # 端口可能变了，同步反代目标；同时把静态资源根路径指到新机的 APP_DIR
      sed -i -E "s#proxy_pass http://127\.0\.0\.1:[0-9]+#proxy_pass http://127.0.0.1:$NEW_PORT#g" "$dst"
      sed -i -E "s#(root\|alias)[[:space:]]+[^;]*/webui/dist#\1 $APP_DIR/webui/dist#g" "$dst"
    else
      cat > "$dst" <<EOF
server {
    listen 80; listen [::]:80;
    server_name $DOMAIN;
    location /.well-known/acme-challenge/ { root /var/www/certbot; }
    location / { return 301 https://\$host\$request_uri; }
}
server {
    listen 443 ssl http2; listen [::]:443 ssl http2;
    server_name $DOMAIN;
    ssl_certificate     /etc/letsencrypt/live/$DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$DOMAIN/privkey.pem;
    client_max_body_size 16m;
    location / {
        proxy_pass http://127.0.0.1:$NEW_PORT;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_buffering off;
        proxy_read_timeout 3600s;
    }
}
EOF
    fi
    ln -sf "$dst" "/etc/nginx/sites-enabled/$DOMAIN.conf"
    if nginx -t 2>/dev/null; then
      systemctl reload nginx
      echo "    nginx 站点已启用"
    else
      err "nginx 配置测试失败，已移除本站点以免影响其它站点："
      nginx -t || true
      rm -f "/etc/nginx/sites-enabled/$DOMAIN.conf"
      systemctl reload nginx 2>/dev/null || true
      warn "请修正 $dst 后手动 ln -s 启用。"
    fi
  elif [[ $HAVE_CADDY -eq 1 ]]; then
    cat <<EOF

    ${BLD}检测到 Caddy${RST}，请把下面这段加进 /etc/caddy/Caddyfile 后 'systemctl reload caddy'：

    $DOMAIN {
        reverse_proxy 127.0.0.1:$NEW_PORT
    }

    （Caddy 会自动签发并续期证书，无需 certbot。）
EOF
  fi

  if [[ ! -d "/etc/letsencrypt/live/$DOMAIN" && $HAVE_NGINX -eq 1 ]]; then
    warn "没有找到证书，签发命令："
    echo "      mkdir -p /var/www/certbot"
    echo "      certbot certonly --webroot -w /var/www/certbot -d $DOMAIN --agree-tos -m you@example.com --deploy-hook 'systemctl reload nginx'"
  fi
fi

cat <<EOF

${GRN}${BLD}✅ 迁移完成${RST}
   应用    : 127.0.0.1:$NEW_PORT （systemd: $SERVICE_NAME）
   目录    : $APP_DIR
   域名    : ${DOMAIN:-<未配置>}
$( [[ "$NEW_PORT" != "$APP_PORT" ]] && echo "   ${YEL}注意：端口已从 $APP_PORT 改为 $NEW_PORT（原端口被占用）${RST}" )

常用命令:
   systemctl status $SERVICE_NAME
   journalctl -u $SERVICE_NAME -f

收尾清单:
   1. 把域名 DNS 解析改到本机 IP
   2. 浏览器访问 https://${DOMAIN:-<域名>} 验证；登录 /admin 检查平台账号状态
   3. 确认无误后再下线原 VPS
   4. 若未迁移音频缓存（默认不迁），首次下载会重新拉取，属正常现象
EOF
