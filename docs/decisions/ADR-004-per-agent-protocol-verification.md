# ADR-004：按 Agent 协议验证与 `PROTOCOL_UNSUPPORTED` 错误码

## Status

Accepted

## Date

2026-07-26

## Context

[ADR-003](ADR-003-three-platform-python-core-and-release-policy.md) 冻结了五个自动配置 Agent 与配置适配器映射，但连接测试始终只发一种请求：`POST <openai-base>/v1/chat/completions`。

这与 Agent 配置后的真实行为不一致：

| Agent | 配置写入的协议 |
| --- | --- |
| Codex | Responses（`installer.write_codex_config` 写死 `wire_api = "responses"`） |
| Claude Code | Anthropic Messages |
| OpenCode、Kilo CLI、Aider | OpenAI-compatible |

README 早已声明"同一个模型 ID 不一定同时兼容 OpenAI、Anthropic 和 Responses 协议"，但这一约束没有进入任何代码路径。

2026 年 7 月 26 日对一个 OpenAI-compatible 中转端点的 36 个文本模型实测（跳过 24 个图像、视频、语音与 embedding 模型，因为用对话载荷调用它们可能触发单独计费的生成任务）：

| 协议 | 通过 |
| --- | --- |
| Chat Completions | 31 / 36 |
| Anthropic Messages | 23 / 36 |
| Responses | 10 / 36 |

在 30 个能明确判定的模型中只有 10 个支持 Responses。端点以两种形式明确拒绝，均可检测：

- `400 INVALID_REQUEST_BODY`，消息含 `does not support endpoint: responses`
- `500`，消息含 `not implemented`

因此原实现存在一条可复现的失败路径：用户选择一个只支持 Chat Completions 的模型 → 连接测试通过 → OneAgent 写入 Codex 配置 → Codex 首次请求失败，且 OneAgent 的输出中没有任何线索指向根因。

## Decision

### 协议映射

每个 Agent 的推理协议由 `agents.lock.json` 的 `config_adapter` 推导，映射表位于 `oneagent/catalog.py`，与配置写入使用同一来源，避免两处漂移。未登记的适配器回退为 OpenAI-compatible。

### 验证时机

`install_many` 在写入任何配置**之前**，对所选 Agent 涉及的每种协议各发一次最小请求。相同协议只探测一次。`--skip-test` 与 `--check-agent-only` 维持原语义，跳过探测。

### 失败处理

探测到协议不兼容时，该 Agent 以 `PROTOCOL_UNSUPPORTED` 失败，**不写入配置文件，也不写入环境摘要**。不提供"忽略警告继续写入"的路径：写出一份注定失败的配置，只会把错误从 OneAgent 转移到 Agent 内部，且失去可读的错误信息。

### 错误码

新增 `PROTOCOL_UNSUPPORTED`，退出码 `7`。这是对 [ADR-003](ADR-003-three-platform-python-core-and-release-policy.md) 错误契约的**增量扩展**：不修改、不复用任何既有错误码，既有客户端的行为不变。

该错误 `retryable = false`。配额超限（`429`）、上游过载（`503`）和超时仍按原有的可重试语义处理——繁忙的上游不等于不支持该协议的模型。

### 本地 API

`POST /api/probe` 接受可选的 `agents` 数组，按其协议逐一探测，并在 `protocols` 字段返回每种协议的结果。未提供 `agents` 时维持原有的 OpenAI-compatible 单协议行为，保证旧客户端兼容。

## Consequences

- 用户在写入配置前就会知道模型与 Agent 不兼容，而不是在 Agent 内部遇到无上下文的错误。
- 同一个模型对不同 Agent 的判定相互独立：一个模型可能被 Codex 拒绝、同时正常配置 OpenCode。
- 一次多选 Agent 的激活最多发出三次探测请求，而非一次。
- `/v1/models` 返回的清单仍未按协议过滤或标注，用户仍可能选中非对话模型；此时探测必然失败，因此不会写出损坏配置，但错误信息表述为"不支持该协议"而非"这不是对话模型"。该问题单独跟踪。
- 正式 Release Candidate 仍须以真实 Agent 首次请求为门禁；本地探测不能替代 PPIO 与 Novita 的正式验收。
