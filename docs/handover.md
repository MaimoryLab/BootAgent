# 交接文档

截至 `1b66ae7`，已推送 `origin/main`。这份文档回答三件事：**做到了哪一步、还有哪些问题、下一步做什么**。

过程记录见 [迁移进展](migration-progress.md)，执行细节见 [执行计划](wails-migration-execution.md)，战略见 [重写计划](wails-rewrite-plan.md)，决策见 [ADR-008](decisions/ADR-008-go-core-and-wails-desktop-shell.md)。

## 1. 一句话现状

**Python 仍是唯一的生产实现。** Go 内核已完成阶段 0–4（全部内核逻辑 + 编排层 + 一个可用的 CLI），但**尚未接线到任何用户入口**——接线是阶段 5 的内容。两个实现在同一批输入上逐字节等价，由 61 个跨语言门禁锁定。

这意味着：现在删掉 `desktop/` 整个目录，产品功能不受任何影响。这是有意的——止损点设在阶段 2，等价性证明成立后才继续。

## 2. 做了什么

### 2.1 阶段划分与状态

| 阶段 | 内容 | 状态 |
| --- | --- | --- |
| 0 | 错误码契约、可注入 Runtime、测试替身、parity 门禁 | 完成 |
| 1 | `catalog`、`provider` 纯逻辑 | 完成 |
| 2 | `securefs`、`jsonorder`、`shellquote`、五个写入适配器 | 完成（**止损点通过**） |
| 3 | 五个读取器与配置发现 | 完成 |
| 4 | 安装编排、integrity、镜像、前置条件、profile 存储、`install_many`、`activate_agent`、`status_payload`、共享写锁、CLI | 完成 |
| 5 | Wails 外壳与托盘 | **未开始** |
| 6 | 分发 | 未开始（按要求暂缓） |

### 2.2 代码规模

```
Python 内核  3198 行（生产实现，未改动语义，只修了发现的缺陷）
Go 实现      6887 行 + 测试 12021 行
```

Go 侧 16 个包，`go test -race` 干净：

| 包 | 覆盖 | 职责 |
| --- | --- | --- |
| `oerr` | 100% | 错误码与响应形，跨语言门禁锁定 |
| `runtime` | 100% | 全部副作用的注入接缝 |
| `provider` | 95.5% | URL 校验推导、协议映射、探测与模型发现 |
| `catalog` | 92.6% | 内嵌 manifest、声明序与 rank 序、Provider 与镜像投影 |
| `testutil` | 92.1% | 测试替身（自身的保证也被测试） |
| `config` | 91.2% | 五写五读适配器、TOML 合并、env 文件 |
| `install` | 87.9% | 锁定安装、integrity、镜像、前置条件 |
| `shellquote` | 87.5% | `shlex.quote`/`split` 与 PowerShell 引用 |
| `securefs` | 86.5% | 七步原子写、备份、权限、Windows ACL |
| `app` | 86.2% | 编排：install/activate/status，共享写锁 |
| `cmd/oneagent` | 83.7% | 纯 Go CLI，不链接 Wails/GTK |
| `jsonorder` | 83.3% | 保序 JSON，复刻 `json.dumps` |
| `profile` | 82.9% | profile 存储、密钥文件、Agent binding |

### 2.3 计数（机械测得，不要手工累加）

```
Go       399 个顶层用例 / 769 顶层加子测试合计 / 整体覆盖 88.0%
跨语言   16 个文件、61 个顶层用例、247 个逐输入子测试（合计 308）
Python   249 用例，installer.py 100% 分支 0 partial，整体 97%
前端     覆盖 97.84%，20 个 e2e
```

**报数前跑[执行计划 §4.6](wails-migration-execution.md) 那条命令。** 曾经手工报的「807 用例」是错的；后来我又因为混用两种口径把一个对的数字改错了，所以**口径必须跟数字写在一起**——脚本的 `with subtests` 含顶层，「247 个子测试」是纯子测试。

### 2.4 方法论：字节比对找到了手写测试不会发现的分歧

这是整个迁移最有价值的部分。移植 245 个测试只能证明「有人想到要写下来的场景」一致。把两个实现跑在同一批输入上逐字节比对，找到 5 处分歧：

