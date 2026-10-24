#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/.." && pwd)

metadata_project=openneko-backend-eval-metadata
metadata_port=${OPENNEKO_EVAL_METADATA_PORT:-15432}
metadata_config_root=
openshell_sandbox_baseline=
openshell_provider_baseline=
metadata_started=0
data_attempted=0
data_ready=0
cleanup_failed=0

config_input=
resume_run=
restart=0
promotion=
build_agent=1

usage() {
  cat <<'EOF'
Usage: scripts/eval-openneko-backend.sh [SUITE] [OPTIONS]

Suites (default: --smoke):
  --smoke             Provider-free 13-call passing walking skeleton
  --smoke-v2          Provider-free 53-call full-coverage walking skeleton
  --smoke-v3          Provider-free 59-call stateful walking skeleton
  --smoke-v4          Provider-free 195-episode v4 qualification control
  --contrast          Provider-free 52-call good/bad discrimination run
  --ax-v4             One-repetition, 65-episode Ax v4 Gemini cohort
  --config PATH       Run another openneko.work-backend config

Options:
  --resume RUN_ID     Resume the exact durable run
  --restart           Start a new run even if a compatible run is available
  --promote           Write and verify a sanitized result (default)
  --no-promote        Keep only private durable run state; skip result verification
  --no-build-agent    Reuse OPENNEKO_EVAL_AGENT_IMAGE after verifying it exists
  -h, --help          Show this help

Environment overrides:
  OPENNEKO_EVAL_METADATA_PORT                 default 15432
  OPENNEKO_EVAL_AW_DB_PORT                    default 15433
  OPENNEKO_EVAL_GRAPHJIN_PORT                  default 18080
  OPENNEKO_EVAL_AGENT_IMAGE                    default openneko-agent:eval
  OPENNEKO_EVAL_ADVENTUREWORKS_DATABASE_URL   full oracle URL override

Ax configs declare their credential as env:NAME. That variable must be
non-empty before this script starts. Credentials are never written to eval
artifacts or forwarded directly into the agent container.
EOF
}

select_config() {
  [ -z "$config_input" ] || {
    echo "select only one suite or --config" >&2
    exit 2
  }
  config_input=$1
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --smoke)
      select_config evals/configs/openneko-backend-scripted-good.yaml
      ;;
    --smoke-v2)
      select_config evals/configs/openneko-backend-scripted-good-v2.yaml
      ;;
    --smoke-v3)
      select_config evals/configs/openneko-backend-scripted-good-v3.yaml
      ;;
    --smoke-v4)
      select_config evals/configs/openneko-backend-scripted-good-v4.yaml
      ;;
    --contrast)
      select_config evals/configs/openneko-backend-scripted.yaml
      ;;
    --ax-v4)
      select_config evals/configs/openneko-backend-ax-gemini-3.8-flash-v4.yaml
      ;;
    --config)
      [ "$#" -ge 2 ] || { echo "--config requires a path" >&2; exit 2; }
      select_config "$2"
      shift
      ;;
    --resume)
      [ "$#" -ge 2 ] || { echo "--resume requires a run ID" >&2; exit 2; }
      resume_run=$2
      shift
      ;;
    --restart)
      restart=1
      ;;
    --promote)
      [ -z "$promotion" ] || { echo "choose only one promotion mode" >&2; exit 2; }
      promotion=--promote
      ;;
    --no-promote)
      [ -z "$promotion" ] || { echo "choose only one promotion mode" >&2; exit 2; }
      promotion=--no-promote
      ;;
    --no-build-agent)
      build_agent=0
      ;;
    --)
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

[ -n "$config_input" ] || config_input=evals/configs/openneko-backend-scripted-good.yaml
[ -n "$promotion" ] || promotion=--promote
[ -z "$resume_run" ] || [ "$restart" -eq 0 ] || {
  echo "--resume and --restart are mutually exclusive" >&2
  exit 2
}

