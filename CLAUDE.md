# CLAUDE.md

OneAgent：本地 AI 开发环境激活器。Python 3.12 标准库内核 + React 七页向导，经 `127.0.0.1` HTTP 通信，负责检测、安装并配置 5 个 CLI Agent 指向 OpenAI- 或 Anthropic-compatible Provider。

当前 `0.2.0-dev`，发行渠道只能标记 `technical-preview-unsigned`。功能说明与发行流程见 [README.md](README.md)。

Wails v3 迁移已在当前分支进行，阶段 0-3 的退出门禁已通过，但尚未切换生产入口。Go 核心
位于 `internal/`、`cmd/oneagent` 和 `cmd/oneagent-desktop`；默认桌面命令只有在显式使用
`wails` build tag 时才链接 Wails。

**CLI 路径已迁移到 Go**：`scripts/install.sh` 与 `.ps1` 是纯转发层，转发到
`cmd/oneagent` 且不再定位 Python，改动 CLI 行为要改 Go 而不是 `oneagent/cli.py`。它们
不做按需构建（调用方使用临时 HOME，`go build` 会把 module cache 写进去），所以
`tests/install_test.sh` 之前需要先 `go build -o bin/oneagent ./cmd/oneagent`。

GUI 路径（`scripts/gui.py`、`oneagent/server.py`）、Python 测试和发布流程在阶段 4-6
的门禁通过前继续保留并作为当前生产路径。

## 沟通语言

**回复一律使用简体中文，没有例外。** 包括分析、结论、计划、代码审查意见和确认提问；用户用英文提问也不切换。代码、标识符、提交信息和代码注释保持英文（与现有代码库一致）；README、`docs/` 和 ADR 保持中文。

## 常用命令

本机 `python3` 是 3.14，测试与打包必须显式用 `python3.12`；`scripts/gui.py` 用任意 ≥3.12 均可。

```bash
# Python 契约测试（74 用例，约 7s）
python3.12 -m unittest tests.test_core tests.test_cli tests.test_server \
  tests.test_release_policy tests.test_edge_cases tests.test_rc_scripts

# 覆盖率门禁：整体 ≥85%，installer.py 必须 100% 分支且无 partial
python3.12 -m coverage run --branch -m unittest tests.test_core tests.test_cli \
  tests.test_server tests.test_release_policy tests.test_edge_cases tests.test_rc_scripts
python3.12 -m coverage report --fail-under=85

# 源码 GUI
python3 scripts/gui.py --port 8765 --no-open

# 前端（build 会先跑 tsc --noEmit）
cd frontend && npm ci && npm run build
npm run test:coverage
npm run e2e                            # Playwright 自动拉起 gui.py:8765

# Go 迁移线（不需要 Python；parity 门禁需要 ≥3.12 才会真正运行）
go vet ./... && ONEAGENT_REQUIRE_PARITY=1 go test ./... && go test -race ./...
go build -o bin/oneagent ./cmd/oneagent   # install_test.sh 依赖它先存在

# 隔离验证
bash tests/install_test.sh             # 经 install.sh 转发到 Go CLI，临时 HOME
python3.12 tests/gui_smoke_test.py     # 真实 HTTP + Cookie/Origin 冒烟
bash scripts/test_docker_cleanroom.sh  # Linux 断网 cleanroom
```

## 代码地图

```
oneagent/          Python 内核，零第三方依赖
  catalog.py       读 agents.lock.json、平台/HOME 解析、PROVIDERS 常量
  providers.py     base URL 校验与推导、chat_probe、list_models
  installer.py     主体：原子写、备份、权限、5 个配置适配器、install_many、status_payload
  server.py        stdlib http.server：/api/{status,probe,models,install,profiles,open-register}、
                   POST /api/agents/<id>/activate（单 Agent 重新指向）+ 静态托管
  cli.py           argparse CLI（已被 cmd/oneagent 取代，仅 GUI 打包入口仍引用）
  entrypoint.py    打包版入口：无参→GUI，有参→CLI
internal/          Go 核心，桌面壳与 CLI 共用
  app/             use case：GetStatus、InstallAgents、ActivateAgent、SaveProfile
  binding/         Wails service 与传输 DTO，不放业务逻辑
  catalog/ provider/ install/ config/ profile/ securefs/ process/ platform/
cmd/oneagent       纯 Go CLI（CLI 路径的真源）
cmd/oneagent-desktop  Wails 壳，仅 `-tags wails` 时链接 Wails
frontend/src/
  App.tsx          react-router 七页 + SetupGuard 前置校验
  state/           useReducer + Context，WizardState 是唯一状态源
  backend/         传输 adapter：按运行时选 Wails binding 或 HTTP
  api/client.ts    fetch 封装，非 2xx 抛 OneAgentApiError（GUI 路径）
frontend/bindings/ Wails 生成物，禁止手改；改 Go DTO 后重新生成
agents.lock.json   Agent 版本/包管理器/配置适配器/平台/许可证的唯一真源
scripts/           gui.py、install.sh/.ps1（转发到 Go CLI）、build_release.py
```

