# 前端管理控制台改造计划（已实施）

> 状态：已实施（2026-07-31）。React 管理页面通过 Wails bindings 使用 Go profile/config/status 用例。

## 已交付

- 首屏环境总览、Agent 行式管理、Provider 和 Profile 页面。
- 单 Agent 页面（`/agents/:agentId`）由 `AgentProfilePage` 承担：切换 Provider、模型和 Profile。
- Key 不进入 reducer、浏览器存储或 binding 公开摘要；仅在用户主动打开 Provider 编辑/配置表单时通过本机 binding 读入密码字段。
- 生成的 `frontend/bindings` 是后端 DTO 的唯一类型来源。
- Wails server/e2e fake runner 覆盖导航、重试、错误 cause 和临时 HOME；生产构建不使用 server tag。

## 当前验证

```bash
cd frontend
pnpm run test:coverage
pnpm run build
pnpm run test:e2e
cd ..
go test ./internal/app ./internal/profile ./internal/binding
```

新增页面或字段必须先更新 Go DTO 和 binding，再更新 React adapter、state 和测试。旧 HTTP、Cookie/Origin 和解释器契约不再扩展。
