# ADR-003：三平台 Python 安装核心与版本锁定发行策略

## Status

Accepted

## Date

2026-07-22

## Scope Update

版本锁定、配置适配、结构化错误、三平台适配设计和 Windows 用原生用户目录的决策继续有效。当前渠道分发范围和发行门禁已由 [ADR-005](ADR-005-channel-neutral-distribution-and-compliance.md) 更新，不再以四平台同时发布作为当前产品阶段门槛。

**三项决策已由 [ADR-008](ADR-008-go-core-and-wails-desktop-shell.md) 取代**：Python 共用内核（改为 Go）、本地 HTTP 浏览器 GUI（改为 Wails IPC，生产进程不监听端口）、PyInstaller 打包（改为单个 Go 二进制）。下文「Electron 或 Tauri 桌面壳」一节含一处事实错误，已在该节就地标注。

## Context

OneAgent 的产品基线要求 macOS、Windows 和 Linux 在发布前分别验证，但早期实现以 Bash 安装脚本和浏览器 GUI 为中心，无法为 Windows 原生路径、ACL、PowerShell 环境文件和跨平台打包提供同一套行为契约。

Agent 的上游安装版本持续变化。默认安装 `latest` 会使测试、配置兼容性和发行包验收无法复现，也无法准确生成第三方许可证与版本清单。

React 前端需要稳定的状态、错误和安装结果 API。如果前端迁移早于安装核心和 API 契约冻结，页面会固化尚未闭合的后端语义。

## Decision

### 平台与架构（内核兼容设计，不是当前分发门槛）

- Python 核心保留 macOS、Windows 和 Linux 的路径、权限和前置条件适配设计；当前发行包只声明实际构建和验证过的目标环境。
- 不以 macOS、Windows 和 Linux 同时构建或验收作为当前产品阶段门槛；每个实际发布的平台仍须在对应操作系统原生构建，并有 `ci.yml` cleanroom 作业或 Release Candidate 流程的验收证据。
- 各平台最低目标：macOS 13+（arm64/x64）、Windows 10 22H2 / Windows 11（x64）、Ubuntu 22.04+ 或兼容 glibc 环境（x64）。产物声明的目标环境不得低于上述最低版本。
- 检测、版本校验、前置条件、安装编排、备份、配置合并、权限和环境摘要统一由 Python 核心实现。
- `scripts/install.sh` 和 `scripts/install.ps1` 只负责定位 Python 并转发参数；本地 GUI 直接调用同一个 Python 核心。
- Windows 使用 `%USERPROFILE%` 对应的原生用户目录，不写 WSL HOME，不自动调用 `wsl.exe`。

### Agent 范围

自动配置范围固定为五个 Agent：

1. Codex
2. Claude Code
3. OpenCode
4. Kilo CLI
5. Aider

其他 catalog Agent 保持 `guide-only`。OneAgent 不猜测或修改没有稳定公开配置合约的私有状态文件。

### 安装与版本

- 默认安装版本来自受版本控制的 `agents.lock.json`。
- 每个自动配置 Agent 必须显式声明包管理器、包名、版本、完整性信息（适用时）、版本检查命令、支持平台、配置适配器、官方来源和许可证地址；不能只依赖 Agent ID 的隐式分支。
- 只允许 manifest 中声明的 npm 或 uv tool 安装命令，subprocess 必须使用参数数组且禁止 `shell=True`。Aider 使用隔离的 uv tool 环境，不调用系统级 pip。
- `--latest` 是用户显式选择的高级选项，不进入默认流程、PR 测试或发行验收。
- 缺少 npm、uv、Python 3.12、Git for Windows 等前置条件时返回 `PREREQUISITE_MISSING`。Aider 安装固定传入 `--no-python-downloads`，不自动安装语言运行时或 Git Bash。

### 密钥与本地状态

- API Key 只写入对应的本地密钥文件或 Agent 配置；Unix 目录使用 `0700`、文件使用 `0600`，Windows ACL 只允许当前用户和 SYSTEM。
- 权限设置失败时配置失败，不静默降级。
- Key 不进入命令行、URL、日志、环境摘要、React reducer、测试报告或遥测。
- `~/.oneagent/profile.json` 只保存 schema version、Provider、Base URL、模型、Agent、配置模式和激活时间。

### Provider 协议

