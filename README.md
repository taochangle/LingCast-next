# LingCast Next

LingCast Next 将数字人、形象与 idle 素材、话术、单场直播管理和观众互动接入同一后台。Go 提供管理页面和 API，Python Worker 调用常驻 CosyVoice 与 MuseTalk。FFmpeg 发送内部直播流，经 SRS、Worker 和 Go 提供后台预览。Chrome 插件由运营人员主动启用并采集当前场次事件。

## 部署前准备

- 使用 Docker Engine 或 Docker Desktop，启用 BuildKit，安装 Docker Compose **2.30.0 或更高版本**。模型容器使用 Compose 的 [GPU 设备设置](https://docs.docker.com/reference/compose-file/services/#gpus)。
- Linux NVIDIA 部署使用 x86_64、宿主机 NVIDIA 驱动和 NVIDIA Container Toolkit，先按官方指引验证容器内 GPU 可用。[NVIDIA Container Toolkit 安装指引](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html)
- Windows NVIDIA 部署使用 WSL2 的 Linux 容器和 Docker Desktop WSL2 后端，更新 WSL 内核并在 **Windows 宿主机**安装 NVIDIA 驱动。建议把仓库、模型与参考音频放在 WSL 的 Linux 文件系统。先运行 Docker 官方 GPU 测试。[Docker Desktop Windows GPU 支持](https://docs.docker.com/desktop/features/gpu/)
- 构建机需要访问镜像仓库、系统软件包仓库、npm 和 Go 模块仓库。构建模型还需要 GitHub、PyPI、PyTorch 及 OpenMMLab 等依赖源。GPU 镜像的构建和真实推理仍待目标平台验收。详见 [验收记录](docs/acceptance.md)。
- 复制 `.env.example` 为 `.env` 并限制其访问权限。所有启用服务的令牌均需设置，示例值仅供配置检查。独立生成高熵令牌，例如运行 `openssl rand -hex 32`。两端的 Worker 令牌需一致，每条模型链路也需要一致的令牌。关闭 LLM 时让 `LLM_URL`、`LLM_MODEL` 留空，并给 `LLM_TOKEN` 填一个独立随机值。

`PUBLIC_BASE_URL` 是 Worker 与浏览器共同可访问的业务 HTTP(S) 源。Worker 会从该源下载带 `MEDIA_TOKEN` 的素材，浏览器也用它加载素材。应配置双方均可访问的 HTTPS 网关，或在本机受控网络验收时绑定 Docker 主机的私网地址，并将该地址和发布端口写入 `PUBLIC_BASE_URL`。容器里的 `localhost` 指容器自身。Compose 中的 `app` DNS 名仅供同一 Compose 网络使用。

应用、Worker 和模型端口默认只绑定到宿主机回环地址。需要跨主机时，通过有访问控制的 HTTPS 网关发布所需端点。通常保留默认绑定，由宿主机网关连接本地端口。需要直接绑定指定私网接口时，通过对应的 `*_BIND_ADDRESS` 和 `*_HOST_PORT` 显式设置。使用流式反向代理时为 FLV 关闭响应缓冲，为开播准备和预览流配置适当超时，并从访问日志中排除查询参数。Worker 和模型地址只填受信任的 HTTPS 源或同一 Compose 网络内的服务名。SRS 的 RTMP 和 HTTP 均留在内部网络。

## 四种启动方式

在仓库根目录执行所选命令。首次构建会拉取基础镜像和依赖。对私有网络的本机验收，可将后台地址设置为 `http://宿主机私网地址:8080`，并用同一地址打开浏览器。

| 拓扑 | 配置 |
| --- | --- |
| 业务独立 | `WORKER_URL` 指向 AI 机器的 HTTPS Worker 地址。确保 AI 机器能访问 `PUBLIC_BASE_URL`。 |
| AI 独立，模型同机 | `TTS_URL=http://tts:8091`、`LIPSYNC_URL=http://lipsync:8092`。业务侧通过 Worker 的 HTTPS 网关访问，AI 机器能访问业务素材源。 |
| 业务和 AI 同机，模型同机 | `WORKER_URL=http://worker:8090`，模型服务地址仍为上述内部 DNS。为 Worker 与浏览器配置共同可访问的素材源。 |
| 业务和 AI 同机，模型远程 | `WORKER_URL=http://worker:8090`。`TTS_URL` 和 `LIPSYNC_URL` 指向远程兼容模型服务的 HTTPS 地址。远程 MuseTalk 服务自身也需能访问已配置的 TTS 服务。 |

**业务独立**

```sh
docker compose -f compose.yaml up -d --build
```

**AI 独立，模型同机**

```sh
docker compose -f compose.ai.yaml --profile models up -d --build
```

**业务和 AI 同机，模型同机**

```sh
docker compose -f compose.yaml -f compose.ai.yaml --profile models up -d --build
```

**业务和 AI 同机，模型远程**

```sh
docker compose -f compose.yaml -f compose.ai.yaml up -d --build
```

AI 独立也可以连接远程模型。设置两个远程模型地址后，运行 `docker compose -f compose.ai.yaml up -d --build`。若只在本机部署其中一个模型，可显式指定要启动的 `worker tts` 或 `worker lipsync` 服务，并配置另一个服务的远程地址。各服务的 `readyz` 或健康接口反映模型实际就绪情况，容器首次加载时请等待就绪再开播。

## 模型和参考音频

1. `deploy/tts.Dockerfile` 和 `deploy/lipsync.Dockerfile` 在构建时获取固定 Git 提交。CosyVoice 使用 Python 3.10 和 CUDA 12.1。MuseTalk 使用 Python 3.10 和 CUDA 11.8。镜像启动时只读取本地权重。
2. 从 [CosyVoice3 官方仓库](https://huggingface.co/FunAudioLLM/Fun-CosyVoice3-0.5B-2512) 获取完整快照。选定实际提交并记录，使用 `hf download FunAudioLLM/Fun-CosyVoice3-0.5B-2512 --revision "$COSYVOICE_REVISION" --local-dir ./models/cosyvoice`。MuseTalk 根据 [固定提交的资源清单](https://github.com/TMElyralab/MuseTalk/blob/0a89dec45a0192b824e3cf4daf96c239440c5ed8/download_weights.sh) 准备完整权重树，补齐 [模型服务文档](docs/model-services.md) 列出的检测器等文件。记录实际权重版本、SHA256 和许可。下载由操作者显式完成。
3. 创建 `TTS_DATA_DIR` 和 `LIPSYNC_DATA_DIR`。在 TTS 数据目录中放入有权使用的参考音频及 `manifest.json`，填写对应音频逐字文本。MuseTalk 的数据目录需要可写，准备底片时会创建形象注册信息。权重目录和参考音频使用只读挂载。
4. 启动后通过 Bearer 鉴权检查 TTS 与 MuseTalk 的 `GET /readyz`、TTS 的 `GET /voices` 和 Worker 的 `GET /healthz`。在后台创建数字人时填写已配置的 `voiceId`。首次开播会准备形象的默认 idle，可能需要几分钟。也可在开播前按模型服务文档调用 MuseTalk `/prepare`。

Go 的 `app-data` 卷保存 SQLite 和上传文件，`musetalk-cache` 卷保存预处理数据，`worker-cache` 卷用于 Worker 下载临时文件。更新时沿用原 Compose 项目和卷，备份业务卷及模型数据。日常停机使用对应启动命令的同一组 `-f` 与 `--profile` 参数执行 `down`，保留卷。

## 操作与验收

用 `ADMIN_TOKEN` 登录后台。创建数字人，添加形象图和同尺寸、25 fps 的无声 idle 视频，选一个默认 idle，再选择平台开播。浏览器预览通过后台播放。选择平台不会启动转推。

插件安装与采集请见 [Chrome 插件说明](extensions/chrome/README.md)。使用当场的采集令牌，停播后令牌失效。

`./scripts/check-compose.sh` 验证部署组合配置，`./scripts/smoke-compose.sh` 启动隔离的 CPU Compose 联调。CPU 联调需要宿主机 Python 3、FFmpeg 和 Docker，使用合成推理响应、临时卷及本机回环端口。当前验收结果和目标硬件步骤见 [验收记录](docs/acceptance.md)。
