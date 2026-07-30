# Generated Wails bindings

This directory is generated from the Go services. Do not edit the generated
TypeScript files by hand.

Regenerate with the pinned CLI and build tag:

```text
wails3 generate bindings -f "-tags wails" -ts -i -d frontend/bindings ./cmd/oneagent-desktop
```

The current production frontend still uses `frontend/src/api/client.ts`; these
bindings are staged for the later service-switch phase. Until that phase is
complete, the Python HTTP path remains authoritative.
