<div align="center">
  <img src="build/appicon.png" alt="OneAgent" width="96">
  <h1>OneAgent</h1>
  <p>在一个地方安装、配置并维护你的 AI 编程 Agent。</p>
  <p>
    <a href="https://github.com/MaimoryLab/OneAgent/releases/latest"><img src="https://img.shields.io/github/v/release/MaimoryLab/OneAgent?display_name=tag&sort=semver" alt="最新版本"></a>
    <a href="https://github.com/MaimoryLab/OneAgent/actions/workflows/ci.yml"><img src="https://github.com/MaimoryLab/OneAgent/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
    <a href="https://github.com/MaimoryLab/OneAgent/stargazers"><img src="https://img.shields.io/github/stars/MaimoryLab/OneAgent?style=flat" alt="GitHub Stars"></a>
    <a href="LICENSE"><img src="https://img.shields.io/github/license/MaimoryLab/OneAgent" alt="许可证"></a>
  </p>
  <p><a href="README.md">English</a></p>
</div>

OneAgent 是一个本地桌面工作台，用来统一管理 AI 编程 Agent。它把检测、安装、模型服务配置和日常维护放进一条清晰的流程，不必再手动修改多个工具各自的配置文件。

## 核心能力

- 检测、安装、更新并启动支持的 CLI 和桌面 Agent。
- 连接内置或自定义模型服务（Provider），选择模型，并按 Agent 实际协议检查连接。
- 保存可复用的配置模版（Profile），一键应用到对应 Agent。
- 按需准备 Node.js、uv，以及 Aider 所需的托管 Python 运行时。
- 长时间安装任务在任务中心持续可见，并且可以取消。
- 导入和导出 Provider、Profile；默认不导出 API Key，也支持密码加密导出。
- 创建备份、原子写入，并将凭据保存在本机私有存储中。
- 检查 OneAgent 更新，并通过内置更新器安装发行包。

## 支持的 Agent

| CLI Agent | 桌面 Agent |
| --- | --- |
| Codex · Claude Code | ChatGPT Desktop（macOS/Windows） |
| Kilo CLI · Aider· OpenCode | WorkBuddy（macOS/Windows） |
| Hermes Agent · OpenClaw | |
| Kimi Code | |

内置 PPIO 和 Novita，也可以在模型服务页面添加任意 OpenAI 兼容或 Anthropic 兼容服务。OneAgent 会按 Agent 真正使用的协议探测；如果端点只支持另一种 API，会在写入配置前拒绝它。

## 下载

从 [GitHub Releases](https://github.com/MaimoryLab/OneAgent/releases/latest) 下载最新的 macOS 或 Windows 安装包。发行包附带 SHA-256 校验文件。Wails 仍处于 Alpha，当前发布渠道为 `technical-preview-unsigned`，暂不提供平台签名和公证。

OneAgent 不重新分发 Agent 包，也不捆绑 Node.js、Git、WebView 或 API Key。缺少前置条件时，应用会给出明确错误和官方安装指引。

## 从源码构建

需要 Go、Node.js、pnpm 11.21.0，以及目标平台的 WebView 依赖。

```text
git clone https://github.com/MaimoryLab/OneAgent.git
cd OneAgent
cd frontend && pnpm install --frozen-lockfile && pnpm run build && cd ..
go run -tags wails ./cmd/oneagent-desktop
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
- [公开站仓库](https://github.com/MaimoryLab/OneAgent-site)
- [Issues 与功能请求](https://github.com/MaimoryLab/OneAgent/issues)

## Star History

<a href="https://www.star-history.com/?type=date&repos=MaimoryLab%2FOneAgent">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=MaimoryLab/OneAgent&type=date&theme=dark&legend=top-left&sealed_token=84lSKNkgdwfnYMfYU-oYgjwZ_hAFohYbDV5eeoXC_1lQvIsQnaD9EW37_C6-_seReMYRMGKR7G3W_APuS4xO13KlMBwwPHZ-_wtA04c4MxouycuOV7gip89Hd-BFzTAiz1lqDcHOxb7-X6zZRxKElZpRpC-VXe1pWUL8vp_gu9qq9OKkeA-fMShYgEqI" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=MaimoryLab/OneAgent&type=date&legend=top-left&sealed_token=84lSKNkgdwfnYMfYU-oYgjwZ_hAFohYbDV5eeoXC_1lQvIsQnaD9EW37_C6-_seReMYRMGKR7G3W_APuS4xO13KlMBwwPHZ-_wtA04c4MxouycuOV7gip89Hd-BFzTAiz1lqDcHOxb7-X6zZRxKElZpRpC-VXe1pWUL8vp_gu9qq9OKkeA-fMShYgEqI" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=MaimoryLab/OneAgent&type=date&legend=top-left&sealed_token=84lSKNkgdwfnYMfYU-oYgjwZ_hAFohYbDV5eeoXC_1lQvIsQnaD9EW37_C6-_seReMYRMGKR7G3W_APuS4xO13KlMBwwPHZ-_wtA04c4MxouycuOV7gip89Hd-BFzTAiz1lqDcHOxb7-X6zZRxKElZpRpC-VXe1pWUL8vp_gu9qq9OKkeA-fMShYgEqI" />
 </picture>
</a>
</div>

## 赞助

<p>
  <a href="https://ppio.com/"><img src="docs/assets/sponsors/ppio-color.png" alt="PPIO" height="40"></a>
  &nbsp;&nbsp;
  <a href="https://novita.ai/"><img src="docs/assets/sponsors/novita-color.png" alt="Novita" height="40"></a>
</p>

OneAgent 以 [Apache License 2.0](LICENSE) 发布，第三方归属见 [NOTICE](NOTICE)。
