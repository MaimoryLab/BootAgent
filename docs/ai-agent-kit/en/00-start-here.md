# Start here

[简体中文](../zh/00-start-here.md) · **English**

This kit walks you through three things:

1. Get a model Provider ready to use.
2. Install or configure an AI agent.
3. Make a first model request that succeeds.

## Before you start

- A supported macOS, Windows, or Linux machine. The normal BootAgent flow needs no Python.
- A working model Provider account, or be ready to sign up for one.
- The agent you intend to use — Codex, Claude Code, OpenCode, or Aider, for example.

## The path

```text
Launch BootAgent
→ Sign up for or log in to a Provider
→ Create an API key
→ Pick an agent
→ Pick a config tool
→ Pick a model
→ Run the setup
→ Make your first request
```

If you are already comfortable switching between Providers, choose **CC Switch** on the
config method page, then read [Choosing a config tool](./03-config-tools.md) and the
[CC Switch guide](./tools/cc-switch.md).

## Launching the desktop app

Download the `technical-preview-unsigned` package for your platform, unpack it, and
launch it directly: `BootAgent.app` on macOS, `bootagent-desktop.exe` on Windows, or the
Linux AppImage. Linux also provides `deb`, `rpm`, and OTA `zip` packages for amd64 and
arm64. macOS packages are Developer ID signed and notarized; Windows and Linux packages
remain unsigned, so their first launch may need manual approval from the operating system.

To run from source instead, for development or review:

```bash
cd frontend && pnpm install --frozen-lockfile && pnpm run build
cd ..
go run -tags wails ./cmd/bootagent-desktop
```

## Three safety rules

1. Never paste an API key into a chat, an issue, homework, or a screenshot.
2. Never commit an API key to a repository.
3. If you are unsure whether a config tool is trustworthy, fall back to BootAgent's
   built-in configuration path.

## When an agent will not download

If the official install source is unreachable from your network:

1. Use the compliant network access your organization or ISP provides.
2. Use an authorized mirror, and verify the version and checksum.
3. Install manually from another compliant environment, then return to BootAgent to
   detect it locally.

BootAgent does not provide a VPN, a proxy, node subscriptions, or any configuration that
circumvents network restrictions.

## If something goes wrong

- Provider trouble: see [PPIO account and Provider setup](./01-ppio-account.md).
- Key trouble: see [Create and store an API key](./02-api-key.md).
- Config tool trouble: see [Choosing a config tool](./03-config-tools.md).
- Agent trouble: see [Agent categories and install guides](./04-agent-guides.md).
- Request trouble: see [Verify your first request](./05-first-request.md).
