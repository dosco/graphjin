#!/bin/sh
# Records GraphJin CLI for host development. The worker validates and syncs
# records configs that name the records database by its Docker hostname, so
# the CLI runs in the pinned records GraphJin image on the stack network.
# scripts/dev-env.sh points OPENNEKO_RECORDS_GRAPHJIN_BINARY here.
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
work=""
previous=""
for arg do
  shift
  if [ "$previous" = "--path" ]; then
    work="$arg"
    arg=/work
  fi
  previous="$arg"
  set -- "$@" "$arg"
done

# Run as the host user: the worker writes the configs it validates owner-only.
set -- --user "${OPENNEKO_DEV_UID:-$(id -u)}:${OPENNEKO_DEV_GID:-$(id -g)}" \
  --entrypoint graphjin records-graphjin "$@"
if [ -n "$work" ]; then
  set -- -v "$work:/work" "$@"
fi

cd "$root"
exec docker compose -f compose.yml -f compose.adventureworks.yml -f compose.graphjin-agent.yml -f compose.dev.yml \
  run --rm --no-deps -T "$@"
