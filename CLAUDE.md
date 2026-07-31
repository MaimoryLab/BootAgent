# OneAgent 开发约定

当前分支是 Go/Wails 收尾线，版本为 `0.3.0-dev`。桌面入口是 `cmd/oneagent-desktop`，headless CLI 是 `cmd/oneagent`；两者共享 `internal/` 用例。React 只通过生成的 Wails bindings 调用后端。

## 目录

- `internal/app`：Status、Provider、Agent、Profile 用例和写入协调锁。
- `internal/catalog`：嵌入的 `agents.lock.json` 与 Provider 目录。
- `internal/config`：TOML/JSON/JSONC 适配器、配置发现和 golden fixtures。
- `internal/install`：锁定包安装、registry/integrity 校验和 Aider 外部前置条件。
- `internal/profile`、`internal/securefs`：profile、secret、备份、权限和原子写。
- `cmd/oneagent`
- `cmd/oneagent-release`：原生 Wails/Go/React 发布包、notice、manifest 和 SHA-256。
- `cmd/oneagent-rc`、`cmd/oneagent-provider-smoke`：发行候选的真实 Agent/Provider 检查。
- `frontend/bindings`：Wails 生成物，禁止手工编辑。
- `site`：独立 Astro 公开站。

## 本地命令

```bash
go test ./...
go test -race ./...
go vet ./...
go build -o bin/oneagent ./cmd/oneagent

cd frontend
npm ci
npm run test
npm run build
npm run test:e2e
```

构建和检查发行包：

```bash
go run ./cmd/oneagent-release build --channel technical-preview-unsigned --source
go run ./cmd/oneagent-release check release
```

真实 RC 只在受控环境运行：

```bash
go run ./cmd/oneagent-rc verify-agents
go run ./cmd/oneagent-rc adopted
go run ./cmd/oneagent-provider-smoke --provider all --timeout 30s
```

普通测试、Wails 构建、站点构建和发布工具不需要 Python。Aider 是外部上游例外：选择安装 Aider 时必须有本机 Python 3.12；OneAgent 不下载它，也不把它放进发行包。

## 代码边界

- `agents.lock.json` 是 Agent 元数据唯一真源。新增自动配置 Agent 时先补 lock，再添加对应 config adapter 和 Go 测试。
- 子进程必须使用 argv 数组和受控环境，设置超时并保留可诊断但已脱敏的输出。
- 写入顺序必须是私有目录、备份、同目录临时文件、收紧权限、原子替换；密钥备份无法收紧时删除并失败。
- 不把 API Key 写入 profile、binding、日志、URL、React state、浏览器存储或测试附件。
- Provider 按 Agent 协议探测；`/v1/models` 不能替代 Responses、Anthropic Messages 或 Chat Completions 检查。
- Wails 生产构建不能使用 `server` tag；浏览器 E2E 才能使用 server/e2e fake runner。
- Linux 发行构建固定使用 `gtk3` tag，Wails Alpha 阶段只允许 `technical-preview-unsigned`。

## 文档维护

README、workflow、Taskfile、Dockerfile 和 AI Agent Kit 里的命令必须对应当前仓库文件。历史 ADR 可以保留背景，但必须明确标记为 Superseded，不能作为操作指南。