1. **数字格式**：Python 的 `json.loads` 把带指数的数字提升为 float，`1e10` 往返后变成 `10000000000.0`。这些是用户设的 timeout 与 token 上限，改写它们正是「保留非管理字段」要防的静默编辑。
2. **codex 与 JSON 适配器的转义语义相反**：前者 `ensure_ascii=True`（因为在生成 TOML 字符串字面量），后三者 `False`。共用一个 helper 会通过**所有**手写测试。
3. **`openai_base_url` 的后缀剥离**：改成「循环直到稳定」后 `/v1/models/models` 两侧结果不同。没有任何手写用例会覆盖这种输入。
4. **home 解析的兜底能力不同**：Python 的 `Path.home()` 在 `HOME` 缺失时回落 passwd，Go 的 `os.UserHomeDir` 报错。差异表现为**不同的退出码**，即外部契约的静默改变。
5. **`catalog.Agent` 漏读两个 manifest 键**：字节一致的 embed 对此毫无保证——**字节相同不代表读取相同**。

### 2.5 缺陷都表现为「成功」

跨语言比对再密也抓不到「承诺与实现不符」，因为两个实现在这些点上是一致的。把产品真跑一遍（Chromium 驱动 + CLI 实跑）另外找到：

- **API Key 进入 React state 与页面 markup**。`SecureKeyField` 把 Key 镜像进 `useState` 使输入变成受控，React 把 value 写成 DOM 属性，`outerHTML` 因此含明文。同时导致 `clearApiKey()` 清不掉字段。已改为非受控 + 挂载时命令式恢复。
- **文案承诺了一个不存在的包管理器**。Agent 选择页写「npm 或 pip」，而 manifest 只允许 npm 与 uv。pip 只存在于那一句里。
- **无效 registry 被静默接受**。`resolve_registry` 只在 `install_locked_agent` 内调用，而后者对已安装 Agent 提前返回，所以 `http://` registry 返回 200 且设置被丢弃。
- **三个颜色 token 低于 WCAG AA**。只有对真正被绘制的颜色算出比值才会显现。
- **TOML 解析错误把密钥材料带进 status 响应**。`BurntSushi/toml` 回显出问题的 token，而这条消息进 React state 并显示在界面上。
- **失败摘要截断按字节而非码位**，中文错误从字符中间切断。注释声称两侧一致，而语料 11 个用例没有一个超过 600。
- **CLI 退出码只手工验证过一次，没有门禁**。已补 `cmd/oneagent/exitcode_parity_test.go`。

### 2.6 门禁本身的五个静默失效路径

`go test -run` **匹配零个测试时退出码为 0**。全部已堵住并实测验证：

| 失效方式 | 现在 |
| --- | --- |
| 改名（不再匹配 `-run`） | `internal/parity` 计数断言点名失败 |
| 删掉整个 parity 文件 | `internal/parity` 断言文件存在 |
| Python 不在 PATH | CI 设 `ONEAGENT_REQUIRE_PARITY`，硬失败 |
| 新增 parity 文件未进清单 | 反向检查遍历整个 module |
| **CI 步骤按包名枚举** | 改成 `./... -run TestParity`，不再枚举 |

最后一条是最近发现的：那个名叫 "cross-language parity" 的步骤列了 4 个包而 12 个包带 parity 测试，**37 / 61 个用例从未被它跑到**。它们只在同一 job 里更早的 `go test ./...` 中跑过，所以没露出来。**枚举就是漏的原因。**

## 3. 还有哪些问题

### 3.1 阻塞阶段 5 的决策（需要人来定，不能顺手做掉）

