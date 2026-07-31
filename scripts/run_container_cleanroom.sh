#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACT_DIR="${ONEAGENT_ARTIFACT_DIR:-/artifacts}"
expected_platform="linux"

if [[ "${ONEAGENT_CLEANROOM_SANITIZED:-0}" != "1" ]]; then
  # The Go CLI is built into the image, not here: this stage runs with
  # --network none and the sanitized PATH below carries no Go toolchain.
  # scripts/install.sh is a pure forwarding layer and needs the binary named.
  exec env -i \
    PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" \
    LANG="C.UTF-8" \
    LC_ALL="C.UTF-8" \
    PLAYWRIGHT_BROWSERS_PATH="/ms-playwright" \
    ONEAGENT_CLEANROOM_SANITIZED="1" \
    ONEAGENT_DISABLE_BROWSER="1" \
    ONEAGENT_ARTIFACT_DIR="$ARTIFACT_DIR" \
    ONEAGENT_CLI_BINARY="${ONEAGENT_CLI_BINARY:-$ROOT_DIR/bin/oneagent}" \
    bash "$0"
fi

if [[ ! -x "${ONEAGENT_CLI_BINARY:-}" ]]; then
  echo "The Go CLI must be built into the image before the cleanroom runs." >&2
  exit 2
fi

mkdir -p "$ARTIFACT_DIR/logs"
HOME="$(mktemp -d /tmp/oneagent-cleanroom-home.XXXXXX)"
TMPDIR="$(mktemp -d /tmp/oneagent-cleanroom-tmp.XXXXXX)"
export HOME TMPDIR

REPORT="$ARTIFACT_DIR/cleanroom-report.json"
STATUS="failed"
FAILED_STAGE="initialization"

write_report() {
  local finished_at
  local architecture
  finished_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  architecture="$(uname -m)"
  printf '{\n  "schemaVersion": 1,\n  "environment": "docker-cleanroom",\n  "platform": "%s",\n  "arch": "%s",\n  "network": "none",\n  "status": "%s",\n  "failedStage": "%s",\n  "finishedAt": "%s"\n}\n' \
    "$expected_platform" "$architecture" "$STATUS" "$FAILED_STAGE" "$finished_at" > "$REPORT"
}

collect_artifacts() {
  if [[ -f "$ROOT_DIR/frontend/coverage/coverage-summary.json" ]]; then
    cp -f "$ROOT_DIR/frontend/coverage/coverage-summary.json" "$ARTIFACT_DIR/coverage-react.json" || true
  fi
  if [[ -d "$ROOT_DIR/frontend/test-results" ]]; then
    rm -rf "$ARTIFACT_DIR/playwright-test-results"
    cp -R "$ROOT_DIR/frontend/test-results" "$ARTIFACT_DIR/playwright-test-results" || true
  fi
  if [[ -d "$ROOT_DIR/frontend/playwright-report" ]]; then
    rm -rf "$ARTIFACT_DIR/playwright-report"
    cp -R "$ROOT_DIR/frontend/playwright-report" "$ARTIFACT_DIR/playwright-report" || true
  fi
}

finish() {
  local exit_code=$?
  trap - EXIT
  collect_artifacts
  write_report
  rm -rf "$HOME" "$TMPDIR"
  exit "$exit_code"
}
trap finish EXIT

run_stage() {
  local name="$1"
  shift
  FAILED_STAGE="$name"
  echo "==> $name"
  "$@" \
    > >(tee "$ARTIFACT_DIR/logs/$name.stdout.log") \
    2> >(tee "$ARTIFACT_DIR/logs/$name.stderr.log" >&2)
}

assert_clean_environment() {
  [[ "$(uname -s)" == "Linux" ]] || {
    echo "Docker cleanroom must report the real Linux platform." >&2
    return 1
  }
  [[ "$(id -u)" != "0" ]] || {
    echo "Docker cleanroom must run as a non-root user." >&2
    return 1
  }

  local variable
  while IFS='=' read -r variable _; do
    if [[ "$variable" == *KEY* || "$variable" == *TOKEN* || "$variable" == *SECRET* || "$variable" == *PASSWORD* ]]; then
      echo "secret-shaped environment variable remains: $variable" >&2
      return 1
    fi
  done < <(env)

  local command_name
  for command_name in codex claude opencode kilo aider uv; do
    if command -v "$command_name" >/dev/null 2>&1; then
      echo "unexpected preinstalled command: $command_name" >&2
      return 1
    fi
  done

  local relative
  for relative in .codex .claude .config/opencode .config/kilo .oneagent .aider.conf.yml; do
    if [[ -e "$HOME/$relative" ]]; then
      echo "unexpected user configuration: $HOME/$relative" >&2
      return 1
    fi
  done
}

