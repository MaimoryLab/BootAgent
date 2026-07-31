# React 前端实现与发布门禁（已实施）

> 本文原为前端重构计划，现转为当前实现摘要。历史 PyInstaller、解释器测试和旧 HTTP 命令已删除。

## 当前实现

- React 19 + TypeScript + Vite 构建 `frontend/dist`。
- 页面通过 `frontend/src/backend/wails.ts` 调用生成 bindings。
- Vitest 覆盖 backend adapter、state、页面和安全字段。
- Playwright 使用 Wails `server,e2e` build tag；生产桌面构建不使用 server。
- `cmd/oneagent-release` 在打包前检查 source map、远程资源和 secret，并将 Go/Wails/React 版本写入 manifest。

## 本地门禁

```bash
cd frontend
npm ci
npm run test:coverage
npm run build
npm run test:e2e
cd ..
go run ./cmd/oneagent-release build --channel technical-preview-unsigned --skip-frontend
go run ./cmd/oneagent-release check release
```

Wails Alpha 阶段只允许 `technical-preview-unsigned`。真实 Agent/Provider 验收由 `cmd/oneagent-rc`、`cmd/oneagent-provider-smoke` 和对应 cleanroom 负责。
