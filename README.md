# OneAgent

OneAgent 是一个本地 AI 开发环境激活器。React 向导和纯 Go CLI 共用同一套 Go 用例，负责检测 Agent、安装锁定版本、探测 Provider、合并配置、创建备份并收紧权限。桌面应用使用 Wails v3 binding；生产进程不监听业务 TCP 端口。

OneAgent 不重新分发 Agent 包，也不捆绑 Node.js、系统 WebView、Git 或 API Key。缺少运行前置条件时返回明确错误和官方安装指引。

## 当前状态

当前版本为 `0.3.0-dev`，Wails 仍处于 Alpha，因此发布渠道只能是 `technical-preview-unsigned`。Python 迁移已经完成：受版本控制的旧实现、测试、PyInstaller/wheel 打包链路均已删除；普通构建、测试、运行和发布只需要 Go、Node（构建前端）及目标平台 WebView。Aider 是唯一例外：只有用户选择安装 Aider 时，才要求本机已有 Python 3.12，OneAgent 不会下载或管理它。

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

Pure Go CLI --------------------^      Astro site ---- release metadata
```

- `cmd/oneagent-desktop`：Wails 桌面入口。
- `cmd/oneagent`：纯 Go headless CLI。
- `cmd/oneagent-release`：构建、notice、manifest、SHA-256 和发行包检查。
- `cmd/oneagent-rc`：真实锁定 Agent 安装和无密钥配置采用检查。
- `cmd/oneagent-provider-smoke`：PPIO/Novita 三协议 RC smoke。
- `internal/`：桌面、CLI 和 RC 工具共用的 Go 核心。
- `frontend/`：React 应用；发行包只携带构建后的静态资源。
- `site/`：独立 Astro 公开站，不进入桌面包体。
- `agents.lock.json`：Agent 版本、来源、配置适配器和许可证的唯一清单。
- `providers.lock.json`：Provider 端点、fallback probe model 和公开站披露字段的唯一清单。

## 快速启动

### 桌面应用

```bash
cd frontend
npm ci
npm run build
cd ..
go run -tags wails ./cmd/oneagent-desktop
```

生产构建需要目标平台的 Wails/WebView 依赖。Linux 当前使用 `gtk3` tag（Ubuntu 22.04 cleanroom）；macOS 使用系统 WKWebView；Windows 使用 WebView2 Runtime。

### CLI

```bash
go build -o bin/oneagent ./cmd/oneagent
```

Windows PowerShell：

```powershell
go build -o bin\\oneagent.exe .\\cmd\\oneagent
$env:ONEAGENT_API_KEY = $MyApiKey
.\\scripts\\install.ps1 --agent codex --provider ppio --model your-model-id
```

日常使用优先通过 `ONEAGENT_API_KEY`、桌面粘贴或已保存 profile 传递凭据；`--api-key` 仅保留给受控脚本。 `--registry` 默认是官方 npm registry，镜像必须显式选择并使用 HTTPS。

### 公开站

```bash
cd site
npm ci
npm test
npm run build
npx playwright install chromium
npm run test:e2e
```

站点只读取 GitHub Release、`agents.lock.json` 和 `providers.lock.json`，不读取本地 `release/`，也不依赖桌面构建环境。

## Agent 与 Provider

自动配置 Agent：

| Agent | 锁定版本 | 安装器 | 协议 |
| --- | --- | --- | --- |
| Codex | `0.145.0` | npm | Responses |
| Claude Code | `2.1.217` | npm | Anthropic Messages |
| OpenCode | `1.18.4` | npm | OpenAI-compatible |
| Kilo CLI | `7.4.11` | npm | OpenAI-compatible |
| Aider | `0.86.2` | uv tool | OpenAI-compatible |

OpenClaw、Hermes、Cursor、Kiro、Gemini CLI、Cline、Continue、Qwen Code 和 Kilo VS Code 仅提供官方安装引导，不安装包、不写私有配置、不启动后台服务。

支持 PPIO、Novita 和 Custom。配置后按 Agent 实际协议探测：Codex 使用 `/v1/responses`，Claude Code 使用 `/v1/messages`，其余自动配置 Agent 使用 `/v1/chat/completions`。协议不兼容时返回 `PROTOCOL_UNSUPPORTED`，不会先写入不可用配置。

Aider 的安装命令由 Go 后端固定为 `uv tool install --force --python python3.12 --no-python-downloads ...`。这条路径只在选择 Aider 时执行；缺少 `uv` 或 Python 3.12 会返回 `PREREQUISITE_MISSING`。

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
npm ci
npm run test:coverage
npm run build
npx playwright install chromium
npm run test:e2e
cd ..
task test:native
```


## Release Candidate

真实锁定 Agent 安装（默认四个 npm Agent，不包含可选 Aider）：

```bash
go build -o bin/oneagent ./cmd/oneagent
go run ./cmd/oneagent-rc verify-agents
go run ./cmd/oneagent-rc adopted
```

Provider 三协议 smoke 从受保护环境变量读取凭据和模型，不接受命令行 Key：

```bash
ONEAGENT_PPIO_API_KEY=... \
ONEAGENT_PPIO_OPENAI_MODEL=... \
ONEAGENT_PPIO_ANTHROPIC_MODEL=... \
ONEAGENT_PPIO_RESPONSES_MODEL=... \
ONEAGENT_NOVITA_API_KEY=... \
ONEAGENT_NOVITA_OPENAI_MODEL=... \
ONEAGENT_NOVITA_ANTHROPIC_MODEL=... \
ONEAGENT_NOVITA_RESPONSES_MODEL=... \
go run ./cmd/oneagent-provider-smoke --provider all --timeout 30s
```

## 发行

在本机生成当前平台的未签名技术预览包：

```bash
go run ./cmd/oneagent-release build \
  --channel technical-preview-unsigned \
  --source
go run ./cmd/oneagent-release check release
```

命令会构建 React、Wails 桌面二进制和纯 Go CLI，生成 macOS `.app` 或 Windows/Linux ZIP、可选源码 ZIP、第三方 notices、release manifest 和 SHA-256 清单。检查会拒绝 source map、远程资源、secret、Agent 二进制、语言运行时和不完整的锁定版本信息。

Wails 仍处于 Alpha，当前不发布 Stable，不做平台签名、公证或商店分发。Stable 的签名门禁保留在后续发行阶段。

## 文档

- [Wails v3 迁移收尾计划](docs/wails-v3-migration-plan.md)
- [发行与合规政策](docs/distribution-compliance-policy.md)
- [公开站运营手册](docs/public-site-operations.md)
- [Provider RC 测试说明](docs/provider-rc-testing.md)
- [AI Agent Kit](docs/ai-agent-kit/00-start-here.md)
- [Wails 架构 ADR](docs/decisions/ADR-007-wails-v3-go-migration.md)
- [按 Agent 协议验证 ADR](docs/decisions/ADR-004-per-agent-protocol-verification.md)
- [历史 Python 发行 ADR（已废弃）](docs/decisions/ADR-003-three-platform-python-core-and-release-policy.md)
