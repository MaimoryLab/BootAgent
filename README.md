# OneAgent

OneAgent 是一个本地 AI 开发环境激活器。React 向导和纯 Go CLI 共用同一套 Go 用例，负责检测 Agent、安装最新版本、探测 Provider、合并配置、创建备份并收紧权限。桌面应用使用 Wails v3 binding；生产进程不监听业务 TCP 端口。

OneAgent 不重新分发 Agent 包，也不捆绑 Node.js、系统 WebView、Git 或 API Key。缺少运行前置条件时返回明确错误和官方安装指引。

## 当前状态

当前版本为 `0.3.0-dev`，Wails 仍处于 Alpha，因此发布渠道只能是 `technical-preview-unsigned`。Python 迁移已经完成：受版本控制的旧实现、测试、PyInstaller/wheel 打包链路均已删除；普通构建、测试、运行和发布只需要 Go、Node、pnpm 11.17.0（构建前端）及目标平台 WebView。Aider 是唯一例外：只有用户选择安装 Aider 时，才要求本机已有 Python 3.12，OneAgent 不会下载或管理它。

## 架构

```text
React + TypeScript + Vite
          |
          | generated Wails bindings
          v
Status / Provider / Agent / Profile services
          |
          v
      Go application use cases
          |
  catalog / provider / install / config / profile / securefs

Pure Go CLI --------------------^
```

