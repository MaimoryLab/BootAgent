# 官方账号登录：让 Agent 切回自己的登录方式

> 状态：方案草稿，未实施。起草于 2026-10-12，代码核对至 `claude/compat-mode-flow` 工作区（基于 `3d335e0`）。

## 1. 结论

「切到官方登录」这一步本身确实只是改配置文件：

- **Codex**：在 `config.toml` 删掉 3 个键，在 `auth.json` 改 2 个键。
- **Claude Code**：删掉 `settings.json` 里 `env` 下的 3 个键。

难点不在写文件，而在以下三处：

1. **登录这一步改文件做不到。** ChatGPT 账号和 Claude 订阅都是浏览器 OAuth，凭据由 Agent 自己写入 `auth.json` 或系统钥匙串。BootAgent 能做的是：
   - 判断用户是否已经登录；
   - 没登录时打开终端执行官方的登录命令。
2. **Codex 的 API Key 和 ChatGPT 登录共用 `auth.json`。** BootAgent 现在写入 Key 的方式本身会改动登录状态。往回切时需要恢复登录状态，而且两种方式共存时有一个需要先核实的安全风险，见 §5。
3. **BootAgent 自己的状态还不认识「没有模型服务」这种情况。** 涉及 Agent 绑定、保存模型服务后的重新应用、配置识别、启动命令里的 `-m`、模型检测按钮，每一处都默认 Agent 背后有一个模型服务、一个 Key、一个模型。

## 2. CC Switch 的做法

参考：<https://github.com/farion1231/cc-switch>（README）。

- **内置官方预设**：Claude Code、Codex 等都有「Claude Official」「OpenAI Official」这类预设。切到它之后，需要用户重启 Agent，再走 Agent 自己的登录流程（Claude Code 的 `/login`、Codex 的 `codex login`）。CC Switch 不自己实现 OAuth。
- **只替换连接字段**：端点、Key、模型、协议（Codex 另加推理强度）。插件、hooks、MCP 等不动。
- **Codex 登录暂存**：CC Switch 切换时整份覆盖 `auth.json`。所以切到第三方时，它先把官方登录挪到 `codex-login-stash.json`，切回官方时再恢复。
- **Claude 官方**：没有暂存，因为 Claude Code 的登录凭据不在 `settings.json` 里。

BootAgent 写 `auth.json` 时只合并 `OPENAI_API_KEY` 和 `auth_mode` 两个键，ChatGPT 的 `tokens` 一直留在原处（`internal/config/write.go:215`，ADR-008 的取舍）。所以不需要暂存文件，切回时把这两个键改回来即可。

## 3. 各 Agent 怎么切

### 3.1 第一批：Codex、Claude Code

| | Codex（同时影响共用其配置的 ChatGPT 桌面版，`internal/desktopapp/registry.go:90`） | Claude Code |
| --- | --- | --- |
| BootAgent 现在写的 | `config.toml`：`model_provider = "bootagent"`、`model`、`model_reasoning_effort`、`[model_providers.bootagent]`（其中 `requires_openai_auth = true`），见 `write.go:173`、`:1048`<br>`auth.json`：`OPENAI_API_KEY`、`auth_mode = "apikey"`，见 `write.go:215` | `settings.json` 的 `env.ANTHROPIC_BASE_URL`、`env.ANTHROPIC_AUTH_TOKEN`、`env.ANTHROPIC_MODEL`，见 `write.go:227` |
| 切到官方要写的 | `config.toml`：删掉 3 个自有键和 `[model_providers.bootagent]` 表<br>`auth.json`：`auth_mode = "chatgpt"`，`OPENAI_API_KEY = null`，`tokens` 不动 | 删掉这 3 个 env 键，`env` 里其他键和文件里其他内容不动 |
| 登录凭据在哪 | 由 `cli_auth_credentials_store` 决定：`auth.json`（默认）或系统钥匙串 | macOS 钥匙串；Linux 和 Windows 上是 `~/.claude/.credentials.json`（Agent 自管，BootAgent 不读也不写） |
| 判断是否已登录（本机命令，不发计费请求） | `codex login status` | `claude auth status --json` |
| 登录命令（在终端里执行，会打开浏览器） | `codex login`；无浏览器时用 `codex login --device-auth` | `claude auth login`（默认使用 Claude 订阅，`--console` 改用 Console 计费） |
| 切换后还会覆盖官方登录的东西 | 待核实：shell 里的 `OPENAI_API_KEY`，以及管理员强制的 `forced_login_method` | shell 里的 `ANTHROPIC_API_KEY` / `ANTHROPIC_AUTH_TOKEN`，以及 `settings.json` 的 `apiKeyHelper`，会优先于订阅登录。BootAgent 能识别这些，但不应该去改 |

上表中的登录命令和子命令是在本机核对的：Codex CLI 0.153.4、Claude Code 2.1.223。凭据存储方式参考 Codex 官方文档（<https://learn.chatgpt.com/docs/auth>）。

### 3.2 第二批：需要先调研

- **Claude 桌面版**：BootAgent 写的是一个自有的第三方推理 profile（`internal/config/claude_desktop.go`）。切回官方应该是取消激活这个 profile，而不是删除它。需要先确认桌面版切回自身账号的开关在哪个文件里。
- **OpenCode、Kilo CLI**：这两个 Agent 都有自己的 `auth login`，可以登录多家服务，凭据存在各自的数据目录。「官方」对它们来说不是一个固定的选项，而是从它们自己已登录的服务里选一个。建议放在后面做，或者不做。
- **Kimi Code**：有 Kimi 账号登录。它和 Kimi Code 桌面版共用配置（`registry.go:154`）。需要先确认登录后的凭据和 `default_model` 是什么关系。
- **其他 Agent**（Aider、OpenClaw、Hermes、Pi、DeepSeek Harness）：没有统一的官方账号登录，这次不做。

