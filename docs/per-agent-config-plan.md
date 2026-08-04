# Per-Agent 配置计划（已实施）

> 当前实现位于 `internal/config` 和 `internal/app`。本文保留设计结论，旧脚本路径不再适用。

> 补注（2026-08-04）：本文提到的 `cmd/oneagent-release`、`cmd/oneagent-rc`、
> `cmd/oneagent-provider-smoke` 已于 `23805b0` 移除，职责交给
> `.github/workflows/build-artifacts.yml`。相关命令是历史背景，不可执行。

## 适配器

| Agent | 配置适配器 | 凭据交付 |
| --- | --- | --- |
| Codex | TOML provider + 专属 env | `oneagent_env` |
| Claude Code | settings JSON + native env | `native_env` |
| OpenCode/Kilo CLI | OpenAI-compatible JSON + 专属 env | `oneagent_env` |
| Aider | env 脚本 | `config_file` |

适配器由 `agents.lock.json.config_adapter` 选择；新增 Agent 不复制版本、命令或路径常量。配置写入会保留用户未管理字段、创建安全备份并原子替换。

## Claude Code fast model

`small_fast_model` 是可选字段。为空时回退主模型；有值时同时写入 settings 和 native env。前端 Advanced section、Go binding 和 CLI 使用同一字段。

## 验收

```text
go test ./internal/config ./internal/app
bash tests/install_test.sh
go run ./cmd/oneagent-rc adopted
```

配置采用检查使用丢弃端口和假 Key，确保 Agent 读取了配置后才会在网络层失败；它不需要真实 Provider Key。
