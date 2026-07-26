# OneAgent

OneAgent 是一个本地 AI 开发环境激活器。它用 React 七页向导检测、安装并初始化常用 Agent，同时保留 Bash、PowerShell 和结构化 CLI。安装、备份、权限、Provider 探测和配置写入统一由 Python 3.12 核心完成。

OneAgent 不重新分发 Agent 二进制，不捆绑 Node.js、Python、Git Bash、VPN、代理、共享 API Key 或第三方配置工具。缺少前置工具时只返回明确错误和官方安装指引。

## 当前状态

当前版本为 `0.2.0-dev`，发行渠道只能标记为 `technical-preview-unsigned`。

截至 2026 年 7 月 24 日，本地已完成 macOS arm64 的源码、React、浏览器、PyInstaller onedir 和真实 macOS cleanroom 验证，并完成 Docker Linux arm64 的断网 cleanroom。macOS x64、Windows x64、Linux x64 由 GitHub Actions 在目标系统分别构建和验证；在四平台产物、真实 Agent 安装和真实 Provider 冒烟全部通过前，不应标记 Stable。

首发平台目标：

| 平台 | 架构 | 最低目标 |
| --- | --- | --- |
| macOS | arm64、x64 | macOS 13+ |
| Windows | x64 | Windows 10 22H2 / Windows 11 |
| Linux | x64 | Ubuntu 22.04+ 或兼容 glibc 环境 |

## 架构

```text
React + TypeScript + Vite
          |
          | localhost JSON API
          v
Python 3.12 HTTP Server
          |
          v
oneagent.installer
  - 平台路径和前置检测
  - 锁定版本安装
  - 配置合并和备份
  - Unix mode / Windows ACL
  - Provider 探测和结构化错误
```

- `scripts/gui.py`：源码 GUI 入口。
- `scripts/install.sh`：macOS/Linux CLI 转发层。
- `scripts/install.ps1`：Windows CLI 转发层。
- `oneagent/`：三平台共用安装核心、API Server 和 CLI。
- `frontend/`：React 七页向导；发行包只携带构建后的 `dist`，终端用户不需要 Node.js。
- `agents.lock.json`：五个自动配置 Agent 的版本、包管理器、配置适配器、平台、来源和许可证锁定清单。

## 快速启动

### 源码 GUI

源码运行要求 Python 3.12+。如需让 OneAgent 自动安装 Aider，还需要预先安装 `uv`；OneAgent 不会自动下载 Python：

```bash
python3 scripts/gui.py
```

固定端口且不自动打开浏览器：

```bash
python3 scripts/gui.py --port 8765 --no-open
```

GUI 只监听 `127.0.0.1`。首页设置随机 HttpOnly、SameSite=Strict 会话 Cookie，所有 POST 同时校验 Cookie 和 localhost Origin。

### 打包版

解压对应平台的 onedir 压缩包后运行：

```bash
./OneAgent/OneAgent
```

Windows：

```powershell
.\OneAgent\OneAgent.exe
```

未签名预览版不是 Stable。OneAgent 不提供绕过操作系统安全策略的指令。

### CLI

macOS/Linux：

```bash
ONEAGENT_API_KEY="$MY_API_KEY" \
./scripts/install.sh \
  --agent codex \
  --provider ppio \
  --model your-model-id \
  --channel direct
```

Windows PowerShell：

```powershell
$env:ONEAGENT_API_KEY = $MyApiKey
.\scripts\install.ps1 --agent codex --provider ppio --model your-model-id
```

`--api-key` 仅为旧参数兼容和受控测试保留。日常使用应通过 GUI、交互粘贴或 `ONEAGENT_API_KEY` 传入，避免进入 shell history。

只检测或安装 Agent、不写模型配置：

```bash
./scripts/install.sh --agent codex --check-agent-only
```

显式安装锁定版本：

```bash
./scripts/install.sh --agent codex --check-agent-only --install-agent --locked-version
```

`--latest` 只能由用户显式选择，默认安装与发布测试均使用 `agents.lock.json` 的锁定版本。

## GUI 流程

1. 多选 Agent，并查看本机安装、配置和前置条件状态。
2. 选择配置模型服务，或使用官方账号/已有本地配置。
3. 选择 PPIO、Novita 或 Custom，填写 Key 并测试连接。
4. 请求 `GET <openai-base>/v1/models`；失败时手动输入模型 ID。
5. 确认 Agent、写入路径、备份策略和 guide-only 项目。
6. 同步执行安装与配置，按 Agent 返回最终状态；不伪造百分比。
7. 从不含 Key 的 `~/.oneagent/profile.json` 恢复环境总览。

## Provider

| Provider | 官网 | OpenAI-compatible base | Anthropic-compatible base |
| --- | --- | --- | --- |
| PPIO | `https://ppio.com/` | `https://api.ppio.com/openai` | `https://api.ppio.com/anthropic` |
| Novita | `https://novita.ai/` | `https://api.novita.ai/openai` | `https://api.novita.ai/anthropic` |