assert_go_cli_isolated() {
  local probe_home probe_path status
  probe_home="$(mktemp -d "$TMPDIR/oneagent-go-cli.XXXXXX")"
  # No Node or package manager on PATH: the headless Go path must still run.
  probe_path="/usr/bin:/bin"

  if env -i HOME="$probe_home" PATH="$probe_path" TMPDIR="$TMPDIR" \
    "$ONEAGENT_CLI_BINARY" --version >/dev/null; then
    :
  else
    echo "the Go CLI could not report its version in the isolated PATH" >&2
    rm -rf "$probe_home"
    return 1
  fi

  env -i HOME="$probe_home" PATH="$probe_path" TMPDIR="$TMPDIR" \
    "$ONEAGENT_CLI_BINARY" --agent codex \
    --api-base-url https://models.example.com/v1 \
    --api-key cleanroom-placeholder-key \
    --model cleanroom-model \
    --skip-test --no-open --json > "$probe_home/install.json"
  status=$?
  if [[ "$status" -ne 0 ]]; then
    echo "the Go CLI could not configure an Agent in the isolated PATH" >&2
    rm -rf "$probe_home"
    return 1
  fi

  if ! grep -Fq 'model_provider = "oneagent"' "$probe_home/.codex/config.toml"; then
    echo "the Go CLI did not write the expected Codex configuration" >&2
    rm -rf "$probe_home"
    return 1
  fi
  if grep -Fq "cleanroom-placeholder-key" "$probe_home/install.json"; then
    echo "the Go CLI leaked the API key into its JSON result" >&2
    rm -rf "$probe_home"
    return 1
  fi
  rm -rf "$probe_home"
}

scan_release_policy() {
  if find "$ROOT_DIR/frontend/dist" -type f -name '*.map' -print -quit | grep -q .; then
    echo "frontend build contains source maps" >&2
    return 1
  fi
  if rg -n 'https?://(fonts\.|cdn\.|unpkg\.|jsdelivr\.)' \
    "$ROOT_DIR/frontend/dist"; then
    echo "frontend build contains a forbidden remote asset" >&2
    return 1
  fi
  if rg -n 'shell\s*=\s*True' "$ROOT_DIR/internal" "$ROOT_DIR/cmd" "$ROOT_DIR/scripts"; then
    echo "shell subprocess invocation is forbidden" >&2
    return 1
  fi
  if rg -n 'curl[^|\n]*\|\s*(bash|sh)' \
    "$ROOT_DIR/oneagent" "$ROOT_DIR/scripts" "$ROOT_DIR/frontend/src"; then
    echo "executable curl-pipe installation is forbidden" >&2
    return 1
  fi
  if rg -n -o 'sk-[A-Za-z0-9_-]{20,}|Bearer[[:space:]]+[A-Za-z0-9._-]{24,}' \
    --glob '!frontend/node_modules/**' \
    --glob '!build/**' \
    --glob '!release/**' \
    --glob '!output/**' \
    "$ROOT_DIR"; then
    echo "possible secret found in source or generated assets" >&2
    return 1
  fi
  if rg -n -o 'sk-[A-Za-z0-9_-]{20,}|Bearer[[:space:]]+[A-Za-z0-9._-]{24,}' \
    "$ARTIFACT_DIR"; then
    echo "possible secret found in cleanroom artifacts" >&2
    return 1
  fi
}

cd "$ROOT_DIR"
run_stage environment assert_clean_environment

# The bash contracts exercise the Go CLI through the forwarding wrapper, so
# this stage is the cleanroom's evidence that the migrated install path works
# with only the prebuilt binary on PATH.
run_stage go-cli-isolated assert_go_cli_isolated
run_stage bash-compatibility bash tests/install_test.sh

run_stage react-coverage bash -c 'cd frontend && npm run test:coverage'

run_stage react-build bash -c 'cd frontend && npm run build'

collect_artifacts
run_stage policy-scan scan_release_policy

STATUS="passed"
FAILED_STAGE=""
echo "Docker Linux cleanroom passed."
