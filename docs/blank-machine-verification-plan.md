# 空白机器可用性验证计划（Codex 与 Claude Code）

目标：证明一台没装过任何 Agent 的机器，能用 OneAgent 把 Codex 和 Claude Code 装好并真正跑起来。范围限定这两个自动配置 Agent。

> **实施状态（2026-07-29）**：四层全部落地。`tests/test_install_contract.py`（29 用例，离线）进常规 CI；`tests/real_install_test.sh` 真实安装两个 Agent，官方源与 npmmirror 双路径实测通过，进 RC workflow；`scripts/agent_e2e_smoke.py` 需真实 Key，手动运行；integrity 校验已在 `install_locked_agent()` 生效。`installer.py` 保持 274/274 分支、零 partial。

两个结论：

**现有验证设施覆盖不到这条链路。** 不是测试写得不够多，而是所有既有测试都绕开了「真的装包」和「真的连服务商」这两步。已手动补跑（第 4 节），装得上，但结论未固化成可重复的脚本。

**国内网络下需要授权镜像，这已落在产品边界内。** 镜像与官方源的 integrity 逐字节相同，属「同包不同渠道」而非绕过网络限制，详见 3.5 节。

## 1. 现有设施实际验了什么

| 设施 | 真装包 | 真连 Provider | 实际覆盖 |
| --- | --- | --- | --- |
| `tests/test_core.py` 等 162 个 Python 用例 | 否，`Runtime.runner` 被替换 | 否 | 配置文件内容、错误码、权限 |
| `tests/install_test.sh` (263 行) | 否，全程 `--skip-test` | 否 | CLI 参数契约、退出码 |
| `tests/macos_cleanroom_test.sh` (301 行) | **否** | 否，指向 `127.0.0.1:9` | 干净 HOME 下写配置、文件权限 0600/0700、密钥不落盘 |
| `scripts/run_container_cleanroom.sh` | 否 | 否 | Linux 断网环境下的策略扫描 |
| `scripts/verify_locked_agents.py`（RC workflow 调用） | **是** | 否 | 五个 Agent 全部真实安装、隔离前缀、版本断言 |
| `scripts/provider_rc_smoke.py` | 不适用 | **是** | Provider 端点可达性 |

**更正**：先前记录的「没有任何一层真的装包」不成立。`scripts/verify_locked_agents.py` 一直在真实安装：`install_agent=True`、`locked_version=True`、隔离的 npm 前缀与 uv 目录、剔除带 `KEY`/`TOKEN`/`SECRET` 的环境变量，装完还用 `installed_version()` 断言版本相符（`:113`），且覆盖全部五个自动配置 Agent。它由 RC workflow 的 `Install and verify all locked Agents` 步骤调用。**真实安装本来就有覆盖，缺的是它之后的两步。**

实际的三个缺口：

**macOS cleanroom 链接了真实 npm 却从不用它。** `tests/macos_cleanroom_test.sh:40` 把真实 `npm` 链进干净 PATH，看起来具备安装能力，但 `:235-244` 的循环只传 `--provider/--model/--skip-test`，走纯配置路径。它验证「配置写得对」，不验证「Agent 装得上」——后者由 `verify_locked_agents.py` 在另一处覆盖。

**可执行文件是否落到 PATH 无人断言。** `verify_locked_agents.py` 用注入的 `isolated_which` 在自己构造的 PATH 里查找，验证的是「OneAgent 认为它装好了」。真实机器上 npm 全局前缀的差异是最常见的故障点，而这一步没有独立验证。

**`integrity` 记录了但从不校验。** `agents.lock.json` 为两个 Agent 都记了 `sha512-`，`tests/test_release_policy.py:36` 也断言它存在，但 `install_locked_agent()` 全函数不出现 `integrity`，只执行 `npm install -g <name>@<version>`。**版本锁住了，字节没有。**

## 2. 待验证的链路

空白机器到「Agent 能回答一个请求」，中间有六个环节，每一个都可能独立失败：

1. **前置条件** —— Node.js 是否存在、版本是否够；Windows 上 Claude Code 还要求 `git`（lock 里的 `windows_prerequisites`）。
2. **装包** —— `npm install -g @openai/codex@0.145.0` 与 `@anthropic-ai/claude-code@2.1.217` 在真实 registry 上是否可解析、可安装。
3. **可执行文件落到 PATH** —— 装完之后 `runtime.which("codex")` / `which("claude")` 是否真的找得到。npm 全局前缀在不同机器上差异很大，这一步是最常见的实际故障点。
4. **版本一致** —— `codex --version` 报出的是否就是锁定的版本。
5. **写配置** —— 这一环现有 cleanroom 已覆盖，且是唯一被覆盖的。
6. **Agent 真的能用** —— 带着写好的配置和真实 Key，Codex 和 Claude Code 能否各自完成一次请求。这是「可用」的唯一判据，也是目前完全空白的一环。

