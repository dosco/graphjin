#!/usr/bin/env bash
# agent-data-eval.sh — the GraphJin agent data-accuracy validation loop.
#
# Boots the saas-ops demo with a deliberately low default row limit (so the
# truncation failure class is reproducible), then runs the ground-truth data
# corpus through agent/cmd/skill-eval: every case is scored against a runtime
# oracle (trusted aggregate GraphQL against the same server) plus a method
# check (did the database compute, or did the model sum a row page?).
#
# Workflow:
#   scripts/agent-data-eval.sh --phase baseline                # before a change
#   ...land the change...
#   scripts/agent-data-eval.sh --phase candidate --baseline .graphjin-evals/<report>.json
#   cd agent && go run ./cmd/skill-eval -trend ../.graphjin-evals
#
# Candidate phase exits 2 when a hard gate fails: ground-truth or method
# recall regressing vs the baseline. Below-target recall (< 0.90) warns.
set -euo pipefail

INVOKE_DIR="$PWD"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PHASE="baseline"
MODEL="${GJ_AGENT_MODEL:-gpt-4.1}"
PROVIDER=""
BASELINE=""
REPORT=""
LEDGER="$ROOT/.graphjin-evals"
REPEATS=3
PORT=8083
DEMO_PATH="examples/saas-ops"
DEFAULT_LIMIT=10
WEAK_ARM=""
ALLOW_WEAK=0
JWT_SECRET="saas-ops-demo-jwt-secret"

usage() { sed -n '2,17p' "$0" | sed 's/^# \{0,1\}//'; }

while [ $# -gt 0 ]; do
  case "$1" in
    --phase) PHASE="$2"; shift 2 ;;
    --model) MODEL="$2"; shift 2 ;;
    --provider) PROVIDER="$2"; shift 2 ;;
    --baseline) BASELINE="$2"; shift 2 ;;
    --report) REPORT="$2"; shift 2 ;;
    --ledger) LEDGER="$2"; shift 2 ;;
    --repeats) REPEATS="$2"; shift 2 ;;
    --port) PORT="$2"; shift 2 ;;
    --demo-path) DEMO_PATH="$2"; shift 2 ;;
    --default-limit) DEFAULT_LIMIT="$2"; shift 2 ;;
    --weak-arm) WEAK_ARM="$2"; shift 2 ;;
    --allow-weak) ALLOW_WEAK=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 1 ;;
  esac
done

# Load provider keys the same way the demo does (shell env wins). Values may
# be quoted in .env; strip the quotes like `source` would.
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
    case "$line" in *=*) ;; *) continue ;; esac
    key="${line%%=*}"
    value="${line#*=}"
    key="${key%"${key##*[![:space:]]}"}"
    [[ "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || continue
    value="${value#"${value%%[![:space:]]*}"}"
    case "$value" in
      \"*\") value="${value#\"}"; value="${value%\"}" ;;
      \'*\') value="${value#\'}"; value="${value%\'}" ;;
    esac
    if [ -z "${!key:-}" ]; then export "$key=$value"; fi
  done <"$file"
}
load_env_file "$ROOT/.env"
load_env_file "$ROOT/$DEMO_PATH/.env"

if [ -z "${OPENAI_API_KEY:-}${ANTHROPIC_API_KEY:-}${GOOGLE_API_KEY:-}${GEMINI_API_KEY:-}" ]; then
  echo "no provider API key found (.env or shell); the agent needs one to run" >&2
  exit 1
fi

# --provider pins the agent provider instead of the demo's OPENAI-first probe.
case "$PROVIDER" in
  "") ;;
  openai) export GJ_AGENT_PROVIDER=openai GJ_AGENT_API_KEY_ENV=OPENAI_API_KEY ;;
  anthropic) export GJ_AGENT_PROVIDER=anthropic GJ_AGENT_API_KEY_ENV=ANTHROPIC_API_KEY ;;
  google|gemini) export GJ_AGENT_PROVIDER=google GJ_AGENT_API_KEY_ENV=GOOGLE_API_KEY ;;
  *) echo "unknown --provider '$PROVIDER' (openai|anthropic|google)" >&2; exit 1 ;;
