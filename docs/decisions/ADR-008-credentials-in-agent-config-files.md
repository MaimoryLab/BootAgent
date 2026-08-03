# ADR-008：凭据写入 Agent 自己的配置文件

## Status

Implemented

## Date

2026-08-03

- Supersedes: ADR-006 的 `ONEAGENT_API_KEY_<AGENT>` + `~/.oneagent/agents/<id>.env` 凭据投递方案

## Context

ADR-006 修订版让每个 Agent 读自己的环境变量：`~/.oneagent/agents/<id>.env` 写 `ONEAGENT_API_KEY_<AGENT>`，Codex 的 `config.toml` 用 `env_key` 指向它，OpenCode / Kilo 的 JSON 用 `"apiKey": "{env:...}"` 引用它。这解决了三个 Agent 共用一个变量名的耦合，但保留了 env 文件本身的代价：

- 配置只在 sourced 过 env 文件的 shell 里生效。用户从 Dock、桌面快捷方式或已开着的终端启动 Agent 就会看到未认证错误，而 OneAgent 的界面显示"已配置"。
- 每个 Agent 的重启指引都得带一句 `source ~/.oneagent/agents/<id>.env`，桌面的 Launch 按钮也得把这句拼进终端命令，否则打开的窗口跑的是未配置的 Agent。
- 同一份 Key 落在两处（`secrets/<id>.env` 和 `agents/<id>.env`），加上 `~/.oneagent/env` 兼容层是三处。

三个 Agent 都有自己的凭据文件位置，且都是 OneAgent 已经在写的那个文件或它的同目录邻居。CC Switch（Tauri）走的正是这条路：Codex 写 `~/.codex/auth.json`，Claude 写 `settings.json` 的 `env` 块，OpenCode 写 `opencode.json` 的 `provider.<id>.options.apiKey`，没有任何 env 文件。

## Decision

凭据写进 Agent 自己的配置文件，删除全部 env 文件写入逻辑。

| Agent | 凭据位置 |
| --- | --- |
| Codex | `~/.codex/auth.json` 的 `OPENAI_API_KEY`，`auth_mode` 置为 `apikey` |
| Claude Code | `~/.claude/settings.json` 的 `env.ANTHROPIC_AUTH_TOKEN`（本来就是这样） |
| OpenCode | `~/.config/opencode/opencode.json` 的 `provider.oneagent.options.apiKey` |
| Kilo CLI | `~/.config/kilo/kilo.jsonc` 的同一位置 |
| Aider | `~/.oneagent/aider.env`（由 Aider 的 `--env-file` 直接加载） |

配套改动：

- Codex 的 `[model_providers.oneagent]` 去掉 `env_key`，加 `requires_openai_auth = true`，让 Codex 用 `auth.json` 里的 Key 认证托管 provider。`auth_mode` 必须显式写成 `apikey`：残留的 `chatgpt` 会让 Codex 优先用缓存的 OAuth token，把新 Key 忽略掉。
- `auth.json` 先写、`config.toml` 后写。指向一个认证不了的 provider 比留一个暂时没人引用的 Key 更糟。写入复用 `securefs.AtomicWrite`，`auth.json` 按 secret 处理（0600 / Windows ACL，备份同样收紧权限）。
- OpenCode / Kilo 的配置文件现在含明文 Key，因此按 secret 写入。
- OpenCode 的路径从 `opencode.jsonc` 改为 `opencode.json`：Key 进了这个文件之后它是 OneAgent 的主要写入目标，`.json` 是 OpenCode 自己的默认名。JSONC 注释检测不再看扩展名（OpenCode 用 JSON5 解析，`.json` 里也可能有注释），检测到就拒写并保留原文件。
- 删除：`internal/config/env.go`（`WriteAgentEnv` / `WriteSharedEnv` / `agentEnvVar`）、`agents.lock.json` 的 `credential_delivery` 字段、status 的 `paths.env_file` 与 `backups.env`、重启指引和 Launch 命令里的 `source` 前缀；Aider 改由 `--env-file` 加载唯一保留的环境文件。

## Alternatives Considered

### Codex 继续用 env_key，只改 OpenCode / Kilo

- 优点：不动 `auth.json`，不碰 Codex 的登录态。
- 缺点：Codex 是最主要的 Agent，留着 env 文件等于「不依赖环境变量」这个目标没达成，`source` 指引和 Launch 拼接逻辑都得留着。
- 结论：拒绝。

### `codex login --api-key` 代替直接写 auth.json

- 优点：用 Codex 官方入口，格式由它自己保证。
- 缺点：多一次子进程调用与超时处理，且它会覆盖整个 `auth.json`（丢掉用户的 OAuth 缓存）。直接 merge 只改两个键。
- 结论：拒绝。

## Consequences

- `auth_mode = "apikey"` 会让 Codex Desktop 把账号视作 API-Key 认证，ChatGPT 登录相关的功能（Fast mode 等）不可用。这是用托管 provider 的必然结果，CC Switch 也记录了同一现象；OneAgent 的定位就是把 Codex 指向第三方 Provider，所以接受。
- OpenCode / Kilo 的配置文件从「可以公开」变成「含密钥」，写入路径与权限断言随之收紧，任何未来的读取投影都不能回传这两个文件的原文。
- `~/.oneagent/env`、`~/.oneagent/agents/*.env` 不再写入。旧版本留下的文件不会被删除，但也不再被引用；用户手工清理。
- status 传输契约变更（`paths` 少一个键、`backups` 少一个键），已同步 `frontend/src/types/api.ts` 使用方与冻结的 status fixture。
