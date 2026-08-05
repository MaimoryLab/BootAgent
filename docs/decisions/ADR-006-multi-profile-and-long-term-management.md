# ADR-006: Multiple Profiles and Long-Term Environment Management

## Status

Implemented; the credential delivery part is superseded by
[ADR-008](ADR-008-credentials-in-agent-config-files.md) (see the "Revision" section
below). The conclusions about multiple Profiles and long-term management still hold.

## Date

2026-07-27

## Context

OneAgent is currently a one-shot wizard: it ends the moment setup completes,
`~/.oneagent/profile.json` stores a single active state (`schema_version: 1`: one
provider, one model, one agent list), and `EnvironmentOverviewPage` is a read-only
page. After setup there is nowhere for the user's long-term needs to live:

- Switching between Providers, or between models of the same Provider, means walking
  through all seven wizard pages again.
- When an Agent version falls behind the version pinned in `agents.lock.json` --
  `status_payload` can already compare `version` against `lockedVersion` -- the UI
  gives no hint at all.
- Config writes already produce `*.backup-<ts>` backups through `atomic_write`, but
  the user cannot see them and cannot roll back.

Users are in fact using CC Switch to switch Profiles (see the
[CC Switch configuration guide](../ai-agent-kit/zh/tools/cc-switch.md)). That shows
the need to switch is real, and the CC Switch document also records one key lesson:
**an Agent does not reload its configuration automatically after a switch**, so it is
not enough to display "switched" and stop there.

## Decision

### Storage layout

```text
~/.oneagent/
  profile.json          # schema_version: 2, active pointer {active: <id>}
  profiles/             # one file per configuration, no Key inside
    <id>.json
  secrets/              # secret file per configuration, 0600 / Windows ACL
    <id>.env
```

- A Profile record holds `id`, `label`, `provider`, `base_url`, `model`,
  `config_mode`, `protocol`, `created_at`, `activated_at`. **The Key does not go into the
  profile file** (a product boundary and a hard constraint in CLAUDE.md).
- `id` is a restricted slug (`[a-z0-9][a-z0-9_-]*`); invalid input returns
  `INVALID_REQUEST`.
- `secrets/<id>.env` stores the Key and Base URL for that profile template. The
  credentials an Agent actually reads live in its own config file (see the Revision
  section below and ADR-008).

### v1 to v2 migration

When a `profile.json` with `schema_version: 1` is read, migrate it automatically:
back the original file up with `backup_file` first, then convert its contents into
`profiles/default.json` and write the v2 pointer. Under no circumstances does an old
file cause an error outright. The migration must be pinned by a test (the "read a v1
file" case), so that later refactoring cannot quietly break existing users.

### Writing and switching

- Wizard setup updates or creates the requested Profile and records its API
  protocol. Agent ownership lives only in the per-Agent binding files; Profiles do
  not duplicate it.
- Switching means rewriting the same set of config files with another set of
  parameters, **fully reusing the existing write path** (`_write_agent_config`
  dispatch plus `atomic_write` plus backup); no new write logic is introduced.
- The response to `POST /api/activate` must carry per-Agent **restart instructions**
  (adopting the CC Switch lesson that an Agent does not reload config on its own),
  rather than returning only "switched".
- Profile operations all reuse the Go `ProfileService` and the `securefs` write
  boundary; no new HTTP channel is added. The Key only lands in `secrets/`, and the
  binding returns a public summary.

### CLI

Add the subcommands `oneagent profile list / add / activate / remove`. When `argv[1]`
is not a known subcommand, fall through to the existing flat parser to stay backward
compatible.

### Turning Overview into a management surface

- Profile cards, an active marker, and one-click switching.
- Agent rows show the drift of `version` against `lockedVersion`; "update" goes
  through the existing install path.
- Routing change: when a profile exists, the root path goes to `/overview`; only
  without one does it go to the wizard. This is the most direct expression of the
  "long-term tool" positioning.
- Listing and rolling back backups (`*.backup-<ts>` already exist but are not
  exposed) is an optional follow-up increment (3b); it needs a new endpoint and is
  evaluated separately.

### Explicitly out of scope

- **Rewriting shell rc automatically**: `--wire-shell` remains a separate explicit
  flag, evaluated on its own after layer 2.
- **Reloading Agent processes automatically**: different Agents apply configuration
  differently, so we do not operate on processes; we only give restart instructions.

## Revision: from a global profile to per-agent independent configuration

The first draft of this ADR listed "per-agent independent profiles" as out of scope,
on the grounds that `~/.oneagent/env` is inherently shared globally. That reasoning
has been overturned: a shared env is not an external constraint but artificial
coupling we created ourselves by writing the same variable name,
`ONEAGENT_API_KEY`, into three Agent configs. The configs of all five Agents already
land in separate files, and the credentials for Claude Code and Aider have long been
independent; only Codex, OpenCode, and Kilo CLI could not point at different
Providers in the same shell, and only because they shared one variable name.

So the decision changes to per-agent independent credentials. The first version
implemented this with `ONEAGENT_API_KEY_<AGENT>` plus
`~/.oneagent/agents/<agent-id>.env`, and **has been superseded by ADR-008**:
credentials are now written into the Agent's own config file (Codex uses
`~/.codex/auth.json`, Claude Code uses the `env` block of `settings.json`,
OpenCode/Kilo use `options.apiKey` in their config), so sourced env files are no
longer needed. The conclusions about decoupling and about a failure affecting only a
single Agent are unchanged.

The `profiles/` storage stays, with its meaning changed from "the one active state"
to **reusable templates**: three Agents sharing the same Provider and Key is a common
case, and templates avoid retyping. A Profile records API protocol compatibility;
per-Agent binding files remain the source of truth for actual assignment.

## Alternatives Considered

### Embed every profile inside a single profile.json

- Upside: atomic replacement is simple, no directory to keep in sync.
- Downside: the file grows with the number of profiles; secret isolation is
  misaligned with file granularity; it does not match the mental model of CC Switch
  users.
- Conclusion: rejected. One file per profile has a smaller read/write surface, and
  secret files separate naturally.

### Per-agent independent configuration

- Upside: each Agent can point at a different Provider.
- Downside: the shared env contract stops working, the number of secret files
  doubles, and switching semantics get one level more complex.
- Conclusion: rejected; ship the global profile first and extend later if a real need
  appears.

### Keep things as they are and keep recommending CC Switch

- Upside: zero development effort.
- Downside: OneAgent hardens into a one-shot tool, and the probe/verify capability is
  split away from the switching capability across two products.
- Conclusion: rejected.

## Consequences

- The `profile.json` schema changes, and migration is the only data risk point:
  backup first, and pin it with tests.
- `status_payload` gains `profiles` and `activeProfile` fields, which must be mirrored
  in `frontend/src/types/api.ts` (the transport contract rule).
- The recommendation order in the CC Switch document needs adjusting: switching built
  into OneAgent is the main path, with CC Switch as an optional downstream.
- Go profile/config tests, React state tests, and Wails binding tests cover the new
  endpoints and the migration path.
- The backup rollback UI, per-agent profiles, and `--wire-shell` each need their own
  follow-up evaluation; the rollback UI among them needs a new endpoint.