公开站不在这张图里，也不在这个仓库里：它已迁出到
[MaimoryLab/OneAgent-site](https://github.com/MaimoryLab/OneAgent-site)。它从
GitHub Releases API 读取下载信息，并把 `agents.lock.json`、`providers.lock.json`
vendor 到自己仓库，从发行 tag 刷新——所以改本仓库这两个文件不会自动改变站上内容。

- `cmd/oneagent-desktop`：Wails 桌面入口。
- `cmd/oneagent`：纯 Go headless CLI。
- `internal/`：桌面和 CLI 共用的 Go 核心。
- `frontend/`：React 应用；发行包只携带构建后的静态资源。
- `agents.lock.json`：Agent 包名、来源、配置适配器和许可证的唯一清单；不固定 Agent 版本或包哈希。
- `providers.lock.json`：内置 Provider 端点、fallback probe model 和公开站披露字段清单；桌面端用户 Provider 保存在本机 `~/.oneagent/providers.json`。

## 快速启动

### 桌面应用

```bash
cd frontend
pnpm install --frozen-lockfile
pnpm run build
cd ..
go run -tags wails ./cmd/oneagent-desktop
```

生产构建需要目标平台的 Wails/WebView 依赖。Linux 当前使用 `gtk3` tag（Ubuntu 22.04 cleanroom）；macOS 使用系统 WKWebView；Windows 使用 WebView2 Runtime。

### CLI

```bash
go build -o bin/oneagent ./cmd/oneagent
```

Windows CMD：

```cmd
go build -o bin\oneagent.exe .\cmd\oneagent
bin\oneagent.exe agent set codex --provider ppio --model your-model-id --api-key your-api-key
```

日常使用优先通过桌面粘贴或已保存 profile 传递凭据；`ONEAGENT_API_KEY` 和 `--api-key` 仅保留给受控脚本。 `--registry` 默认是官方 npm registry，镜像必须显式选择并使用 HTTPS。

### 公开站

```bash
cd site
pnpm install --frozen-lockfile
pnpm test
pnpm run build
pnpm exec playwright install chromium
pnpm run test:e2e
```

站点只读取 GitHub Release、`agents.lock.json` 和 `providers.lock.json`，不读取本地 `release/`，也不依赖桌面构建环境。

## Agent 与 Provider

自动配置 Agent：

| Agent | 包 | 安装器 | 协议 |
| --- | --- | --- | --- |
| Codex | `@openai/codex` | npm | Responses |
| Claude Code | `@anthropic-ai/claude-code` | npm | Anthropic Messages |
| OpenCode | `opencode-ai` | npm | OpenAI-compatible |
| Kilo CLI | `@kilocode/cli` | npm | OpenAI-compatible |
| Aider | `aider-chat` | uv tool | OpenAI-compatible |

安装器默认让 npm 或 uv 解析最新版本。需要复现特定版本时可传 `--agent-version VERSION`，例如 `oneagent --agent codex --install-agent --check-agent-only --agent-version 0.145.0`。

OpenClaw、Hermes、Cursor、Kiro、Gemini CLI、Cline、Continue、Qwen Code 和 Kilo VS Code 仅提供官方安装引导，不安装包、不写私有配置、不启动后台服务。

内置 PPIO、Novita，并支持在 Provider 页面增删改用户 Provider。配置后按 Agent 实际协议探测：Codex 使用 `/v1/responses`，Claude Code 使用 `/v1/messages`，其余自动配置 Agent 使用 `/v1/chat/completions`。协议不兼容时返回 `PROTOCOL_UNSUPPORTED`，不会先写入不可用配置。

Aider 的安装命令由 Go 后端固定为 `uv tool install --force --python 3.12 ...`，由 uv 复用或提供匹配的 Python。这条路径只在选择 Aider 时执行；缺少 `uv` 会返回 `PREREQUISITE_MISSING`。

## 开发与测试

Go 核心、发行工具和 RC 工具：

```bash
go vet ./...
go test ./...
go test -race ./...
go run honnef.co/go/tools/cmd/staticcheck@2025.1.1 ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...
```
React/Wails 测试：

```bash
cd frontend
pnpm install --frozen-lockfile
pnpm run test:coverage
pnpm run build
pnpm exec playwright install chromium
pnpm run test:e2e
```

每个 pull request 都会运行 `.github/workflows/ci.yml`：Go 侧是 `go vet` 加
`go test -race`，前端侧是 `pnpm run test` 加 `pnpm run build`。

## 发行

发行包由 `.github/workflows/build-artifacts.yml` 构建，手动触发（`workflow_dispatch`）。
它为 macOS 与 Windows 各构建 x64/arm64 的 Wails 桌面二进制和纯 Go CLI，macOS 产物打成
`.app`。

Wails 仍处于 Alpha，当前不发布 Stable，不做平台签名、公证或商店分发。Stable 的签名门禁
保留在后续发行阶段。

本机要复现一份等价产物，见「快速启动」里的桌面构建步骤；发行渠道标签、SHA-256 清单和
第三方 notices 曾由 `cmd/oneagent-release` 生成，该工具已于 `23805b0` 随构建流程迁移到
GitHub Actions 而移除。第三方归属现在维护在仓库根的 [NOTICE](NOTICE)。

## 文档

`docs/` 按受众分三层：根下是当前有效的规范，`decisions/` 是架构决策，
`internal/` 是维护者视角的历史记录。

**使用与规范**

- [AI Agent Kit](docs/ai-agent-kit/00-start-here.md)：从零配置一个 Agent 环境
- [产品边界基线](docs/product-boundary-baseline.md)：做什么、不做什么，以及为什么
- [分发与合规政策](docs/distribution-compliance-policy.md)：发行前的权利、安全与渠道要求
- [公开站运营手册](docs/public-site-operations.md)

**架构决策**

- [decisions/](docs/decisions/)：ADR-001 至 ADR-009，含已被取代的决策及其去向
- [Wails v3 / Go 迁移](docs/decisions/ADR-007-wails-v3-go-migration.md)
- [按 Agent 协议验证](docs/decisions/ADR-004-per-agent-protocol-verification.md)
- [凭据写入 Agent 配置文件](docs/decisions/ADR-008-credentials-in-agent-config-files.md)

**内部实现记录**

- [internal/](docs/internal/README.md)：各次改造的完工记录与验证清单。里面的命令可能已随
  工具移除而失效，该目录的 README 说明了当前的替代入口。

## 许可证

Apache License 2.0，见 [LICENSE](LICENSE)。

[NOTICE](NOTICE) 列出随二进制分发的第三方组件及其许可证，以及运行时下载但不再分发的
Node.js、uv 和 Agent 包。界面中显示的各 Agent 官方标识属于 nominative use，用于指明某一
行对应哪个工具，不表示背书或关联，商标归各自所有者。
