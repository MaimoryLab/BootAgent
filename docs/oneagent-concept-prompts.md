# OneAgent 概念图 Prompt 与生成记录

## 文档状态

- 生成日期：2026-07-21
- 视觉阶段：已完成
- 最终方向：`Native Utility Split View`
- 生成模型：`gpt-image-2`
- 图片用途：视觉方向与组件关系参考，不作为运行时位图或切图素材

本轮从零构建 Prompt，没有使用历史概念图作为输入、构图参考或 Prompt 来源。三个母版独立生成；七张页面图与三张组件状态图均以最终选定的 `master-a-native-utility.png` 为唯一输入图。

## 调用参数

### Image Generation

```text
POST https://apiproxy.paigod.work/v1/images/generations
Content-Type: application/json

model: gpt-image-2
size: 1536x1024
quality: high
output_format: png
n: 1
```

### Image Edit

```text
POST https://apiproxy.paigod.work/v1/images/edits
Content-Type: multipart/form-data

model: gpt-image-2
size: 1536x1024        # 页面图
size: 1024x1024        # 组件状态图
quality: high
output_format: png
n: 1
image: master-a-native-utility.png
```

API Key 只从本机认证文件读取，仅进入请求头；没有写入 Prompt、命令、图片元数据或本文档。每张 PNG 相邻的 `.png.json` 文件保存实际 endpoint、模型、尺寸、Prompt、输入图和 usage，便于复核。

组件图请求尺寸为 `1024x1024`，接口原始返回文件实际为 `1254x1254`。交付前使用 macOS `sips` 做无裁切等比缩放，最终三张组件 PNG 均为 `1024x1024`；对应 sidecar 的 `postprocess` 字段保留原尺寸、原文件 SHA-256 和处理方式。

## 母版公共 Prompt

以下公共 Prompt 与一个方向增量拼接，组成三个母版的完整请求。

```text
Create a coherent high-fidelity desktop application UI concept for OneAgent, a local-first utility that activates a usable AI development environment. This must be an actual software interface, not a marketing page, poster, dashboard collage, device mockup, browser page, or explanatory diagram.

Canvas and shell: 1536x1024 landscape. Center one application window approximately 1392x900 with balanced outer margin. Use a 52px title bar, a restrained desktop window frame, and a stable 64px bottom action area. Show no browser address bar and no physical device. The interface should occupy at least 90 percent of the image.

Design philosophy: macOS-inspired but original, guided by safety, predictability, understanding, achievement, familiarity, simplicity, craft, and immediate feedback. Use a translucent pale-gray navigation material, white workspace, graphite typography, thin neutral separators, subtle physical depth, precise alignment, restrained shadow, system-blue #007AFF for the single primary action, semantic green #34C759 only for successful states, warning orange #FF9F0A only when necessary. Use modest 6px to 8px radii. No large pill containers, no card-heavy SaaS dashboard, no gradients, no decorative blobs, no illustrations.

Component language: compact list rows instead of promotional cards, familiar checkboxes and segmented controls, clear selected state, visible keyboard focus, progressive disclosure, one obvious primary action, stable Back and Continue placement. Use neutral category glyphs rather than copied or invented brand logos.

Text budget: render only these short labels when needed and no other sentences: “OneAgent”, “激活环境”, “总览”, “Agent”, “Provider”, “配置档案”, “继续”. Keep text below 10 percent of the visual area. No annotations or design callouts.

Hard constraints: no Apple logo, no copied Apple application, no browser chrome, no Windows paths, no Docker, no Ollama, no fake API keys, no unrelated products, no terminal dominating the screen, no marketing hero, no oversized headings, no dense paragraphs, no nested cards, no watermark.
```

### Direction A：Native Utility Split View

```text
Direction A — Native Utility Split View. Use a 232px translucent left sidebar with OneAgent at the top and compact navigation rows. The main workspace is a continuous setup surface with a quiet page heading, a five-step progress indicator, three compact agent selection rows, and a fixed bottom action area. Do not use a permanent right inspector. The composition should feel like a calm native system utility that can scale across seven setup pages.
```

输出：[`master-a-native-utility.png`](../output/imagegen/react-apple-v1/master-a-native-utility.png)

### Direction B：Focused Setup Assistant

```text
Direction B — Focused Setup Assistant. Keep the same outer application window and title bar, but replace the full navigation sidebar with a narrow 176px setup progress column. The main workspace centers one task at a time with generous negative space, a small selection summary, and a stable bottom action area. Make the experience feel like a trustworthy first-run setup assistant rather than a long-term dashboard. It must still be practical for dense Agent and Provider steps.
```

输出：[`master-b-setup-assistant.png`](../output/imagegen/react-apple-v1/master-b-setup-assistant.png)

