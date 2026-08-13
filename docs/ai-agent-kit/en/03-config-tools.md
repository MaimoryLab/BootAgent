# Choosing a config tool

[简体中文](../zh/03-config-tools.md) · **English**

BootAgent offers three ways to configure an agent. For a first run, use the built-in
configuration. Reach for a local tool like CC Switch once you need several Providers or
several accounts.

## Option 1: BootAgent's built-in configuration

Good for:

- Using PPIO for the first time.
- Needing only one Provider.
- Wanting the setup to be as easy to review and debug as possible.

What it does:

1. Detects whether the agent is installed.
2. Calls the official install source, or shows the official install command.
3. Writes to the target agent's official configuration entry point.
4. Fetches the model list via `/v1/models`.
5. Sends one minimal request to verify.

This is the default path on a first run.

## Option 2: CC Switch

Good for:

- Switching between PPIO, other OpenAI-compatible services, and first-party accounts.
- Keeping different configurations for different projects.
- Moving frequently between Claude Code, Codex, OpenCode, and similar tools.

CC Switch is an optional local configuration tool, not a BootAgent dependency. Get the
current version from its own official project page — BootAgent does not repackage its
binary or install script into the kit.

For the steps, see the [CC Switch guide](./tools/cc-switch.md).

## Option 3: Manual configuration

Good for:

- Agents that manage their own configuration their own way.
- Not wanting an extra local configuration tool.
- Debugging what an automatic setup produced.

Manual configuration means confirming at least:

```text
Base URL: https://api.ppio.com/openai
Models:   GET /v1/models
Chat:     POST /v1/chat/completions
Key:      local secure storage only
```

## Which to pick

| Situation | Recommended |
| --- | --- |
| First run | BootAgent built-in configuration |
| A single PPIO account | BootAgent built-in configuration |
| Several Providers | CC Switch or a comparable profile tool |
| OpenClaw's model provider | BootAgent built-in configuration |
| OpenClaw's channels, tools, and daemon | Its own commands, such as `openclaw onboard` |
| IDE extensions | The extension's own Provider settings |
| Unsure where a tool came from | Manual or BootAgent built-in configuration |

## One distinction that matters

CC Switch being a local configuration tool and a third-party hosted API service are two
different things. Using CC Switch means switching configuration on your own machine. It
does not mean your PPIO key is handed to a CC Switch server, and it does not mean that
tool's service address can be used as the PPIO base URL.

No config tool substitutes for compliant network access. BootAgent provides no VPN, proxy,
node subscription, or circumvention capability. If a tool or install source is
unreachable, go back to [Start here](./00-start-here.md) for the manual install path.
