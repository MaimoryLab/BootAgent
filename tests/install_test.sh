#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALLER="$ROOT_DIR/scripts/install.sh"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

assert_file_contains() {
  local file="$1"
  local expected="$2"
  grep -Fq "$expected" "$file" || fail "$file missing: $expected"
}

test_codex_writes_config_and_env() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  HOME="$tmp" "$INSTALLER" \
    --agent codex \
    --api-base-url https://models.example.com/v1 \
    --api-key sk-test \
    --model gpt-test \
    --channel test \
    --skip-test \
    --no-open >/dev/null

  assert_file_contains "$tmp/.codex/config.toml" 'model_provider = "oneagent"'
  assert_file_contains "$tmp/.codex/config.toml" 'model = "gpt-test"'
  assert_file_contains "$tmp/.codex/config.toml" 'base_url = "https://models.example.com/v1"'
  assert_file_contains "$tmp/.codex/config.toml" 'env_key = "ONEAGENT_API_KEY_CODEX"'
  assert_file_contains "$tmp/.oneagent/agents/codex.env" "export ONEAGENT_API_KEY_CODEX=sk-test"
  # The shared file stays for configs written before the per-Agent split.
  assert_file_contains "$tmp/.oneagent/env" "export ONEAGENT_API_KEY=sk-test"
}

test_codex_backs_up_existing_config() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  mkdir -p "$tmp/.codex"
  echo 'model = "old"' > "$tmp/.codex/config.toml"

  HOME="$tmp" "$INSTALLER" \
    --agent codex \
    --api-base-url https://models.example.com/v1 \
    --api-key sk-test \
    --skip-test \
    --no-open >/dev/null

  find "$tmp/.codex" -name 'config.toml.backup-*' | grep -q . || fail "missing Codex backup"
}

test_ppio_is_default_provider() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  HOME="$tmp" "$INSTALLER" \
    --agent codex \
    --api-key sk-test \
    --model gpt-test \
    --skip-test \
    --no-open >/dev/null

  assert_file_contains "$tmp/.codex/config.toml" 'name = "PPIO"'
  assert_file_contains "$tmp/.codex/config.toml" 'base_url = "https://api.ppio.com/openai"'
}

test_novita_provider() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  HOME="$tmp" "$INSTALLER" \
    --agent codex \
    --provider novita \
    --api-key sk-test \
    --model gpt-test \
    --skip-test \
    --no-open >/dev/null

  assert_file_contains "$tmp/.codex/config.toml" 'name = "Novita"'
  assert_file_contains "$tmp/.codex/config.toml" 'base_url = "https://api.novita.ai/openai"'
}

test_rowita_is_rejected() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  if HOME="$tmp" "$INSTALLER" \
    --agent codex \
    --provider rowita \
    --api-key sk-test \
    --skip-test \
    --no-open >/dev/null 2>&1; then
    fail "rowita provider should be rejected"
  fi
}

test_register_url_override_is_documented_in_help() {
  "$INSTALLER" --help | grep -Fq -- "--register-url URL" || fail "missing --register-url help"
}

test_check_agent_only_does_not_require_key_or_write_config() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  HOME="$tmp" "$INSTALLER" \
    --agent codex \
    --check-agent-only \
    --no-open >/dev/null

  [ ! -e "$tmp/.codex/config.toml" ] || fail "check-agent-only should not write Codex config"
  [ ! -e "$tmp/.oneagent/env" ] || fail "check-agent-only should not write env file"
}

test_readme_does_not_recommend_plaintext_api_key_arg() {
  if grep -Fq -- "--api-key sk-your-key" "$ROOT_DIR/README.md"; then
    fail "README should not recommend plaintext --api-key"
  fi
}

test_missing_agent_message_points_to_official_install() {
  local tmp out
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  mkdir -p "$tmp/bin"
  out="$(
    PATH="$tmp/bin:/usr/bin:/bin" HOME="$tmp" "$INSTALLER" \
      --agent codex \
      --api-key sk-test \
      --skip-test \
      --no-open
  )"

  printf "%s" "$out" | grep -Fq "official install:" || fail "missing official install hint"
}

