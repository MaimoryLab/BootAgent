# 第一次请求验证

[English](../en/05-first-request.md) · **简体中文**

## 1. 先验证模型列表

```bash
curl https://api.ppio.com/openai/v1/models \
  -H "Authorization: Bearer $ONEAGENT_API_KEY"
```

确认返回结果中存在你准备使用的模型 ID。

## 2. 用最小 Chat 请求验证

```bash
curl https://api.ppio.com/openai/v1/chat/completions \
  -H "Authorization: Bearer $ONEAGENT_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "YOUR_MODEL_ID",
    "messages": [{"role": "user", "content": "Reply with OK."}],
    "max_tokens": 8
  }'
```

不要把真实项目代码、个人信息或长 Prompt 作为第一次测试内容。

## 3. 常见状态码

| 状态 | 含义 | 处理 |
| --- | --- | --- |
| 200 | 请求成功 | 继续启动 Agent |
| 401 / 403 | Key 被拒绝或无权限 | 重新创建 Key，确认粘贴完整 |
| 404 / 405 | 地址或接口不支持 | 确认 Base URL 没有重复 `/v1` 或完整路径 |
| 429 | 频率或额度限制 | 查看账户额度和请求频率 |
| 500 | 服务端错误 | 记录请求时间和状态码，稍后重试 |

## 4. Agent 验证

启动 Agent 后，用一个小任务验证：

```text
请读取当前目录的 README，并列出三个可以改进的地方，不要修改文件。
```

如果 Agent 能返回结果，说明安装、Key、模型和基础配置已经打通。

## 官方参考

- PPIO 模型总览：<https://ppio.com/docs/model/overview>
- PPIO API 接入说明：<https://ppio.com/docs/model/inference>
