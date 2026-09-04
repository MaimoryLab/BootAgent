<div align="center">
  <img src="build/appicon.png" alt="BootAgent" width="96">
  <h1>BootAgent</h1>
  <p><strong>Agent 很多，管理只需一个。</strong></p>
  <p>
    <a href="https://github.com/MaimoryLab/BootAgent/releases/latest"><img src="https://img.shields.io/github/v/release/MaimoryLab/BootAgent?display_name=tag&amp;sort=semver" alt="最新版本"></a>
    <a href="https://github.com/MaimoryLab/BootAgent/actions/workflows/ci.yml"><img src="https://github.com/MaimoryLab/BootAgent/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
    <a href="https://github.com/MaimoryLab/BootAgent/stargazers"><img src="https://img.shields.io/github/stars/MaimoryLab/BootAgent?style=flat" alt="GitHub Stars"></a>
    <a href="LICENSE"><img src="https://img.shields.io/github/license/MaimoryLab/BootAgent" alt="许可证"></a>
  </p>
  <p>
    <a href="https://github.com/MaimoryLab/BootAgent/releases/latest">下载最新版本</a>
    · <a href="docs/ai-agent-kit/zh/00-start-here.md">五分钟开始使用</a>
    · <a href="README.md">English</a>
  </p>
</div>

<p align="center">
  <a href="https://www.producthunt.com/products/bootagent?embed=true&amp;utm_source=badge-featured&amp;utm_medium=badge&amp;utm_campaign=badge-bootagent" target="_blank" rel="noopener noreferrer"><img alt="BootAgent on Product Hunt" width="250" height="54" src="https://api.producthunt.com/widgets/embed-image/v1/featured.svg?post_id=1225464&amp;theme=light&amp;t=1787018185057"></a>
</p>

BootAgent 是一个本地桌面工作台，用来统一管理 AI 编程 Agent。它可以检测电脑上已有的工具，连接模型服务，配置 Agent，并帮助你完成第一次成功的请求，不必手动修改多个工具各自的配置文件。

## 从这里开始