**Wails 仍是 Alpha。** 官方 [status 页](https://v3.wails.io/status/) 明确「Current Status: Alpha」，下一目标 Beta。所以 OneAgent 只能发 `technical-preview-unsigned`，**不因应用功能完成而自动提升 Stable**。每次升级 Wails 都要重跑 binding diff、四平台 native smoke 与产物扫描。

**Linux 的 GTK 版本是产品承诺变更。** GTK4 + WebKitGTK 6.0 已在 alpha.93 提升为**默认**栈；只提供 WebKit2GTK 4.1 的发行版（含 Ubuntu 22.04）要走 legacy GTK3 路径。ADR-003 承诺 Ubuntu 22.04+，所以迁移期 Linux 正式产物应固定 `gtk3` build tag 并在 22.04 cleanroom 验证。**改用 GTK4 / 要求 24.04+ 必须另立 ADR**，不能在迁移里顺手做掉。

**Wails CLI 未安装**（本机 Go 1.26.4 满足其 1.25+ 要求）。阶段 5 开始前需要装。

### 3.2 已知局限

- **`ci.yml` 是 `workflow_dispatch` 专属**（按仓库所有者要求关闭自动触发）。所以上面那些门禁**只在手动从 Actions 页触发时生效，推送时不看着任何人**。这是当前最大的流程缺口：五个失效路径都堵住了，但整个门禁体系可以因为没人点那个按钮而完全不运行。恢复只需把 `push` / `pull_request` 触发器加回去，`ci.yml` 顶部注释说明了这一点。
- **Windows 的 `MoveFileEx` 路径只有交叉编译验证**，真机测试要等 CI 的 Windows runner。这条路径决定每一次覆盖写是否会破坏用户配置，且是在最难发现的平台上。
- **`platformNote` 与 `platform.shell` 取自宿主而 `platform.os` 取自 runtime**。这个不对称是 Python 的，照抄未修正——只在 runtime 模拟另一平台时可观测（即测试里，不是生产）。注释记了原因与「等 Python 删掉后再议」。

### 3.3 不是问题的两件事（曾被列为待办，已核实落地）

- **NOTICE 文件**：不在仓库里，因为它是**生成物**。`build_release.py:232` 调 `packaging/generate_notices.py` 生成进 `build/metadata/`，`:91` 声明它进产物，`test_release_policy.py:582` 的门禁断言 `go.mod` 每个模块都带许可文本进 NOTICE。生成器不依赖 `go` 在 PATH 上（`_module_cache()` 有 fallback），已用 `env -i PATH=/usr/bin:/bin` 实测 249 用例全过。
- **文案与 manifest 的联系**：`test_release_policy.py:548` 从 lock 读出实际的 manager 集合，断言文案提到每一个、且不提任何实现不接受的。把 `uv` 改回 `pip` 会红。

## 4. 之后要做什么

### 4.1 阶段 5：Wails 外壳与托盘

已核对官方文档（2026-07-29）的四条：

- 托盘 API 是 `app.SystemTray.New()`——application 上的一个 manager，不是 `application.SystemTray` 或 `app.NewSystemTray()`。方法含 `SetIcon`、`SetLabel`（macOS 是 label、Windows 是 tooltip）、`SetDarkModeIcon`、`SetTemplateIcon`、`SetMenu`。三平台都支持，图标规格各不同：Windows 16×16/32×32 PNG 或 ICO，macOS 18×18–22×22 PNG（推荐 template），Linux 22×22–48×48 且随桌面环境而异。
- 关窗不退出在 macOS 上要显式声明：`application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false}`。
- 需要 Go 1.25+（本机 1.26.4 满足）。
- Linux 见 §3.1。

实施要点：

1. **`client.ts` 的 `api` 对象换成 IPC 绑定实现，页面不改。** 前端的 `fetch(` 只有 `src/api/client.ts:43` 一处，`api` 对象（`client.ts:82`）是天然的适配器边界——页面只调 `api.status()` / `api.install(...)`，不接触路径与 HTTP。**这是迁移最大的省力点。**
2. **`POST /api/profiles` 不能丢。** 后端有、前端未接，但它是既有契约，不能因为「前端没用」而在换传输时丢掉。
3. **写锁已就位但要用对。** Wails binding 默认并发，而现有 `HTTPServer` 是串行的——这是形态切换带来的**新**并发面。`internal/app` 的 `Service` 已挂共享写锁，`Install`/`Activate`/`SaveProfile` 走它，读操作刻意不加锁（依赖原子替换）。危险不是文件写坏而是**工作被丢弃**：profile 的 `agent_ids` 并发装 4 个 Agent 实测丢 3 个（测试在 `desktop/internal/app/lock_test.go:32`，去掉锁会红并点名丢了哪三个）。
4. **生成的 binding 是后端 DTO 的唯一 TypeScript 真源。** `frontend/src/types/api.ts` 里手写的后端类型最终删除或改为薄别名，避免两份真源。CI 重生 binding 后做 diff 检查。
5. **外链只接 Provider ID**，由 Go catalog 解析白名单 URL 再交系统浏览器；不允许前端传任意 URL。
6. **常驻带来的四个新问题见[重写计划 §6](wails-rewrite-plan.md)，不解决不上线。** 其中一条是内存密钥显式清零，不为「下次免输」缓存。

### 4.2 阶段 6：分发（暂缓）

签名与公证换栈不会变简单，Developer ID 与 Authenticode 仍是前提。release manifest 删 `python` 字段，增加 Go/Wails/WebView 要求。

### 4.3 建议优先于阶段 5 处理的一件事

**把 CI 自动触发加回去，或者建立「每次推送前手动跑一遍」的明确约定。** 现在的状态是门禁很密但守卫可能没上班——这跟这套方法论一路在消灭的那类问题（「看着被覆盖，实际没有」）是同一个形状，只是位置在流程而非代码里。这是仓库所有者的决定，所以只提出不擅动。

### 4.4 删 Python 的前提

**不删，直到 Go 通过全部移植测试并完成一轮真实 cleanroom。** 当前 Go 已通过全部移植测试，但真实 cleanroom（尤其 Windows 真机与 Linux 22.04）还没跑。

## 5. 贯穿全程的不变量（改动前先确认不破坏）

- **lock 唯一真源**：`command`/`config_path`/`credential_delivery`/`windows_prerequisites` 全从 lock 读，**禁按 agent id 硬编码**。Claude Code 曾因为不在硬编码集合里而报「配置完成」却无法认证。
- **错误码契约**：11 个码与响应六键（`ok`/`error`/`message`/`status`/`error_code`/`retryable`，`exit_code` **不在其中**）不变。CLI 退出码是外部契约，`install.sh` 与 CI 分支读它。
- **密钥不落地**：不进 profile、argv、URL、日志、前端状态、浏览器存储；常驻新增一条：内存密钥显式清零。
- **传输契约**：`StatusResponse` 等派生字段 camelCase、请求体 snake_case。**不因 Go 惯例改动，也不借迁移做命名清理。**
- **不在迁移期新增功能**：托盘之外一切 1:1 迁移。不重设计 lock 格式、错误码或七页向导。
- **不让 Python 与 Go 在同一个真实 HOME 上双写做对比**；差分测试只操作独立临时目录。

## 6. 上手命令

```bash
# Go 内核。-race 是必须的：Wails 每个 binding 调用跑在独立 goroutine 里
cd desktop && go vet ./... && ONEAGENT_REQUIRE_PARITY=1 go test -race -cover ./...

# 只跑跨语言门禁
cd desktop && ONEAGENT_REQUIRE_PARITY=1 go test ./... -run TestParity -v -count=1

# Python 契约测试。必须是这 9 个模块——漏掉后三个只跑出 171
python3.12 -m unittest tests.test_core tests.test_cli tests.test_server \
  tests.test_release_policy tests.test_edge_cases tests.test_rc_scripts \
  tests.test_install_contract tests.test_config_discovery tests.test_distribution_data

# 前端
cd frontend && npm ci && npm run build && npm run test:coverage && npm run e2e

# Go CLI 实跑（阶段 5 之前唯一能执行 Go 内核的入口）
cd desktop && go build -o /tmp/oneagent ./cmd/oneagent
/tmp/oneagent --agent codex --api-key sk-... --model gpt-5-mini --skip-test --home /tmp/scratch
/tmp/oneagent status --home /tmp/scratch
```

本机 `python3` 是 3.14，测试与打包**必须显式用 `python3.12`**。Go 经 mise 安装，不在默认 PATH 上：`export PATH="$HOME/.local/share/mise/installs/go/1.26.4/bin:$PATH"`。

## 7. 交接时值得知道的两句话

**这套迁移的核心资产不是 Go 代码，是门禁。** 6887 行实现配 12021 行测试，其中 61 个跨语言用例把「等价」变成可证的而非声称的。删掉实现可以重写，删掉门禁就再也不知道哪里不等价了。

**缺陷持续表现为「成功」。** 记录在案的十余处里，绝大多数都通过了当时全部的门禁——无效 registry 返回 200、密钥在页面 markup 里而 e2e 报绿、四个适配器产物被写出后被忽略并报告一致、parity 步骤跑了 4 个包却名叫 all。所以新增门禁时**先把它弄红一次再相信它**，这条在这个项目里救过很多次。
