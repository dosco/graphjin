#!/usr/bin/env bash
# Boot every example demo in sequence, run its full smoke suite (base + agent
# + agent-eval), tear it down, and print a summary table. Also runs the MCP
# sampling checks: the full borrow-the-client's-model loop against coffee
# (agent.sampling: auto) and the require-mode fail-closed quartet against a
# rebooted clinic-scheduler.
#
# The "default" entry covers the bare `graphjin serve --demo` flow: a
# CGO_ENABLED=0 binary (like releases) run in an empty directory must
# extract the built-in clinic-scheduler demo to ./graphjin-demo, pass the
# clinic smoke suite, and reuse the extracted state on a second boot.
#
# Preconditions: Docker running (not needed for --only default); ./.env with
# a provider key (OPENAI_API_KEY / ANTHROPIC_API_KEY / GOOGLE_APIKEY); curl,
# jq, openssl on PATH; ports 8080-8083 and 8093 free.
#
# Usage: scripts/demo-smoke-all.sh [--only <demo>] [--skip-sampling]
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

load_env_file() {
  local file="$1"
  [ -f "$file" ] || return 0
  local line key value
  while IFS= read -r line || [ -n "$line" ]; do
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    case "$line" in
      ""|\#*) continue ;;
      export\ *) line="${line#export }" ;;
    esac
    case "$line" in
      *=*) ;;
      *) continue ;;
    esac
    key="${line%%=*}"
    value="${line#*=}"
    key="${key%"${key##*[![:space:]]}"}"
    if [[ ! "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
      continue
    fi
    if [ -n "${!key:-}" ]; then
      continue
    fi
    value="${value#"${value%%[![:space:]]*}"}"
    value="${value%"${value##*[![:space:]]}"}"
    case "$value" in
      \"*\") value="${value#\"}"; value="${value%\"}" ;;
      \'*\') value="${value#\'}"; value="${value%\'}" ;;
    esac
    export "${key}=${value}"
  done <"$file"
}

load_env_file ".env"

ONLY=""
SKIP_SAMPLING=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --only)
      ONLY="${2:-}"
      shift 2
      ;;
    --skip-sampling)
      SKIP_SAMPLING=1
      shift
      ;;
    -h|--help)
      sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

DEMOS="default coffee-roastery clinic-scheduler corrugated-plant pcb-fab"
SAMPLING_DISCOVERY_INSTRUCTION='Say hello. Do not run any tools.'

port_for_demo() {
  case "$1" in
    coffee-roastery) echo 8080 ;;
    corrugated-plant) echo 8081 ;;
    pcb-fab) echo 8082 ;;
    clinic-scheduler) echo 8083 ;;
    *) echo "" ;;
  esac
}

RESULTS=()
SERVER_PID=""
SERVER_LOG=""
SERVER_PORT=""
SERVER_OWNED=""

port_listeners() {
  local port="$1"
  if ! command -v lsof >/dev/null 2>&1; then
    return 0
  fi
  lsof -tiTCP:"${port}" -sTCP:LISTEN 2>/dev/null || true
}

wait_port_free() {
  local port="$1"
  local waited=0
  while [ "$waited" -lt 30 ]; do
    if [ -z "$(port_listeners "$port")" ]; then
      return 0
    fi
    sleep 1
    waited=$((waited + 1))
  done
  return 1
}

kill_port_listeners() {
  local port="$1"
  local pids
  pids="$(port_listeners "$port" | tr '\n' ' ')"
  if [ -z "$pids" ]; then
    return 0
  fi
  kill -TERM $pids 2>/dev/null || true
  if wait_port_free "$port"; then
    return 0
  fi
  pids="$(port_listeners "$port" | tr '\n' ' ')"
  if [ -n "$pids" ]; then
    kill -KILL $pids 2>/dev/null || true
  fi
}

kill_server() {
  if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    # go run forwards signals to the built binary, which tears down its
    # testcontainers on SIGTERM.
    kill -TERM "$SERVER_PID" 2>/dev/null || true
    local waited=0
    while kill -0 "$SERVER_PID" 2>/dev/null && [ "$waited" -lt 60 ]; do
      sleep 1
      waited=$((waited + 1))
    done
    if kill -0 "$SERVER_PID" 2>/dev/null; then
      kill -KILL "$SERVER_PID" 2>/dev/null || true
    fi
  fi
  if [ -n "$SERVER_OWNED" ] && [ -n "$SERVER_PORT" ] && ! wait_port_free "$SERVER_PORT"; then
    kill_port_listeners "$SERVER_PORT"
    wait_port_free "$SERVER_PORT" || true
  fi
  SERVER_PID=""
  SERVER_PORT=""
  SERVER_OWNED=""
}
trap 'kill_server' EXIT