连接测试使用：

```text
POST <openai-base>/v1/chat/completions
GET  <openai-base>/v1/models
```

Custom 支持 HTTP/HTTPS，包括用户主动配置的本机地址；拒绝 URL 凭据、非法 scheme 和控制字符。推荐显式使用 `--provider custom --api-base-url ...`。为兼容旧 CLI，合法 Provider 也可以用 `--api-base-url` 做显式覆盖。

协议注意事项：

- Claude Code 使用 Provider 的 Anthropic-compatible base，并写入 `ANTHROPIC_BASE_URL`、`ANTHROPIC_AUTH_TOKEN`、`ANTHROPIC_MODEL` 和 `ANTHROPIC_SMALL_FAST_MODEL`。
- Codex 当前配置使用 Responses 协议。PPIO/Novita 的 `/v1/responses` 必须在 Release Candidate 中使用专用低权限 Key 做真实验收；仅验证 Chat Completions 不能证明 Codex 可用。
- 同一个模型 ID 不一定同时兼容 OpenAI、Anthropic 和 Responses 协议。真实 Agent 首次请求是发布门禁，不以 `/v1/models` 成功代替。

## Agent 范围

### 自动配置

| Agent | 锁定版本 | 安装器 | 配置协议 |
| --- | --- | --- | --- |
| Codex | `0.145.0` | npm | Responses |
| Claude Code | `2.1.217` | npm | Anthropic Messages |
| OpenCode | `1.18.4` | npm | OpenAI-compatible |
| Kilo CLI | `7.4.11` | npm | OpenAI-compatible |
| Aider | `0.86.2` | uv tool | OpenAI-compatible |

### 只做引导

- 网关型：OpenClaw、Hermes。
- 官方账号/平台型：Cursor、Kiro、Gemini CLI。
- IDE 扩展型：Cline、Continue、Qwen Code、Kilo VS Code。

guide-only Agent 不执行包管理器安装，不写私有配置，不启动 daemon、gateway、WSL 或后台服务。

Aider 使用隔离的 `uv tool install --python python3.12 --no-python-downloads`，不再调用系统级 pip。缺少 `uv` 或本机 Python 3.12 时返回 `PREREQUISITE_MISSING`，不会绕过 externally-managed Python，也不会自动安装语言运行时。

## 配置与备份

| Agent/状态 | 写入路径 |
| --- | --- |
| Codex | `~/.codex/config.toml`、`~/.oneagent/env` 或 Windows `env.ps1` |
| Claude Code | `~/.claude/settings.json` |
| OpenCode | `~/.config/opencode/opencode.jsonc`、共享 env |
| Kilo CLI | `~/.config/kilo/kilo.jsonc`、共享 env |
| Aider | `~/.oneagent/aider.env` 或 Windows `aider.ps1` |
| 环境摘要 | `~/.oneagent/profile.json` |

Codex TOML、Claude/OpenCode/Kilo JSON 会保留非 OneAgent 管理字段。写入前创建 `*.backup-<timestamp>`；损坏配置返回 `CONFIG_WRITE_FAILED`，不会静默覆盖。

API Key 只进入本地密钥配置，不进入 `profile.json`、命令行、URL、日志、React reducer、浏览器存储或遥测。Unix 私有目录使用 `0700`、密钥文件和备份使用 `0600`；Windows 关闭 ACL 继承，仅允许当前用户和 SYSTEM。权限设置失败会终止发布写入。

## 错误契约

CLI `--json` 和本地 API 使用稳定错误码：

- `INVALID_REQUEST`
- `INVALID_ORIGIN`
- `PREREQUISITE_MISSING`
- `API_KEY_REJECTED`
- `PROVIDER_UNREACHABLE`
- `MODELS_UNSUPPORTED`
- `AGENT_INSTALL_FAILED`
- `CONFIG_WRITE_FAILED`
- `TIMEOUT`

错误响应保留 `error`、`message`、`status`，并提供 `error_code` 和 `retryable`。

## 开发与测试

安装前端依赖并构建：

```bash
cd frontend
npm ci
npm run build
```

Python 3.12 契约和覆盖率：

```bash
python3.12 -m coverage run --branch -m unittest \
  tests.test_core tests.test_cli tests.test_server \
  tests.test_release_policy tests.test_edge_cases tests.test_rc_scripts
python3.12 -m coverage report --fail-under=85
python3.12 -m coverage json
python3.12 -c "import json; s=json.load(open('build/coverage/coverage.json'))['files']['oneagent/installer.py']['summary']; assert s['percent_branches_covered'] == 100 and s['num_partial_branches'] == 0"
```

兼容测试：

```bash
bash tests/install_test.sh
python3.12 tests/gui_smoke_test.py
```

React 与浏览器：

```bash
cd frontend
npm run test:coverage
npm run build
npx playwright install chromium
npm run e2e
```

### Docker Linux Cleanroom

