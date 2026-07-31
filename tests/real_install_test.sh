#!/usr/bin/env bash
# Really installs the locked npm Agents and verifies PATH, versions, and config
# in an isolated HOME. Aider is intentionally outside this check because its
# upstream installer has a separate optional runtime prerequisite.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REAL_HOME="${HOME:?HOME is required}"
REGISTRY="${ONEAGENT_REGISTRY:-}"
ARTIFACT_DIR="${ONEAGENT_REAL_INSTALL_ARTIFACTS:-$ROOT_DIR/build/real-install}"
CLI_BINARY="${ONEAGENT_CLI_BINARY:-$ROOT_DIR/bin/oneagent}"
NODE_BIN="$(command -v node || true)"
NPM_BIN="$(command -v npm || true)"
if [[ ! -x "$CLI_BINARY" || -z "$NODE_BIN" || -z "$NPM_BIN" ]]; then
  echo "The Go CLI, Node and npm are required for the real install cleanroom." >&2
  exit 2
fi

CLEAN_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/oneagent-real-install.XXXXXX")"
CLEAN_HOME="$CLEAN_ROOT/home"
CLEAN_TMP="$CLEAN_ROOT/tmp"
CLEAN_BIN="$CLEAN_ROOT/bin"
NPM_PREFIX="$CLEAN_ROOT/npm-prefix"
BEFORE_SNAPSHOT="$CLEAN_ROOT/real-home-before.json"
AFTER_SNAPSHOT="$CLEAN_ROOT/real-home-after.json"
MOCK_KEY="oneagent-real-install-placeholder"

rm -rf "$ARTIFACT_DIR"
mkdir -p "$CLEAN_HOME" "$CLEAN_TMP" "$CLEAN_BIN" "$NPM_PREFIX" "$ARTIFACT_DIR"
chmod 0700 "$CLEAN_HOME" "$CLEAN_TMP"
ln -s "$NODE_BIN" "$CLEAN_BIN/node"
ln -s "$NPM_BIN" "$CLEAN_BIN/npm"

# Keep the isolated npm prefix first so a globally installed Agent cannot satisfy
# an assertion this run is supposed to prove.
CLEAN_PATH="$NPM_PREFIX/bin:$CLEAN_BIN:/usr/bin:/bin:/usr/sbin:/sbin"

snapshot_home() {
  "$NODE_BIN" "$ROOT_DIR/scripts/snapshot-home.mjs" "$1" "$2"
}

snapshot_home "$REAL_HOME" "$BEFORE_SNAPSHOT"

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
    npm_config_prefix="$NPM_PREFIX" \
    npm_config_update_notifier="false" \
    npm_config_fund="false" \
    npm_config_audit="false" \
    ONEAGENT_DISABLE_BROWSER="1" \
    ONEAGENT_CLI_BINARY="$CLI_BINARY" \
    "$@"
}

finish() {
  local status=$?
  trap - EXIT INT TERM
  snapshot_home "$REAL_HOME" "$AFTER_SNAPSHOT"
  if ! cmp -s "$BEFORE_SNAPSHOT" "$AFTER_SNAPSHOT"; then
    echo "Real install cleanroom modified a real user configuration path." >&2
    cp "$BEFORE_SNAPSHOT" "$ARTIFACT_DIR/real-home-before.json"
    cp "$AFTER_SNAPSHOT" "$ARTIFACT_DIR/real-home-after.json"
    status=1
  fi
  rm -rf "$CLEAN_ROOT"
  exit "$status"
}
trap finish EXIT INT TERM

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

locked_version() {
  LOCK_FILE="$ROOT_DIR/agents.lock.json" AGENT_ID="$1" "$NODE_BIN" --input-type=module <<'NODE'
import { readFileSync } from "node:fs";
const lock = JSON.parse(readFileSync(process.env.LOCK_FILE, "utf8"));
console.log(lock.agents[process.env.AGENT_ID].package.version);
NODE
}

