# CC Switch 参考笔记

对 [CC Switch](https://github.com/farion1231/cc-switch) 的一次定向调研，用途是为 OneAgent 的 Agent 配置界面找可借鉴的做法。**只做参考，不移植代码**——两者许可、技术栈与产品定位都不同。

调研版本：`cc-switch` 3.18.0（commit `708b387`，2026-07-27），MIT。克隆在 `output/reference/cc-switch`，该目录被 `.gitignore` 忽略，不进仓库。

## 1. 两者定位差异（先说清，避免误抄）

| | CC Switch | OneAgent |
| --- | --- | --- |
| 形态 | Tauri 桌面应用（Rust + React） | 本地 HTTP + React，Python 标准库内核 |
| 职责 | Provider 配置的**存取与切换** | Agent 的**检测、安装、配置** |
| 配置模型 | 整块存原始配置对象 | 结构化字段，由适配器翻译 |
| 外围能力 | 本地代理、熔断器、故障转移、用量统计、MCP、skills、prompts、sessions | 无 |
| 单表单规模 | `ClaudeFormFields.tsx` 1126 行；`ProviderForm.tsx` 2693 行 | `AgentDetailPage.tsx` 218 行 |

它的 `src/components/` 下有 19 个子目录（mcp、skills、prompts、sessions、proxy、usage…）。这些不在 OneAgent 的产品边界内（见 [产品边界基线](product-boundary-baseline.md)），尤其**本地代理与故障转移属于明确禁止范围**，不予借鉴。

## 2. 配置模型：它不解析语义，我们解析

CC Switch 的 `Provider`（`src/types.ts:11`）核心只有一个配置字段：

```ts
settingsConfig: Record<string, any>  // Claude 为 settings.json；Codex 为 { auth, config }
```

其余全是元数据：`icon` / `iconColor` / `category` / `sortIndex` / `notes` / `isPartner` / `inFailoverQueue`。

**含义**：它整块存 Agent 的原始配置，不理解字段语义，因此用户需要自己懂 `settings.json` 的结构。

**更正**：先前记录的「CC Switch 从不安装 Agent」不准确。`src-tauri/src/commands/misc.rs:452` 起有一键安装，OpenClaw 走 `npm i -g openclaw@latest`，Hermes 走 `NousResearch/hermes-agent` 的官方 install.sh。这不改变 OneAgent 仍将两者列为 guide-only 的结论——理由是常驻网关形态被 ADR-002/ADR-003 划在范围外，与能否安装无关。

**OneAgent 的做法相反且更适合当前定位**：`agents/<id>.json` 存 `provider` / `model` / `base_url` 等结构化字段，由 5 个适配器翻译成各 Agent 的格式。用户不需要知道目标文件长什么样。这个差异是设计选择，不是我们缺功能，**不要向它靠拢**。

字段量级对比：它的 Claude 表单认 12 个 `ANTHROPIC_*` 环境变量（含 Opus / Sonnet / Haiku / Fable 分模型指定），我们只写 4 个。

## 3. 值得借鉴：官方图标来源

CC Switch 依赖 **`@lobehub/icons-static-svg`**（MIT，723 个 AI 品牌 SVG）。核对结果：

| 我们需要的 | lobehub 是否提供 |
| --- | --- |
| Codex（openai） | **有** `openai.svg` |
| Claude Code（claude / anthropic） | 有 |
| Cursor（guide-only） | 有 |
| OpenCode | 无 |
| Aider | 无 |
| Kilo CLI | 无 |

`openai.svg` 的形态与 OneAgent 的图标规范完全一致，可直接内联：

```
<svg fill="currentColor" viewBox="0 0 24 24" ...>
```

这把官方图标覆盖从 2/5 提到 3/5，Codex 不必再自绘。OpenCode 沿用 simple-icons（CC0）已有的官方图标，只有 Aider 与 Kilo 仍需自绘。

### 更正：八个 Agent 全部有官方图标

先前判断「Codex / Aider / Kilo 无官方图标」是错的，那只说明 simple-icons 里没有。逐个核对各项目自己的站点与仓库后，八个全都有：

| Agent | 官方来源 |
| --- | --- |
| Codex | lobehub `openai.svg`（MIT） |
| Claude Code | `claude.ai/favicon.svg`（品牌橙 `#D97757`） |
| Cursor | `cursor.com/favicon.svg` |
| OpenCode | lobehub `opencode.svg`（MIT） |
| OpenClaw | 官方仓库 `docs/assets/pixel-lobster.svg`（MIT） |
| Hermes | `hermes-agent.nousresearch.com/icon.png`（48px，无矢量版） |
| Kilo CLI | `kilocode.ai/favicon/favicon.svg` |
| Aider | `aider.chat/assets/icons/favicon-32x32.png` |

因此不再有任何自绘图标。**结论：判断某个品牌「没有官方图标」之前，要查该项目自己的站点，而不是只查一个图标集。**

两个资产不能直接用，需换等价版本：`openai.com/favicon.svg` 与 `opencode.ai/favicon.svg` 都带 `:root` CSS 变量和 `prefers-color-scheme` 媒体查询，内联进宿主文档会污染全局样式；`aider.chat/assets/logo.svg` 是 200×60 的文字标加高斯模糊滤镜，不是方形图标。

**OpenClaw 的标志是龙虾，不是螃蟹。** CC Switch 的手绘稿容易让人误解形象——这也是不该以它为准的另一个理由。

### OpenClaw 与 Hermes 的图标不可取用

核对 `src/icons/extracted/`（98 个文件）的结果：

- **OpenClaw** 是 CC Switch **自绘**的彩色 SVG，`viewBox="0 0 120 120"`，用 `linearGradient` 从 `#ff4d4d` 渐变到 `#991b1b`，配青色眼睛。不是官方 logo。
- **Hermes** 是一张 256×256 PNG（39 KB），不是矢量。

两者都无法满足 24×24 单色 `currentColor` 的规范：渐变填充无法继承 `currentColor`，位图在 18px 渲染位与矢量字形并排时轻重不一。因此这两个按同一几何规范自绘，而不是取用 CC Switch 的资产。这不是许可问题（MIT 允许取用），是形态不兼容。

**注意**：CC Switch 自己并未真的使用这个包——源码零引用，实际只在 `src/assets/icons/` 放了手工的 `chatgpt.svg` 与 `claude.svg`。这个依赖是装了没用，所以「它用了什么」不能作为可用性证据，必须自己核对。

**许可与商标**：MIT 覆盖 SVG 文件本身，不覆盖商标权。在自己 UI 内标示「这一行是哪个 Agent」属指示性使用；不得用作 OneAgent 的产品视觉资产。统一 `currentColor` 单色渲染，既统一风格也避免为第三方商标着色。

## 4. 值得借鉴：高级项用折叠，不做模式切换

`ClaudeFormFields.tsx:800` 的做法：

```tsx
<Collapsible open={advancedExpanded} onOpenChange={setAdvancedExpanded}>
  <CollapsibleTrigger asChild><Button>…</Button></CollapsibleTrigger>
  {!advancedExpanded && <p className="text-xs">{t("providerForm.advancedOptionsHint")}</p>}
  <CollapsibleContent>…</CollapsibleContent>
</Collapsible>
```

两个细节值得抄：

**折叠而非模式切换。** 默认收起、需要时展开，比切换「简单／高级」模式轻，也不会让用户困在错误的模式里出不来。

**收起时显示一行说明里面有什么**，而不是只留一个光秃的三角。它的原文是：

> 包含 API 格式、认证字段、模型映射等配置。大多数场景下保持默认即可。

最后一句尤其值得学：明确告诉用户「不用管」，而不是让人担心自己漏配了什么。

它折叠区里的字段：API 格式（Anthropic / OpenAI Chat / OpenAI Responses）、认证字段名（`ANTHROPIC_AUTH_TOKEN` vs `ANTHROPIC_API_KEY`）、模型映射与显示名、分模型指定、一键设置。

### OneAgent 未来的候选高级项

按同一收纳方式，优先级从高到低：

1. **认证字段名切换**——已有真实差异（Claude 系可读两个不同变量名）。
2. **分模型指定**——Opus / Sonnet / Haiku 各自的模型名，Claude Code 支持。
3. 请求超时。
4. 自定义 User-Agent。

现阶段都不做。当前 218 行的详情页方向正确，加高级项前应先有真实需求。

## 5. 值得借鉴：预设机制（方向与我们相反）

它为每个 Agent 备了 `src/config/*ProviderPresets.ts`（Claude、Codex、Gemini、OpenCode 各一份），用户选预设而非从零填写。预设的最小形态：

```ts
{
  name: "Claude Official",
  websiteUrl: "https://www.anthropic.com/claude-code",
  settingsConfig: { env: {} },
  isOfficial: true,
  category: "official",
  icon: "anthropic",
}
```

**与 OneAgent 的 `profiles/` 模板方向相反**：它的预设是**内置的服务商清单**（我们出，用户选），我们的模板是**用户自存的组合**（用户出，用户复用）。两者不冲突，可以并存——内置预设降低首次配置成本，用户模板降低重复配置成本。

若将来要做内置预设，注意 `isPartner` / `primePartner` / `partnerPromotionKey` 这类商业合作字段**不要引入**：[产品边界基线](product-boundary-baseline.md) 第 8 节要求公开权益文案不得承诺固定额度，带推广属性的预设排序会踩这条线。

## 6. UI 布局：与我们已高度一致

`ProviderCard.tsx` 的结构是**图标 + 主信息 + 右侧操作 + 底部分隔线**，与 OneAgent 刚落地的 `AgentManageRow` 基本同构，无需调整。

它多出的部件是我们当前不需要的：健康状态指示器（`HealthStatusIndicator`）、故障转移优先级徽章（`FailoverPriorityBadge`）、拖拽排序（`@dnd-kit`）。前两个依赖本地代理，属于边界外。

## 7. 结论

**采纳**：

- lobehub 的 `openai.svg` 替换自绘的 Codex 图标（第 3 节）。
- 高级项一律用折叠收纳，收起时带一行说明（第 4 节）。

**明确不采纳**：

- 整块存原始配置 —— 我们的结构化模型更适合「用户不必懂目标格式」的定位。
- 本地代理、熔断器、故障转移 —— 产品边界禁止。
- 商业合作伙伴字段与推广排序 —— 与公开权益文案约束冲突。
- 拖拽排序、健康徽章 —— 当前无对应需求。

相关文档：[前端管理控制台改造计划](frontend-management-console-plan.md)、[产品边界基线](product-boundary-baseline.md)、[CC Switch 可选配置说明](ai-agent-kit/tools/cc-switch.md)。
