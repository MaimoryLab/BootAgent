# OneAgent 渠道台账

## 用途

本文件是 [多渠道分发与合规政策](distribution-compliance-policy.md) 第 2 节定义的发行台账载体，也是第 9 节发布门禁中台账条目的核对对象。字段定义以政策第 2 节为准，本文件不得另立 schema。

台账为人工流程，仓库中没有自动化门禁强制它；渠道负责人必须在每次发布、弃用或撤回时同步更新本文件。

## 字段说明

| 字段 | 要求 |
| --- | --- |
| `release_version` | 发布版本号，与 `release-manifest-*.json` 一致。 |
| `artifact_name` | 压缩包文件名，含平台、架构和发布状态。 |
| `sha256` | 与 `SHA256SUMS-*.txt` 一致；同一版本所有渠道必须相同。 |
| `channel` | `github-release` / `official-site` / `netdisk` / `enterprise-drive` 等。 |
| `download_url` | 渠道下载链接。 |
| `uploaded_at` | 上传时间（UTC）。 |
| `uploaded_by` | 负责人，不使用无法追溯的临时账号。 |
| `status` | 只能是 `active`、`deprecated` 或 `withdrawn`。 |
| `withdrawn_at` | 撤回时间（UTC），未撤回留空。 |
| `withdrawal_reason` | 撤回原因，未撤回留空。 |

原文件被网盘重新处理或校验值发生变化时，该条目不得继续标记为 `active`。已撤回版本不得换链接继续分发；修复后发布新版本和新校验值。

## 台账

| release_version | artifact_name | sha256 | channel | download_url | uploaded_at | uploaded_by | status | withdrawn_at | withdrawal_reason |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |

截至 2026-07-27，尚无通过外部渠道分发的版本。CI 生成的 `technical-preview-unsigned` 产物（`technical-preview.yml`）只是构建输出，未经渠道上传流程，不计入台账。
