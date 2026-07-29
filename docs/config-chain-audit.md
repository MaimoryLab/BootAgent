# 配置链路实测与硬编码审查

只验证 Codex 与 Claude Code。问的是两件事：配置写完之后 Agent 是否真的采用；以及支撑这条链路的代码是否可扩展。

两条结论：

- **Codex 通，Claude Code 不通。** 我们对 Claude Code 只写 `settings.json`，而它不从那里取认证，实机启动得到 `Not logged in`——但 OneAgent 报的是 `status: configured`。
- **硬编码规模足以阻碍扩展。** `agents.lock.json` 号称唯一真源，但命令名与配置路径在 Python 里被重复写了一遍，新增一个 Agent 要改 6 处分散代码。

## 1. 实测：配置写完之后 Agent 认不认

方法是把配置指向 `127.0.0.1:9`（丢弃端口）。若 Agent 报连接失败，说明它读到并采用了我们的配置；若报别的，说明配置没生效。

### Codex：通

```
$ codex exec --skip-git-repo-check "say ok"
provider: oneagent
ERROR: Reconnecting... 1/5
```

`provider: oneagent` 是决定性的——Codex 读了我们写入 `~/.codex/config.toml` 的 `[model_providers.oneagent]`，并按 `base_url` 去连。`env_key = "ONEAGENT_API_KEY_CODEX"` 也生效，密钥经环境变量间接传入。**这条链路完整。**

### Claude Code：不通

```
$ claude -p "say ok"          # HOME 指向写好 settings.json 的干净目录
Not logged in · Please run /login
```

同一份配置改用环境变量直接给，则不再报错（进入连接尝试）：

```
$ ANTHROPIC_BASE_URL=... ANTHROPIC_AUTH_TOKEN=... claude -p "say ok"
（无报错输出）
```

**所以 `settings.json` 的 `env` 块不足以让 Claude Code 认证。** 我们写进去的四个变量：

```json
{"env": {"ANTHROPIC_BASE_URL": "...", "ANTHROPIC_AUTH_TOKEN": "...",
         "ANTHROPIC_MODEL": "...", "ANTHROPIC_SMALL_FAST_MODEL": "..."}}
```

而 OneAgent 对此报告：

```
status: configured
next:   claude
```

**这是本轮最严重的问题**：产品声称配置完成并给出启动命令，用户照做会撞上 `Not logged in`，且没有任何线索指向 OneAgent。相比之下 Codex 之所以能用，正是因为它额外有 `~/.oneagent/agents/codex.env`。

根因在 `install_many`（`installer.py:1137`）与 `activate_agent`（`:1375`）都写着：

```python
if agent_id in {"codex", "opencode", "kilo-cli"}:
    write_agent_env(...)
```

**Claude Code 是唯一被排除在 env 文件之外、却又依赖环境变量的自动配置 Agent。** Aider 有自己的 `aider.env`，另外三个有 `agents/<id>.env`，只有它两头都没有。

## 2. 硬编码审查

`agents.lock.json` 每个 auto Agent 已有 `command`、`config_path`、`config_adapter`、`version_args` 等字段。问题是 Python 里又写了一遍，两处会不一致。

| 位置 | 硬编码内容 | 性质 |
| --- | --- | --- |
| `installer.py:694-698` | `_next_step` 的五个 Agent 启动命令 | **重复了 lock 的 `command`** |
| `installer.py:1335` | `_restart_hint` 的四个命令名映射 | **重复了 lock 的 `command`** |
| `installer.py:1454-1455` | `backups` 手写 `.codex/config.toml` 与 `.claude/settings.json` | **重复了 lock 的 `config_path`** |
| `installer.py:1137`、`:1375` | 需要 env 文件的 Agent 集合 | 行为未在 lock 声明（第 1 节的缺陷根因） |
| `installer.py:530`、`:1421` | Windows 上 Claude Code 需要 git | lock 有 `windows_prerequisites`，此处未读它 |
| `installer.py:712-724` | `_write_agent_config` 按 adapter 分派 | **合理**：适配器是代码，不是数据 |
| `installer.py:293` | `write_codex_config` 里的 `agent_env_var("codex")` | **合理**：该函数专属 Codex |
| `providers.py:66`、`:178` | `"claude-code"` 判断 Anthropic 协议 | 已有 `ADAPTER_PROTOCOLS`，此处绕过了它 |

前三项是真正的可维护性问题：同一事实存在两份，改 lock 不会改行为。`:1137` 那项更进一步——它是一个**未被声明的行为**，也正是 Claude Code 失效的原因。

新增一个自动配置 Agent 现在要动：lock 一处 + `_write_agent_config` 分派 + `_next_step` + `_restart_hint` + env 文件名单 + `backups`，共 6 处，其中 4 处纯属重复。

