# iOS 快捷指令音乐链接解析 API

## 管理 API Key

登录 `/admin`，在“快捷指令 API Keys”中生成密钥。每个密钥可以独立设置：

- 名称；
- 总解析次数，默认 100；
- 无限次数；
- 启用/停用；
- 使用次数清零或删除。

密钥明文只在创建成功时显示一次，数据库只保存 SHA-256 摘要。旧配置项 `WebShortcutAPIKey` 仅用于向后兼容，不计次数。

## Endpoint 与认证

```text
POST /api/v1/shortcut/resolve
```

```http
Authorization: Bearer <admin生成的API Key>
```

也支持 `X-API-Key`。密钥不允许放在 URL 查询参数中。

## 解析并准备文件

```json
{
  "input": "从 App 复制或分享出来的文字及音乐链接",
  "action": "download",
  "quality": "hires",
  "files": 3,
  "wait_seconds": 15
}
```

- `quality`: `standard`、`high`、`lossless`、`hires`；省略时默认 `hires`（平台实际最高可用音质）。
- `files=1`: 仅音乐，音频内嵌封面和歌词。
- `files=2`: 音乐 + 独立 LRC 歌词。
- `files=3`: 音乐 + LRC 歌词 + 尽可能大的独立专辑封面；默认值。
- `wait_seconds`: 0 到 25，默认 15。

解析成功才扣除一次额度；获取任务状态和文件不重复扣除。

## 响应

```json
{
  "ok": true,
  "version": "v1",
  "directory": "网易云音乐",
  "track": {
    "platform": "netease",
    "track_id": "123",
    "title": "歌曲名称",
    "artists": ["歌手"],
    "album": "专辑名称"
  },
  "download": {
    "job_id": "web_xxx",
    "status": "ready"
  },
  "assets": [
    {
      "kind": "music",
      "file_name": "歌手-歌曲名称-网易云音乐.flac",
      "url": "https://music.example.com/api/v1/shortcut/assets/web_xxx/music"
    },
    {
      "kind": "lyrics",
      "file_name": "歌手-歌曲名称-网易云音乐.lrc",
      "url": "https://music.example.com/api/v1/shortcut/assets/web_xxx/lyrics"
    },
    {
      "kind": "cover",
      "file_name": "歌手-专辑名称-网易云音乐.jpg",
      "url": "https://music.example.com/api/v1/media/image?..."
    }
  ],
  "quota": {
    "key_id": "...",
    "used": 1,
    "limit": 100,
    "remaining": 99,
    "unlimited": false
  }
}
```

音乐、歌词下载 URL 同样需要携带 `Authorization`。封面 URL 只代理受信任的音乐图片域名。

## 快捷指令流程

1. 接收共享表单中的 URL、文本或 Safari 网页；没有共享输入时读取剪贴板。
2. “从菜单中选择”音质：最高音质（默认）、无损、高音质、标准；映射为 `hires/lossless/high/standard`。
3. 第二个菜单选择文件：1 仅音乐、2 音乐和歌词、3 音乐/歌词/封面（默认）。
4. POST JSON 到 `/api/v1/shortcut/resolve`，Header 加 `Authorization: Bearer API_KEY`。
5. 若 `download.status` 不是 `ready`，每 2 秒读取 `links.status`，直到 `ready` 或 `failed`。
6. 遍历 `assets`，对每个 URL 使用相同 Authorization Header 下载。
7. 在 iCloud Drive 或“我的 iPhone”中创建 `jellymusicdl/<directory>/`，按 `file_name` 保存。

服务端只返回目录名和规范文件名；`/jellymusicdl/` 是快捷指令设备端目录，不是 VPS 目录。

## 服务端配置

```ini
WebPublicBaseURL = https://music.example.com
WebShortcutRateLimitPerMinute = 30
```

API Key 直接在 `/admin` 生成，不再需要手动运行 `openssl rand`。
