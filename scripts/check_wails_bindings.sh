#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
expected_version="$(sed -n 's/^WAILS_CLI_VERSION=//p' "$repo_root/build/tool-versions.env")"
if [[ -n "${WAILS3_BIN:-}" ]]; then
  wails_cmd=("$WAILS3_BIN")
else
  wails_cmd=(go run "github.com/wailsapp/wails/v3/cmd/wails3@${expected_version}")
fi
actual_version="$("${wails_cmd[@]}" version 2>&1 | tail -n 1 | tr -d '\r')"
if [[ "$actual_version" != "$expected_version" ]]; then
  printf 'Wails CLI version %s does not match pinned %s\n' "$actual_version" "$expected_version" >&2
  exit 1
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
(
  cd "$repo_root"
  "${wails_cmd[@]}" generate bindings -f "-tags wails" -ts -i -d "$tmp_dir" -clean=true ./cmd/oneagent-desktop
)

diff -ru --exclude=README.md "$repo_root/frontend/bindings" "$tmp_dir"
