# React 前端实现与发布门禁（已实施）

> 本文原为前端重构计划，现转为当前实现摘要。历史 PyInstaller、解释器测试和旧 HTTP 命令已删除。

> 补注（2026-08-04）：本文提到的 `cmd/bootagent-release`、`cmd/bootagent-rc`、
> `cmd/bootagent-provider-smoke` 已于 `23805b0` 移除，职责交给
> `.github/workflows/build-artifacts.yml`。相关命令是历史背景，不可执行。

## 当前实现

- React 19 + TypeScript + Vite 构建 `frontend/dist`。
- 页面通过 `frontend/src/backend/wails.ts` 调用生成 bindings。
- Vitest 覆盖 backend adapter、state、页面和安全字段。
- Playwright 使用 Wails `server,e2e` build tag；生产桌面构建不使用 server。
- `cmd/bootagent-release` 在打包前检查 source map、远程资源和 secret，并将 Go/Wails/React 版本写入 manifest。

## 本地门禁

```text
cd frontend
pnpm install --frozen-lockfile
pnpm run test:coverage
pnpm run build
pnpm run test:e2e
cd ..
go run ./cmd/bootagent-release build --channel technical-preview-unsigned --skip-frontend
go run ./cmd/bootagent-release check release
```

Wails Alpha 阶段只允许 `technical-preview-unsigned`。真实 Agent/Provider 验收由 `cmd/bootagent-rc`、`cmd/bootagent-provider-smoke` 和对应 cleanroom 负责。
