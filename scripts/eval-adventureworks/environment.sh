#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
compose_file="$repo_root/compose.adventureworks.eval.yml"
project_name=openneko-backend-eval

compose() {
  docker compose --project-name "$project_name" --file "$compose_file" "$@"
}

usage() {
  cat <<'EOF'
Usage: scripts/eval-adventureworks/environment.sh COMMAND

Commands:
  up             Build, seed/restore, verify the pre-run fingerprint, and start GraphJin
  verify-pre     Fail unless the current dataset matches the frozen baseline
  verify-post    Fail unless the current dataset matches the frozen baseline
  fingerprint    Print the current logical dataset fingerprint
  restore        Restore the frozen snapshot, restart GraphJin, and verify it
  down           Verify the post-run fingerprint, then stop the eval project (keep volumes)
  reset --yes    Stop the eval project and delete only its three dedicated volumes
  config         Render the Compose config without starting containers
  status         Show the dedicated eval project's containers
EOF
}

stop_graphjin() {
  compose stop --timeout 20 graphjin >/dev/null 2>&1 || true
}

start_database() {
  compose up --detach --wait adventureworks-db
}

start_api_fixture() {
  compose up --detach --no-deps --wait eval-api-fixture
}

run_verify() {
  verify_label=$1
  compose run --rm --no-deps adventureworks-freeze verify "$verify_label"
}

start_graphjin() {
  compose up --detach --no-deps --wait graphjin
}

case "${1:-}" in
  up)
    stop_graphjin
    compose build graphjin eval-api-fixture embedding
    compose up --detach --wait embedding
    start_database

    set +e
    compose run --rm --no-deps adventureworks-freeze bootstrap
    bootstrap_status=$?
    set -e

    case "$bootstrap_status" in
      0) ;;
      42)
        compose run --rm --no-deps adventureworks-freeze prepare-seed
        compose build adventureworks-init
        compose run --rm --no-deps adventureworks-init
        compose run --rm --no-deps adventureworks-freeze capture
        ;;
      *)
        echo "[eval-environment] bootstrap failed with status $bootstrap_status" >&2
        exit "$bootstrap_status"
        ;;
    esac

    start_api_fixture
    start_graphjin
    run_verify pre
    ;;
  verify-pre)
    run_verify pre
    ;;
  verify-post)
    run_verify post
    ;;
  fingerprint)
    compose run --rm --no-deps adventureworks-freeze fingerprint manual
    ;;
  restore)
    stop_graphjin
    start_database
    compose run --rm --no-deps adventureworks-freeze restore recovery
    start_api_fixture
    start_graphjin
    run_verify recovery
    ;;
  down)
    run_verify post
    compose down --remove-orphans
    ;;
  reset)
    if [ "${2:-}" != "--yes" ]; then
      echo "[eval-environment] reset permanently removes only the dedicated eval database, cache, and snapshot volumes." >&2
      echo "[eval-environment] Re-run with: $0 reset --yes" >&2
      exit 2
    fi
    compose down --volumes --remove-orphans
    ;;
  config)
    compose config
    ;;
  status)
    compose ps --all
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac
