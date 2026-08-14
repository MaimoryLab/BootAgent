<div align="center">
  <img src="build/appicon.png" alt="BootAgent" width="96">
  <h1>BootAgent</h1>
  <p>在一个地方安装、配置并维护你的 AI 编程 Agent。</p>
  <p>
    <a href="https://github.com/MaimoryLab/BootAgent/releases/latest"><img src="https://img.shields.io/github/v/release/MaimoryLab/BootAgent?display_name=tag&sort=semver" alt="最新版本"></a>
    <a href="https://github.com/MaimoryLab/BootAgent/actions/workflows/ci.yml"><img src="https://github.com/MaimoryLab/BootAgent/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
    <a href="https://github.com/MaimoryLab/BootAgent/stargazers"><img src="https://img.shields.io/github/stars/MaimoryLab/BootAgent?style=flat" alt="GitHub Stars"></a>
    <a href="LICENSE"><img src="https://img.shields.io/github/license/MaimoryLab/BootAgent" alt="许可证"></a>
  </p>
  <p><a href="README.md">English</a></p>
</div>

BootAgent 是一个本地桌面工作台，用来统一管理 AI 编程 Agent。它把检测、安装、模型服务配置和日常维护放进一条清晰的流程，不必再手动修改多个工具各自的配置文件。

## 核心能力

- 检测、安装、更新并启动支持的 CLI 和桌面 Agent。
- 可从 Codex 与 ChatGPT Desktop 的 Agent 行把现有对话迁入 BootAgent 的 `bootagent` Provider 分桶；此操作按设计不创建历史备份。
- 连接内置或自定义模型服务（Provider），选择模型，并按 Agent 实际协议检查连接。
- 保存可复用的配置模版（Profile），一键应用到对应 Agent。
- 把 Profile 上的思考深度（`off`、`low`、`medium`、`high`、`max`）写入每个自身配置格式有对应位置的 Agent，并按各自接受的取值做换算。没有文档化深度设置的 Agent 保持原样，不会被塞进自造的字段。
- 按需准备 Node.js、uv，以及 Aider 所需的托管 Python 运行时。
- 长时间安装任务在任务中心持续可见，并且可以取消。
- 导入和导出 Provider、Profile 以及选中的 MCP 服务器；默认不导出 API Key 和 MCP 秘密，也支持密码加密或明确确认后的明文导出。
- 从已初始化的 Claude Code、Codex、OpenCode、Kilo CLI 和 Hermes 中发现 MCP 服务器，并在 MCP Registry 页面选择同步目标。扫描在后台进行，编辑必须显式应用并只写入本机。
- 创建备份、原子写入，并将凭据保存在本机私有存储中。Profile、Provider、MCP、Agent 配置目标和 Skill 分别保留最近 3 个历史版本；备份统一放在 `~/.bootagent/backup`，可在“设置”中修改每个目标的保留数量。
- 检查 BootAgent 更新，并通过内置更新器安装发行包。

## 支持的 Agent

| CLI Agent | 桌面 Agent |
| --- | --- |
| Codex · Claude Code | ChatGPT Desktop（macOS/Windows） |
| Kilo CLI · Aider · OpenCode | WorkBuddy · WorkBuddy AI（macOS/Windows） |
| Hermes Agent · OpenClaw | ZCode（macOS/Windows） |
| Kimi Code · DeepSeek Harness | |

内置 JieKou.AI、PPIO、Novita、DeepSeek 和 Moonshot，顺序如此；也可以在模型服务页面添加任意 OpenAI 兼容或 Anthropic 兼容服务。BootAgent 会按 Agent 真正使用的协议探测；如果端点只支持另一种 API，会在写入配置前拒绝它。

DeepSeek Harness 除了走通用的 OpenAI 兼容端点，也可以直接激活到 DeepSeek 自己的官方线路——该线路由它出厂配置本身定义。

