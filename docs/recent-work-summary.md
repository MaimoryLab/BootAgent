# 近期工作纪要

分支 `codex/trusted-distribution-site`，9 个提交，82 文件 +11537/−45。已推送 `origin` 与 `maimory`。

三条线：公开分发站、安装链路可验证、配置链路能用。每条都是先实测、再修、再用测试固定住结论。

## 1. 公开分发站（`bc01bcd` → `67b3706`）

独立的 Astro 静态站，26 页，无第三方脚本，CSP `default-src 'self'`。

**核心决定是下载页的每个断言都必须可核对。** 版本、SHA-256、cleanroom 结论都由 `scripts/build_release_index.py` 从 `release/` 的实际产物生成，缺产物或校验和不符就拒绝出条目。手工维护版本表会让页面宣传一个从未构建出来的包，对未签名预览版而言这是最不能犯的错。

`site/src/generated/` 与 `site/public/downloads/` 不入库：前者是 `agents.lock.json` 加 Provider 配置的纯函数，提交只会让每次构建产生 diff；后者含 artifact 校验和，提交等于把某台机器的构建结果冻进仓库。

**过程中发现一个真实缺陷。** 站点在本地打开时整站无样式、图标全 404，但**每个页面仍返回 200**。根因是 `BaseLayout.astro` 输出绝对 URL 的 `<base href>`，与同文件 CSP 的 `base-uri 'self'` 冲突——浏览器拒绝该标签，资源路径全部落空。线上不受影响（Pages 与页面同源），但任何人手动带 `BASE_PATH` 构建后本地预览就是坏的，而症状是 200 而非报错。`site.spec.ts` 现在同时检查无 4xx、背景色取自本站样式表、图片全部解码，让这种「200 但坏了」的状态在测试里可见。这条断言做过反向验证：故意用 `/OneAgent` base 构建后，它确实失败。

顺带修掉一个分发问题：源码归档白名单加 `site/src` 时把 `src/generated/` 一并扫进去了，发布的源码包会携带某台机器的校验和。原有测试只 grep 函数源码文本，看不到这个；新测试断言 `source_files()` 的实际返回值。

## 2. 安装链路可验证（`7c30c5d`、`9a20b21`、`f271d88`）

问题是「Agent 装得上」此前是个假设。手动实测确认装得上（两个锁定版本在 registry 上都存在，落到 PATH，版本相符），然后把结论固化成可重复的脚本。

**`integrity` 记而不验。** `agents.lock.json` 为每个 npm 包记了 sha512，也有测试断言它存在，但 `install_locked_agent()` 全函数不读它——**版本锁住了，字节没有。** 现在安装前用 `npm view` 取 registry 声明的 `dist.integrity` 与清单比对。

**这个校验正是镜像可被接受的前提。** 国内网络下官方 registry 常不可达，而 [产品边界基线](product-boundary-baseline.md) 第 5 节已把「授权镜像」列为优先级 2，条件是有许可证、版本锁定、校验值和上游地址。换 registry 是换取包的渠道，不是代理用户的网络——不建隧道、不转发流量，这是基线划的界。两个锁定包在 npmmirror 与官方源上的 integrity **逐字节相同**，所以这是同一份包的两个渠道。

三个设计决定：默认永远官方源，镜像只能显式选择（自动切换会让用户不知道包从哪来）；只允许 HTTPS 且拒绝内嵌凭据（registry URL 会进入安装环境和日志）；实际使用的 registry 记入日志。镜像不得指向 OneAgent 运营的存储——重新分发商业 Agent 需要授权，而指向公开只读镜像不需要。

验证分三层：`tests/test_install_contract.py`（42 用例，离线，断言 argv 而非执行）进常规 CI；`tests/real_install_test.sh` 真实安装，隔离 npm 前缀排在 PATH 最前，官方源与镜像双路径实测通过；`scripts/agent_e2e_smoke.py` 需真实 Key，手动运行。真实安装不进常规 CI——每次提交打 registry 既慢又会限流。

**一处更正**：我曾判断「没有任何一层真的装包」，这是错的。`scripts/verify_locked_agents.py` 一直在真实安装，且覆盖全部五个 Agent。真实缺口是它之后的两步：shell 是否同意二进制可达，以及 Agent 带着配置能否真的应答。

## 3. 配置链路能用（`74d6633`、`e3e4782`）

装上不等于能用。把配置指向 `127.0.0.1:9`（丢弃端口）实测：Agent 报连接失败说明配置生效，报别的说明没生效。

Codex 通——输出 `provider: oneagent`，证明它读了我们写的 `[model_providers.oneagent]`。

**Claude Code 不通**——报 `Not logged in`。它不从 `settings.json` 取认证，而 OneAgent 报的是 `status: configured` 并告知运行 `claude`。用户照做会撞墙，且没有线索指向 OneAgent。

根因是两处硬编码的集合：

```python
if agent_id in {"codex", "opencode", "kilo-cli"}:
    write_agent_env(...)
```

Codex 能用正是因为它多一个 env 文件，而**唯一无法认证的 Agent 恰好是被这个集合漏掉的那个**。

修法是让 lock 声明凭据途径（`credential_delivery`：`oneagent_env` / `native_env` / `config_file`），Claude Code 另有 `env_vars` 声明它自己读的四个 `ANTHROPIC_*` 变量。`install_many`、`activate_agent`、`_next_step`、`_restart_hint` 都改为读声明，不再按 id 特判。实测确认按新的 `next` 指引启动后不再报 `Not logged in`。

**缺陷能存在，是因为没有任何测试问过密钥怎么到达 Agent**，只验了文件写没写。现在 `CredentialDeliveryTests` 遍历所有 auto Agent 要求凭据可达，`test_release_policy` 要求 lock 声明安装器依赖的字段。

## 现状与未完成

测试：Python 211 用例，`installer.py` 288/288 分支且零 partial，整体 96%；前端 68 用例 + 14 个 e2e；站点 10 + 33；真实安装 cleanroom 双 registry 通过。

[配置链路审查](config-chain-audit.md) 里还有两项未做：

- **任务 2**：消除剩余硬编码。`backups` 仍手写 `.codex/config.toml` 与 `.claude/settings.json`（重复了 lock 的 `config_path`），`providers.py:66`/`:178` 仍比较 `"claude-code"` 判协议（已有 `ADAPTER_PROTOCOLS` 可用）。目标是新增 Agent 只改 lock 加一个适配器函数。
- **任务 3**：把「配置后能用」做成不需要 Key 的检查进常规 CI。本轮缺陷正是在真实 Key 那道门槛之外发现的——指向丢弃端口、断言报的是连接失败而非认证错误，就能区分本轮两种结果。

另有 [按 Agent 分化配置界面](per-agent-config-plan.md) 停在「评估 Claude Code 双模型字段」一级，未启动。

## 相关文档

- [空白机器可用性验证计划](blank-machine-verification-plan.md) —— 三层验证的设计与实测结论
- [配置链路审查](config-chain-audit.md) —— 硬编码清单与剩余任务
- [公开分发站运营与发布手册](public-site-operations.md)
- [CC Switch 参考笔记](cc-switch-reference-notes.md)
