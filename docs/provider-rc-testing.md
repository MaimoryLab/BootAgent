# Provider RC 测试说明

## 测试层级

OneAgent 将 Provider 测试分为两个层级：

1. 本地临时预检：验证测试脚本、鉴权头和三类协议请求结构可以工作。
2. 正式 Release Candidate：使用 PPIO、Novita 各自的受保护低权限 Key 验证真实供应商能力。

本地预检不能替代正式 RC，也不能用于宣称 PPIO 或 Novita 已通过兼容性验收。

## 三类协议模型槽位

每个正式 Provider 必须分别提供以下变量，即使三个变量暂时使用同一个模型 ID，也不得合并为单一变量：

```text
ONEAGENT_<PROVIDER>_OPENAI_MODEL
ONEAGENT_<PROVIDER>_ANTHROPIC_MODEL
ONEAGENT_<PROVIDER>_RESPONSES_MODEL
```

对应请求为：

| 槽位 | 请求 |
|---|---|
| OpenAI | `POST /v1/chat/completions` |
| Anthropic | `POST /v1/messages`，包含 `X-Api-Key` 和 `Anthropic-Version` |
| Responses | `POST /v1/responses` |

`GET /v1/models` 只验证鉴权和模型目录可访问，不能证明三个推理协议均兼容。

## 临时 apiproxy 档案

截至 2026 年 7 月 22 日，本地低 Token 请求确认 `openai/gpt-5.6-terra` 在以下四个请求上均返回 HTTP 200：

```text
GET  https://apiproxy.paigod.work/v1/models
POST https://apiproxy.paigod.work/v1/chat/completions
POST https://apiproxy.paigod.work/v1/responses
POST https://apiproxy.paigod.work/v1/messages
```

临时档案的三个模型槽位当前都设置为：

```text
openai/gpt-5.6-terra
```

`openai/gpt-5.6-luna` 只支持两类协议，因此不能作为三协议预检的统一默认模型。

本地执行命令：

```bash
python3 scripts/provider_rc_smoke.py \
  --provider apiproxy \
  --api-key-json ~/.codex/auth.json \
  --api-key-field OPENAI_API_KEY \
  --timeout 45
```

Key 从 JSON 文件内部读取，不作为命令行参数值传递。当前本机认证文件中的实际字段名是 `OPENAI_API_KEY`，不是 `OPENAIKEY`。

## 正式 RC 门禁

`.github/workflows/release-candidate.yml` 的 `--provider all` 只运行 `ppio` 和 `novita`。正式 RC 仍要求：

- `ONEAGENT_PPIO_API_KEY` 和 `ONEAGENT_NOVITA_API_KEY` 存放在受保护 CI Secret。
- 两个 Provider 分别配置 OpenAI、Anthropic、Responses 三个模型变量。
- 四个端点全部成功，并继续执行真实 Agent 首次请求验收。
- 任一协议不支持时，明确降级对应 Agent/Provider 组合，不使用代理预检结果绕过发布门禁。

临时 `apiproxy` 档案不进入 `--provider all`，也不写入正式 RC workflow 的 Secret 或变量列表。
