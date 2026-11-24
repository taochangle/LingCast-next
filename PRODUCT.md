# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

普通直播运营者。用户确认以桌面直播工作台为主要工作环境，手机兼容但非主工作环境。

## Product Purpose

管理数字人及其形象素材，准备并运行一场数字人直播，查看画面、插播原文话术并处理服务状态。

## Operating Context

Go 主服务和 Web 管理端；Python Worker、配音与口型服务；FFmpeg/SRS 输出。标准 Docker Compose 本地或分机部署。运营者主动开启 Chrome 插件采集抖音或小红书事件。

## Capabilities and Constraints

- 多数字人，每个拥有独立人设、音色ID与分类话术。
- 数字人拥有多个形象，每个形象含用户提供的图片及多个idle视频；不生成素材。
- 系统同时一场直播，开播选择数字人、形象、平台，直播中不换形象。
- 固定稿按原文播放；LLM互动使用服务器时间和明确时区。
- 工作台保留开停播、预览、主持、手动口播、插件凭据与显式转推。
- 人物素材、话术、服务配置分开管理，用户已确认此主线。
- 本次视觉重设计保留后端契约与功能，允许使用Tailwind。

## Brand Commitments

产品名称沿用LingCast。中文界面。用户明确否定现有前端视觉，要求重新设计；已确认“钴蓝 · 轻盈编辑室”视觉方向，以桌面运营工作台为主要使用环境。

## Evidence on Hand

当前React/TypeScript/Vite界面、真实管理API、组件与异步竞态测试。真实GPU、平台DOM及外部RTMP尚未验收。不得编造实时观众数、收入、推流质量指标或演示人物为真实数据。

## Product Principles

- 直播画面和当前可执行操作优先。
- 服务/素材未就绪和失败应明确可见。
- 面向普通运营者使用熟悉且一致的控件。
- 视觉改版保留认证、状态校验和显式转推边界。
