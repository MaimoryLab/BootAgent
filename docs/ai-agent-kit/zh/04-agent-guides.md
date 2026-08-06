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

OneAgent 安装 `openclaw` 包，并把 Provider 与默认模型写入 `~/.openclaw/openclaw.json` 的 `models.providers.oneagent`，同时把 `agents.defaults.model.primary` 指向 `oneagent/<model>`。

**只写这两处。** `channels`、`tools`、`agents.defaults` 的其他字段、以及你已有的其他 provider 都会原样保留 —— 这些是你通过 `openclaw onboard` 决定的内容，OneAgent 没有依据去改。

以下动作不在范围内，仍由 OpenClaw 自己的命令负责：

- 启动或停止 Gateway，注册 launchd / systemd 服务
- 配对聊天渠道（Discord、Telegram、WhatsApp 等）
- Control UI 的端口与访问控制
- 插件启用

配置完成后运行 `openclaw onboard` 完成渠道配对。改完配置需要让 Gateway 重读，运行 `openclaw gateway restart` —— 它是常驻进程，不像前台 CLI 那样重开一次就生效。

`openclaw.json` 是 JSON5，允许注释。如果你的文件里有注释，OneAgent 会拒绝写入并报错，而不是把注释丢掉。这种情况下按上面的字段手工填写。

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
