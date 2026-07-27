# ADR-006：多 Profile 与长期环境管理

## Status

Accepted

## Date

2026-07-27

## Context

OneAgent 目前是一次性向导：激活完成即结束，`~/.oneagent/profile.json` 只保存单一激活态（`schema_version: 1`：一个 provider、一个 model、一份 agent 列表），`EnvironmentOverviewPage` 是只读页面。激活之后用户的长期需求没有载体：

- 在多个 Provider（或同一 Provider 的多个模型）之间切换，必须重走一遍七页向导。
- Agent 版本落后于 `agents.lock.json` 的锁定版本时（`status_payload` 已能对比出 `version` 与 `lockedVersion`），界面没有任何提示。
- 写入配置时 `atomic_write` 已经产生 `*.backup-<ts>` 备份，但用户看不到、也无法回滚。

用户事实上在用 CC Switch 做 Profile 切换（见 [CC Switch 配置指引](../ai-agent-kit/tools/cc-switch.md)）。这说明切换需求真实存在，CC Switch 文档也记录了一条关键教训：**切换配置后 Agent 不会自动重新加载**，不能显示"已切换"就了事。

## Decision

### 存储布局

```text
~/.oneagent/
  profile.json          # schema_version: 2，当前激活指针 {active: <id>}
  profiles/             # 每套配置一个文件，不含 Key
    <id>.json
  secrets/              # 每套配置的密钥文件，0600 / Windows ACL
    <id>.env
  env / env.ps1         # 固定路径投影：当前激活 profile 的密钥
```

- Profile 记录：`id`、`label`、`provider`、`api_base_url`、`model`、`agent_ids`、`created_at`、`last_activated_at`。**Key 不进 profile 文件**（产品边界与 CLAUDE.md 硬约束）。
- `id` 是受限 slug（`[a-z0-9][a-z0-9_-]*`），非法输入返回 `INVALID_REQUEST`。
- `secrets/<id>.env` 保存该 profile 模板的 Key 与 Base URL。Agent 实际读取的凭据位于 `~/.oneagent/agents/<agent-id>.env`，变量名按 Agent 区分（见下方修订小节）；`~/.oneagent/env` 保留共享的 `ONEAGENT_API_KEY`，仅作为旧配置的兼容层。

### v1 → v2 迁移

读到 `schema_version: 1` 的 `profile.json` 时自动迁移：先 `backup_file` 备份原文件，再把内容转成 `profiles/default.json` 并写入 v2 指针。任何情况下不对旧文件直接报错。迁移必须由测试固定（"读 v1 文件"用例），防止后续重构悄悄破坏老用户。

### 写入与切换

- 向导激活（`install_many` 收尾的 `write_profile`）变为"更新或创建当前激活 profile"：同一 `provider + model` 沿用原 id 并保留 `agent_ids` 合并语义，否则新建。
- 切换 = 用另一组参数重写同一批配置文件，**完全复用现有写入链路**（`_write_agent_config` 分派 + `atomic_write` + 备份），不引入新的写入逻辑。
- `POST /api/activate` 的响应必须携带逐 Agent 的**重启指引**（采纳 CC Switch 教训：Agent 不自动重载配置），而不是只返回"已切换"。
- 新增端点一律复用 `server.py` 现有 POST 校验（Origin 白名单 + HttpOnly 会话 Cookie），不另开通道。Key 经请求体传入、只落 `secrets/`，与现有 `/api/install` 的安全姿态一致。

### CLI

新增子命令 `oneagent profile list / add / activate / remove`。`argv[1]` 不是已知子命令时走原有扁平解析器，保持向后兼容。

### Overview 管理化

- Profile 卡片 + 激活标识 + 一键切换。
- Agent 行展示 `version` 对 `lockedVersion` 的漂移，"更新"走现有安装链路。
- 路由调整：已有 profile 时根路径进 `/overview`，没有才进向导。这一条是"长期工具"定位最直接的体现。
- 备份的列表与回滚（`*.backup-<ts>` 已存在但未暴露）作为可选的后续增量（3b），需要新端点，单独评估。

### 明确不做

- **自动改写 shell rc**：`--wire-shell` 仍作为独立显式开关，在第 2 层之后单独评估。
- **Agent 进程自动重载**：不同 Agent 的生效方式不同，不做进程操作，只给重启指引。

## 修订：从全局 profile 改为 per-agent 独立配置

本 ADR 初稿把「per-agent 独立 profile」列为不做，理由是 `~/.oneagent/env` 天然全局共享。该理由已被推翻：共享 env 不是外部约束，而是我们自己写进三个 Agent 配置的同名变量 `ONEAGENT_API_KEY` 造成的人为耦合。五个 Agent 的配置本来就落在各自的文件里，Claude Code 与 Aider 的凭据早已独立，只有 Codex、OpenCode、Kilo CLI 因为共用一个变量名才无法在同一 shell 里指向不同 Provider。

因此改为：每个 Agent 读自己的环境变量（`ONEAGENT_API_KEY_<AGENT>`），凭据落在 `~/.oneagent/agents/<agent-id>.env`。密钥文件确实变多了，但换来的是配置真正解耦，且激活失败的影响范围收窄到单个 Agent，不需要跨文件回滚。

`profiles/` 存储保留，语义从「唯一激活态」改为**可复用模板**：三个 Agent 共用同一 Provider 与 Key 是常见场景，模板避免重复输入。`~/.oneagent/env` 降级为兼容层，只服务旧版本写下的配置。

## Alternatives Considered

### 单个 profile.json 内嵌所有 profile

- 优点：原子替换简单，无需目录同步。
- 缺点：文件随 profile 增长；密钥隔离与文件粒度错位；与 CC Switch 用户的心智模型不一致。
- 结论：拒绝。每 profile 一个文件读写面更小，密钥文件天然分离。

### per-agent 独立配置

- 优点：每个 Agent 可以指向不同 Provider。
- 缺点：共享 env 契约失效，密钥文件数量翻倍，切换语义复杂一档。
- 结论：拒绝；全局 profile 先行，后续有真实需求再扩展。

### 维持现状，继续推荐 CC Switch

- 优点：零开发量。
- 缺点：OneAgent 固化为一次性工具，探测/验证能力与切换能力割裂在两个产品里。
- 结论：拒绝。

## Consequences

- `profile.json` schema 变更，迁移是唯一的数据风险点：备份先行 + 测试固定。
- `status_payload` 增加 `profiles` / `activeProfile` 字段，必须同步 `frontend/src/types/api.ts`（传输契约规则）。
- CC Switch 文档的推荐顺序需要调整：OneAgent 内置切换为主路径，CC Switch 作为可选下游。
- 覆盖率门禁不变：`installer.py` 100% 分支、整体 ≥85%、前端 `src/api`/`src/state` ≥85%；新端点与迁移路径都要配测试。
- 备份回滚 UI、per-agent profile、`--wire-shell` 需要各自的后续评估，其中回滚 UI 需要新端点。
