# 内部实现记录

这个目录是**维护者视角的历史记录**，不是使用说明。里面的文件回答「当时为什么这样做、
做完之后验证了什么」，而不是「现在该怎么用」。

面向使用者的文档在上一层：[产品边界基线](../product-boundary-baseline.md)、
[分发与合规政策](../distribution-compliance-policy.md)、
[AI Agent Kit](../ai-agent-kit/README.md)；架构决策在
[decisions/](../decisions/)。

## 为什么分开放

这些文件大多写于某次改造完成时，标题里带「计划」二字是遗留——它们已经是既成事实的记录。
混在 `docs/` 根下的问题是：读者分不清哪份是「已经这样了」、哪份是「打算这样做」，于是
照着一份过期文档执行命令。分到这里之后，`docs/` 根下只剩当前有效的规范和用户文档。

## 读这里的文件时注意

**命令可能已经不能执行。** `cmd/oneagent-release`、`cmd/oneagent-rc`、
`cmd/oneagent-provider-smoke` 已于 `23805b0` 移除，职责交给
`.github/workflows/build-artifacts.yml`；`tests/` 目录也已不存在。引用它们的地方用
` ```text ` 而不是 ` ```bash ` 标注，表示那是历史命令而非可运行指令。

独立的 `cmd/oneagent` CLI 及其测试已于 2026-08-06 移除，桌面应用是当前唯一产品入口；
本目录里保留的 CLI 路径和命令同样只表示历史状态。

**当前可运行的验证入口只有两处**：`.github/workflows/ci.yml`（每个 PR 自动跑
`go vet`、`go test -race`、前端 test 和 build），以及
`.github/workflows/build-artifacts.yml`（手动触发的发行构建）。本地命令见
[CLAUDE.md](../../CLAUDE.md)。

## 目录

| 文件 | 记录了什么 |
| --- | --- |
| [wails-v3-migration-plan.md](wails-v3-migration-plan.md) | Go/Wails 迁移的收尾验收，本目录最完整的一份架构说明 |
| [config-chain-audit.md](config-chain-audit.md) | 从 `agents.lock.json` 到磁盘写入的完整配置链 |
| [config-discovery-plan.md](config-discovery-plan.md) | 只读配置发现器的行为契约 |
| [per-agent-config-plan.md](per-agent-config-plan.md) | 各 Agent 的适配器与凭据位置 |
| [frontend-management-console-plan.md](frontend-management-console-plan.md) | React 管理页面与 Key 处理边界 |
| [frontend-component-redesign-plan.md](frontend-component-redesign-plan.md) | 前端构建链与发布门禁 |
| [provider-rc-testing.md](provider-rc-testing.md) | Provider 三协议验证要求（执行入口已移除） |
| [blank-machine-verification-plan.md](blank-machine-verification-plan.md) | 空白机器验证的断言清单（执行入口已移除） |
| [recent-work-summary.md](recent-work-summary.md) | 一次阶段性完工清单，已被上面几份取代 |
| [mvp-agent-installer-plan.md](mvp-agent-installer-plan.md) | 最初的 HTTP/Python 原型，仅存背景 |
| [release-evidence/](release-evidence/) | 历史发行包的 SHA-256 与 cleanroom 记录 |
