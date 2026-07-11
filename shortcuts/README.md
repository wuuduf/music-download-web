# Jelly Music DL 快捷指令

可直接安装的已签名文件：

```text
dist/JellyMusicDL.shortcut
```

逐节点弹窗并保留原始 HTTP 响应的排错版本：

```text
dist/JellyMusicDL-Debug.shortcut
```

## 安装

1. 将 `.shortcut` 文件发送到 iPhone（AirDrop、iCloud Drive 或“文件”App）。
2. 在 iPhone 点击文件并选择“添加快捷指令”。
3. 导入时粘贴在 MusicWeb `/admin` 生成的 API Key。
4. 复制音乐分享链接后运行“Jelly Music DL”，也可从分享菜单直接运行。

快捷指令依次询问音质和文件模式，并保存到：

```text
iCloud Drive/Shortcuts/jellymusicdl/<平台名称>/
```

当前版本使用单次 `POST /api/v1/shortcut/bundle` 请求。快捷指令以表单提交
`files=1|2|3`，服务端负责转换为整数、解析链接、等待下载并返回 ZIP，因此不再
依赖快捷指令解析嵌套 JSON 或轮询 `statusURL`。

使用此版本前，MusicWeb 服务端也必须升级到包含 `shortcut/bundle` 接口的版本。

## 源码和复现

源码为 `JellyMusicDL.cherri`，使用 Cherri v2.3.0、commit
`68f3f3feaf00768f7943f650b2230605355936ed` 编译，然后通过 macOS
`shortcuts sign --mode anyone` 兼容的流程签名。

```bash
cherri JellyMusicDL.cherri \
  --share=anyone \
  --output=dist/JellyMusicDL.shortcut
```

源码和签名文件都不包含真实 API Key；Key 仅在用户导入时填写。
