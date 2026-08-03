# Wails v3 / Go 迁移收尾计划

> 状态：**已完成**（2026-07-31）
>
> 本文是收尾验收记录。当前生产实现是 Go + Wails + React；旧脚本和旧测试已删除。Wails 仍为 Alpha，所以发行渠道保持 `technical-preview-unsigned`。

## 1. 目标与边界

- 桌面应用使用 Wails v3，React 只调用生成的 TypeScript bindings。
- headless CLI 和桌面 service 共用 `internal/` Go 用例。
- Agent catalog、Provider、安装、配置发现、配置写入、profile、备份、权限和错误契约全部由 Go 实现。
- 发行工具由 `cmd/oneagent-release` 统一负责；官网只读取 Release 与仓库 JSON。
- Aider 的上游安装流程可以要求本机 Python 3.12，但这是用户选择 Aider 时的外部前置条件，不是 OneAgent 的构建、测试、运行或发行依赖。

## 2. 最终架构

```text
React + Vite
    |
generated Wails bindings
    |
Status / Provider / Agent / Profile services
    |
Go application use cases
    |
catalog / provider / install / config / profile / securefs / process

cmd/oneagent                 headless CLI
cmd/oneagent-release         native package + manifest/checks
cmd/oneagent-rc              latest Agent RC checks
cmd/oneagent-provider-smoke  Provider protocol RC checks
site/                        independent Astro release site
```

生产桌面构建不使用 `server` tag，不监听业务端口。浏览器 E2E 才使用 Wails server/e2e runner。

## 3. 已完成的交付

### 核心迁移

- Go catalog 嵌入并校验 `agents.lock.json`。
- Go Provider URL、模型发现和三协议 probe。
- Go 安装编排、npm integrity 校验、registry 白名单和超时取消。
- Go 配置适配器覆盖 Codex、Claude Code、OpenCode、Kilo CLI、Aider。
- Go 配置发现、profile/secret 存储、原子写、备份和 Unix/Windows 权限。
- Wails `StatusService`、`ProviderService`、`AgentService`、`ProfileService`。
- React 页面和状态层切换到生成 binding；生产代码没有业务 `fetch` 或本地 HTTP API。

### 发行与 RC

- `cmd/oneagent-release` 构建 React、Wails desktop 和纯 Go CLI。
- 发行包生成 macOS `.app`、Windows/Linux ZIP、源码 ZIP、manifest、SHA-256 和第三方 notices。
- release check 拒绝 source map、远程资源、secret、Agent 二进制、旧语言 runtime、wheel 和 PyInstaller 文件。
- `cmd/oneagent-rc verify-agents` 在隔离 HOME/npm prefix 中真实安装锁定 npm Agent，并验证 PATH 和版本。
- `cmd/oneagent-rc adopted` 将 Codex/Claude Code 指向丢弃端口，区分“配置已采用”和“认证未采用”。
- `cmd/oneagent-provider-smoke` 复用 Go Provider client 验证 models、Chat、Responses、Anthropic Messages。
- Docker 和 macOS cleanroom 只运行 Go/Node/shell 验收。

### 清理

已删除：

- 旧 `oneagent/` 实现目录。
- 旧 Python 测试、RC 脚本、GUI 和发布脚本。
- `setup.py`、`pyproject.toml`、PyInstaller spec、wheel/resource staging。
- CI/Docker 中的 setup-python、pip、coverage、PyInstaller 和 wheel 步骤。

## 4. 文件映射

| 旧职责 | 当前实现 |
| --- | --- |
| catalog、provider、installer、server | `internal/catalog`、`internal/provider`、`internal/app`、`internal/config` |
| 本地 GUI | `cmd/oneagent-desktop` + Wails |
| headless CLI | `cmd/oneagent` |
| release/build/check/notices | `cmd/oneagent-release` |
| latest Agent RC | `cmd/oneagent-rc verify-agents` |
| Provider RC | `cmd/oneagent-provider-smoke` |
| config adoption RC | `cmd/oneagent-rc adopted` |
| resource staging | `go:embed` / Wails assets |

## 5. 验收命令

```bash
go vet ./...
go test ./...
go test -race ./...

cd frontend
npm ci
npm run test:coverage
npm run build
npm run test:e2e
cd ..

go build -o bin/oneagent ./cmd/oneagent
bash tests/install_test.sh
go run ./cmd/oneagent-release build --channel technical-preview-unsigned --source
go run ./cmd/oneagent-release check release
```

发行候选在受保护环境执行：

```bash
go run ./cmd/oneagent-rc verify-agents
go run ./cmd/oneagent-rc adopted
go run ./cmd/oneagent-provider-smoke --provider all --timeout 30s
bash tests/real_install_test.sh
```

## 6. 最终门禁

- [x] 工作树中不再有受版本控制的旧实现文件。
- [x] active workflow、Taskfile、Dockerfile 和 README 不调用旧脚本或旧打包工具。
- [x] Go、React、Astro、Wails binding 和 native smoke 有独立验证入口。
- [x] 无可选 runtime 的 PATH 下 Go CLI、Go tests、React build 和 release check 可运行。
- [x] 发行包不含 `.py`、`.pyc`、wheel、PyInstaller 或 Agent 二进制。
- [x] API Key 不进入 profile、日志、binding、URL 或测试附件。
- [x] Wails Alpha 阶段只允许 `technical-preview-unsigned`。
- [x] Aider 的 Python 3.12 只在选择 Aider 安装时作为外部 prerequisite 出现。

## 7. 兼容与回滚

用户的 `~/.oneagent`、Agent 配置路径、profile schema、secret 文件和 backup 命名保持兼容。回滚通过发布上一版已验收的原生包完成，不在新包中恢复已删除的旧 runtime。
