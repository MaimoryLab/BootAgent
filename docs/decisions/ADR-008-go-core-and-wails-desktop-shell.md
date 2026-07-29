# ADR-008：Go 内核与 Wails 桌面壳

## Status

Proposed

## Date

2026-07-29

## Scope

只替换 [ADR-003](ADR-003-three-platform-python-core-and-release-policy.md) 中的三项决策：Python 共用内核、本地 HTTP GUI、PyInstaller 打包。ADR-003 的其余部分——三平台适配设计、版本锁定、配置适配、结构化错误、Windows 用原生用户目录——继续有效。

[ADR-002](ADR-002-product-boundary-and-network-access.md) 的产品边界（禁代理、禁共享 Key、禁常驻网关）不因换栈而放宽。[ADR-005](ADR-005-channel-neutral-distribution-and-compliance.md) 的渠道中立与版本锁定同样继续有效，且对自动更新构成约束（见下）。

## Context

托盘常驻已确定为产品要求。ADR-003 拒绝桌面壳的理由是「当前本地浏览器 GUI 已能满足流程需求」——这个前提对托盘不成立：托盘需要原生窗口系统 API，进程生命周期不能绑在浏览器标签页上，开机自启需要一个能被 LaunchAgent 或注册表拉起的可执行体。前提不再成立，结论要重算。

同时修正 ADR-003 的一处事实错误。它称桌面壳「引入额外运行时」，这对 Electron 成立（打包 Chromium），对 Wails 不成立：Wails 使用系统 WebView（macOS WKWebView、Windows WebView2、Linux WebKitGTK），产物是单个 Go 二进制。当前 PyInstaller 产物反而**确实**带运行时——9.2MB zip 内含 `libpython3.12.dylib`，且用户需自行解压。这个说法留着会误导下次评估。

团队已有 Wails 背景，所以选型不是待决问题；待决的是如何迁移而不丢掉已验证过的行为。

## Decision

### 1. Go 领域内核 + Wails 桌面壳 + 独立 CLI

`oneagent/*.py` 重写为 Go（`desktop/internal/*`），桌面壳用 Wails v3，并**另发一个不链接 Wails/GTK 的纯 Go CLI**。让 headless CLI 依赖 GUI 运行时，会在「去 Python」之后又引入一个不必要的运行依赖，自动化与容器环境会退化。

`agents.lock.json` 继续是 Agent 行为与版本的唯一真源，经 `go:embed` 进入两个产物。禁止按 agent id 硬编码行为。

### 2. 舍弃本地 HTTP 是安全约束的性质变化，不是放弃

这一条单独记录，因为下次审查会正当地怀疑约束被悄悄删掉了。

当前形态的三项约束——只绑 `127.0.0.1`、Origin 白名单、`compare_digest` 会话 Cookie——存在的目的是**防止本机其他进程访问 API**。Wails 用 IPC 而非 HTTP，生产进程不监听任何 TCP 端口，因此这三项约束既随之消失，也不再有对应威胁。

替代它们的是三条新约束，必须被测试守住：

- **生产二进制不监听端口**。`server` build tag 只允许出现在 e2e/CI task，不得进入发布构建；产物扫描要能发现它。
- **binding allowlist**。只注册明确列出的业务 service，不配置 HTTP Route，不启用 Raw Message Handler。前端类型不可信，所有 binding 输入仍在 Go 端完整校验。
- **外链白名单**。`OpenRegistration` 只接收 Provider ID，由 Go catalog 解析并校验 `http/https` 后交系统浏览器；不允许前端传任意 URL。

`INVALID_ORIGIN` 保留为已废弃的保留码值，不复用——退出码是外部契约。

### 3. 错误码与 home 解析用跨语言测试锁定，而非人工核对

迁移期两个实现同时声称遵守同一批外部契约。凡此类契约，Go 侧测试**读 Python 源码并比较**，而不是复制常量后指望它保持同步：

- `EXIT_CODES` 全表、未知码回落的字面量、`to_dict()` 的键集
- `resolve_home` 的完整优先级顺序

这已经抓到一处真实分歧：Python 的 `Path.home()` 在 `HOME` 缺失时回落到 passwd 数据库，而 Go 的 `os.UserHomeDir` 只读 `$HOME` 并报错。若照直移植，Go 会在 Python 能解析出 home 的场合解析不出，而这**不会表现为明显失败**——它会表现为上层某处给出不同的退出码，也就是外部契约的静默改变。Go 侧因此补了 `user.Current()` 一跳，并由 `home_parity_test.go` 锁死。

配套要求：CI 的 parity 步骤不能靠 `-run` 模式过滤，因为 **`go test -run` 匹配零个测试时退出码为 0**。删掉一个 parity 文件会让该步骤变绿。因此由 `internal/parity` 从外部断言每个 parity 文件仍存在、仍带着它的测试；Python 缺失时 `ONEAGENT_REQUIRE_PARITY` 使跨语言比较硬失败而非 skip。

### 4. 等价性用字节比对证明，止损点在阶段 2

245 个用例移植完不等于行为一致：测试覆盖已知场景，而配置合并、权限加固、路径处理有大量未被显式测试的边角。因此阶段 2 引入**跨实现字节比对**——同输入下 Python 与 Go 产出的配置文件必须逐字节相同，迁移期一直保留。

已实测确认五处 Go 默认行为会静默产生差异：`encoding/json` 默认转义 `&`；`map` 序列化按字母序（会重排用户既有字段）；codex 适配器需要 `ensure_ascii=True` 语义而其余三个需要 `False`；`MarshalIndent` 不加结尾换行；`os.Rename` 在 Windows 上对已存在目标会失败，需 `MoveFileEx`。

