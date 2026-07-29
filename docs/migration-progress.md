# Go 迁移进展与审查记录

covering 13 commits，从 `6899cf1`（关闭自动 CI）到 `0794918`。

战略见 [重写计划](wails-rewrite-plan.md)，执行细节见 [执行计划](wails-migration-execution.md)，决策见 [ADR-008](decisions/ADR-008-go-core-and-wails-desktop-shell.md)。这份文档记录**做到了哪一步、发现了什么、还差什么**。

## 1. 现状

阶段 0 到 4 的核心已完成，**止损点已通过**。生产实现仍是 Python，Go 未接线。

| 阶段 | 内容 | 状态 |
| --- | --- | --- |
| 0 | 错误码契约、可注入 Runtime、测试替身 | 完成 |
| 1 | catalog、provider 纯逻辑 | 完成 |
| 2 | securefs、jsonorder、shellquote、五个写入适配器 | 完成（止损点通过） |
| 3 | 五个读取器与配置发现 | 完成 |
| 4 | 安装编排、integrity、镜像、前置条件 | 核心完成；`install_many`/`activate_agent`/`status_payload` 未移植 |
| 5 | Wails 外壳与托盘 | 未开始 |
| 6 | 分发 | 未开始（按要求暂缓） |

全部数字用 `go test -json` 逐条计数得出，不再手工累加——**上一版写的「807 用例」是错的**，实测顶层 325、含子测试 574。审查指出它时我的第一反应是去核对而不是解释，因为「106 不是 111」用的就是同一把尺子。

```
Go 源码 3867 行，测试 7929 行
测试 325 个顶层用例 / 574 个含子测试
跨语言门禁 11 个文件、47 个顶层用例、199 个逐输入子测试
  其中 87 个是配置文件与 env 文件的逐字节比对，13 个是原子写次序
整体覆盖 90.4%（逐包见下表），go test -race 干净
Python 246 用例不受影响（新增 1 个回归测试）
前端 78 单测 + 20 e2e（新增 5 个对比度测试）
```

`go test -json` 的计数命令记在 [执行计划 §4.6](wails-migration-execution.md)，下次报数直接跑，不再凭记忆。

| 包 | 覆盖 | 职责 |
| --- | --- | --- |
| `oerr` | 100% | 错误码与响应形，跨语言门禁锁定 |
| `runtime` | 100% | 全部副作用的注入接缝 |
| `catalog` | 98.0% | 内嵌 manifest、Provider 与镜像投影 |
| `provider` | 97.2% | URL 校验推导、协议映射 |
| `config` | 93.0% | 五写五读适配器、TOML 合并、env 文件 |
| `testutil` | 92.1% | 测试替身（自身的保证也被测试） |
| `install` | 87.9% | 锁定安装、integrity、镜像、前置条件 |
| `securefs` | 86.5% | 七步原子写、备份、权限、Windows ACL |
| `jsonorder` | 83.7% | 保序 JSON，复刻 `json.dumps` |
| `shellquote` | 76.9% | `shlex.quote` 与 PowerShell 引用 |

## 2. 字节比对找到了什么

**这是整个迁移最有价值的部分。** 移植 245 个测试只能证明「有人想到要写下来的场景」一致；把两个实现跑在同一批输入上、逐字节比对输出，找到了 5 处手写测试不会发现的分歧。

### 2.1 数字格式（jsonorder）

保留原文本是显而易见的做法,也是错的。Python 的 `json.loads` 把任何带指数的数字提升为 float,`json.dumps` 再写 `repr()`:

```
用户文件里的 1e10  →  往返后变成 10000000000.0
用户文件里的 1e-5  →  往返后变成 1e-05
整数与普通小数     →  原样保留
```

这些值是用户设的 timeout 与 token 上限,**改写它们正是「保留非管理字段」要防的静默编辑**。现有 40 种数字形态跨越 Python 两个指数阈值两侧的比对。

### 2.2 codex 与 JSON 适配器的转义语义相反

codex 适配器用 `json.dumps` 默认值(`ensure_ascii=True`),非 ASCII 模型名写成 `\uXXXX`;另外三个用 `ensure_ascii=False`,原样 UTF-8。共用一个 helper 会通过**所有**手写测试,只在没人写测试的输入上产出不同字节。已通过故意改错验证会被抓住。