| 你想做什么 | 从哪里开始 |
| --- | --- |
| 第一次尝试 Agent | [下载发行版](https://github.com/MaimoryLab/BootAgent/releases/latest)，再阅读 [AI Agent Kit](docs/ai-agent-kit/zh/00-start-here.md) |
| 把已有 Agent 集中管理 | 打开“环境总览”，让 BootAgent 扫描本机 |
| 查找 Skill、MCP、插件或独立 AI 产品 | 打开“工具市场”，搜索或筛选后进入详情页查看安装说明和上游链接 |
| 把环境迁移到另一台电脑 | 打开“设置 > 导入导出”，选择 v1 JSON 或 v2 压缩包 |
| 排查安装或更新问题 | 打开“任务中心”，查看来源、进度和诊断日志 |

第一次使用可以按这条最短路径操作：

```text
启动 BootAgent
→ 选择或添加 Provider
→ 在 Provider 官方账号页面创建 API Key
→ 选择 Agent 和 Profile
→ 开始配置
→ 发起第一次请求
```

## 你可以用 BootAgent 做什么

### 管理 Agent

- 检测、安装、更新、启动和卸载支持的 CLI 与桌面 Agent。
- 只要命令或已知安装路径可发现，也可以识别不是由 BootAgent 安装的 Agent。
- 在任务中心查看长时间安装和更新任务的进度、来源及可取消步骤。
- 选择具体安装实例卸载，同时保留 Profile、Provider、配置文件和对话。

### 连接 Provider 与 Profile

- 使用内置 Provider，或添加 OpenAI 兼容、Anthropic 兼容的服务端点。
- 按所选 Agent 实际使用的协议检查连接，避免把错误协议写进配置。
- 保存可复用的 Profile，并在 Agent 配置页选择 Profile 和模型。
- API Key 保存在本机私有存储中，不会出现在普通摘要或推荐提示词里。

### 管理 Skills 和 MCP 服务器

- 在本机维护 Skill 管理库，扫描支持的 Agent 已有的 Skill，并选择每个 Skill 要同步到哪些 Agent。
- 从已初始化的 Claude Code、Codex、OpenCode、Kilo CLI 和 Hermes 中发现 MCP 服务器。
- 使用本机用户级 Registry 保存 MCP 服务器，明确选择同步目标，并在确认后再应用改动。

### 本地工具

- 当 Agent 需要时，按需准备 Node.js、uv 和 Aider 所需的托管 Python 运行时。
- 可选启用本地 API 格式转换和开机启动；两项功能默认关闭。
- 将已有 Codex 和 ChatGPT Desktop 对话迁移到 BootAgent 的本地 Provider 分桶；此迁移按设计不创建历史备份。

### 在工具市场发现工具

- 浏览 Skill、MCP 服务器、插件、独立 AI 产品、提示词集合和工作流模板。
- 组合工具类型、来源、使用场景、API Key 和多类型条件进行筛选；一个条目可以同时属于多个类型。
- 支持的公开来源可以分别刷新；某个在线来源失败时，内置目录仍然保留，不会让整个市场变空。
- 详情页展示介绍、安装指南、来源提供的 README 和上游链接。
- 可以让本机的 Codex 或 Claude Code CLI 根据需求筛选工具；展示前会逐项核对返回的工具 ID。
- 推荐请求和结果快照保存在本机，之后可以重新打开历史记录；推荐历史不会作为遥测上传。

### 安全迁移和统一管理

- 导入导出 Provider、Profile、选中的 MCP 服务器和 Skill。
- 继续兼容旧版 v1 JSON；v2 便携压缩包把配置 JSON 与 Skills ZIP 分开保存。
- Skill 会先导入 BootAgent 的管理库，之后在 Skills 页面选择要同步的 Agent，不会直接覆盖 Agent 目录。
- 导入前可以预览新增、覆盖和冲突资源；写入使用快照，失败时自动回滚。
- 默认不导出 API Key 和 MCP 秘密，也支持密码加密导出或明确确认后的明文导出。

## 支持的 Agent

BootAgent 会根据每个 Agent 的官方约定提供检测、配置、启动、安装或安装指引。出现在列表中不代表 BootAgent 会重新分发该工具包，详情页会显示实际可用的安装来源。

| CLI 和本地 Agent | 桌面 Agent |
| --- | --- |
| Codex · Claude Code · OpenCode | DSH Desktop · Claude Desktop |
| Kilo CLI · Aider · OpenClaw | ChatGPT Desktop · WorkBuddy |
| Hermes Agent · Kimi Code · Pi | WorkBuddy AI · ZCode |
| DeepSeek Harness（本地 Web 应用） | |

Claude Desktop 可以在 macOS 和 Windows 上被检测和启动，但需要用户从[官方下载页面](https://claude.com/download)自行安装。BootAgent 只写入兼容配置，不下载也不代理 Claude Desktop。

DeepSeek Harness 可以使用自带的 DeepSeek 官方线路，也可以使用已配置的兼容 Provider。安装后它会打开本地 Web 应用，具体引导命令以其官方文档为准。

内置 Provider 包括 JieKou.AI、PPIO、Novita、DeepSeek 和 Moonshot；也可以在 Provider 页面添加自定义的 OpenAI 兼容或 Anthropic 兼容服务。

## 下载

从 [GitHub Releases](https://github.com/MaimoryLab/BootAgent/releases/latest) 下载最新版本。

| 平台 | 推荐包 | 架构 |
| --- | --- | --- |
| macOS | DMG | Intel 和 Apple Silicon |
| Windows | NSIS 安装包 | x64 和 ARM64 |
| Linux | AppImage、deb 或 rpm | amd64 和 arm64 |

Linux 还提供 OTA ZIP。发行附件包含 `SHA256SUMS` 校验文件。macOS 包使用 Developer ID 签名并经过公证。当前发布渠道仍处于 Wails Alpha 技术预览阶段：Windows 和 Linux 包暂未签名，首次启动时可能需要在系统中手动允许。

BootAgent 不重新分发 Agent 包，也不捆绑 Node.js、Git、WebView 或 API Key。缺少前置条件时，应用会给出对应的官方安装指引。

## 数据、隐私与恢复

- BootAgent 以本地存储为主。Provider 凭据、配置、推荐历史和备份默认只保存在本机。
- 推荐提示词只包含用户提出的需求和公开目录元数据；推荐进程没有安装或写文件工具。
- 每个 Profile、Provider、MCP、Agent 配置目标和 Skill 默认保留最近 3 个历史版本。备份位于 `~/.bootagent/backup`，可在“设置”中修改保留数量。
- 卸载只移除选中的程序实例，不会删除用户的 Profile、Provider、配置文件或对话。
- BootAgent 不是 VPN、代理、共享 Key 服务或 Agent 软件包分发平台。下载使用官方来源、授权镜像或文档化的手动安装路径。

## 常见问题

| 现象 | 先检查什么 |
| --- | --- |
| 找不到 Agent | 刷新“环境总览”，确认命令在 `PATH` 中，或按 Agent 官方指引手动安装后再次检测。 |
| Provider 连接检查失败 | 确认端点和模型与 Agent 使用的协议一致；只支持另一种 API 的端点会在写入前被拒绝。 |
| 工具市场内容不完整 | 查看来源状态并点击刷新；在线来源不可用时仍会显示内置目录。 |
| 导入提示冲突 | 查看导入预览，选择跳过或覆盖，并确认受影响的资源名称。 |
| BootAgent 检查更新失败 | 在“设置”中切换 GitHub 与国内 Gitee 镜像后重试；检查失败不会替换现有安装文件。 |
| Agent 下载失败 | 打开官方安装页面或手动安装，再回到 BootAgent 检测本机路径。BootAgent 不提供绕过网络限制的方式。 |

## 从源码构建

普通用户建议直接使用上面的发行包。开发需要 Go（版本以 `go.mod` 为准）、Node.js、pnpm `11.21.0` 和目标平台的 WebView 依赖。Linux 构建使用 GTK4 和 WebKitGTK 6.0。

```text
git clone https://github.com/MaimoryLab/BootAgent.git
cd BootAgent
cd frontend
pnpm install --frozen-lockfile
pnpm run build
cd ..
go run -tags wails ./cmd/bootagent-desktop
```

要在当前主机生成生产二进制，运行 `task build:desktop`。常用检查命令：

```text
go test ./...
go test -race ./...
go vet ./...
cd frontend && pnpm run test && pnpm run build
```

## 项目链接

- [AI Agent Kit](docs/ai-agent-kit/README.md)：从零完成第一次请求
- [文档](docs/)：规范与架构决策
- [安全策略](SECURITY.md)
- [贡献指南](CONTRIBUTING.md)
- [公开站仓库](https://github.com/MaimoryLab/BootAgent-site)
- [Issues 与功能请求](https://github.com/MaimoryLab/BootAgent/issues)

## Star History

<a href="https://www.star-history.com/?repos=MaimoryLab%2FBootAgent&type=date&legend=top-left">
  <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=MaimoryLab/BootAgent&type=Date">
</a>

## 赞助

<p>
  <a href="https://ppio.com/"><img src="docs/assets/sponsors/ppio-color.png" alt="PPIO" height="40"></a>
  &nbsp;&nbsp;
  <a href="https://novita.ai/"><img src="docs/assets/sponsors/novita-color.png" alt="Novita" height="40"></a>
</p>

BootAgent 以 [Apache License 2.0](LICENSE) 发布，第三方归属见 [NOTICE](NOTICE)。
