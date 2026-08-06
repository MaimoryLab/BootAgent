# OneAgent

**English** · [简体中文](README_ZH.md)

OneAgent is a local AI development environment activator. A React wizard and a pure Go
CLI share the same Go use cases: detecting agents, installing the latest version, probing
Providers, merging configuration, creating backups, and tightening permissions. The
desktop app uses Wails v3 bindings, and the production process listens on no application
TCP port.

OneAgent does not redistribute agent packages, and it bundles no Node.js, system WebView,
Git, or API key. When a prerequisite is missing it returns a specific error and points at
the official install instructions.

## Current status

The current version is `0.3.0-dev`. Wails is still in Alpha, so the only release channel
is `technical-preview-unsigned`.

The Python migration is complete: the previous implementation, its tests, and the
PyInstaller/wheel packaging chain are all deleted. Building, testing, running, and
releasing need only Go, Node, pnpm 11.17.0 (to build the frontend), and the target
platform's WebView. Installing Aider needs Python 3.12, but no longer needs one
preinstalled — `uv` resolves it, reusing a matching local interpreter or downloading a
managed CPython into `~/.oneagent/runtimes/python`. Python never ships in a release
package.

## Architecture

```text
React + TypeScript + Vite
          |
          | generated Wails bindings
          v
Status / Provider / Agent / Profile services
          |
          v
      Go application use cases
          |
  catalog / provider / install / config / profile / securefs

Pure Go CLI --------------------^
```

