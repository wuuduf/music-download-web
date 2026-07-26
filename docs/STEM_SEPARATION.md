# Studio 多阶段分轨模块

该模块为 AMLL Studio 项目音频提供可试听、可下载的分轨文件。Go 服务只负责权限、任务状态与 Range 文件输出，Torch 和模型全部隔离在 Python Worker 中。

## 流程

```text
项目缓存音频
  └─ htdemucs_6s
      ├─ vocals ─ Mel-Band RoFormer ─ karaoke preset ─ lead / backing
      ├─ drums
      ├─ bass
      ├─ guitar ─ 可选专用模型
      ├─ piano  ─ 可选专用模型
      └─ other
```

第一阶段六轨是稳定契约。第二阶段任何模型缺失或执行失败都只产生 warning，并保留可用的 Demucs 基础轨。

## 安装

建议在 Apple Silicon Mac、独立 GPU 节点或至少 8 GB 内存的机器上安装。不要在当前 1 GB VPS 上执行模型推理。

```bash
cd /opt/musicweb/app
python3 -m venv /opt/musicweb/stem-venv
/opt/musicweb/stem-venv/bin/python -m pip install -U pip
/opt/musicweb/stem-venv/bin/python -m pip install -r stem-worker/requirements.txt
```

首次执行会下载模型。生产环境应预热模型缓存，并确保运行 `musicweb` 的系统用户可以读取模型缓存、写入 `WebStemWorkDir`。

## 配置

基础六轨：

```ini
WebStemEnabled = true
WebStemPython = /opt/musicweb/stem-venv/bin/python
WebStemScript = /opt/musicweb/app/stem-worker/worker.py
WebStemWorkDir = /var/lib/musicweb/stems
WebStemMaxConcurrent = 1
WebStemTTLHours = 24
WebStemDemucsModel = htdemucs_6s
WebStemDevice = auto
WebStemShifts = 1
WebStemOverlap = 0.25
```

第二阶段精修：

```ini
WebStemRefinementEnabled = true
WebStemSeparatorBinary = /opt/musicweb/stem-venv/bin/audio-separator
WebStemVocalModel = model_mel_band_roformer_ep_3005_sdr_11.4360.ckpt
WebStemBackingPreset = karaoke
WebStemPianoModel =
WebStemGuitarModel =
```

钢琴和吉他模型名称不写死在代码中。确认模型输出包含目标 stem 后再填入对应 `audio-separator` 模型文件名；留空时使用 `htdemucs_6s` 结果。

## API

接口均要求管理员会话：

- `GET /api/v1/studio/stems`：运行能力及精修配置。
- `POST /api/v1/studio/projects/{project_id}/stems`：创建任务，请求体为 `{"refine":true}`。
- `GET /api/v1/studio/projects/{project_id}/stems/{job_id}`：任务状态与轨道清单。
- `GET /api/v1/studio/projects/{project_id}/stems/{job_id}/assets/{track_id}`：支持 Range 的 WAV 音频。

## 输出与稳定性

- 每个任务独立目录，Worker 以原子方式写 `progress.json` 和 `manifest.json`。
- 清单路径经过 Go 服务校验，客户端不能读取任务目录外的文件。
- 同一 Studio 项目同一时间只运行一个任务。
- 默认最多一个模型任务，避免内存和显存争抢。
- 同一文件系统优先使用硬链接，避免 Demucs 临时文件和交付文件重复占用空间；过期任务按 `WebStemTTLHours` 清理。
- 网页播放器使用 `preload="none"`，打开弹窗不会一次加载所有 WAV。

模型及其权重可能有单独许可证，部署或公开服务前应逐一核对模型许可证与使用条件。
