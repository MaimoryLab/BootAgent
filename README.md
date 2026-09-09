<div align="center">
  <img src="build/appicon.png" alt="BootAgent" width="96">
  <h1>BootAgent</h1>
  <p><strong>Many Agents. One place to manage them.</strong></p>
  <p>
    <a href="https://github.com/MaimoryLab/BootAgent/releases/latest"><img src="https://img.shields.io/github/v/release/MaimoryLab/BootAgent?display_name=tag&amp;sort=semver" alt="Latest release"></a>
    <a href="https://github.com/MaimoryLab/BootAgent/actions/workflows/ci.yml"><img src="https://github.com/MaimoryLab/BootAgent/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
    <a href="https://github.com/MaimoryLab/BootAgent/stargazers"><img src="https://img.shields.io/github/stars/MaimoryLab/BootAgent?style=flat" alt="GitHub stars"></a>
    <a href="LICENSE"><img src="https://img.shields.io/github/license/MaimoryLab/BootAgent" alt="License"></a>
  </p>
  <p>
    <a href="https://github.com/MaimoryLab/BootAgent/releases/latest">Download the latest release</a>
    · <a href="docs/ai-agent-kit/en/00-start-here.md">Start in five minutes</a>
    · <a href="README_ZH.md">简体中文</a>
  </p>
</div>

<p align="center">
  <a href="https://www.producthunt.com/products/bootagent?embed=true&amp;utm_source=badge-featured&amp;utm_medium=badge&amp;utm_campaign=badge-bootagent" target="_blank" rel="noopener noreferrer"><img alt="BootAgent on Product Hunt" width="250" height="54" src="https://api.producthunt.com/widgets/embed-image/v1/featured.svg?post_id=1225464&amp;theme=light&amp;t=1787018185057"></a>
</p>

BootAgent is a local desktop workspace for AI coding Agents. It helps you detect what is
already on your machine, connect a Provider, configure an Agent, and get to a first
successful request without editing several tool-specific files by hand.

## Start here

