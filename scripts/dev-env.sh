# Environment for host web and worker processes against `pnpm dev:up`.
# Source from the repository root: `. scripts/dev-env.sh`.
#
# OPENNEKO_DEV_CONFIG_HOME: config home for OpenNeko state
#   (default: your XDG config home). Set it per worktree to keep stacks apart.
# OPENNEKO_DEV_STATE: GraphJin config directories shared with Docker
#   (default: .openneko/dev in this checkout).

if [ -n "${OPENNEKO_DEV_CONFIG_HOME:-}" ]; then
  export XDG_CONFIG_HOME="$OPENNEKO_DEV_CONFIG_HOME"
fi
export OPENNEKO_DEV_STATE="${OPENNEKO_DEV_STATE:-$PWD/.openneko/dev}"

# Every shell must reach the stack dev-up.sh started. Without one project
# name, a later compose call builds a second stack and the network clashes.
if [ -z "${COMPOSE_PROJECT_NAME:-}" ] && [ -s "$OPENNEKO_DEV_STATE/compose-project" ]; then
  COMPOSE_PROJECT_NAME="$(cat "$OPENNEKO_DEV_STATE/compose-project")"
fi
# Compose accepts only lowercase letters, digits, "-" and "_".
COMPOSE_PROJECT_NAME="$(printf '%s' "${COMPOSE_PROJECT_NAME:-$(basename "$PWD")}" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9_-' '-')"
export COMPOSE_PROJECT_NAME
export OPENNEKO_CONFIG_VOLUME="${OPENNEKO_CONFIG_VOLUME:-${XDG_CONFIG_HOME:-$HOME/.config}/openneko}"
# `pnpm dev:up` installs the pinned GraphJin CLI here.
export PATH="$OPENNEKO_DEV_STATE/bin:$PATH"

# Docker hostnames in config.json do not resolve on the host; the password
# still comes from config.json.
export OPENNEKO_PG_ENV_OVERRIDE=1
export NEKO_PG_HOST=127.0.0.1
export NEKO_PG_PORT="${OPENNEKO_DB_PORT:-5432}"

export NEKO_EMBEDDING_URL="http://127.0.0.1:${OPENNEKO_EMBEDDING_PORT:-5003}"
export OPENNEKO_GRAPHJIN_URL="http://127.0.0.1:${OPENNEKO_GRAPHJIN_PORT:-8089}"
export OPENNEKO_GRAPHJIN_CONFIG="$OPENNEKO_DEV_STATE/graphjin/agentic.yml"
export OPENNEKO_LIBRARY_GRAPHJIN_URL="http://127.0.0.1:${GRAPHJIN_PORT:-8080}/api/v1/graphql"
# The host web and worker write beside the customer GraphJin config, so that
# GraphJin container runs as the host user to read it (compose.dev.yml).
export OPENNEKO_DEV_UID="${OPENNEKO_DEV_UID:-$(id -u)}"
export OPENNEKO_DEV_GID="${OPENNEKO_DEV_GID:-$(id -g)}"

# Agent runtime. `pnpm dev` runs `openneko serve` on
# the host (scripts/dev-agent.sh); web and worker start each run's runner from
# source with tsx, and the runner reaches the broker on loopback. Sockets
# live under /tmp because a Unix socket path must stay short.
export OPENNEKO_DEV_RUN="${OPENNEKO_DEV_RUN:-/tmp/openneko-$COMPOSE_PROJECT_NAME}"
export OPENNEKO_SERVE_SOCKET="$OPENNEKO_DEV_RUN/serve.sock"
export OPENNEKO_AGENT_RUNS_DIR="${OPENNEKO_AGENT_RUNS_DIR:-$OPENNEKO_DEV_STATE/agent-runs}"
export OPENNEKO_AGENT_ENTRY="${OPENNEKO_AGENT_ENTRY:-$PWD/apps/worker/src/agent-sandbox/entry.ts}"

export OPENNEKO_PUBLIC_URL="${OPENNEKO_PUBLIC_URL:-http://localhost:${OPENNEKO_PORT:-3000}}"
export WORKER_ADMIN_URL="${WORKER_ADMIN_URL:-http://127.0.0.1:4100}"
