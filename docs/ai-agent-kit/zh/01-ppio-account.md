# PPIO 账号和 Provider 准备

[English](../en/01-ppio-account.md) · **简体中文**

> 本文用 PPIO 举例。内置的 Novita 和自定义 OpenAI-compatible 端点步骤相同，把官网入口和
> Base URL 换成对应 Provider 的即可。

## 1. 打开官方入口

请从 OneAgent 或官方文档打开 PPIO 官网，不要从不明群聊链接下载脚本或提交账号信息。

如果你已经有 PPIO 账号，直接登录即可；没有账号则按页面提示注册。

## 2. 检查账户状态

根据页面提示完成必要的账户验证，并确认账户具备可用额度或公开权益。

Provider 的免费额度、新用户权益、邀请权益和其他公开权益可能有资格、有效期和模型范围限制。请以账户页面当前显示为准，不要把任何固定金额写入项目配置或宣传文案。

## 3. 如果没有公开权益

可以：

1. 查看账户页面的可用额度和权益说明。
2. 根据 Provider 官方页面选择充值或其他合法使用方式。
3. 使用自定义 Provider，前提是它提供 OpenAI-compatible 接口并允许你的使用场景。

OneAgent 不代替用户充值，也不自动操作账户权益页面。

## 4. 进入 API Key 配置

确认账户和额度准备好后，继续阅读 [创建和保存 API Key](./02-api-key.md)。

不要把 API Key 发给任何人来“代查余额”。需要支持时，只提供状态码、模型 ID 是否存在、Base URL 是否正确和脱敏后的日志。

## 官方参考

- PPIO 官网：<https://ppio.com/>
- PPIO 快速开始：<https://ppio.com/docs/support/quickstart>
- PPIO 常见问题：<https://ppio.com/docs/model/FAQs>