## 3. 要完成的任务

### 任务 1：修 Claude Code 的认证链路（阻塞级）— 已完成

lock 里为每个 auto Agent 增 `credential_delivery`（`oneagent_env` / `native_env` / `config_file`），Claude Code 另有 `env_vars` 声明它自己读的四个变量名。`install_many` 与 `activate_agent` 改读 `needs_env_file(meta)`，不再判断 id 集合；`_next_step` 与 `_restart_hint` 也由 lock 推导。

实测确认（配置指向 `127.0.0.1:9`，按 `next` 指引启动）：

```
next: source ~/.oneagent/agents/codex.env && codex
      source ~/.oneagent/agents/claude-code.env && claude

Codex        采用了我们写的 provider（provider: oneagent）
Claude Code  不再出现 Not logged in
```

防复发的断言分两处：`test_install_contract.py` 的 `CredentialDeliveryTests` 遍历所有 auto Agent，要求每个在配置后都能从 env 文件或配置文件之一取到密钥；`test_release_policy.py` 要求 lock 里每个 auto Agent 都声明 `command` / `config_path` / `config_adapter` / `credential_delivery`，`native_env` 还必须给出变量名。原先的缺陷正是「没有任何测试问过密钥怎么到达 Agent」。



让 Claude Code 也拿到 env 文件。lock 里为每个 auto Agent 声明它需要哪些环境变量，`install_many` 与 `activate_agent` 读该声明而不是硬编码集合。

Claude Code 的 env 文件应导出 `ANTHROPIC_BASE_URL` / `ANTHROPIC_AUTH_TOKEN` / `ANTHROPIC_MODEL` / `ANTHROPIC_SMALL_FAST_MODEL`，`_next_step` 相应改为先 source 再启动。`settings.json` 继续写（它承载非认证配置），但不再是认证的唯一途径。

必须有一个测试断言「每个自动配置 Agent 都有可用的认证途径」，否则同类缺陷会再次静默通过。

### 任务 2：让 lock 成为真正的唯一真源 — 已完成

落实情况：`backups` 改为遍历 lock、按各 Agent 的 `config_path` 推导备份 glob，不再手写两条路径；`providers.py` 的 `provider_config_base` 改收推理协议（调用方传 `agent_protocol(adapter)`），两处 `"claude-code"` 字面量比较移除；`_require_prerequisites` 与 `status_payload` 的 Windows 门禁改读 `windows_prerequisites`。`test_release_policy.py` 新增 `LockIsTheSourceOfTruthTests`，遍历 lock 断言备份、Windows 门禁与协议判定都由声明驱动。

把重复的三项改为读 lock：

- `_next_step` 与 `_restart_hint` 用 `meta["command"]`，不再手写命令名。
- `backups` 用 `meta["config_path"]` 推导备份 glob，不再手写路径。
- `_require_prerequisites` 与 `status_payload` 读 `meta["windows_prerequisites"]`，不再判断 `agent_id == "claude-code"`。
- `providers.py` 用 `agent_protocol(adapter)` 判断，不再比较 agent id。

目标：新增 Agent 只需改 lock 加一个适配器函数。加一个测试遍历 lock，断言不存在只在 Python 里出现的 Agent 行为。

### 任务 3：把「配置后能用」纳入验证 — 已完成

落实情况：新增 `scripts/agent_config_adopted_check.py`。其中纯分类器 `classify_adoption` 判别「连接失败 = 配置已采用」与「认证/登录错误 = 配置未采用」，由 `test_rc_scripts.py` 用本轮 Codex 与 Claude Code 两个真实输出在常规 CI 离线覆盖；脚本本体把配置指向丢弃端口 `127.0.0.1:9`、用假 Key，自包含安装并实跑 Agent，无需任何真实 Key，已接入 `release-candidate.yml`（非 Windows）。这把发现本轮缺陷的那道「真实 Key 门槛」从该检查里移除了。

`agent_e2e_smoke.py` 已经会实际调用两个 Agent，但它需要真实 Key，本轮缺陷就是在那道门槛之外发现的。补一个不需要 Key 的检查：把配置指向丢弃端口，断言 Agent 报的是连接失败而非认证/配置错误——这恰好能区分本轮两种结果，且能进常规 CI。

## 4. 明确不做

- 不改 `_write_agent_config` 的 adapter 分派。适配器是代码而非数据，五个函数写的是五种格式，这不是硬编码。
- 不为 guide-only Agent 建配置或 env 文件。
- 不扩展到另外三个 Agent 的实测。本轮范围是 Codex 与 Claude Code；但任务 1、2 的实现应当对五个都成立，因为它们要消除的正是「按 id 特判」。

相关文档：[空白机器可用性验证计划](blank-machine-verification-plan.md)、[产品边界基线](product-boundary-baseline.md)。
