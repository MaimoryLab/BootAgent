# Generated Wails bindings

This directory is generated from the Go services. Do not edit the generated
TypeScript files by hand.

Regenerate with the pinned CLI and build tag:

```text
wails3 generate bindings -f "-tags wails" -ts -i -d frontend/bindings ./cmd/oneagent-desktop
```

`frontend/src/backend/wails.ts` is the only page-facing transport adapter.
React calls these generated bindings directly; there is no HTTP fallback.
