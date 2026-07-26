#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACT_DIR="${ONEAGENT_ARTIFACT_DIR:-/artifacts}"
expected_platform="linux"

if [[ "${ONEAGENT_CLEANROOM_SANITIZED:-0}" != "1" ]]; then
  exec env -i \
    PATH="/opt/oneagent-venv/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" \
    LANG="C.UTF-8" \
    LC_ALL="C.UTF-8" \
    PLAYWRIGHT_BROWSERS_PATH="/ms-playwright" \
    ONEAGENT_CLEANROOM_SANITIZED="1" \
    ONEAGENT_DISABLE_BROWSER="1" \
    ONEAGENT_ARTIFACT_DIR="$ARTIFACT_DIR" \
    bash "$0"
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
  if [[ -f "$ROOT_DIR/build/coverage/coverage.json" ]]; then
    cp -f "$ROOT_DIR/build/coverage/coverage.json" "$ARTIFACT_DIR/coverage-python.json" || true
  fi
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

  python3.12 - <<'PY'
import os

markers = ("KEY", "TOKEN", "SECRET", "PASSWORD")
leaked = sorted(name for name in os.environ if any(marker in name.upper() for marker in markers))
if leaked:
    raise SystemExit(f"secret-shaped environment variables remain: {', '.join(leaked)}")
PY

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
  if rg -n 'shell\s*=\s*True' "$ROOT_DIR/oneagent" "$ROOT_DIR/scripts"; then
    echo "Python subprocess shell invocation is forbidden" >&2
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

mkdir -p build/coverage
run_stage python-contracts bash -c '
  coverage erase
  coverage run --branch -m unittest \
    tests.test_core tests.test_cli tests.test_server \
    tests.test_release_policy tests.test_edge_cases tests.test_rc_scripts
  coverage report --fail-under=85
  coverage json
  python3.12 -c '\''import json; summary = json.load(open("build/coverage/coverage.json", encoding="utf-8"))["files"]["oneagent/installer.py"]["summary"]; assert summary["percent_branches_covered"] == 100 and summary["num_partial_branches"] == 0, summary'\''
'

run_stage bash-compatibility bash tests/install_test.sh
run_stage gui-smoke python3.12 tests/gui_smoke_test.py

run_stage react-coverage bash -c 'cd frontend && npm run test:coverage'

run_stage react-build bash -c 'cd frontend && npm run build'
run_stage browser-e2e bash -c 'cd frontend && npm run e2e'

collect_artifacts
run_stage policy-scan scan_release_policy

STATUS="passed"
FAILED_STAGE=""
echo "Docker Linux cleanroom passed."
