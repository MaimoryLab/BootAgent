# 多页绿色启动器 MVP 计划

## 目标

做出最小可用的绿色启动器：不重新分发 Agent 包体，用一个本地浏览器多页向导引导用户检测、安装和配置常用 Agent。

MVP 的成功标准是：新用户打开本地 GUI 后，可以选择一个或多个 Agent，决定是否配置模型服务，完成 API Key 与模型 ID 初始化，并看到下一步启动命令。

当前实现入口：

- GUI 启动器：[scripts/gui.py](/Users/ppio/Documents/OneAgent/scripts/gui.py)
- CLI 安装内核：[scripts/install.sh](/Users/ppio/Documents/OneAgent/scripts/install.sh)
- CLI 测试：[tests/install_test.sh](/Users/ppio/Documents/OneAgent/tests/install_test.sh)
- GUI 冒烟测试：[tests/gui_smoke_test.py](/Users/ppio/Documents/OneAgent/tests/gui_smoke_test.py)
- 使用说明：[README.md](/Users/ppio/Documents/OneAgent/README.md)

## 产品边界

- 这是绿色启动器，不是 Agent 包体合集。
- 不打包、不修改、不重新分发 Claude Code 或 Codex。
- 不打包、不修改、不重新分发任何上游 Agent。
- 缺少 Agent 时默认只提示官方安装命令。
- 用户显式选择后，才对 allowlist 内 CLI Agent 调用包管理器安装。
- 模型服务配置可跳过，跳过时不要求 API Key，也不写 `~/.oneagent/env`。
- 默认推荐 PPIO 和 Novita，同时保留 Custom OpenAI-compatible base URL。
- API Key 只通过本地页面输入或环境变量传给子进程，不放进 GUI 日志和命令行参数。
- OpenClaw、Hermes、Cursor、Kiro、Gemini CLI 和 IDE 扩展类 Agent 第一版只做官方引导，不写私有配置。

## MVP 流程

1. 用户运行 `python3 scripts/gui.py`。
2. 本地服务只监听 `127.0.0.1`，并打开浏览器页面。
3. 用户从分组目录里多选要处理的 Agent。
4. 页面展示每个 Agent 的本机安装状态。
5. 用户选择配置模型服务，或跳过配置并使用官方账号/已有本地配置。
6. 如果跳过配置，直接进入确认页，只做 Agent 检测或可选官方安装。
7. 如果配置模型服务，用户选择 PPIO、Novita 或 Custom。
8. 用户填写 API Key；没有 Key 时可打开对应官网注册或获取 Key。
9. 页面用当前 base URL + API Key 请求 `GET <base-url>/v1/models`。
10. 模型列表成功时默认选择第一个模型 ID；失败时允许手动输入，默认值为 `gpt-4.1`。
11. 确认页展示 Agent 列表、Provider、base URL、模型 ID、写入路径和备份策略。
12. GUI 按 Agent 循环调用 CLI 安装内核。
13. 完成页展示每个 Agent 的结果和下一步命令。

## 当前实现

### GUI

- `scripts/gui.py` 使用 Python 标准库 HTTP server。
- 页面是内嵌 HTML/CSS/JS，不引入 React、Vite、Electron 或 Tauri。
- API 包括：
  - `GET /api/status`
  - `POST /api/probe`
  - `POST /api/models`
  - `POST /api/install`
  - `POST /api/open-register`
- 多选 Agent 时，Python 层循环调用 `scripts/install.sh`，CLI 仍保持单 Agent。
- `AGENT_CATALOG` 是 GUI 和 API 共用的 Agent 元数据源。
- Guide-only Agent 不调用安装内核，只返回官方安装/配置指引。

### CLI

- `scripts/install.sh` 支持 `--agent codex|claude-code|opencode|kilo-cli|aider`。
- Provider 支持 `ppio|novita|custom`。
- `--install-agent` 才会调用 allowlist 内包管理器安装源。
- `--check-agent-only` 只检测或安装 Agent，不写模型配置。
- 写配置前会备份旧文件。

