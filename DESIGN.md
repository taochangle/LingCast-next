---
name: LingCast
description: 钴蓝 · 轻盈编辑室，面向普通直播运营者的桌面工作台。
colors:
  cobalt: "#2449cf"
  cobalt-hover: "#1c3bb4"
  ink: "#17213a"
  muted: "#646e81"
  line: "#e4e8ef"
  ground: "#f7f8fb"
  white: "#ffffff"
  selected: "#ebf0ff"
  control-line: "#dce1ea"
  field-line: "#d9dfe9"
  hover-ground: "#eef2ff"
  hover-line: "#b8c6f6"
  focus: "#839bf0"
  disabled-ground: "#e9ecf3"
  disabled-ink: "#687489"
  preview-mat: "#edf0f5"
  video-ground: "#10131a"
  notice-ground: "#e9efff"
  notice-ink: "#254599"
  error-ground: "#fff0ee"
  error-ink: "#8f3340"
  danger: "#a83643"
  live: "#21815e"
typography:
  headline:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, \"PingFang SC\", \"Microsoft YaHei\", sans-serif"
    fontSize: "27px"
    fontWeight: 670
    lineHeight: 1.3
    letterSpacing: "-0.025em"
  title:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, \"PingFang SC\", \"Microsoft YaHei\", sans-serif"
    fontSize: "17px"
    fontWeight: 650
    lineHeight: 1.4
  body:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, \"PingFang SC\", \"Microsoft YaHei\", sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.55
  label:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, \"PingFang SC\", \"Microsoft YaHei\", sans-serif"
    fontSize: "13px"
    fontWeight: 560
  button:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, \"PingFang SC\", \"Microsoft YaHei\", sans-serif"
    fontSize: "13px"
    fontWeight: 550
    lineHeight: 1.45
rounded:
  control: "8px"
  media: "12px"
  login: "14px"
spacing:
  step-8: "8px"
  step-12: "12px"
  step-14: "14px"
  step-16: "16px"
  step-17: "17px"
  step-20: "20px"
  step-24: "24px"
  step-28: "28px"
  step-32: "32px"
components:
  button-primary:
    backgroundColor: "{colors.cobalt}"
    textColor: "{colors.white}"
    typography: "{typography.button}"
    rounded: "{rounded.control}"
    padding: "8px 14px"
  button-primary-hover:
    backgroundColor: "{colors.cobalt-hover}"
  button-secondary:
    backgroundColor: "{colors.white}"
    textColor: "{colors.ink}"
    rounded: "{rounded.control}"
    padding: "8px 14px"
  button-text:
    backgroundColor: "transparent"
    textColor: "{colors.cobalt}"
    padding: "8px 0"
  input:
    backgroundColor: "{colors.white}"
    textColor: "{colors.ink}"
    rounded: "{rounded.control}"
    padding: "10px 12px"
    height: "42px"
  navigation-selected:
    backgroundColor: "{colors.selected}"
    textColor: "{colors.cobalt}"
    rounded: "{rounded.control}"
    padding: "12px 14px"
  speech-container:
    backgroundColor: "{colors.white}"
    rounded: "{rounded.media}"
---
# Design System: LingCast

## Overview

**Creative North Star: "钴蓝 · 轻盈编辑室"**

以钴蓝 · 轻盈编辑室为视觉北极星：冷白底、清楚的编辑列和克制的操作强调，使普通运营者能够阅读素材、确认状态并完成直播操作。桌面是主要工作环境，手机延续同一控件语言。

系统使用紧凑中文无衬线层级、细分隔线和真实上传素材。蓝色标记当前选择及主要操作；平面分组承担信息组织，图像与视频承担内容表现。

**Key Characteristics:**
- 冷白底与钴蓝操作强调
- 细分隔线与编辑列对齐
- 真实素材、明确状态和可恢复错误
- 桌面优先，窄屏连续堆叠

## Colors

钴蓝与冷白中性色构成主体系；绿、红只承担实际状态语义。所有规范色值见 frontmatter，源自 frontend/src/styles.css。

### Primary
- **钴蓝 / 深钴蓝**：主要按钮和文字操作；悬停时加深主要按钮。
- **淡钴蓝选择底**：导航与人物、形象的当前选择；按钮悬停使用独立淡色底和描边。

### Neutral
- **冷白底 / 白色**：应用底面、导航栏及表单控件。
- **深墨 / 次级墨**：标题正文与帮助文案。
- **细灰线 / 控件线 / 字段线**：页面分隔与可编辑边界。
- **浅灰预览垫 / 视频深底**：分别承托空态、图片及播放视频。
- **禁用底 / 禁用墨**：表达不可执行的操作。