环节 3 与 6 是这次要补的重点：前者决定装完能不能被调用，后者决定配置写对了是否等于能用。

## 3. 检查计划

分三层，按成本和可重复性从低到高。第一层进常规 CI，第二三层手动或按发行触发——**真实装包与真实 Key 不能进常规 CI**，那会让每次提交都打 registry 和 Provider。

### 第一层：装包契约（进常规 CI，无网络）

补 `tests/test_install_contract.py`，用替换过的 `Runtime.runner` 断言即将执行的命令，不真的执行：

- Codex 与 Claude Code 的安装命令恰好是 `npm install -g <name>@<locked>`，版本取自 lock 而非硬编码。
- `--latest` 未指定时命令里必须带 `@<version>`；这是版本锁定的直接体现。
- 已装且版本相符时不重复安装（`install_locked_agent` 的短路分支）。
- 已装但版本落后时会重装。
- Node.js 缺失时报 `PREREQUISITE_MISSING` 而不是让 npm 自己失败。
- Windows 平台缺 `git` 时 Claude Code 报 `PREREQUISITE_MISSING`（用 `Runtime(os_id="windows")` 模拟）。

这一层保证命令构造正确，能在几毫秒内跑完，且不依赖外网。

### 第二层：真实安装 cleanroom（手动 / 发行前）

新增 `tests/real_install_test.sh`，与 macOS cleanroom 同样的隔离方式（干净 `HOME`、`env -i`、前后快照真实 HOME 确认零污染），但**真的装包**：

1. 断言起点为空：`codex` 与 `claude` 都不在 PATH 上，干净 HOME 下无 `.codex` / `.claude`。
2. 用真实 npm 安装两个 Agent 的锁定版本，npm 全局前缀指向干净 HOME 内的临时目录（避免污染真实机器）。
3. **断言可执行文件确实出现在 PATH 上**——环节 3，现有测试完全没有覆盖。
4. 断言 `codex --version` 与 `claude --version` 报出锁定版本，不是别的版本。
5. 断言 `/api/status` 把两者报成 `installed: true` 且版本相符（走真实 HTTP，与 `gui_smoke_test.py` 同样方式）。
6. 断言真实 HOME 前后快照一致——这是 macOS cleanroom 已有的做法，直接沿用。

只装这两个 Agent，不碰另外三个。默认不在 CI 跑；给 `release-candidate.yml` 加一个显式 job，并把那个名不副实的 `native-build-and-agent-install` 改成实际执行安装，或改名为它真正做的事。

### 第三层：端到端可用性（手动，需真实 Key）

新增 `scripts/agent_e2e_smoke.py`，在第二层基础上加真实 Provider：

1. 用真实 Key 走完整 `install_many`（不带 `--skip-test`），让协议探测真实执行——Codex 走 Responses，Claude Code 走 Anthropic Messages，两者协议不同，这正是需要分别验证的原因。
2. 读回写好的配置，确认 Codex 的 `config.toml` 里 `env_key` 指向的环境变量文件存在且含 Key，Claude Code 的 `settings.json` 里四个 `ANTHROPIC_*` 变量齐全。
3. **实际调用两个 Agent 各完成一次最小请求**，断言退出码为 0 且有输出。这是唯一能证明「可用」的一步。
4. 断言全过程的日志里不出现 Key 明文（`redact` 生效），沿用 macOS cleanroom 的 `grep -R -Fq` 手法。

只手动运行，需要 `ONEAGENT_API_KEY` 与一个可用 Provider。产出写进 `docs/release-evidence/`，与现有 cleanroom 证据同格式。

## 3.5 安装源可达性：授权镜像（已核实可行）

上面第 2 节的环节 2 假设 `registry.npmjs.org` 可达。在国内网络下这个假设经常不成立，而这恰好是 [产品边界基线](product-boundary-baseline.md) 已经预留了答案的场景。

### 为什么这不是「绕过网络限制」

