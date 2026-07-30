# OneAgent 全量迁移至 Wails v3 规划方案

- 状态：In Progress（阶段 0-3 退出门禁已通过，阶段 4 进行中，生产入口尚未切换）
- 日期：2026-07-30
- 目标版本：下一主版本（建议 `0.3.0-dev` 开始迁移）
- 适用范围：桌面应用、Go 核心、CLI、前端通信、测试、构建、发布和公开站数据生成

当前进度：

- 阶段 0-1 已完成：Go module、嵌入式 Agent catalog、稳定错误/平台类型、纯 Go CLI
  和带 `wails` 构建标签的桌面空壳；status/catalog fixture 由 Python 当前实现冻结，
  Go 测试独立读取。
- 阶段 2 已完成：Provider URL 校验、模型发现、三协议探测、原子写、备份、Unix mode
  与 Windows ACL、profile/secret/Agent binding store 和五个配置 adapter 均已移植；
  `internal/config` 另有一道 JSON 写入器 parity 门禁，直接与 Python 写入器逐字节比对。
- 阶段 3 已完成：安装编排、prerequisite、版本解析、npm/uv 安装、integrity 校验、
  日志脱敏和 `agent list/set` 均在 Go 中，`scripts/install.sh` 与 `.ps1` 已改为纯
  转发层调用 Go CLI，不再定位 Python；`tests/install_test.sh` 的 13 项契约现在由
  Go CLI 通过。CLI 支持中断取消（退出码 130），帮助文本保持 argparse 的双破折号形式。
- 阶段 4 进行中：四个 service 已注册，`frontend/bindings/` 已生成并提交，
  `frontend/src/backend/` 薄 adapter 已按运行时选择 Wails 或 HTTP 传输。尚未删除
  `frontend/src/api/client.ts` 的 fetch 路径，手写后端 DTO 也尚未改为生成类型别名。

现有 Python 核心、HTTP GUI 和发布流程仍是当前生产路径；在阶段 4-6 的行为等价门禁
通过前不得删除或旁路它们。

## 1. 结论

OneAgent 应迁移为“Wails v3 桌面壳 + Go 领域核心 + React/Vite 前端 + 独立 Go CLI”的结构。生产桌面应用不再启动 localhost HTTP Server，React 只通过 Wails 自动生成的 TypeScript binding 调用已注册 Go service；CLI 与桌面 service 共用同一套 Go use case，不复制安装、配置或安全逻辑。

本次“去除 Python 依赖”按仓库级目标执行：

- OneAgent 的源码开发、测试、构建、运行和发布均不要求 Python。
- 删除所有受版本控制的 `.py`、PyInstaller、wheel/setuptools 和 Python CI 步骤。
- `site/` 保持 Astro 静态站，不塞入 Wails；它当前依赖的 Python 数据生成器改为 TypeScript。
- Aider 是唯一需要单独解释的外部边界：Aider 上游本身是 Python 工具。建议保留“检测、配置已安装 Aider”和“用户明确选择时通过 `uv` 安装”的能力，但将 Python 3.12 明确标记为 **Aider 的可选上游前置条件**，不是 OneAgent 的运行依赖。若产品要求目标机器绝对不能存在 Python，则必须把 Aider 自动安装降为 guide-only；两者不能同时成立。

迁移采用契约优先、分阶段切换，不做一次性重写。Python 生产入口只在 Go 实现尚未达到行为等价时保留；正式切换后不提供 Python fallback，否则无法证明已经完成去依赖。

## 2. 迁移目标与非目标

### 2.1 必须达成

1. React 生产代码中不存在 `fetch("/api/...")`、Cookie 会话或 localhost API fallback。
2. Wails 仅注册明确列出的 service；不使用 `RawMessageHandler`，不为业务 service 配置 HTTP `Route`。
3. 当前 Python 核心的行为契约保持不变：版本锁定、npm integrity 校验、Provider 多协议探测、原子写入、备份、Unix mode、Windows ACL、配置保留合并、密钥脱敏和稳定错误码。
4. `agents.lock.json` 继续作为 Agent 行为和版本的唯一真源，使用 `go:embed` 进入桌面与 CLI 产物。
5. 桌面应用与 CLI 共用 Go 核心；CLI 不依赖 GTK/WebKit，适合 headless 和自动化环境。
6. macOS arm64/x64、Windows x64、Linux x64 均在目标系统原生构建并取得 cleanroom 证据。
7. 迁移完成后，未安装 Python 的干净环境可以构建、测试并运行 OneAgent 的全部非 Aider-install 流程。

