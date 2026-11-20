# 交互修复与操作反馈方案

> 状态：实施中，§5 第 1–5 步已完成（2026-10-12）。起草于 2026-10-12，代码核对至 `claude/compat-mode-flow` 工作区（基于 `3d335e0`）。
> 本文只管「点了之后发生什么」：流程断点、失败处理和操作反馈。字号、术语和信息层级见
> [界面友好度改造方案](ui-friendliness-plan.md)，两份方案互不覆盖；重叠处（10px 错误小字）以本文 §3 的做法为准。

## 1. 问题清单

编号供 PR 引用。「反馈」列标 ✓ 的条目由 §3 的反馈方式一并解决，不单独修。

### P0：首次使用的主线断掉

| 编号 | 问题 | 位置 | 修复方向 | 反馈 |
| --- | --- | --- | --- | --- |
| I-01 | 点「开始安装」后立刻跳到任务页，「下一步命令」、连接测试警告、「前往 Claude Desktop 下载」都只渲染在激活页，用户永远看不到；任务页没有「启动」按钮，标题重复为「正在安装 · 安装 X」 | `frontend/src/pages/ActivationPage.tsx:253`、`:491`、`:496`、`:509`；`frontend/src/pages/InstallTaskPage.tsx:48` | 安装期间留在激活页，任务页只用于从任务中心回看进度；完成后主按钮为「启动 {Agent}」，「进入总览」降为次要 | |
| I-02 | 向导里 Key 未保存前「测试连接」不可用且无说明；兼容模式只在测试失败后提出，不测就会在 Agent 包装完后配置失败，失败页只有必然再失败的「重试」 | `frontend/src/pages/ProviderKeyPage.tsx:28`、`:52`、`:209`；`internal/app/install.go:442` | 向导测试复用 `draft: true`（`frontend/src/pages/ProvidersPage.tsx:170`），允许用未保存的 Key 测试；激活失败且错误可由兼容模式解决时，行内给出「改用兼容模式」 | |

### P1：启动与失败处理

| 编号 | 问题 | 位置 | 修复方向 | 反馈 |
| --- | --- | --- | --- | --- |
| I-03 | 命令行 Agent 启动成功无任何反馈；后端返回的实际终端、Web 地址被丢弃；桌面 Agent 却会提示「已打开」，两边不一致 | `frontend/src/components/AgentManageRow.tsx:189`；`internal/app/launch.go:20`；`frontend/src/components/DesktopAppSection.tsx:100` | 读取 `LaunchAgentResult`，终端被替换时说明用了哪个 | ✓ |
| I-04 | 未配置模型服务的 Agent 也能直接启动，到第一句对话才报错 | `frontend/src/components/AgentManageRow.tsx:179` | 未配置时主按钮改为「先配置」，或在启动目录弹窗中提醒 | |
| I-05 | 行内失败 10px 单行截断、无悬停全文、不可关闭；无中文文案的错误码直接显示后端英文（含测试**成功**消息）；52 处只取 `.message`，丢掉了 `hint` | `frontend/src/styles/app.css:1407`；`frontend/src/backend/errors.ts:59`；`internal/app/launch.go:77`；`internal/app/terminal.go:223`；`internal/provider/client.go:393`；`frontend/src/pages/ModelSelectionPage.tsx:17` | 失败统一走 §3.3；补齐常见错误码文案；成功消息不再透传后端原文 | ✓ |
| I-06 | 模型服务页编辑模式忽略 `returnTo`，从向导去改 Key 后回不去；侧边栏无向导入口，「安装 Agent」会重置向导 | `frontend/src/pages/ProvidersPage.tsx:120`、`:228`；`frontend/src/components/NavigationSidebar.tsx:8`；`frontend/src/pages/EnvironmentOverviewPage.tsx:24` | 编辑保存与取消都遵守 `returnTo`；向导未完成时总览给「继续设置」 | |
| I-07 | 应用桌面 Agent 配置丢掉后端的「需要重启」说明；应用后跳回总览无任何确认 | `frontend/src/pages/AgentProfilePage.tsx:372`、`:396`；对照 `frontend/src/pages/ActivationPage.tsx:239` | 读取 `restart`，跳转后用 §3.3 告知 | ✓ |
| I-08 | MCP、Skills 页请求失败几乎全部静默：读取失败显示为「尚未发现」；编辑失败无反应；「应用更改」无忙碌态可重复点、无结果；Skills 删除先从列表移除再发请求，失败后仍显示已删 | `frontend/src/pages/MCPPage.tsx:121`、`:124`、`:137`、`:141`、`:148`；`frontend/src/pages/SkillsPage.tsx:89`、`:96` | 区分「读取失败」与「为空」；应用期间禁用；删除等请求成功后再改列表 | ✓ |

