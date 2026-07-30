#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REAL_HOME="${HOME:?HOME is required}"
PACKAGED_BINARY="${ONEAGENT_PACKAGED_BINARY:-}"
ARTIFACT_DIR="${ONEAGENT_MACOS_CLEANROOM_ARTIFACTS:-$ROOT_DIR/build/macos-cleanroom}"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "macOS cleanroom requires a real Darwin host." >&2
  exit 2
fi
if [[ -z "$PACKAGED_BINARY" || ! -x "$PACKAGED_BINARY" ]]; then
  echo "ONEAGENT_PACKAGED_BINARY must point to the native OneAgent executable." >&2
  exit 2
fi

PYTHON_BIN="$(command -v python3.12 || true)"
NODE_BIN="$(command -v node || true)"
NPM_BIN="$(command -v npm || true)"
GO_BIN="$(command -v go || true)"
if [[ -z "$PYTHON_BIN" || -z "$NODE_BIN" || -z "$NPM_BIN" ]]; then
  echo "Python 3.12, Node and npm are required for the macOS cleanroom." >&2
  exit 2
fi
if [[ -z "$GO_BIN" ]]; then
  echo "Go is required to build the CLI the wrapper contracts forward to." >&2
  exit 2
fi

# The CLI is built before the sanitized PATH is assembled: scripts/install.sh is
# a pure forwarding layer, and the cleanroom stages below run without Go.
ONEAGENT_CLI_BINARY="$ROOT_DIR/bin/oneagent"
(cd "$ROOT_DIR" && "$GO_BIN" build -o "$ONEAGENT_CLI_BINARY" ./cmd/oneagent)
export ONEAGENT_CLI_BINARY

CLEAN_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/oneagent-macos-cleanroom.XXXXXX")"
CLEAN_HOME="$CLEAN_ROOT/home"
CLEAN_TMP="$CLEAN_ROOT/tmp"
FAKE_BIN="$CLEAN_ROOT/bin"
BEFORE_SNAPSHOT="$CLEAN_ROOT/real-home-before.json"
AFTER_SNAPSHOT="$CLEAN_ROOT/real-home-after.json"
MOCK_KEY="oneagent-macos-cleanroom-placeholder"
SERVER_PIDS=()

mkdir -p "$CLEAN_HOME" "$CLEAN_TMP" "$FAKE_BIN" "$ARTIFACT_DIR"
chmod 0700 "$CLEAN_HOME" "$CLEAN_TMP" "$FAKE_BIN"
ln -s "$PYTHON_BIN" "$FAKE_BIN/python3.12"
ln -s "$PYTHON_BIN" "$FAKE_BIN/python3"
ln -s "$NODE_BIN" "$FAKE_BIN/node"
ln -s "$NPM_BIN" "$FAKE_BIN/npm"
printf '%s\n' '#!/bin/bash' 'echo "fake uv must not install a real package" >&2' 'exit 97' > "$FAKE_BIN/uv"
chmod 0755 "$FAKE_BIN/uv"

CLEAN_PATH="$FAKE_BIN:/usr/bin:/bin:/usr/sbin:/sbin"

rm -rf "$ARTIFACT_DIR"
mkdir -p "$ARTIFACT_DIR"

snapshot_real_home() {
  local destination="$1"
  "$PYTHON_BIN" - "$REAL_HOME" "$destination" <<'PY'
from __future__ import annotations

import hashlib
import json
import os
import stat
import sys
from pathlib import Path

home = Path(sys.argv[1])
destination = Path(sys.argv[2])
targets = [
    ".codex/config.toml",
    ".claude/settings.json",
    ".config/opencode/opencode.jsonc",
    ".config/kilo/kilo.jsonc",
    ".oneagent",
    ".aider.conf.yml",
]
snapshot: dict[str, object] = {}

for relative in targets:
    target = home / relative
    if not target.exists() and not target.is_symlink():
        snapshot[relative] = None
        continue
    entries = [target]
    if target.is_dir():
        entries.extend(sorted(target.rglob("*")))
    values = []
    for entry in entries:
        info = entry.lstat()
        item = {
            "path": str(entry.relative_to(home)),
            "mode": stat.S_IMODE(info.st_mode),
            "size": info.st_size,
        }
        if entry.is_symlink():
            item["symlink"] = os.readlink(entry)
        elif entry.is_file():
            item["sha256"] = hashlib.sha256(entry.read_bytes()).hexdigest()
        values.append(item)
    snapshot[relative] = values

destination.write_text(json.dumps(snapshot, sort_keys=True, indent=2), encoding="utf-8")
PY
}