test_claude_merges_settings_json() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  mkdir -p "$tmp/.claude"
  printf '{"permissions":{"allow":["Bash(ls)"]},"env":{"FOO":"bar"}}\n' > "$tmp/.claude/settings.json"

  HOME="$tmp" "$INSTALLER" \
    --agent claude-code \
    --api-base-url https://anthropic.example.com \
    --api-key claude-test \
    --model sonnet \
    --skip-test \
    --no-open >/dev/null

  JSON_FILE="$tmp/.claude/settings.json" node --input-type=module <<'NODE'
import { readFileSync } from "node:fs";
const data = JSON.parse(readFileSync(process.env.JSON_FILE, "utf8"));
if (JSON.stringify(data.permissions?.allow) !== JSON.stringify(["Bash(ls)"]) ||
    data.env?.FOO !== "bar" ||
    data.env?.ANTHROPIC_BASE_URL !== "https://anthropic.example.com" ||
    data.env?.ANTHROPIC_AUTH_TOKEN !== "claude-test" ||
    data.env?.ANTHROPIC_MODEL !== "sonnet") process.exit(1);
NODE
}

test_opencode_writes_openai_compatible_config() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  HOME="$tmp" "$INSTALLER" \
    --agent opencode \
    --provider ppio \
    --api-key sk-test \
    --model gpt-test \
    --skip-test \
    --no-open >/dev/null

  JSON_FILE="$tmp/.config/opencode/opencode.jsonc" node --input-type=module <<'NODE'
import { readFileSync } from "node:fs";
const data = JSON.parse(readFileSync(process.env.JSON_FILE, "utf8"));
const provider = data.provider?.oneagent;
if (data.model !== "oneagent/gpt-test" || provider?.npm !== "@ai-sdk/openai-compatible" ||
    provider?.options?.baseURL !== "https://api.ppio.com/openai/v1" ||
    provider?.options?.apiKey !== "{env:ONEAGENT_API_KEY_OPENCODE}" ||
    provider?.models?.["gpt-test"]?.name !== "gpt-test") process.exit(1);
NODE
}

test_kilo_writes_openai_compatible_config() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  HOME="$tmp" "$INSTALLER" \
    --agent kilo-cli \
    --api-base-url https://models.example.com/openai/v1/chat/completions \
    --api-key sk-test \
    --model gpt-test \
    --skip-test \
    --no-open >/dev/null

  JSON_FILE="$tmp/.config/kilo/kilo.jsonc" node --input-type=module <<'NODE'
import { readFileSync } from "node:fs";
const data = JSON.parse(readFileSync(process.env.JSON_FILE, "utf8"));
if (data.provider?.oneagent?.options?.baseURL !== "https://models.example.com/openai/v1") process.exit(1);
NODE
}

test_aider_writes_independent_env() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  HOME="$tmp" "$INSTALLER" \
    --agent aider \
    --provider novita \
    --api-key sk-test \
    --model gpt-test \
    --skip-test \
    --no-open >/dev/null

  assert_file_contains "$tmp/.oneagent/aider.env" "export OPENAI_API_BASE=https://api.novita.ai/openai/v1"
  assert_file_contains "$tmp/.oneagent/aider.env" "export OPENAI_API_KEY=sk-test"
  [ ! -e "$tmp/.oneagent/env" ] || fail "aider should not write shared env file"
}

test_codex_writes_config_and_env
test_codex_backs_up_existing_config
test_ppio_is_default_provider
test_novita_provider
test_rowita_is_rejected
test_register_url_override_is_documented_in_help
test_check_agent_only_does_not_require_key_or_write_config
test_readme_does_not_recommend_plaintext_api_key_arg
test_missing_agent_message_points_to_official_install
test_claude_merges_settings_json
test_opencode_writes_openai_compatible_config
test_kilo_writes_openai_compatible_config
test_aider_writes_independent_env

echo "install tests passed"