### Agent 分类

可一键配置：

- Codex
- Claude Code
- OpenCode
- Kilo CLI
- Aider

只做引导：

- OpenClaw、Hermes
- Cursor、Kiro、Gemini CLI
- Cline、Continue、Qwen Code、Kilo VS Code

### Provider 默认值

PPIO：

- 官网：`https://ppio.com/`
- Base URL：`https://api.ppio.com/openai`
- Chat 请求：`https://api.ppio.com/openai/v1/chat/completions`

Novita：

- 官网：`https://novita.ai/`
- Base URL：`https://api.novita.ai/openai`
- Chat 请求：`https://api.novita.ai/openai/v1/chat/completions`

## 配置写入策略

### Codex

写入：

- `~/.codex/config.toml`
- `~/.oneagent/env`

完成后提示：

```bash
source ~/.oneagent/env && codex
```

### Claude Code

写入：

- `~/.claude/settings.json`

字段：

- `ANTHROPIC_BASE_URL`
- `ANTHROPIC_AUTH_TOKEN`
- `ANTHROPIC_MODEL`

完成后提示：

```bash
claude
```

### OpenCode / Kilo CLI

写入：

- `~/.config/opencode/opencode.jsonc`
- `~/.config/kilo/kilo.jsonc`
- `~/.oneagent/env`

配置使用 `@ai-sdk/openai-compatible`，Base URL 写成 `<api-base-url>/v1`。

### Aider

写入：

- `~/.oneagent/aider.env`

完成后提示：

```bash
source ~/.oneagent/aider.env && aider --model openai/<model>
```

## 验收标准

- [ ] 首屏展示 Agent 分组目录和安装状态。
- [ ] 可以跨分类多选 Agent。
- [ ] 可一键配置 Agent 能写入本地配置。
- [ ] Guide-only Agent 不写本地配置，只返回指引。
- [ ] 可以跳过模型服务配置，并且不要求 API Key。
- [ ] 跳过配置时不写 `~/.oneagent/env`。
- [ ] PPIO 和 Novita 显示正确官网和 base URL。
- [ ] Custom 允许填写自定义 base URL。
- [ ] 可以通过 `GET <base-url>/v1/models` 获取模型 ID。
- [ ] 模型列表失败时可以手动输入模型 ID。
- [ ] API Key 不出现在 GUI 日志或子进程命令行参数中。
- [ ] CLI 仍可独立运行。
- [ ] README 不推荐日常使用明文 `--api-key` 参数。

## 验证方式

自动测试：

```bash
./tests/install_test.sh
python3 tests/gui_smoke_test.py
bash -n scripts/install.sh tests/install_test.sh
python3 -m py_compile scripts/gui.py tests/gui_smoke_test.py
```

手动验收：

1. 运行 `python3 scripts/gui.py`。
2. 选择 Codex + OpenCode + OpenClaw。
3. 选择跳过配置，确认可以直接进入最终确认页。
4. 选择 PPIO 或 Novita，确认无 Key 时能打开官网。
5. 使用 mock key 测试连接，确认 401/403 被解释为端点可达但 Key 被拒绝。
6. 模型列表失败时，确认仍可手动输入模型 ID 并继续。

## 暂不做

- Windows 原生安装器。
- Electron / Tauri 桌面包。
- 重新分发 Agent 包体。
- 为每个 Agent 单独配置不同 Provider 或模型。
- 企业 SSO。
- 团队管理和账单。
- Agent 自动更新。
- 复杂模型路由界面。

## 后续方向

1. 将注册页参数和官网归因数据打通。
2. 增加更完整的本地诊断报告。
3. 支持设备码登录，减少手动粘贴 Key。
4. 在有稳定激活数据后，再考虑桌面安装包。
5. 多个 Agent 跑通后，再考虑 Agent Hub。
