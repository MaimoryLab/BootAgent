# 界面友好度改造方案：少一点小字，让新手看得懂

> 状态：已确认，按阶段实施。起草于 2026-10-10，代码核对至 `main`（`3d335e0`）。
> 文中的现状均带本仓库 `file:line`；字号统计来自 `frontend/src/styles/app.css`。

## 1. 目标与非目标

目标：第一次打开 BootAgent 的人，不需要懂配置文件、环境变量和 API 协议，也能看懂每个页面在说什么、下一步该点哪里。

非目标：

- **不换配色和字体。** [ADR-010](decisions/ADR-010-visual-restyle-swiss-blueprint.md) 的配色改版这次不做（2026-10-10 决定）。扫描器关于配色的提示（见 §3.5）记录在案，不处理。
- 不删功能，不改后端行为。技术细节从「默认展示」改为「展开可见」，信息一条不丢。
- 不弱化安全提示。[ADR-011](decisions/ADR-011-provider-keys-for-multimodal-skills.md) 要求安装确认告知 Key 对 Agent 可见，这类提示只缩短，不删除。

## 2. 原则

1. **默认只回答三个问题**：这是什么，现在什么状态，下一步点哪里。
2. **技术细节收进「详情」**：文件路径、环境变量名、协议名、Skill ID、程序路径、安装目录，默认折叠在每一项自己的「详情」里，开发者一键展开。不做全局的「开发者模式」开关：开关藏得深，新手不会开，老手每次都要找。
3. **一件事一个状态**：同一位置不并排两个徽标。能用一个词说清的状态，不写成一句话。
4. **一句话说清，长解释收起来**：说明文字最多一句，超过的部分放进「了解详情」。
5. **用任务语言，不用实现语言**：说「让 Codex 也能用这个模型服务」，不说「把 Responses 请求转换为 Chat Completions」。
6. **风险和花钱的事必须看得见**：删除、覆盖、计费、Key 可见性，保留在默认视图，压成一句。

## 3. 现状

### 3.1 字号

没有统一的正文字号：页面说明和区块说明各自写 13px（`frontend/src/styles/app.css:202`、`441`），按钮也是 13px。`app.css` 中另有 127 处小于 13px 的声明：

| 字号 | 声明数 | 典型用途 |
| --- | --- | --- |
| 10px | 13 | `.provider-title small`、`.profile-title small`、`.runtime-fact small`、`.agent-manage-identity-copy small` |
| 11px | 47 | 路径 `code`、徽标、元信息 |
| 12px | 67 | 提示文字、下拉框、说明 |

中文在 12px 以下明显难读，10px 已经低于大多数桌面应用的下限。

### 3.2 默认展示的技术细节

全部文案共 964 条，其中 88 条直接出现路径、变量名、协议名或「配置文件」等实现概念。主要位置：

- 配置文件路径：`frontend/src/components/AgentManageRow.tsx:99`（「配置文件当前指向 {url}」）、`frontend/src/components/DesktopAppSection.tsx:226`（「与 {name} 共用配置文件 {path}」「配置文件：{path}」）。
- 程序路径：`frontend/src/components/AgentManageRow.tsx:449`。
- 运行时安装目录与 PATH：`frontend/src/components/RuntimeSection.tsx:145`。
- 多模态：Skill ID（`frontend/src/components/MultimodalSection.tsx:334`）、环境变量名与私有 Key 文件路径（安装弹窗）、笔记路径、说明文件路径（`MultimodalSection.tsx:617`）。
- 模型服务表单：「OpenAI 兼容 Base URL」「Anthropic 兼容 Base URL」。
- 协议适配：约 30 条文案直接使用 Responses、Chat Completions、Anthropic Messages。

### 3.3 说明过长

- 多模态安装弹窗：4 段说明（Key 写入位置、私有文件、Node 版本与计费、探索模式三段）。
- 卸载确认、全量清理、npm 环境卸载：整段长句。
- 协议适配页的开头说明：两段，各三到四句。
- 多模态账号行：「证据来源」与「安装状态」两个徽标并排。

### 3.4 信息最密的页面

按含辅助文字的元素数（`<small>` 与 note/hint/detail 类）：

| 位置 | 数量 |
| --- | --- |
| `frontend/src/pages/MarketplaceDetailPage.tsx` | 57 |
| `frontend/src/components/AgentManageRow.tsx` | 14 |
| `frontend/src/components/SkillhubDetailSection.tsx` | 8 |
| `frontend/src/components/MultimodalSection.tsx` | 8 |

### 3.5 「AI 味」扫描基线

用 `avoid-ai-design` 的扫描器（见 §7）对 `frontend/src` 的结果：P0 0 条，P1 20 条，P2 3 条。

| 编号 | 条数 | 内容 | 处理 |
| --- | --- | --- | --- |
| I3 | 18 | 常见 Lucide 图标组合；`Sparkles` 用作 Skills 图标（`SkillsPage.tsx:149`、`TransferPage.tsx:416`）和推荐按钮（`MarketplacePage.tsx:725`） | 第 1 阶段换掉 `Sparkles`；`Check` 等通用图标保留 |
| SD4 | 1 | 全大写加字距的标签（`app.css:2077`、`3607`、`3671`） | 第 2 阶段改为常规大小写 |
| SD4d | 1 | 用「·」拼接的元信息（`TaskCenter.tsx:39`、`52`、`53`） | 第 1 阶段改为分行或标签 |
| SD5 | 1 | `PageScaffold.tsx:47` 标题旁的徽标 | 误报：是独立徽标，不是标题里的强调词，保留 |
| C8 | 1 | 主色与强调色色相过近（`tokens.css`） | 属于配色，本次不做 |

