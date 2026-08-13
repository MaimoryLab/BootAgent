# 读取用户既有 Agent 配置

> 状态：已实施（2026-07-31）。Go `internal/config` 提供只读发现器，Wails status 和 CLI 共用；本文件记录行为而非旧实现路径。

## 行为

每个 auto Agent 的 status 同时提供：

- BootAgent 自己的 binding（Provider、模型、更新时间）。
- `detected`：从 Agent 实际配置文件读出的 base URL、模型、是否由 BootAgent 标记、解析错误（不包含 Key）。

支持 Codex TOML、Claude settings JSON、OpenCode/Kilo JSON、Aider env 脚本。读取只做行解析或 JSON/TOML 解析，绝不执行配置脚本、修正文件或返回凭据。坏文件只标记 `unreadable`，不会让整个 status 请求失败。Aider 无法可靠区分手写和 BootAgent 形态，因此 `managedByBootAgent` 保守为 false。

## 安全约束

- 检测响应不含 API Key、Token 或 Key 是否存在的推断。
- 错误只包含路径和解析诊断，不回显文件内容。
- 覆盖非 BootAgent 配置前保留备份（`internal/securefs`）。确认页说明会创建时间戳备份；针对「这份配置不是 BootAgent 写的」的专门警告目前没有界面入口。
- guide-only Agent 不生成 `detected`。
