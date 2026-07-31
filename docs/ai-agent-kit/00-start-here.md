# OneAgent AI 开发环境激活指南

这套启动包帮助你完成三件事：

1. 准备一个可用的模型 Provider。
2. 安装或配置一个 AI Agent。
3. 运行第一个成功的模型请求。

## 开始前准备

- 一台支持的 macOS、Windows 或 Linux 设备；普通 OneAgent 流程不需要 Python。
- 一个可用的模型 Provider 账号，或准备注册一个。
- 你准备使用的 Agent，例如 Codex、Claude Code、OpenCode 或 Aider。

## 推荐路径

```text
启动 OneAgent
→ 注册或登录 Provider
→ 创建 API Key
→ 选择 Agent
→ 选择配置工具
→ 选择模型
→ 开始配置
→ 完成第一次请求
```

如果你已经熟悉多 Provider 切换，可以在配置方式页面选择 **CC Switch**，然后阅读 [配置工具选择](./03-config-tools.md) 和 [CC Switch 指引](./tools/cc-switch.md)。

## 启动桌面应用

```bash
cd frontend && npm ci && npm run build
cd ..
go run -tags wails ./cmd/oneagent-desktop
```

也可以下载对应平台的 `technical-preview-unsigned` 包后直接启动 Wails 应用。

## 三条安全规则

1. 不要把 API Key 发到聊天、Issue、作业或截图中。
2. 不要把 API Key 直接写进代码仓库。
3. 如果你不确定某个配置工具是否可信，回到 OneAgent 内置配置路径。

## 下载不到 Agent 时

如果官方安装源在当前网络中不可达：

1. 使用所在组织或网络服务商提供的合规网络接入。
2. 使用经过授权的镜像，并核对版本和校验值。
3. 在其他合规环境手动安装，再回到 OneAgent 做本机检测。

OneAgent 不提供 VPN、代理、节点订阅或绕过网络限制的配置。

## 遇到问题

- Provider 问题：查看 [PPIO 账号和 Provider 准备](./01-ppio-account.md)。
- Key 问题：查看 [创建和保存 API Key](./02-api-key.md)。
- 配置工具问题：查看 [配置工具选择](./03-config-tools.md)。
- Agent 问题：查看 [Agent 分类和安装指引](./04-agent-guides.md)。
- 请求问题：查看 [第一次请求验证](./05-first-request.md)。
