# LingCast 管理端

`npm ci` 后运行 `npm run dev`，开发端口默认为 5173，`/api` 与 `/media` 代理到 `http://localhost:8080`。生产运行 `npm run build`，由 Go 服务提供 `dist` 目录。

管理员令牌仅保留在当前标签页的 sessionStorage，断开管理连接会清除；不得通过 Vite 环境变量写入构建。视频预览使用同源 `/api/live/{id}/preview?token=...` 认证代理，mpegts.js 按需加载。浏览器需要 MediaSource 支持（桌面 Chrome / Edge）。

页面：直播控制、人物与素材、分类话术库、只读服务设置。平台选择不会触发外部转推；转推需要明确点击启用，刷新后也始终提供独立停止命令。采集令牌仅显示刚创建/轮换结果，重新加载时需轮换后重新配置插件。

`npm test -- --run` 使用模拟 HTTP 验证客户端交互，不代表实际模型、GPU、转推或平台 DOM 验收。上传内容以服务端完整解码与归属校验为最终准入依据。
