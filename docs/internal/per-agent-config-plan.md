# Per-Agent 配置计划（已实施）

> 当前实现位于 `internal/config` 和 `internal/app`。本文保留设计结论，旧脚本路径不再适用。

> 补注（2026-08-04）：本文提到的 `cmd/oneagent-release`、`cmd/oneagent-rc`、
> `cmd/oneagent-provider-smoke` 已于 `23805b0` 移除，职责交给
> `.github/workflows/build-artifacts.yml`。相关命令是历史背景，不可执行。

## 适配器

| Agent | 配置适配器 | 凭据位置 |
| --- | --- | --- |
| Codex | TOML provider | `~/.codex/auth.json` 的 `OPENAI_API_KEY` |
| Claude Code | settings JSON | `settings.json` 的 `env.ANTHROPIC_AUTH_TOKEN` |
| OpenCode / Kilo CLI | OpenAI-compatible JSON | 配置文件里的 `options.apiKey` |
| Aider | env 文件 | `~/.oneagent/aider.env`（Aider 用 `--env-file` 加载） |

凭据位置由适配器代码决定，**不是** manifest 字段。早期设计用过 `credential_delivery`
声明，该字段已随 env 文件方案一起删除，见
[ADR-008](../decisions/ADR-008-credentials-in-agent-config-files.md)。

适配器由 `agents.lock.json.config_adapter` 选择；新增 Agent 不复制版本、命令或路径常量。配置写入会保留用户未管理字段、创建安全备份并原子替换。

## Claude Code fast model

`small_fast_model` 是可选字段。为空时回退主模型（`internal/config/write.go`）；有值时同时写入 settings 和 native env。Go binding 与 CLI 的 `--small-fast-model` 使用同一字段。

当前**桌面界面没有输入它的地方**：`AgentProfilePage` 与 `AgentQuickSwitch` 都传空值，因此 GUI 路径始终走回退。要设置非默认的 fast model 只能用 CLI。

## 验收

```text
go test ./internal/config ./internal/app
bash tests/install_test.sh
go run ./cmd/oneagent-rc adopted
```

配置采用检查使用丢弃端口和假 Key，确保 Agent 读取了配置后才会在网络层失败；它不需要真实 Provider Key。
