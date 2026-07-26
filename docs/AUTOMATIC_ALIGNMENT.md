# AMLL Studio AI 自动打轴

MusicWeb 可以在歌词制作页面中调用独立 Python Worker，执行：

```text
项目音频 → Demucs 人声分离 → Qwen3 ForcedAligner → AMLL TTML 草稿
```

Worker 与 Go 服务分进程运行。模型崩溃或内存不足不会直接带崩 Web
服务，并且默认只允许一个任务并发。

## 资源要求

- Python 3.12；
- `ffmpeg`；
- 建议至少 4 GB 可用内存；
- 首次运行需要下载 Qwen/Demucs 模型，模型缓存还需要数 GB 磁盘空间；
- macOS Apple Silicon 可以使用 MPS，NVIDIA 主机可以使用 CUDA；无 GPU 时使用 CPU。

1 GB VPS 不应直接启用本功能。可以继续使用 Web/Studio，其自动打轴按钮会显示
“服务未启用”；自动打轴 Worker 应迁移到内存更大的主机后再开启。

## 安装 Worker

Actions 构建产物包含 `alignment-worker/`。在部署目录执行：

```bash
apt update
apt install -y ffmpeg python3 python3-venv

curl -LsSf https://astral.sh/uv/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"

uv venv --python 3.12 /opt/musicweb/alignment-venv
uv pip install \
  --python /opt/musicweb/alignment-venv/bin/python \
  -r /opt/musicweb/app/alignment-worker/requirements.txt

mkdir -p /var/lib/musicweb/alignment
mkdir -p /var/lib/musicweb/models/{huggingface,torch}
chown -R musicweb:musicweb /var/lib/musicweb/alignment /var/lib/musicweb/models
```

## 配置

在 `/etc/musicweb/config.ini` 的全局部分加入：

```ini
WebAlignmentEnabled = true
WebAlignmentPython = /opt/musicweb/alignment-venv/bin/python
WebAlignmentScript = /opt/musicweb/app/alignment-worker/worker.py
WebAlignmentWorkDir = /var/lib/musicweb/alignment
WebAlignmentMaxConcurrent = 1
```

确认 systemd 允许写入工作目录：

```ini
ReadWritePaths=/var/lib/musicweb /etc/musicweb /opt/musicweb/app/log
Environment=HF_HOME=/var/lib/musicweb/models/huggingface
Environment=TORCH_HOME=/var/lib/musicweb/models/torch
```

然后重启：

```bash
systemctl restart musicweb
curl -s http://127.0.0.1:8080/api/v1/studio/alignment
```

最后一条接口受管理员会话保护，浏览器中的 Studio 会自动读取能力状态。

## 使用

1. 从搜索结果点击“制作歌词”；
2. 等待音频与种子歌词自动导入；
3. 点击右上角“AI 自动打轴”；
4. 确认后等待“分离人声 → 逐字对齐 → 生成 TTML”；
5. 结果自动导入编辑器并进入原有双重自动保存；
6. 重点复核低置信度 token、主唱重叠和背景和声。