状态色单独表达通知、错误、危险操作与直播运行，不构成第二套装饰性品牌强调色。

### Named Rules
**The Cobalt Selection Rule.** 钴蓝同时标记当前选择和主要行动，状态反馈使用各自语义色。

## Typography

**Body Font:** Inter，回退至系统中文无衬线字体；完整栈见 frontmatter。源码未载入独立字体文件，实际字体取决于设备。没有单独的展示字体或等宽字体角色。

### Hierarchy
- **Headline**：页面标题；窄屏降为 24px。
- **Title**：内容区标题。
- **Body**：常规界面内容；紧凑但保持中文行高。
- **Label**：表单标签；帮助与空态也使用 13px，但采用次级墨，行高 1.75。
- **Button**：共享按钮层级；次要元信息采用 12px。

## Layout

应用壳为 190px 白色侧栏与可收缩主区；主区横向留白 clamp(22px, 2.65vw, 48px)。桌面工作台使用弹性内容列与 306px 准备列，列间距 32px，准备列有左分隔线。人物和话术编辑区也采用列表与编辑列分离。它们是当前表面的实现，不要求所有新页面复制工作台构图。

在 1100px 以下侧栏缩至 175px，准备列缩至 258px；820px 以下侧栏变为顶部四项导航，准备列缩至 225px；650px 以下编辑区、工作台和连接区变为单列，页面横留白 16px。365px 以下导航图标与文字纵向排列。共享间距来自 frontmatter 中已观察的尺度；并非所有局部尺寸都属于全局间距 token。

图片和视频使用真实素材；预览采用 contain，图库缩略图采用 cover。长资源标识允许任意位置换行。

## Elevation & Depth

当前实现没有 box-shadow。层次来自冷白与白色的差别、细线、空间和局部选中底色；不将阴影引入为默认容器装饰。源码没有常规动画或过渡 token；展开连接面板直接旋转箭头。减少动态效果偏好会禁用动画与过渡。

### Named Rules
**The Flat Grouping Rule.** 静态编辑内容以分隔和间距组织，避免把每个区域变成同等强调的浮起卡片。

## Shapes

控件采用轻圆角；大型预览与口播容器采用更舒展的圆角，登录面板使用独立较大圆角。具体复用半径见 frontmatter。状态点是圆形；预览与页脚共同形成上下连续的圆角边界。常规描边为 1px，图标采用一致的细描边 SVG，默认 18px、stroke-width 1.65。

## Components

### Buttons
克制、熟悉、明确可执行。常规按钮最小高度 40px，主行动可用 44px。主按钮钴蓝白字，次按钮白底细线，文字按钮透明底钴蓝字，危险按钮使用危险文字色。悬停有淡底与描边响应；主按钮悬停加深。禁用覆盖颜色并取消手形指针。全局 focus-visible 使用 3px 焦点描边与 3px 外偏移。

### Cards / Containers
大多数面板透明且平面分组；口播编辑器使用白底、细线和媒体半径，内部文本区与操作条以横线分开。登录面板才使用完整白色有边框容器。不要把这两个局部容器推广为全部页面布局。

### Inputs / Fields
白底、字段线、控件半径；普通输入和选择器高 42px。字段标签与控件之间距 7px；文本域允许纵向调整。禁用字段采用灰底与次级文字。错误反馈置于独立提示区；当前源码没有字段级错误描边模式。

### Navigation
桌面左侧四目的地，当前项使用淡钴蓝底和钴蓝文字；其余项透明底。窄屏顶部导航采用单行“人物素材”，可访问名称仍为“人物与素材”。不使用图标字符替代 SVG。

### Preview / Speech Container
预览垫在空态显示居中图标与说明，在播放时切换深底。口播编辑器位于预览之后，以文本输入和底部动作条组成单个容器。预览下沿的状态垫保持轻灰层次，不模仿实时流质量指标。

### Status / Recovery
通知和错误采用独立浅色底与对应文字；错误用中文解释问题及恢复操作，原始详情可展开。直播状态点仅表达真实运行状态。当前实现没有独立 chip/tag 原语。

## Do's and Don'ts

### Do:
- Do 使用钴蓝区分主要操作和当前选择，并保留辅助文字的安静层级。
- Do 延续统一描边 SVG 图标、可见键盘焦点和禁用状态。
- Do 用细分隔线和间距组织编辑内容，素材按真实比例容纳。
- Do 为长中文名称、地址和来源标识提供换行空间。

### Don't:
- Don't 引入装饰性大标题、虚构运营指标或替代真实素材的插画。
- Don't 把所有内容都封装成同等强调的卡片。
- Don't 只暴露后端错误原文而缺少中文说明与恢复操作。
