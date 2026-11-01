#!/bin/sh
set -eu

# Loads the AdventureWorks 2014 OLTP sample into adventureworks-db.
# Pipeline mirrors lorint/AdventureWorks-for-Postgres exactly, with
# apps/worker/scripts/load-adventureworks.ts standing in for the
# original update_csvs.rb (so we don't carry a ruby dep).
#
# This wrapper exists only to bring up the OS-level deps (curl, unzip,
# psql, node) outside the read-only workspace mount. The actual load
# logic lives in TypeScript.

LOADER_DIR=/tmp/loader

echo "[adventureworks] installing curl, unzip, postgresql-client"
apt-get update -qq
apt-get install -y -qq --no-install-recommends \
  curl ca-certificates unzip postgresql-client \
  >/dev/null

echo "[adventureworks] setting up loader workspace at ${LOADER_DIR}"
mkdir -p "${LOADER_DIR}"
cd "${LOADER_DIR}"

if [ ! -d node_modules ]; then
  echo "[adventureworks] installing pg + tsx"
  cat >package.json <<'EOF'
{
  "name": "neko-adventureworks-loader",
  "version": "0.0.0",
  "private": true,
  "type": "module"
}
EOF
  npm install --silent --no-audit --no-fund \
    pg@^8.20.0 \
    tsx@^4.19.0 \
    @types/pg@^8.20.0
fi

# Copy the loader into our workspace so node's package resolution finds
# pg next to it (NODE_PATH is deprecated for ESM).
cp /workspace/apps/worker/scripts/load-adventureworks.ts "${LOADER_DIR}/load.ts"

echo "[adventureworks] running loader"
exec ./node_modules/.bin/tsx "${LOADER_DIR}/load.ts"