本地 Docker cleanroom 会构建测试专用镜像，再以非 root 用户、全新 HOME 和 `--network none` 执行 Python、Bash、GUI、React、Chromium E2E 与发行策略扫描：

```bash
bash scripts/test_docker_cleanroom.sh
```

镜像构建阶段允许下载 apt、pip 和 npm 锁定依赖；正式测试容器不挂载源码、Docker Socket 或用户 HOME，只把结果写入 `build/docker-cleanroom/`。镜像不包含五个 Agent、`uv`、Provider Key 或用户配置，也不会上传到镜像仓库。

Docker Desktop 在 macOS 上仍运行 Linux VM。该报告固定标记为 `linux`，只能证明 Linux cleanroom，不能替代 Darwin、APFS、`stat -f`、macOS PyInstaller、签名或公证验证。

### 真实 macOS Cleanroom

当前架构的前端和 unsigned onedir 构建完成后，可以在真实 macOS 上运行：

```bash
ONEAGENT_PACKAGED_BINARY="$PWD/build/pyinstaller-dist/OneAgent/OneAgent" \
bash tests/macos_cleanroom_test.sh
```

脚本要求真实 `uname -s == Darwin`，使用 `env -i`、临时 HOME/TMPDIR 和受控 PATH，验证源码 GUI、打包 GUI、随机本地端口、Cookie/Origin、五个配置适配器、备份以及目录 `0700`/文件 `0600`。执行前后会比对真实用户配置目标，发现污染立即失败。

GitHub Actions 的 `macos-15` arm64 和 `macos-15-intel` x64 Runner 是 macOS cleanroom 的正式依据。普通 PR 与常规 CI 只使用 fake npm/uv，不下载真实 Agent，也不访问 PPIO/Novita；只有手动 Release Candidate 才在隔离 prefix/tool 目录中安装五个锁定版本并执行真实 Provider 冒烟。

覆盖门槛：安全、备份、配置写入、权限、脱敏和 manifest 校验逻辑要求 100% 分支覆盖；Python 核心与 React 状态/API 层整体分支覆盖不低于 85%。

## 发行

本机生成未签名预览包：

```bash
python3.12 -m pip install pyinstaller==6.21.0
python3.12 scripts/build_release.py \
  --channel technical-preview-unsigned \
  --source
python3.12 scripts/check_release.py release
```

产物包括：

- 当前平台 PyInstaller onedir ZIP。
- 可选源码 ZIP。
- `release-manifest-<platform>-<arch>.json`。
- `SHA256SUMS-<platform>-<arch>.txt`。
- 第三方许可证和五个 Agent 的锁定版本清单。

PyInstaller 必须在目标操作系统构建，不能从一个平台交叉生成四个平台。GitHub Actions 使用 `macos-15`、`macos-15-intel`、`windows-2022` 和 `ubuntu-22.04`。

`.github/workflows/release-candidate.yml` 为手动 RC 门禁：四平台真实安装五个锁定 Agent，并使用受保护的 `ONEAGENT_PPIO_API_KEY`、`ONEAGENT_NOVITA_API_KEY` 与对应三类协议模型变量执行低 token 请求。缺少任一 Key 或协议模型 ID 时流程会失败，不会退化成假通过。

在真实 PPIO/Novita 低权限 Key 尚未配置到受保护 CI Secret 前，可以使用 `apiproxy` 档案做本地三协议预检。该档案分别保留 OpenAI、Anthropic、Responses 三个模型槽位，当前统一使用 `openai/gpt-5.6-terra`；`openai/gpt-5.6-luna` 只支持两类协议，不作为三协议预检默认模型。

```bash
python3 scripts/provider_rc_smoke.py \
  --provider apiproxy \
  --api-key-json ~/.codex/auth.json \
  --api-key-field OPENAI_API_KEY \
  --timeout 45
```

此命令只读取本机 JSON 中的 Key，不会把 Key 放入命令行值或输出。`--provider all` 仍严格只运行 PPIO 和 Novita；代理预检成功不能替代正式 RC 验收。详细边界见 [Provider RC 测试说明](docs/provider-rc-testing.md)。

Stable 额外要求 macOS 签名/公证和 Windows Authenticode。未满足签名条件时只能发布明确标记的 `technical-preview-unsigned`。

## 文档

- [产品边界基线](docs/product-boundary-baseline.md)
- [三平台 Python 内核与版本锁定 ADR](docs/decisions/ADR-003-three-platform-python-core-and-release-policy.md)
- [React 前端实现与发布门禁](docs/frontend-component-redesign-plan.md)
- [Provider RC 测试说明](docs/provider-rc-testing.md)
- [用户使用文档](docs/ai-agent-kit/00-start-here.md)
- [配置工具选择](docs/ai-agent-kit/03-config-tools.md)
- [CC Switch 可选配置说明](docs/ai-agent-kit/tools/cc-switch.md)

CC Switch 仅为可选文档，不自动安装、不进入运行依赖，也不替代 Provider API 服务。
