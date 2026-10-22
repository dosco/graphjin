#!/bin/sh
set -eu

refuse_non_development_mode() {
  echo "Host web against a running stack is development-only." >&2
  echo "Production and demo modes must run the web service inside the stack." >&2
  exit 1
}

case "${NODE_ENV:-development}" in
  development) ;;
  *) refuse_non_development_mode ;;
esac

case "${OPENNEKO_STACK_MODE:-}" in
  demo|DEMO) refuse_non_development_mode ;;
esac

case "${NEXT_PUBLIC_DEMO:-false}" in
  1|true|TRUE|yes|YES|on|ON) refuse_non_development_mode ;;
esac

case "${DEMO:-false}" in
  1|true|TRUE|yes|YES|on|ON) refuse_non_development_mode ;;
esac

web_port="${OPENNEKO_PORT:-3200}"
state_root="${OPENNEKO_HOST_WEB_STATE_DIR:-$PWD/.openneko/host-web}"

if [ -n "${OPENNEKO_DEV_STACK:-}" ]; then
  project_name="$OPENNEKO_DEV_STACK"
else
  running_projects="$(
    docker ps \
      --filter label=com.docker.compose.service=worker \
      --format '{{.Label "com.docker.compose.project"}}' |
      sed '/^$/d' |
      sort -u
  )"
  set -f
  set -- $running_projects
  set +f
  case "$#" in
    1) project_name="$1" ;;
    0)
      echo "No running OpenNeko Compose stack was found." >&2
      echo "Start the development backing stack before running the host web server." >&2
      exit 1
      ;;
    *)
      echo "More than one OpenNeko Compose stack is running:" >&2
      printf '  %s\n' "$@" >&2
      echo "Choose one with OPENNEKO_DEV_STACK=<project> pnpm dev:web:stack." >&2
      exit 1
      ;;
  esac
fi

container_prefix="$project_name"

container_ip() {
  docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$1"
}

published_port() {
  docker port "$1" "$2/tcp" 2>/dev/null | sed -n '1s/.*://p'
}

# A stack started by the CLI publishes only the web port. When a service
# port is unpublished, reach the container by IP and its internal port;
# OrbStack routes container IPs to macOS, Docker Desktop does not.
endpoint_host=""
endpoint_port=""
resolve_endpoint() {
  endpoint_port="$(published_port "$1" "$2")"
  if [ -n "$endpoint_port" ]; then
    endpoint_host=127.0.0.1
    return
  fi
  endpoint_host="$(container_ip "$1")"
  endpoint_port="$2"
  if ! nc -z -w 2 "$endpoint_host" "$endpoint_port" >/dev/null 2>&1; then
    echo "$1 publishes no port $2, and its container IP $endpoint_host is not reachable." >&2
    echo "Publish the port, or use a Docker runtime that routes container IPs (OrbStack)." >&2
    exit 1
  fi
}

worker_container="${container_prefix}-worker-1"
embedding_container="${container_prefix}-embedding-1"
metadata_db_container="${container_prefix}-neko-db-1"
metadata_graphjin_container="${container_prefix}-neko-graphjin-1"

for required_container in \
  "$worker_container" \
  "$embedding_container" \
  "$metadata_db_container" \
  "$metadata_graphjin_container"
do
  if ! docker inspect "$required_container" >/dev/null 2>&1 || \
     [ "$(docker inspect -f '{{.State.Running}}' "$required_container")" != "true" ]; then
    echo "OpenNeko service is unavailable: $required_container" >&2
    echo "Start the ${project_name} stack before running the host web server." >&2
    exit 1
  fi
done

web_container="${container_prefix}-web-1"
if [ ! -f "$state_root/config/openneko/secret-key" ]; then
  # Seed the state from the stack's web container. The copy holds the
  # secret key that decrypts the database passwords in config.json.
  if docker inspect "$web_container" >/dev/null 2>&1; then
    echo "Copying stack config from $web_container to $state_root/config"
    (umask 077 && mkdir -p "$state_root/config")
    for config_dir in openneko graphjin; do
      docker cp -q "$web_container:/config/$config_dir" "$state_root/config/"
    done
  fi
fi

if [ ! -f "$state_root/config/openneko/secret-key" ]; then
  echo "Host web state is missing at $state_root." >&2
  echo "Copy the stack config and agent workspace there before first use." >&2
  exit 1
fi

mkdir -p "$state_root/tmp"

export OPENNEKO_AGENT_HOME="$state_root/home"
export OPENNEKO_HOST_WEB_DEV=1
export NODE_ENV=development
export TMPDIR="$state_root/tmp"
export XDG_CONFIG_HOME="$state_root/config"
export XDG_CACHE_HOME="$state_root/home/.cache"
export XDG_DATA_HOME="$state_root/home/.local/share"
export XDG_STATE_HOME="$state_root/home/.local/state"

resolve_endpoint "$metadata_db_container" 5432
export NEKO_PG_HOST="$endpoint_host"
export NEKO_PG_PORT="$endpoint_port"

export OPENNEKO_PG_ENV_OVERRIDE=1

export NEKO_EMBEDDING_URL="http://$(container_ip "$embedding_container"):5003"
export WORKER_ADMIN_URL="http://$(container_ip "$worker_container"):4100"
resolve_endpoint "$metadata_graphjin_container" 8089
export OPENNEKO_GRAPHJIN_URL="http://${endpoint_host}:${endpoint_port}"
export OPENNEKO_GRAPHJIN_CONFIG="$state_root/config/graphjin/agentic.yml"

# Agent runs use the host agent runtime: run `pnpm dev:agent` alongside.
export OPENNEKO_DEV_STATE="${OPENNEKO_DEV_STATE:-$PWD/.openneko/dev}"
# Same socket folder scripts/dev-env.sh derives from the dev project name.
dev_project="$(cat "$OPENNEKO_DEV_STATE/compose-project" 2>/dev/null || basename "$PWD")"
dev_project="$(printf '%s' "$dev_project" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9_-' '-')"
export OPENNEKO_DEV_RUN="${OPENNEKO_DEV_RUN:-/tmp/openneko-$dev_project}"
export OPENNEKO_SERVE_SOCKET="$OPENNEKO_DEV_RUN/serve.sock"
export OPENNEKO_AGENT_RUNS_DIR="${OPENNEKO_AGENT_RUNS_DIR:-$OPENNEKO_DEV_STATE/agent-runs}"
export OPENNEKO_AGENT_ENTRY="${OPENNEKO_AGENT_ENTRY:-$PWD/apps/worker/src/agent-sandbox/entry.ts}"
export OPENNEKO_BROKER_PORT="${OPENNEKO_WEB_BROKER_PORT:-4198}"

echo "OpenNeko web: http://localhost:${web_port} (hot reload)"
echo "Backing services: Docker Compose project ${project_name}"
exec pnpm --filter @neko/web exec next dev --port "$web_port"
