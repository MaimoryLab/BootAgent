# 配置工具选择

[English](../en/03-config-tools.md) · **简体中文**

OneAgent 提供三种配置方式。第一次使用建议选择内置配置；需要多个 Provider 或多个账号时，再选择 CC Switch 等本机工具。

## 方式一：OneAgent 内置配置

适合：

- 第一次使用 PPIO。
- 只需要一个 Provider。
- 希望配置过程最容易审查和排错。

行为：

1. 检测 Agent 是否已安装。
2. 调用官方安装源或显示官方安装命令。
3. 写入目标 Agent 的官方配置入口。
4. 通过 `/v1/models` 获取模型列表。
5. 发起一次最小请求验证。

这是第一次使用时的默认路径。

## 方式二：CC Switch

适合：

- 需要在 PPIO、其他 OpenAI-compatible 服务和官方账号之间切换。
- 需要为不同项目保存不同配置。
- 经常在 Claude Code、Codex、OpenCode 等工具之间切换。

CC Switch 是可选的本机配置工具，不是 OneAgent 的必需依赖。请从其官方项目入口获取当前版本，OneAgent 不把它的二进制或安装脚本重新打进启动包。

具体步骤见 [CC Switch 指引](./tools/cc-switch.md)。

## 方式三：手动配置

适合：

- 你的 Agent 有独立的配置管理方式。
- 你不希望引入额外的本机配置工具。
- 你正在排查自动配置结果。

手动配置至少需要确认：

```text
Base URL: https://api.ppio.com/openai
Models:   GET /v1/models
Chat:     POST /v1/chat/completions
Key:      只放在本机安全存储中
```

## 工具选择规则

| 需求 | 推荐方式 |
| --- | --- |
| 第一次使用 | OneAgent 内置配置 |
| 单一 PPIO 账号 | OneAgent 内置配置 |
| 多个 Provider | CC Switch 或同类 Profile 工具 |
| OpenClaw / Hermes Gateway | 官方文档和人工确认 |
| IDE 扩展 | 扩展自己的 Provider 设置 |
| 不确定工具来源 | 手动配置或 OneAgent 内置配置 |

## 重要区别

CC Switch 的本地配置工具功能和任何第三方托管 API 服务是两回事。使用 CC Switch 只代表在本机切换配置，不代表 PPIO Key 被转交给 CC Switch 的服务器，也不代表其服务地址可以写成 PPIO Base URL。

任何配置工具都不能替代合规网络接入。OneAgent 不提供 VPN、代理、节点订阅或“翻墙下载”能力；如果工具或安装源不可达，请返回 [开始使用](./00-start-here.md) 查看手动安装路径。