### 2.2 不在本次范围

- 不把公开站 `site/` 改成桌面应用或 Wails Server 部署。
- 不新增统一网关、代理、VPN、后台 daemon、遥测或共享 API Key。
- 不顺带引入自动更新、应用商店、Stable 签名策略变更或新的 Agent。
- 不借迁移重设计七页向导、profile schema、Agent 配置格式或错误码。
- 不把 guide-only Agent 变成自动配置 Agent。

## 3. 当前基线与不可丢失的契约

| 当前模块 | 当前职责 | 迁移后的归属 |
| --- | --- | --- |
| `oneagent/catalog.py` | manifest、Provider、平台和 HOME 解析 | `internal/catalog`、`internal/platform` |
| `oneagent/providers.py` | URL 校验、模型发现、三协议探测 | `internal/provider` |
| `oneagent/installer.py` | 安装、配置适配、权限、备份、profile、status | `internal/install`、`internal/config`、`internal/securefs`、`internal/profile` |
| `oneagent/server.py` | 静态资源、本地 HTTP API、Cookie/Origin | Wails Assets + Go services；HTTP 层删除 |
| `oneagent/cli.py` | 结构化 CLI | `cmd/oneagent` |
| `oneagent/entrypoint.py`、`scripts/gui.py` | GUI/CLI 分流和源码入口 | `cmd/oneagent-desktop`、`cmd/oneagent` |
| `frontend/src/api/client.ts` | fetch、HTTP 错误归一化 | `frontend/src/backend/wails.ts` |
| `packaging/oneagent.spec` | PyInstaller onedir | Wails v3 Taskfile 与 `build/config.yml` |
| Python 发布脚本 | 构建、校验、notice、manifest、官网数据 | Go release tools + `site/scripts/*.ts` |

以下是迁移验收契约，不是实现细节：

- Key 不进入 profile、Agent binding、URL、日志、事件、React reducer、浏览器存储或测试报告。
- 写入顺序保持为：私有目录 -> 备份 -> 同目录临时文件 -> 权限收紧 -> 原子替换。
- 密钥备份如果无法收紧权限，必须删除并失败，不能留下宽权限副本。
- Codex TOML 与 Claude/OpenCode/Kilo JSON 保留非 OneAgent 管理字段；无法无损处理的 JSONC 注释继续明确拒绝，不能静默覆盖。
- 子进程只使用参数数组和受控环境，禁止 shell 拼接；超时和取消必须终止子进程。
- npm 镜像必须显式选择，HTTPS、无 URL 凭据，并把 registry 返回的 integrity 与 lock 中的 `sha512-` 值比较。
- Codex、Claude Code、OpenCode/Kilo/Aider 分别按 Responses、Anthropic、OpenAI-compatible 协议验证，不能用 `/v1/models` 代替真实协议探测。
- `INVALID_REQUEST`、`PREREQUISITE_MISSING`、`CONFIG_WRITE_FAILED` 等既有错误码和 CLI 退出码保持；`INVALID_ORIGIN` 在 HTTP 删除后保留为已废弃保留值，不复用。

## 4. Wails v3 官方约束与本项目决策

