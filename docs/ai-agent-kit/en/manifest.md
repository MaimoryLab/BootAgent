# Kit release package manifest

[简体中文](../zh/manifest.md) · **English**

## Runtime files

| File | Purpose | Needs a key |
| --- | --- | --- |
| `OneAgent.app` (macOS) / `oneagent-desktop.exe` (Windows) | Desktop app | No |

Release packages are built by `.github/workflows/build-artifacts.yml`. Earlier versions
launched a local GUI through three scripts — `launcher`, `start.sh`, and `start.command` —
which the Go/Wails migration made unnecessary.

## Documentation files

| Document | Audience |
| --- | --- |
| `00-start-here.md` | Everyone |
| `01-ppio-account.md` | Users who need to set up a Provider account |
| `02-api-key.md` | Users at the API key step |
| `03-config-tools.md` | Users choosing a config tool |
| `tools/cc-switch.md` | CC Switch users |
| `04-agent-guides.md` | Users picking an agent |
| `05-first-request.md` | Users who have finished setup and are verifying it |

## Version and provenance record

Every release package should record:

```text
Package version:
Release date:
Provider documentation verified on:
Agent version range:
Config tool version range:
```

A distributing organization may maintain its own redemption codes or onboarding notes
outside the package, but that content does not enter OneAgent's core manifest.

Never write a real API key, a shared gateway key, or personal account details into the
manifest.

## Never bundle

- VPN or proxy clients.
- Proxy nodes, subscription links, or cross-border relay configuration.
- Agent packages whose licence has not been confirmed.
- Unlicensed binaries of commercial agents.
- Shared Provider API keys.
- User passwords, phone numbers, or identity documents.