### P2：易误操作或困惑

| 编号 | 问题 | 位置 | 修复方向 | 反馈 |
| --- | --- | --- | --- | --- |
| I-09 | 勾选「记住此 Agent 的目录」后每次仍弹目录框；目录为空时点「启动」无反应 | `frontend/src/components/AgentManageRow.tsx:199`、`:217` | 已记住则直接启动，目录可在「更多」里修改；空目录禁用「启动」并说明 | |
| I-10 | 配置页模型框失焦即保存，且会改动共用该配置模版的其他 Agent，无提示、无成功反馈 | `frontend/src/pages/AgentProfilePage.tsx:182`、`:597` | 共用时在模型框下注明「也会改动 {agents}」；保存结果走 §3.2 | ✓ |
| I-11 | 无说明的禁用或回跳：配置模版 ID 重复时保存变灰；确认页无 Key 时静默跳回；切换模型服务静默清空已粘贴的 Key | `frontend/src/pages/AgentProfilePage.tsx:164`；`frontend/src/pages/ReviewPage.tsx:44`；`frontend/src/pages/ProviderKeyPage.tsx:67` | 字段下给出原因；回跳前说明；切换前若有未保存 Key 先确认 | |
| I-12 | 总览读取失败时写「请刷新后重试」却无刷新按钮；两处「按引导安装…」空状态看似可点实为文字 | `frontend/src/pages/EnvironmentOverviewPage.tsx:37`、`:117`、`:153` | 加「重试」；空状态改为按钮 | |
| I-13 | 离开 MCP/Skills 页时不清除草稿状态，退出应用时会为已丢弃的草稿弹「未应用的更改」 | `frontend/src/pages/MCPPage.tsx:127`；`frontend/src/pages/SkillsPage.tsx:90` | effect 卸载时上报 `dirty=false`；站内离开有草稿时先确认 | |
| I-14 | 命令行 Agent 列表无简介（只在图标 title 里）；「待安装」读作已排队 | `frontend/src/components/AgentRow.tsx:21`、`:34` | 显示一行简介；状态改为「未安装」 | |

### P3：细节

| 编号 | 问题 | 位置 |
| --- | --- | --- |
| I-15 | 已安装的 Agent 在确认页仍写「开始安装」 | `frontend/src/pages/ReviewPage.tsx:60` |
| I-16 | 模型服务步的「返回」跳过「选择配置模版」；步骤条不可点击回退 | `frontend/src/pages/ProviderKeyPage.tsx:145`；`frontend/src/components/SetupStepper.tsx:39` |
| I-17 | 卸载实例弹窗「继续卸载」不是危险样式 | `frontend/src/components/AgentManageRow.tsx:392` |
| I-18 | 运行环境任何失败都提示「官方源和镜像都已尝试过」 | `frontend/src/components/RuntimeSection.tsx:96` |
| I-19 | 中文界面残留英文：「{n} variant」「MCP Registry」「Base URL」 | `frontend/src/pages/SkillsPage.tsx:151`；`frontend/src/pages/MCPPage.tsx:200`；`frontend/src/pages/ReviewPage.tsx:84` |