GUI 主链路：`React → POST /api/install → install_many() → _write_agent_config() → atomic_write()`。
CLI 主链路：`cmd/oneagent → app.InstallAgents() → config.Writer → securefs.AtomicWrite()`。

## 硬性约束

改动前先确认不会破坏以下条目，`tests/test_release_policy.py` 与 CI 会直接拦截：

- **零运行时依赖**：`oneagent/` 只用标准库，`pyproject.toml` 的 `dependencies` 保持为空。
- **禁止 `shell=True` 与 `curl | sh`**：子进程一律走 `runtime.runner([...])` 列表参数。
- **只绑定 127.0.0.1**：`create_server` 拒绝其他 host；POST 同时校验 Origin 白名单与 HttpOnly/SameSite=Strict 会话 Cookie（`secrets.compare_digest`）。
- **API Key 不落地**：不进 `profile.json`、argv、URL、日志、React state、浏览器存储；日志一律过 `redact(text, [api_key])`。
- **写配置只走 `atomic_write`**：`ensure_private_dir`(0700) → 备份 `*.backup-<ts>` → 临时文件先 `secure_path`(0600 / Windows icacls 断继承) → `os.replace`。密钥备份无法加固时删除并报错。
- **保留用户字段**：Codex TOML 与 Claude/OpenCode/Kilo JSON 合并时不得丢弃非 OneAgent 管理的键；解析失败返回 `CONFIG_WRITE_FAILED`，绝不静默覆盖。
- **版本锁定**：`agents.lock.json` 不允许 `latest`，npm 包必须带 `sha512-` integrity；`--latest` 仅在用户显式指定时生效。
- **guide-only Agent** 不装包、不写私有配置、不起后台服务。
- **前端产物**不得包含 source map 或 CDN/远程字体引用。
- **覆盖率**：`oneagent/installer.py` 100% 分支且 `num_partial_branches == 0`；Python 整体与前端 `src/api`、`src/state` 均 ≥85%。
- 产品边界（禁 VPN/代理/共享 Key/自动登录）见 [docs/product-boundary-baseline.md](docs/product-boundary-baseline.md)，突破需新增 ADR。

## 代码约定

- Python 文件均以 `from __future__ import annotations` 开头，全量类型注解，dataclass 承载配置。
- **副作用全部经 `Runtime`**：`home / os_id / runner / which / env` 都是可注入字段，测试靠替换它们模拟 npm、uv 与四个平台，不触碰真实系统。新增代码不要直接调用 `subprocess.run`、`shutil.which`、`os.environ` 或 `Path.home()`。
- 失败一律 `raise OneAgentError(code, message)`，code 取自 `errors.EXIT_CODES`；响应恒定携带 `error / message / status / error_code / retryable`。
- 传输契约：请求体 snake_case，`StatusResponse` 等派生字段 camelCase——改一侧必须同步 `frontend/src/types/api.ts`。
- 常规 CI 使用假的 npm/uv，不下载真实 Agent、不访问 Provider；真实安装与 Provider 冒烟只在手动 `release-candidate.yml` 执行。

## 常见任务

**新增自动配置 Agent**：`agents.lock.json` 补条目（`command` / `config_path` / `config_adapter` / `credential_delivery` 必填，version/integrity/source/license/platforms 同旧）→ 在 `installer.py` 写 `write_*_config` 并注册到 `_write_agent_config` 分派 → 更新 `tests/test_core.py` 的适配器断言。

`credential_delivery` 决定密钥怎么到达 Agent，取值 `oneagent_env`（配置文件引用 `ONEAGENT_*` 变量）、`native_env`（Agent 只读自己的变量名，需同时给 `env_vars`）、`config_file`（密钥在适配器写的配置里）。**启动命令、重启提示和 env 文件都由此推导，不要在 Python 里按 agent id 特判**——Claude Code 曾因为不在硬编码集合里而报「配置完成」却无法认证。

**新增 Provider**：`catalog.py` 的 `PROVIDERS` 加 `base_url` 与 `anthropic_base_url` → 同步 `frontend/src/types/api.ts` 的 `ProviderId`。

**改错误码**：`errors.EXIT_CODES` 与 README「错误契约」小节必须同时更新。