## 4. BootAgent 里需要认识「官方登录」的地方

| 位置 | 现状 | 需要改成 |
| --- | --- | --- |
| `internal/app/agent.go:62` `activateAgentLocked` | 必须有 Key 和模型，否则报错；然后写入配置和绑定 | 新增一条独立的 `UseOfficialLogin(agentID)` 路径。不经过 Provider 解析，不要求 Key，也不检测模型 |
| `internal/profile/binding.go:113` | 绑定必须有 provider、base URL 和 model | 允许一种 `Auth: "official"` 的绑定，此时 base URL 和 model 可以为空，`Provider` 记为 `official` |
| `internal/app/provider.go:389` 重新应用 | 按 provider 或 profile 匹配，然后改写 Agent | 天然不会选中 `official` 绑定，但要加测试固定这一点，防止以后有人建了一个 ID 叫 `official` 的模型服务 |
| `internal/config/discovery.go:24`、`:42` | Codex 以有无 `[model_providers.bootagent]` 判断是否由 BootAgent 管理；Claude 以 3 个 env 键是否都有值判断 | 增加 `Official` 判断：<br>• Codex：没有 `model_provider`，且 `auth_mode` 不是 `apikey`<br>• Claude：没有这 3 个键<br>只返回布尔值，不返回任何凭据内容 |
| `internal/app/launch.go:57` | 从绑定读取模型，拼进启动命令 | 官方登录的绑定没有模型，命令里不带 `-m`，由 Agent 用自己的默认模型 |
| 前端：总览的 Agent 行、Agent 配置页 | 显示「模型服务 · 模型」，并提供检测模型按钮 | 显示「官方登录 · 已登录 / 未登录」。没有检测模型按钮（没有 Key，也不应发计费请求） |

## 5. 先核实的风险：ChatGPT 令牌可能发给第三方

`[model_providers.bootagent]` 写的是 `requires_openai_auth = true`。按 Codex 的 schema，这个字段表示该模型服务「使用 OpenAI API Key 或 ChatGPT 登录令牌」，凭据从 `cli_auth_credentials_store` 指定的位置读取。

由此推测有两种情况会出问题：

- 用户在 BootAgent 配置好 Codex 之后，自己又执行了 `codex login`，这会把 `auth_mode` 改回 `chatgpt`。
- 用户的 Codex 用的是钥匙串存储（`cli_auth_credentials_store = "keyring"` 或 `auto`），根本不读 BootAgent 写的 `auth.json`。

这两种情况下，发往第三方 `base_url` 的请求带的可能是 ChatGPT 的访问令牌。第一种还会让 BootAgent 写入的 Key 失效。这一点需要先用一个本机抓包的假服务验证；如果属实，单独作为一个问题处理。

如果属实，Codex 的模型服务配置还提供另外两种传 Key 的方式，可以把 BootAgent 的 Key 和登录状态彻底分开。这样「切回官方」就真的只是删掉 `model_provider`：

- `experimental_bearer_token`：Key 写进 `config.toml`。Codex 的说明里不推荐，字段名也标着 experimental。
- `auth`：由一个命令输出 Key。这要求 BootAgent 提供一个可执行的取 Key 命令。

这会推翻 ADR-008 中 Codex 那一行，所以需要写一份新的 ADR，不在本方案里直接改。在此之前，本方案按现状设计：切换时改写 `auth_mode`。

## 6. 交互

- **入口**：Agent 配置页的配置模版列表最上方，固定一项「官方账号登录」，副标题分别为「使用 ChatGPT 账号」和「使用 Claude 订阅」。它不是一个配置模版，不出现在配置模版页，也不能被编辑或删除。
- **应用后**：
  - 已登录时，回执条显示「Codex 已改用官方登录」。
  - 未登录时，回执条为警告「Codex 已改用官方登录，还需要登录」，并带动作按钮「去登录」。点击后用设置里选择的终端执行登录命令；终端关闭后重新检查登录状态。
- **总览行**：标签为「官方登录」。未登录时行内显示可点击的「未登录」标记，与「启动失败」的标记用法一致。
- **切回第三方**：照常选一个配置模版应用。Codex 会再次把 `auth_mode` 写成 `apikey`，ChatGPT 的 `tokens` 保留，下次切回官方时不需要重新登录。但如果令牌在这段时间里过期了，仍然要重新登录，这时用上面的登录状态检查兜底。
- **不做的事**：BootAgent 不读取、不保存、不导出任何 OAuth 令牌；迁移包（TransferPage）不包含官方登录凭据；不从 BootAgent 内部发起 OAuth。

## 7. 实施顺序

1. **核实 §5**：用本机假服务观察 `auth_mode = "chatgpt"` 和钥匙串存储两种情况下，发往 `bootagent` 模型服务的 `Authorization` 头。结论记入 `docs/internal/`。
2. **后端**：
   - 新增 `WriteCodexOfficial`、`WriteClaudeOfficial`，并配 golden 测试：只删除自有键，其他内容逐字节不变。
   - 新增 `UseOfficialLogin`，以及 `official` 绑定和识别规则。
   - 新增 `OfficialLoginStatus`，它执行上表里的本机状态命令并设置超时，只返回「已登录 / 未登录 / 无法判断」。
   - 新增 `LaunchOfficialLogin`，复用 `launch.go` 的终端选择。
3. **前端**：Agent 配置页的固定项、回执条和「去登录」、总览行标签。
4. **第二批 Agent**：按 §3.2 逐个调研后再决定做不做。