### 2.3 `openai_base_url` 的后缀剥离

现有实现是「每个后缀检查一次」。改写成看起来更整洁的「循环直到稳定」后:

```
OpenAIBaseURL("https://example.com/v1/models/models")
  Go:     "https://example.com/v1"
  Python: "https://example.com/v1/models/v1"
```

没有任何手写用例会覆盖 `/v1/models/models` 这种输入。

### 2.4 home 解析的兜底能力不同

Python 的 `Path.home()` 在 `HOME` 缺失时回落到 passwd 数据库;Go 的 `os.UserHomeDir` 只读 `$HOME` 并报错。照直移植会让 Go 在 Python 能解析出 home 的场合解析不出,而这**不表现为明显失败**——它表现为上层某处给出不同的退出码,也就是外部契约的静默改变。Go 侧补了 `user.Current()` 一跳。

### 2.5 `catalog.Agent` 漏读两个 manifest 键

`windows_config_path` 与 `version_args` 在 lock 里有、Go 结构体里没有,静默读成空。**字节一致的 embed 对此毫无保证——字节相同不代表读取相同。** Aider 是唯一声明 Windows 专用配置路径的 Agent,漏读会在 Windows 上把配置写到 POSIX 位置。

这个是手工发现的,不可复制,所以加了一个反射结构体标签的测试:manifest 声明了而无字段读取即失败。

## 3. Go 默认行为的五个陷阱

第 2 阶段预测并全部处理:

| 陷阱 | Go 默认 | 后果 |
| --- | --- | --- |
| HTML 转义 | `encoding/json` 把 `&` 写成 `&` | 含查询串的 URL 与 Python 输出不同 |
| 键序 | `map` 序列化按字母序 | 用户既有字段被重排 |
| `ensure_ascii` 双语义 | 无对应概念 | 见 2.2 |
| 结尾换行 | `MarshalIndent` 不加 | 每个配置文件都差一个字节 |
| Windows 替换 | `os.Rename` 对已存在目标失败 | 在最难发现的平台上破坏每次覆盖写 |

前四个已通过「故意改回 Go 默认」验证比对会打红。第五个是 Windows-only,目前只有交叉编译验证。

## 4. 门禁本身的四个静默失效路径

`go test -run` **匹配零个测试时退出码为 0**。这让「跑 parity 测试」这个 CI 步骤有四种变绿而不报错的方式,全部已堵住并实测验证:

| 失效方式 | 旧行为 | 现在 |
| --- | --- | --- |
| 改名（不再匹配 `-run`） | 静默 skip | `internal/parity` 计数断言点名失败 |
| 删掉整个 parity 文件 | `ok ... [no tests to run]` | `internal/parity` 断言文件存在 |
| Python 不在 PATH | `t.Skip`,报 ok | CI 设 `ONEAGENT_REQUIRE_PARITY`,硬失败 |
| 新增 parity 文件未进清单 | 看着被覆盖,实际不在计数内 | 反向检查捕获 |

第一版把计数自检写在被计数的文件内部——能拦改名,但**删掉该文件后自检本身也不在了**。所以计数移到独立包,从外部断言。

`internal/parity/gate_test.go` 的 `expected` 表逐文件声明最低测试数。新增 parity 文件必须加一行,这是有意的。

## 5. 真实浏览器审查

在 Chromium 中驱动运行中的产品,而非只跑测试套件。发现两个缺陷,**都表现为成功**——与此前三个缺陷同一形状。

### 5.1 无效 registry 被静默接受（已修）

`resolve_registry` 只在 `install_locked_agent` 内被调用,而后者对**已安装的 Agent 提前返回**。所以一个指定 `http://` registry 或 URL 内嵌凭据的请求返回 200,设置被忽略:

```
修前: registry=http://evil.test/          → HTTP 200 ok（设置被丢弃）
修后: registry=http://evil.test/          → HTTP 400 INVALID_REQUEST
      registry=https://user:pass@evil...  → HTTP 400 INVALID_REQUEST（不回显凭据）
```

