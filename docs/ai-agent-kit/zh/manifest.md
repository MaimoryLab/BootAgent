# AI 开发环境启动包清单

[English](../en/manifest.md) · **简体中文**

## 运行时文件

| 文件 | 作用 | 是否需要密钥 |
| --- | --- | --- |
| `BootAgent.app`（macOS）/ `bootagent-desktop.exe`（Windows）/ Linux AppImage | 桌面应用 | 否 |

发行包由 `.github/workflows/build-artifacts.yml` 构建。早期版本用 `launcher`、
`start.sh`、`start.command` 三个脚本启动本地 GUI，Go/Wails 迁移后不再需要。

## 文档文件

| 文档 | 目标用户 |
| --- | --- |
| `00-start-here.md` | 所有用户 |
| `01-ppio-account.md` | 需要准备 Provider 账号的用户 |
| `02-api-key.md` | 已经进入 API Key 配置的用户 |
| `03-config-tools.md` | 需要配置切换工具的用户 |
| `tools/cc-switch.md` | 使用 CC Switch 的用户 |
| `04-agent-guides.md` | 选择 Agent 的用户 |
| `05-first-request.md` | 已完成配置、准备验证的用户 |

## 版本和来源记录

每个发行包应记录：

```text
包版本：
发布日期：
Provider 文档验证日期：
Agent 版本范围：
配置工具版本范围：
```

组织发行方可以在包外维护自己的兑换码或项目说明，但这些内容不进入 BootAgent 核心 manifest。

不在 manifest 中写入真实 API Key、共享网关 Key 或个人账号信息。

## 禁止打包内容

- VPN 或代理客户端。
- 代理节点、订阅链接或跨境中转配置。
- 未确认许可证的 Agent 包体。
- 商业 Agent 的未授权二进制。
- Provider 共享 API Key。
- 用户账号密码、手机号或身份证信息。
