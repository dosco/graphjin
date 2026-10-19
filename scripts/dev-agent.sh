#!/bin/sh
# Runs the agent runtime on the host for `pnpm dev`: `openneko serve` runs
# agent turns and `openneko sandbox` runs the agent's terminal commands. Not
# being root, the sandbox runs commands as you instead of a per-run user.
# `pnpm dev:up` builds the openneko binary this uses.
set -eu

. scripts/dev-env.sh

mkdir -p "$OPENNEKO_DEV_RUN" "$OPENNEKO_AGENT_RUNS_DIR" "$OPENNEKO_DEV_STATE/sandbox-homes"
OPENNEKO_SANDBOX_WORKSPACES="$OPENNEKO_AGENT_RUNS_DIR" \
OPENNEKO_SANDBOX_HOMES="$OPENNEKO_DEV_STATE/sandbox-homes" \
  openneko sandbox --socket "$OPENNEKO_SANDBOX_SOCKET" &
sandbox=$!
trap 'kill "$sandbox" 2>/dev/null || true' EXIT INT TERM
openneko serve --socket "$OPENNEKO_SERVE_SOCKET"
