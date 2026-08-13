# BootAgent 开发约定

当前代码线是 Go/Wails 稳定化主线。不要在本文硬编码发布版本；发布版本以 Git tag、
GitHub Release 和构建工作流注入的版本为准。唯一产品入口是
`cmd/bootagent-desktop`，React 只能通过生成的 Wails bindings 调用后端。

## 目录

- `internal/app`：Status、Provider、Agent、Profile、Runtime、DesktopAgent、安装、
  启动、设置和更新等用例，以及统一的写操作锁。
- `internal/catalog`：嵌入的 `agents.lock.json`、`providers.lock.json`、
  `runtimes.lock.json`，以及内置 Provider 目录。
- `internal/config`：TOML/JSON/JSONC 适配器、配置发现和 golden fixtures。
- `internal/install`：默认安装最新版或可选精确版本的 Agent 包、registry 选择、
  Node.js/uv runtime 引导（下载、校验、解压和 PATH 更新），以及 Aider 的 Python
  管理边界。
- `internal/profile` 与 `internal/securefs`：Profile、密钥、备份、权限和原子写入。
- `internal/mcp`：MCP 规范化模型、秘密处理、Registry 私有存储、导入导出，以及 Claude Code、Codex、OpenCode、Kilo CLI 和 Hermes 原生配置适配器。
- `internal/binding`：React 与 Go 之间唯一的边界。`Services` 聚合 Status、Provider、
  Agent、Profile、Runtime、DesktopAgent、Transfer 和 MCP 八个服务；UpdateService 在桌面
  入口中单独注册。这里的 DTO 发生变化时，必须重新生成 `frontend/bindings`，并同步
  `frontend/src/backend/wails.ts` 与 `frontend/src/types/api.ts`。
- `cmd/bootagent-desktop`：Wails 桌面入口。
- `frontend/bindings`：Wails 生成文件，禁止手工编辑。

`cmd/bootagent-release`、`cmd/bootagent-rc` 和 `cmd/bootagent-provider-smoke` 已在
`23805b0` 删除，其职责转移到 `.github/workflows/build-artifacts.yml`。历史文档中
出现这些命令只代表背景，不是可执行指令。