基线第 5 节的软件获取策略把「授权镜像」列为优先级 2，条件是**有许可证、版本锁定、校验值和上游地址**；第 3.2 节允许「许可证允许的开源软件镜像」与「同包镜像」，同时第 4 节禁止「翻墙下载」「免代理访问受限网站」和「把 OneAgent 的服务器作为中转代理」。

分界线在于：**换 registry 是换取包的渠道，不是代理用户的网络。** OneAgent 不建隧道、不转发流量、不接触用户与镜像之间的连接，只是把 npm 的下载地址指向另一个同样公开可达的地址。这与基线允许「网盘和企业云盘上的同包镜像」是同一性质。

已核实的关键事实（2026-07-29）：

```
@openai/codex@0.145.0
  官方 dist.integrity  sha512-/PSPSFujjjmiyVFvG2yu/grOFhsWdokTH8t2KGWhXSo/M5n/dIDsnbsnO82/7bLtIoDuzQf7ATBUMWqPWQINlQ==
  镜像 dist.integrity  sha512-/PSPSFujjjmiyVFvG2yu/grOFhsWdokTH8t2KGWhXSo/M5n/dIDsnbsnO82/7bLtIoDuzQf7ATBUMWqPWQINlQ==

@anthropic-ai/claude-code@2.1.217
  官方 dist.integrity  sha512-EIcc3GmI7x+qPlKCjpcLIjCh7YOaCFbOqKfL4BmwZS6QmtduVNT5E98oyr8n2cxsgeWVbnQ0mSVljTw5C/kFtA==
  镜像 dist.integrity  sha512-EIcc3GmI7x+qPlKCjpcLIjCh7YOaCFbOqKfL4BmwZS6QmtduVNT5E98oyr8n2cxsgeWVbnQ0mSVljTw5C/kFtA==
```

两者**逐字节相同，且正好等于 `agents.lock.json` 里已记录的 `integrity`**。所以这不是「换了个源装到不同的东西」，而是同一份包的另一个渠道——基线第 3.2 节要求的「同一版本的所有渠道必须使用相同产物和相同 SHA-256」天然满足。

实测从镜像安装两个 Agent 共 4 秒完成，版本报告 `codex-cli 0.145.0` 与 `2.1.217 (Claude Code)`，与锁定一致。

### Claude Code 的许可证需要单独说明

`agents.lock.json` 记录 Claude Code 的 license 是 **Proprietary**，而基线第 4 节禁止「未经许可重新分发商业 Agent 包体」。这一条**不构成障碍，但理由必须写准**：

我们不重新分发任何包体。npmmirror 是上游 registry 的公开只读镜像，包由版权方自己发布到 npm；OneAgent 只是让用户的 npm 从哪个地址取包。禁止的是我们自己托管、重打包或再分发——那需要授权，而指向一个公开镜像不需要。

**因此实现上有一条硬性约束：绝不把 Agent 包体放进 OneAgent 自己的任何渠道**（发行包、对象存储、网盘）。镜像只能是第三方公开 registry，不能是我们运营的存储。

### 实现方式

落点很干净，因为 `Runtime.env` 已经是可注入字段，且 `install_locked_agent()` 已经把 `env=runtime.env` 传给 runner。所以不需要改命令构造，只需要在 env 里设 `npm_config_registry`：

- `catalog.py` 增一个 `PACKAGE_MIRRORS` 常量，声明可选镜像及其上游地址与用途说明。每个条目必须带上游 registry 地址，满足基线「有上游地址」的要求。
- 新增 `--registry <url>` CLI 参数与对应的请求字段；**默认保持官方源**，镜像永远是用户显式选择的结果，不做自动探测切换。这一点重要：自动切换会让用户不知道包从哪来。
- 只接受 HTTPS，且校验 URL 形态（复用 `providers.py` 里 base URL 校验的同类做法），拒绝 `http://` 和畸形值。
- 安装日志里记录实际使用的 registry。用户必须能事后知道包是从哪个地址来的。
- 前端在高级项里提供镜像选择，收起时说明「默认使用官方源，网络不可达时可选国内镜像」。

### 与 integrity 校验的关系

**镜像使这一层从可选变成必要。** 官方源下 integrity 只是「记而不验」的落差；一旦允许第三方镜像，「同包」就从 npm 的信任模型变成了我们自己的声明。`npm install` 会用 registry 自己返回的 integrity 校验下载，但那是镜像说什么就信什么。

所以引入镜像的同时，`install_locked_agent()` 应当把 `agents.lock.json` 里记录的 integrity 与实际安装的包核对——这是把「授权镜像」从口头承诺变成可执行检查的唯一方式，也正是基线第 5 节要求镜像必须有「校验值」的本意。

