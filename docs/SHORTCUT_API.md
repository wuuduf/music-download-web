# iOS 快捷指令音乐链接解析 API

## Endpoint

```text
POST /api/v1/shortcut/resolve
```

认证请求头二选一：

```http
Authorization: Bearer <WebShortcutAPIKey>
X-API-Key: <WebShortcutAPIKey>
```

API Key 不允许放在查询参数中，避免被 Nginx 和浏览器历史记录保存。

## 只解析歌曲

```json
{
  "input": "从 App 分享出来的任意文字和音乐链接"
}
```

`input` 可以是裸链接，也可以是包含标题、描述和链接的完整分享文本。服务会提取其中的第一个受支持音乐链接，并返回唯一歌曲，不进行模糊搜索。

## 解析并准备下载

```json
{
  "input": "https://music.example/song/123",
  "action": "download",
  "quality": "high",
  "wait_seconds": 15
}
```

- `quality`: `standard`、`high`、`lossless`、`hires`
- `wait_seconds`: `0` 到 `25`；默认 `15`
- 也可用 `prepare_download: true` 代替 `action: download`

如果等待时间内下载完成，响应中的 `download.status` 为 `ready`，快捷指令可直接获取 `links.file`；否则会返回 HTTP `202`，随后轮询 `links.status`。

## 响应示例

```json
{
  "ok": true,
  "version": "v1",
  "track": {
    "track_id": "123",
    "platform": "netease",
    "title": "歌曲名称",
    "artists": ["歌手"],
    "album": "专辑",
    "cover_url": "https://...",
    "qualities": [{"value": "high", "label": "高音质"}]
  },
  "download": {
    "job_id": "...",
    "status": "ready",
    "file_name": "歌曲名称.flac"
  },
  "links": {
    "website": "https://music.example.com/",
    "lyrics": "https://music.example.com/api/v1/lyrics/netease/123?format=ttml&translation=1&roma=1",
    "status": "https://music.example.com/api/v1/downloads/...",
    "events": "https://music.example.com/api/v1/downloads/.../events",
    "file": "https://music.example.com/api/v1/downloads/.../file"
  }
}
```

## iOS 快捷指令流程

1. 快捷指令接收类型选择“URL、文本、Safari 网页”，并启用共享表单。
2. 如果没有共享输入，使用“获取剪贴板”。
3. 添加“获取 URL 内容”：
   - URL：`https://你的域名/api/v1/shortcut/resolve`
   - 方法：`POST`
   - 请求体：JSON
   - `input`：快捷指令输入或剪贴板
   - `action`：`download`
   - `quality`：`high`
   - `wait_seconds`：`15`
   - Header `Authorization`：`Bearer 你的API密钥`
4. 从返回字典读取 `download.status`。
5. `ready` 时获取 `links.file`，再使用“存储文件”或“共享”。
6. 非 `ready` 时等待 2 秒并读取 `links.status`，直到 `ready` 或 `failed`。

## 服务端配置

```ini
WebPublicBaseURL = https://music.example.com
WebShortcutAPIKey = 至少32字节的随机密钥
WebShortcutRateLimitPerMinute = 30
```

生成随机密钥：

```bash
openssl rand -hex 32
```
