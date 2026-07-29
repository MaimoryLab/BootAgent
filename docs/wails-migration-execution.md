# Wails v3 迁移执行计划

战略与取舍见 [重写计划](wails-rewrite-plan.md)。这份文档是它的执行层：目录布局、每阶段的可验证门禁、以及字节等价的具体陷阱。

两份文档的分工——重写计划回答「为什么迁、什么不能丢」，本文回答「先动哪一行、怎么证明没丢」。

## 1. 已确认的前提

| 项 | 状态 |
| --- | --- |
| Go | 1.26.4，已由 mise 全局配置安装 |
| Node | 22.23.1（前端），mise 配置声明 24.16.0 |
| Wails CLI | 未安装。第 0–4 阶段不需要，第 5 阶段前装 |
| Wails 版本 | 仍是 Alpha，只能进 technical preview（见阶段 5） |
| 当前测试基线 | 245 用例，16.9s，1 skip |
| 前端 HTTP 面积 | `fetch(` 仅 `src/api/client.ts:43` 一处 |

前端的 `api` 对象（`client.ts:82`）是天然的适配器边界：页面只调 `api.status()` / `api.install(...)`，不接触路径与 HTTP。**替换实现不需要改页面**，这是迁移最大的省力点。

`POST /api/profiles` 例外：后端有、前端未接。它是既有契约，迁移时不能因为「前端没用」而丢。

## 2. 去 Python 的真实边界：Aider

`agents.lock.json` 里 Aider 是唯一 `manager=uv` 的 Agent——它的上游本身是 Python 工具。这意味着「零 Python」有两种互斥的读法，必须先选一个：

- **A（建议）**：OneAgent 自身零 Python；Aider 的 Python 3.12 降级为**该 Agent 的外部前置条件**，与 npm 需要 Node 同性质。安装 Aider 时检查并提示，OneAgent 的构建、测试、运行、发布均不需要 Python。
- **B**：目标机器绝对不能有 Python，则 Aider 必须从 `config_mode=auto` 降为 `guide`。

两者不能同时成立。选 B 要改 lock 并调整五个自动 Agent 的验收基线，代价明显更大。**默认取 A。**

其余 Python 资产按用途分三类，处置方式不同：

| 类别 | 文件 | 去向 |
| --- | --- | --- |
| 内核 | `oneagent/*.py` | Go 重写（阶段 1–4） |
| 发布工具 | `build_release.py`、`check_release.py`、`generate_notices.py`、`verify_locked_agents.py`、`verify_wheel.py`、`stage_resources.py` | Go release 子命令（阶段 6）；`stage_resources` 由 `go:embed` 取代 |
| 站点数据 | `build_release_index.py`、`build_site_catalog.py` | TypeScript，落 `site/scripts/`（阶段 6） |

`site/playwright.config.ts:12` 用 `python -m http.server` 托静态产物，改用 Astro preview。`site/package.json:7` 的 `prepare:data` 是上表第三类的调用点。

## 3. 目录布局

新增顶层 `desktop/` 作为 Go 模块，与 Python 树隔离——迁移期两者并存，Python 保持可发布。

```
desktop/
  go.mod
  internal/
    oerr/          阶段 0  错误码与响应形，含跨语言 parity 门禁
    runtime/       阶段 0  可注入 Runtime
    testutil/      阶段 0  测试替身
    catalog/       ✓ 阶段 1  lock 解析与排序、Provider 与镜像投影
    provider/      ✓ 阶段 1  URL 校验推导、协议映射（探测在阶段 2）
    securefs/      阶段 2  原子写、备份、权限
    config/        阶段 2–3  五写五读适配器
    install/       阶段 4  编排、integrity、前置条件
    app/           阶段 4  use case，桌面与 CLI 共用
  cmd/
    oneagent/          纯 Go CLI，不链接 Wails/GTK
    oneagent-desktop/   阶段 5  Wails 外壳
    oneagent-release/   阶段 6
```

`cmd/oneagent` 直接调 `internal/app`，不反向依赖 Wails service。CLI 不链接 GUI 运行时——否则「去 Python」之后又引入一个不必要的运行依赖，headless 与自动化环境会退化。

## 4. 阶段 0：地基

无业务逻辑。目标是让阶段 1 起的测试**能够被移植**。