MCP Registry 只管理用户级配置并保存在本机，支持 Claude Code、Codex、OpenCode、Kilo CLI 和 Hermes 的 stdio、HTTP、SSE 服务器。用户可以单独选择同步目标并显式应用；清空所有同步目标只会从 Agent 配置中移除服务器，仍保留在 Registry 中。点击删除后，会先从 Agent 配置中移除，应用成功后才从 Registry 中删除。MCP 导出按服务器选择且不携带 Agent 绑定，导入其他机器后可重新选择本机同步目标。

## 下载

从 [GitHub Releases](https://github.com/MaimoryLab/BootAgent/releases/latest) 下载最新的 macOS、Windows 或 Linux 包。Linux 发行包为 amd64 和 arm64 提供 `deb`、`rpm`、AppImage 以及 OTA `zip`。发行包附带 SHA-256 校验文件。Wails 仍处于 Alpha，当前发布渠道为 `technical-preview-unsigned`，暂不提供平台签名和公证。

BootAgent 不重新分发 Agent 包，也不捆绑 Node.js、Git、WebView 或 API Key。缺少前置条件时，应用会给出明确错误和官方安装指引。

## 从源码构建

需要 Go、Node.js、pnpm 11.21.0，以及目标平台的 WebView 依赖。Linux 构建使用 GTK4 和 WebKitGTK 6.0，不支持 GTK3。

```text
git clone https://github.com/MaimoryLab/BootAgent.git
cd BootAgent
cd frontend && pnpm install --frozen-lockfile && pnpm run build && cd ..
go run -tags wails ./cmd/bootagent-desktop
```

日常开发可安装 [Task](https://taskfile.dev/) 后运行 `task dev`。常用检查：

```text
go test ./...
go test -race ./...
go vet ./...
cd frontend && pnpm run test && pnpm run build
```

## 项目链接

- [AI Agent Kit](docs/ai-agent-kit/zh/README.md)：从零配置 Agent 环境
- [文档](docs/)：规范与架构决策
- [公开站仓库](https://github.com/MaimoryLab/BootAgent-site)
- [Issues 与功能请求](https://github.com/MaimoryLab/BootAgent/issues)

## Star History

<a href="https://www.star-history.com/?repos=MaimoryLab%2FBootAgent&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=MaimoryLab/BootAgent&type=date&theme=dark&legend=top-left&sealed_token=wiOLSOTugmyFse5tBQ7xZAHS9_V3irdr9ft2xDkbPA2rgy4SNDmm09LA6m0Umxjop30R4kn8yj675c_d5Q5NHGecjs3fB2FwpnKxVTDGomAZsz2OxbfN5ND7comOV52I39nuTN1T-zShOiDil29DAq92aduIm30ekevoULQV9mSaMacoTpsSo0O0tPPS" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=MaimoryLab/BootAgent&type=date&legend=top-left&sealed_token=wiOLSOTugmyFse5tBQ7xZAHS9_V3irdr9ft2xDkbPA2rgy4SNDmm09LA6m0Umxjop30R4kn8yj675c_d5Q5NHGecjs3fB2FwpnKxVTDGomAZsz2OxbfN5ND7comOV52I39nuTN1T-zShOiDil29DAq92aduIm30ekevoULQV9mSaMacoTpsSo0O0tPPS" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=MaimoryLab/BootAgent&type=date&legend=top-left&sealed_token=wiOLSOTugmyFse5tBQ7xZAHS9_V3irdr9ft2xDkbPA2rgy4SNDmm09LA6m0Umxjop30R4kn8yj675c_d5Q5NHGecjs3fB2FwpnKxVTDGomAZsz2OxbfN5ND7comOV52I39nuTN1T-zShOiDil29DAq92aduIm30ekevoULQV9mSaMacoTpsSo0O0tPPS" />
 </picture>
</a>

## 赞助

<p>
  <a href="https://ppio.com/"><img src="docs/assets/sponsors/ppio-color.png" alt="PPIO" height="40"></a>
  &nbsp;&nbsp;
  <a href="https://novita.ai/"><img src="docs/assets/sponsors/novita-color.png" alt="Novita" height="40"></a>
</p>

BootAgent 以 [Apache License 2.0](LICENSE) 发布，第三方归属见 [NOTICE](NOTICE)。
