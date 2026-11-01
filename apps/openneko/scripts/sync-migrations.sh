#!/usr/bin/env bash
# Copy the canonical db/migrations/*.sql into apps/openneko/assets/migrations/
# so the Go binary embeds them via go:embed. Run this whenever a migration is
# added or renamed. CI also runs it (with --check) to fail the build if the
# embedded copies drift from the source.
set -euo pipefail

HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT=$(cd "$HERE/../../.." && pwd)
SRC="$ROOT/db/migrations"
DST="$HERE/../assets/migrations"

mkdir -p "$DST"

if [[ "${1:-}" == "--check" ]]; then
  TMP=$(mktemp -d)
  trap 'rm -rf "$TMP"' EXIT
  cp "$SRC"/*.sql "$TMP/"
  if ! diff -q "$TMP" "$DST" >/dev/null; then
    echo "embedded migrations are stale; run scripts/sync-migrations.sh" >&2
    diff -ruN "$DST" "$TMP" || true
    exit 1
  fi
  echo "embedded migrations up to date"
  exit 0
fi

rm -f "$DST"/*.sql
cp "$SRC"/*.sql "$DST/"
echo "synced $(ls "$DST"/*.sql | wc -l | tr -d ' ') migration files"