### 4.1 `internal/oerr`

照搬 `oneagent/errors.py`（已逐字核对）：11 个码，退出码 2/2/3/4/5/6/6/6/7/8/10。三个码共享 6，1 与 9 未使用。

两处容易写错：

- 未知码回落到 **10**，不是查 `EXIT_CODES["INTERNAL_ERROR"]`。语义相同但来源不同，Go 侧照抄字面量。
- `to_dict()` 输出恰好六键：`ok`、`error`、`message`、`status`、`error_code`、`retryable`。`message` 出现两次（`error` 与 `message` 同值），`exit_code` **不在其中**。

**门禁**：`codes_parity_test.go` 解析 `../oneagent/errors.py` 的 `EXIT_CODES` 字面量，逐键与 Go 对比；并断言回落字面量与序列化键集合一致。码值是外部契约，真源锁在 Python 文件——Go 偏离即红。这个测试在 Python 删除时才随之改为 Go 内部常量断言，契约比 Python 文件活得久。

### 4.2 `internal/runtime`

```go
type Runtime struct {
    Home  string
    OSID  string
    Run   Runner
    Which WhichFn
    Env   map[string]string
}
```

`Home` 解析优先级与 `catalog.py:214` 一致：`ONEAGENT_HOME` →（windows：`USERPROFILE` → `HOMEDRIVE`+`HOMEPATH`）→ `HOME` → 系统用户目录。最后一跳要**两次**尝试（`os.UserHomeDir` 后再 `user.Current`）才与 Python 等价，理由见 4.5。平台判定：`darwin`→`macos`、`windows`→`windows`、其余 `linux`；arch 取 `arm64`/`x64`；shell 取 `powershell`/`bash`。

**门禁**：`home_parity_test.go` 调用真实的 Python `resolve_home`（而非重新实现它——重新实现只能证明测试与自己一致）并逐例比较，覆盖优先级全表与「什么都没声明」的场景。

`Run` 的错误分类是移植重点，但**分类与错误码要分开**。Python 侧两处超时映射到不同的码：安装 Agent 超时是 `TIMEOUT`（`installer.py:723`），读取 integrity 超时是 `AGENT_INSTALL_FAILED`（`installer.py:641`），两者都 retryable。这不是不一致，是调用点各自的判断。

所以 `runtime` 只负责回答「是超时、是起进程失败、还是正常退出（含非零）」，导出 `IsTimeout`/`IsStartFailure` 由调用点决定码值。在这一层固化码值会把那个区分抹掉。

非零退出是 `Result` 而不是 `error`：调用点通常需要捕获的输出来生成脱敏摘要，返回 error 会把它丢掉。

### 4.3 `internal/testutil`

`RecordingRunner` 记录每次的 argv 与 env，并离线应答 `npm view <spec> dist.integrity`——真值从 `agents.lock.json` 读。这一条决定 integrity 校验路径能否离线测试；Python 侧曾因测试替身不认识这条新命令而一次打红 11 个用例，Go 侧从第一天就要认。

`FakeWhich` 用 map 模拟 `npm`/`uv`/`icacls` 的存在与缺失。

### 4.4 工程配置

`.gitignore` 忽略 Go 产物；mise 配置锁 Go 版本；`ci.yml` 现有四平台矩阵（ubuntu-22.04 / windows-2022 / macos-15 / macos-15-intel）并入 `go vet ./...` 与 `go test ./...`。Python 作业不动。

### 4.5 已完成状态

阶段 0 已落地，实测结果：

| 包 | 语句覆盖 | 说明 |
| --- | --- | --- |
| `oerr` | 100% | 3 个跨语言门禁：码表、回落字面量、响应键集 |
| `runtime` | 100% | 2 个跨语言门禁：home 优先级全表 + 无 HOME 场景 |
| `testutil` | 92% | 替身自身的保证也被测试 |
| `parity` | — | 只含门禁，无实现语句 |

`go test -race ./...` 干净；Python 245 用例不受影响（要用 CI 的 9 个模块集合；`CLAUDE.md` 里列的 6 个只跑出 171）。

实施与审查中发现三处需要修正，已并入本文与 [ADR-008](decisions/ADR-008-go-core-and-wails-desktop-shell.md)：

