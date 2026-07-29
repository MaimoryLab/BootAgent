# 读取用户既有 Agent 配置，与全链路验证环境

> **实施状态（2026-07-29）**：第 2 节全部落地。五个 `read_*_config` 加 `detect_agent_config`，`status_payload` 每个 auto Agent 增 `detected`；前端 `targetSummary` 统一显示逻辑，详情页在覆盖非 OneAgent 配置前预警。`tests/test_config_discovery.py`（21 用例）与 `tests/existing_config_test.sh`（fixture，已接入两个 cleanroom）。真机验证：本机 Codex / Claude Code / OpenCode 的真实配置此前全部显示「未配置」，现在各自端点与模型都能读出并标注来源。
>
> 第 3 节的容器化 macOS 结论不变：硬件不支持，以 fixture 层替代。

两个需求：检测不能只看环境、还要看各 Agent 实际配置成什么样；以及在容器里的 macOS 虚拟机中跑全链路。

第二个需求在当前硬件上不成立，第 3 节给出证据和替代方案。第一个是真实缺口，已实测确认。

## 1. 现状：OneAgent 看不见自己没写过的配置

`status_payload()` 对每个 Agent 报四个字段，来源如下：

| 字段 | 来源 | 问题 |
| --- | --- | --- |
| `installed` | `runtime.which(command)` | 无 |
| `configured` | `path.exists()` | **只知道文件在，不知道里面是什么** |
| `provider` / `model` / `baseUrl` | `~/.oneagent/agents/<id>.json` | **OneAgent 自己的记账，不是 Agent 的真实配置** |

实测：手写一份 Codex 与 Claude Code 配置（模拟用户自己配过、或用别的工具配过），不建任何 OneAgent binding，然后读 status：

```
codex        configured=True  provider=None  model=None  baseUrl=None
claude-code  configured=True  provider=None  model=None  baseUrl=None

磁盘真实内容   codex  -> someone-else / gpt-5-mini / api.other-vendor.com
              claude -> api.third-party.com / claude-x
```

**`configured=True` 但三个字段全是 `None`。** 界面因此只能显示「未配置」，而磁盘上明明有一份指向别处的配置。三个后果：

- **总览说谎。** 用户看到「未配置」，实际有配置在生效。
- **覆盖无预警。** 用户点「应用」会静默盖掉自己手写的 provider（备份有，但界面没提示即将覆盖什么）。
- **产品定位打折。** [近期工作纪要](recent-work-summary.md) 说它要从「一次性工具」变成「长期管理设备上各个 Agent」，而管理的前提是先看得见。

顺带一个更小的问题：`configured` 这个名字对 guide-only Agent 也返回 `path.exists()`，而我们从不为它们写配置——那个 `True` 的含义与 auto Agent 完全不同。

## 2. 要做的：配置读取（inspect）

方向是**每个适配器补一个反向函数**：现在有五个 `write_*_config` 把结构化字段翻译成各 Agent 的格式，加五个 `read_*_config` 把格式翻译回结构化字段。这与既有设计一致——适配器是代码而非数据（见 [配置链路审查](config-chain-audit.md) 第 4 节），所以反向也应是代码。

### 2.1 数据形状

`status_payload` 的每个 Agent 增一个 `detected` 对象，与 `provider`/`model`/`baseUrl`（OneAgent 记账）并列而不是替换：

```python
"detected": {
    "baseUrl": "https://api.other-vendor.com/v1",
    "model": "gpt-5-mini",
    "managedByOneAgent": False,   # 配置里是否有 OneAgent 写的标记
    "provider": None,             # 能反查到内置 Provider 则给 id，否则 None
    "unreadable": None,           # 解析失败时给原因
}
```

两者并列的理由：不一致本身就是要显示的信息。binding 说 PPIO 而磁盘说别的，意味着用户在 OneAgent 之外改过配置——这正是「长期管理」要告诉用户的事。

