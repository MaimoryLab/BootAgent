# ADR-007: Wails v3 Desktop Shell and Go Core Migration

- Status: Implemented (2026-07-31)
- Date: 2026-07-30
- Supersedes: the Python-core, localhost-HTTP and PyInstaller decisions in ADR-003 only

> Addendum (2026-08-06): the standalone `cmd/oneagent` CLI and its tests were
> removed. The desktop application is now the only product entry point. CLI
> references below describe the decision as originally implemented.

## Context

The migration plan called for a Wails v3 desktop shell, a transport-independent
Go core, and a separate headless CLI. Wails v3 is still pre-release, so the
shipped channel remains an unsigned technical preview.

## Decision

1. The migration line uses Go 1.26+ and pins Wails v3 to
   `v3.0.0-beta.4` for the initial native spike. The matching CLI uses the
   same module version. The browser runtime is pinned separately to
   `3.0.0-beta.1`, the version embedded in that Wails module and present in the
   frontend lockfile used by the production bundle.
2. `agents.lock.json` remains the only hand-edited Agent catalog source. The
   root Go package embeds that file once, and `internal/catalog` parses the
   embedded bytes for both the CLI and desktop shell.
3. `internal/app`, `internal/catalog`, `internal/platform`,
   `internal/errors`, and `internal/binding` are the shared production core.
   The native Wails implementation is enabled explicitly with the `wails` build
   tag, while the headless CLI remains dependency-free from Wails/GTK.
4. The first Wails shell registers only `StatusService`, `ProviderService`,
   `AgentService`, and `ProfileService`. It does not configure `Route` or
   `RawMessageHandler`, and it does not open a business HTTP listener.
5. The Go/Wails path is now the only production implementation. The old source,
   tests, packaging metadata and release scripts were removed after the Go
   fixtures, bindings, cleanrooms and release checks passed. Shell wrappers remain
   only as thin CLI forwarding compatibility layers.

## Consequences

- The Go status/catalog path and the full release toolchain can be tested without
  a Python installation.
- Native Wails builds currently require the platform WebView toolchain and a
  generated frontend bundle; the default `go test ./...` does not link either.
- The desktop UI and headless CLI call the same Go use cases; there is no HTTP or
  legacy runtime fallback.
- Updating Wails or the runtime requires rerunning binding generation and the
  four-target native spike before changing the pins.