- `Run` 的错误分类不能固化码值（见 4.2）。
- 平台判定直读 `goruntime.GOOS` 会让覆盖率依赖 CI 矩阵而非测试，抽成取参数的纯函数后同一进程可覆盖三个映射。
- **home 解析的兜底行为原本悄悄改了外部契约**。Python 的 `Path.home()` 在 `HOME` 缺失时回落到 passwd，Go 的 `os.UserHomeDir` 只读 `$HOME` 并报错。最初的 Go 实现在这里返回 `""`，意图是让上层报 `PREREQUISITE_MISSING`（exit 3）——但 Python 在同样输入下能解析出 home，上层不会走到那条路径。这不是「兜底返回什么」的选择，是两侧解析能力不同，而差异会表现为不同的退出码。Go 侧补了 `user.Current()` 一跳恢复等价，并由 `home_parity_test.go` 的 7 个用例锁死。真正「无处可写」的分支仍返回 `""`，通过可注入的查询函数测试到。

### 4.6 parity 门禁本身的三个静默失效路径

`go test -run` **匹配零个测试时退出码为 0**。这让「跑 parity 测试」这个 CI 步骤有三种变绿而不报错的方式，全部已堵住并实测验证：

| 失效方式 | 旧行为 | 现在 |
| --- | --- | --- |
| 改名（不再匹配 `-run` 模式） | 静默 skip | `internal/parity` 计数断言失败并点名 |
| 删掉整个 parity 文件 | `ok ... [no tests to run]` | `internal/parity` 断言文件存在 |
| Python 不在 PATH（跨语言比较无法进行） | `t.Skip`，报 ok | CI 设 `ONEAGENT_REQUIRE_PARITY`，改为硬失败 |
| 新增 parity 文件但没进门禁清单 | 看着被覆盖，实际不在计数内 | `TestNoParityFileEscapesTheDeclaredList` 反向检查 |

`internal/parity/gate_test.go` 的 `expected` 表逐文件声明最低测试数（目前 `oerr` 3、`runtime` 2、`catalog` 5、`provider` 9）。新增 parity 文件必须加一行——这是有意的，替代方案是一个悄悄停止覆盖它的门禁。

第一版把计数自检写在 `codes_parity_test.go` 内部——能拦改名，但**删掉该文件后自检本身也不在了**。所以计数移到独立的 `internal/parity` 包，从外部断言每个 parity 文件仍存在、仍带着它的测试，并反向检查没有未声明的 parity 文件游离在门禁之外。

**当前局限**：`ci.yml` 是 `workflow_dispatch` 专属（自动触发已按仓库所有者要求关闭），所以这些门禁只在手动触发时生效，推送时不看着任何人。阶段 1 落地前需记住这一点。

**阶段 0 验收**：`go vet` 干净、`go test -race ./...` 全过（含 parity 门禁）、四平台 CI 绿、Python 245 用例与 `installer.py` 100% 分支门禁不受影响。

## 5. 字节等价的五个陷阱

阶段 2 的跨实现字节比对是「等价」唯一的证明手段。以下五处 Go 的**默认行为**会静默产生差异，全部实测确认：

1. **HTML 转义**：Go `encoding/json` 默认 `SetEscapeHTML(true)`，把 `&` 写成 `&`。Python 不转义——`{"baseUrl": "https://x/?a=1&b=2"}` 两侧不同。必须 `SetEscapeHTML(false)`。
2. **键序**：Python dict 保插入序，Go `map[string]any` 序列化按字母序。合并用户既有配置时字段会被重排，字节比对立刻红。需要有序结构。
3. **`ensure_ascii` 两种语义**：codex 适配器用 `json.dumps` 默认（`ensure_ascii=True`），把非 ASCII 模型名写成 `通义`——因为它是在生成 **TOML 字符串字面量**；claude/opencode/profile 用 `ensure_ascii=False, indent=2`，原样 UTF-8。同一个 Go helper 不能两处共用。
4. **缩进与结尾**：`indent=2` 隐含分隔符 `(',', ': ')`，且四处调用都显式 `+ "\n"`。Go 的 `MarshalIndent` 不加结尾换行。
5. **Windows 替换**：Go `os.Rename` 对已存在目标会失败，与 Python `os.replace` 不同。需 `windows.MoveFileEx(..., MOVEFILE_REPLACE_EXISTING)`。