## 2. 操作反馈：现状

成功和失败都写进正文：

| 形式 | 数量 | 例 |
| --- | --- | --- |
| `notice` 块（插在正文顶部或中间） | 18 处 | `DesktopAppSection.tsx:147`、`AgentProfilePage.tsx:438`、`ProviderKeyPage.tsx:219` |
| 错误段落（`agent-manage-error`、`settings-field-error` 等） | 13 处 | `ProvidersPage.tsx:392`、`ConversionPage.tsx:140` |
| 行内 10px 小字 | Agent 行 | `AgentManageRow.tsx:344` |
| 全局横幅 | 1 处 | `AppWindow.tsx:15`（`app.css:170` 的注释记录了它曾把页脚按钮挤出窗口） |

由此带来四个问题：

1. **挤压布局。** 提示出现时正文整体下移，用户正要点的按钮会跟着移走；消失时再移回来。
2. **出现在别处。** 提示插在页面顶部，按钮在页脚或列表深处，用户看不到结果。
3. **不会消失。** 成功提示大多一直留着，直到下一次操作或离开页面（`DesktopAppSection` 的 4 秒自动消失是唯一例外）。
4. **跨页即丢。** 「应用」后跳回总览，提示属于上一页的组件状态，跟着一起卸载（I-07）。

## 3. 设计

### 3.1 原则

与 [ADR-010](decisions/ADR-010-visual-restyle-swiss-blueprint.md) 的「安静的表面、扁平、1px 线代替阴影」以及友好度方案的「一句话」一致：

1. **不改变布局。** 反馈只在预留的位置或浮层上出现，正文不因它移动一个像素。
2. **结果出现在视线所在处。** 优先在被点的按钮上，其次在固定位置。
3. **一句话，十八个字以内。** 原因和建议收进「详情」，需要时再展开。
4. **成功自己消失，失败等人处理。** 失败一律不自动消失。
5. **一个结果只说一次。** 按钮已经表达了成功，就不再弹提示。

### 3.2 四个去处

| 结果 | 去处 | 停留 |
| --- | --- | --- |
| 单个按钮触发、结果立刻可见的**成功** | 按钮原位（§3.2.1） | 1.6 秒后复原 |
| 成功的影响在别处，或发生了跳转 | 回执条（§3.3） | 3 秒，鼠标悬停时暂停 |
| 任何**失败** | 回执条；属于某个 Agent 的失败另在该行留状态标记（§3.4） | 直到关闭或重试成功 |
| 耗时任务（安装、下载、更新、迁移） | 任务中心（不变） | 直到关闭 |

另外两类不属于「反馈」，保持现有方式：

- **需要用户做决定的提示**仍留在正文：兼容模式提议、缺少运行环境、模型检测结果。它们本身就是页面内容，挪走反而找不到。这类提示只允许这三种，新增需在本文登记。
- **确认**（删除、卸载、迁移）继续用系统原生对话框（`frontend/src/state/confirmDelete.ts`）。

#### 3.2.1 按钮原位

按钮宽度在首次渲染时锁定，后面三个状态都不改变宽度：

```text
 [ ▶ 启动 ]  →  [ ◌ 启动 ]  →  [ ✓ 已打开 ]  →  [ ▶ 启动 ]
   空闲          进行中         成功 1.6s        复原
```

- 失败时按钮直接复原，原因交给回执条。不做抖动或变红：按钮变红会被误读为危险操作。
- 文字由调用方给出，限四个字以内：「已打开」「已保存」「已应用」「已刷新」。
- 按钮旁放一个视觉隐藏的 `role="status"` 区域，供读屏软件播报。

适用于：启动、打开桌面 Agent、保存模型服务（未改动其他 Agent 时）、刷新状态、设置页的各项保存。

### 3.3 回执条

一个全局浮层（下称「回执条」），挂在 `App` 层（与 `TaskCenterProvider` 同级），所以跨页跳转后仍在。