- PPIO/Novita 的 OpenAI-compatible base 用于模型列表、Chat Completions、OpenCode、Kilo CLI 和 Aider。
- Claude Code 对内置 Provider 使用各自公开的 Anthropic-compatible base；Custom 显式覆盖由用户负责协议兼容。
- Codex 当前配置使用 Responses 协议。Chat Completions 探测成功不能证明 Codex 可用，必须在 Release Candidate 中测试 `/v1/responses` 和真实 Codex 首次请求。
- 如果某个内置 Provider 不支持 Agent 当前所需协议，该组合必须明确降级为不支持或 `guide-only`。V1 不通过本地协议网关隐藏兼容性缺口。

### API 与前端门槛

- `/api/status` 只做向后兼容的字段追加；错误响应保留 `error/message/status` 语义并增加稳定 `error_code` 与 `retryable`。
- `/api/install` V1 保持同步，前端只显示不定进度和最终逐 Agent 结果。
- 所有 POST 必须同时通过随机 HttpOnly 会话 Cookie 与 localhost Origin 校验。
- React 七页流程只能建立在 Python 核心、三平台适配和 API contract tests 全绿的冻结契约上。

### 发行

- 默认发布源码 ZIP 与 PyInstaller onedir 压缩包，不发布单文件自解压版本。
- 每个产物必须明确记录实际构建环境、架构和验证范围；未验证的平台不得出现在兼容性承诺中。
- GitHub、官网、网盘和企业云盘只作为同一官方构建的镜像渠道，不维护渠道专属包体。
- 未签名产物只能标记为 `technical-preview-unsigned`。
- Stable 额外要求 macOS 签名/公证和 Windows Authenticode，由 `scripts/build_release.py` 对产物本身做签名验证强制（环境变量不构成证据）。该门槛未被任何决策取代；当前阶段只是不发布 Stable。
- 每个产物必须附 SHA-256、锁定版本清单和第三方许可证清单。
- `release-candidate.yml` 的定义门禁是真实验收：四平台真实安装五个锁定 Agent，并用受保护 Provider Key 执行真实协议冒烟。在 CI Secret 配置完成前它尚未运行；常规 CI 以包体完整性、临时 HOME 启动、许可证、secret 扫描和本地 Mock 流程为门禁。

## Alternatives Considered

### 继续扩展 Bash，并为 Windows 单独维护 PowerShell 实现

- 优点：短期改动较少。
- 缺点：核心逻辑会形成两套实现，备份、权限、错误码和配置合并容易漂移。
- 结论：拒绝。

### 先完成 React，再逐步替换后端

- 优点：更早看到新界面。
- 缺点：前端会依赖未冻结的安装语义，导致重复重构和错误状态映射。
- 结论：拒绝。

### 默认安装上游 latest

- 优点：用户总能获得最新功能。
- 缺点：测试不可复现，可能在无代码变化时产生安装回归，也无法准确审核许可证和版本。
- 结论：拒绝；保留显式 `--latest`。

### Electron 或 Tauri 桌面壳

- 优点：更接近原生桌面分发。
- 缺点：引入额外运行时、签名面和构建复杂度，当前本地浏览器 GUI 已能满足流程需求。
- 结论：V1 不采用。

> **已由 [ADR-008](ADR-008-go-core-and-wails-desktop-shell.md) 推翻，并修正其中一处事实错误。**
>
> 「引入额外运行时」对 Electron 成立（打包 Chromium），对 Tauri 与 Wails 不成立——两者都使用系统 WebView，产物是单个二进制。本 ADR 决定的 PyInstaller 方案反而确实携带运行时（见下条）。
>
> 「当前本地浏览器 GUI 已能满足流程需求」这个前提在托盘常驻成为产品要求后不再成立：托盘需要原生窗口系统 API，进程生命周期不能绑在浏览器标签页上。

## Consequences

- 源码模式仍需要本机 Python；发布包通过 PyInstaller 携带运行时，不要求终端用户安装 Python。
- 安装核心和测试规模增加，但三平台行为可由同一契约验证。
- React 不直接执行命令或写配置，只消费本地 API。
- 每次更新锁定 Agent 版本都必须同步验证真实安装、版本输出、许可证和发行清单。
- 同一个模型 ID 跨 OpenAI、Anthropic 和 Responses 协议的可用性不能由 `/v1/models` 推断，必须进入 Provider/Agent 兼容性矩阵。
- WSL 管理、自动更新、后台服务、统一网关和遥测需要单独 ADR。