## 6. 阶段 1–6

每阶段以「该阶段移植的测试全过」为门禁，不允许先全写完再补测试。战略见[重写计划 §4](wails-rewrite-plan.md)，此处只记它没写到的执行细节。

### 阶段 1：纯逻辑（已完成）

`catalog` 与 `provider` 的无 IO 部分：lock 解析与排序、Provider 与镜像的公开投影、URL 校验推导、协议映射与分类、模型挑选。触网的 `chat_probe`/`list_models`/`protocol_probe` 留到阶段 2。

| 包 | 覆盖 | 跨语言门禁 |
| --- | --- | --- |
| `catalog` | 96.4% | 5 个：embed 字节一致、Provider 投影、镜像、目录顺序、14 个 Agent 逐字段 |
| `provider` | 96.9% | 9 个：两种 URL 推导、校验拒绝边界、模型分类逐 ID、ConfigBase 路由、fallback、协议分类与文案 |

整体 94.0%，`-race` 干净。

**`go:embed` 迫使 manifest 有一份副本。** 它既不能跨出包目录，也拒绝符号链接。副本命名为 `agents.lock.embed.json` 并由 `go generate ./internal/catalog/` 生成；`TestParityEmbeddedManifestIsByteIdenticalToTheRealOne` 对任何差异失败，CI 另跑一次 `go generate` 后做 `git diff --exit-code`，所以改了 manifest 却忘了同步副本会红而不是静默带着旧数据构建。仓库根目录仍是唯一可编辑的真源。

**跨语言对比抓到一处我不会手写出来的分歧。** `openai_base_url` 的后缀剥离是「每个后缀检查一次」，改写成看起来更整洁的「循环直到稳定」后，`https://example.com/v1/models/models` 两侧结果不同（Go 得 `/v1`，Python 得 `/v1/models/v1`）。没有任何手写用例会覆盖这种输入——这正是字节/逐输入对比相对「移植测试」的额外价值。

Go 的 RE2 原样接受 Python 那条 non-chat 模型正则，29 个模型 ID 逐个分类结果一致，无需改写。

### 阶段 2：写入链路

先 `atomic_write` 的全部失败分支，再五个适配器。`atomic_write` 的七步次序、密钥备份加固失败的处置、临时文件清理失败本身也报错，见[重写计划 §3.1](wails-rewrite-plan.md)。

Windows 权限走 `icacls` 两步（`/reset` 后 `/inheritance:r /grant:r`，授权当前用户与 SYSTEM），经可注入 runner，找不到 `icacls` 是硬 `CONFIG_WRITE_FAILED`。DACL 直设版作后续优化，两者过同一组测试。

`_merge_codex_toml` 的行级合并要保留用户键，两处 `Unsupported ... syntax` 拒绝不能放宽，合并后回读校验保留。`_load_json` 对 JSONC 注释 fail closed。

**门禁**：写入用例全移植 + 跨实现字节比对（第 5 节五条）。**止损点在此**——比对过不去说明等价成本被低估，此时前端未动、Python 仍可发布。

### 阶段 3：读取链路

五个 `read_*_config` 与 `detect_agent_config`（21 用例）。返回形恰好四键 `{baseUrl, model, managedByOneAgent, unreadable}`，刻意不含密钥**及其存在性**——连 `hasKey` 布尔都会泄漏「这台机器配了 Key」。

三条不能丢：坏配置不得崩进程（Go 用 `recover` 对应 Python 的兜底 `except Exception`，失败本地化为 `unreadable`）；`unreadable` 的中文文案是用户可见契约，逐字复刻；Aider 读取绝不执行脚本，且 `managedByOneAgent` 恒 `false`——手写脚本与 OneAgent 产物字节同形，声称能区分是猜测。

### 阶段 4：安装链路

42 用例移植；`real_install_test.sh` 改驱动 Go 二进制后官方源与镜像双路径仍通过。

### 阶段 5：Wails 外壳与托盘

已核对官方文档（2026-07-29）的四条，直接影响本阶段的决策：