**位置**：内容区右上角（2026-10-12 确认）。这是桌面应用通知出现的位置，符合用户预期。

- 上沿贴在页头分隔线下方 12px，右沿与正文右边距对齐（42px）。不压页头，所以向导页右上的步骤条不受影响。
- 覆盖正文右上角的一小块，不遮挡页脚按钮、侧边栏和任务中心。
- 窗口宽度小于 560px 时，左右各留 16px 横向铺满。

```text
┌ sidebar ┬───────────────────────────────────────────────────────┐
│         │ 环境总览                                [刷新] [安装] │
│         ├───────────────────────────────────────────────────────┤
│         │               ┌──────────────────────────────────────┐│
│         │               ┃ ✕  无法启动 Claude Code   详情 重试 × ││
│         │               └──────────────────────────────────────┘│
│         │  Codex        团队默认 · PPIO · gpt-5     [配置][启动] │
│         │  Claude Code  无配置模版 · 无模型服务      [配置][启动] │
│         │                                                       │
│         ├───────────────────────────────────────────────────────┤
│ 任务中心 │                                          页脚按钮      │
└─────────┴───────────────────────────────────────────────────────┘
```

**样式**：

- 单行，最大宽度 420px；窗口宽度小于 560px 时左右各留 16px。
- `--window-bg` 底，1px `--border` 边，左侧 3px 状态色条（`--green` / `--red` / `--orange` / `--info`），圆角 `--radius-control`。不用阴影和模糊（Linux 的 WebKitGTK 不适合模糊效果）。
- 文字 13px（友好度方案中的辅助说明层级），图标 16px。
- 浅色与深色只换 token，不写死颜色。

**结构**：`图标 · 一句话 · 详情（有详情时） · 最多一个动作 · 关闭`。

- 动作只能是「重试」「查看」「撤销」之一。点击动作会先关闭回执条再执行；重试用同一个 key 报告结果。
- 「详情」在回执条下方展开为小卡片，内容是 `describeFailure` 给出的原因、建议，以及错误码。正文依旧不动。这样做同时解决了 I-05 中被丢掉的 hint。

**时序**：

- 进入：160ms 淡入，同时下移 4px；`prefers-reduced-motion` 下不做动画。
- 成功：3 秒后消失，鼠标悬停或键盘聚焦时暂停计时。
- 失败：不自动消失。
- 同时只显示一条。新消息与当前消息同 key（如 `launch:codex`）时直接替换；不同 key 的排队，最多排 3 条。未关闭的失败不会被后来的成功顶掉；排在失败后面的成功如果等待超过 3 秒就丢弃，免得关掉失败后冒出一条过时的「已保存」。

**无障碍**：

- 播报区域常驻 DOM，内容为空也存在，避免首次插入时读屏软件漏报。
- 成功用 `role="status"`，失败用 `role="alert"`。
- 不抢焦点；回执条获得焦点时，按 Esc 关闭。

**文案预算**（主句十八个字以内，英文约 40 个字符）：

| 场景 | 现在 | 改为 |
| --- | --- | --- |
| 启动失败 | 行内 10px：`Invalid launch directory: /Users/…/proj`（截断） | 「无法启动 Claude Code」·详情 → 「启动目录不存在」+ 完整路径 |
| 终端被替换 | 无 | 「已用「终端」打开，iTerm2 未安装」 |
| 保存模型服务并重新应用 | 列表上方绿框「应用完成 已重新应用到 Codex、Claude Code」 | 「已更新 2 个 Agent」·查看 |
| 重新应用部分失败 | 红色段落：「Codex 重新应用失败：…」 | 「Codex 未能更新」·详情 |
| 应用配置模版（跳回总览） | 无 | 「Codex 已改用「团队默认」」 |
| 桌面 Agent 需重启 | 无（I-07） | 「重启 Claude Desktop 后生效」（失败级别：不自动消失） |
| MCP 应用更改 | 无 | 「已同步 3 个 MCP 服务器」/「2 个未同步」·详情 |
| 连接测试成功 | `OpenAI Chat Completions connection test passed.` | 保持在测试按钮旁的状态位（见 §3.5），文案「连接正常」 |

