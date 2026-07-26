# OneAgent AI 开发环境激活器产品设计

> 本文是 OneAgent 的通用产品规划。产品边界以 [OneAgent 产品边界基线](product-boundary-baseline.md) 为准。

## 1. 产品定义

OneAgent 是一个帮助用户激活、配置和启动本地 AI 开发环境的工具。

它连接三类对象：

```text
模型服务 Provider
        ↓
本地配置和 Profile
        ↓
AI Agent / IDE / Gateway 工具
```

OneAgent 的结果不是“下载了多少软件”，而是：

> 用户能否在自己的设备上完成第一次成功的 AI Agent 请求。

## 2. 核心边界

### OneAgent 负责

- 引导用户注册或登录 PPIO 等模型服务。
- 引导用户创建自己的 API Key。
- 检测本机 Agent 和运行环境。
- 调用官方安装源，或提供官方安装命令。
- 写入已经确认的 Agent 配置入口。
- 获取模型列表并选择模型 ID。
- 完成最小请求验证。
- 提供 Agent、配置工具和项目模板文档。

### OneAgent 不负责

- VPN、代理、专线或绕过网络限制。
- 共享 API Key 或默认统一网关。
- 自动操作第三方网站登录、验证码和账户权益。
- 未授权重新分发商业 Agent 包体。
- 默认上传 API Key、Prompt、源代码或完整请求日志。

## 3. 用户范围

OneAgent 面向所有需要本地 AI 开发环境的用户，包括：

- 第一次使用 AI Agent 的个人用户。
- 已有 PPIO 账号和 API Key 的开发者。
- 需要多个 Provider 或模型 Profile 的高级用户。
- 公司、社区和其他组织发行方。

组织发行方可以提供自己的说明、下载入口和项目模板，但这些内容属于发行方配置，不进入 OneAgent 核心数据模型。

## 4. 产品组成

### 4.1 Activation Center

线上入口负责：

- 解释产品能做什么。
- 引导用户前往 Provider 官方注册页。
- 提供 API Key 和模型配置说明。
- 提供启动器下载。
- 展示当前版本、兼容范围和安全边界。

它不是复杂的营销落地页，而是一个面向操作的激活入口。

### 4.2 Local Launcher

本地启动器负责：

- 启动本地 GUI。
- 检测 Agent。
- 选择 Provider。
- 输入 API Key。
- 获取模型列表。
- 选择配置方式。
- 安装和写入配置。
- 测试第一次请求。

### 4.3 Configuration Tools

配置工具是独立的可选层：

- OneAgent 内置配置：默认路径。
- CC Switch：多 Provider、多账号、多 Profile 场景。
- 手动配置：高级用户和故障排查路径。

OneAgent 不把第三方配置工具打进启动包，也不默认自动安装。

### 4.4 Agent and Project Guides

文档包包含：

- Agent 安装说明。
- Agent 配置说明。
- Provider 字段说明。
- 配置工具说明。
- 第一次请求示例。
- 可运行的开源项目模板。

## 5. 用户流程

### 5.1 首次用户

```text
开始配置
→ 注册或登录 Provider
→ 创建 API Key
→ 选择 Agent
→ 选择配置工具
→ 获取模型
→ 写入配置
→ 第一次请求
→ 进入项目模板
```

### 5.2 已有 API Key 的用户

```text
输入已有 API Key
→ 获取模型
→ 选择 Agent
→ 写入配置
→ 第一次请求
```

### 5.3 多 Provider 用户

```text
完成一个可用 Profile
→ 选择 CC Switch 或手动管理
→ 保存多个 Provider Profile
→ 切换后重新启动 Agent
→ 验证当前 Profile
```

### 5.4 只安装 Agent 的用户

```text
选择 Agent
→ 只做检测或官方安装
→ 跳过 Provider 配置
→ 提示用户之后自行登录和配置
```

## 6. GUI 页面结构

### 页面 0：选择路径

```text
我还没有 Provider 账号
我已有账号但没有 API Key
我已有 API Key
我只想安装 Agent
```

### 页面 1：选择 Agent

按使用场景和配置方式分组：

- 命令行 Agent：Codex、Claude Code、OpenCode、Aider。
- IDE / 编辑器 Agent：Cursor、Cline、Continue、Qwen Code、Kilo VS Code。
- Gateway Agent：OpenClaw、Hermes。
- 其他官方账号型 Agent：Kiro、Gemini CLI。

用户可以多选，但默认只推荐少量适合当前场景的 Agent。

### 页面 2：选择 Provider

默认 Provider 为 PPIO，也可以选择 Novita 或 Custom OpenAI-compatible 服务。

配置字段统一为：

```text
Provider
Base URL
API Key
Model ID
```

