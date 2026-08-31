#!/usr/bin/env bash
# Deploying without a registry: build the images here, carry them to the host over
# ssh, and bring the stack up there.
#
# The published path goes through a registry, and this one exists for when that
# path is not available - a registry that is down, an account that cannot push, a
# host with no credentials of its own. It reaches the same end by a shorter road:
# `docker save` writes the images to a stream, ssh carries it, `docker load` reads
# it back. Nothing is published, so a private repository stays private.
#
#   DEPLOY_HOST=example.com ./docker/deploy-over-ssh.sh v0.3.0
#
# The tag is a name for this build, not a git tag: it is what the host's .env will
# name in IMAGE_TAG, so it must be one that is not already loaded there.
set -euo pipefail

TAG="${1:?usage: deploy-over-ssh.sh <tag>}"
HOST="${DEPLOY_HOST:?set DEPLOY_HOST to the host to deploy to}"
USER_AT="${DEPLOY_USER:-root}@${HOST}"
SSH_KEY="${DEPLOY_SSH_KEY:-}"
REMOTE_DIR="${DEPLOY_DIR:-/opt/alpaca-stand}"
REGISTRY="ghcr.io/swiftward/swiftward-alpaca-options-agents"
# The host runs on this; the machine building may not. The Dockerfile cross-
# compiles rather than emulating, so naming it here costs nothing.
PLATFORM="${DEPLOY_PLATFORM:-linux/amd64}"

SSH=(ssh -o BatchMode=yes -o ConnectTimeout=15)
[ -n "$SSH_KEY" ] && SSH+=(-i "$SSH_KEY")

cd "$(dirname "$0")/.."

# name:dockerfile:target - the same six the registry path builds.
IMAGES=(
  "agent:docker/app.Dockerfile:agent"
  "page:docker/app.Dockerfile:page"
  "envelope:docker/app.Dockerfile:envelope"
  "alpaca-mcp:docker/alpaca-mcp.Dockerfile:"
  "egress:docker/egress.Dockerfile:"
  "migrate:docker/migrate.Dockerfile:"
)

echo "==> building ${#IMAGES[@]} images for ${PLATFORM} as ${TAG}"
built=()
for spec in "${IMAGES[@]}"; do
  IFS=: read -r name dockerfile target <<<"$spec"
  ref="${REGISTRY}/${name}:${TAG}"
  args=(buildx build --platform "$PLATFORM" --file "$dockerfile" --tag "$ref" --load)
  [ -n "$target" ] && args+=(--target "$target")
  # The egress allowlist is a build argument, and an image built without it is one
  # that cannot reach the hosts this deployment needs. Empty is allowed; silently
  # empty is what caused a ten-hour outage once, so it is printed either way.
  if [ "$name" = "egress" ]; then
    echo "    egress allowlist: ${EXTRA_ALLOWED_HOSTS:-<empty>}"
    args+=(--build-arg "EXTRA_ALLOWED_HOSTS=${EXTRA_ALLOWED_HOSTS:-}")
  fi
  echo "--> $name"
  docker "${args[@]}" .
  built+=("$ref")
done

# The stack is described by two files that are not inside any image, so a
# deployment that only carries images leaves the host running the old shape of the
# new thing. The registry path copies them for the same reason.
echo "==> copying the files the stack is described by"
SCP=(scp -o BatchMode=yes -o ConnectTimeout=15)
[ -n "$SSH_KEY" ] && SCP+=(-i "$SSH_KEY")
"${SSH[@]}" "$USER_AT" "mkdir -p '${REMOTE_DIR}/docker/traefik'"
"${SCP[@]}" compose.prod.yaml "${USER_AT}:${REMOTE_DIR}/compose.prod.yaml"
"${SCP[@]}" docker/traefik/dynamic.yml "${USER_AT}:${REMOTE_DIR}/docker/traefik/dynamic.yml"

echo "==> carrying them to ${HOST}"
# One stream for all six: the shared layers travel once instead of six times, and
# there is no half-loaded state to reason about if the link drops.
docker save "${built[@]}" | gzip -1 | "${SSH[@]}" "$USER_AT" 'gunzip | docker load'

echo "==> pointing ${REMOTE_DIR}/.env at ${TAG} and bringing the stack up"
"${SSH[@]}" "$USER_AT" "TAG='${TAG}' REMOTE_DIR='${REMOTE_DIR}' bash -s" <<'REMOTE'
set -euo pipefail
cd "$REMOTE_DIR"
cp -a .env ".env.bak-$(date +%Y%m%d-%H%M%S)"
if grep -q '^IMAGE_TAG=' .env; then
  sed -i "s|^IMAGE_TAG=.*|IMAGE_TAG=${TAG}|" .env
else
  echo "IMAGE_TAG=${TAG}" >> .env
fi
# No pull: these images exist only on this host, and asking a registry for them
# would fail on the one thing this script exists to avoid.
docker compose --env-file .env -f compose.prod.yaml --profile session up -d

# Coming up is not the same as staying up, and the difference is a container that
# restarts every ten seconds. Give it long enough to fail before reporting.
sleep 25
docker compose --env-file .env -f compose.prod.yaml --profile session ps \
  --format 'table {{.Service}}\t{{.State}}\t{{.Status}}'
broken=$(docker compose --env-file .env -f compose.prod.yaml --profile session ps \
  --format '{{.Service}} {{.State}}' | awk '$2 != "running" && $2 != "exited" {print $1}')
if [ -n "$broken" ]; then
  echo "NOT HEALTHY: $broken"
  for svc in $broken; do
    echo "--- $svc ---"
    docker compose --env-file .env -f compose.prod.yaml logs --tail 15 "$svc"
  done
  exit 1
fi
echo "all services are up"
REMOTE

echo "==> done: ${TAG} is live on ${HOST}"
