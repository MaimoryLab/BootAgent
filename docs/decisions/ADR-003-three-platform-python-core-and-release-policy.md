# ADR-003：三平台运行时与版本锁定发行策略（已废弃）

> 状态：**Superseded**（2026-07-31）。当前实现和发行规则由 [ADR-007](ADR-007-wails-v3-go-migration.md)、[ADR-005](ADR-005-channel-neutral-distribution-and-compliance.md) 和 `cmd/oneagent-release` 定义。本文件只保留历史背景，不是安装或发布操作指南。

## 历史背景

早期 OneAgent 使用跨平台脚本和 Python 标准库实现 Agent catalog、配置适配、安装编排和本地 HTTP GUI。该方案曾强调三平台路径、权限、锁定版本、npm/uv allowlist、完整错误码和 cleanroom 证据。

这些产品约束仍然有效，但实现已经迁移为：

- Go catalog、provider、install、config、profile、securefs 和 process 包。
- `cmd/oneagent` 纯 Go CLI 与 `cmd/oneagent-desktop` Wails 应用。
- React 通过生成的 Wails bindings 调用 Go service。
- `cmd/oneagent-release` 生成原生 Wails/Go 包、manifest、SHA-256 和第三方 notices。
- `cmd/oneagent-rc` 与 `cmd/oneagent-provider-smoke` 承担发行候选检查。

## 仍保留的产品约束

- Agent 包不进入 OneAgent 发行包；安装只能来自 lock 声明的官方源或用户明确选择的 HTTPS 镜像。
- 子进程使用参数数组、受控环境和超时；禁止 shell 拼接和未审查的下载管道。
- API Key 不进入 profile、日志、URL、命令行、React 状态或发行附件。
- 配置写入必须备份、原子替换并收紧 Unix mode/Windows ACL。
- Codex、Claude Code 和 OpenAI-compatible Agent 按实际协议分别探测。
- Wails Alpha 阶段只发布 `technical-preview-unsigned`；Stable 需要单独的签名、公证和原生验证证据。
- Aider 是可选外部上游例外：用户选择安装时需要已有 Python 3.12，OneAgent 不捆绑或下载该运行时。

## 迁移记录

Python 实现、Python 测试、PyInstaller/wheel/setuptools 配置和相关工作流已删除。新的验收清单见 [Wails v3 迁移收尾计划](../wails-v3-migration-plan.md)。