### 3.4 Agent 行的状态标记

属于某个 Agent 的失败（启动、更新、卸载），除了弹出回执条，还要在该行留下记号，因为用户可能已经关掉了回执条：

- 复用 `agent-manage-meta` 中已有的状态位（`AgentManageRow.tsx:371`），显示一个固定尺寸的标记，如「启动失败」，取代 10px 小字行。
- 点击标记弹出与回执条「详情」相同的小卡片；该 Agent 下一次操作成功后，标记自动清除。
- 与状态灯计划的圆点方向兼容：圆点落地后，这里改为红点加悬停卡片。

### 3.5 原地状态位（表单内）

「测试连接」「检测模型」这类结果本身就是表单的一部分，继续留在按钮旁，但要预留固定高度。目前 `ConnectionStatus` 的「尚未测试连接」已经起到占位作用；失败时如果有多行细节（按协议分列），只显示一行摘要，其余收进「详情」。

字段校验错误显示在对应字段下方，并预留一行高度。按钮变灰时，用 `title` 和 `aria-describedby` 说明原因（I-11）。

### 3.6 迁移映射

| 现有位置 | 改为 |
| --- | --- |
| `AgentManageRow` 启动、更新、卸载的 `failure` 与 `taskFailure` 小字 | 回执条 + 行状态标记 |
| `DesktopAppSection.tsx:147`、`:148`（含 4 秒自动消失的「已打开」） | 按钮原位；失败走回执条 |
| `ProvidersPage.tsx:392`、`:396` | 回执条 |
| `ProfilesPage`、`AgentProfilePage.tsx:438` 的保存与应用失败 | 回执条 |
| `ConversionPage.tsx:140` | 回执条 |
| `SettingsPage` 各 `*Failure` 段落 | 按钮原位 + 回执条 |
| MCP、Skills 的应用与导入（含 `SkillsPage` 的 `message`） | 回执条 |
| `RuntimeSection`、`RuntimePrompt` 的下载失败 | 保持：任务中心 + 原位（属于任务） |
| `ProviderKeyPage` 兼容模式提议、`RuntimePrompt`、`ModelCheckNotice` | 保持在正文（需要决策） |
| `AppWindow.tsx:15` 迁移横幅 | 改为一次性回执条，失败级别（不自动消失），动作为「详情」 |

## 4. 接口草案

```ts
// frontend/src/state/FeedbackContext.tsx
type FeedbackTone = "success" | "failure" | "warning";
interface FeedbackAction { label: string; run: () => void | Promise<void> }
interface FeedbackInput {
  key?: string;          // 同 key 替换，如 `launch:${agentId}`
  tone: FeedbackTone;
  message: string;       // 主句，≤ 18 字
  detail?: string;       // 「详情」卡片正文
  action?: FeedbackAction;
}
const feedback = useFeedback();
feedback.show(input);
// 失败的快捷写法：内部调用 describeFailure，message → 主句，hint 与错误码 → detail
feedback.failure(error, t("无法启动 {name}", { name }), { key, action });
```

```tsx
// 按钮原位：调用方返回 Promise，成功后显示 doneLabel，失败时复原并交给 feedback.failure
<ActionButton icon={Play} label={t("启动")} doneLabel={t("已打开")} onAction={launch} />
```

`failureLine` 与 `.message` 的 52 处调用，凡是把结果展示给用户的，逐页改用 `feedback.failure`。只用于判断错误码的调用保持不变。

## 5. 实施顺序

