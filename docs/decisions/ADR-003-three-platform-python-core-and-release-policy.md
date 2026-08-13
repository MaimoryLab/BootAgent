# ADR-003: Three-Platform Runtime and Version-Locked Release Policy (Obsolete)

> Status: **Superseded** (2026-07-31). The current implementation and release
> rules are defined by [ADR-007](ADR-007-wails-v3-go-migration.md),
> [ADR-005](ADR-005-channel-neutral-distribution-and-compliance.md) and
> `cmd/bootagent-release`. This file is kept for historical background only; it is
> not an install or release guide.

> Addendum (2026-08-04): `cmd/bootagent-release`, `cmd/bootagent-rc` and
> `cmd/bootagent-provider-smoke`, mentioned in this document, were removed in
> `23805b0`, with their responsibilities handed to
> `.github/workflows/build-artifacts.yml`. The commands involved are historical
> background and are not executable.

## Historical Background

Early BootAgent used cross-platform scripts and the Python standard library to
implement the Agent catalog, config adaptation, install orchestration and a local
HTTP GUI. That approach emphasized three-platform paths, permissions, locked
versions, an npm/uv allowlist, a complete set of error codes, and cleanroom
evidence.

Those product constraints still hold, but the implementation has since moved to:

- Go catalog, provider, install, config, profile, securefs and process packages.
- The pure Go CLI `cmd/bootagent` and the Wails app `cmd/bootagent-desktop`.
- React calling Go services through generated Wails bindings.
- `cmd/bootagent-release` producing native Wails/Go packages, manifests, SHA-256
  values and third-party notices.
- `cmd/bootagent-rc` and `cmd/bootagent-provider-smoke` carrying out release
  candidate checks.

## Product Constraints That Still Hold

- Agent packages do not go into the BootAgent release bundle; installation may
  only come from an official source declared in the lock file, or an HTTPS mirror
  the user explicitly chose.
- Subprocesses use argument arrays, a controlled environment and timeouts; shell
  string concatenation and unreviewed download pipelines are forbidden.
- API Keys do not go into a profile, a log, a URL, a command line, React state,
  or a release attachment.
- Config writes must back up, replace atomically, and tighten the Unix mode or
  Windows ACL.
- Codex, Claude Code and OpenAI-compatible Agents are probed separately according
  to the protocol each one actually uses.
- During the Wails Alpha phase, only `technical-preview-unsigned` is released;
  Stable requires separate signing, notarization and native verification
  evidence.
- Aider is an optional external upstream exception: when a user chooses to
  install it, Python 3.12 must already be present, and BootAgent neither bundles
  nor downloads that runtime.

## Migration Record

The Python implementation, the Python tests, the PyInstaller/wheel/setuptools
configuration and the related workflows have been deleted. For the new acceptance
checklist see the
[Wails v3 migration wrap-up plan](../internal/wails-v3-migration-plan.md).
