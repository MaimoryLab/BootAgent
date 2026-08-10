# OneAgent Development Conventions

The current branch is the Go/Wails stabilization line, at version `0.4.0`. The only
product entry point is `cmd/oneagent-desktop`. React calls the backend only through
generated Wails bindings.

## Directories

- `internal/app`: Status, Provider, Agent, and Profile use cases, plus the coordinated
  write lock.
- `internal/catalog`: the embedded `agents.lock.json`, `providers.lock.json`, and
  `runtimes.lock.json` files, plus the built-in Provider catalog.
- `internal/config`: TOML/JSON/JSONC adapters, configuration discovery, and golden
  fixtures.
- `internal/install`: Agent package installation using the latest version by default or
  an optional exact version, registry selection, Node.js/uv runtime bootstrapping
  (download, verification, extraction, and PATH updates), and the Aider Python management
  boundary.
- `internal/profile` and `internal/securefs`: profiles, secrets, backups, permissions,
  and atomic writes.
- `internal/binding`: the five services exposed to the frontend through Wails and the
  only boundary between React and Go. Changes to DTOs here require regenerating
  `frontend/bindings` and synchronizing `frontend/src/backend/wails.ts`.
- `cmd/oneagent-desktop`: the Wails desktop entry point.
- `frontend/bindings`: Wails-generated files. Do not edit them manually.

`cmd/oneagent-release`, `cmd/oneagent-rc`, and `cmd/oneagent-provider-smoke` were removed
in `23805b0`, with their responsibilities moved to
`.github/workflows/build-artifacts.yml`. References to them in historical documentation
are background information, not executable instructions.

