#!/bin/sh
set -eu

# The shared loader converts extracted CSVs in place. Preserve only the pinned
# source archive and re-extract on every seed attempt so an interrupted load
# cannot convert an already-converted cache a second time.

eval_dir=${OPENNEKO_EVAL_DIR:-/eval}
cache_dir=${ADVENTUREWORKS_CACHE_DIR:-/cache}
work_dir="$cache_dir/adventureworks"
zip_file="$work_dir/ms.zip"
preserved_zip="$cache_dir/.openneko-eval-adventureworks-ms.zip"

[ "$cache_dir" = /cache ] || {
  echo "[eval-seed] refusing to clean unexpected cache directory: $cache_dir" >&2
  exit 1
}
[ -f "$eval_dir/source.sha256" ] && [ -f /app/load-adventureworks.js ] || {
  echo "[eval-seed] required eval and loader mounts are absent; refusing cache cleanup" >&2
  exit 1
}

if [ -f "$preserved_zip" ]; then
  if [ -f "$zip_file" ]; then
    echo "[eval-seed] ambiguous cache state; run environment.sh reset --yes" >&2
    exit 1
  fi
  mkdir -p "$work_dir"
  mv "$preserved_zip" "$zip_file"
fi

if [ -f "$zip_file" ]; then
  expected_zip_sha=$(awk '$2 == "/cache/adventureworks/ms.zip" { print $1 }' \
    "$eval_dir/source.sha256")
  observed_zip_sha=$(sha256sum "$zip_file" | awk '{print $1}')
  [ -n "$expected_zip_sha" ] && [ "$observed_zip_sha" = "$expected_zip_sha" ] || {
    echo "[eval-seed] cached AdventureWorks archive does not match source.sha256; reset is required" >&2
    exit 1
  }
  mv "$zip_file" "$preserved_zip"
fi

# This exact directory belongs to the dedicated eval cache volume. Never use a
# caller-provided or unresolved path as the recursive target.
rm -rf /cache/adventureworks
mkdir -p "$work_dir"

if [ -f "$preserved_zip" ]; then
  mv "$preserved_zip" "$zip_file"
fi

exec node /app/load-adventureworks.js
