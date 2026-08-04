# Provider RC 测试说明

> 状态：**协议要求仍然有效，执行入口已不存在**（2026-08-04）。
>
> 本文描述的三协议槽位、凭据处理和判定口径仍是 Provider 接入的要求。但执行它的
> `cmd/oneagent-provider-smoke` 已于 `23805b0` 随构建流程迁移而移除，本文的命令
> **无法运行**。恢复自动化前，正式 RC 需按下面的口径手工执行。
>
> 本文引用的 `.github/workflows/release-candidate.yml` 同样不存在；当前仓库只有
> `ci.yml` 和 `build-artifacts.yml`。

## 测试层级

OneAgent 将 Provider 测试分为两个层级：

1. 本地预检：复用 Go Provider 客户端，验证鉴权头和三类协议请求结构。
2. 正式 Release Candidate：使用 PPIO、Novita 各自的受保护低权限 Key 验证真实供应商能力。

本地预检不能替代正式 RC，也不能用于宣称 PPIO 或 Novita 已通过兼容性验收。

## 三类协议模型槽位

每个正式 Provider 必须分别提供以下变量，即使三个变量暂时使用同一个模型 ID，也不得合并为单一变量：

```text
ONEAGENT_<PROVIDER>_API_KEY
ONEAGENT_<PROVIDER>_OPENAI_MODEL
ONEAGENT_<PROVIDER>_ANTHROPIC_MODEL
ONEAGENT_<PROVIDER>_RESPONSES_MODEL
```

对应请求为：

| 槽位 | 请求 |
| --- | --- |
| OpenAI | `POST /v1/chat/completions` |
| Anthropic | `POST /v1/messages`，包含 `X-Api-Key` 和 `Anthropic-Version` |
| Responses | `POST /v1/responses` |

`GET /v1/models` 只验证鉴权和模型目录可访问，不能证明三个推理协议均兼容。

## 本地执行

```text
go run ./cmd/oneagent-provider-smoke --provider ppio --timeout 30s
```

命令只从环境变量读取 Key 和模型，不接受命令行凭据，也不会把响应正文写入日志。正式 RC：

```text
go run ./cmd/oneagent-provider-smoke --provider all --timeout 30s
```

`all` 严格只运行 PPIO 和 Novita。自定义端点应在单独的环境隔离中运行，临时代理结果不能替代正式供应商证据。

## 正式 RC 门禁

`.github/workflows/release-candidate.yml` 的 Go smoke 仍要求：

- `ONEAGENT_PPIO_API_KEY` 和 `ONEAGENT_NOVITA_API_KEY` 存放在受保护 CI Secret。
- 两个 Provider 分别配置 OpenAI、Anthropic、Responses 三个模型变量。
- `/v1/models`、Chat Completions、Responses 和 Anthropic Messages 全部成功。
- 随后的真实 Agent 最新版本安装、PATH 和无密钥配置采用检查全部通过。
- 任一协议不支持时，明确阻止对应 Agent/Provider 组合，不使用其他协议或临时端点绕过门禁。