boot_server() {
  local demo="$1"
  local port="$2"
  local workdir="$3"
  shift 3
  SERVER_PORT="$port"
  if [ -n "$(port_listeners "$port")" ]; then
    echo "port ${port} is already in use; refusing to boot ${demo}" >&2
    return 1
  fi
  SERVER_LOG="$(mktemp "${TMPDIR:-/tmp}/gj-demo-${demo}.XXXXXX")"
  echo "==> booting ${demo} on :${port} (log: ${SERVER_LOG})"
  (cd "$workdir" && exec "$@") >"$SERVER_LOG" 2>&1 &
  SERVER_PID=$!
  SERVER_OWNED=1
  local waited=0
  until curl -sf "http://localhost:${port}/health" >/dev/null 2>&1; do
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
      echo "server for ${demo} exited during boot; last log lines:" >&2
      tail -40 "$SERVER_LOG" >&2
      return 1
    fi
    sleep 2
    waited=$((waited + 2))
    if [ "$waited" -ge 300 ]; then
      echo "server for ${demo} did not become healthy within 300s; last log lines:" >&2
      tail -40 "$SERVER_LOG" >&2
      return 1
    fi
  done
  echo "==> ${demo} healthy after ~${waited}s"
}

boot_demo() {
  local demo="$1"
  local port="$2"
  shift 2
  boot_server "$demo" "$port" "." env "$@" go run ./cmd serve --demo --path "examples/${demo}"
}

record() {
  RESULTS+=("$1")
  printf '%s\n' "$1"
}

run_demo() {
  local demo="$1"
  local port
  port="$(port_for_demo "$demo")"
  local started ok=1 sampling="-"
  started="$(date +%s)"
  load_env_file "examples/${demo}/.env"

  if ! boot_demo "$demo" "$port" GO_ENV=agentic; then
    record "${demo} | BOOT FAILED | - | $(($(date +%s) - started))s"
    kill_server
    return 1
  fi

  if ! "examples/${demo}/scripts/smoke.sh" --url "http://localhost:${port}" --agent-eval; then
    ok=""
  fi

  # Sampling auto-loop: coffee runs agent.sampling: auto, so a sampling-capable
  # client must drive at least one sampling/createMessage round trip.
  if [ -z "$SKIP_SAMPLING" ] && [ "$demo" = "coffee-roastery" ] && [ -n "$ok" ]; then
    echo "==> running sampling auto-loop via mcp-sampling-client"
    if go run ./tools/mcp-sampling-client \
        --url "http://localhost:${port}/api/v1/mcp" \
        --jwt-secret "coffee-roastery-demo-jwt-secret" \
        --instruction "$SAMPLING_DISCOVERY_INSTRUCTION" \
        | tee /dev/stderr | jq -e '.sampling_calls >= 1 and (.is_error | not)' >/dev/null; then
      sampling="auto:ok"
    else
      sampling="auto:FAIL"
      ok=""
    fi
  fi

  kill_server

  # Sampling require-mode quartet: reboot the fast clinic demo with
  # GJ_AGENT_SAMPLING=require and assert fail-closed + client behavior.
  if [ -z "$SKIP_SAMPLING" ] && [ "$demo" = "clinic-scheduler" ] && [ -n "$ok" ]; then
    echo "==> rebooting ${demo} in sampling require mode"
    if boot_demo "$demo" "$port" GO_ENV=agentic GJ_AGENT_SAMPLING=require GJ_MCP_HTTP_STATEFUL=true; then
      local req_ok=1
      "examples/${demo}/scripts/smoke.sh" --url "http://localhost:${port}" --no-agent --sampling || req_ok=""
      if [ -n "$req_ok" ]; then
        if go run ./tools/mcp-sampling-client \
            --url "http://localhost:${port}/api/v1/mcp" \
            --jwt-secret "clinic-scheduler-demo-jwt-secret" \
            --instruction "$SAMPLING_DISCOVERY_INSTRUCTION" \
            | tee /dev/stderr | jq -e '.sampling_calls >= 1 and (.is_error | not)' >/dev/null; then
          :
        else
          req_ok=""
        fi
      fi
      if [ -n "$req_ok" ]; then
        if go run ./tools/mcp-sampling-client --no-sampling \
            --url "http://localhost:${port}/api/v1/mcp" \
            --jwt-secret "clinic-scheduler-demo-jwt-secret" \
            --instruction "List one saved query." \
            | tee /dev/stderr | jq -e '.is_error == true' >/dev/null; then
          :
        else
          req_ok=""
        fi
      fi
      if [ -n "$req_ok" ]; then
        sampling="require:ok"
      else
        sampling="require:FAIL"
        ok=""
      fi
      kill_server
    else
      sampling="require:BOOT-FAIL"
      ok=""
    fi
  fi

  local dur=$(($(date +%s) - started))
  if [ -n "$ok" ]; then
    record "${demo} | PASS | ${sampling} | ${dur}s"
  else
    record "${demo} | FAIL | ${sampling} | ${dur}s"
    return 1
  fi
}

