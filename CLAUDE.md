# OneAgent 开发约定

当前分支是 Go/Wails 收尾线，版本为 `0.3.0-dev`。桌面入口是 `cmd/oneagent-desktop`，headless CLI 是 `cmd/oneagent`；两者共享 `internal/` 用例。React 只通过生成的 Wails bindings 调用后端。

## 目录

- `internal/app`：Status、Provider、Agent、Profile 用例和写入协调锁。
- `internal/catalog`：嵌入的 `agents.lock.json`、`providers.lock.json`、`runtimes.lock.json` 与内置 Provider 目录。
- `internal/config`：TOML/JSON/JSONC 适配器、配置发现和 golden fixtures。
- `internal/install`：默认最新、可选精确版本的 Agent 包安装，registry 选择、Node.js/uv 运行时引导（下载、校验、解压、写入 PATH）和 Aider Python 管理边界。
- `internal/profile`、`internal/securefs`：profile、secret、备份、权限和原子写。
- `cmd/oneagent`
- `cmd/oneagent-release`：原生 Wails/Go/React 发布包、notice、manifest 和 SHA-256。
- `cmd/oneagent-rc`、`cmd/oneagent-provider-smoke`：发行候选的真实 Agent/Provider 检查。
- `frontend/bindings`：Wails 生成物，禁止手工编辑。
- `site`：独立 Astro 公开站。

`providers.lock.json` 是内置 Provider 端点、fallback model 和公开站商业披露字段的真源；用户 Provider 与内置覆盖保存在 `~/.oneagent/providers.json`。

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

普通测试、Wails 构建、站点构建和发布工具不需要 Python。安装 Aider 需要 Python 3.12，但不再要求本机预装：uv 自己解析解释器，本机有匹配版本就复用，否则下载一份托管 CPython 到 `~/.oneagent/runtimes/python`。Python 仍然不进发行包。

## 代码边界

- `agents.lock.json` 是 Agent 元数据唯一真源，但不保存 Agent 版本或包哈希。新增自动配置 Agent 时先补包名和元数据，再添加对应 config adapter 和 Go 测试。
- 子进程必须使用 argv 数组和受控环境，设置超时并保留可诊断但已脱敏的输出。
- 写入顺序必须是私有目录、备份、同目录临时文件、收紧权限、原子替换；密钥备份无法收紧时删除并失败。
- 不把 API Key 写入普通 profile、状态摘要、日志、URL、全局 React state、浏览器存储或测试附件；仅 Provider 编辑/配置表单可通过本机 binding 按需读取私有存储中的 Key。
- Provider 按 Agent 协议探测；`/v1/models` 不能替代 Responses、Anthropic Messages 或 Chat Completions 检查。
- Wails 生产构建不能使用 `server` tag；浏览器 E2E 才能使用 server/e2e fake runner。
- Linux 发行构建固定使用 `gtk3` tag，Wails Alpha 阶段只允许 `technical-preview-unsigned`。

## 文档维护

README、workflow、Taskfile、Dockerfile 和 AI Agent Kit 里的命令必须对应当前仓库文件。历史 ADR 可以保留背景，但必须明确标记为 Superseded，不能作为操作指南。
