#!/bin/sh
set -eu

# Self-contained AdventureWorks loader for `openneko start --mode demo`.
# Same pipeline as db/load-adventureworks.sh, but assumes everything is
# baked into the worker image (no /workspace bind mount, no apt-get).
# That mirror is intentional: the source-build path keeps using
# load-adventureworks.sh; only the binary-distributed image uses this.
#
# Required env: PGHOST, PGPORT, PGUSER, PGPASSWORD, ADVENTUREWORKS_DB.
# Optional: ADVENTUREWORKS_CACHE_DIR (defaults to /cache).

: "${PGHOST:?PGHOST required}"
: "${PGUSER:?PGUSER required}"
: "${PGPASSWORD:?PGPASSWORD required}"
: "${ADVENTUREWORKS_DB:?ADVENTUREWORKS_DB required}"

export ADVENTUREWORKS_CACHE_DIR="${ADVENTUREWORKS_CACHE_DIR:-/cache}"
export ADVENTUREWORKS_INSTALL_SQL="${ADVENTUREWORKS_INSTALL_SQL:-/app/db/seeds/dev/adventureworks-install.sql}"

# The worker image is a `pnpm deploy --prod` closure rooted at /app, so the
# loader is /app/scripts/load-adventureworks.ts (not /app/apps/worker/...).
cd /app
echo "[adventureworks] running TS loader from baked worker image"
exec node --import tsx/esm scripts/load-adventureworks.ts