`managedByOneAgent` 的判据是配置里的既有标记，不需要新增字段：Codex 看有没有 `[model_providers.oneagent]`，OpenCode/Kilo 看 `provider.oneagent`，Claude Code 看 `env` 里四个 `ANTHROPIC_*` 是否齐全。

**Aider 是例外，恒为 `false`。** 计划原本写「看脚本里是否是我们的两行 export」，fixture 实测推翻了它：手写的 Aider 脚本与我们写的形态完全一样（都是两行 export），没有任何标记能区分。它的配置又在 `~/.oneagent/` 下——本来就是我们的目录。所以承认无法区分，而不是让一个猜测冒充判据。

### 2.2 五个读取函数

- `read_codex_config` —— TOML，取 `model_provider` 指向的那个 `[model_providers.*]` 的 `base_url`，以及顶层 `model`。注意**不读 `env_key` 指向的值**（那是密钥）。
- `read_claude_config` —— JSON，取 `env` 里的 `ANTHROPIC_BASE_URL` 与 `ANTHROPIC_MODEL`。
- `read_openai_compatible_config` —— JSON，取 `provider.<name>.options.baseURL` 与 `model`（形如 `oneagent/<model>`，要剥前缀）。OpenCode 与 Kilo 共用。
- `read_aider_config` —— shell/PowerShell 脚本，取 `OPENAI_API_BASE`。这个要用行解析而非执行脚本。
- 未知 adapter 一律返回 `unreadable`，不猜。

### 2.3 硬性约束

**绝不把读到的密钥放进响应。** 这是最需要小心的一点：五份配置里有三份含密钥明文（Claude 的 `ANTHROPIC_AUTH_TOKEN`、Aider 的 `OPENAI_API_KEY`，以及 OpenCode 若用户手填过 apiKey）。读取函数必须只提取 base URL 与 model，且：

- `detected` 里没有任何密钥字段，连布尔的 `hasKey` 也先不加（想加要单独评估：它会泄漏「这台机器上有没有配过 Key」）。
- 读取过程中的异常消息不得包含文件内容片段——`CONFIG_WRITE_FAILED` 现有的错误消息带路径不带内容，沿用同样做法。
- 加一个测试：构造含 `sk-` 明文的配置，断言 `/api/status` 全文不含它。这类断言 `test_core.py` 已有先例（`test_api_key_reaches_only_the_designated_secret_files`）。

**解析失败不能让 status 挂掉。** 一份被改坏的 TOML 现在会让整个 `/api/status` 500，从而整个界面白屏。每个读取函数必须自己吞掉解析异常并返回 `unreadable`，理由与 `write_*` 遇到坏配置时返回 `CONFIG_WRITE_FAILED` 而非静默覆盖一致——只是这里的失败必须是局部的。

**只读，不改。** inspect 路径不得触发任何写入，包括不得「顺手修正」格式。

### 2.4 界面

`AgentManageRow` 与 `AgentDetailPage` 现在显示 binding。改为：

- 有 binding 且与 detected 一致 → 照现在显示。
- 有 detected 无 binding → 显示 detected，标注「检测到的配置（非 OneAgent 写入）」。
- 两者不一致 → 显示 detected 为主，提示 binding 记录的值不同。
- `unreadable` → 显示「配置无法解析」并给出路径，不显示猜测值。

详情页的「应用」按钮在 detected 存在且非 OneAgent 管理时，应当说明即将覆盖什么。这是把「无预警覆盖」变成有预警。

### 2.5 测试

- 五个适配器各一组「写入→读回」往返用例，断言读回的 baseUrl 与 model 等于写入值。这条最有价值：它同时锁住两个方向。
- 手写的、非 OneAgent 格式的配置能被读出正确值（就是第 1 节那个实测场景，固化下来）。
- 坏配置返回 `unreadable` 且 `/api/status` 仍返回 200。
- 密钥不出现在响应里。
- guide-only Agent 不产生 `detected`。
- `installer.py` 100% 分支、零 partial 的门禁不变。