### Direction C：Professional Workspace

```text
Direction C — Professional Workspace. Use a 220px translucent navigation sidebar, a wide central setup workspace, and a restrained 288px contextual inspector on the right. The inspector shows only compact local readiness status and a privacy state, never a large terminal. The central workspace contains list-based setup controls and one blue primary action. The composition should feel like a professional developer utility while remaining quiet and approachable.
```

输出：[`master-c-professional-workspace.png`](../output/imagegen/react-apple-v1/master-c-professional-workspace.png)

## 页面编辑公共 Prompt

七张页面图都使用母版 A 作为输入，并将以下公共 Prompt 与对应页面增量拼接。

```text
Edit the input image. Preserve exactly the application window position, 52px title bar, 232px translucent sidebar, sidebar navigation order, window shadow, white main workspace, system-blue #007AFF, semantic green #34C759, graphite typography, thin separators, five-step progress geometry, and fixed 64px bottom action area. Keep the same camera angle and overall visual density. Change ONLY the main workspace content and the active setup step. Use compact native list rows and familiar controls. Render only the explicitly listed short labels and no other sentences. Do not add browser chrome, device hardware, Apple logo, copied product UI, fake Agent logos, marketing content, decorative graphics, gradients, large cards, unrelated software, terminal-first layout, Windows paths, Docker, or Ollama.
```

### Page 1：选择 Agent

```text
Create the Agent selection page. Active step is 1. Heading: “选择 Agent”. Show a compact section “常用” with exactly three multi-select rows: “Codex”, “Claude Code”, “OpenCode”. Codex and OpenCode are selected and marked “已安装”; Claude Code is selected and marked “待安装”. Below them show one collapsed disclosure row “更多分类” without listing every Agent. Use neutral coding glyphs, real checkboxes, subtle installed status, and one clear blue “继续” button. Allowed text only: 选择 Agent, 常用, Codex, Claude Code, OpenCode, 已安装, 待安装, 更多分类, 继续.
```

输出：[`page-1-agent-selection.png`](../output/imagegen/react-apple-v1/page-1-agent-selection.png)

### Page 2：配置方式

```text
Create the configuration mode page. Active step is 2. Heading: “配置方式”. Show exactly two full-width native choice rows, not large cards. The first row “配置模型服务” is selected with a blue radio indicator and a small provider glyph. The second row “使用已有账号” is unselected with a person-and-key glyph. Add a compact privacy line “密钥仅保存在本机” near the selected option. Keep one clear blue “继续” button. Allowed text only: 配置方式, 配置模型服务, 使用已有账号, 密钥仅保存在本机, 继续.
```

输出：[`page-2-config-mode.png`](../output/imagegen/react-apple-v1/page-2-config-mode.png)

### Page 3：Provider 与 Key

```text
Create the Provider and API Key page. Active step is 3. Heading: “连接模型服务”. At the top of the main workspace use a restrained segmented control with “PPIO”, “Novita”, “自定义”; PPIO is selected. Below show one secure API Key field with masked dots and a compact privacy lock icon. Place a secondary “测试连接” button beside the field and show a small green inline state “已连接” after a successful test. Keep the custom base URL field hidden because PPIO is selected. Add one quiet external-link icon for registration without explanatory copy. Use one clear blue “继续” button. Allowed text only: 连接模型服务, PPIO, Novita, 自定义, API Key, 测试连接, 已连接, 继续.
```

输出：[`page-3-provider-key.png`](../output/imagegen/react-apple-v1/page-3-provider-key.png)

### Page 4：模型选择

```text
Create the model selection page. Active step is 4. Heading: “选择模型”. Show a compact search or model input at the top with a small secondary command “刷新列表”. Under it show one selected native list row “deepseek-v3” with a green available indicator, followed by one quiet disclosure row “手动输入”. Keep the layout sparse but not empty, with the controls aligned to the same content grid as previous pages. Use one clear blue “继续” button. Allowed text only: 选择模型, 刷新列表, deepseek-v3, 可用, 手动输入, 继续.
```

输出：[`page-4-model-selection.png`](../output/imagegen/react-apple-v1/page-4-model-selection.png)

### Page 5：确认激活

```text
Create the final review page. Active step is 5. Heading: “确认激活”. Use one continuous grouped list, not a table and not metric cards. Show four review rows: “Agent” with value “3 项”; “Provider” with value “PPIO”; “模型” with value “deepseek-v3”; “备份” with value “自动备份”. Each row has a quiet edit chevron. Add one small green readiness state “准备就绪”. Replace the blue Continue button with the primary command “开始激活”. Allowed text only: 确认激活, Agent, 3 项, Provider, PPIO, 模型, deepseek-v3, 备份, 自动备份, 准备就绪, 开始激活.
```