扫描器只看代码能确定的部分。奶油底加赭红的配色属于视觉层面，扫描器看不出来，也不在本次范围。

## 4. 文案规则与用词表

规则：

- 一条提示最多一句。需要第二句时，第二句以后放进「了解详情」。
- 不在默认视图出现：文件路径、`~/`、环境变量名、协议名、Skill ID、哈希、程序路径。
- 状态用短词：可以直接安装 / 需要先测试 / 暂不支持 / 已安装。
- 按钮写动作：「安装」「测试连接」「去添加模型服务」，不写「确定」「继续」。

用词表（中文界面）：

| 现在 | 改为 | 说明 |
| --- | --- | --- |
| Base URL | 接口地址 | 字段旁给出示例占位 |
| OpenAI 兼容 Base URL | 接口地址（OpenAI 格式） | |
| Anthropic 兼容 Base URL | 接口地址（Anthropic 格式） | |
| 协议适配 | 兼容模式 | 说明写「让只认某种格式的 Agent 也能用这个模型服务」 |
| Responses / Chat Completions / Anthropic Messages | 不出现在默认视图 | 放进「详情」 |
| 配置文件 | 不出现在默认视图 | 需要时说「Agent 的设置」 |
| 运行时 | 运行环境 | 如 Node.js |
| 实测 | 测试 | 按钮仍提示可能产生费用 |
| 按模型名推断 / 模型服务声明 / 内置目录 | 不单独展示 | 并入一个状态，来源放进「详情」 |

英文界面同步改写，原则相同。

## 5. 公共做法

### 5.1 「详情」折叠

`AgentManageRow` 已有 `agent-manage-details` 折叠，抽成一个公共组件，供所有卡片和行使用：

- 默认收起，标题固定为「详情」。
- 内容是键值列表：「设置文件」「程序位置」「安装目录」「接口地址」等，路径用等宽字体，可复制。
- 键盘可操作，展开状态不跨页面保存。

### 5.2 「了解详情」

弹窗和说明里第二句以后的内容，用同一个小链接展开，展开后原地显示，不跳页。

### 5.3 字号层级（第 2 阶段）

| 角色 | 字号 |
| --- | --- |
| 正文、表单、按钮 | 14px |
| 辅助说明 | 13px |
| 徽标、计数、等宽路径 | 12px（下限） |

在现有 CSS 不变量测试旁加一条：`app.css` 与 `base.css` 中不得出现小于 12px 的字号。

## 6. 分阶段

每个阶段内，每个页面一个 PR，独立可回退。

### 第 1 阶段：信息层级（按新手路径）

| 顺序 | 页面 | 主要改动 |
| --- | --- | --- |
| 1 | 总览、Agent 行（`AgentManageRow`、`DesktopAppSection`、`RuntimeSection`） | 路径、程序位置、安装目录进「详情」；「配置文件当前指向 {url}」改为「正在使用：{模型服务}」，有偏差时只提示「设置被其他程序改过」；卸载类确认压成一句加「了解详情」 |
| 2 | 配置向导（`setup/*`） | 每步一句说明；协议名移出默认视图 |
| 3 | 模型服务、配置模板 | 按用词表改字段名；示例占位；一条提示一句 |
| 4 | 多模态 | 账号行合并为一个状态徽标；安装弹窗压成「会使用你在 {服务} 的 Key，每次调用按价格计费」一句，其余进「了解详情」；Skill ID、环境变量、文件路径进「详情」 |
| 5 | 兼容模式（原协议适配） | 已扩大为动线改造（2026-10-12）：在出问题的地方提出兼容模式——向导测试连接、Agent 页选模版、检测模型失败；命令行 Agent 一键开启（`ConfigureAgentWithConversion`）；兼容模式页改为状态页；下拉框隐藏转换器条目并写明不可选原因 |
| 6 | Skills、MCP | 同上规则；`Sparkles` 换为与 Skill 含义相符的图标 |
| 7 | Marketplace 详情 | 元信息分组，次要字段进「详情」 |
| 8 | 设置、导入导出 | 同上规则 |

### 第 2 阶段：字号和间距

按 §5.3 调整，并加 CSS 不变量测试；全大写标签改为常规大小写。

## 7. 工具

项目内安装了两个第三方 Skill，均固定版本，详见各自的 `VENDORED.md`：

- `.claude/skills/anti-ui-slop/`（Apache-2.0，固定在 `github/awesome-copilot@82701c2`）：做每个页面时加载一个对应的手册——第 1 阶段用 `reference/distill.md`（精简），收尾用 `reference/polish.md`。已移除付费 MCP 相关内容。
- `.claude/skills/avoid-ai-design/`（MIT，固定在 `funboy322/avoid-ai-design@2b7c718`）：以 `detect` 模式使用（只审查、不重写），并用其扫描器检查代码层面的问题：

```bash
node .claude/skills/avoid-ai-design/scripts/detect.mjs --min=P1 frontend/src
```

配色相关的提示（C8 等）按 §1 不处理。扫描器目前不进 CI：它的大部分规则面向营销页，误报（如 §3.5 的 SD5）需要人工判断。

## 8. 验收

每个页面的 PR 都要满足：

- 默认视图不出现路径、环境变量名、协议名、Skill ID。
- 页面上每条提示不超过一句；风险和计费提示仍在默认视图。
- 原有信息在「详情」或「了解详情」中都能找到。
- 中英文文案同步。
- `pnpm run test`、`pnpm run build` 通过；新增或更新的测试覆盖「默认不显示、展开可见」。
- 用预览检查：计算后的字号、窄窗口（560px）下无横向溢出、深色模式。
- 扫描器对改动文件不新增 P1。
