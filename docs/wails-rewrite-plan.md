# 迁移到 Wails 的重写计划

目标：把 Python 内核全量重写为 Go，用 Wails 取代「本地 HTTP + 浏览器」的形态，以支持托盘常驻、开机自启和单文件安装。团队已有 Wails 背景，所以选型不再是问题；这份计划要解决的是**怎么迁移而不丢掉已经验证过的东西**。

## 1. 前提变更

[ADR-003](decisions/ADR-003-three-platform-python-core-and-release-policy.md#L105) 拒绝桌面壳的理由是「当前本地浏览器 GUI 已能满足流程需求」。**托盘常驻在这个形态下不可能实现**——托盘需要原生窗口系统 API，进程生命周期不能绑在浏览器标签页上，开机自启需要一个能被 LaunchAgent / 注册表拉起的可执行体。前提不再成立，结论要重算。

同时修正 ADR-003 的一处事实错误：它说桌面壳「引入额外运行时」，这对 Electron 成立，对 Wails 不成立——Wails 用系统 WebView（macOS WKWebView、Windows WebView2、Linux WebKitGTK），产物是单个 Go 二进制。

边界确认：`CLAUDE.md:71` 的「不起后台服务」约束针对 **guide-only Agent**（不代管别人的网关），不禁止 OneAgent 自己常驻。托盘不越界。**但常驻会带来新的边界问题，见第 6 节。**

## 2. 要迁移的资产

```
oneagent/          2900 行 Go 重写
  installer.py     1745 行  67 函数  ← 主体：原子写、备份、权限、5 适配器、5 读取器
  server.py         504 行  26 函数  ← 大部分作废（Wails 用 IPC 而非 HTTP）
  providers.py      406 行  15 函数  ← 探测与 URL 推导，纯逻辑，最易迁移
  catalog.py        227 行  10 函数  ← lock 解析与平台判定
  cli.py            207 行   7 函数  ← 保留为独立 CLI（见 5.4）
  errors.py          45 行         ← 错误码契约，直接照搬

frontend/src       3134 行  ← 完整保留，一行不改
agents.lock.json   ← 纯数据，直接复用
distribution/*.json ← 纯数据，直接复用

tests/             4400 行 245 用例  ← 必须逐条移植，不是重写
```

**前端不动是这次迁移最大的省力点。** Wails 的前端就是普通的 Vite 产物，`frontend/` 现有的 React、路由、状态、78 个单测和 15 个 e2e 全部照用。只有 `src/api/client.ts` 一个文件要改——从 `fetch` 换成 Wails 生成的 IPC 绑定。

## 3. 最难的三处,以及为什么

这三处决定迁移是否真的等价。它们都不是「用 Go 重写一遍」那么简单。

### 3.1 `atomic_write` 的失败路径

现在的实现（`installer.py:167`）有一条不显眼但重要的次序：**先给临时文件加固权限，再 `os.replace`**。注释写着理由——Windows 上如果 ACL 设置失败，用户的原文件不能已经被替换掉。

Go 侧要对应的完整语义：

1. `ensure_private_dir` → 0700（Windows 走 `icacls /inheritance:r`）
2. 备份 `*.backup-<ts>`；**若是密钥文件且备份无法加固，删除备份并报错**（不能留一个宽权限的密钥副本）
3. 临时文件写入同目录（跨设备 rename 会失败）
4. **加固临时文件权限**
5. `os.Rename`（Go 的 `os.Rename` 在 Windows 上对已存在目标的行为与 POSIX 不同，需要用 `MoveFileEx` 加 `MOVEFILE_REPLACE_EXISTING`）
6. `finally` 清理临时文件，**清理失败本身也要报错**

第 2 步和第 6 步是现有测试专门覆盖的分支。Go 侧不能简化。

### 3.2 Windows 权限加固

`secure_path` 在 POSIX 上是 `chmod(0o600/0o700)`，在 Windows 上调 `icacls` 断继承并只授权当前用户与 SYSTEM。Go 有两条路：

- **继续调 `icacls`** —— 与现在等价，但仍是子进程，且要保留「找不到 icacls 就报错」的行为
- **用 `golang.org/x/sys/windows` 直接设 DACL** —— 更干净，但要自己构造 SID 与 ACL，出错方式更隐蔽

建议先用 `icacls` 保持等价，把 DACL 版本作为后续优化，并且**两者都要通过同一组测试**。

### 3.3 Runtime 依赖注入

现在所有副作用走 `Runtime` dataclass（`home`/`os_id`/`runner`/`which`/`env`），测试靠替换这五个字段模拟 npm、uv 和四个平台，**不触碰真实系统**。这是 245 个用例能在毫秒级跑完、且 `installer.py` 能有 100% 分支覆盖的原因。

Go 侧必须先建立等价物，否则测试无法移植：

```go
type Runtime struct {
    Home  string
    OS    string
    Run   func(ctx context.Context, argv []string, env []string) (Result, error)
    Which func(name string) (string, error)
    Env   map[string]string
}
```

**这一步要排在所有业务代码之前。** 先有可注入的 Runtime，再逐个迁移函数，每迁一个就把对应测试移过来。反过来做会得到一堆无法测试的代码。

## 4. 阶段划分

每个阶段结束时都要能跑通全部已移植的测试。不允许「先全写完再补测试」。

### 阶段 0：地基（无业务逻辑）

- Go 项目骨架、Wails 初始化、CI 加 Go 工具链
- `Runtime` 及其测试替身
- `errors` 包：照搬 `EXIT_CODES` 与 `OneAgentError` 的码值和退出码映射
- **门禁**：`errors` 的码值与 `oneagent/errors.py` 逐一相等，用一个读取 Python 源码或共享 JSON 的测试锁住

### 阶段 1：纯逻辑（无 IO）

`catalog.py` 与 `providers.py` 的绝大部分：lock 解析、平台判定、URL 校验与推导、`pick_chat_model`、`resolve_probe_model` 的纯计算部分。

这一批最容易，也最先能建立信心。`test_core.py` 与 `test_edge_cases.py` 里对应的用例可以几乎一比一移植。

- **门禁**：移植的用例全过；`agents.lock.json` 由两侧解析出相同结果（可写一个交叉验证测试）

### 阶段 2：写入链路（最危险）

`atomic_write`、`ensure_private_dir`、`secure_path`、`backup_file`，以及五个 `write_*_config`。

顺序建议：先做 `atomic_write` 及其全部失败分支，再做适配器。适配器依赖前者，反过来会让失败路径缺测试。

- **门禁**：`installer.py` 现有的写入相关用例全部移植且通过；**新增一组跨实现对比测试**——同样输入下 Python 与 Go 产出字节相同的配置文件。这是唯一能证明「等价」而非「大概一样」的手段。

### 阶段 3：读取链路

五个 `read_*_config` 与 `detect_agent_config`。这批刚做完（见 [配置读取计划](config-discovery-plan.md)），语义新鲜且测试完整（21 用例）。

- **门禁**：`tests/test_config_discovery.py` 全部移植；坏配置不得导致进程崩溃（Go 里要用 `recover` 对应 Python 的兜底 `except Exception`）；密钥不出现在任何返回值中

### 阶段 4：安装链路

`install_locked_agent`、`install_many`、`verify_npm_integrity`、镜像与 registry 解析、前置条件检查。

- **门禁**：`test_install_contract.py` 的 42 个用例全部移植；`tests/real_install_test.sh` 改为驱动 Go 二进制后仍通过（官方源与镜像双路径）

### 阶段 5：Wails 外壳与托盘

- IPC 绑定替换 `client.ts` 的 `fetch`
- 托盘：菜单项、状态显示、退出
- 窗口生命周期：关窗到托盘而非退出
- 开机自启：macOS LaunchAgent、Windows 注册表、Linux autostart
- **托盘常驻带来的新问题见第 6 节，必须一并解决**

### 阶段 6：分发

- macOS `.app` + `.dmg`、Windows 安装器、Linux AppImage 或 deb
- 签名与公证：**这是当前未解决的问题，换栈不会让它变简单**，Developer ID 与 Authenticode 证书仍是前提
- `build_release.py`、`check_release.py`、发行索引与公开站的产物校验都要跟着改

## 5. 明确的保留与舍弃

### 5.1 保留：`agents.lock.json` 作为唯一真源

这条已经付出过代价才建立起来（见 [配置链路审查](config-chain-audit.md)）。Go 侧同样禁止按 agent id 硬编码行为，`command`、`config_path`、`credential_delivery`、`windows_prerequisites` 都从 lock 读。

### 5.2 保留：错误码契约

`errors.EXIT_CODES` 与响应恒定携带 `error/message/status/error_code/retryable`。CLI 的退出码是外部契约，不能因为换语言而变。

### 5.3 保留：密钥不落地的全部约束

API Key 不进 `profile.json`、argv、URL、日志、前端状态、浏览器存储；日志过 `redact`。Go 侧要有等价的 `redact` 并同样被测试覆盖。**托盘常驻会新增一条：内存中的密钥不能因为进程长期存活而长期驻留。**

### 5.4 保留：独立 CLI

`cli.py` 现在是 `python -m oneagent.cli`，有 16 个测试。Wails 二进制应支持无 GUI 的命令行模式（Wails 允许在 `main` 里根据 argv 分流），沿用 `entrypoint.py` 现在的做法：无参数进 GUI，有参数走 CLI。

### 5.5 舍弃：本地 HTTP server

`server.py` 的 504 行大部分作废——Wails 用 IPC，不需要 HTTP、Origin 白名单、会话 Cookie。**但要注意这是安全约束的一次性质变**：现在的「只绑 127.0.0.1 + Origin 校验 + `compare_digest` 会话」是为了防本地其他进程访问；换成 IPC 后这些约束消失，但也不再需要——这一点要在 ADR 里写清楚，否则下次审查会以为约束被悄悄放弃了。

`gui_smoke_test.py` 与相关的 e2e 要相应改写。

### 5.6 舍弃：PyInstaller 与 Python 运行时

产物从 9.2MB 的 zip（含 `libpython3.12.dylib`）变成单个 Go 二进制装在 `.app`/安装器里。用户不再需要解压。

## 6. 常驻带来的新问题（不解决就不该上线）

这些是当前形态下不存在、常驻后必然出现的：

- **长期存活的进程持有过什么？** 现在进程随浏览器标签页结束，密钥在内存里活不过一次操作。常驻后必须显式清零，且不能为了「下次不用重输」而缓存。
- **托盘是否让用户以为 OneAgent 在代理流量？** 产品边界明确禁止代理与常驻网关。托盘图标容易造成这种误解，文案与菜单要主动说明「OneAgent 不转发任何请求」。
- **开机自启是否默认开启？** 建议默认关闭、由用户显式启用。一个配置工具默认自启会被合理地质疑在后台做什么。
- **自动更新。** 常驻应用通常带自动更新，而 [ADR-005](decisions/ADR-005-channel-neutral-distribution-and-compliance.md) 的渠道中立与版本锁定要求意味着不能静默换二进制。若要做，必须是「提示 + 用户确认 + 校验签名」。

## 7. 风险与止损点

**最大风险是「等价」难以证明。** 245 个用例移植完不等于行为一致——测试覆盖的是已知场景，而配置文件合并、权限加固、路径处理都有大量未被显式测试的边角。缓解手段是第 2 阶段那组**跨实现字节比对测试**：同输入下两个实现产出的文件必须完全相同。这个测试在迁移期一直保留，迁移完成后再删。

**建议的止损点**：阶段 2 结束时评估。如果 `atomic_write` 及五个适配器的字节比对无法通过，说明等价性成本被低估，此时沉没成本仍可控（前端未动、Python 版仍可发布），应当回头考虑「Wails 外壳 + Python sidecar」的折中方案。

**不建议的做法**：Python 与 Go 双版本长期并行。两套实现会分叉，而 lock 是唯一真源这件事只有在一处实现时才守得住。

## 8. 不做

- 不在迁移期新增功能。托盘之外的一切按现状 1:1 迁移，功能演进等迁移完成。
- 不重新设计 lock 格式或错误码。它们是跨语言的契约，换语言正是它们该保持不变的时候。
- 不因为 Go 的惯例改动前端契约。`StatusResponse` 等派生字段仍是 camelCase，请求体仍是 snake_case。
- 不删除 Python 实现，直到 Go 版通过全部移植测试并完成一轮真实 cleanroom。

相关文档：[ADR-003](decisions/ADR-003-three-platform-python-core-and-release-policy.md)、[产品边界基线](product-boundary-baseline.md)、[配置链路审查](config-chain-audit.md)、[配置读取计划](config-discovery-plan.md)。