| You want to... | Start with... |
| --- | --- |
| Try an Agent for the first time | [Download a release](https://github.com/MaimoryLab/BootAgent/releases/latest), then follow the [AI Agent Kit](docs/ai-agent-kit/en/00-start-here.md) |
| Keep existing Agents in one place | Open **Environment overview** and let BootAgent scan the machine |
| Find a Skill, MCP server, plugin, or AI product | Open **Marketplace**, search or filter, then open an item for its details and upstream links |
| Move a setup to another machine | Open **Settings > Import and export** and choose a v1 JSON file or a v2 bundle |
| Fix an install or update | Open **Task Center** for the source, progress, and diagnostic output |

The shortest first-run path is:

```text
Launch BootAgent
→ Choose or add a Provider
→ Create an API Key in that Provider's official account page
→ Choose an Agent and a Profile
→ Run setup
→ Send a first request
```

## What you can do

### Manage Agents

- Detect, install, update, launch, and uninstall supported CLI and desktop Agents.
- Detect Agents installed outside BootAgent when their command or known installation path is available.
- See long-running install and update tasks in Task Center, including progress, source, and cancellable steps.
- Uninstall selected installation instances while keeping Profiles, Providers, configuration files, and conversations.

### Connect Providers and Profiles

- Use built-in Providers or add an OpenAI-compatible or Anthropic-compatible endpoint.
- Check an endpoint using the protocol that the selected Agent actually uses.
- Save reusable Profiles and select the Profile and model from an Agent's configuration screen.
- Keep API Keys in private local storage; they are not placed in ordinary summaries or recommendation prompts.

### Manage Skills and MCP servers

- Keep a local Skill library, scan Skills already present on supported Agents, and choose which Agents receive each Skill.
- Discover MCP servers from initialized Claude Code, Codex, OpenCode, Kilo CLI, and Hermes installations.
- Store MCP servers in a local user-level Registry, select sync targets explicitly, and apply changes only when you are ready.

### Local utilities

- Bootstrap Node.js, uv, and Aider's managed Python runtime when an Agent needs them.
- Optionally run local API format conversion and launch BootAgent at login; both features are off by default.
- Migrate existing Codex and ChatGPT Desktop conversations into BootAgent's local provider bucket. This migration intentionally creates no history backup.

### Discover tools in Marketplace

- Browse Skills, MCP servers, plugins, standalone AI products, prompt collections, and workflow templates.
- Combine category, source, use-case, API-key, and multi-type filters. An item can belong to more than one type.
- Refresh supported public sources independently. A source failure keeps the bundled catalog available instead of blanking the market.
- Open a detail page for the description, install guidance, README when the source provides one, and upstream links.
- Ask an installed Codex or Claude Code CLI to shortlist tools for a stated need. Returned IDs are checked against the current catalog before display.
- Save recommendation requests and result snapshots locally so a previous search can be reopened later. Recommendation history is never uploaded as telemetry.

### Keep setups portable

- Import and export Providers, Profiles, selected MCP servers, and Skills.
- Legacy v1 JSON files remain importable. The v2 portable bundle separates configuration JSON from a Skills ZIP.
- Skills first enter BootAgent's managed library; you choose target Agents from the Skills page afterward.
- Preview new, overwritten, and conflicting resources before applying an import. Writes use snapshots and rollback on failure.
- API Keys and MCP secrets are omitted by default. Password-encrypted export or explicitly confirmed plaintext export is available.

## Supported Agents

BootAgent supports detection, configuration, launch, or installation guidance according to each Agent's upstream contract. An entry below does not mean that BootAgent redistributes the Agent package; the detail page shows the available install source.

| CLI and local Agents | Desktop Agents |
| --- | --- |
| Codex · Claude Code · OpenCode | DSH Desktop · Claude Desktop |
| Kilo CLI · Aider · OpenClaw | ChatGPT Desktop · WorkBuddy |
| Hermes Agent · Kimi Code · Pi | WorkBuddy AI · ZCode |
| DeepSeek Harness (local web app) | |

Claude Desktop can be detected and launched on macOS and Windows, but it must be installed by the user from the [official download page](https://claude.com/download). BootAgent writes compatible configuration and does not download or proxy Claude Desktop.

DeepSeek Harness can use its shipped DeepSeek route or a configured compatible Provider. It opens a local web app after installation; its own onboarding commands remain the source of truth.

JieKou.AI, PPIO, Novita, DeepSeek, and Moonshot are built-in Providers. Custom OpenAI-compatible and Anthropic-compatible Providers can be added from the Provider page.

## Download

Download the latest package from [GitHub Releases](https://github.com/MaimoryLab/BootAgent/releases/latest).

| Platform | Recommended package | Architectures |
| --- | --- | --- |
| macOS | DMG | Intel and Apple Silicon |
| Windows | NSIS installer | x64 and ARM64 |
| Linux | AppImage, deb, or rpm | amd64 and arm64 |

Linux also provides an OTA ZIP archive. Release artifacts include `SHA256SUMS`. macOS packages are Developer ID signed and notarized. The release channel is still a Wails Alpha technical preview: Windows and Linux packages are currently unsigned and may require an operating-system approval on first launch.

BootAgent does not redistribute Agent packages and does not bundle Node.js, Git, WebView, or API Keys. Missing prerequisites are reported with a link to the relevant official installation instructions.

## Data, privacy, and recovery

- BootAgent is local-first. Provider credentials, configuration, recommendation history, and backups stay on the machine by default.
- Recommendation prompts contain the stated need and public catalog metadata only; the recommendation process receives no install or file-write tools.
- The latest three historical versions are kept for each Profile, Provider, MCP, Agent configuration target, and Skill by default. Backups live under `~/.bootagent/backup`; retention can be changed in Settings.
- Uninstall removes the selected program instance, not the user's Profiles, Providers, configuration files, or conversations.
- BootAgent is not a VPN, proxy, shared-key service, or Agent package distributor. Downloads use official sources, authorized mirrors, or documented manual installation paths.

## Troubleshooting

| Symptom | First check |
| --- | --- |
| Agent is not listed | Refresh Environment overview, check that the command is on `PATH`, or use the Agent's documented manual install path. |
| Provider connection check fails | Confirm the endpoint and model match the Agent's protocol; an endpoint that exposes only another API is rejected before writing configuration. |
| Marketplace looks incomplete | Check the source status and refresh. A bundled snapshot remains available when an online source is unavailable. |
| Import reports a conflict | Review the preview, choose skip or overwrite, and apply only after checking the affected resource names. |
| BootAgent cannot check for an update | In Settings, switch between GitHub and the domestic Gitee mirror, then retry. Existing installation files are not replaced by a failed check. |
| The Agent will not download | Follow the official install page or install manually, then return to BootAgent to detect the local installation. BootAgent does not bypass network restrictions. |

## Build from source

For users, the release packages above are the supported installation path. For development, you need Go (the version declared in `go.mod`), Node.js, pnpm `11.21.0`, and the target platform's WebView dependencies. Linux builds use GTK4 and WebKitGTK 6.0.

```text
git clone https://github.com/MaimoryLab/BootAgent.git
cd BootAgent
cd frontend
pnpm install --frozen-lockfile
pnpm run build
cd ..
go run -tags wails ./cmd/bootagent-desktop
```

For a production binary on the current host, use `task build:desktop`. Useful checks are:

```text
go test ./...
go test -race ./...
go vet ./...
cd frontend && pnpm run test && pnpm run build
```

## Project links

- [AI Agent Kit](docs/ai-agent-kit/README.md): guided setup for a first request
- [Documentation](docs/): specifications and architecture decisions
- [Security policy](SECURITY.md)
- [Contributing](CONTRIBUTING.md)
- [Public site repository](https://github.com/MaimoryLab/BootAgent-site)
- [Issues and feature requests](https://github.com/MaimoryLab/BootAgent/issues)

## Star History

<a href="https://www.star-history.com/?repos=MaimoryLab%2FBootAgent&type=date&legend=top-left">
  <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=MaimoryLab/BootAgent&type=Date">
</a>

## Sponsors

<p>
  <a href="https://ppio.com/"><img src="docs/assets/sponsors/ppio-color.png" alt="PPIO" height="40"></a>
  &nbsp;&nbsp;
  <a href="https://novita.ai/"><img src="docs/assets/sponsors/novita-color.png" alt="Novita" height="40"></a>
</p>

BootAgent is released under the [Apache License 2.0](LICENSE). Third-party notices are listed in [NOTICE](NOTICE).