## 3. 关于「Docker 里的 macOS 虚拟机」

**这台机器上不可行**，原因是硬件而非配置：

```
主机架构        arm64（Apple Silicon）
kern.hv_support 1（主机支持虚拟化）
容器内 /dev/kvm No such file or directory
容器 x86 支持   有（用户态转译）
```

`sickcodes/docker-osx` 一类方案的原理是容器内 QEMU 加载 **x86_64** macOS 镜像，前提是 `/dev/kvm` 可用。两个条件都不满足：

1. **Docker Desktop 的 LinuxKit VM 不暴露嵌套虚拟化**，容器里没有 `/dev/kvm`。这不是权限问题，是 Docker Desktop on macOS 的架构决定的。
2. **arm64 主机上没有可用的 macOS 虚拟磁盘镜像。** Apple Silicon 版 macOS 只能通过 Apple 自己的 Virtualization.framework 跑，不能被 QEMU 当作 x86 客户机加载。容器里的 x86 支持是用户态二进制转译，跑不动一个内核。

即使强行在 x86 主机上做，还有两个问题值得先说清：**Apple 许可证只允许在 Apple 硬件上虚拟化 macOS**，容器化 macOS 镜像的分发本身处在灰区——这与 [产品边界基线](product-boundary-baseline.md) 对分发合规的要求不一致，不适合成为项目的标准验证设施。

### 替代方案：已有的两层，加一层缺口

需求的实质是「全链路在干净 macOS 上可重复验证」。现有设施已经覆盖了大部分：

| 层 | 设施 | 覆盖 |
| --- | --- | --- |
| Linux 断网容器 | `scripts/test_docker_cleanroom.sh` | 契约测试、`install.sh`、GUI 冒烟、浏览器 e2e、策略扫描 |
| 真实 macOS | `tests/macos_cleanroom_test.sh` | 干净 HOME、权限、打包二进制、真实 HOME 零污染快照 |
| 真实安装 | `tests/real_install_test.sh` | 真装两个 Agent、PATH、版本、双 registry |

**真实缺口不是「缺一个 macOS 环境」，而是这三层都不覆盖「用户已有配置」这个起点。** 它们都从干净 HOME 开始，所以第 2 节要读的那种「外部写入的配置」在任何 cleanroom 里都不会出现。

所以要加的是一层 fixture，而不是一个虚拟机：

- 在 `tests/` 下建一组**既有配置样本**（手写风格的 Codex TOML、第三方 Claude settings、含额外用户字段的 OpenCode JSONC、坏格式各一份）。
- macOS cleanroom 与容器 cleanroom 各增一个阶段：把样本放进干净 HOME，断言 status 能读出正确的 detected 值、坏样本报 `unreadable` 而非 500、写入后用户的额外字段仍在。
- 这一层是离线纯文件操作，两个 cleanroom 都能跑，也能进常规 CI。

如果确实需要一台干净 macOS 做人工验收，可行路径是本机的 Virtualization.framework（Tart、UTM 或 `macosvm`），而非 Docker。那属于本地开发环境搭建，不该进 CI——CI 里的 macOS 由 GitHub Actions 的 `macos-14` runner 提供，已经在用。

## 4. 明确不做

- **不为读取配置引入任何写入。** inspect 只读。
- **不把密钥或其存在性放进 API 响应。** 只提取 base URL 与 model。
- **不猜未知格式。** 未知 adapter 或解析失败一律 `unreadable`。
- **不做容器化 macOS。** 硬件不支持，且分发合规不成立。
- **不改 `configured` 字段的现有语义**（避免破坏前端契约），新增 `detected` 与之并列；若将来要收敛，另开一次改动。

相关文档：[配置链路审查](config-chain-audit.md)、[空白机器可用性验证计划](blank-machine-verification-plan.md)、[产品边界基线](product-boundary-baseline.md)。