# run_default_demo smokes the bare `graphjin serve --demo` flow: build a
# CGO_ENABLED=0 binary (matching release builds), run it in an empty
# directory with only the provider key in the environment, and expect the
# built-in clinic-scheduler demo extracted to ./graphjin-demo, the clinic
# smoke suite green, and the extracted state reused on a second boot.
run_default_demo() {
  local port=8083
  local started ok=1
  started="$(date +%s)"
  load_env_file "examples/clinic-scheduler/.env"

  local bindir workdir
  bindir="$(mktemp -d "${TMPDIR:-/tmp}/gj-default-bin.XXXXXX")"
  workdir="$(mktemp -d "${TMPDIR:-/tmp}/gj-default-demo.XXXXXX")"
  echo "==> building CGO_ENABLED=0 binary for the built-in demo"
  if ! CGO_ENABLED=0 go build -o "${bindir}/graphjin" ./cmd; then
    record "default | BUILD FAILED | - | $(($(date +%s) - started))s"
    rm -rf "$bindir" "$workdir"
    return 1
  fi

  if ! boot_server "default" "$port" "$workdir" "${bindir}/graphjin" serve --demo; then
    record "default | BOOT FAILED | - | $(($(date +%s) - started))s"
    kill_server
    rm -rf "$bindir" "$workdir"
    return 1
  fi
  if ! grep -q "demo project.*created" "$SERVER_LOG"; then
    echo "default demo did not extract the built-in project" >&2
    ok=""
  fi
  if [ ! -f "${workdir}/graphjin-demo/demo/manifest.json" ]; then
    echo "default demo state manifest missing under ${workdir}/graphjin-demo/demo" >&2
    ok=""
  fi
  if [ -n "$ok" ] && ! "examples/clinic-scheduler/scripts/smoke.sh" --url "http://localhost:${port}" --agent-eval; then
    ok=""
  fi
  kill_server

  # Second boot must reuse the extracted project and keep its data.
  if [ -n "$ok" ]; then
    if boot_server "default" "$port" "$workdir" "${bindir}/graphjin" serve --demo; then
      if ! grep -q "demo project.*reused" "$SERVER_LOG"; then
        echo "default demo did not reuse ./graphjin-demo on reboot" >&2
        ok=""
      fi
      if [ -n "$ok" ] && ! "examples/clinic-scheduler/scripts/smoke.sh" --url "http://localhost:${port}" --no-agent; then
        ok=""
      fi
    else
      ok=""
    fi
    kill_server
  fi
  rm -rf "$bindir" "$workdir"

  local dur=$(($(date +%s) - started))
  if [ -n "$ok" ]; then
    record "default | PASS | - | ${dur}s"
  else
    record "default | FAIL | - | ${dur}s"
    return 1
  fi
}

FAILED=""
for demo in $DEMOS; do
  if [ -n "$ONLY" ] && [ "$demo" != "$ONLY" ]; then
    continue
  fi
  if [ "$demo" = "default" ]; then
    run_default_demo || FAILED=1
  else
    run_demo "$demo" || FAILED=1
  fi
done

echo
echo "demo | result | sampling | duration"
echo "---- | ------ | -------- | --------"
for row in "${RESULTS[@]}"; do
  printf '%s\n' "$row"
done

if [ -n "$FAILED" ]; then
  exit 1
fi
echo
echo "all demo smoke suites passed"
