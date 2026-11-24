# LingCast Next 验收

## 本地复现

以下命令从仓库根目录执行，模块检查使用各自锁文件安装依赖。业务媒体验证需要 `ffmpeg` 与 `ffprobe`。

```sh
(cd backend && go test -race ./... && go vet ./...)
(cd frontend && npm ci && npm test -- --run && npm run build)
python3 -m pip install -r worker/requirements.txt
python3 -m unittest discover -s worker/tests -v
python3 -m unittest discover -s tests/integration -p test_preview_streaming.py -v
python3 -m compileall -q worker services tests/integration
(cd extensions/chrome && npm ci && npm test)
./scripts/check-compose.sh
./scripts/smoke-compose.sh
```

建议在 Python 3.12 虚拟环境中安装 Worker 测试依赖。Smoke 脚本使用独立 Compose 项目、随机回环端口、合成素材和临时卷。其 TTS 和渲染端点只提供协议夹具，包括校验准备底片、返回合成 JPEG 帧和无声 PCM。业务 API、素材下载、Worker、FFmpeg 和私有 SRS 走实际容器与网络链路。测试使用模拟观众事件，且始终关闭转推。

## 2026-11-24 本地验收结果

| 检查 | 结果 |
| --- | --- |
| Go、React、Worker 和插件 | Go race 与 vet、React 测试及生产构建、Python 测试、Chrome 插件测试均通过。Worker 测试在带 OpenCV 和 FFmpeg 的 Linux 容器运行，包含实际 FFmpeg 小帧输出与进程清理。 |
| Compose 配置 | 业务独立、AI 独立使用同机模型或远程模型、合并部署使用同机模型或远程模型，共五种配置通过校验。 |
| 业务独立运行 | Linux ARM64 上构建并启动 Go 与 React 镜像，健康接口和后台 HTML 正常，容器含 FFprobe。重建业务容器后，同一卷中的数字人记录和素材内容仍可读取。 |
| AI 独立运行 | Linux ARM64 上启动真实 Worker 和私有 SRS，模型端点使用合成夹具。带 Worker 令牌的健康接口返回就绪，无令牌请求返回 401。 |
| 合并运行 | 真实 Go、Worker、FFmpeg 和 SRS 验证了签名素材、开播互斥、鉴权 FLV 预览、手动口播、事件平台隔离与去重确认、长文本原样分段、停播及旧请求拒绝。推理响应来自合成夹具。 |
| 低码率预览 | Worker 流式回归测试收到 3 字节 FLV 数据时，上游仍保持连接。实际合并联调使用低码率合成 idle，经带浏览器查询令牌的 Go 预览路由收到 FLV 头。 |

## 目标环境待验收

| 检查项 | 实地操作与需记录的证据 |
| --- | --- |
| Linux x86_64 NVIDIA | 记录驱动、显卡、显存、Docker/Compose 版本、镜像 ID、上游源提交及权重清单。构建两种模型镜像，分别验证容器 CUDA 与模型就绪。 |
| Windows WSL2 NVIDIA | 记录 Windows NVIDIA 驱动、WSL 内核和 Docker Desktop 版本。在 WSL2 启动同机 Compose，复验素材读写、模型加载、开停播及后台播放。 |
| 模型与持续输出 | 用授权音色和可见人脸的符合规范的 idle。核对模型准备、重复使用缓存、默认底片更新触发新准备、两种音色、长文本连续口型、挂起 inference 后的停播，以及持续播放期间的音画同步。 |
| 业务和 AI 分机 | 分别启动两份 Compose。浏览器和远程 Worker 均能访问业务素材源。校验负向鉴权，开播后验证私网 TLS、FLV 播放、推理返回与停播。 |
| 业务状态与恢复 | 复验并发开播互斥、停播后旧 token 拒绝、不同平台事件隔离、主机或容器重启的场次恢复、SQLite 与素材在业务容器重建后仍存在。 |
| Chrome 真实页面 | 在获授权场次分别检查抖音、小红书当前 DOM 与插件识别，核对评论、进场、礼物、点赞和去重。核对仅运营人员启用采集。 |
| 平台转推 | 在另行获授权的非公开场次中显式开启和关闭转推，核对平台接收及停播清理。 |

真实 GPU 推理、外部 RTMP、平台真实 DOM 和目标环境长期直播均属于实地项。此仓库的本地验收使用测试数据，无生产部署记录。

## 最终审阅与复验

最终代码 `5b3fcf8`：Go race/vet、React15项及构建、Python33项（含真实FFmpeg）、插件10项、五种Compose配置及完整CPU合成推理联调通过。整仓审阅三项Important均修复并复审通过。

[持久化验证证据](verification/2026-11-24/README.md) 与 [实施默认及验收边界](implementation-notes.md)。本地分支保留；无推送、生产部署或真实平台评论/推流。