clean_env() {
  # env -i drops the exported CLI path, so it is reinstated explicitly. It names
  # a prebuilt binary rather than a toolchain, so the environment stays clean.
  env -i \
    HOME="$CLEAN_HOME" \
    USERPROFILE="$CLEAN_HOME" \
    TMPDIR="$CLEAN_TMP" \
    TEMP="$CLEAN_TMP" \
    TMP="$CLEAN_TMP" \
    PATH="$CLEAN_PATH" \
    LANG="C" \
    LC_ALL="C" \
    ONEAGENT_DISABLE_BROWSER="1" \
    ONEAGENT_CLI_BINARY="$ONEAGENT_CLI_BINARY" \
    "$@"
}

finish() {
  local status=$?
  trap - EXIT INT TERM
  local pid
  for pid in "${SERVER_PIDS[@]:-}"; do
    kill "$pid" >/dev/null 2>&1 || true
    wait "$pid" >/dev/null 2>&1 || true
  done
  snapshot_real_home "$AFTER_SNAPSHOT"
  if ! cmp -s "$BEFORE_SNAPSHOT" "$AFTER_SNAPSHOT"; then
    echo "macOS cleanroom modified a real user configuration path." >&2
    cp "$BEFORE_SNAPSHOT" "$ARTIFACT_DIR/real-home-before.json"
    cp "$AFTER_SNAPSHOT" "$ARTIFACT_DIR/real-home-after.json"
    status=1
  fi
  rm -rf "$CLEAN_ROOT"
  exit "$status"
}
trap finish EXIT INT TERM

assert_mode() {
  local expected="$1"
  local path="$2"
  local actual
  actual="$(stat -f %Lp "$path")"
  if [[ "$actual" != "$expected" ]]; then
    echo "unexpected mode for $path: expected $expected, got $actual" >&2
    return 1
  fi
}

wait_for_url() {
  local file="$1"
  local pid="$2"
  for _ in {1..100}; do
    if [[ -s "$file" ]]; then
      head -n 1 "$file"
      return 0
    fi
    if ! kill -0 "$pid" >/dev/null 2>&1; then
      echo "GUI process exited before publishing its URL." >&2
      return 1
    fi
    sleep 0.1
  done
  echo "Timed out waiting for GUI URL." >&2
  return 1
}

check_gui() {
  local label="$1"
  shift
  local url_file="$CLEAN_ROOT/$label.url"
  local log_file="$ARTIFACT_DIR/$label.log"
  clean_env "$@" > "$url_file" 2> "$log_file" &
  local pid=$!
  SERVER_PIDS+=("$pid")
  local url
  url="$(wait_for_url "$url_file" "$pid")"

  clean_env "$PYTHON_BIN" - "$url" "$REAL_HOME" <<'PY'
from __future__ import annotations

import json
import sys
from http.cookiejar import CookieJar
from urllib.error import HTTPError
from urllib.parse import urljoin
from urllib.request import HTTPCookieProcessor, Request, build_opener, urlopen

base = sys.argv[1]
real_home = sys.argv[2]
opener = build_opener(HTTPCookieProcessor(CookieJar()))
root = opener.open(base, timeout=5)
cookie = root.headers.get("Set-Cookie", "")
assert "HttpOnly" in cookie and "SameSite=Strict" in cookie, cookie
assert root.headers.get("Content-Security-Policy"), "CSP header is missing"

status = json.load(opener.open(urljoin(base, "api/status"), timeout=5))
assert status["platform"]["os"] == "macos", status["platform"]
for path in status["paths"].values():
    assert not str(path).startswith(real_home), path

payload = json.dumps({}).encode()
try:
    opener.open(Request(urljoin(base, "api/models"), data=payload, method="POST", headers={"Content-Type": "application/json"}), timeout=5)
except HTTPError as exc:
    assert exc.code == 403
    assert json.load(exc)["error_code"] == "INVALID_ORIGIN"
else:
    raise AssertionError("POST without Origin must be rejected")

origin = base.rstrip("/")
try:
    urlopen(Request(urljoin(base, "api/models"), data=payload, method="POST", headers={"Content-Type": "application/json", "Origin": origin}), timeout=5)
except HTTPError as exc:
    assert exc.code == 403
else:
    raise AssertionError("POST without session cookie must be rejected")
PY

  kill "$pid"
  wait "$pid" || true
}

