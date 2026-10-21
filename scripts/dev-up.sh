#!/bin/sh
# Starts the development backing services in Docker. Web and worker run on
# the host with hot reload: `pnpm dev`.
set -eu

. scripts/dev-env.sh

compose() {
  docker compose -f compose.yml -f compose.adventureworks.yml -f compose.graphjin-agent.yml -f compose.dev.yml "$@"
}

mkdir -p "$OPENNEKO_DEV_STATE/graphjin" "$OPENNEKO_DEV_STATE/records-graphjin" "$OPENNEKO_CONFIG_VOLUME"
printf '%s\n' "$COMPOSE_PROJECT_NAME" > "$OPENNEKO_DEV_STATE/compose-project"
if [ ! -s .openneko/development-backup-key ]; then
  (umask 077 && openssl rand -hex 32 > .openneko/development-backup-key)
fi

# Host processes validate GraphJin configs with the graphjin role of the
# openneko binary, built from graphjin/ in this checkout; scripts/dev-agent.sh
# runs its serve and sandbox roles.
mkdir -p "$OPENNEKO_DEV_STATE/bin"
(cd apps/openneko && go build -o "$OPENNEKO_DEV_STATE/bin/openneko" ./cmd/openneko)
ln -sf openneko "$OPENNEKO_DEV_STATE/bin/graphjin"

compose up -d --wait neko-db records-db
(cd apps/openneko && go run ./cmd/openneko migrate)

# GraphJin reads its auth key only at start, so the per-org secret goes into
# agentic.yml before the server starts.
compose run --rm --no-deps graphjin-config-init
GRAPHJIN_SOURCES_CONFIG="$OPENNEKO_GRAPHJIN_CONFIG" \
  pnpm --filter @neko/worker exec tsx ../../packages/llm/src/graphjin/init-secret.mjs

compose up -d \
  neko-graphjin graphjin records-graphjin records-watch-graphjin \
  embedding librarian \
  adventureworks-simulator adventureworks-scenario-injector

echo "Backing services are up. Start the agent runtime, web and worker with: pnpm dev"
