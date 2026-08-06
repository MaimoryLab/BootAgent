# Agent categories and install guides

[简体中文](../zh/04-agent-guides.md) · **English**

## One-click configuration

### Codex

- Prefer the official install source.
- OneAgent writes `~/.codex/config.toml`, and the key into `~/.codex/auth.json` beside it.
- No environment variables involved. Restart codex after a configuration change.

### Claude Code

- Prefer the official install source.
- OneAgent writes `~/.claude/settings.json`.
- Configured through `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN`, and the model field.
- Restart Claude Code after a configuration change.

### OpenCode

- Uses an OpenAI-compatible Provider. OneAgent writes
  `~/.config/opencode/opencode.json`.
- The base URL takes the form `https://api.ppio.com/openai/v1`.
- The API key goes directly into that file's `provider.oneagent.options.apiKey`, with
  permissions tightened to 0600. No environment variables involved.

### Kilo CLI

- Uses an OpenAI-compatible Provider. OneAgent writes `~/.config/kilo/kilo.jsonc`.
- As with OpenCode, the API key lives in the config file's `options.apiKey`, with
  permissions tightened to 0600.

### Aider

- Python 3.12 is needed only if you choose to install Aider, and `uv` resolves it itself:
  a matching local interpreter is reused, otherwise a managed CPython is downloaded to
  `~/.oneagent/runtimes/python`. No other agent, and not OneAgent itself, needs Python.
- OneAgent installs `uv` as a runtime into `~/.oneagent/runtimes`. You do not need it
  preinstalled.
- PPIO settings are kept in a dedicated environment file.
- Aider loads it itself at launch via `aider --env-file ~/.oneagent/aider.env`. There is
  no need to source anything in your shell.
- When using the `openai/<model>` form, follow the guidance for your installed Aider
  version.

## Gateway agents

### OpenClaw

OneAgent installs the `openclaw` package, writes the Provider into
`models.providers.oneagent` in `~/.openclaw/openclaw.json`, and points
`agents.defaults.model.primary` at `oneagent/<model>`.

**Those two places only.** `channels`, `tools`, the other fields under
`agents.defaults`, and any other provider you already have are left exactly as they
were. Those are what you decide through `openclaw onboard`, and OneAgent has no
basis to change them.

The following stay with OpenClaw's own commands:

- Starting or stopping the gateway, and registering a launchd or systemd service
- Pairing chat channels (Discord, Telegram, WhatsApp, and the rest)
- The Control UI port and its access control
- Enabling plugins

Run `openclaw onboard` afterwards to pair channels. To make the gateway pick up a
changed config, run `openclaw gateway restart`: it is a long-lived process, so
reopening a foreground command is not enough.

`openclaw.json` is JSON5 and may contain comments. If yours does, OneAgent refuses
the write and reports it rather than dropping the comments; fill in the fields above
by hand in that case.

## Other tools

`agents.lock.json` is the single source of truth for the agent catalog. Tools that are not
in it — IDE extensions, other terminal agents — are neither detected nor configured by
OneAgent. Such tools usually take an OpenAI-compatible endpoint in their own Provider
settings; you can copy the field values from
[Verify your first request](./05-first-request.md), but do it in that tool's own
interface. OneAgent does not modify an IDE's private state files.

## Debugging order

```text
Test the PPIO API first
→ then confirm the model ID
→ then confirm the agent's config file
→ restart the agent
→ then retry the first request
```
