#!/usr/bin/env bash
# Detection against configuration OneAgent did not write.
#
# Every cleanroom starts from an empty HOME, so the case that matters most for a
# tool meant to manage Agents over time never appeared in any of them: a machine
# that already has configuration on it. status_payload used to report such a
# machine as configured=True with provider, model and baseUrl all null -- the
# file was seen but never read.
#
# Pure file operations, no network and no package manager, so both cleanrooms and
# ordinary CI can run it.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PYTHON_BIN="${ONEAGENT_PYTHON:-python3.12}"
HOME_DIR="$(mktemp -d "${TMPDIR:-/tmp}/oneagent-existing-config.XXXXXX")"
SENTINEL="sk-existing-config-must-not-surface"

cleanup() { rm -rf "$HOME_DIR"; }
trap cleanup EXIT INT TERM

fail() { echo "FAIL: $*" >&2; exit 1; }

mkdir -p \
  "$HOME_DIR/.codex" \
  "$HOME_DIR/.claude" \
  "$HOME_DIR/.config/opencode" \
  "$HOME_DIR/.config/kilo" \
  "$HOME_DIR/.oneagent"

# 1. Hand-written Codex config pointing at a third party, with a comment and an
#    unrelated table the user cares about.
cat > "$HOME_DIR/.codex/config.toml" <<EOF
# my own notes
model_provider = "vendor"
model = "gpt-5-mini"

[model_providers.vendor]
base_url = "https://api.other-vendor.com/v1"
env_key = "VENDOR_KEY"

[tui]
theme = "dark"
EOF

# 2. Claude Code configured by another tool: endpoint and credential present,
#    but not the full set our adapter writes.
cat > "$HOME_DIR/.claude/settings.json" <<EOF
{
  "env": {
    "ANTHROPIC_BASE_URL": "https://api.third-party.com",
    "ANTHROPIC_AUTH_TOKEN": "$SENTINEL"
  },
  "theme": "dark"
}
EOF

# 3. OpenCode with extra user fields that must survive a later write.
cat > "$HOME_DIR/.config/opencode/opencode.jsonc" <<EOF
{
  "provider": {
    "mine": { "options": { "baseURL": "https://mine.example/v1", "apiKey": "$SENTINEL" } }
  },
  "model": "mine/local-llm",
  "keybinds": { "leader": "ctrl+x" }
}
EOF

# 4. A broken file: must be reported, must not take the status request down.
printf '{"provider": broken' > "$HOME_DIR/.config/kilo/kilo.jsonc"

# 5. Aider script written by hand.
cat > "$HOME_DIR/.oneagent/aider.env" <<EOF
export OPENAI_API_BASE='https://hand-written.example/v1'
export OPENAI_API_KEY='$SENTINEL'
EOF

cd "$ROOT_DIR"
"$PYTHON_BIN" - "$HOME_DIR" "$SENTINEL" <<'PY'
import json
import sys
from pathlib import Path

sys.path.insert(0, ".")
from oneagent.installer import InstallOptions, Runtime, install_many, status_payload

home, sentinel = Path(sys.argv[1]), sys.argv[2]
runtime = Runtime.create(home=home)
payload = status_payload(runtime)
problems = []

expected = {
    "codex": ("https://api.other-vendor.com/v1", "gpt-5-mini"),
    "claude-code": ("https://api.third-party.com", ""),
    "opencode": ("https://mine.example/v1", "local-llm"),
    "aider": ("https://hand-written.example/v1", ""),
}
for agent_id, (base_url, model) in expected.items():
    detected = payload["agents"][agent_id]["detected"]
    if detected is None:
        problems.append(f"{agent_id}: nothing detected")
        continue
    if detected["baseUrl"] != base_url:
        problems.append(f"{agent_id}: baseUrl {detected['baseUrl']!r} != {base_url!r}")
    if model and detected["model"] != model:
        problems.append(f"{agent_id}: model {detected['model']!r} != {model!r}")
    if detected["managedByOneAgent"]:
        problems.append(f"{agent_id}: reported as OneAgent-managed, but we did not write it")
    print(f"  {agent_id:12} {detected['baseUrl']}  {detected['model'] or '-'}")

# The binding is empty: this is exactly the state that used to render as
# "not configured" while a live configuration sat on disk.
if payload["agents"]["codex"]["provider"] is not None:
    problems.append("codex: a binding exists, so this is not the external-config case")

broken = payload["agents"]["kilo-cli"]["detected"]
if not broken or not broken["unreadable"]:
    problems.append("kilo-cli: a broken config was not reported as unreadable")
else:
    print(f"  {'kilo-cli':12} unreadable: {broken['unreadable'][:48]}")

if sentinel in json.dumps(payload, ensure_ascii=False):
    problems.append("a credential from an existing config reached the status payload")

# Writing over an existing config must keep the fields the user owns.
install_many(
    InstallOptions(
        agents=["opencode"],
        provider="ppio",
        api_key="K-OVERWRITE",
        model="new-model",
        configure=True,
        skip_test=True,
        home=home,
    ),
    runtime,
)
after = json.loads((home / ".config" / "opencode" / "opencode.jsonc").read_text(encoding="utf-8"))
if after.get("keybinds", {}).get("leader") != "ctrl+x":
    problems.append("writing the config dropped a field the user owns")
if "mine" not in after.get("provider", {}):
    problems.append("writing the config dropped the user's own provider entry")
detected = status_payload(runtime)["agents"]["opencode"]["detected"]
if not detected["managedByOneAgent"]:
    problems.append("after our write, the config is not reported as OneAgent-managed")
print(f"  {'opencode':12} after write: {detected['baseUrl']}  managed={detected['managedByOneAgent']}")

if problems:
    print("\n".join(f"FAIL: {problem}" for problem in problems))
    raise SystemExit(1)
PY

echo "Existing-configuration detection passed."
