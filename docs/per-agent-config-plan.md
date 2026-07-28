# 选择页排序修复，与详情页分化的评估

起因是两个诉求：激活环境页没把重要 Agent 排在前面；每个 Agent 的配置界面完全一致。参考对象是 CC Switch。

结论先行：**只做选择页排序修复。** 详情页按适配器分化经核对后不成立——它要暴露的多数是内部实现差异，而隐藏这些差异正是 OneAgent 的价值。唯一值得单独评估的是 Claude Code 的快速小模型字段。

调研版本 `cc-switch` 3.18.0（commit `708b387`），MIT，克隆在被忽略的 `output/reference/cc-switch`。背景见 [CC Switch 参考笔记](cc-switch-reference-notes.md)。

## 1. CC Switch 的表单结构

它的形态是三层，不是「每个 Agent 一套独立表单」：

```
共享 ProviderForm 编排器（2693 行）
  + 共享字段组件与 hooks
      hooks/    24 个文件 4733 行（useOpencodeFormState、useHermesFormState、useCodexConfigState…）
      shared/   5 个文件 410 行（ModelInputWithFetch、EndpointField、ApiKeySection、ModelDropdown）
      helpers/  166 行
      BasicFormFields 177 · ApiKeyInput 69 · ProviderAdvancedConfig 182
  + 各应用专属 FormFields 区块
      OpenCode 1148 · Codex 1107 · Claude 1127 · OpenClaw 673 · Hermes 569 · Gemini 213
```

共享层本身接近 6000 行，专属区块坐在它上面。这与「共用骨架 + 按需分区块」是同一种形态。

几点需要说准：

- **`OmoFormFields`（1321 行）不是同级 Agent。** `types.ts:8-9` 写的是 `"omo" // Oh My OpenCode` 与 `"omo-slim"`，属 OpenCode 下的配置类别。
- **Gemini 表单不只有 OAuth。** 官方 Google 路径走 OAuth，自定义或第三方 Gemini Provider 仍可填 Key、Endpoint、模型并拉取模型列表。213 行是因为它复用共享字段，不是因为没什么可填。
- **Codex 的上游格式三选不是 `wire_api` 三选。** 源码明确标注 Codex 的 `wire_api` 固定为 `responses`；三选属于它的代理转换层——而本地代理在 [产品边界基线](product-boundary-baseline.md) 里是明确禁止范围。
- **它并非「用户必须自己懂原始格式」。** `settingsConfig` 是原生 JSON 对象存储，但 UI 与 Service 层仍会理解、校验和编辑字段语义。参考笔记第 2 节的表述过头了。

**正确的借鉴原则**：只有当用户面对一个真实的、可选择的语义需求时才分化 UI。**底层配置文件不同本身不构成分化理由。**

## 2. 我们的适配器差异，哪些是用户的选择

`_write_agent_config`（`oneagent/installer.py:399`）分派四个适配器，写的东西确实不同：

| Agent | 目标 | 密钥去处 | 模型 |
| --- | --- | --- | --- |
| Codex | `config.toml` 的 `[model_providers.oneagent]` | env 文件，经 `env_key` 间接引用 | `model` 单值 |
| Claude Code | `settings.json` 的 `env` | 写进配置文件（`ANTHROPIC_AUTH_TOKEN`） | `ANTHROPIC_MODEL` + `ANTHROPIC_SMALL_FAST_MODEL` |
| OpenCode / Kilo | `provider.oneagent` + `models` 映射 | env 文件，`{env:...}` 占位 | `oneagent/<model>` |
| Aider | shell / PowerShell 脚本 | 写进脚本（`secret=True`） | 不写，靠启动参数 |

按第 1 节的原则筛一遍，**只有一项是用户的选择**：

- **Claude Code 的 `ANTHROPIC_SMALL_FAST_MODEL`**（`installer.py:384-385`）现在被强制等于主模型。「主模型 + 快速小模型分别指定」是一个真实能力，用户可能确实想让小模型更便宜。这是唯一值得建 UI 的差异。

其余三项都只是内部实现，不该出现在界面上：

- 密钥最终写到配置文件还是 env 文件；
- 模型放在配置文件还是启动参数；
- 用哪种 env 占位格式（`env_key` / `{env:...}` / `export`）。

用户对这些没有选择权，讲出来只是增加认知负担。**隐藏它们正是产品价值。**

## 3. 先前判断中已修正的错误

记下来避免重复踩：

