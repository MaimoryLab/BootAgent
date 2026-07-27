# 前端管理控制台改造计划

把「跑完一次向导就结束」的界面改成能长期使用的管理控制台。

**已完成**：首屏减负（`a55f597`）。总览首屏非内容占用从 55% 降到 23%，Agent 列表起始位置从 y=470 提到 197，五个 Agent 完整落在首屏。

**本计划涵盖余下四项**：Agent 品牌图标、侧边栏可用性与两个新页面、配置独立成页、视觉规范收敛。

范围限于 `frontend/`，不改 Python 内核，唯一的后端改动是修一处已发现的契约泄漏（见 0.4）。

## 0. 先确认的四个事实

**0.1 官方图标覆盖不全。** simple-icons（CC0）有 `claude`、`anthropic`、`opencode`、`cursor`，但 **Codex（openai）、Aider、Kilo 均为 404**。CC Switch 也只自带 `chatgpt.svg` 与 `claude.svg` 两个文件——它只管两个 Agent，我们有五个。因此采用**官方 + 自绘补齐**的混合方案（已确认）。

**0.2 禁止 CDN。** `CLAUDE.md:72` 规定前端产物不得含 CDN 或远程字体引用，`cdn.simpleicons.org` 不可引用，图标必须内联进 bundle。也不装 `simple-icons` 依赖——为两三个图标引入整包不合理，只取所需 path 内联并注明来源与 CC0。

**0.3 商标与 CC0 是两件事。** CC0 只覆盖 SVG 文件本身，不覆盖商标权。在自己 UI 内标示「这一行是哪个 Agent」属指示性使用；不得将第三方 logo 用作 OneAgent 的产品视觉资产、应用图标或营销素材。统一以 `currentColor` 单色渲染而非品牌色，既统一风格，也回避商标着色问题——这正是 macOS 的处理方式：形状各异，容器与尺寸一致。

**0.4 已发现一处契约泄漏（需修）。** `installer.py:1348` 把 `PROVIDERS` 常量整体写入 `status_payload`，导致上一轮新增的内部字段 `fallback_probe_model` 泄漏到 `/api/status`，而 `types/api.ts:78` 的类型定义并不包含它。这是探测模型改造引入的漂移。修法：`status_payload` 只投影 `name`/`home`/`base_url`/`anthropic_base_url` 四个公开字段，并补一个测试断言响应中不含 `fallback_probe_model`。

**0.5 没有深色模式。** 三个样式文件里 `prefers-color-scheme` 出现 0 次。本次**不新增**深色模式——那是独立的一项工作，混进来会让每条视觉调整都要验两套配色。计划中所有颜色沿用现有亮色 token。

## 1. Agent 图标

### 实现

新增 `frontend/src/components/icons/agents.tsx`，按 Agent ID 映射到图标组件：

| Agent | 来源 |
| --- | --- |
| Claude Code | simple-icons `claude`（官方，CC0） |
| OpenCode | simple-icons `opencode`（官方，CC0） |
| Cursor 及其他 guide-only | simple-icons 对应条目，有则用 |
| Codex | 自绘 |
| Aider | 自绘 |
| Kilo CLI | 自绘 |

未知 ID 回退到 lucide `Bot`，保证新增 Agent 不会渲染空白。

### 统一规范

- 视图框统一 `24×24`；渲染 `18`（列表）/`20`（详情页）。
- 单色 `currentColor`。官方图标多为实心填充，自绘为线条，两者视觉重量对齐的手段是：实心图标降到 `0.85` 不透明度，线条图标笔画宽 `1.8`（与 lucide 一致）。
- 复用现有 `.agent-icon` 容器（38×38 圆角方块）。

### 文字与可访问性

图标**替换**当前按 group 区分的通用图标（现在五个 auto Agent 共用一个 `Code2`，毫无辨识度）。Agent 名称**保留**为主标题——删掉名称会牺牲可访问性与可搜索性。图标 `aria-hidden`，容器加 `title` 提供悬停说明，内容是该 Agent 的一句话定位，不重复名称。

## 2. 侧边栏与两个新页面

### Bug 根因

`NavigationSidebar` 把 `/setup/provider` 和 `/setup/review` 当作独立页面，但它们是受 `SetupGuard` 保护的**向导步骤**：未选 Agent 时守卫重定向回 `/setup/agents`，所以点击像是没反应。这不是样式问题，是把流程步骤误当成了导航目的地。

### 新的导航结构

| 项 | 路由 | 内容 |
| --- | --- | --- |
| 环境总览 | `/overview` | 已有，Agent 列表 |
| Provider | `/providers` | **新增** |
| 配置模板 | `/profiles` | **新增** |

向导（`/setup/*`）从侧边栏移除，仅由「激活环境」主按钮与首次运行进入。原「Agent」项指向总览，与「环境总览」重复，删除。

### `/providers`

每个内置 Provider 一行：名称、OpenAI-compatible base URL、Anthropic-compatible base URL（若有）、**正在使用它的 Agent 列表**（由各 Agent 绑定反查，这是 per-agent 之后才有意义的信息）、注册页链接。

底部一个自定义 Provider 说明块，指出 Custom 由用户自行保证协议兼容——这条约束已在 ADR-003，界面应当复述。

数据全部来自 `status.providers` 与 `status.agents[*].provider`，无需新 API。

### `/profiles`

`profiles/` 模板的可视化管理：模板名、Provider、模型、`hasKey` 标记、创建时间、适用 Agent。操作为「应用到某个 Agent」和「删除」。

**安全约束**：模板可携带 Key，页面只能显示 `hasKey` 布尔，**绝不回显 Key 本身**。应用模板时 Key 由后端从 `secrets/` 读取（`--profile` 已有此能力），前端不接触。

