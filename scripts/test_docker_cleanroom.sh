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

docker build --progress plain -f "$ROOT_DIR/Dockerfile.test" -t "$IMAGE" "$ROOT_DIR" \
  2>&1 | tee "$ARTIFACT_DIR/docker-build.log"
docker run --rm --init --network none --shm-size=1g \
  --memory=4g --cpus=4 \
  -v "$ARTIFACT_DIR:/artifacts" \
  "$IMAGE" 2>&1 | tee "$ARTIFACT_DIR/docker-run.log"

echo "Linux cleanroom artifacts: $ARTIFACT_DIR"
