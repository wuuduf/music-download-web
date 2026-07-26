# MusicBot-Go

多平台音乐下载、在线播放与逐字歌词制作平台。两种运行形态共用同一套插件与配置：

- **Telegram Bot** —— 发链接或关键词即可下载音乐、歌词与封面，带缓存、限流和插件化多平台扩展。
- **Web 网站（musicweb）** —— 液态玻璃风格的搜索 / 下载 / 在线播放页面，内置 AMLL 逐字歌词工作台（跨平台 ID 匹配、AI 打轴、人声分轨），以及 iOS 快捷指令解析 API。

> 基于 [XiaoMengXinX/Music163bot-Go](https://github.com/XiaoMengXinX/Music163bot-Go) 重构，改为插件化架构以支持多平台。许可证 GPL-3.0。

---

## 目录

- [支持平台](#支持平台)
- [两种运行模式](#两种运行模式)
- [快速开始 · Telegram Bot](#快速开始--telegram-bot)
- [快速开始 · Web 网站](#快速开始--web-网站)
- [配置](#配置)
- [安全（Web 模式必读）](#安全web-模式必读)
- [生产部署 · Web](#生产部署--web)
- [使用 · Bot 命令](#使用--bot-命令)
- [使用 · Web 网站](#使用--web-网站)
- [使用 · AMLL 歌词工作台](#使用--amll-歌词工作台)
- [使用 · iOS 快捷指令 API](#使用--ios-快捷指令-api)
- [Apple Music 无损（Hi-Res/Atmos）](#apple-music-无损hi-resatmos)
- [可选进阶 · AI 打轴与人声分轨](#可选进阶--ai-打轴与人声分轨)
- [插件开发](#插件开发)
- [文档与架构](#文档与架构)

---

## 支持平台

| 平台 | 下载 | 搜索 | 歌词 | Hi-Res / 无损 | 识曲 |
|------|:--:|:--:|:--:|:--:|:--:|
| 网易云音乐 | ✓ | ✓ | ✓ | ✓ | ✓ |
| QQ 音乐 | ✓ | ✓ | ✓ | ✓ | — |
| 酷狗音乐 | ✓ | ✓ | ✓ | ✓ | — |
| 汽水音乐 | ✓ | ✓ | ✓ | ✓ | — |
| 哔哩哔哩 | ✓ | ✓ | ✓ | — | — |
| Apple Music | ✓ | ✓ | ✓ | ✓ ¹ | — |
| Spotify | ✓ ² | ✓ | ✓ | ✓ ² | — |
| YouTube Music | ✓ | ✓ | ✓ | — | — |

¹ Apple Music 的 AAC 256k 开箱即用；无损 / Hi-Res / Atmos 需额外的解密服务，见 [Apple Music 无损](#apple-music-无损hi-resatmos)。
² Spotify 搜索与元数据仅需 Client Credentials；原生音频解密需自备 `sp_dc` 与 Widevine 设备（`.wvd`），在 `/admin` 中配置。

---

## 两种运行模式

两个入口，**同一份 `config.ini`、同一套平台插件与账号凭据**：

| | 二进制 | 入口 | 需要 `BOT_TOKEN` | 说明 |
|---|---|---|---|---|
| Telegram Bot | `MusicBot-Go` | `main.go` | 是 | 聊天里下载 / 搜索 / 识曲 |
| Web 网站 | `musicweb` | `cmd/musicweb` | 否 | 浏览器搜索、在线播放、歌词工作台、快捷指令 API |

可以只跑其中之一，也可以两个一起跑（共享 `config.ini`，各平台账号只需配一次）。Bot 用 Docker 镜像最省事；Web 需要额外构建前端资源，推荐 systemd + nginx 部署（见下）。

---

## 快速开始 · Telegram Bot

### Docker（推荐）

镜像由 CI 自动构建并推送到 GHCR：

- `ghcr.io/liuran001/musicbot-go:latest` —— 含精简版 ffmpeg（仅运行时所需共享库），支持 `/recognize` 听歌识曲。识曲指纹编码已用纯 Go（wazero + afp.wasm）实现，无需 Node.js。

所有运行数据（配置、数据库、缓存、脚本）放在一个挂载目录里：

```bash
mkdir -p docker-data
cp config_example.ini docker-data/config.ini
# 编辑 docker-data/config.ini，至少填 BOT_TOKEN

docker run -d --name musicbot-go --restart unless-stopped \
  -w /app/workdir -v "$(pwd)/docker-data:/app/workdir" \
  -e TZ=Asia/Shanghai \
  ghcr.io/liuran001/musicbot-go:latest -c /app/workdir/config.ini
```

或用仓库自带的 `docker-compose.yml`（本地构建）：

```bash
docker compose up -d --build
```

> 不需要识曲时，建议在配置里显式 `EnableRecognize = false`。

### 裸机运行

需要 Go 1.26+；用 `/recognize` 还需 ffmpeg（识曲指纹编码已用纯 Go 实现，无需 Node.js）。

```bash
go build -o MusicBot-Go .      # 或 ./build.sh，会注入版本号
./MusicBot-Go -c config.ini
```

---

## 快速开始 · Web 网站

Web 模式没有官方镜像，需要构建前端资源 + `musicweb` 二进制。前置：**Go 1.26+**、**Node.js ≥ 20.19（或 ≥ 22.12）**、**pnpm 11**。

```bash
# 1. 构建前端（产出 webui/dist/{site,studio,editor}）
cd webui
pnpm install
pnpm build:all          # = pnpm build && pnpm build:studio && pnpm build:editor
cd ..

# 2. 构建 Web 服务端
go build -o musicweb ./cmd/musicweb

# 3. 准备配置
cp config_example.ini config.ini
# 至少设置 WebAdminPassword 或 WebAdminPasswordHash（否则管理后台无法登录，见「安全」）
# Web 模式不需要 BOT_TOKEN；平台账号（网易云 music_u、QQ cookie 等）配置方式与 Bot 相同

# 4. 运行
./musicweb -c config.ini
```

默认监听 `127.0.0.1:8080`（`WebListenAddr`）。打开：

| 路径 | 页面 |
|---|---|
| `/` | 公开搜索 / 下载 / 在线播放页 |
| `/player/<session>` | AMLL 在线播放器（在线播放时自动生成） |
| `/admin` | 管理后台（平台账号、API Key、歌词索引、下载历史） |
| `/studio/<projectID>` | AMLL TTML Tool 歌词工作台（需登录） |
| `/studio-editor/<projectID>` | AMLL Editor 新版编辑器（需登录） |
| `/api/v1/health` | 健康检查（运行时长、平台数） |

> 只想快速试玩：`WebListenAddr` 保持 `127.0.0.1:8080` 仅本机可访问；对外提供服务请务必按 [生产部署](#生产部署--web) 配 HTTPS 反向代理。

---

## 配置

复制 `config_example.ini` 为 `config.ini`，按注释填写。`config_example.ini` 里每一项都有中文说明，这里只列关键项。

### Bot 最小配置

```ini
BOT_TOKEN = YOUR_BOT_TOKEN   # Bot 模式必填；Web 模式无需
BotAdmin  = 123456789        # 管理员 Telegram ID（逗号分隔），管理命令需要
```

### 平台账号（Bot / Web 共用）

各平台凭证写在对应的 `[plugins.<name>]` 段：

```ini
[plugins.netease]
music_u = YOUR_MUSIC_U_COOKIE      # 网易云无损需要

[plugins.qqmusic]
cookie = YOUR_QQMUSIC_COOKIE       # 高音质 / Hi-Res 需要

[plugins.applemusic]
media_user_token = YOUR_TOKEN      # 登录 music.apple.com 后从浏览器 Cookie 复制

[plugins.spotify]
client_id = ...                    # 搜索 / 元数据
client_secret = ...
# sp_dc 与 wvd_path 用于原生音频解密，建议在 /admin 里配置
```

> 多数平台账号也可以不写进配置：Bot 用管理员命令 `/login <平台> cookie <cookie>`，Web 用 `/admin` 页面在运行时导入（都会回写 `config.ini`）。

### Web 模式关键项

```ini
WebListenAddr        = 127.0.0.1:8080   # 监听地址（生产环境交给反向代理）
WebPublicBaseURL     = https://music.example.com   # 对外地址，用于 API 返回完整链接
WebTrustProxyHeaders = false            # 仅在可信反向代理之后才设 true（见「安全」）
WebDownloadCacheDir  = ./cache/web      # 下载文件缓存
WebCredentialDir     = ./data/credentials  # 管理后台上传的凭据（WVD 等）
WebDownloadTTLHours  = 24               # 下载文件保留时长
WebMaxConcurrentDownloads = 4           # 下载并发
WebStaticDir         = ./webui/dist/site
WebStudioStaticDir   = ./webui/dist/studio
WebEditorStaticDir   = ./webui/dist/editor
```

完整选项（并发、缓存、限流、代理、日志、AMLL 歌词库、AI 打轴 / 分轨等）见 `config_example.ini` 注释。

---

## 安全（Web 模式必读）

Web 模式对外暴露管理后台，请按下面几点加固：

1. **必须设置管理员密码。** `WebAdminPassword` 和 `WebAdminPasswordHash` 都为空时，管理后台登录一律失败（无默认口令），服务启动会打印告警。生产环境请用 bcrypt hash：

   ```bash
   # 生成 bcrypt hash 填入 WebAdminPasswordHash（优先于明文 WebAdminPassword）
   htpasswd -bnBC 12 "" '你的密码' | tr -d ':\n'
   ```

2. **Session 密钥。** `WebSessionSecret` 留空或保持 `change-me` 时，服务每次启动生成随机密钥（更安全，但重启后登录态失效）。想让登录态跨重启保留，就设一个随机长字符串。

3. **反向代理头信任。** `WebTrustProxyHeaders` 默认 `false`。**只有**当服务确实部署在可信反向代理（nginx / Caddy）之后时才设 `true`——否则任何客户端都能伪造 `X-Forwarded-For` / `X-Forwarded-Proto` 绕过按 IP 的限流、并影响 https 链接生成。直接对外暴露端口时保持 `false`。

4. **内置防护（无需配置，已默认开启）：**
   - 管理后台与所有 cookie 鉴权接口（`/admin`、`/api/v1/studio/*`、`/api/v1/admin/*`）带 CSRF 同源校验，跨站写请求返回 403；
   - 管理员登录限流 10 次/分钟/IP，搜索 / 解析 / 下载 / 歌词 / 图片代理各有独立限流桶；
   - 管理员 cookie 为 `HttpOnly` + `SameSite=Lax`，走 HTTPS 时自动加 `Secure`；
   - 图片代理有 host 白名单，日志会脱敏平台下载 URL（去掉 vkey 等临时凭据）。

5. **务必走 HTTPS。** 管理员登录、Studio 编辑都通过 cookie 鉴权，明文 HTTP 会泄露 session。用下面的 nginx 配置终结 TLS。

---

## 生产部署 · Web

仓库 `deploy/` 提供了开箱即用的 systemd + nginx 配置。

### systemd

`deploy/musicweb.service` 以专用用户运行，带资源与文件系统隔离（`NoNewPrivileges`、`ProtectSystem`、`GOMEMLIMIT=700MiB`，适合小内存 VPS）：

```bash
# 假设二进制与前端资源在 /opt/musicweb/app/
sudo useradd -r -s /usr/sbin/nologin musicweb
sudo cp deploy/musicweb.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now musicweb
sudo journalctl -u musicweb -f      # 看日志
```

目录约定（可在 service 文件里改）：二进制 `musicweb`、`config.ini`、`webui/dist/` 放 `/opt/musicweb/app/`，可写目录为 `cache/`、`data/`、`logs/`。

### nginx 反向代理 + HTTPS

`deploy/nginx.musicweb.conf` 已处理好：80→443 跳转、TLS、Vite 哈希资源长缓存（不占用 Go worker）、Studio 的 COOP/COEP 头、以及长连接超时（SSE 下载进度）。改掉 `server_name` 和证书路径即可：

```bash
sudo cp deploy/nginx.musicweb.conf /etc/nginx/sites-available/musicweb.conf
sudo ln -s /etc/nginx/sites-available/musicweb.conf /etc/nginx/sites-enabled/
# 用 certbot 签发 music.example.com 证书后
sudo nginx -t && sudo systemctl reload nginx
```

在反代之后，记得 `config.ini` 里设 `WebTrustProxyHeaders = true` 和 `WebPublicBaseURL = https://你的域名`。

### 定时备份（可选）

`deploy/musicweb-backup.{service,timer}` + `deploy/backup.sh` 提供数据库定时备份，按需启用：

```bash
sudo cp deploy/musicweb-backup.* /etc/systemd/system/
sudo systemctl enable --now musicweb-backup.timer
```

---

## 使用 · Bot 命令

**通用命令**

| 命令 | 说明 |
|------|------|
| `/music <URL 或关键词>` | 下载音乐；直接发音乐链接也会自动识别下载 |
| `/search <关键词>` | 搜索并选择下载 |
| `/lyric <URL>` | 获取歌词 |
| `/recognize` | 回复一条语音消息识别歌曲（需 `EnableRecognize`） |
| `/settings` | 设置默认平台与音质（支持私聊 / 群聊维度） |
| `/status` | 查看统计与各平台账号状态 |
| `/queue` | 查看当前下载、发送和 Telegram API 队列 |
| `/about` · `/help` | 关于 / 帮助 |

也支持 Inline 模式（`@bot 关键词`）和直接粘贴链接。

**管理员命令**（需在 `BotAdmin` 中）

| 命令 | 说明 |
|------|------|
| `/login <平台> cookie <cookie>` | 导入平台 Cookie |
| `/login kugou qr` | 扫码登录酷狗概念版 |
| `/login <平台> check` · `/login check` | 检查单个 / 全部平台账号 |
| `/login <平台> renew` · `/login renew` | 手动续期 |
| `/login <平台> auto on\|off\|status [秒]` | 自动续期开关 |
| `/login applemusic lang [语言]` | 查看 / 设置 Apple Music 元数据语言 |
| `/reload` | 重载配置与动态脚本插件 |
| `/rmcache <平台>\|all` | 清除缓存 |
| `/wl add\|del\|list [chatID]` | 白名单管理（需 `EnableWhitelist = true`） |

---

## 使用 · Web 网站

打开首页 `/`：

- **搜索 / 解析二合一。** 输入关键词按所选平台搜索；直接粘贴歌曲链接会自动识别平台并解析成单曲。
- **每首歌一排操作：** 选音质 → **在线播放**（跳到 AMLL 逐字歌词播放器）/ **下载**（后端排队，卡片实时显示进度，完成后可下文件）/ **制作歌词**（进 Studio）。
- **歌词下载：** 点「歌词」展开面板，选格式（LRC / YRC / QRC / LYS / TTML / ASS / SRT / Apple JSON 等 15 种），可勾翻译、罗马音，一键下载。

在线播放页 `/player/<session>` 是仿 Apple Music 的逐字歌词播放器（纯 CSS 背景，AMLL 逐字滚动，可切翻译 / 罗马音）。

管理后台 `/admin`：平台账号状态与登录（Cookie 导入 / 扫码 / 续期 / 签到 / Spotify 设置 / WVD 上传）、快捷指令 API Key 管理、AMLL 歌词库同步、下载历史与清理。

---

## 使用 · AMLL 歌词工作台

在首页对任意歌曲点「制作歌词」创建一个 Studio 项目，会自动导入音频、逐字歌词种子和跨平台元数据。项目可在两个编辑器间随时切换：

- **AMLL TTML Tool**（`/studio/<id>`）—— 功能完整，适合 TTML 元数据、审核与传统制作流程。
- **AMLL Editor**（`/studio-editor/<id>`）—— 新版 Vue 编辑器，适合内容编辑与打轴。

工作台顶栏的几个面板（均需管理员登录）：

- **平台 ID** —— 跨平台 ID 匹配。自动匹配网易云 / QQ / Spotify / Apple Music 的歌曲 ID（ISRC 优先、按标题/歌手/专辑/时长打分），未达自动阈值的可手动「搜索候选」逐个确认，一键写入 TTML 元数据（`ncmMusicId` / `qqMusicId` / `spotifyId` / `appleMusicId`）。
- **AI 六轨分离** —— 用 Demucs 把音频分成人声 / 鼓 / 贝斯 / 其他等轨（需 Python worker，见下）。
- **AI 自动打轴** —— 用当前歌词文本 + 项目音频生成逐字时间轴草稿（需 Python worker，见下）。

编辑成果会乐观保存到服务端（IndexedDB 本地自动保存仍然生效）。

---

## 使用 · iOS 快捷指令 API

Web 模式内置为 iOS「快捷指令」设计的稳定解析端点，可在手机上一键解析链接并下载音乐 / 歌词 / 封面。

1. 登录 `/admin`，在「快捷指令 API Keys」生成密钥（可设总解析次数或无限，明文只显示一次，库里只存 SHA-256）。
2. 请求：

   ```http
   POST /api/v1/shortcut/resolve
   Authorization: Bearer <你的 API Key>
   Content-Type: application/json

   { "input": "<歌曲链接或分享文本>", "action": "download", "quality": "hires", "files": 3 }
   ```

   接受复制的分享文本（不必是纯 URL），可选同请求内发起下载并返回音乐 / 歌词 / 封面的下载地址。

仓库 `shortcuts/` 提供了现成的快捷指令源码（`.cherri`）。完整参数、音质、限流与响应格式见 [`docs/SHORTCUT_API.md`](docs/SHORTCUT_API.md)。

---

## Apple Music 无损（Hi-Res/Atmos）

Apple Music 的解密分两档：

- **AAC 256k —— 开箱即用。** 插件内置原生 Go Widevine 解密，填好 `media_user_token` 即可，无需任何额外服务。
- **无损 ALAC / Hi-Res 24bit / Dolby Atmos —— 需要外部 wrapper。** 这些音质走 FairPlay，Apple 不对 Widevine 放行，必须经
  [WorldObservationLog/wrapper](https://github.com/WorldObservationLog/wrapper) 解密。请求高于 AAC 的音质时若 wrapper 不可用，会自动回退到 AAC 256k。

启用无损（Docker）：

1. **构建 wrapper 镜像。** 上游不发布镜像，仓库提供了手动工作流：进入 GitHub → Actions → **Build Apple Music Wrapper Image** → Run，它会从上游 Release 取预编译二进制打包并推到 `ghcr.io/<你的用户名>/musicbot-wrapper`（仅 x86_64）。
2. **登录 wrapper。** 在 `docker-compose.yml` 的 `wrapper` 服务里填一个**有订阅的 Apple ID**（`USERNAME` / `PASSWORD`）。它模拟安卓客户端，登录是设备级的，**无法复用 bot 的 `media_user_token`**——两套独立凭证，都要有。首次启动会自动登录（含 2FA），会话持久化到挂载卷，之后可清空账密。
3. **指向 wrapper。** 在 `config.ini` 设 `wrapper_host = wrapper`（compose 服务名），`docker compose up -d`。

> 2FA：首次启动后看 wrapper 日志，出现 `Waiting for input...` 时把收到的 6 位验证码写入挂载目录的 `data/com.apple.android.music/files/2fa.txt`（60 秒内）。
>
> **裸机**：自行运行 wrapper（见其仓库），把 `wrapper_host` 指向它的地址（如 `127.0.0.1`，端口 10020/20020/30020）。

---

## 可选进阶 · AI 打轴与人声分轨

Studio 的「AI 自动打轴」和「AI 六轨分离」依赖独立的 Python worker，默认关闭（`WebAlignmentEnabled = false`、`WebStemEnabled = false`）。它们需要 Python、Demucs、对齐模型和 ffmpeg，**不适合 1 GB 内存的小 VPS**——建议至少 4 GB 内存，或放到 Apple Silicon 本机 / 独立 GPU worker 上。

- 人声分轨（Demucs 六轨 + 可选 Mel-Band RoFormer 精修）：见 [`docs/STEM_SEPARATION.md`](docs/STEM_SEPARATION.md)。
- 自动逐字打轴（强制对齐）：见 [`docs/AUTOMATIC_ALIGNMENT.md`](docs/AUTOMATIC_ALIGNMENT.md)。

相关 `config.ini` 项（venv 路径、并发、模型名、设备、TTL）在 `config_example.ini` 的 Web 段有完整注释。

---

## 插件开发

两种方式：

- **动态脚本插件**（无需重新编译）：源码放 `plugins/scripts/<name>/`，在 `config.ini` 加 `[plugins.<name>]` 段，管理员 `/reload` 即可热加载。最小入口见 [`plugins/scripts/README.md`](plugins/scripts/README.md)。
- **静态插件**（编译进二进制，能力最全）：实现 `platform.Platform` 接口并注册，见 [`plugins/README.md`](plugins/README.md)。

---

## 文档与架构

| 文档 | 内容 |
|---|---|
| [`ARCHITECTURE.md`](ARCHITECTURE.md) | 整体架构设计 |
| [`docs/SHORTCUT_API.md`](docs/SHORTCUT_API.md) | iOS 快捷指令解析 API 完整参数 |
| [`docs/AMLL_API_CONTRACT.md`](docs/AMLL_API_CONTRACT.md) · [`docs/AMLL_IMPLEMENTATION.md`](docs/AMLL_IMPLEMENTATION.md) | AMLL 歌词工作台 API 契约与实现 |
| [`docs/STEM_SEPARATION.md`](docs/STEM_SEPARATION.md) | 人声分轨 worker 部署 |
| [`docs/AUTOMATIC_ALIGNMENT.md`](docs/AUTOMATIC_ALIGNMENT.md) | 自动打轴 worker 部署 |

许可证：GPL-3.0。