- **「Aider 留空会使启动命令缺模型参数」错。** `install_many` 与 `activate_agent` 都在写任何东西之前无条件补齐模型（discovery，失败则 `fallback_probe_model`）。后端拿到的 `model` 永远非空，「留空则由端点自动选择」对 Aider 同样成立。误推的环节是漏掉了 resolve 这一步。
- **「只有 Codex/OpenCode/Kilo 需要 source」错。** `_next_step` 里 Aider 也要 `source ~/.oneagent/aider.env && aider --model openai/<model>`。**只有 Claude Code 是裸 `claude`。**
- **「详情页对所有 Agent 给出相同启动说明」错。** 输入表单是共用的，但 `restart` 与 `next` 由后端按 Agent 生成，详情页直接显示 `applied.restart` / `applied.next`，结果区本就是分化的。
- **「rank 完全没用上」错。** `catalog.py` 已按 rank 排序输出，选择页拿到的就是排好的数组。准确的说法是 rank 没有参与「首屏还是折叠」的决定。
- **「guide-only 必须不可勾选」错，且会破坏现有流程。** `installer.py:1079-1082` 对 `config_mode == "guide"` 返回 `status: "guide-only"`，把 `meta["guide"]` 放入 `next_steps`，不装包、不写私有配置；Review 页显示「显示引导」。**合规要求是「不自动配置」，不是「不允许选择」。**

## 4. 要做的：选择页排序修复

`AgentSelectionPage.tsx:13` 以 `group === "auto"` 决定首屏，`:14-20` 把其余按 `group.id` 塞进「更多分类」折叠。结果 Kilo（rank 8）、Aider（9）在首屏，而 Cursor（3）、OpenClaw（5）、Hermes（6）被折叠。总览页 `EnvironmentOverviewPage.tsx:35-37` 已按 rank 排并以 `PRIMARY_RANK_LIMIT = 6` 划线，选择页没跟上。

改动：

- 把 rank 排序与首屏阈值抽到 `frontend/src/state/ranking.ts`，选择页与总览页共用，避免第三次各写一遍。
- 选择页改用统一排序划分首屏与折叠区，删掉 `automatic` / `additionalGroups` 两个 `useMemo`。
- 首屏标题从「可一键配置」改为「常用 Agent」——按 rank 混排后首屏含 guide-only，原标题不再成立。折叠区标题改为「更多 Agent」。
- **guide-only 保持可勾选**。`AgentRow` 的 `disabled` 条件不动（仍只在 `platforms.length === 0` 时禁用）。注意「仅引导」徽标只在该 Agent **未安装** 时出现——`AgentRow` 的状态优先显示「已安装」。本机已装 Cursor 时首屏三个 guide-only 行都显示「已安装」，说明这一点的是行内那句「显示官方安装与配置步骤」，断言应该盯它而不是徽标。

测试：

- `AgentSelectionPage.test.tsx`（新建）——断言渲染顺序为 rank 顺序、Cursor 在 Kilo 之前；断言 guide-only 行的 checkbox 未禁用；mock 的 catalog 故意逆序传入，确保页面不依赖服务端顺序。
- `ranking.test.ts`（新建）——排序、并列按 id、`undefined` 视作空、阈值取等号侧。
- `wizard.spec.ts` 已有 rank 齐全的 mock（`:15-20`），补首屏顺序与 guide-only 可勾选断言。

改完开 `8765` 用真实 DOM 复查顺序与勾选态。

## 5. 暂缓：按 `configAdapter` 拆详情页

按第 2 节的筛选结果，Codex / OpenCode / Kilo / Aider 的「专属区块」内容都是内部实现说明，不建。不启动 `configAdapter → 多区块` 体系，也不为此改 `AgentCatalogItem` 契约。

## 6. 待评估：Claude Code 双模型字段

若产品确认支持「主模型 + 快速小模型」，落点是现有 `AdvancedSection` 里增一个 Claude Code 专属可选字段，留空回退到主模型。涉及 `write_claude_config` 与 `activate_agent` 增一个可选参数、activate 端点与 `types/api.ts` 同步，以及 `installer.py` 100% 分支门禁要求的两侧用例（给值 / 留空）。

**仅这一个字段不需要建立完整的分区块体系。** 先确认产品是否要这个能力，再决定是否实施。

## 7. 明确不做

- CC Switch 的上游格式三选、prompt cache 路由、reasoning 档位、四项分价、任意 headers 与 extra options —— 我们的适配器不写这些，建了 UI 就是空承诺；其中上游格式三选依赖它的代理转换层，属产品边界禁止范围。
- 每 Agent 独立表单组件 —— 我们四个适配器的共用部分远大于差异部分。
- guide-only Agent 的配置界面 —— 两个页面上都只提供官方引导入口。
- 改 `rank` 数值 —— 当前排序是上一轮的决定。

相关文档：[CC Switch 参考笔记](cc-switch-reference-notes.md)、[产品边界基线](product-boundary-baseline.md)、[前端管理控制台改造计划](frontend-management-console-plan.md)。