case "$config_input" in
  /*) config_candidate=$config_input ;;
  *) config_candidate=$repo_root/$config_input ;;
esac
config_dir=$(CDPATH='' cd -- "$(dirname -- "$config_candidate")" 2>/dev/null && pwd) || {
  echo "config directory does not exist: $config_input" >&2
  exit 2
}
config_path=$config_dir/$(basename -- "$config_candidate")
[ -f "$config_path" ] || {
  echo "config does not exist: $config_input" >&2
  exit 2
}
case "$config_path" in
  "$repo_root"/*) ;;
  *)
    echo "eval config must be inside $repo_root" >&2
    exit 2
    ;;
esac
grep -Eq '^[[:space:]]*adapter:[[:space:]]*openneko\.work-backend([[:space:]#]|$)' "$config_path" || {
  echo "config does not use the openneko.work-backend adapter: $config_path" >&2
  exit 2
}

case "$metadata_port" in
  ''|*[!0-9]*) echo "OPENNEKO_EVAL_METADATA_PORT must be a TCP port" >&2; exit 2 ;;
esac
[ "$metadata_port" -ge 1 ] && [ "$metadata_port" -le 65535 ] || {
  echo "OPENNEKO_EVAL_METADATA_PORT must be between 1 and 65535" >&2
  exit 2
}

requires_agent=0
if grep -Eq '^[[:space:]]*backend:[[:space:]]*ax([[:space:]#]|$)' "$config_path"; then
  requires_agent=1
  credential_env=$(sed -n \
    's/^[[:space:]]*credential_ref:[[:space:]]*env:\([A-Za-z_][A-Za-z0-9_]*\)[[:space:]]*$/\1/p' \
    "$config_path" | head -n 1)
  [ -n "$credential_env" ] || {
    echo "agent config must declare credential_ref: env:NAME" >&2
    exit 2
  }
  if ! printenv "$credential_env" | grep -q .; then
    echo "required agent credential is not set: $credential_env" >&2
    exit 2
  fi
  command -v openshell >/dev/null 2>&1 || {
    echo "openshell is required for an agent eval" >&2
    exit 2
  }
  openshell status >/dev/null 2>&1 || {
    echo "the configured OpenShell gateway is not reachable" >&2
    exit 2
  }
fi

for command_name in docker pnpm go openssl; do
  command -v "$command_name" >/dev/null 2>&1 || {
    echo "required command is unavailable: $command_name" >&2
    exit 2
  }
done
docker info >/dev/null 2>&1 || {
  echo "Docker is not reachable" >&2
  exit 2
}

run_eval_cli() {
  (
    cd "$repo_root/apps/worker"
    pnpm eval "$@"
  )
}

echo "[backend-eval] validating $(basename -- "$config_path")"
run_eval_cli validate --config "$config_path"
run_eval_cli plan --config "$config_path"
"$repo_root/scripts/eval-adventureworks/check-static.sh"

agent_image=${OPENNEKO_EVAL_AGENT_IMAGE:-openneko-agent:eval}
if [ "$requires_agent" -eq 1 ]; then
  if [ "$build_agent" -eq 1 ]; then
    echo "[backend-eval] building candidate agent image $agent_image"
    docker build --target agent --tag "$agent_image" "$repo_root"
  else
    docker image inspect "$agent_image" >/dev/null 2>&1 || {
      echo "agent image does not exist: $agent_image" >&2
      exit 2
    }
  fi
fi

metadata_config_root=$(mktemp -d "${TMPDIR:-/tmp}/openneko-backend-eval-migrate.XXXXXX")
if [ "$requires_agent" -eq 1 ]; then
  openshell_sandbox_baseline=$metadata_config_root/openshell-sandboxes.baseline
  openshell_provider_baseline=$metadata_config_root/openshell-providers.baseline
  openshell sandbox list --names | sort >"$openshell_sandbox_baseline"
  openshell provider list --names | sort >"$openshell_provider_baseline"
fi
metadata_key=$repo_root/.openneko/evals/metadata-backup-key
metadata_backups=$repo_root/.openneko/evals/metadata-backups
install -d -m 0700 "$repo_root/.openneko" "$repo_root/.openneko/evals" "$metadata_backups"
if [ ! -s "$metadata_key" ]; then
  umask 077
  openssl rand -hex 32 >"$metadata_key"
fi
chmod 0600 "$metadata_key"

metadata_compose() {
  OPENNEKO_DB_PORT=$metadata_port \
  OPENNEKO_BACKUP_KEY_FILE=$metadata_key \
  OPENNEKO_BACKUP_REPOSITORY=$metadata_backups \
    docker compose --project-name "$metadata_project" --file "$repo_root/compose.yml" "$@"
}

wait_for_metadata_database() {
  # The upstream Postgres entrypoint briefly accepts connections on a
  # temporary server while it initializes a brand-new data directory. Docker
  # can therefore report a healthy container before that server shuts down and
  # the final postmaster starts. Wait for the init-complete boundary, then
  # prove the final server accepts a query before running migrations.
  attempts=0
  until metadata_compose logs --no-color neko-db 2>&1 \
    | grep -Fq 'PostgreSQL init process complete; ready for start up.'; do
    attempts=$((attempts + 1))
    if [ "$attempts" -ge 60 ]; then
      echo "metadata PostgreSQL did not finish initialization" >&2
      metadata_compose logs --no-color --tail 80 neko-db >&2 || true
      return 1
    fi
    sleep 1
  done

  attempts=0
  until metadata_compose exec -T neko-db \
    psql --no-psqlrc --username neko --dbname neko \
      --command 'select 1' >/dev/null 2>&1; do
    attempts=$((attempts + 1))
    if [ "$attempts" -ge 60 ]; then
      echo "metadata PostgreSQL did not accept a query after initialization" >&2
      metadata_compose logs --no-color --tail 80 neko-db >&2 || true
      return 1
    fi
    sleep 1
  done
}

cleanup() {
  status=$?
  trap - EXIT HUP INT TERM

  if [ "$data_ready" -eq 1 ]; then
    if ! "$repo_root/scripts/eval-adventureworks/environment.sh" down; then
      echo "[backend-eval] AdventureWorks postflight failed; leaving that stack running for inspection" >&2
      cleanup_failed=1
    fi
  elif [ "$data_attempted" -eq 1 ]; then
    docker compose --project-name openneko-backend-eval \
      --file "$repo_root/compose.adventureworks.eval.yml" \
      down --remove-orphans >/dev/null 2>&1 || cleanup_failed=1
  fi

  # A hard interrupt can terminate the TypeScript driver before its per-org
  # finally block removes its sandbox and gateway provider. Resolve the exact
  # work-run sandbox names from this invocation's isolated metadata DB, and
  # intersect them with sandboxes created after the pre-run snapshot. Delete
  # those sandboxes before providers because OpenShell will not delete a
  # provider while a live sandbox still references it.
  if [ "$requires_agent" -eq 1 ] && [ "$metadata_started" -eq 1 ] \
    && [ -n "$openshell_sandbox_baseline" ] \
    && [ -f "$openshell_sandbox_baseline" ]; then
    openshell_sandbox_current=$metadata_config_root/openshell-sandboxes.current
    eval_sandbox_names=$metadata_config_root/openshell-sandboxes.eval
    if metadata_compose exec -T neko-db \
      psql --no-psqlrc --tuples-only --no-align --username neko --dbname neko \
        --command "select 'work-' || id from work_run" 2>/dev/null \
        | tr -d '\r' | sort >"$eval_sandbox_names" \
      && openshell sandbox list --names 2>/dev/null \
        | sort >"$openshell_sandbox_current"; then
      comm -13 "$openshell_sandbox_baseline" "$openshell_sandbox_current" \
        | comm -12 - "$eval_sandbox_names" \
        | while IFS= read -r sandbox_name; do
            [ -n "$sandbox_name" ] || continue
            if ! openshell sandbox delete "$sandbox_name" >/dev/null 2>&1; then
              echo "[backend-eval] failed to delete OpenShell sandbox $sandbox_name" >&2
              exit 1
            fi
          done || cleanup_failed=1
    else
      echo "[backend-eval] could not resolve OpenShell sandboxes for cleanup" >&2
      cleanup_failed=1
    fi
  fi

  # Delete only OpenNeko providers created after this invocation's snapshot;
  # pre-existing developer or production providers remain untouched.
  if [ -n "$openshell_provider_baseline" ] && [ -f "$openshell_provider_baseline" ]; then
    openshell_provider_current=$metadata_config_root/openshell-providers.current
    if openshell provider list --names 2>/dev/null | sort >"$openshell_provider_current"; then
      comm -13 "$openshell_provider_baseline" "$openshell_provider_current" \
        | awk '/^openneko-agent-/' \
        | while IFS= read -r provider_name; do
            [ -n "$provider_name" ] || continue
            if ! openshell provider delete "$provider_name" >/dev/null 2>&1; then
              echo "[backend-eval] failed to delete OpenShell provider $provider_name" >&2
              exit 1
            fi
          done || cleanup_failed=1
    else
      cleanup_failed=1
    fi
  fi

  if [ "$metadata_started" -eq 1 ]; then
    metadata_compose down --volumes --remove-orphans >/dev/null 2>&1 || cleanup_failed=1
  fi

  if [ -n "$metadata_config_root" ]; then
    case "$metadata_config_root" in
      "${TMPDIR:-/tmp}"/openneko-backend-eval-migrate.*)
        rm -rf -- "$metadata_config_root"
        ;;
      *)
        echo "[backend-eval] refusing to remove unexpected temporary path: $metadata_config_root" >&2
        cleanup_failed=1
        ;;
    esac
  fi

  if [ "$status" -eq 0 ] && [ "$cleanup_failed" -ne 0 ]; then
    status=1
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM

echo "[backend-eval] starting isolated metadata PostgreSQL on 127.0.0.1:$metadata_port"
metadata_started=1
metadata_compose up --detach --wait neko-db
wait_for_metadata_database

echo "[backend-eval] applying OpenNeko metadata migrations"
(
  cd "$repo_root/apps/openneko"
  XDG_CONFIG_HOME=$metadata_config_root \
  NEKO_PG_HOST=127.0.0.1 \
  NEKO_PG_PORT=$metadata_port \
  NEKO_PG_USER=neko \
  NEKO_PG_PASSWORD=secret \
  NEKO_PG_DATABASE=neko \
  NEKO_PG_SSLMODE=disable \
  OPENNEKO_OPENSHELL_DB_PASSWORD=openneko-eval-openshell-development-password \
  OPENNEKO_REQUIRE_EXPLICIT_OPENSHELL_DB_PASSWORD=1 \
    go run ./cmd/openneko migrate
)

expected_migrations=$(find "$repo_root/db/migrations" -type f -name '*.sql' | wc -l | tr -d '[:space:]')
observed_migrations=$(metadata_compose exec -T neko-db \
  psql --tuples-only --no-align --username neko --dbname neko \
    --command 'select count(*) from schema_migrations' | tr -d '[:space:]')
[ "$observed_migrations" = "$expected_migrations" ] || {
  echo "metadata migration count mismatch: expected $expected_migrations, observed $observed_migrations" >&2
  exit 1
}

echo "[backend-eval] restoring and verifying frozen AdventureWorks"
data_attempted=1
"$repo_root/scripts/eval-adventureworks/environment.sh" up
data_ready=1

aw_db_port=${OPENNEKO_EVAL_AW_DB_PORT:-15433}
graphjin_port=${OPENNEKO_EVAL_GRAPHJIN_PORT:-18080}
export OPENNEKO_EVAL_ADVENTUREWORKS_GRAPHQL_URL="${OPENNEKO_EVAL_ADVENTUREWORKS_GRAPHQL_URL:-http://127.0.0.1:$graphjin_port/api/v1/graphql}"
export OPENNEKO_EVAL_ADVENTUREWORKS_MCP_URL="${OPENNEKO_EVAL_ADVENTUREWORKS_MCP_URL:-http://127.0.0.1:$graphjin_port/api/v1/mcp}"
export OPENNEKO_EVAL_ADVENTUREWORKS_DATABASE_URL="${OPENNEKO_EVAL_ADVENTUREWORKS_DATABASE_URL:-postgresql://openneko_eval_reader:eval-reader-only@127.0.0.1:$aw_db_port/adventureworks}"
export OPENNEKO_AGENT_IMAGE="$agent_image"
export NEKO_EMBEDDING_URL="http://127.0.0.1:${OPENNEKO_EVAL_EMBEDDING_PORT:-15003}"
export OPENNEKO_PG_ENV_OVERRIDE=1
export NEKO_PG_HOST=127.0.0.1
export NEKO_PG_PORT="$metadata_port"
export NEKO_PG_USER=neko
export NEKO_PG_PASSWORD=secret
export NEKO_PG_DATABASE=neko

set -- run --config "$config_path"
if [ -n "$resume_run" ]; then
  set -- "$@" --resume "$resume_run"
fi
if [ "$restart" -eq 1 ]; then
  set -- "$@" --restart
fi
if [ -n "$promotion" ]; then
  set -- "$@" "$promotion"
fi

echo "[backend-eval] running cohort"
run_fifo=$metadata_config_root/run-output.fifo
run_log=$metadata_config_root/run-output.log
mkfifo "$run_fifo"
tee "$run_log" <"$run_fifo" &
stream_pid=$!
if run_eval_cli "$@" >"$run_fifo" 2>&1; then
  run_status=0
else
  run_status=$?
fi
wait "$stream_pid" || {
  echo "failed to stream eval output" >&2
  exit 1
}
rm -f -- "$run_fifo"
case "$run_status" in
  0|2) ;;
  *) exit "$run_status" ;;
esac

if [ "$promotion" = "--promote" ]; then
  result_dir=$(sed -n \
    -e 's/^completed run-[^;]*; reused [0-9][0-9]* episodes; result //p' \
    -e 's/^resumed run-[^;]*; reused [0-9][0-9]* episodes; result //p' \
    "$run_log" | tail -n 1)
  [ -n "$result_dir" ] || {
    echo "eval completed without reporting its promoted result directory" >&2
    exit 1
  }
  echo "[backend-eval] verifying sanitized result"
  if run_eval_cli verify --result "$result_dir"; then
    verify_status=0
  else
    verify_status=$?
  fi
  case "$verify_status" in
    0|2) ;;
    *) exit "$verify_status" ;;
  esac
  [ "$verify_status" -eq "$run_status" ] || {
    echo "run and verification gate status disagree" >&2
    exit 1
  }
fi

exit "$run_status"
