# ADR-008: Credentials Written Into Each Agent's Own Config File

## Status

Implemented

## Date

2026-08-03

- Supersedes: the `ONEAGENT_API_KEY_<AGENT>` + `~/.bootagent/agents/<id>.env`
  credential delivery scheme in ADR-006

## Context

The revised ADR-006 had every Agent read its own environment variable:
`~/.bootagent/agents/<id>.env` wrote `ONEAGENT_API_KEY_<AGENT>`, the Codex
`config.toml` pointed at it through `env_key`, and the OpenCode / Kilo JSON referenced
it with `"apiKey": "{env:...}"`. That solved the coupling of three Agents sharing one
variable name, but it kept the cost of the env file itself:

- The configuration only takes effect in a shell that has sourced the env file. A user
  who starts an Agent from the Dock, a desktop shortcut, or an already-open terminal
  sees an unauthenticated error while the BootAgent UI shows "configured".
- Every Agent's restart instructions had to carry a
  `source ~/.bootagent/agents/<id>.env` line, and the desktop Launch button had to
  splice that line into the terminal command as well, or the window it opened would be
  running an unconfigured Agent.
- The same Key landed in two places (`secrets/<id>.env` and `agents/<id>.env`), three
  once you count the `~/.bootagent/env` compatibility layer.

All three Agents have their own credential file location, and each is either the file
BootAgent already writes or a neighbor in the same directory. CC Switch (Tauri) takes
exactly this route: Codex writes `~/.codex/auth.json`, Claude writes the `env` block
of `settings.json`, OpenCode writes `provider.<id>.options.apiKey` in
`opencode.json`, with no env file anywhere.

## Decision

Credentials are written into each Agent's own config file, and all env-file write
logic is deleted.

| Agent | Credential location |
| --- | --- |
| Codex | `OPENAI_API_KEY` in `~/.codex/auth.json`, with `auth_mode` set to `apikey` |
| Claude Code | `env.ANTHROPIC_AUTH_TOKEN` in `~/.claude/settings.json` (already the case) |
| OpenCode | `provider.bootagent.options.apiKey` in `~/.config/opencode/opencode.json` |
| Kilo CLI | the same location in `~/.config/kilo/kilo.jsonc` |
| Aider | `~/.bootagent/aider.env` (loaded directly by Aider's `--env-file`) |

Accompanying changes:

- The Codex `[model_providers.bootagent]` block drops `env_key` and adds
  `requires_openai_auth = true`, so Codex authenticates the hosted provider with the
  Key in `auth.json`. `auth_mode` must be written explicitly as `apikey`: a leftover
  `chatgpt` makes Codex prefer a cached OAuth token and ignore the new Key.
- `auth.json` is written first, `config.toml` second. Pointing at a provider that
  cannot authenticate is worse than leaving a Key that nothing references yet. The
  write reuses `securefs.AtomicWrite`, and `auth.json` is handled as a secret (0600 /
  Windows ACL, with the backup's permissions tightened the same way).
- The OpenCode / Kilo config files now contain a plaintext Key, so they are written as
  secrets.
- The OpenCode path changes from `opencode.jsonc` to `opencode.json`: once the Key
  goes into this file it is BootAgent's primary write target, and `.json` is OpenCode's
  own default name. JSONC comment detection no longer looks at the extension (OpenCode
  parses with JSON5, so a `.json` file may also contain comments); on detection it
  refuses to write and leaves the original file intact.
- Deleted: `internal/config/env.go` (`WriteAgentEnv` / `WriteSharedEnv` /
  `agentEnvVar`), the `credential_delivery` field in `agents.lock.json`, the status
  fields `paths.env_file` and `backups.env`, and the `source` prefix in the restart
  instructions and Launch command; Aider switches to loading the one remaining
  environment file through `--env-file`.

## Alternatives Considered

### Keep env_key for Codex and change only OpenCode / Kilo

- Upside: `auth.json` is left alone, and Codex's login state is untouched.
- Downside: Codex is the primary Agent, so keeping the env file means the goal of "not
  depending on environment variables" is not met, and the `source` instructions and
  the Launch splicing logic all have to stay.
- Conclusion: rejected.

### `codex login --api-key` instead of writing auth.json directly

- Upside: it uses the official Codex entry point, which guarantees the format itself.
- Downside: one more subprocess call and timeout to handle, and it overwrites the
  whole of `auth.json` (losing the user's OAuth cache). Merging directly touches only
  two keys.
- Conclusion: rejected.

## Consequences

- `auth_mode = "apikey"` makes Codex Desktop treat the account as API-Key
  authenticated, so features tied to ChatGPT login (Fast mode and the like) are
  unavailable. That is an unavoidable result of using a hosted provider, and CC Switch
  records the same behavior; BootAgent's whole purpose is to point Codex at a
  third-party Provider, so this is accepted.
- The OpenCode / Kilo config files go from "can be published" to "contains a secret",
  so the write path and permission assertions tighten accordingly, and no future read
  projection may return the raw contents of these two files.
- `~/.bootagent/env` and `~/.bootagent/agents/*.env` are no longer written. Files left
  behind by older versions are not deleted, but they are no longer referenced either;
  users clean them up by hand.
- The status transport contract changes (`paths` has one key fewer, `backups` one key
  fewer), and the consumers in `frontend/src/types/api.ts` and the frozen status
  fixture have been updated to match.
