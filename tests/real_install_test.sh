#!/usr/bin/env bash
# Really installs Codex and Claude Code, which no other suite does.
#
# Every other test replaces Runtime.runner, so "the Agent can be installed" was
# an assumption: nothing verified that the locked version resolves on a registry,
# that npm puts the executable somewhere PATH can reach, or that the binary
# reports the version the manifest pinned. Those are the steps that actually
# fail on a user's machine.
#
# Scope is deliberately the two Agents under verification. Isolation follows
# macos_cleanroom_test.sh: a clean HOME, env -i, and a snapshot of the real HOME
# before and after so a leak fails the run.
#
#   bash tests/real_install_test.sh                 # official registry
#   ONEAGENT_REGISTRY=npmmirror bash tests/...      # authorised mirror
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REAL_HOME="${HOME:?HOME is required}"
REGISTRY="${ONEAGENT_REGISTRY:-}"
ARTIFACT_DIR="${ONEAGENT_REAL_INSTALL_ARTIFACTS:-$ROOT_DIR/build/real-install}"

PYTHON_BIN="$(command -v python3.12 || true)"
NODE_BIN="$(command -v node || true)"
NPM_BIN="$(command -v npm || true)"
if [[ -z "$PYTHON_BIN" || -z "$NODE_BIN" || -z "$NPM_BIN" ]]; then
  echo "Python 3.12, Node and npm are required for the real install cleanroom." >&2
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
ln -s "$PYTHON_BIN" "$CLEAN_BIN/python3.12"
ln -s "$PYTHON_BIN" "$CLEAN_BIN/python3"
ln -s "$NODE_BIN" "$CLEAN_BIN/node"
ln -s "$NPM_BIN" "$CLEAN_BIN/npm"

# The prefix comes first so a globally installed Agent on the developer's own
# machine cannot satisfy an assertion this run is supposed to prove.
CLEAN_PATH="$NPM_PREFIX/bin:$CLEAN_BIN:/usr/bin:/bin:/usr/sbin:/sbin"

snapshot_real_home() {
  "$PYTHON_BIN" - "$REAL_HOME" "$1" <<'PY'
import hashlib, json, sys
from pathlib import Path

home, destination = Path(sys.argv[1]), Path(sys.argv[2])
watched = [
    ".codex/config.toml",
    ".claude/settings.json",
    ".config/opencode/opencode.jsonc",
    ".config/kilo/kilo.jsonc",
    ".oneagent",
]
state = {}
for relative in watched:
    path = home / relative
    if path.is_file():
        state[relative] = hashlib.sha256(path.read_bytes()).hexdigest()
    elif path.is_dir():
        state[relative] = sorted(
            f"{item.relative_to(path)}:{hashlib.sha256(item.read_bytes()).hexdigest()}"
            for item in sorted(path.rglob("*"))
            if item.is_file()
        )
    else:
        state[relative] = None
destination.write_text(json.dumps(state, indent=2, sort_keys=True), encoding="utf-8")
PY
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
    npm_config_prefix="$NPM_PREFIX" \
    npm_config_update_notifier="false" \
    npm_config_fund="false" \
    npm_config_audit="false" \
    ONEAGENT_DISABLE_BROWSER="1" \
    "$@"
}

finish() {
  local status=$?
  trap - EXIT INT TERM
  snapshot_real_home "$AFTER_SNAPSHOT"
  if ! cmp -s "$BEFORE_SNAPSHOT" "$AFTER_SNAPSHOT"; then
    echo "Real install cleanroom modified a real user configuration path." >&2
    cp "$BEFORE_SNAPSHOT" "$ARTIFACT_DIR/real-home-before.json"
    cp "$AFTER_SNAPSHOT" "$ARTIFACT_DIR/real-home-after.json"
    status=1
  fi
  rm -rf "$CLEAN_ROOT"
  exit "$status"
}

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

snapshot_real_home "$BEFORE_SNAPSHOT"
trap finish EXIT INT TERM

locked_version() {
  "$PYTHON_BIN" -c "
import json,sys
print(json.load(open('$ROOT_DIR/agents.lock.json'))['agents'][sys.argv[1]]['package']['version'])
" "$1"
}

echo "== 起点：两个 Agent 都不存在 =="
for command_name in codex claude; do
  if clean_env command -v "$command_name" >/dev/null 2>&1; then
    fail "$command_name already resolves inside the cleanroom; the run would prove nothing"
  fi
done
for relative in .codex .claude; do
  [[ -e "$CLEAN_HOME/$relative" ]] && fail "clean HOME already holds $relative"
done
echo "  ok"

echo "== 安装锁定版本（registry: ${REGISTRY:-official}）=="
registry_args=()
[[ -n "$REGISTRY" ]] && registry_args=(--registry "$REGISTRY")
clean_env env ONEAGENT_API_KEY="$MOCK_KEY" "$PYTHON_BIN" -m oneagent.cli \
  --agent codex,claude-code \
  --install-agent \
  --locked-version \
  --check-agent-only \
  --json \
  --home "$CLEAN_HOME" \
  "${registry_args[@]}" \
  > "$ARTIFACT_DIR/install.json" 2> "$ARTIFACT_DIR/install.err" \
  || { cat "$ARTIFACT_DIR/install.err" >&2; fail "installation failed"; }
echo "  ok"

echo "== 环节3：可执行文件落到 PATH =="
for command_name in codex claude; do
  resolved="$(clean_env command -v "$command_name" 2>/dev/null || true)"
  [[ -n "$resolved" ]] || fail "$command_name did not land anywhere on PATH after installing"
  echo "  $command_name -> $resolved"
done

echo "== 环节4：版本等于锁定版本 =="
for agent in codex claude-code; do
  expected="$(locked_version "$agent")"
  command_name="codex"; [[ "$agent" == "claude-code" ]] && command_name="claude"
  reported="$(clean_env "$command_name" --version 2>&1 | head -1)"
  case "$reported" in
    *"$expected"*) echo "  $command_name reports $reported (locked $expected)" ;;
    *) fail "$command_name reports '$reported' but the manifest locks $expected" ;;
  esac
done

echo "== OneAgent 自己的检测与锁定版本一致 =="
clean_env "$PYTHON_BIN" - "$CLEAN_HOME" <<'PY' || fail "status did not report both Agents as installed at the locked version"
import sys
from pathlib import Path

sys.path.insert(0, ".")
from oneagent.installer import Runtime, status_payload

payload = status_payload(Runtime.create(home=Path(sys.argv[1])))
ok = True
for agent_id in ("codex", "claude-code"):
    agent = payload["agents"][agent_id]
    matched = agent["installed"] and agent["version"] == agent["lockedVersion"]
    print(f"  {agent_id}: installed={agent['installed']} version={agent['version']} locked={agent['lockedVersion']}")
    ok = ok and matched
sys.exit(0 if ok else 1)
PY

echo "== 配置写入干净 HOME 且权限正确 =="
clean_env env ONEAGENT_API_KEY="$MOCK_KEY" "$PYTHON_BIN" -m oneagent.cli \
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

echo "== 密钥不落入 profile 与日志 =="
if grep -Fq "$MOCK_KEY" "$CLEAN_HOME/.oneagent/profile.json" 2>/dev/null; then
  fail "the environment profile contains the API key"
fi
if grep -R -Fq -- "$MOCK_KEY" "$ARTIFACT_DIR"; then
  fail "cleanroom artifacts contain the API key"
fi
echo "  ok"

echo
echo "Real install cleanroom passed on $(uname -s) $(uname -m) (registry: ${REGISTRY:-official})."
