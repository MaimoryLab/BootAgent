#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE="${ONEAGENT_CLEANROOM_IMAGE:-oneagent-cleanroom:0.2.0-dev}"
ARTIFACT_DIR="$ROOT_DIR/build/docker-cleanroom"

command -v docker >/dev/null 2>&1 || {
  echo "Docker is required to run the Linux cleanroom." >&2
  exit 2
}

rm -rf "$ARTIFACT_DIR"
mkdir -p "$ARTIFACT_DIR"
chmod 0777 "$ARTIFACT_DIR"

# The daemon rejects --cpus above its own CPU count, and the daemon may have
# fewer CPUs than the host (Docker Desktop VM, 2-vCPU CI runners). Ask the
# daemon rather than the host, and cap the request at what it reports.
CLEANROOM_CPUS="${ONEAGENT_CLEANROOM_CPUS:-}"
if [ -z "$CLEANROOM_CPUS" ]; then
  DAEMON_CPUS="$(docker info --format '{{.NCPU}}' 2>/dev/null || true)"
  case "$DAEMON_CPUS" in
    ''|*[!0-9]*) DAEMON_CPUS=2 ;;
  esac
  if [ "$DAEMON_CPUS" -lt 4 ]; then
    CLEANROOM_CPUS="$DAEMON_CPUS"
  else
    CLEANROOM_CPUS=4
  fi
fi

docker build --progress plain -f "$ROOT_DIR/Dockerfile.test" -t "$IMAGE" "$ROOT_DIR" \
  2>&1 | tee "$ARTIFACT_DIR/docker-build.log"
docker run --rm --init --network none --shm-size=1g \
  --memory=4g --cpus="$CLEANROOM_CPUS" \
  -v "$ARTIFACT_DIR:/artifacts" \
  "$IMAGE" 2>&1 | tee "$ARTIFACT_DIR/docker-run.log"

echo "Linux cleanroom artifacts: $ARTIFACT_DIR"