输出：[`page-5-review.png`](../output/imagegen/react-apple-v1/page-5-review.png)

### Page 6：执行结果

```text
Create the activation execution and result page. Replace the five-step progress control with a compact completion state while preserving its location. Heading: “正在激活”. Show exactly three horizontal progress rows: “Codex”, “Claude Code”, “OpenCode”. Codex and OpenCode are “已完成”; Claude Code shows one recoverable failed state with a quiet “重试” command. Beneath the rows place a collapsed “查看日志” disclosure, not an open terminal. The primary blue button is “进入总览”. Keep the failure visually restrained and actionable. Allowed text only: 正在激活, Codex, Claude Code, OpenCode, 已完成, 重试, 查看日志, 进入总览.
```

输出：[`page-6-activation-result.png`](../output/imagegen/react-apple-v1/page-6-activation-result.png)

### Page 7：环境总览

```text
Create the activated environment overview page. Remove the five-step progress indicator while preserving the header spacing and application shell. Heading: “开发环境已就绪”. Show one calm success symbol beside the heading. Use a continuous summary list with three rows: “Provider” value “PPIO”; “模型” value “deepseek-v3”; “Agent” value “3 项”. Below add a compact first-request state “首次请求” and green “已通过”. Provide one blue primary command “运行测试” and one quiet secondary command “重新配置”. Keep the page useful as the long-term home screen, not a success marketing page. Allowed text only: 开发环境已就绪, Provider, PPIO, 模型, deepseek-v3, Agent, 3 项, 首次请求, 已通过, 运行测试, 重新配置.
```

输出：[`page-7-environment-overview.png`](../output/imagegen/react-apple-v1/page-7-environment-overview.png)

## 组件状态公共 Prompt

三张组件图同样以母版 A 为输入，使用以下公共 Prompt 与各自增量。

```text
Edit the input image into a square 1024x1024 component-state study for the same OneAgent interface. Preserve the exact visual language from the source: off-white background, white surfaces, graphite typography, #007AFF selection and focus, #34C759 success, #FF9F0A warning, restrained red error, 6px to 8px radii, thin separators, compact native list rows, neutral line icons, and precise spacing. Remove the full application shell and show one centered component panel occupying at least 88 percent of the canvas. This is a UI component sheet, not an annotated design document: no arrows, measurements, paragraphs, browser chrome, device frame, Apple logo, fake brand logos, marketing graphics, gradients, nested cards, or watermark. Render only the explicitly allowed labels.
```

### Agent 行状态

```text
Show six vertically stacked variants of the same AgentRow component using “Codex” as the stable item name: default, hover, selected, installed, pending install, and guide-only. Use real checkbox geometry, a neutral coding glyph, stable row height, right-aligned status, and visible keyboard focus on one row. Allowed labels only: Agent 状态, Codex, 默认, 悬停, 已选择, 已安装, 待安装, 仅引导.
```

输出：[`components-agent-row-states.png`](../output/imagegen/react-apple-v1/components-agent-row-states.png)

### Key 与连接状态

```text
Show six vertically stacked SecureKeyField and ConnectionStatus variants: empty, masked value, testing with spinner, connected success, rejected credential, endpoint error. Keep the field width and button placement identical in every row. Use a lock icon, masked dots, inline status, and restrained validation colors. Allowed labels only: API Key, 未填写, 测试中, 已连接, Key 被拒绝, 端点错误, 测试连接.
```

输出：[`components-key-connection-states.png`](../output/imagegen/react-apple-v1/components-key-connection-states.png)

### 安装执行状态

```text
Show six vertically stacked AgentProgressRow variants using the stable names Codex, Claude Code, and OpenCode across the examples: waiting, installing, configuring, completed, failed with retry, and a collapsed log disclosure. Use compact progress indicators and keep failures recoverable rather than alarming. Allowed labels only: 执行状态, Codex, Claude Code, OpenCode, 等待, 安装中, 配置中, 已完成, 失败, 重试, 查看日志.
```

输出：[`components-activation-states.png`](../output/imagegen/react-apple-v1/components-activation-states.png)

## Prompt 使用规则

- 后续修图只允许使用母版 A 或以上述母版派生的同页图片，不混合 B、C 的布局语言。
- 页面编辑只改变主工作区和当前步骤；窗口、侧栏、标题栏、步骤条位置与底部操作区保持稳定。
- 生图中的文字只用于验证信息密度。真实产品文字、焦点、错误语义和动态数据由 React 组件重建。
- 同一页面最多进行两次布局修订；若只是错字，不继续消耗生成次数。
- 概念图不得直接进入产品资源目录，也不得作为 CSS 背景或运行时截图。