### 测试

- `PACKAGE_MIRRORS` 每个条目都有 HTTPS 地址、上游地址和说明（同 `test_release_policy.py` 对 lock 的断言风格）。
- `--registry` 未指定时，env 里不出现 `npm_config_registry`——默认行为不变。
- 指定时 env 正确携带，且命令构造不因此改变（版本锁定不受影响）。
- `http://` 与畸形 URL 被拒。
- 安装日志包含实际 registry，且不含任何密钥。
- 第二层的真实安装脚本增加一轮镜像安装，断言装出的版本与官方源一致。

### 不做

- **不自动探测网络并切换源。** 用户显式选择，否则包的来源变成隐式行为。
- **不自建镜像，不把 Agent 包体放进 OneAgent 的任何渠道。** 见上面的许可证说明。
- **不为 registry 做代理、隧道或任何形式的流量转发。** 基线第 4 节明确禁止，且这与换 registry 是完全不同的两件事。
- **不因为镜像可用就放宽版本锁定。** 镜像上取不到锁定版本时应当报「安装源不可达」，而不是退到别的版本——基线第 5 节最后一段已经规定了这个行为。

### 第四层：integrity 校验

`integrity` 目前记而不验：`agents.lock.json` 为两个 Agent 都记了 `sha512-`，`tests/test_release_policy.py:36` 断言它存在，但 `install_locked_agent()` 全函数不出现 `integrity`。**版本锁住了，字节没锁。**

只用官方源时这是一个可以接受的落差——npm 自己会用 registry 返回的 integrity 校验下载，信任链落在 npm 身上。**但一旦引入第三方镜像（3.5 节），这一层就从可选变成必要**：镜像返回的 integrity 是镜像自己说的，我们凭什么相信它与官方一致？答案只能是拿 lock 里记录的值去核对。

所以优先级取决于是否实施 3.5：
- 只做前三层、不引入镜像 —— 第四层可以留作已知落差，记录在案即可。
- 实施镜像 —— 第四层必须同时落地，否则「授权镜像」缺了基线第 5 节要求的「校验值」这一条件。

## 4. 现状确认（已执行，2026-07-29）

在写任何新测试之前先手动跑了一遍，用隔离的 `npm_config_prefix` 与干净 `HOME`，不污染本机。环节 1–5 全部通过：

| 环节 | 结果 |
| --- | --- |
| 1 前置条件 | Node v22.23.1 / npm 10.9.8 |
| 2 装包 | 两个锁定版本在 registry 上均存在，`npm install -g` 各 2–3 秒完成 |
| 3 落到 PATH | `codex` 与 `claude` 都出现在前缀的 `bin/` 下，可调用 |
| 4 版本一致 | `codex-cli 0.145.0`、`2.1.217 (Claude Code)`，与锁定完全相符 |
| 5 写配置 | 两个 Agent 的配置均落地，权限 0600；`config.toml` 的 `env_key` 正确指向 `ONEAGENT_API_KEY_CODEX`；`profile.json` 无明文密钥 |

同时确认 `status_payload()` 在隔离环境中把两者都报成 `installed: true` 且版本匹配——**OneAgent 对真实安装的检测是准的**，此前只有替换过 `runner` 的单元测试覆盖这一点。

**所以「装得上」不再是假设，锁定版本无需调整。** 剩下唯一未验证的是环节 6（带真实 Key 让两个 Agent 各完成一次请求），需要可用的 Provider 凭据，属第三层。

这次是手动执行、结论未固化。第二层的价值就是把上面这张表变成可重复运行的脚本，避免下次锁定版本变更后又要靠手工确认。

## 5. 明确不做

- **不在常规 CI 里真实装包或连 Provider。** 每次提交打 registry 与服务商既慢又会触发限流，也让 CI 结果依赖外部可用性。这也是现有 CI 用假 npm 的正当理由——问题不在于它用了假 npm，而在于**没有任何一层用真的**。
- **不验证另外三个 Agent。** 本轮范围就是 Codex 与 Claude Code。
- **不为 guide-only Agent 做安装验证。** 它们按设计不由 OneAgent 安装。
- **不改 `agents.lock.json` 的锁定版本**，除非第 4 节的现状确认表明当前版本已装不上。

相关文档：[产品边界基线](product-boundary-baseline.md)、[三平台 Python 内核与版本锁定 ADR](decisions/ADR-003-three-platform-python-core-and-release-policy.md)。