修法是移到 `install_many` 与 Agent ID 校验并列:请求要么可接受要么不可接受,与是否需要安装无关。已加回归测试。

### 5.2 三个颜色 token 低于 WCAG AA（已修）

在实际使用的字号下测量:

| token | 用法 | 修前 | 修后 |
| --- | --- | --- | --- |
| `--text-tertiary` | 11px 次要文字 | 3.0:1 | `#6b6b70` → 4.9:1 |
| `--blue` | 链接文字与按钮填充 | 4.0:1 | `#0b66cc` → 5.6:1 |
| `--green` | 「已安装」徽章 | 4.0:1 | `#1f7a35` → 4.9:1 |

三者都像是有意的设计值,这是没被发现的原因——**只有对真正被绘制的颜色算出比值,失败才会显现**。新值是算出来的而非挑的:`#6b6b70` 在它出现的三种背景上都过 4.5:1,同时比 `--text-secondary` 仍有可见差别。

新增 5 个 e2e 对比度测试。它们把半透明层**合成**到不透明色再测量——第一版没有,于是在分段控件上报了一个并不存在的失败(读 chip 的原始 rgba 得到一个从不被绘制的颜色,合成后实际是 14.9:1)。恢复旧 token 会让全部 5 个测试失败,以此确认它们非空转。

### 5.3 确认正常的部分

审查同样值得记录否证:

- 产物无 source map、无 CDN 引用、仅系统字体
- 静态处理器无路径穿越(5 种尝试全部 404 且无内容泄漏)
- CSP 含 `frame-ancestors 'none'`,因此 `X-Frame-Options` 是冗余而非缺失
- POST 的三重拒绝全部生效:缺失 Origin、外部 Origin、缺失会话
- 配置检测在真实机器上工作:发现一个手写的第三方 Codex 配置,并正确标记 `managedByOneAgent: false`
- `/api/status` 中唯一的 key 相关字段是 `profiles[].hasKey` 布尔(契约允许),无任何凭据形状的值

## 6. 第二轮审查修掉的四处

前三处的共同点是**注释断言了一个契约,而语料没有测它**——正是这套方法论声称要消灭的那类分歧。

### 6.1 TOML 解析错误会把密钥材料带进 status 响应（已修）

`readers.go` 把 `err.Error()` 拼进 `unreadable`,而 `BurntSushi/toml` 会回显出问题的 token:

```
api_key = sk-probe-abc      → expected value but found "sk" instead
api_key = 1.2.3.4.5.6       → Invalid float value: "1.2.3.4.5.6"   ← 完整回显
```

`unreadable` 经 status 进 React state 并显示在界面上,这直接撞上「API Key 不进 React state、日志」的明文承诺。触发条件(用户手工把配置编辑成非法 TOML 且密钥未加引号)罕见,但**处理坏掉的用户文件是这个读取器存在的全部理由**。

它同时是与 Python 的分歧,而正确答案不是「无法一致所以放行」:`tomllib` 与 `json` 只报位置、从不回显内容,所以**砍掉 `err.Error()` 既是安全修复,也是更严格的等价位置**。连报行列号都不行——`[a\n` 在 `tomllib` 是第 1 行、`BurntSushi/toml` 是第 2 行,那会引入一处新分歧。中文前缀原样保留,它是前端展示的契约。

新增 4 个用例,断言消息**恰好**等于前缀。恢复 `err.Error()` 会让 4 个全红。

### 6.2 失败摘要的截断与 Python 不一致（已修）

注释写「按字节截断以匹配 Python 的切片」——这是错的。Python 的 `[:600]` 切 `str`,数的是码位;Go 的 `len()` 数字节。npmmirror 的错误就是中文:

```
600 个中文字符  Python 保留 600 字  |  Go 保留 200 字,且从字符中间切断
```

从多字节字符中间切断留下非法 UTF-8 尾巴,`encoding/json` 把它变成 U+FFFD。原有 11 个用例**没有一个超过 600**,所以那句注释声称双方一致的截断从未被比对过。改为 `[]rune` 切片,并补三条:超长 ASCII、超长中文、边界正好压在多字节字符上。**只有后两条会因字节切片打红**——超长 ASCII 两种实现都对,这也说明为什么原语料通不过。