echo "== Starting with no npm Agent binaries =="
for command_name in codex claude; do
  if clean_env command -v "$command_name" >/dev/null 2>&1; then
    fail "$command_name already resolves inside the cleanroom"
  fi
done
for relative in .codex .claude; do
  [[ -e "$CLEAN_HOME/$relative" ]] && fail "clean HOME already holds $relative"
done

echo "== Installing locked versions (registry: ${REGISTRY:-official}) =="
registry_args=()
[[ -n "$REGISTRY" ]] && registry_args=(--registry "$REGISTRY")
clean_env env ONEAGENT_API_KEY="$MOCK_KEY" "$CLI_BINARY" \
  --agent codex,claude-code \
  --install-agent \
  --locked-version \
  --check-agent-only \
  --json \
  --home "$CLEAN_HOME" \
  "${registry_args[@]}" \
  > "$ARTIFACT_DIR/install.json" 2> "$ARTIFACT_DIR/install.err" \
  || { cat "$ARTIFACT_DIR/install.err" >&2; fail "installation failed"; }

echo "== Verifying binaries and locked versions =="
for agent in codex claude-code; do
  expected="$(locked_version "$agent")"
  command_name="codex"
  [[ "$agent" == "claude-code" ]] && command_name="claude"
  resolved="$(clean_env command -v "$command_name" 2>/dev/null || true)"
  [[ -n "$resolved" ]] || fail "$command_name did not land on PATH"
  reported="$(clean_env "$command_name" --version 2>&1 | head -1)"
  case "$reported" in
    *"$expected"*) echo "  $command_name reports $reported (locked $expected)" ;;
    *) fail "$command_name reports '$reported' but the lock says $expected" ;;
  esac
done

echo "== Verifying OneAgent status =="
clean_env "$CLI_BINARY" status --json --home "$CLEAN_HOME" > "$ARTIFACT_DIR/status.json" \
  || fail "status command failed"
STATUS_FILE="$ARTIFACT_DIR/status.json" LOCK_FILE="$ROOT_DIR/agents.lock.json" "$NODE_BIN" --input-type=module <<'NODE'
import { readFileSync } from "node:fs";
const status = JSON.parse(readFileSync(process.env.STATUS_FILE, "utf8"));
const lock = JSON.parse(readFileSync(process.env.LOCK_FILE, "utf8"));
for (const id of ["codex", "claude-code"]) {
  const agent = status.agents?.[id];
  if (!agent?.installed || agent.version !== lock.agents[id].package.version) process.exit(1);
}
NODE

echo "== Verifying config writes and permissions =="
clean_env env ONEAGENT_API_KEY="$MOCK_KEY" "$CLI_BINARY" \
  --agent codex,claude-code \
  --provider custom \
  --api-base-url http://127.0.0.1:9/openai \
  --model real-install-model \
  --skip-test \
  --json \
  --home "$CLEAN_HOME" \
  > "$ARTIFACT_DIR/configure.json" 2> "$ARTIFACT_DIR/configure.err" \
  || { cat "$ARTIFACT_DIR/configure.err" >&2; fail "configuration failed"; }

for file in "$CLEAN_HOME/.codex/config.toml" "$CLEAN_HOME/.claude/settings.json"; do
  [[ -f "$file" ]] || fail "$file was not written"
  mode="$(stat -f %Lp "$file" 2>/dev/null || stat -c %a "$file")"
  [[ "$mode" == "600" ]] || fail "$file has mode $mode, expected 600"
  echo "  $mode $(basename "$file")"
done

if grep -Fq "$MOCK_KEY" "$CLEAN_HOME/.oneagent/profile.json" 2>/dev/null; then
  fail "the environment profile contains the API key"
fi
if grep -R -Fq -- "$MOCK_KEY" "$ARTIFACT_DIR"; then
  fail "cleanroom artifacts contain the API key"
fi

echo
echo "Real install cleanroom passed on $(uname -s) $(uname -m) (registry: ${REGISTRY:-official})."