snapshot_real_home "$BEFORE_SNAPSHOT"

for command_name in codex claude opencode kilo aider; do
  if clean_env "$PYTHON_BIN" -c 'import shutil, sys; raise SystemExit(0 if shutil.which(sys.argv[1]) else 1)' "$command_name"; then
    echo "Unexpected preinstalled Agent in macOS cleanroom PATH: $command_name" >&2
    exit 1
  fi
done

clean_env "$PYTHON_BIN" -m unittest \
  tests.test_core tests.test_cli tests.test_server tests.test_edge_cases \
  > "$ARTIFACT_DIR/python-contracts.log" 2>&1
clean_env /bin/bash tests/install_test.sh > "$ARTIFACT_DIR/bash-contracts.log" 2>&1
# Every stage above starts from an empty HOME, so a machine that already has
# Agent configuration on it -- the normal case for a tool meant to manage them
# over time -- was never exercised here.
clean_env /bin/bash tests/existing_config_test.sh > "$ARTIFACT_DIR/existing-config.log" 2>&1
clean_env "$PYTHON_BIN" tests/gui_smoke_test.py > "$ARTIFACT_DIR/gui-smoke.log" 2>&1

for agent in codex claude-code opencode kilo-cli aider; do
  clean_env env ONEAGENT_API_KEY="$MOCK_KEY" /bin/bash scripts/install.sh \
    --agent "$agent" \
    --provider custom \
    --api-base-url http://127.0.0.1:9/openai \
    --model cleanroom-model \
    --skip-test \
    --no-open \
    --json \
    --home "$CLEAN_HOME" \
    > "$ARTIFACT_DIR/configure-$agent.log"
done

clean_env env ONEAGENT_API_KEY="$MOCK_KEY" /bin/bash scripts/install.sh \
  --agent codex \
  --provider custom \
  --api-base-url http://127.0.0.1:9/openai \
  --model cleanroom-model \
  --skip-test \
  --no-open \
  --json \
  --home "$CLEAN_HOME" \
  > "$ARTIFACT_DIR/configure-codex-backup.log"

for directory in \
  "$CLEAN_HOME" \
  "$CLEAN_HOME/.codex" \
  "$CLEAN_HOME/.claude" \
  "$CLEAN_HOME/.config/opencode" \
  "$CLEAN_HOME/.config/kilo" \
  "$CLEAN_HOME/.oneagent"; do
  assert_mode 700 "$directory"
done

for file in \
  "$CLEAN_HOME/.codex/config.toml" \
  "$CLEAN_HOME/.claude/settings.json" \
  "$CLEAN_HOME/.config/opencode/opencode.jsonc" \
  "$CLEAN_HOME/.config/kilo/kilo.jsonc" \
  "$CLEAN_HOME/.oneagent/env" \
  "$CLEAN_HOME/.oneagent/aider.env" \
  "$CLEAN_HOME/.oneagent/profile.json"; do
  assert_mode 600 "$file"
done

backup_count=0
while IFS= read -r backup; do
  assert_mode 600 "$backup"
  backup_count=$((backup_count + 1))
done < <(find "$CLEAN_HOME" -type f -name '*.backup-*' -print)
if [[ "$backup_count" -lt 1 ]]; then
  echo "Expected at least one secured backup file." >&2
  exit 1
fi

if grep -Fq "$MOCK_KEY" "$CLEAN_HOME/.oneagent/profile.json"; then
  echo "Environment profile must not contain the API key." >&2
  exit 1
fi
if grep -R -Fq -- "$MOCK_KEY" "$ARTIFACT_DIR"; then
  echo "macOS cleanroom logs contain the mock API key." >&2
  exit 1
fi

check_gui source-gui "$PYTHON_BIN" scripts/gui.py --port 0 --no-open
check_gui packaged-gui "$PACKAGED_BINARY" gui --port 0 --no-open

echo "Real macOS cleanroom passed on $(uname -m)."