**止损点**：阶段 2 结束评估。若 `atomic_write` 与五个适配器无法通过字节比对，说明等价成本被低估，此时前端未动、Python 版仍可发布，应回头考虑「Wails 壳 + Python sidecar」折中，而不是继续投入。

不允许 Python 与 Go 长期双版本并行：两套实现会分叉，而「lock 是唯一真源」只在一处实现时守得住。

### 5. Linux 保持 Ubuntu 22.04+，正式产物固定 GTK3

Wails v3 已在 alpha.93 把 GTK4 + WebKitGTK 6.0 提升为**默认**栈，而 Ubuntu 22.04 只提供 WebKit2GTK 4.1，需走 legacy GTK3 路径。ADR-003 承诺 Ubuntu 22.04+，因此迁移期 Linux 正式产物固定 GTK3 build tag，并在 22.04 cleanroom 验证。

改用 GTK4（把底线提到 Ubuntu 24.04+）是**产品承诺变更，必须另立 ADR**，不能在迁移里顺手做掉。

### 6. Wails 仍是 Alpha，只发 technical preview

Wails v3 官方状态页明确当前为 Alpha。因此 OneAgent 只发布 `technical-preview-unsigned`，不因应用功能完成而自动提升 Stable——Stable 仍需 macOS Developer ID 签名/公证与 Windows Authenticode 的真实产物证据，换栈不会让这件事变简单。

每次升级 Wails 必须重跑 binding diff、四个原生构建目标的 native smoke 与发布产物扫描。

### 7. 常驻带来的新约束

托盘常驻在当前形态下不存在，因此以下是**新增**约束，不解决不上线：

- **内存中的密钥必须显式清零**，且不得为「下次不用重输」而缓存。当前进程随浏览器标签页结束，密钥在内存里活不过一次操作；常驻后这个天然保护消失。
- **托盘文案必须主动声明「OneAgent 不转发任何请求」**。产品边界禁止代理与常驻网关，而托盘图标容易造成相反印象。
- **开机自启默认关闭**，由用户显式启用。一个配置工具默认自启会被合理地质疑在后台做什么。
- **自动更新若要做，必须是「提示 + 用户确认 + 验签」**。ADR-005 的渠道中立与版本锁定要求意味着不能静默替换二进制。

### 8. Aider 的 Python 是外部前置条件

`agents.lock.json` 中 Aider 是唯一 `manager=uv` 的 Agent——它的上游本身是 Python 工具。因此「零 Python」有两种互斥读法，此处选定前者：

- **采纳**：OneAgent 自身零 Python（源码、测试、构建、运行、发布均不需要）；Aider 的 Python 3.12 是**该 Agent 的外部前置条件**，与 npm 需要 Node 同性质，仅在用户选择安装 Aider 时出现。
- **未采纳**：若要求目标机器绝对不存在 Python，则 Aider 必须从 `config_mode=auto` 降为 `guide`。

两者不能同时成立。改选后者需修改 lock 并调整五个自动 Agent 的验收基线。

## Consequences

产物从 9.2MB zip（含 Python 运行时、需用户解压）变为单个 Go 二进制装在 `.app` / 安装器里，并获得托盘常驻与开机自启。

代价是一次全量重写：约 2900 行 Python 内核、4400 行测试（245 用例）需逐条移植。前端 3134 行完整保留——`frontend/src/api/client.ts` 的 `api` 对象是天然适配器边界，页面只调 `api.status()` 一类方法而不接触 HTTP，替换实现不需要改页面。

Wails binding 默认并发，而当前 `HTTPServer` 是串行的。这是形态切换带来的**新**并发面：`Install`/`Activate`/`SaveProfile` 需共享写锁，避免两个调用同时备份或覆盖同一配置。Go 测试因此从一开始就跑 `-race`。

这一条在编排层落地前就已经付过一次代价：`catalog.Load()` 曾用 nil 检查缓存解析结果，`-race` 在 8 个并发读者下报 `WARNING: DATA RACE`。它在当时不炸，只因为 Go 还没接线——**而一旦接进外壳，外壳就会成为表面原因**。已改用 `sync.OnceValues`，并有一个并发读取测试锁住它：改回 nil 检查会让该测试打红。凡是「现在不炸只因为还没接线」的并发问题，都按此处理，不留到接线那一刻。

生成的 binding 成为后端 DTO 的唯一 TypeScript 真源，`frontend/src/types/api.ts` 中手写的后端类型最终删除或改为薄别名，避免两份真源。

Python 实现不删除，直到 Go 版通过全部移植测试并完成一轮真实 cleanroom。

## Alternatives Considered

**Tauri**：与 Wails 同样使用系统 WebView、产物单二进制，技术上可行。未选择的唯一原因是团队已有 Wails 背景而无 Rust 背景；迁移风险集中在「证明等价」而非「写得出来」，因此熟悉度的权重高于语言偏好。

**Electron**：会真正引入额外运行时（打包 Chromium），产物体积远大于当前 zip，与 local-first 的轻量定位相悖。

**保留 Python 内核，仅加桌面壳（sidecar）**：能最快拿到托盘，但要维持一个 Python 运行时加一个壳进程，安装体积与失败模式都比现状更复杂。作为阶段 2 止损后的**退路**保留，不作为首选。

**只做单文件打包，不做托盘**：能解决「用户需自行解压」，但托盘常驻是已确定的产品要求，不解决它等于不解决问题。

## 相关文档

[迁移战略](../wails-rewrite-plan.md)、[迁移执行计划](../wails-migration-execution.md)、[产品边界基线](../product-boundary-baseline.md)。
