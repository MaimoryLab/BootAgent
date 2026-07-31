# 读取用户既有 Agent 配置

> 状态：已实施（2026-07-31）。Go `internal/config` 提供只读发现器，Wails status 和 CLI 共用；本文件记录行为而非旧实现路径。

## 行为

每个 auto Agent 的 status 同时提供：

- OneAgent 自己的 binding（Provider、模型、更新时间）。
- `detected`：从 Agent 实际配置文件读出的 base URL、模型、是否由 OneAgent 标记、解析错误（不包含 Key）。

支持 Codex TOML、Claude settings JSON、OpenCode/Kilo JSON、Aider env 脚本。读取只做行解析或 JSON/TOML 解析，绝不执行配置脚本、修正文件或返回凭据。坏文件只标记 `unreadable`，不会让整个 status 请求失败。Aider 无法可靠区分手写和 OneAgent 形态，因此 `managedByOneAgent` 保守为 false。

## 安全约束

- 检测响应不含 API Key、Token 或 Key 是否存在的推断。
- 错误只包含路径和解析诊断，不回显文件内容。
- 覆盖非 OneAgent 配置前，前端显示警告并保留备份。
- guide-only Agent 不生成 `detected`。

## 验证

```bash
go test ./internal/config ./internal/app
go build -o bin/oneagent ./cmd/oneagent
bash tests/install_test.sh
bash tests/macos_cleanroom_test.sh
```

真实用户 HOME 在 macOS cleanroom 前后快照比对，发现污染即失败。容器化 macOS 不属于项目设施；需要人工 Darwin 验收时使用原生 runner 或受控虚拟机。