The public site has moved to
[MaimoryLab/OneAgent-site](https://github.com/MaimoryLab/OneAgent-site), and this
repository no longer contains `site/`. The site vendors `agents.lock.json` and
`providers.lock.json` into its own `data/` directory and refreshes them from release
tags, not from this repository's `main` branch. Changing those two files here does not
automatically update the site, and should not: the site describes what a published
version supports.

`providers.lock.json` is the source of truth for built-in Provider endpoints, fallback
models, and public-site commercial disclosure fields. User Providers and overrides of
built-in Providers are stored in `~/.oneagent/providers.json`.

## Local Commands

```bash
go test ./...
go test -race ./...
go vet ./...
cd frontend
pnpm install --frozen-lockfile
pnpm run test
pnpm run build
pnpm run test:e2e
```

Every pull request runs these two groups of gates through `.github/workflows/ci.yml`.
Release artifacts are built by manually triggering
`.github/workflows/build-artifacts.yml`.

Regular tests, Wails builds, site builds, and release tools do not require Python.
Installing Aider requires Python 3.12, but no longer requires it to be preinstalled: uv
resolves the interpreter itself, reusing a matching local interpreter or downloading a
managed CPython into `~/.oneagent/runtimes/python`. Python is still not included in
release packages.

## CodeGraph

This repository is indexed (`.codegraph/`, not committed; rebuild it with
`codegraph index .`, which takes about 0.5 seconds). Use it **before grep** when locating
or understanding code:

```bash
codegraph explore "binding Service Install"
```

Its most useful feature in this repository is connecting Go to the frontend. Looking up
`AgentService` lists `internal/binding/services.go`, the generated
`frontend/bindings/.../index.ts`, and the handwritten
`frontend/src/backend/wails.ts` together. These are the three places that must move
together when a backend DTO changes. Types in `frontend/src/types/api.ts` are handwritten
rather than imported from the bindings, so the backend DTOs and this file are **two
sources of truth**. The index is the fastest way to find an omitted update.

**Known limitation**: the blast-radius warning "no covering tests found" considers only
**direct** callers. Implementations invoked inside higher-level functions may therefore
be incorrectly reported as untested. Do not add duplicate tests based on this warning;
confirm the call path first.

## Code Boundaries

- `agents.lock.json` is the sole source of truth for Agent metadata, but it does not store
  Agent versions or package hashes. When adding an automatically configured Agent, add
  its package name and metadata first, then add the corresponding config adapter and Go
  tests.
- Child processes must use argv arrays and a controlled environment, set timeouts, and
  retain diagnostic but redacted output.
- Writes must proceed in this order: private directory, backup, temporary file in the
  same directory, permission tightening, and atomic replacement. If permissions on a
  secret backup cannot be tightened, delete it and fail.
- Do not write API keys to ordinary profiles, status summaries, logs, URLs, global React
  state, browser storage, or test artifacts. Only Provider editing and configuration
  forms may read a key from private local storage on demand through a local binding.
- Probe Providers using the Agent's protocol. `/v1/models` cannot replace Responses,
  Anthropic Messages, or Chat Completions checks.
- Wails production builds must not use the `server` tag. Only browser E2E may use the
  server/e2e fake runner.
- Linux release builds must use the `gtk3` tag. While Wails is in Alpha, only the
  `technical-preview-unsigned` channel is allowed.

## Documentation Maintenance

`docs/` is organized by audience. Choose the correct location before adding a document:

- The `docs/` root contains current specifications and policies that readers can follow
  directly.
- `docs/ai-agent-kit/` contains user-facing instructions. The default path is downloading
  a release package, not building from source.
- `docs/decisions/` contains ADRs. Preserve superseded decisions, mark them as
  Superseded, and link to their replacements instead of rewriting history.
- `docs/internal/` contains maintainer-facing completion records and verification
  checklists. Do not place unimplemented plans there; those belong in issues.

## Documentation Language

All public documentation must be written in English, with Chinese versions provided only
for the two exceptions below:

| Location | Language |
| --- | --- |
| `README.md` | English; Chinese is in `README_ZH.md`, and both versions must stay synchronized |
| Specifications in the `docs/` root and ADRs in `docs/decisions/` | English only |
| `docs/ai-agent-kit/` | Bilingual, with complete sets under `en/` and `zh/` |
| `CLAUDE.md` and `docs/internal/` | Chinese. Their audience is maintainers, and bilingual copies would only create drift |

Use `frontend/src/i18n.tsx`, the source of truth for product UI terminology, for English
terms: runtime = Runtimes, configuration template = Profiles, environment overview =
Environment overview, guide only = Guide only, and activation steps = Setup steps. A
separate vocabulary in the documentation would make the UI and documentation
inconsistent. Do not translate identifiers such as `Agent`, `Provider`, `Profile`,
`technical-preview-unsigned`, and `agents.lock.json`.

**UI copy runs in the opposite direction from the documentation**: `translate()` looks
up a translation only when `locale === "en"`; otherwise it returns the Chinese key
directly, so Chinese is the **source language** for i18n. Continue to write new UI copy
as a Chinese key first and then add its English translation. Do not change keys merely
because public documentation is written in English; doing so would require changing
every associated entry.

`python3 scripts/check-docs.py` checks that relative links resolve and that English
documents contain no leftover Chinese. Run it after documentation changes;
`.github/workflows/ci.yml` runs it as well.

Commands in README files, workflows, Taskfiles, and the AI Agent Kit must correspond to
files in the current repository. Use ` ```text ` instead of ` ```bash ` for command
blocks in `docs/internal/` that reference removed tools, so they are not mistaken for
executable instructions.

`LICENSE` is Apache-2.0, and `NOTICE` is the source of truth for third-party attribution.
When adding a dependency distributed with the package or a new third-party mark to the
UI, update `NOTICE` as well. `docs/distribution-compliance-policy.md` lists this as a
release prerequisite.

## README Maintenance

- After changing user-visible behavior, supported Agents or Providers, architecture,
  prerequisites, paths, or build, test, and release commands, update `README.md` and
  `README_ZH.md` in the same change without waiting to be asked.
- Keep the English and Chinese versions synchronized. Skip README churn for internal
  changes that do not affect documented behavior.
- Verify README claims against the current code, manifests, Taskfiles, and workflows,
  then run `python3 scripts/check-docs.py`.