### 6.3 `Load()` 的数据竞争（已修，被指两次）

`Load()` 用 nil 检查缓存解析结果,8 协程并发下 `-race` 报 `WARNING: DATA RACE`。它今天不炸只因为 Go 还没接线,而 ADR-008 自己写明 Wails binding 并发——**编排层一接进外壳它就是门禁下的第一个失败,且外壳会成为表面原因**。改用 `sync.OnceValues`;`Load` 保持函数形态,`loadEmbedded` 才是变量,否则调用方可以替换它。

新增 `TestLoadIsSafeForConcurrentReaders`。改回 nil 检查会让它红——已实测,这是它不是装饰的依据。

### 6.4 空 userinfo 的分歧（已修）

`https://@host/` 不携带任何凭据,但 Go 为空 userinfo 段也构造 `Userinfo`,所以 `parsed.User != nil` 拒了 Python 接受的 URL。抽出共享的 `provider.HasCredentials`,两处语料各补三条。这一条两个调用点(`ValidateBaseURL` 与 `ResolveRegistry`)同源,所以一起收。

## 7. 我在过程中出错的地方

值得记录,因为它们说明哪种测试真正有效:

- **三次把测试写成断言错误行为**。清空 HOME 应返回空(错)、数字应保留原文本(错)、版本号不应从长数字中读取(错)。三次都是跨语言比对纠正了我,而不是我的推理。
- **CI 过滤器第一版永远绿**。写成 `-run Parity` 时匹配零个测试,而 `go test` 对此返回成功。
- **对比度测量第一版报假阳性**。未合成半透明层。
- **两次误判浏览器审查结果**。`localhost` 不可达(错,curl 会回落)、装饰性图标缺 alt(错,`alt=""` + `aria-hidden` 是正确写法)。
- **报了一个手工累加的用例数**。上一版写「807 用例」,`go test -json` 实测顶层 325、含子测试 574。同一份文档里我为「111 改 106」专门留了一行,却对更大的那个数字没用同一把尺子。现在计数命令写进[执行计划 §4.6](wails-migration-execution.md),报数前跑一遍。
- **两处注释断言了没被测的契约**(见 6.1、6.2)。注释里写「双方必须一致」而语料不覆盖它,比不写更糟:它让下一个人以为这里已经被守住了。

## 8. 还差什么

**阶段 4 剩余**:`install_many`(228 行)、`activate_agent`、`status_payload`、profile 存储。这些是编排层,依赖已完成的全部下层。

**阶段 5**:Wails 外壳。已核对官方文档的四条约束——仍是 Alpha(只能发 technical preview)、需 Go 1.25+、托盘 API 是 `app.SystemTray.New()`、Linux 因 GTK4 成为默认栈需固定 `gtk3` tag 以维持 Ubuntu 22.04 承诺。

**阶段 6**:分发。按要求暂缓。

**两个已知局限**:

- `ci.yml` 是 `workflow_dispatch` 专属(按仓库所有者要求关闭自动触发),所以所有门禁只在手动触发时生效。
- Windows 的 `MoveFileEx` 路径只有交叉编译验证,真机测试要等 CI 的 Windows runner。

**两个新依赖**需进第三方 notice:`BurntSushi/toml` v1.6.0（MIT，仅校验）与 `golang.org/x/sys` v0.47.0（BSD-3-Clause，Windows rename），均精确锁版本。本机到 `proxy.golang.org` 不通,经 `goproxy.cn` 获取——与 npm 路径同样的授权镜像原则,`go.sum` 锁住字节。

**两种许可都要求随二进制分发许可文本**,而 NOTICE 文件目前还不存在——现在只是文档里写着「需进」。这必须在下一次发布构建之前落地,不能等到阶段 6:一旦有人跑出一个带 Go 内核的产物,它就已经缺了该带的东西。

**移植编排层时要一并搬过去的一件事**:Python 侧的 registry 前置校验已提到 `install_many`（见 5.1），而 Go 的 `LockedAgent` 内部仍是惰性校验。不搬,浏览器审查抓到的那个 200 就会在 Go 版复活。