The public website is neither in this diagram nor in this repository. It moved to
[MaimoryLab/OneAgent-site](https://github.com/MaimoryLab/OneAgent-site), reads download
information from the GitHub Releases API, and vendors `agents.lock.json` and
`providers.lock.json` into its own repository, refreshed from release tags. Changing
those two files here does not change the site, and should not: the site describes what a
published version supports.

- `cmd/oneagent-desktop`: the Wails desktop entry point.
- `cmd/oneagent`: the pure Go headless CLI.
- `internal/`: the Go core shared by desktop and CLI.
- `frontend/`: the React app. Release packages carry only its built static assets.
- `agents.lock.json`: the single manifest of agent package names, sources, config
  adapters, and licences. It pins neither agent versions nor package hashes.
- `providers.lock.json`: built-in Provider endpoints, fallback probe models, and the
  disclosure fields the public site reads. User-defined Providers live on the machine in
  `~/.oneagent/providers.json`.

## Quick start

### Desktop app

```bash
cd frontend
pnpm install --frozen-lockfile
pnpm run build
cd ..
go run -tags wails ./cmd/oneagent-desktop
```

A production build needs the target platform's Wails/WebView dependencies. Linux
currently uses the `gtk3` tag (Ubuntu 22.04 cleanroom), macOS uses the system WKWebView,
and Windows uses the WebView2 Runtime.

### CLI

```bash
go build -o bin/oneagent ./cmd/oneagent
```

On Windows CMD:

```cmd
go build -o bin\oneagent.exe .\cmd\oneagent
bin\oneagent.exe agent set codex --provider ppio --model your-model-id --api-key your-api-key
```

Day to day, pass credentials by pasting into the desktop app or from a saved profile.
`ONEAGENT_API_KEY` and `--api-key` exist for controlled scripts. `--registry` defaults to
the official npm registry; a mirror must be chosen explicitly and must use HTTPS.

## Agents and Providers

Automatically configured agents:

| Agent | Package | Installer | Protocol |
| --- | --- | --- | --- |
| Codex | `@openai/codex` | npm | Responses |
| Claude Code | `@anthropic-ai/claude-code` | npm | Anthropic Messages |
| OpenCode | `opencode-ai` | npm | OpenAI-compatible |
| Kilo CLI | `@kilocode/cli` | npm | OpenAI-compatible |
| Aider | `aider-chat` | uv tool | OpenAI-compatible |
| OpenClaw | `openclaw` | npm | OpenAI-compatible |

By default the installer lets npm or uv resolve the latest version. To reproduce a
specific one, pass `--agent-version VERSION`, for example
`oneagent --agent codex --install-agent --check-agent-only --agent-version 0.145.0`.

OpenClaw is a gateway, and OneAgent's scope for it stops at the model provider. It
installs the package and writes the provider and default model into
`~/.openclaw/openclaw.json`, leaving `channels`, `tools`, and every other section
untouched. Starting the gateway, registering it as a service, and pairing chat
channels stay with OpenClaw's own commands: run `openclaw onboard` afterwards.
OneAgent never starts a background service.

PPIO and Novita are built in, and Providers can be added, edited, or removed on the
Provider page. After configuration, each agent is probed over the protocol it actually
speaks: Codex via `/v1/responses`, Claude Code via `/v1/messages`, and the remaining
automatic agents via `/v1/chat/completions`. An incompatible protocol returns
`PROTOCOL_UNSUPPORTED` rather than writing configuration that cannot work.

Aider's install command is fixed by the Go backend as
`uv tool install --force --python 3.12 ...`, leaving uv to reuse or supply a matching
Python. This path runs only when Aider is selected, and a missing `uv` returns
`PREREQUISITE_MISSING`.

## Development and testing

Go core:

```bash
go vet ./...
go test ./...
go test -race ./...
go run honnef.co/go/tools/cmd/staticcheck@2025.1.1 ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...
```

React and Wails:

```bash
cd frontend
pnpm install --frozen-lockfile
pnpm run test:coverage
pnpm run build
pnpm exec playwright install chromium
pnpm run test:e2e
```

Documentation:

```bash
python3 scripts/check-docs.py
```

Every pull request runs `.github/workflows/ci.yml`: `go vet` plus `go test -race` on the
Go side, `pnpm run test` plus `pnpm run build` on the frontend side, and the
documentation link and language check.

## Releasing

Pushing a stable `vX.Y.Z` tag triggers `.github/workflows/build-artifacts.yml` and publishes
macOS and Windows OTA archives for amd64 and arm64, plus `SHA256SUMS`, to the matching
GitHub Release. Each macOS archive contains `OneAgent.app`; each Windows archive contains
`oneagent-desktop.exe`. The workflow can also be run manually with a required version.

Wails is still in Alpha, so there is no Stable release, and no platform signing,
notarization, or store distribution. The signing gate for Stable is deferred to a later
release stage.

To reproduce an equivalent artifact locally, use the desktop build steps under Quick
start. Channel labels, the SHA-256 manifest, and third-party notices were once generated
by `cmd/oneagent-release`; that tool was removed in `23805b0` when the build moved to
GitHub Actions. Third-party attribution now lives in [NOTICE](NOTICE) at the repository
root.

## Documentation

`docs/` is layered by audience: current specifications at the root, architecture
decisions in `decisions/`, and maintainer history in `internal/`.

Outward-facing documentation is written in English. The AI Agent Kit is also available in
Chinese, and this README has a [Chinese version](README_ZH.md). `docs/internal/` is
maintainer-facing and stays in Chinese.

**Usage and specifications**

- [AI Agent Kit](docs/ai-agent-kit/README.md): set up an agent environment from scratch
- [Product boundary baseline](docs/product-boundary-baseline.md): what OneAgent does, what
  it does not, and why
- [Distribution and compliance policy](docs/distribution-compliance-policy.md): the
  rights, security, and channel requirements that precede a release
- [Public site operations](docs/public-site-operations.md)

**Architecture decisions**

- [decisions/](docs/decisions/): ADR-001 through ADR-009, including superseded decisions
  and where they went
- [Wails v3 / Go migration](docs/decisions/ADR-007-wails-v3-go-migration.md)
- [Per-agent protocol verification](docs/decisions/ADR-004-per-agent-protocol-verification.md)
- [Credentials in agent config files](docs/decisions/ADR-008-credentials-in-agent-config-files.md)

**Internal records**

- [internal/](docs/internal/README.md): completion records and verification checklists for
  past work. Commands in there may have stopped working as tools were removed; that
  directory's README names the current entry points.

## Sponsors

OneAgent's development is supported by:

<p>
  <a href="https://ppio.com/"><img src="docs/assets/sponsors/ppio-color.png" alt="PPIO" height="48"></a>
  &nbsp;&nbsp;
  <a href="https://novita.ai/"><img src="docs/assets/sponsors/novita-color.png" alt="Novita" height="48"></a>
</p>

Both are also built-in Providers in OneAgent. Sponsorship does not change which
Provider is recommended or how one is verified: every Provider is probed against
the protocol its Agent actually speaks, and the built-in list is ordered in
`providers.lock.json` independently of any commercial relationship. See
[providers.lock.json](providers.lock.json) for each entry's declared
relationship and disclosure.

## Licence

Apache License 2.0. See [LICENSE](LICENSE).

[NOTICE](NOTICE) lists the third-party components distributed with the binary and their
licences, along with Node.js, uv, and the agent packages that are downloaded at runtime
rather than redistributed. Agent marks shown in the interface are nominative use,
identifying which tool a row refers to. They imply no endorsement or affiliation, and each
mark belongs to its owner.
