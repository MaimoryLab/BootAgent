# ADR-011: Provider Keys for Multimodal Skills Delivered as Agent Environment Variables

## Status

Accepted

## Date

2026-10-09

- Related: [ADR-008](ADR-008-credentials-in-agent-config-files.md), which this
  extends to Codex's `config.toml` and to Providers an Agent does not otherwise use
- Design: [Multimodal Skills](../multimodal-skills-design.md)

## Context

BootAgent is adding generated Skills that let an Agent reach image, audio and
video models on a Provider the user already has a key for. A Skill is a directory
of files that the Agent reads and whose script it runs through its own shell tool.
The script has to authenticate to the Provider, so the key has to reach that
process somehow.

The Skill tree itself cannot carry the key. Skills are distributed through the
existing hash, variant and backup machinery, the registry records them, and users
copy them between machines; a key inside the tree would leak through every one of
those paths.

ADR-008 already writes Provider keys into each Agent's own configuration so the
Agent works no matter how it is started. Claude Code's `env` block in
`~/.claude/settings.json` is passed to every command its Bash tool runs. Codex has
`[shell_environment_policy] set` in `~/.codex/config.toml`, which is applied after
Codex's default exclusion of variables whose names contain `KEY`, `SECRET` or
`TOKEN`, so a variable set there reaches the shell tool.

## Decision

The key reaches a Skill as an environment variable named
`BOOTAGENT_<PROVIDER>_API_KEY`: the Provider ID upper-cased, with every character
outside `[A-Z0-9]` replaced by `_`. BootAgent writes the variable into the target
Agent's own configuration.

| Agent | Where the variable is written |
| --- | --- |
| Claude Code | `env` in `~/.claude/settings.json`, which is already written as a secret |
| Codex | `[shell_environment_policy] set` in `~/.codex/config.toml`; `config.toml` is written as a secret from then on, backups included |
| Agents with no configurable environment | A private fallback file `~/.bootagent/skill-env/<provider>.env`, the pattern ADR-008 uses for Aider |

Accompanying rules:

- BootAgent manages only `BOOTAGENT_*` names. Every other variable in Claude's
  `env` and every other key in Codex's `shell_environment_policy` table is the
  user's and is preserved.
- The generated script reads the variable first and the fallback file only when the
  variable is unset. Base URLs, model IDs and the variable name are not secret and
  live in the Skill's `config.json`.
- Saving a Provider with a new key rewrites the variable on the reapply path that
  already rewrites Agent configs. Uninstalling the last Skill that uses a Provider
  removes it.
- Writes follow the existing order: private directory, backup, temp file in the same
  directory, tighten permissions, atomic replace.

## Alternatives Considered

### A local loopback proxy that injects the key

- Upside: the key never enters any Agent's configuration or environment, so the
  Agent's model cannot read it.
- Downside: Skills work only while BootAgent is running, which turns a configurator
  into a resident service. It adds a network listener and a request path BootAgent
  would have to keep correct for every Provider's native API.
- Conclusion: rejected.

### Generate documentation only and let the user wire up the key

- Upside: BootAgent writes no credential anywhere new.
- Downside: the user must export a variable in every shell, and an Agent started
  from the Dock or a desktop shortcut sees nothing. That is the failure ADR-008
  removed; it also defeats one-click install.
- Conclusion: rejected.

## Consequences

- Accepted risk: the key is visible to every command the Agent runs, so the Agent's
  model can read it. Claude Code's own Provider token already has that exposure
  under ADR-008. The install confirmation tells the user before anything is written.
- The exposure now also covers keys for Providers the Agent does not otherwise use,
  and Codex's `config.toml`, which until now held no secret.
- `config.toml` goes from a plain file to a secret one: its writes and backups are
  tightened the same way `auth.json` is, and no read projection may return its raw
  contents once it carries a key.
- The variable name is stable and derived from the Provider ID, so a Skill generated
  on one machine works on another once BootAgent has written the variable there.
