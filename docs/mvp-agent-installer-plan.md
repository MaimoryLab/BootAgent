# MVP Agent Installer 计划（已废弃）

> 状态：**Superseded**（2026-07-31）。本文件记录最初的本地 HTTP/Python 原型，不能作为当前操作指南。当前入口请看 [README](../README.md) 和 [Wails 迁移收尾计划](wails-v3-migration-plan.md)。

早期原型使用标准库 HTTP GUI、单 Agent CLI 和脚本式配置写入。该原型已由以下实现替代：

- Wails desktop：`cmd/oneagent-desktop`
- headless CLI：`cmd/oneagent`
- Go services/use cases：`internal/app`
- React bindings：`frontend/bindings`
- 配置和安装契约：Go tests、golden fixtures、`tests/install_test.sh`

历史方案中的 localhost HTTP、Cookie/Origin、解释器启动器和旧 GUI 文件均已删除。保留本文件只是为了说明早期产品边界；新增功能不得依赖其中的路径、端口或命令。