空态给「从现有 Agent 配置创建模板」的入口——比让用户从零填表更符合真实使用顺序。

删除模板需要确认，且要说明删除不会改变已按该模板配置好的 Agent（模板只是模板）。

## 3. 配置独立成页

现在点「改配置」在卡片内展开面板，导致列表被撑长、多个面板可同时展开、状态散落在各行。

### 路由与拆分

新增 `/agents/:agentId`。总览的「改配置」改为跳转；整行可点击进入。

`AgentManageRow` 退化为**纯展示行**：图标、名称、当前指向、版本、状态徽章。移除内联面板、probe 状态与 apply 逻辑。

### 详情页

- **头部**：图标、名称、当前 Provider 与模型、版本状态、返回总览。
- **配置区**：Provider 分段、Base URL（custom 时）、模型、API Key、测试连接、应用。
- **信息区**（详情页才有空间放的东西）：配置文件路径、凭据文件路径、是否有备份、最后更新时间。这些目前无处可去。
- **应用结果**：重启指引与启动命令。

### 必须随代码一起搬走的四条行为约束

它们都有测试保护，平移时让测试跟着搬，不要删掉重写：

1. 探测通过才能应用（防止错误 Key 写入配置）。
2. 改动 Key 作废上次探测结论。
3. 应用后显示重启指引（Agent 启动时读配置，不说等于让用户以为失败）。
4. 应用后清空 Key 输入框（需 remount `SecureKeyField`，它按设计从内部 state 回显）。

### 收益

列表长度恒定；一次只有一个配置上下文；详情页容得下版本、备份、路径等信息。

## 4. 视觉规范收敛

现有 `tokens.css` 已是 macOS 风格（`#1d1d1f` 文字、`#007aff` 强调、`rgba(60,60,67,*)` 分隔线），继续沿用，不引入新色板。

### 字号收敛

实测现有 8 级：11px×14、12px×13、13px×9、14px×4、15px×2、16px、23px、26px。收敛到四级：

| 级别 | 用途 |
| --- | --- |
| 11px | 辅助信息、路径、时间戳 |
| 13px | 正文、列表项标题 |
| 16px | 分区标题 |
| 23px | 页面标题 |

12px 合并进 13px，14/15px 合并进 13 或 16，26px 归并到 23px。

### 层级只用一种手段

现在 `.agent-manage-row` 同时用了边框、白底、阴影三重区分（`app.css:937`）。改为**只靠留白与分隔线**：卡片去掉边框与阴影，行间用 1px 分隔线，容器统一白底。悬停时才给极轻的底色变化。

### 其余规则

- **一屏一个主操作**：详情页只有「应用」是 primary，其余 secondary 或纯文字按钮。
- **状态色只用于状态本身**，不作装饰；成功态用完即隐，不常驻（这正是首屏减负的原则，推广到全局）。
- **动效**只保留 ≤150ms 的 opacity 与 transform 过渡，不做位移动画。
- **触感一致**：所有可点区域最小高度 28px，圆角统一走 `--radius-control` / `--radius-panel`。

## 5. 测试先行

按既有约定，每步先写测试。

**单元（vitest）**
- `icons/agents.test.tsx`：五个 auto Agent 均有映射；未知 ID 回退；图标 `aria-hidden`。
- `AgentManageRow.test.tsx`：改造为展示行——断言不再渲染表单控件；点击触发导航。
- `AgentDetailPage.test.tsx`：平移第 3 节那四条约束。
- `ProvidersPage.test.tsx`：某 Provider 下正确列出使用它的 Agent；无 Agent 使用时的表述。
- `ProfilesPage.test.tsx`：列表、空态、**Key 不出现在 DOM 与浏览器存储**、删除需确认。

**契约（Python）**
- `test_server.py`：断言 `/api/status` 的 providers 不含 `fallback_probe_model`（0.4 的回归防护）。

**e2e（Playwright）**
- 侧边栏三项逐一点击，URL 与标题正确——直接覆盖第 2 节的 bug。
- 总览进详情，改配置后返回总览看到新指向。
- 列表长度不因操作而改变（第 3 节的动机）。
- 三视口无横向溢出（沿用 `expectNoHorizontalOverflow`）。
- 首屏预算测试已存在，保持通过。

覆盖率门禁不变：`src/api`、`src/state` ≥85%；Python 整体 ≥85%，`installer.py` 100% 分支。

## 6. 实施顺序

| 步 | 内容 | 截图确认 |
| --- | --- | --- |
| 1 | 契约泄漏修复（0.4）+ 图标集 | 是 |
| 2 | 侧边栏修复 + `/providers` + `/profiles` | 是 |
| 3 | `/agents/:agentId` 详情页，`AgentManageRow` 瘦身 | 是 |
| 4 | 视觉规范收敛（字号、层级、动效） | 是 |

每步独立提交，跑该步相关测试加 `npm run build`；每步结束截图确认后再推进下一步。第 1 步的两项虽都不大，但都是独立可验证的前置工作，合并为一次提交。

## 7. 风险

- **第 3 步动的是刚落地的 `AgentManageRow`**，四条行为约束必须随测试一起搬移，这是本计划最容易丢东西的地方。
- **第 2 步会影响既有 e2e**：`wizard.spec.ts` 有从侧边栏进入向导步骤的路径，需同步更新。上一轮移除横幅时已因类似原因让 4 个 e2e 变红，改断言的表达而非删除断言。
- **第 4 步是大面积样式改动**，回归靠三视口无溢出测试与截图，不靠肉眼扫一遍。
- **`/profiles` 涉及 Key**，见第 2 节安全约束。
- 新增图标逐个确认许可，见 0.3。
