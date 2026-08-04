# 创建和保存 PPIO API Key

[English](../en/02-api-key.md) · **简体中文**

## 创建前确认

先确认：

- 已登录正确的 PPIO 账号。
- 账户已经具备可用额度或权益。
- 你知道这个 Key 将用于哪台设备和哪个项目。

## 创建步骤

1. 打开 PPIO 控制台的 API Key 页面。
2. 创建一个新的 API Key。
3. 在 Key 首次显示时立即复制。
4. 粘贴到 OneAgent 的密码输入框。
5. 点击“测试连接”。

创建后的 Key 只显示一次。不要等离开页面后再尝试查看。

## OneAgent 如何处理 Key

OneAgent 的默认约束是：

- Key 只通过本地表单传给本地安装流程。
- Key 不放在命令行参数中。
- Key 不写入日志和错误信息。
- Key 不上传到 OneAgent 服务端。
- 覆盖配置前创建时间戳备份。

## 推荐的本地环境变量

```bash
export ONEAGENT_API_KEY='你的 PPIO API Key'
export ONEAGENT_API_BASE_URL='https://api.ppio.com/openai'
export ONEAGENT_MODEL='你的模型 ID'
```

如果你使用 CC Switch，Key 应只填写在 CC Switch 的本地 Profile 中，并遵循该工具当前版本的本地存储和权限提示。

## 发现 Key 泄露时

立即在 PPIO 控制台撤销旧 Key，创建新 Key，并重新运行 OneAgent 配置。不要只删除聊天记录或 Git 提交，因为 Key 可能已经进入缓存、日志或截图。

## 官方参考

- PPIO API Key 说明：<https://resource.ppio.com/docs/support/api-key>
- PPIO API 接入说明：<https://ppio.com/docs/model/inference>
