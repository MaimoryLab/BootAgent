# Agent 分类和安装指引

[English](../en/04-agent-guides.md) · **简体中文**

## 可一键配置

### Codex

- 首选官方安装源。
- OneAgent 写入 `~/.codex/config.toml`，Key 写入同目录的 `~/.codex/auth.json`。
- 不依赖环境变量；修改配置后重启 codex 即可。

### Claude Code

- 首选官方安装源。
- OneAgent 写入 `~/.claude/settings.json`。
- 通过 `ANTHROPIC_BASE_URL`、`ANTHROPIC_AUTH_TOKEN` 和模型字段配置。
- 修改配置后重新启动 Claude Code。

### OpenCode

- 使用 OpenAI-compatible Provider，OneAgent 写入 `~/.config/opencode/opencode.json`。
- Base URL 使用 `https://api.ppio.com/openai/v1` 形式。
- API Key 直接写入该文件的 `provider.oneagent.options.apiKey`，文件权限收紧到 0600，不依赖环境变量。

### Kilo CLI

- 使用 OpenAI-compatible Provider，OneAgent 写入 `~/.config/kilo/kilo.jsonc`。
- API Key 与 OpenCode 同样写在配置文件的 `options.apiKey`，权限收紧到 0600。

### Aider

- 只有选择 Aider 安装时才需要 Python 3.12，由 `uv` 自己解析：本机有匹配版本就复用，否则下载一份托管 CPython 到 `~/.oneagent/runtimes/python`。其他 Agent 和 OneAgent 自身不需要 Python。
- `uv` 本身由 OneAgent 作为运行时安装到 `~/.oneagent/runtimes`，不需要预装。
- 使用独立环境文件保存 PPIO 配置。
- 启动时通过 `aider --env-file ~/.oneagent/aider.env` 由 Aider 自己加载，不需要在 shell 中 source。
- 使用 `openai/<model>` 形式时，以 Aider 当前版本说明为准。

## Gateway 型 Agent

### OpenClaw

OneAgent V1 只做安装检测和官方流程说明，不默认执行 Gateway daemon 安装、端口暴露或插件启用。

### Hermes

OneAgent 只提供安装和模型配置说明，不自动写入私有配置或启动 Gateway。

## 官方账号型 Agent

### Cursor

优先使用官方账号、订阅或登录流程。不要把 Provider 的 Base URL 强行写进没有稳定官方配置契约的工具，OneAgent 也不会为它写私有配置。

## 其他工具

`agents.lock.json` 是 Agent 目录的唯一真源；上面没有的工具（各类 IDE 扩展、其他终端 Agent）OneAgent 不检测也不配置。这类工具通常在自己的 Provider 设置里填 OpenAI-compatible 端点，字段口径可以照 [第一次请求](./05-first-request.md) 的说明复制，但请在该工具自己的界面里操作——OneAgent 不修改 IDE 的私有状态文件。

## 统一排查顺序

```text
先测 PPIO API
→ 再确认模型 ID
→ 再确认 Agent 配置文件
→ 重启 Agent
→ 再测第一次请求
```
