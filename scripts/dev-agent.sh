#!/bin/sh
# Runs the agent runtime on the host for `pnpm dev`: `openneko serve` runs
# agent turns. `pnpm dev:up` builds the openneko binary this uses.
set -eu

. scripts/dev-env.sh

mkdir -p "$OPENNEKO_DEV_RUN" "$OPENNEKO_AGENT_RUNS_DIR"
exec openneko serve --socket "$OPENNEKO_SERVE_SOCKET"