以下结论核对自 2026-07-29 的 [Wails v3 官方文档](https://v3.wails.io/)：

- Wails v3 仍为 Alpha，当前 changelog 最新条目为 [`v3.0.0-alpha2.118`](https://v3.wails.io/changelog)。官方状态页的下一目标仍是 Beta。
- 当前安装文档要求 Go 1.25+。
- service 通过 `application.NewService(...)` 或 `application.NewServiceWithOptions(...)` 注册，只有导出方法进入 binding。
- `wails3 generate bindings -ts` 生成 TypeScript；`wails3 dev` 在 Go 代码变化时自动重生 binding。
- JSON tag 控制前端字段名；struct、slice、map、指针和命名常量可生成对应 TypeScript 模型。
- Go 方法返回的自定义 error 会成为 `Call.RuntimeError`，结构化 JSON 位于 `error.cause`；`ServiceOptions.MarshalError` 可固定序列化格式。
- 每个 binding 调用运行在独立 goroutine，service 是跨窗口共享的 singleton，状态与写操作必须自行同步。
- 方法可接收 `context.Context`；前端断开时会自动取消，适合向 `net/http` 和 `exec.CommandContext` 传递。
- Go 到前端的流式通知使用 `app.Event.Emit`，前端使用 `Events.On`；前端发起请求仍应使用 service binding。
- 生产资源由 `go:embed` 和 `application.AssetFileServerFS` 内嵌；桌面模式不开放网络端口。
- `server` build tag 会启用纯 HTTP Server，适合 CI 浏览器测试，但绝不能进入生产构建。
- Linux 默认栈要求 WebKitGTK 6.0；Ubuntu 22.04 只可走 legacy `gtk3` build tag，默认 GTK4 栈要求 Ubuntu 24.04+。

重点参考：[Installation](https://v3.wails.io/quick-start/installation/)、[Method Bindings](https://v3.wails.io/features/bindings/methods/)、[Services](https://v3.wails.io/features/bindings/services/)、[Server Build](https://v3.wails.io/guides/server-build/)、[Status](https://v3.wails.io/status/) 和 [Changelog](https://v3.wails.io/changelog/)。

据此作出四项默认决策：

1. **版本锁定**：第 0 阶段以 `v3.0.0-alpha2.118` 为候选基线，完成三个操作系统、四个原生构建目标的 spike 后，把 Go module、Wails CLI 和 `@wailsio/runtime` 锁到相互兼容的精确版本；源码、CI 和文档均不得使用 `@latest`。
2. **发布渠道**：Wails 仍为 Alpha 时，OneAgent 只发布 `technical-preview-unsigned`；不得因为应用功能完成而自动提升 Stable。
3. **Linux 兼容**：迁移期保留 ADR-003 的 Ubuntu 22.04+ 承诺，Linux 正式产物使用 `gtk3` tag，并在 Ubuntu 22.04 cleanroom 验证。改为 GTK4/Ubuntu 24.04+ 必须另立 ADR。
4. **双入口**：发布 Wails 桌面入口和纯 Go CLI 两个二进制。不要让 headless CLI 链接 Wails/GTK，以免“去 Python”后又引入不必要的 GUI 运行依赖。

## 5. 目标架构

```text
React 19 + TypeScript + Vite
              |
              | generated Wails TypeScript bindings
              v
StatusService / ProviderService / AgentService / ProfileService
              |
              v
        Go application use cases
              |
       +------+------+---------+
       |             |         |
 catalog/provider  install   profile/config
       |             |         |
       +------ securefs/process/platform -----+

Pure Go CLI --------------------^  (same use cases)

Astro public site -------------- release manifests/catalog only
```

### 5.1 建议目录

```text
cmd/
  oneagent-desktop/       Wails application and window setup
  oneagent/               headless CLI
  oneagent-release/       build, manifest, checksum, notice checks
internal/
  app/                    use cases and operation coordinator
  binding/                Wails services and transport DTOs only
  catalog/                embedded agents.lock.json and provider catalog
  provider/               URL validation, model listing, protocol probes
  install/                orchestration and locked package installation
  config/                 per-Agent readers/writers and merge logic
  profile/                profile, secret and Agent binding stores
  securefs/               backup, mode/ACL and atomic replace
  process/                injectable command runner
  platform/               HOME, OS, arch, shell and path resolution
frontend/
  bindings/               generated by Wails; never edited manually
  src/backend/            thin production adapter and error normalisation
site/scripts/             TypeScript release-index/catalog generators
build/                    Wails Taskfiles, config, icons and platform metadata
```

`internal/binding` 只做输入 DTO、错误翻译和 use case 调用，不放安装业务。CLI 直接调用 `internal/app`，不能反向调用 Wails service。

### 5.2 Service binding 契约

| 现有 HTTP 契约 | Wails 方法 | 说明 |
| --- | --- | --- |
| `GET /api/status` | `StatusService.GetStatus(ctx)` | 返回现有 `StatusResponse` |
| `GET /api/profiles` | `ProfileService.ListProfiles(ctx)` | 返回公开摘要，不含 Key |
| `POST /api/profiles` | `ProfileService.SaveProfile(ctx, request)` | 保存模板和可选 secret |
| `POST /api/probe` | `ProviderService.Probe(ctx, request)` | 按所选 Agent 协议探测 |
| `POST /api/models` | `ProviderService.ListModels(ctx, request)` | 返回模型列表与稳定诊断 |
| `POST /api/install` | `AgentService.Install(ctx, request)` | 保持一次调用、最终逐 Agent 结果 |
| `POST /api/agents/<id>/activate` | `AgentService.Activate(ctx, request)` | `agentId` 改为 DTO 字段 |
| `POST /api/open-register` | `ProviderService.OpenRegistration(ctx, request)` | Go 端按 Provider ID 解析 URL |

迁移期保持现有字段名和 null/缺省语义，使用 Go JSON tag 让生成的 TypeScript 与现有 React 契约一致。不要同时做 snake_case/camelCase 清理。`frontend/src/types/api.ts` 中属于后端 DTO 的手写类型最终删除或改成生成类型的薄别名，避免 Go 与 TypeScript 两份真源。

### 5.3 错误契约

Go 中实现可 JSON 序列化的 `OneAgentError`：

```go
type OneAgentError struct {
    Code      string `json:"error_code"`
    Message   string `json:"message"`
    Status    int    `json:"status"`
    Retryable bool   `json:"retryable"`
    ExitCode  int    `json:"exit_code"`
}
```

- `Error()` 只返回脱敏后的 `Message`。
- Wails service 用 `MarshalError` 固定 `cause` 的 JSON 结构。
- 前端 adapter 只在捕获到 `Call.RuntimeError` 时读取 `cause`；参数类型错误、binding 不存在和其他 bridge 故障统一映射为 `INTERNAL_ERROR`。
- CLI 继续输出现有 JSON shape 和退出码。
- release 模式不得启用会记录 binding 参数的 Debug 日志；API Key 不得进入 Wails logger、panic、event 或 DevTools。

### 5.4 并发、取消与进度

当前 `HTTPServer` 是串行的，而 Wails binding 默认并发。Go 端必须新增共享 `OperationCoordinator`：

- `Install`、`Activate`、`SaveProfile` 和任何状态迁移共用写锁，避免两个调用同时备份或覆盖同一配置。
- `Status`、`ListProfiles` 等读操作只读取完整文件；原子替换保证不会看到半写状态。
- Provider 请求和子进程都接收 binding 的 `context.Context`，分别传给 `http.NewRequestWithContext` 与 `exec.CommandContext`。
- 第一版保持当前同步语义：前端 await 一个 Promise，显示不定进度和最终结果，不伪造百分比。
- 只有确有逐 Agent 阶段信息时才增加 `oneagent:install:progress` 事件；事件载荷不得包含 Key、完整环境、stdout/stderr 或配置内容，并必须在 React unmount 时调用 unsubscribe。

### 5.5 桌面安全边界

HTTP 删除后，Cookie、Host 和 Origin 校验也随之删除，但不能简单删掉安全测试：

- 生产包不得监听 TCP 端口；`server` tag 只允许出现在 `e2e`/CI task。
- 只注册四个业务 service，不配置 HTTP Route，不启用 Raw Message Handler。
- 所有 binding 输入仍在 Go 端完整校验，不能信任 React 类型。
- `OpenRegistration` 只接收 Provider ID，由 Go catalog 解析并校验 `http/https` URL，再调用 `app.Browser.OpenURL`；不允许前端传任意 URL。
- 保留严格生产 CSP，允许 Wails runtime、自有资源和 data image，禁止远程脚本、远程字体、frame 和 object。开发 CSP 单独允许 Vite HMR。
- release 构建关闭 DevTools 和 bridge Debug 日志；产物扫描 source map、远程资源、secret 和测试 adapter。

## 6. Go 核心迁移设计

### 6.1 Runtime 注入

把当前 Python `Runtime` 拆成小接口，而不是做一个无边界的全局容器：

- `Runner`：`Run(ctx, argv, env, timeout)` 和 `LookPath`。
- `Platform`：OS、arch、HOME、shell、环境变量快照。
- `FileSecurity`：私有目录、私有文件、ACL/mode、原子替换。
- `Clock`：UTC 时间与备份时间戳。
- `HTTPDoer`：Provider 请求，测试可替换为本地 mock transport。

测试通过临时目录和 fake runner 覆盖 npm、uv、Windows/macOS/Linux，不修改真实 HOME。

### 6.2 securefs

- Unix 使用 `0700` 目录、`0600` 文件。
- Windows 使用平台文件实现 ACL；可以继续以参数数组调用系统 `icacls`，或使用 `golang.org/x/sys/windows`，但验收必须检查真实 ACE 只含当前用户和 SYSTEM。
- 临时文件必须建在目标目录；先写入、flush/close、收紧权限，再替换目标。
- Windows 的覆盖替换单独实现并测试，不假设 `os.Rename` 与 Python `os.replace` 完全等价。
- 备份命名、冲突递增、metadata 和 secret 失败清理保持现状。

### 6.3 配置适配器

- 继续按 `config_adapter` 注册适配器，不按 Agent ID 散落分支。
- Codex TOML 使用 Go TOML parser 做语法验证，沿用“只替换顶层 `model/model_provider` 与 OneAgent table”的保留式合并。
- Claude/OpenCode/Kilo 使用 `encoding/json` 合并对象；对带注释 JSONC 继续 fail closed，除非另一个独立变更先证明可无损保留注释。
- Aider env/PowerShell quoting、Agent 独立 env、native env 和 shared legacy env 保持 golden fixture 等价。
- profile v1 -> v2 migration、路径校验、ID 正则和 Agent binding schema 保持可回滚兼容。

### 6.4 安装与 Provider

- 使用 `exec.CommandContext`，禁止 `cmd /c`、`sh -c` 和字符串拼接。
- 保留锁定版本、`latest` 仅显式生效、npm mirror integrity 校验和最多 600 字符的脱敏失败摘要。
- Go `net/http` 设置总 timeout、响应体上限和 context；不关闭 TLS 校验，不自动代理或切换镜像。
- 三种协议请求、状态码分类、`PROTOCOL_UNSUPPORTED` 判定和模型 fallback 用当前测试样例固化。
- 内置 Provider 与公开字段继续显式投影，避免内部 fallback model 泄漏到前端。

## 7. 分阶段实施

### 阶段 0：ADR、版本锁定与四个原生构建目标 spike

交付：

- 新 ADR 只替换 ADR-003 中“Python 核心、本地 HTTP、PyInstaller”的决策，其余产品边界继续有效。
- 创建最小 Wails v3 React 项目，验证 `application.NewService`、`generate bindings -ts`、自定义 error `cause`、`context.Context` 取消、`go:embed` 和 `Browser.OpenURL`。
- 在 Windows x64、macOS arm64/x64、Ubuntu 22.04 `gtk3` 各构建并启动一次。
- 锁定 Go、Wails module、Wails CLI、`@wailsio/runtime` 和 Taskfile 版本。

退出门禁：四个原生构建目标 spike 全绿；确认 Alpha 版本可接受；确认生成 binding 的实际 import 形态和错误序列化；确认 Ubuntu 22.04 路径。

### 阶段 1：Go 骨架与 catalog

交付：

- 建立 `go.mod`、`cmd/`、`internal/`、Wails `Taskfile.yml` 和 `build/config.yml`。
- 嵌入并校验 `agents.lock.json`，移植 catalog、Provider 公开投影、平台/HOME 解析和错误码。
- 建立纯 Go CLI 骨架与 Wails 空壳，尚不切换生产入口。

退出门禁：catalog/status fixture 与 Python 输出等价；Go 测试可在无 Python 环境运行。

### 阶段 2：Provider、securefs、profile 与配置适配器

交付：

- 移植 URL 校验、模型发现和三协议 probe。
- 移植原子写、备份、Unix mode、Windows ACL、profile、secret store、Agent binding store。
- 移植五个配置 adapter 和配置发现读取器。

退出门禁：每个 adapter 的新文件、合并、损坏输入、备份和权限 fixture 等价；Windows 真实 ACL 与 Unix mode 测试通过；fuzz 不产生路径逃逸或 secret 泄漏。

### 阶段 3：安装编排与 CLI

交付：

- 移植 prerequisite、版本解析、npm/uv 安装、integrity、registry、日志脱敏、`install_many` 与单 Agent activate。
- 完成 `cmd/oneagent`，兼容现有 flags、`agent list/set`、JSON 输出和退出码。
- `scripts/install.sh`/`.ps1` 暂时保留为纯转发兼容层，改为调用 Go CLI，不再定位 Python；一个发行周期后再评估删除。

退出门禁：fake npm/uv 契约、真实锁定 Agent 安装、CLI 快照和取消/超时全部通过；Aider 边界已按第 1 节落地。

阶段 3 已通过（2026-07-30）。证据：

- `tests/install_test.sh` 的 13 项契约经 `scripts/install.sh` 转发到 Go CLI 全部通过，
  且不再需要 PATH 上有 Python。
- 包装脚本是纯转发层，不做按需构建：调用方使用临时 HOME，`go build` 会把 module
  cache 写进去。二进制缺失时报 `PREREQUISITE_MISSING` 语义的退出码 3。
- 容器 cleanroom 新增 `go-cli-no-python` 阶段：在 `PATH=/usr/bin:/bin`（无 Python、
  无 Node、无包管理器）下完成一次真实 Codex 配置写入，并断言 API Key 不进入 JSON 结果。
- `go vet`、`go test ./...`、`go test -race ./...`、staticcheck 和 govulncheck 均已
  纳入 CI，且当前全绿；JSON 写入器 parity 门禁在 CI 中以 `ONEAGENT_REQUIRE_PARITY=1`
  运行，不允许静默跳过。

### 阶段 4：Wails service 与 React binding 切换

交付：

- 注册 `StatusService`、`ProviderService`、`AgentService`、`ProfileService`，共享同一 use case 和协调锁。
- 生成并提交 `frontend/bindings/`；生成物禁止手改，CI 重生后执行 diff 检查。
- 新增 `frontend/src/backend/wails.ts`，页面和 `WizardContext` 经此薄 adapter 调用生成 binding。
- 使用 `Call.RuntimeError.cause` 恢复稳定错误码；删除 fetch、HTTP status 和 Cookie 语义。
- `HashRouter` 保留，Wails window 加载 `http://wails.localhost/` 内嵌资源。

退出门禁：生产前端没有 `/api/` 或 fetch；TypeScript 不再手写后端 DTO；七页流程、总览和 Agent 详情行为不变；生产进程无监听端口。

### 阶段 5：测试链路切换

交付：

- Python unit/contract tests 逐项转为 Go table tests、golden tests、fuzz tests 和平台 integration tests。
- Vitest mock `src/backend`，不直接伪造 HTTP。
- Playwright 使用 Wails 官方 `server` build tag + `e2e` fake runner 在 localhost 运行完整 binding 流程；生产 task 明确禁止 `server` tag。
- 每个平台增加打包后原生 smoke：临时 HOME、启动 window、调用一次真实 `GetStatus` binding、验证资源和退出。

退出门禁：旧测试表达的行为都有新测试归属；不是简单删除 Cookie/Origin 测试，而是由“无生产监听端口 + binding allowlist + 外链白名单”替代。

### 阶段 6：构建、发布与官网工具去 Python

交付：

- Wails Taskfile 取代 PyInstaller spec；`go:embed` 取代 resource staging。
- `cmd/oneagent-release` 取代 build/check/notices/lock verification Python 脚本。
- `site/scripts/build-release-index.ts` 和 `build-site-catalog.ts` 取代两个 Python 生成器；Playwright 用 Astro preview，不再用 `python -m http.server`。
- release manifest schema 升级，删除 `python` 字段，增加精确 `go`、`wails`、`frontend` 和 system WebView 要求。
- 第三方 notice 同时覆盖 Go modules、Wails、npm 前端依赖和锁定 Agent 元数据。

退出门禁：官网、manifest、SHA-256、notice、签名检查和渠道一致性测试全部由 Go/Node 完成；产物不含 Python runtime、`.py`、wheel 或 PyInstaller 文件。

### 阶段 7：最终切换与清理

交付：

- 删除 `oneagent/*.py`、`setup.py`、`pyproject.toml`、`packaging/oneagent.spec`、全部 Python scripts/tests 和 wheel 流程。
- 更新 README、CLAUDE.md、ADR、安装文档、AI Agent Kit、官网下载字段和 CI。
- 删除本地 HTTP 端口、Cookie/Origin、PyInstaller 和 Python 3.12 的当前态说明；历史 ADR 保留但标记 Superseded。
- 发布第一版 Wails `technical-preview-unsigned`，执行真实 Agent + Provider RC。

退出门禁：第 11 节全部满足；Python 旧实现不再参与任何 build/test/release/runtime 路径。

## 8. 测试与质量门禁

| 层级 | 门禁 |
| --- | --- |
| Go 单元 | `go test ./...`；整体语句覆盖率 >= 85% |
| 安全关键包 | `securefs/config/install` 100% 语句覆盖 + 明确错误分支矩阵；Go 无原生 branch coverage，不能只用一个百分比替代旧分支门禁 |
| 并发 | `go test -race ./...`；并发 Install/Activate/Status 用例 |
| Fuzz | Agent/profile ID、URL、registry、TOML 合并、JSON/JSONC、version output、path traversal |
| Binding | `wails3 generate bindings -ts` 后无 diff；TypeScript build；暴露方法 allowlist |
| React | Vitest 覆盖 `src/backend` 与 `src/state` >= 85%；Key 不进入 reducer/storage/DOM 回显 |
| Browser E2E | `server,e2e` build tag，fake runner + 临时 HOME，覆盖完整向导、重试、按 Agent 配置和错误 cause |
| 原生 smoke | Windows、macOS arm64/x64、Ubuntu 22.04 `gtk3` 启动打包产物并完成至少一次 binding 调用 |
| Cleanroom | PATH 中无 Python，完成 catalog、status、CLI、桌面启动和非 Aider 安装契约 |
| Release Candidate | 五个锁定 Agent 真实安装；PPIO/Novita 三协议和真实 Agent 首次请求 |
| 静态检查 | `go vet`、`staticcheck`、`govulncheck`、npm audit 策略、secret/source-map/remote-asset scan |

测试迁移使用“冻结 fixture -> Go 等价 -> 删除 Python”的顺序。禁止在同一个真实 HOME 上让 Python 和 Go 双写做对比；所有差分测试只操作独立临时目录。

## 9. 发布与兼容策略

### 9.1 产物

- macOS：Wails `.app`，按现有渠道压缩发布；arm64/x64 分别原生验证，是否追加 universal 包另行决定。
- Windows：Wails `.exe` 桌面应用 + 纯 Go `oneagent.exe` CLI，先维持 ZIP 技术预览；NSIS/MSIX 不混入本次迁移。
- Linux：`gtk3` Wails 桌面二进制 + 纯 Go CLI，明确系统 GTK3/WebKitGTK 4.1 依赖；先维持归档包。
- Source ZIP 可保留；wheel/PyPI 渠道删除。

每个产物仍需 release manifest、SHA-256、Agent 版本清单、第三方 notices 和原生 cleanroom 证据。同一版本跨渠道保持字节一致，不由网盘或官网重新打包。

### 9.2 数据兼容

- 保持 `~/.oneagent`、Agent 配置路径、profile schema v2、secret 文件和 backup 命名可读。
- Go 首次启动只执行已有 v1 -> v2 profile migration，不新增不可逆 schema migration。
- 回滚到最后一个 Python technical preview 时，旧版本仍可读取 Go 写出的状态；这是切换前必须验证的双向兼容门禁。

### 9.3 回滚

- 阶段 0-6 均以独立 PR 合入，生产入口仍指向最后一个已验收实现。
- 阶段 7 切换前打迁移前 release tag，并保存四个原生构建目标的产物与 manifest。
- 切换后若出现阻断问题，回滚发布版本而不是在新包中临时恢复 Python fallback。
- 用户配置格式不变，回滚不应要求手工删除 `~/.oneagent`。

## 10. Python 文件清理映射

| 待删除 | 替代 |
| --- | --- |
| `oneagent/*.py` | `internal/*` + `cmd/oneagent*` |
| `scripts/gui.py` | `wails3 dev` / Wails desktop binary |
| `scripts/build_release.py`、`check_release.py`、`verify_locked_agents.py` | `cmd/oneagent-release` 子命令 |
| `packaging/generate_notices.py` | Go module/npm/Agent notice 生成器 |
| `scripts/build_release_index.py`、`build_site_catalog.py` | `site/scripts/*.ts` |
| `scripts/provider_rc_smoke.py`、`agent_e2e_smoke.py`、`agent_config_adopted_check.py` | Go integration commands/tests |
| `scripts/stage_resources.py` | `go:embed` |
| `scripts/verify_wheel.py`、`setup.py`、`pyproject.toml`、`.spec` | Wails build/package/smoke |
| `tests/*.py` 和 shell 中 inline Python | Go tests/helpers；shell 只保留最薄入口 smoke |
| Docker/CI 的 setup-python、coverage、PyInstaller | Go 1.25、Wails、Node 和平台 WebView build dependencies |

本地被 `.gitignore` 忽略的 `.venv/` 不属于迁移提交，不自动删除用户环境；验收针对受版本控制文件和构建路径。

## 11. 最终验收清单

### 11.1 去 Python

- `git ls-files '*.py'` 为空。
- active workflow、Taskfile、package script、Dockerfile 和当前 README 命令不调用 `python`、pip、wheel 或 PyInstaller。
- 从 PATH 移除 Python 后，Go 测试、前端测试、官网 build、Wails build、CLI 和桌面 smoke 通过。
- 发行包扫描不到 Python runtime、stdlib、`.pyc`、wheel metadata 或 PyInstaller bootloader。
- Aider 的 Python 要求只在选择 Aider 安装时作为外部 prerequisite 出现。

### 11.2 Binding 与安全

- `frontend/src` 不含 `fetch(`/XHR/`/api/` 业务调用。
- 生产二进制不使用 `server` tag，不监听 TCP 端口。
- 生成 binding 是后端 DTO 的唯一 TypeScript 真源，CI 可复现且无 diff。
- 自定义错误在前端保留 `error_code/status/retryable`；CLI 退出码不变。
- 并发写不会丢配置、覆盖备份或混用 Agent Key。
- API Key 不出现在日志、event、panic、DevTools、profile、Agent binding、URL 或测试附件。
- 外链只能由 Go 端 catalog 白名单解析并通过系统浏览器打开。

### 11.3 行为与发布

- 五个 auto Agent 的检测、锁定安装、配置发现、配置写入、重启提示和 per-Agent Provider 行为等价。
- guide-only Agent 不安装、不写配置、不启动服务。
- profile v1/v2、现有用户字段、备份与权限兼容。
- Windows、macOS、Linux 原生构建和 cleanroom 通过；Linux 产物与声明的 GTK/WebKit 最低版本一致。
- 官网 release index、manifest、SHA-256、notice、签名与跨渠道一致性门禁通过。
- Wails 仍为 Alpha 时只允许 technical preview；Stable 仍需 macOS 签名/公证和 Windows Authenticode 的真实产物证据。

## 12. 开工前必须确认的四项决策

建议直接采用下列默认值，避免实现中途重新切边界：

1. **Aider**：允许其安装流程拥有外部 Python 3.12 prerequisite；OneAgent 自身仍为零 Python。若不接受，则 Aider install 降为 guide-only。
2. **Linux**：保持 Ubuntu 22.04+，正式构建固定 `gtk3` tag；未来升级 GTK4 另立 ADR。
3. **CLI**：发布独立纯 Go CLI，与 Wails 桌面应用同包但不同二进制；兼容 wrapper 保留一个周期。
4. **Wails Alpha**：候选锁定 `v3.0.0-alpha2.118`，只进入 technical preview；每次升级 Wails 必须重跑 binding diff、四个原生构建目标的 native smoke 和 release scan。

四项确认后按阶段 0 -> 7 推进。任何阶段只有在自己的退出门禁全绿后才能删除对应 Python 实现；阶段 7 才允许宣告迁移完成。
