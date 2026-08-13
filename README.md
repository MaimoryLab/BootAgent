<div align="center">
  <img src="build/appicon.png" alt="BootAgent" width="96">
  <h1>BootAgent</h1>
  <p>One place to install, configure, and keep your AI coding Agents ready.</p>
  <p>
    <a href="https://github.com/MaimoryLab/BootAgent/releases/latest"><img src="https://img.shields.io/github/v/release/MaimoryLab/BootAgent?display_name=tag&sort=semver" alt="Latest release"></a>
    <a href="https://github.com/MaimoryLab/BootAgent/actions/workflows/ci.yml"><img src="https://github.com/MaimoryLab/BootAgent/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
    <a href="https://github.com/MaimoryLab/BootAgent/stargazers"><img src="https://img.shields.io/github/stars/MaimoryLab/BootAgent?style=flat" alt="GitHub stars"></a>
    <a href="LICENSE"><img src="https://img.shields.io/github/license/MaimoryLab/BootAgent" alt="License"></a>
  </p>
  <p><a href="README_ZH.md">简体中文</a></p>
</div>

BootAgent is a local desktop workspace for AI coding Agents. It turns a fresh machine into a usable, repeatable setup without asking you to edit several tool-specific config files by hand.

## What it does

- Detects, installs, updates, and launches supported CLI and desktop Agents.
- Migrates existing Codex and ChatGPT Desktop conversations into BootAgent's `bootagent` provider bucket from their Agent rows. This operation intentionally creates no history backup.
- Connects Agents to built-in or custom Providers, with model selection and protocol-aware connection checks.
- Saves reusable Profiles and applies the right configuration to each Agent.
- Bootstraps required runtimes such as Node.js, uv, and Aider's managed Python when needed.
- Keeps long-running installs visible and cancellable in the Task Center.
- Imports and exports Providers, Profiles, and selected MCP servers. API keys and MCP secrets are excluded by default; password-encrypted or explicitly confirmed plaintext export is also available.
- Discovers MCP servers from initialized Claude Code, Codex, OpenCode, Kilo CLI, and Hermes installations, and applies selected servers across them from the MCP Registry. Scanning runs in the background; edits are explicit and local.
- Creates backups, writes atomically, and keeps credentials in private local storage. The latest three historical versions are kept per Profile, Provider, MCP, Agent configuration target, and Skill; backups live under `~/.bootagent/backup` and the per-target count can be changed in Settings.
- Checks for BootAgent updates and installs release artifacts through the built-in updater.

## Supported Agents

| CLI Agents | Desktop Agents |
| --- | --- |
| Codex · Claude Code | ChatGPT Desktop（macOS/Windows） |
| Kilo CLI · Aider· OpenCode | WorkBuddy（macOS/Windows） |
| Hermes Agent · OpenClaw | |
| Kimi Code | |

JieKou.AI, PPIO, Novita, DeepSeek and Moonshot are built in, listed in that order. Any OpenAI-compatible or Anthropic-compatible Provider can be added from the Provider page. BootAgent probes the protocol an Agent actually uses; an endpoint that only exposes a different API is rejected before configuration is written.

The MCP Registry is user-level only and stored locally. It supports stdio, HTTP, and SSE servers for Claude Code, Codex, OpenCode, Kilo CLI, and Hermes. Select sync targets explicitly and apply changes when ready; clearing all targets removes the server from Agent configs but keeps it in the Registry. Deleting a server removes it from Agent configs first and from the Registry only after the application succeeds. MCP exports are selected per server and carry no Agent bindings, so they can be imported on another machine before choosing local targets.

## Download

Download the latest macOS, Windows, or Linux package from [GitHub Releases](https://github.com/MaimoryLab/BootAgent/releases/latest). Linux releases provide `deb`, `rpm`, AppImage, and OTA `zip` packages for amd64 and arm64. Release artifacts include SHA-256 checksums. The current channel is `technical-preview-unsigned` while Wails remains in Alpha; platform signing and notarization are not yet provided.

BootAgent does not redistribute Agent packages and does not bundle Node.js, Git, WebView, or API keys. Missing prerequisites are reported with a link to the official installation instructions.

## Build from source

Requirements: Go, Node.js, pnpm 11.21.0, and the target platform's WebView dependencies. Linux builds use GTK4 and WebKitGTK 6.0; GTK3 is not supported.

```text
git clone https://github.com/MaimoryLab/BootAgent.git
cd BootAgent
cd frontend && pnpm install --frozen-lockfile && pnpm run build && cd ..
go run -tags wails ./cmd/bootagent-desktop
```

For the usual development loop, install [Task](https://taskfile.dev/) and run `task dev`. Useful checks:

```text
go test ./...
go test -race ./...
go vet ./...
cd frontend && pnpm run test && pnpm run build
```

## Project links

- [AI Agent Kit](docs/ai-agent-kit/README.md): set up an Agent environment step by step
- [Documentation](docs/): specifications and architecture decisions
- [Public site repository](https://github.com/MaimoryLab/BootAgent-site)
- [Issues and feature requests](https://github.com/MaimoryLab/BootAgent/issues)

## Star History

<a href="https://www.star-history.com/?repos=MaimoryLab%2FBootAgent&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=MaimoryLab/BootAgent&type=date&theme=dark&legend=top-left&sealed_token=wiOLSOTugmyFse5tBQ7xZAHS9_V3irdr9ft2xDkbPA2rgy4SNDmm09LA6m0Umxjop30R4kn8yj675c_d5Q5NHGecjs3fB2FwpnKxVTDGomAZsz2OxbfN5ND7comOV52I39nuTN1T-zShOiDil29DAq92aduIm30ekevoULQV9mSaMacoTpsSo0O0tPPS" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=MaimoryLab/BootAgent&type=date&legend=top-left&sealed_token=wiOLSOTugmyFse5tBQ7xZAHS9_V3irdr9ft2xDkbPA2rgy4SNDmm09LA6m0Umxjop30R4kn8yj675c_d5Q5NHGecjs3fB2FwpnKxVTDGomAZsz2OxbfN5ND7comOV52I39nuTN1T-zShOiDil29DAq92aduIm30ekevoULQV9mSaMacoTpsSo0O0tPPS" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=MaimoryLab/BootAgent&type=date&legend=top-left&sealed_token=wiOLSOTugmyFse5tBQ7xZAHS9_V3irdr9ft2xDkbPA2rgy4SNDmm09LA6m0Umxjop30R4kn8yj675c_d5Q5NHGecjs3fB2FwpnKxVTDGomAZsz2OxbfN5ND7comOV52I39nuTN1T-zShOiDil29DAq92aduIm30ekevoULQV9mSaMacoTpsSo0O0tPPS" />
 </picture>
</a>

## Sponsors

<p>
  <a href="https://ppio.com/"><img src="docs/assets/sponsors/ppio-color.png" alt="PPIO" height="40"></a>
  &nbsp;&nbsp;
  <a href="https://novita.ai/"><img src="docs/assets/sponsors/novita-color.png" alt="Novita" height="40"></a>
</p>

BootAgent is released under the [Apache License 2.0](LICENSE). Third-party notices are listed in [NOTICE](NOTICE).