### 页面 3：选择配置工具

```text
OneAgent 内置配置
适合第一次使用

CC Switch
适合多个 Profile 切换

手动配置
适合高级用户和问题排查
```

### 页面 4：获取模型

请求：

```text
GET <base-url>/v1/models
```

成功时展示模型列表；失败时显示原因并允许手动输入模型 ID。

### 页面 5：确认与执行

展示：

- 选中的 Agent。
- Provider 和 Base URL。
- 选中的模型。
- 配置工具。
- 将写入的配置文件。
- 备份策略。
- 将执行的安装命令。

### 页面 6：完成

展示：

- Provider 连接结果。
- Agent 配置结果。
- 第一次请求结果。
- 下一步命令。
- 项目模板入口。
- 配置工具后续使用说明。

## 7. Agent 分类和自动化策略

### 可自动配置

- Codex。
- Claude Code。
- OpenCode。
- Aider。

这些 Agent 只有在官方配置入口和安装源经过验证后，才允许加入自动化 allowlist。

### 只做官方引导

- OpenClaw。
- Hermes。
- Cursor。
- Kiro。
- Gemini CLI。
- Cline。
- Continue。
- Qwen Code。
- Kilo VS Code。

这类工具的官方账号、Gateway、IDE 私有状态或配置契约不稳定时，OneAgent 只显示安装和配置指引，不修改私有状态。

## 8. 软件获取策略

```text
官方安装源
→ 授权镜像
→ 用户手动安装
→ OneAgent 检测
→ 文档引导
```

如果官方源不可达，产品只能提示用户使用其所在组织或网络服务商提供的合规网络接入，或者手动安装后返回检测。OneAgent 不提供 VPN、代理、节点订阅或网络绕过功能。

## 9. API Key 和数据策略

默认模式为用户自己的 Provider 账号和 API Key：

```text
用户创建 Key
→ Key 进入本地配置流程
→ Key 写入系统环境变量、目标配置或本机 Profile
→ OneAgent 不接收服务端副本
```

禁止：

- 把 Key 放入压缩包。
- 把 Key 放进命令行参数。
- 将 Key 写入日志、截图和遥测。
- 默认上传 Prompt、源代码和完整请求。

## 10. 配置工具策略

### OneAgent 内置配置

默认、低复杂度、最容易测试。

### CC Switch

可选的本机 Profile 管理工具，适合多 Provider、多账号、多模型场景。它不属于 Agent，不承担网络访问功能，也不替代 PPIO。

### 手动配置

用于处理未支持 Agent、配置恢复和高级用户需求。只提供已验证字段和配置路径。

## 11. 分发形态

### Web 激活入口

适合公开用户，内容可以持续更新。

### 本地启动器

适合实际执行安装和配置，默认只监听本机。

### 离线压缩包

适合组织或个人离线携带脚本、文档和项目模板。压缩包是发行形式，不是产品边界。

组织发行方可以在包外提供自己的项目、兑换码或权益说明，但不得要求 OneAgent 核心内置这些内容。

## 12. V1 范围

### 包含

- PPIO、Novita、Custom Provider。
- Codex、Claude Code、OpenCode、Aider 自动配置。
- 其他 Agent 的官方安装和配置指引。
- 模型列表获取。
- 第一次请求验证。
- OneAgent 内置配置。
- CC Switch 可选文档。
- 手动配置和备份恢复。
- 通用项目模板。

### 不包含

- 组织发行方的专属业务逻辑。
- 固定免费额度承诺。
- VPN、代理和跨境网络中转。
- 共享 PPIO Key。
- 统一 API 网关。
- 未授权 Agent 包体分发。
- 默认遥测。

## 13. 核心指标

核心指标是“激活成功”，而不是下载量：

- 启动器启动。
- Provider 连接成功。
- API Key 配置完成。
- 模型列表获取成功。
- Agent 配置完成。
- 第一次请求成功。
- 第一个项目模板运行成功。

如果开启匿名统计，只记录版本、Agent、Provider、模型和结果状态，不记录 Key、Prompt 或源代码。

## 14. 验收标准

- 用户不需要组织身份、专属代码或额外权益即可理解和使用核心流程。
- 用户有 API Key 时可以快速完成配置。
- 用户没有 API Key 时可以获得清晰的 Provider 官方注册引导。
- 用户只想安装 Agent 时可以跳过 Provider 配置。
- CC Switch 是可选路径，不是隐藏依赖。
- 官方源不可达时不会引导网络绕过。
- API Key 不出现在日志、命令行、截图和服务端。
- 所有文档和启动包不包含组织发行方专属字段。
- 所有自动化 Agent 都能在临时 HOME 下测试。
