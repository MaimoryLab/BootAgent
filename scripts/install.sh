#!/usr/bin/env bash
# Thin forwarding layer to the Go CLI. It resolves a binary and execs it; all
# argument parsing, validation and exit codes belong to cmd/oneagent.
#
# Kept for one release cycle so existing docs, CI jobs and user scripts keep
# working while the Go CLI remains the only implementation. This wrapper only
# resolves the already-built binary.
#
# It deliberately does not build on demand. Callers run with a temporary HOME,
# and `go build` would write a module cache into it -- a side effect a wrapper
# has no business causing. Build once, then forward.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [ -n "${ONEAGENT_CLI_BINARY:-}" ]; then
  BINARY="$ONEAGENT_CLI_BINARY"
elif [ -x "$ROOT_DIR/bin/oneagent" ]; then
  BINARY="$ROOT_DIR/bin/oneagent"
else
  BINARY="$(command -v oneagent || true)"
fi

if [ -z "$BINARY" ] || [ ! -x "$BINARY" ]; then
  echo "[oneagent] error: the OneAgent CLI was not found." >&2
  echo "[oneagent] build it with: go build -o bin/oneagent ./cmd/oneagent" >&2
  echo "[oneagent] or point ONEAGENT_CLI_BINARY at an existing binary." >&2
  exit 3
fi

exec "$BINARY" "$@"