- **仍是 Alpha**（[status](https://v3.wails.io/status/) 明确「Current Status: Alpha」，下一目标 Beta）。因此 OneAgent 只能发 `technical-preview-unsigned`，不因应用功能完成而自动提升 Stable。每次升级 Wails 都要重跑 binding diff、四平台 native smoke 与产物扫描。
- **需要 Go 1.25+**（安装文档）。本机 1.26.4 满足。
- **托盘 API 是 `app.SystemTray.New()`**——application 上的一个 manager，不是 `application.SystemTray` 或 `app.NewSystemTray()`。方法含 `SetIcon`、`SetLabel`（macOS 是 label、Windows 是 tooltip）、`SetDarkModeIcon`、`SetTemplateIcon`、`SetMenu`。三平台都支持，但图标规格各不同：Windows 16×16/32×32 PNG 或 ICO，macOS 18×18–22×22 PNG（推荐 template），Linux 22×22–48×48 且「随桌面环境而异」。
- **关窗不退出在 macOS 上要显式声明**：`application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false}`。存在 autostart 功能页，具体 API 名未确认。

**Linux 是需要单独决策的一处。** GTK4 + WebKitGTK 6.0 已在 alpha.93 提升为**默认**栈，只提供 WebKit2GTK 4.1 的发行版（含 Ubuntu 22.04）要走 legacy GTK3 路径。ADR-003 承诺 Ubuntu 22.04+，所以迁移期 Linux 正式产物应固定 GTK3 build tag 并在 22.04 cleanroom 验证；改用 GTK4/要求 24.04+ 是**产品承诺变更，必须另立 ADR**，不能在迁移里顺手做掉。

`client.ts` 的 `api` 对象换成 IPC 绑定实现，页面不改。

Wails binding 默认并发，而现有 `HTTPServer` 是串行的——这是形态切换带来的**新**并发面。`Install`/`Activate`/`SaveProfile` 需共享写锁，避免两个调用同时备份或覆盖同一配置；读操作依赖原子替换保证不见半写状态。

生成的 binding 是后端 DTO 的唯一 TypeScript 真源，`frontend/src/types/api.ts` 中的手写后端类型最终删除或改为薄别名，避免两份真源。CI 重生 binding 后做 diff 检查。

外链只接 Provider ID，由 Go catalog 解析白名单 URL 再交系统浏览器；不允许前端传任意 URL。

常驻带来的四个新问题见[重写计划 §6](wails-rewrite-plan.md)，不解决不上线。

### 阶段 6：分发

签名与公证换栈不会变简单，Developer ID 与 Authenticode 仍是前提。release manifest 删 `python` 字段，增加 Go/Wails/WebView 要求。第三方 notice 覆盖 Go modules、Wails、npm 前端依赖与锁定 Agent 元数据。

## 7. 贯穿全程的不变量

- **lock 唯一真源**：`command`/`config_path`/`credential_delivery`/`windows_prerequisites` 全从 lock 读，禁按 agent id 硬编码。Python 侧已有 `LockIsTheSourceOfTruthTests` 守住，Go 侧移植等价断言。
- **错误码契约**：11 个码与响应六键不变（阶段 0 parity 门禁锁死）；CLI 退出码是外部契约。
- **密钥不落地**：不进 profile、argv、URL、日志、前端状态、浏览器存储；Go 侧等价 `redact` 且被测试覆盖。常驻新增一条：内存密钥显式清零，不为「下次免输」缓存。
- **传输契约**：`StatusResponse` 等派生字段 camelCase、请求体 snake_case。不因 Go 惯例改动，也不借迁移做命名清理。

## 8. 决策记录

[ADR-008](decisions/ADR-008-go-core-and-wails-desktop-shell.md) 已写，只替换 ADR-003 中「Python 核心、本地 HTTP、PyInstaller」三项决策；ADR-003 已就地标注被取代的部分与那处事实错误。

顺带修掉一个存量问题：`docs/decisions/` 曾有两个 ADR-006（多 Profile 与公开站）。两者都无外部引用，所以后加的公开站那份重编号为 ADR-007，迁移 ADR 取 008。

## 9. 不做

- 不在迁移期新增功能。托盘之外一切 1:1 迁移。
- 不重设计 lock 格式、错误码或七页向导。
- 不在阶段 0 引入 Wails、IPC 或托盘。
- 不删 Python，直到 Go 通过全部移植测试并完成一轮真实 cleanroom。
- 不让 Python 与 Go 在同一个真实 HOME 上双写做对比；差分测试只操作独立临时目录。