1. `FeedbackProvider`、回执条、`ActionButton` 以及它们的单元测试（不改任何页面）。**已完成**：`frontend/src/state/FeedbackContext.tsx`、`frontend/src/components/FeedbackViewport.tsx`、`frontend/src/components/ActionButton.tsx`；`PageScaffold` 渲染回执条，`App` 挂载 Provider。
2. 总览：Agent 行与桌面 Agent（I-03、I-05、§3.4），同时修 I-07 的跳转后提示。**已完成**：两类「启动」改为 `ActionButton`，命令行 Agent 的按钮在目录弹窗确认后才进入成功态，取消不算成功；启动、更新、卸载、安装失败在行内只留可点击的标记，点开即重现回执条；终端被替换时给出警告回执并提供「去设置」；`DesktopAppSection` 去掉了正文里的成功与失败块和 4 秒计时；配置页应用后在总览给出回执，桌面应用需要重启时改为不自动消失的警告。后端 `LaunchResponse` 没有 Web 地址字段，Web 类 Agent 只确认「已打开」。I-05 的错误码中文文案仍待第 3 步一并补齐。
3. 模型服务、配置模版、兼容模式、设置页。**已完成**：四个页面的保存、删除、应用、检测失败都改为回执条，正文里的错误段落和「应用完成」块已移除；保存结果统一由 `frontend/src/state/saveReceipt.ts` 生成（「已保存，N 个 Agent 已更新 / 未能更新」，涉及的 Agent 在「详情」里），配置页改模型时因此能看到同时改动了哪些 Agent（I-10 的提示部分）；作为「应用」「检测模型」前置步骤的保存不单独报成功。设置页各控件自己显示保存后的值，只在失败时回滚并给回执条。兼容模式读取失败的回执条带「重试」。I-05：后端新增 `LAUNCH_DIRECTORY_INVALID`、`TERMINAL_NOT_FOUND`、`TERMINAL_LAUNCH_FAILED` 三个错误码并配中文文案；连接测试成功不再显示后端英文句子；模型列表的状态由结构化字段生成（`modelListMessage`），并从字段上方的提示块移到字段下方的固定状态行。向导的连接模型服务页仍用正文提示，随 I-02 一起改。
4. MCP、Skills（与 I-08 的失败处理一起修）。**已完成**：两页的扫描失败给回执条并带「重试」，空状态改为「没能读取…」而不是邀请用户新增第一个；MCP 打开编辑失败给回执条，「保存草稿」在 ID 与命令或 URL 填全前禁用；「应用更改」「应用 Skills」应用期间转圈并禁用，结果由 `frontend/src/state/applyReceipt.ts` 统计每个 Agent 的结果（「已同步 N 个 MCP 服务器」/「N 项未能应用」，原因在「详情」里）。Skills 删除改为后端确认后才从列表移除，失败时仍显示「待删除」可重试。导入结果从标题下的一行字改为回执条；后端 `SkillService.PreviewImport` 在用户关闭文件选择框时返回空预览而不是错误，前端据此不提示任何内容。
5. 删除不再使用的 `notice` 样式分支，以及 `agent-manage-error`、`agent-manage-applied`。**已完成**：`agent-manage-error`、`agent-manage-applied` 已删除（向导连接模型服务页最后一处保存失败也改为回执条）；`notice` 的四个色调仍被 §3.6 中保持在正文的提示使用，未删。`AppWindow` 的迁移横幅按 §3.6 改为一次性回执条（失败为失败级别，成功迁移为警告级别，原文在「详情」里），`.app-migration-notice` 已删除。

I-01、I-02 不依赖本方案，可以先行。

## 6. 验收

- 正文中不再出现成功提示；失败提示只出现在回执条、行状态标记或 §3.5 的预留位上。
- 正文因反馈而移动为零：在预览中对比触发前后主按钮的 `getBoundingClientRect()`，差值为 0。
- 回执条：成功 3 秒后消失、悬停时暂停；失败不自动消失；跨页跳转后仍在；同 key 替换。
- 深色模式、`prefers-reduced-motion`、560px 窄窗口下均检查一遍。
- 中英文文案同步；主句不超过 18 个字。
- `pnpm run test`、`pnpm run build` 通过。
