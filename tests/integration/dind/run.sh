#!/usr/bin/env bash
# Build and run the DinD test container interactively.
# Reproduces the environment used by tests/integration/dind.
# Set NO_BUILD=1 to skip rebuilding the image.
set -euo pipefail

ROOT_DIR="$(git rev-parse --show-toplevel)"
IMAGE="vino-dind-test"
NAME="vino-dind-manual"

# Build the dind test image
if [ -z "${NO_BUILD:-}" ]; then
  echo "Building $IMAGE..."
  docker build -t "$IMAGE" -f "$ROOT_DIR/tests/integration/dind/Dockerfile" "$ROOT_DIR"
fi

cleanup() {
  docker rm -f "$NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Start container
echo "Starting container $NAME..."
docker rm -f "$NAME" >/dev/null 2>&1 || true
docker run -d --privileged --name "$NAME" "$IMAGE" >/dev/null

# Wait for Docker daemon inside the container to be ready
printf 'Waiting for Docker to be ready'
until docker logs "$NAME" 2>&1 | grep -q 'API listen on /var/run/docker.sock'; do
  printf '.'
  sleep 1
done
printf '\n'

# Drop into an interactive shell
echo "Entering container. Exit shell to stop."
docker exec -it "$NAME" sh
