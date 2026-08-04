# OneAgent 开发约定

当前分支是 Go/Wails 收尾线，版本为 `0.3.0-dev`。桌面入口是 `cmd/oneagent-desktop`，headless CLI 是 `cmd/oneagent`；两者共享 `internal/` 用例。React 只通过生成的 Wails bindings 调用后端。

## 目录

- `internal/app`：Status、Provider、Agent、Profile 用例和写入协调锁。
- `internal/catalog`：嵌入的 `agents.lock.json`、`providers.lock.json`、`runtimes.lock.json` 与内置 Provider 目录。
- `internal/config`：TOML/JSON/JSONC 适配器、配置发现和 golden fixtures。
- `internal/install`：默认最新、可选精确版本的 Agent 包安装，registry 选择、Node.js/uv 运行时引导（下载、校验、解压、写入 PATH）和 Aider Python 管理边界。
- `internal/profile`、`internal/securefs`：profile、secret、备份、权限和原子写。
- `internal/binding`：Wails 暴露给前端的五个 service，是 React 与 Go 的唯一接缝。改这里的 DTO 必须重新生成 `frontend/bindings` 并同步 `frontend/src/backend/wails.ts`。
- `cmd/oneagent`：纯 Go headless CLI。
- `cmd/oneagent-desktop`：Wails 桌面入口。
- `frontend/bindings`：Wails 生成物，禁止手工编辑。

`cmd/oneagent-release`、`cmd/oneagent-rc`、`cmd/oneagent-provider-smoke` 已于 `23805b0`
移除，职责交给 `.github/workflows/build-artifacts.yml`。历史文档里提到它们的地方是背景，
不是可执行指南。

公开站已迁出到 [MaimoryLab/OneAgent-site](https://github.com/MaimoryLab/OneAgent-site)，本仓库不再有 `site/`。它把 `agents.lock.json` 和 `providers.lock.json` vendor 到自己的 `data/` 下，从发行 tag 而不是本仓库 `main` 刷新——改这两个文件不会自动反映到站上，也不应该：站描述的是已发布版本支持什么。

`providers.lock.json` 是内置 Provider 端点、fallback model 和公开站商业披露字段的真源；用户 Provider 与内置覆盖保存在 `~/.oneagent/providers.json`。

## 本地命令

```bash
go test ./...
go test -race ./...
go vet ./...
go build -o bin/oneagent ./cmd/oneagent

cd frontend
pnpm install --frozen-lockfile
pnpm run test
pnpm run build
pnpm run test:e2e
```

每个 pull request 由 `.github/workflows/ci.yml` 跑上面这两组门。发行包由
`.github/workflows/build-artifacts.yml` 手动触发构建。

普通测试、Wails 构建、站点构建和发布工具不需要 Python。安装 Aider 需要 Python 3.12，但不再要求本机预装：uv 自己解析解释器，本机有匹配版本就复用，否则下载一份托管 CPython 到 `~/.oneagent/runtimes/python`。Python 仍然不进发行包。

## CodeGraph

本仓库已建索引（`.codegraph/`，不提交，重建用 `codegraph index .`，约 0.5s）。定位或理解代码时**先用它，别 grep**：

```bash
codegraph explore "binding Service Install"
```

对这个仓库最有用的一点：它把 Go 与前端连起来。查 `AgentService` 会同时列出 `internal/binding/services.go`、生成的 `frontend/bindings/.../index.ts` 和手写的 `frontend/src/backend/wails.ts`——也就是改一个后端 DTO 需要一起动的三处。`frontend/src/types/api.ts` 里的类型是手写的而非从 bindings 导入，所以后端 DTO 与它是**两份真源**，索引是发现漏改的最快方式。

**一个已知局限**：blast radius 的「⚠️ no covering tests found」只看**直接**调用者，被上层函数内部调用的实现会被误标成无测试。不要照着这个提示补已经存在的测试，先确认调用链。

## 代码边界

- `agents.lock.json` 是 Agent 元数据唯一真源，但不保存 Agent 版本或包哈希。新增自动配置 Agent 时先补包名和元数据，再添加对应 config adapter 和 Go 测试。
- 子进程必须使用 argv 数组和受控环境，设置超时并保留可诊断但已脱敏的输出。
- 写入顺序必须是私有目录、备份、同目录临时文件、收紧权限、原子替换；密钥备份无法收紧时删除并失败。
- 不把 API Key 写入普通 profile、状态摘要、日志、URL、全局 React state、浏览器存储或测试附件；仅 Provider 编辑/配置表单可通过本机 binding 按需读取私有存储中的 Key。
- Provider 按 Agent 协议探测；`/v1/models` 不能替代 Responses、Anthropic Messages 或 Chat Completions 检查。
- Wails 生产构建不能使用 `server` tag；浏览器 E2E 才能使用 server/e2e fake runner。
- Linux 发行构建固定使用 `gtk3` tag，Wails Alpha 阶段只允许 `technical-preview-unsigned`。

## 文档维护

`docs/` 按受众分层，新增文档时先选对位置：

- `docs/` 根：当前有效的规范与政策，读者可以直接照着执行。
- `docs/ai-agent-kit/`：面向使用者的操作文档。默认路径是下载发行包，不是从源码构建。
- `docs/decisions/`：ADR。被推翻的决策保留原文并标注 Superseded 及其去向，不删改历史。
- `docs/internal/`：维护者视角的完工记录与验证清单。不放未实施的计划——那属于 issue。

## 文档语言

对外文档一律英文，只有两处例外提供中文：

| 位置 | 语言 |
| --- | --- |
| `README.md` | 英文；中文在 `README_ZH.md`，两份内容必须同步 |
| `docs/` 根的规范、`docs/decisions/` 的 ADR | 仅英文 |
| `docs/ai-agent-kit/` | 双语，`en/` 与 `zh/` 各自成套 |
| `CLAUDE.md`、`docs/internal/` | 中文。读者是维护者自己，双语只增加分叉成本 |

英文术语以 `frontend/src/i18n.tsx` 为准，那是产品界面的真源：运行时 = Runtimes、
配置模板 = Profiles、环境总览 = Environment overview、仅引导 = Guide only、
激活步骤 = Setup steps。文档另起一套译法会让用户在界面和文档之间对不上。
`Agent`、`Provider`、`Profile`、`technical-preview-unsigned`、`agents.lock.json`
这类标识符不译。

**界面文案的方向与文档相反**：`translate()` 只在 `locale === "en"` 时查表，否则直接
返回中文 key，所以 i18n **以中文为源语言**。新增界面文案仍然先写中文 key 再补英文
翻译，不要因为文档转英文就去改 key——那会连带改掉全部条目。

`python3 scripts/check-docs.py` 检查相对链接是否解析、英文文档里是否残留中文。
改动文档后跑它；`.github/workflows/ci.yml` 也会跑。

README、workflow、Taskfile 和 AI Agent Kit 里的命令必须对应当前仓库文件。`docs/internal/`
里引用已移除工具的命令块用 ` ```text ` 而不是 ` ```bash `，避免被当成可运行指令。

`LICENSE` 是 Apache-2.0，`NOTICE` 是第三方归属的真源。新增随包分发的依赖，或在界面里加入新的第三方标识时，必须同步 `NOTICE`——`docs/distribution-compliance-policy.md` 把它列为发布前置条件。
