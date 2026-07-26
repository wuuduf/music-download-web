# musicweb 迁移指南

把已部署的 musicweb 从一台 VPS 搬到另一台，配套两个脚本：

| 脚本 | 运行位置 | 作用 |
|---|---|---|
| [`deploy/migrate-backup.sh`](../deploy/migrate-backup.sh) | **原 VPS** | 打包应用、数据库、凭据、systemd / nginx / 证书 |
| [`deploy/migrate-deploy.sh`](../deploy/migrate-deploy.sh) | **新 VPS** | 冲突检查 → 还原 → 起服务 → 配反代 |

## 三步走

```bash
# ── 1. 原 VPS：打包（会短暂停服几秒取一致快照，随后自动重启）
sudo ./migrate-backup.sh
#    → /root/musicweb-migration-<时间>.tar.gz

# ── 2. 传到新 VPS
scp -P <端口> /root/musicweb-migration-*.tar.gz root@<新VPS>:/root/
scp -P <端口> deploy/migrate-deploy.sh          root@<新VPS>:/root/

# ── 3. 新 VPS：部署
ssh -p <端口> root@<新VPS>
sudo bash /root/migrate-deploy.sh /root/musicweb-migration-*.tar.gz
```

部署脚本跑完后：把域名 DNS 改到新机 IP → 浏览器验证 → 确认无误再下线原机。

## 包含 / 不包含

**包含**：`musicweb` 二进制、`config.ini`（管理员密码哈希、2FA 密钥、平台 Cookie、会话密钥）、`data.db` + `cache.db`（下载历史、Studio 项目、API Key）、平台凭据 `data/`（WVD 等）、前端 `webui/dist`、systemd unit、nginx / Caddy 站点配置、Let's Encrypt 证书。

**不包含**：`cache/` 音频缓存（可重新下载，通常几十 MB～几 GB）。需要一并搬时用 `--with-cache`。

> ⚠️ 归档内含明文凭据，脚本已设为 `600`。传输请走 scp/rsync，别丢到公开位置；导入完成后建议删除。

## 冲突检查（部署脚本自动做）

### 1. 端口冲突

检测原端口在新机是否被占用。若被占：

- **占用方是 Docker 容器** → 脚本会报出容器名，并给两个选择：
  - **A（推荐）** 让 musicweb 换个空闲端口，不动你的容器；
  - **B** 保留端口给 musicweb，脚本打印改容器端口的具体命令。
    Docker 端口无法在线修改，必须重建容器：compose 管理的改 `ports:` 后 `docker compose up -d`；独立容器按脚本给出的 `docker run` 模板重建（记得补齐原有的挂载/环境变量等参数）。
- **占用方是普通进程** → 自动推荐下一个空闲端口，也可手动指定。

端口变化会自动同步到 `config.ini` 的 `WebListenAddr` 和 nginx 的 `proxy_pass`，无需手改。

### 2. 反向代理 / 域名冲突

检查新机的 nginx（`sites-enabled/`、`conf.d/`）和 Caddy（`Caddyfile`）里是否已有同域名站点。**发现冲突会中止部署**（绝不覆盖你已有的站点配置），并提示三种解决办法：

1. **旧配置已废弃** → 删掉再重跑：
   ```bash
   # nginx
   rm /etc/nginx/sites-enabled/<冲突文件> && nginx -t && systemctl reload nginx
   # caddy：编辑 /etc/caddy/Caddyfile 删掉该站点块
   systemctl reload caddy
   ```
2. **旧配置还要用** → 给 musicweb 换域名：改 `config.ini` 的 `WebPublicBaseURL`，站点配置里的 `server_name` 一并改掉。
3. **想共存** → 用子路径挂载（需自行调整 nginx `location` / Caddy `handle_path`；musicweb 目前按根路径提供服务，改子路径要一并处理静态资源前缀）。

新机若装的是 **Caddy**，脚本不会擅自改 `Caddyfile`，而是打印现成的配置片段让你粘贴：

```caddyfile
music.example.com {
    reverse_proxy 127.0.0.1:<端口>
}
```

Caddy 会自动签发并续期证书，不需要 certbot。

### 3. 证书

优先复用备份里的 Let's Encrypt 证书（含 `archive/`，续期链完整）。没有证书且新机用 nginx 时，脚本会打印签发命令：

```bash
mkdir -p /var/www/certbot
certbot certonly --webroot -w /var/www/certbot -d <域名> \
  --agree-tos -m you@example.com --deploy-hook 'systemctl reload nginx'
```

## 常用参数

```bash
# 备份
./migrate-backup.sh --with-cache          # 连音频缓存一起打包
./migrate-backup.sh -o /tmp/mw.tar.gz     # 指定输出路径
APP_DIR=/srv/musicweb/app ./migrate-backup.sh   # 非默认安装路径

# 部署
./migrate-deploy.sh <包> --port 18081     # 直接指定端口，跳过交互
./migrate-deploy.sh <包> --yes            # 全用推荐值，无人值守
```

## 排查

```bash
systemctl status musicweb
journalctl -u musicweb -n 50 --no-pager
curl -fsS http://127.0.0.1:<端口>/api/v1/health
nginx -t                      # 反代配置是否合法
```

- **服务起不来**：多半是 `config.ini` 权限或路径不对。确认属主是服务用户、权限 600，且 `WorkingDirectory` 与 `APP_DIR` 一致。
- **页面白屏、`/assets/*` 404**：nginx 以 `www-data` 直接读前端文件，需要目录可遍历。脚本已处理；手工改动过就重跑 `chmod o+x /opt/musicweb /opt/musicweb/app && chmod -R o+rX /opt/musicweb/app/webui`。
- **登录不了**：`config.ini` 里的 `WebSessionSecret` 若为空或 `change-me`，服务每次启动会随机生成，登录态不跨重启。迁移会原样带过来，一般不受影响。
