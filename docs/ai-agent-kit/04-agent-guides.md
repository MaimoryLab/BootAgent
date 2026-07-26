# Agent 分类和安装指引

## 可一键配置

### Codex

- 首选官方安装源。
- OneAgent 写入 `~/.codex/config.toml`。
- Key 通过 `~/.oneagent/env` 或系统环境变量提供。
- 修改配置后重新打开终端运行。

### Claude Code

- 首选官方安装源。
- OneAgent 写入 `~/.claude/settings.json`。
- 通过 `ANTHROPIC_BASE_URL`、`ANTHROPIC_AUTH_TOKEN` 和模型字段配置。
- 修改配置后重新启动 Claude Code。

### OpenCode

- 使用 OpenAI-compatible Provider。
- Base URL 使用 `https://api.ppio.com/openai/v1` 形式。
- API Key 使用环境变量引用，不写入 JSON 明文。

### Aider

- 自动安装使用 `uv tool` 和本机已有的 Python 3.12，不使用系统级 pip。
- 缺少 `uv` 或 Python 3.12 时先完成官方前置条件安装；OneAgent 不自动下载 Python。
- 使用独立环境文件保存 PPIO 配置。
- 启动前加载环境文件。
- 使用 `openai/<model>` 形式时，以 Aider 当前版本说明为准。

## Gateway 型 Agent

### OpenClaw

OneAgent V1 只做安装检测和官方流程说明，不默认执行 Gateway daemon 安装、端口暴露或插件启用。

### Hermes

OneAgent 只提供安装和模型配置说明，不自动写入私有配置或启动 Gateway。

## 官方账号型 Agent

### Cursor、Kiro、Gemini CLI

这些工具优先使用各自官方账号、订阅或登录流程。不要把 PPIO Base URL 强行写进没有稳定官方配置契约的工具。

## IDE 扩展型 Agent

### Cline、Continue、Qwen Code、Kilo VS Code

优先在 IDE 扩展的 Provider 设置中配置 OpenAI-compatible 服务。OneAgent 提供可复制的字段说明，但不直接修改 IDE 私有状态文件。

## 统一排查顺序

```text
先测 PPIO API
→ 再确认模型 ID
→ 再确认 Agent 配置文件
→ 重启 Agent
→ 再测第一次请求
```
