# OneAgent AI 开发环境激活文档

[English](../README.md) · **简体中文**

这是一套面向个人开发者、团队和组织发行方的 AI Agent 环境配置文档。

文中以 **PPIO** 作为贯穿示例。OneAgent 内置的另一个 Provider 是 **Novita**，此外还支持
自定义 OpenAI-compatible 端点；三者在应用里的配置步骤相同，只是 Base URL 和 Key 来源不同，
所以下面的流程对它们同样适用。内置 Provider 的真源是仓库根的 `providers.lock.json`。

产品边界以 [OneAgent 产品边界基线](../../product-boundary-baseline.md) 为准。组织发行方可以在项目外部提供自己的兑换码或项目说明，但不改变 OneAgent 核心流程。

## 推荐阅读顺序

1. [开始使用](./00-start-here.md)
2. [PPIO 账号和 Provider 准备](./01-ppio-account.md)
3. [创建和保存 API Key](./02-api-key.md)
4. [配置工具选择](./03-config-tools.md)
5. [Agent 分类和安装指引](./04-agent-guides.md)
6. [第一次请求验证](./05-first-request.md)

需要使用 CC Switch 的用户，再阅读 [CC Switch 配置 PPIO 指引](./tools/cc-switch.md)。

## 文档原则

- Provider 账号和 API Key 属于用户自己的账户。
- OneAgent 负责本地检测、官方安装引导和配置写入。
- 不在文档包中存放真实 API Key。
- 不把第三方配置工具的托管服务地址当作 Provider Base URL。
- Provider 的公开权益以账户页面实际显示为准，不由 OneAgent 承诺固定额度。
- 用户无法访问官方安装源时，使用合规网络、授权镜像或手动安装，不使用 OneAgent 提供的 VPN、代理或绕过方案。
