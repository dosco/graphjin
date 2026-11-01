#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
compose_file="$repo_root/compose.adventureworks.eval.yml"
rendered_config=$(mktemp)

cleanup() {
  rm -f "$rendered_config"
}
trap cleanup EXIT HUP INT TERM

hash_file() {
  target=$1
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$target" | awk '{print $1}'
  else
    shasum -a 256 "$target" | awk '{print $1}'
  fi
}

assert_source_hash() {
  manifest_path=$1
  local_path=$2
  expected=$(awk -v path="$manifest_path" '$2 == path { print $1 }' \
    "$repo_root/evals/environment/adventureworks/source.sha256")
  [ -n "$expected" ] || {
    echo "source manifest is missing $manifest_path" >&2
    exit 1
  }
  observed=$(hash_file "$repo_root/$local_path")
  [ "$observed" = "$expected" ] || {
    echo "source manifest hash is stale for $local_path" >&2
    exit 1
  }
}

sh -n "$script_dir/environment.sh"
sh -n "$repo_root/evals/environment/adventureworks/fingerprint.sh"
sh -n "$repo_root/evals/environment/adventureworks/freeze.sh"
sh -n "$repo_root/evals/environment/adventureworks/seed.sh"
assert_source_hash /workspace/db/seeds/dev/adventureworks-install.sql \
  db/seeds/dev/adventureworks-install.sql
assert_source_hash /workspace/apps/worker/scripts/load-adventureworks.ts \
  apps/worker/scripts/load-adventureworks.ts
assert_source_hash /eval/seed.sh \
  evals/environment/adventureworks/seed.sh

if command -v graphjin >/dev/null 2>&1; then
  env GO_ENV=eval graphjin config validate \
    --file "$repo_root/evals/environment/adventureworks/graphjin/eval.yml"
  mutation_policy=$(env GO_ENV=eval graphjin config get mcp.allow_mutations \
    --file "$repo_root/evals/environment/adventureworks/graphjin/eval.yml")
  [ "$mutation_policy" = true ] || {
    echo "GraphJin eval must let GraphJin apply source-aware mutation policy" >&2
    exit 1
  }
  agent_policy=$(env GO_ENV=eval graphjin config get agent.enabled \
    --file "$repo_root/evals/environment/adventureworks/graphjin/eval.yml")
  [ "$agent_policy" = false ] || {
    echo "GraphJin's built-in agent must be disabled in the direct-tool track" >&2
    exit 1
  }
  awk '
    /^  - name: adventureworks$/ { in_source = 1; next }
    /^  - name:/ { in_source = 0 }
    in_source && /^    read_only: true$/ { found = 1 }
    END { exit found ? 0 : 1 }
  ' "$repo_root/evals/environment/adventureworks/graphjin/eval.yml" || {
    echo "GraphJin eval database must be read_only" >&2
    exit 1
  }
fi

docker compose --project-name openneko-backend-eval \
  --file "$compose_file" config >"$rendered_config"

services=$(docker compose --project-name openneko-backend-eval \
  --file "$compose_file" config --services)

grep -Fx 'name: openneko-backend-eval' "$rendered_config" >/dev/null || {
  echo "Compose project name is not fixed to openneko-backend-eval" >&2
  exit 1
}

for expected in adventureworks-db adventureworks-init adventureworks-freeze eval-api-fixture graphjin; do
  printf '%s\n' "$services" | grep -Fx "$expected" >/dev/null || {
    echo "missing eval service: $expected" >&2
    exit 1
  }
done

for forbidden in adventureworks-simulator adventureworks-scenario-injector; do
  if printf '%s\n' "$services" | grep -Fx "$forbidden" >/dev/null; then
    echo "forbidden mutator service present: $forbidden" >&2
    exit 1
  fi
done

for forbidden_seed_mutator in advance-dates.sql aw-sim-tick.sql scenario-injector.sh; do
  if grep -F "$forbidden_seed_mutator" \
      "$repo_root/evals/environment/adventureworks/seed.sh" \
      "$repo_root/apps/worker/scripts/load-adventureworks.ts" \
      "$repo_root/db/seeds/dev/adventureworks-install.sql" >/dev/null; then
    echo "forbidden backfill reference present in eval seed path: $forbidden_seed_mutator" >&2
    exit 1
  fi
done

for forbidden_resource in adventureworks-db-data graphjin-config; do
  if grep -F "$forbidden_resource" "$rendered_config" >/dev/null; then
    echo "active demo resource leaked into eval Compose config: $forbidden_resource" >&2
    exit 1
  fi
done

for isolated_name in \
  openneko-backend-eval-adventureworks-db-v1 \
  openneko-backend-eval-adventureworks-cache-v1 \
  openneko-backend-eval-adventureworks-snapshot-v1 \
  openneko-backend-eval-adventureworks-v1; do
  grep -F "$isolated_name" "$rendered_config" >/dev/null || {
    echo "missing isolated Compose resource: $isolated_name" >&2
    exit 1
  }
done

echo "AdventureWorks eval environment static contract verified."
