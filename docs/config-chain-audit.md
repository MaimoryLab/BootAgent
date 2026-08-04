# 配置链审计（已实施）

> 更新：2026-07-31。历史审计发现已落实到 Go 核心；本文不再引用已删除的旧实现路径。

> 补注（2026-08-04）：本文提到的 `cmd/oneagent-release`、`cmd/oneagent-rc`、
> `cmd/oneagent-provider-smoke` 已于 `23805b0` 移除，职责交给
> `.github/workflows/build-artifacts.yml`。相关命令是历史背景，不可执行。

## 当前链路

```text
agents.lock.json
      |
internal/catalog
      |
internal/app validation
      |
internal/provider protocol/base resolution
      |
internal/config adapter + securefs atomic write
      |
internal/profile binding/secret store
      |
Wails service or cmd/oneagent
```

## 已修复的风险

- Agent 命令、配置路径、平台、package manager 和包名从 catalog manifest 读取；版本默认由包管理器解析。
- 配置适配器只在 Go 中按 adapter 分派，格式差异不伪装成数据配置。
- 每个 Agent 的凭据交付方式由 `credential_delivery` 和 `env_vars` 声明。
- Claude Code 的 native env 与配置文件同步写入，避免出现配置显示完成但运行时未登录。
- Codex、Claude Code、OpenCode、Kilo CLI、Aider 按各自协议探测。
- 备份、临时文件、权限和原子替换由 `securefs` 统一处理。
- RC 的 `adopted` 检查将配置指向丢弃端口，区分网络失败和认证失败。

## 审计门禁

```text
go test ./internal/config ./internal/install ./internal/app
go run ./cmd/oneagent-rc adopted
go run ./cmd/oneagent-release check release
```

新增自动配置 Agent 时只需更新 `agents.lock.json`、增加对应 Go adapter 和测试，并更新前端生成 binding 需要的公共类型。
