# ADR-007: Wails v3 Desktop Shell and Go Core Migration

- Status: Accepted for staged implementation
- Date: 2026-07-30
- Supersedes: the Python-core, localhost-HTTP and PyInstaller decisions in ADR-003 only

## Context

The migration plan calls for a Wails v3 desktop shell, a transport-independent
Go core, and a separate headless CLI. Wails v3 is still Alpha, so the first
increment must be reversible and must not change the shipped Python entry
points before the Go behavior contracts are complete.

## Decision

1. The migration line uses Go 1.25+ and pins Wails v3 to
   `v3.0.0-alpha2.119` for the initial native spike. The matching CLI uses the
   same module version. The browser runtime candidate is pinned separately to
   `3.0.0-alpha2.117`, the version shipped by that Wails module; it will be
   added to the frontend lockfile when bindings enter the production bundle.
2. `agents.lock.json` remains the only hand-edited Agent catalog source. The
   root Go package embeds that file once, and `internal/catalog` parses the
   embedded bytes for both the CLI and desktop shell.
3. `internal/app`, `internal/catalog`, `internal/platform`,
   `internal/errors`, and `internal/binding` are introduced before any
   production entry-point switch. The default desktop command is a dependency-
   free stub; the native Wails implementation is enabled explicitly with the
   `wails` build tag.
4. The first Wails shell registers only `StatusService`, `ProviderService`,
   `AgentService`, and `ProfileService`. It does not configure `Route` or
   `RawMessageHandler`, and it does not open a business HTTP listener.
5. All Python source, tests, packaging metadata, wrappers, and CI paths remain
   intact until the phase-specific Go replacement has equivalent fixtures and
   its exit gate is green. This ADR does not authorize deleting or bypassing
   the Python implementation.

## Consequences

- The Go status/catalog path can be tested in an environment with no Python.
- Native Wails builds currently require the platform WebView toolchain and a
  generated frontend bundle; the default `go test ./...` does not link either.
- The Go service methods that have not reached behavioral parity return a
  stable not-ready error instead of silently falling back to Python. The
  production UI continues to use the existing Python HTTP path during this
  stage, so users do not encounter that error yet.
- Updating Wails or the runtime requires rerunning binding generation and the
  four-target native spike before changing the pins.