esac
for tool in curl jq openssl git go; do
  command -v "$tool" >/dev/null || { echo "missing required tool: $tool" >&2; exit 1; }
done

# Mini/flash-tier models stall in the discovery loop (see AGENTS.md); they are
# only meaningful as an advisory --weak-arm, never as the gating model.
if [ "$ALLOW_WEAK" -eq 0 ] && printf '%s' "$MODEL" | grep -Eqi '(mini|flash|nano|lite)'; then
  echo "model '$MODEL' looks below the documented floor for agent evals;" >&2
  echo "use a GPT-4.1-class model, or pass --allow-weak to override," >&2
  echo "or measure weak-model robustness with --weak-arm '$MODEL' instead" >&2
  exit 1
fi
case "$PHASE" in baseline|candidate) ;; *) echo "--phase must be baseline or candidate" >&2; exit 1 ;; esac
if [ "$PHASE" = "candidate" ] && [ -z "$BASELINE" ]; then
  echo "--baseline <report.json> is required for the candidate phase" >&2
  exit 1
fi

# The runner executes from agent/, so every user-supplied path must be
# absolute before it is handed on; relative paths keep their meaning
# against the caller's directory.
abspath() {
  case "$1" in
    ""|/*) printf '%s' "$1" ;;
    *) printf '%s/%s' "$INVOKE_DIR" "$1" ;;
  esac
}
BASELINE="$(abspath "$BASELINE")"
REPORT="$(abspath "$REPORT")"
LEDGER="$(abspath "$LEDGER")"
if [ -n "$BASELINE" ] && [ ! -f "$BASELINE" ]; then
  echo "baseline report not found: $BASELINE" >&2
  exit 1
fi

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$LEDGER"
# Default reports live in the ledger directly (one entry per run); an
# explicit --report elsewhere gets an additional ledger copy for -trend.
LEDGER_FLAG="$LEDGER"
if [ -z "$REPORT" ]; then
  REPORT="$LEDGER/$STAMP-$PHASE-$(printf '%s' "$MODEL" | tr -c 'a-zA-Z0-9.-' '_').json"
  LEDGER_FLAG=""
fi
SCRATCH="$(mktemp -d "${TMPDIR:-/tmp}/gj-agent-eval.XXXXXX")"
SERVER_BIN="$SCRATCH/graphjin-server"
EVAL_BIN="$SCRATCH/skill-eval"
SERVER_PID=""
BOOTED_PORT=0
SERVER_LOG="$SCRATCH/server.log"

kill_port() {
  # Last-resort sweep, only when this run booted the port. Two guards matter:
  # BOOTED_PORT (never touch a concurrent session's server) and -sTCP:LISTEN
  # (`lsof -ti :PORT` also matches CLIENTS of that port, so an unfiltered
  # sweep kills the eval runner itself mid-run).
  [ "$BOOTED_PORT" = "1" ] || return 0
  lsof -ti ":$PORT" -sTCP:LISTEN 2>/dev/null | xargs kill 2>/dev/null || true
}

cleanup() {
  if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  kill_port
  rm -rf "$SCRATCH"
}
trap cleanup EXIT

b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }
mint_jwt() {
  local header payload sig claims
  claims="$(jq -nc --arg sub "agent-data-eval" \
    '{sub: $sub, roles: ["user"], exp: (now | floor) + 7200}')"
  header="$(printf '%s' '{"alg":"HS256","typ":"JWT"}' | b64url)"
  payload="$(printf '%s' "$claims" | b64url)"
  sig="$(printf '%s.%s' "$header" "$payload" | openssl dgst -sha256 -hmac "$JWT_SECRET" -binary | b64url)"
  printf '%s.%s.%s' "$header" "$payload" "$sig"
}

boot_server() {
  local model="$1"
  if curl -sf "http://127.0.0.1:${PORT}/health" >/dev/null 2>&1; then
    echo "port ${PORT} is already serving; refusing to boot over it" >&2
    exit 1
  fi
  echo "==> booting $DEMO_PATH (model=$model, default_limit=$DEFAULT_LIMIT)"
  GO_ENV=agentic GJ_AGENT_MODEL="$model" GJ_DEFAULT_LIMIT="$DEFAULT_LIMIT" \
    "$SERVER_BIN" serve --demo --path "$DEMO_PATH" \
    >"$SERVER_LOG" 2>&1 &
  SERVER_PID=$!
  BOOTED_PORT=1
  local waited=0
  until curl -sf "http://127.0.0.1:${PORT}/health" >/dev/null 2>&1; do
    sleep 2
    waited=$((waited + 2))
    if ! kill -0 "$SERVER_PID" 2>/dev/null || [ "$waited" -ge 300 ]; then
      echo "server did not become healthy; last log lines:" >&2
      tail -n 40 "$SERVER_LOG" >&2 || true
      exit 1
    fi
  done
  echo "==> healthy after ~${waited}s"
}

stop_server() {
  if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  kill_port
  SERVER_PID=""
  BOOTED_PORT=0
}

run_eval() {
  local model="$1" phase="$2" report="$3" baseline="$4" gating="$5"
  local token profiles
  token="$(mint_jwt)"
  profiles="$SCRATCH/profiles-$model.json"
  jq -nc --arg url "http://127.0.0.1:${PORT}/api/v1/agent" --arg auth "Bearer $token" '{
    profiles: [{
      name: "user",
      capability_profile: {role_class: "user", read_only: false, available_system_roots: []},
      url: $url,
      headers: {Authorization: $auth}
    }]
  }' >"$profiles"

  local args=(
    -live -corpus "" -data-corpus "$ROOT/agent/testdata/data_eval_cases.json"
    -prompt-registry "$ROOT/agent/skills.go,$ROOT/agent/agent.go"
    -profiles "$profiles" -phase "$phase" -model "$model"
    -graphjin-commit "$(git rev-parse HEAD)" -repeats "$REPEATS"
    -timeout 6m -report "$report"
  )
  [ -n "$LEDGER_FLAG" ] && args+=(-ledger "$LEDGER_FLAG")
  [ -n "$baseline" ] && args+=(-baseline "$baseline")
  local status=0
  "$EVAL_BIN" "${args[@]}" || status=$?
  if [ -f "$report" ]; then
    echo "==> report: $report"
    jq -r '"ground-truth recall: \(.data_metrics.ground_truth_recall // "n/a")  method recall: \(.data_metrics.method_recall // "n/a")",
      "by group: \(.data_metrics.ground_truth_recall_by_group // {} | to_entries | map("\(.key)=\(.value)") | join(" "))",
      "failure buckets: \(.data_metrics.failure_buckets // {} | to_entries | map("\(.key)=\(.value)") | join(" "))",
      "hard pass: \(.acceptance.hard_pass)"' "$report"
  fi
  if [ "$gating" = "gating" ]; then
    return "$status"
  fi
  return 0
}

echo "==> building server and eval runner"
GOTOOLCHAIN=auto go build -o "$SERVER_BIN" ./cmd
(cd agent && GOTOOLCHAIN=auto go build -o "$EVAL_BIN" ./cmd/skill-eval)

boot_server "$MODEL"
STATUS=0
run_eval "$MODEL" "$PHASE" "$REPORT" "$BASELINE" gating || STATUS=$?
stop_server

if [ -n "$WEAK_ARM" ]; then
  echo "==> weak-model arm ($WEAK_ARM); advisory only, never gates"
  boot_server "$WEAK_ARM"
  run_eval "$WEAK_ARM" baseline \
    "$LEDGER/$STAMP-weak-$(printf '%s' "$WEAK_ARM" | tr -c 'a-zA-Z0-9.-' '_').json" "" advisory
  stop_server
fi

exit "$STATUS"
