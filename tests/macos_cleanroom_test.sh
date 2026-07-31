#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REAL_HOME="${HOME:?HOME is required}"
ARTIFACT_DIR="${ONEAGENT_MACOS_CLEANROOM_ARTIFACTS:-$ROOT_DIR/build/macos-cleanroom}"
NODE_BIN="$(command -v node || true)"
NPM_BIN="$(command -v npm || true)"
GO_BIN="$(command -v go || true)"
if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "macOS cleanroom requires a real Darwin host." >&2
  exit 2
fi
if [[ -z "$NODE_BIN" || -z "$NPM_BIN" || -z "$GO_BIN" ]]; then
  echo "Node, npm and Go are required for the macOS cleanroom." >&2
  exit 2
fi

# Build the binary before the sanitized PATH is assembled. The cleanroom then
# exercises the same forwarding wrapper users invoke, without a Go toolchain.
ONEAGENT_CLI_BINARY="${ONEAGENT_CLI_BINARY:-$ROOT_DIR/bin/oneagent}"
(cd "$ROOT_DIR" && "$GO_BIN" build -o "$ONEAGENT_CLI_BINARY" ./cmd/oneagent)
export ONEAGENT_CLI_BINARY

CLEAN_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/oneagent-macos-cleanroom.XXXXXX")"
CLEAN_HOME="$CLEAN_ROOT/home"
CLEAN_TMP="$CLEAN_ROOT/tmp"
FAKE_BIN="$CLEAN_ROOT/bin"
BEFORE_SNAPSHOT="$CLEAN_ROOT/real-home-before.json"
AFTER_SNAPSHOT="$CLEAN_ROOT/real-home-after.json"
MOCK_KEY="oneagent-macos-cleanroom-placeholder"
mkdir -p "$CLEAN_HOME" "$CLEAN_TMP" "$FAKE_BIN" "$ARTIFACT_DIR"
chmod 0700 "$CLEAN_HOME" "$CLEAN_TMP" "$FAKE_BIN"
ln -s "$NODE_BIN" "$FAKE_BIN/node"
ln -s "$NPM_BIN" "$FAKE_BIN/npm"
printf '%s\n' '#!/bin/bash' 'echo "fake uv must not install a real package" >&2' 'exit 97' > "$FAKE_BIN/uv"
chmod 0755 "$FAKE_BIN/uv"

CLEAN_PATH="$FAKE_BIN:/usr/bin:/bin:/usr/sbin:/sbin"
rm -rf "$ARTIFACT_DIR"
mkdir -p "$ARTIFACT_DIR"

snapshot_home() {
  "$NODE_BIN" "$ROOT_DIR/scripts/snapshot-home.mjs" "$1" "$2"
}

clean_env() {
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
  snapshot_home "$REAL_HOME" "$AFTER_SNAPSHOT"
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
  local expected="$1" path="$2" actual
  actual="$(stat -f %Lp "$path")"
  [[ "$actual" == "$expected" ]] || {
    echo "unexpected mode for $path: expected $expected, got $actual" >&2
    return 1
  }
}

snapshot_home "$REAL_HOME" "$BEFORE_SNAPSHOT"

for command_name in codex claude opencode kilo aider; do
  if clean_env command -v "$command_name" >/dev/null 2>&1; then
    echo "Unexpected preinstalled Agent in macOS cleanroom PATH: $command_name" >&2
    exit 1
  fi
done

clean_env /bin/bash tests/install_test.sh > "$ARTIFACT_DIR/install-contracts.log" 2>&1

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
[[ "$backup_count" -ge 1 ]] || { echo "Expected a secured backup file." >&2; exit 1; }

if grep -Fq "$MOCK_KEY" "$CLEAN_HOME/.oneagent/profile.json"; then
  echo "Environment profile must not contain the API key." >&2
  exit 1
fi
if grep -R -Fq -- "$MOCK_KEY" "$ARTIFACT_DIR"; then
  echo "macOS cleanroom logs contain the mock API key." >&2
  exit 1
fi

echo "Real macOS cleanroom passed on $(uname -m)."