公共站点已迁移到
[MaimoryLab/BootAgent-site](https://github.com/MaimoryLab/BootAgent-site)，本仓库不再
包含 `site/`。站点把发布 tag 中的 `agents.lock.json` 和 `providers.lock.json` 复制到
自己的 `data/` 目录；发布完成后，构建工作流会通知站点刷新。直接修改本仓库的
`main` 不应立即改变站点，因为站点描述的是已发布版本支持的内容。

`providers.lock.json` 是内置 Provider endpoint、fallback model 和公共站点商业披露
字段的唯一事实来源。用户 Provider 与内置 Provider 覆盖项保存在
`~/.bootagent/providers.json`。

## 本地命令

```bash
go test ./...
go test -race ./...
go vet ./...
python3 -m unittest scripts/test_generate_third_party_licenses.py
python3 scripts/generate_third_party_licenses.py --check
python3 scripts/check-docs.py
cd frontend
pnpm install --frozen-lockfile
pnpm run test
pnpm run build
pnpm run test:e2e
```

`.github/workflows/ci.yml` 在 pull request 与 `main` push 上运行四组门禁：Go、
Frontend、Docs 和 Release compliance。Go 门禁还运行 `staticcheck`。

`.github/workflows/build-artifacts.yml` 在推送 `vX.Y.Z` tag 时构建并发布 release；
`workflow_dispatch` 用于手工构建验证产物，不会发布 release。

应用常规测试与 Wails 构建不依赖 Python。文档和发行合规脚本使用 Python 3。
安装 Aider 需要 Python 3.12，但不要求预装：uv 会复用匹配的本地解释器，或把托管的
CPython 下载到 `~/.bootagent/runtimes/python`。发布包仍不内置 Python。

## CodeGraph

本仓库已建立索引（`.codegraph/` 不提交；用 `codegraph index .` 重建，通常约
0.5 秒）。定位或理解代码时，先用 CodeGraph，再用 grep：

```bash
codegraph explore "binding Service Install"
```

CodeGraph 在本仓库最有价值的用途是串联 Go 与前端。查询 `AgentService` 会同时列出
`internal/binding/services.go`、生成的 `frontend/bindings/.../index.ts` 和手写的
`frontend/src/backend/wails.ts`。后端 DTO 变化时，这些位置必须一起更新。
`frontend/src/types/api.ts` 的类型是手写的，并未直接导入 bindings，因此后端 DTO 与
该文件是两套事实来源；索引是发现漏改位置的最快方式。

**已知限制**：blast-radius 的 “no covering tests found” 只检查直接调用者。高层函数
内部调用的实现可能被误报为未测试。不要因此重复添加测试，先确认完整调用路径。

## 代码边界

- `agents.lock.json` 是 Agent 元数据的唯一事实来源，但不保存 Agent 版本或包哈希。
  新增自动配置 Agent 时，先加入包名和元数据，再增加对应配置适配器与 Go 测试。
- 子进程必须使用 argv 数组和受控环境，设置超时，并保留经过脱敏的诊断输出。
- 写入顺序必须是：私有目录、备份、同目录临时文件、收紧权限、原子替换。如果密钥
  备份无法收紧权限，删除备份并失败退出。
- API key 禁止进入普通 Profile、状态摘要、日志、URL、React 全局状态、浏览器存储或
  测试产物。只有 Provider 编辑与配置表单可以按需通过本地 binding 从私有存储读取
  key。
- Provider 探测必须使用 Agent 实际协议。`/v1/models` 不能替代 Responses、
  Anthropic Messages 或 Chat Completions 检查。
- Wails 生产构建禁止使用 `server` tag；只有浏览器 E2E 可以使用 server/e2e fake
  runner。
- Linux release 构建必须使用 `gtk3` tag。项目仍处于 Wails Alpha 发布政策阶段时，
  只允许 `technical-preview-unsigned` 渠道。

## 文档维护

`docs/` 按受众组织，新增文档前先选对目录：

- `docs/` 根目录保存读者可直接执行的当前规范与政策。
- `docs/ai-agent-kit/` 保存用户说明，默认路径是下载 release，而不是从源码构建。
- `docs/decisions/` 保存 ADR。被替代的决策必须保留，标记为 Superseded 并链接到替代
  ADR，禁止重写历史。
- `docs/internal/` 保存维护者完成记录和验收清单。未实施计划应进入 issue，不能放在
  这里。

## 文档语言

所有公开文档默认使用英文，只有下列例外：

| 位置 | 语言 |
| --- | --- |
| `README.md` | 英文；中文位于 `README_ZH.md`，两者必须同步 |
| `docs/` 根目录规范与 `docs/decisions/` ADR | 仅英文 |
| `docs/ai-agent-kit/` | `en/` 与 `zh/` 下提供完整双语集合 |
| `AGENTS.md` 与 `docs/internal/` | 中文；受众是维护者，双语副本只会产生漂移 |

英文产品术语以 `frontend/src/i18n.tsx` 为事实来源：runtime = Runtimes，
configuration template = Profiles，environment overview = Environment overview，
guide only = Guide only，activation steps = Setup steps。不要为文档另建一套词汇。
`Agent`、`Provider`、`Profile`、`technical-preview-unsigned` 和
`agents.lock.json` 等标识符不翻译。

**UI 文案与公开文档方向相反**：`translate()` 仅在 `locale === "en"` 时查找翻译，
否则直接返回中文 key，因此中文是 i18n 源语言。新增 UI 文案时先写中文 key，再补英文
翻译。不要因为公开文档使用英文就改 key，否则必须同步修改所有关联项。

`python3 scripts/check-docs.py` 检查相对链接和英文文档中的残留中文；文档变更后必须
运行，`.github/workflows/ci.yml` 也会执行。

README、workflow、Taskfile 与 AI Agent Kit 中的命令必须对应当前仓库文件。
`docs/internal/` 若引用已删除工具，代码块使用 ` ```text ` 而不是 ` ```bash `，避免
被误认为可执行指令。

`LICENSE` 是 Apache-2.0，`NOTICE` 是第三方归属的唯一事实来源。新增随包分发的依赖或
UI 中的新第三方标志时，必须同步更新 `NOTICE`；
`docs/distribution-compliance-policy.md` 将其列为发布前置条件。

## 社区健康文件

以下公开社区文件全部使用英文，并必须与仓库当前行为保持一致：

- `CODE_OF_CONDUCT.md`
- `CONTRIBUTING.md`
- `SECURITY.md`
- `.github/ISSUE_TEMPLATE/*.yml` 与 `.github/ISSUE_TEMPLATE/config.yml`
- `.github/PULL_REQUEST_TEMPLATE.md`

Issue Forms 与 PR 模板中的命令、路径、支持平台和安全提示必须随代码与 CI 同步。
安全漏洞只能按 `SECURITY.md` 私下报告，Issue Forms 不得引导用户公开披露漏洞或 API
key。行为准则事件使用 GitHub 的内容举报功能发送给仓库管理员。

“Repository admins accept content reports” 是 GitHub 仓库设置，不在 Git 中。迁移仓库、
调整 moderation 设置或修改社区文件后，必须确认该设置仍启用，并通过 GitHub Community
Standards 页面或以下 API 复核：

```bash
gh api repos/MaimoryLab/BootAgent/community/profile
```

## README 维护

- 修改用户可见行为、支持的 Agent/Provider、架构、前置条件、路径或构建、测试、发布
  命令后，同一变更中必须同步更新 `README.md` 与 `README_ZH.md`，无需等待提醒。
- 英中版本必须保持同步。纯内部变更且不影响已记录行为时，不要制造 README 噪音。
- README 声明必须对照当前代码、manifest、Taskfile 与 workflow 验证，然后运行
  `python3 scripts/check-docs.py`。
