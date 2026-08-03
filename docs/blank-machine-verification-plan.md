# 空白机器可用性验证计划

> 状态：已实施（2026-07-31）。实现入口已从历史脚本切换为 Go CLI、Go RC 命令和 shell cleanroom。

## 验证层级

| 层级 | 入口 | 证明内容 |
| --- | --- | --- |
| RC 安装 | `go run ./cmd/oneagent-rc verify-agents` | 多 Agent 隔离安装、版本和可执行文件 |
| 配置采用 | `go run ./cmd/oneagent-rc adopted` | Codex/Claude Code 是否真正读取丢弃端点配置 |
| Provider | `go run ./cmd/oneagent-provider-smoke` | models、Chat、Responses、Anthropic Messages |

普通 CI 不访问 registry 或真实 Provider；Release Candidate workflow 在受保护环境中执行真实网络检查。Aider 需要其上游声明的 Python 3.12，但不纳入普通 Go/Wails cleanroom。

## 关键断言

- npm prefix 必须排在 PATH 首位，不能由开发机全局 Agent 假通过。
- 默认安装后每个 Agent 都必须报告有效版本；显式指定版本时必须与请求一致。
- 配置文件和 secret 文件使用预期的 `0700/0600` 权限。
- 真实用户 HOME 在测试前后快照必须完全一致。
- API Key 不出现在 profile、CLI JSON、日志或测试附件。
- 配置指向 `127.0.0.1:9` 时，采用配置的 Agent 应报连接失败而不是登录/认证错误。
- release check 必须拒绝 source map、远程资源、secret、Agent 二进制和旧 runtime。

## 运行

```bash
go build -o bin/oneagent ./cmd/oneagent
go run ./cmd/oneagent-rc verify-agents
go run ./cmd/oneagent-rc adopted
```

真实安装会访问 npm registry，应在隔离网络和可审计的 runner 中运行。
