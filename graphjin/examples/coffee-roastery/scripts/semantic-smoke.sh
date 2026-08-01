#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
DEMO_DIR="${GRAPHJIN_SEMANTIC_SMOKE_DEMO_DIR:-$ROOT_DIR/examples/coffee-roastery}"
MODE="${GRAPHJIN_SEMANTIC_SMOKE_MODE:-fixture}"
PORT="${GRAPHJIN_SEMANTIC_SMOKE_PORT:-18080}"
EMBEDDING_PORT="${GRAPHJIN_SEMANTIC_SMOKE_EMBEDDING_PORT:-18081}"
GRAPHJIN_BIN="${GRAPHJIN_SEMANTIC_SMOKE_BIN:-}"
REPORT_PATH="${GRAPHJIN_SEMANTIC_SMOKE_REPORT:-}"
SEMANTIC_PROVIDER="${GRAPHJIN_SEMANTIC_PROVIDER:-openai}"
SEMANTIC_MODEL="${GRAPHJIN_SEMANTIC_MODEL:-text-embedding-3-small}"
SEMANTIC_BASE_URL="${GRAPHJIN_SEMANTIC_BASE_URL:-}"
SEMANTIC_API_KEY_ENV="${GRAPHJIN_SEMANTIC_API_KEY_ENV:-OPENAI_API_KEY}"
TIMEOUT="${GRAPHJIN_SEMANTIC_SMOKE_TIMEOUT:-240}"
KEEP_TMP="${GRAPHJIN_SEMANTIC_SMOKE_KEEP_TMP:-false}"
RUN_AGENT="${GRAPHJIN_SEMANTIC_AGENT_SMOKE:-false}"

usage() {
  printf '%s\n' 'Coffee roastery lexical-vs-semantic discovery smoke test.

Usage:
  examples/coffee-roastery/scripts/semantic-smoke.sh [options]

Options:
  --live                  Use a real embedding provider instead of the deterministic fixture.
  --port PORT             GraphJin port (default: 18080).
  --embedding-port PORT   Fixture embedding port (default: 18081).
  --graphjin-bin PATH     Reuse a GraphJin binary instead of building ./cmd.
  --report PATH           Keep the JSON comparison report at PATH.
  --agent                 Run the deterministic end-to-end Ax/Goja agent check.
  -h, --help              Show this help.

Live-provider environment:
  GRAPHJIN_SEMANTIC_SMOKE_DEMO_DIR Override the demo directory (useful for isolated state).
  GRAPHJIN_SEMANTIC_PROVIDER       Ax provider name (default: openai).
  GRAPHJIN_SEMANTIC_MODEL          Embedding model (default: text-embedding-3-small).
  GRAPHJIN_SEMANTIC_BASE_URL       Optional provider base URL.
  GRAPHJIN_SEMANTIC_API_KEY_ENV    Name of the provider key variable.

The deterministic mode proves the Ax HTTP adapter, cold and warm filesystem
generations, hybrid ranking, explanations, exact-match bypass, relationship
paths, and low-confidence fallback. --agent additionally drives the real REST
agent through one adaptive coverage batch and detail inspection. --live
measures the quality of the configured production embedding model.'
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --live)
      MODE="live"
      shift
      ;;
    --port)
      PORT="${2:-}"
      shift 2
      ;;
    --embedding-port)
      EMBEDDING_PORT="${2:-}"
      shift 2
      ;;
    --graphjin-bin)
      GRAPHJIN_BIN="${2:-}"
      shift 2
      ;;
    --report)
      REPORT_PATH="${2:-}"
      shift 2
      ;;
    --agent)
      RUN_AGENT=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

for binary in curl docker jq; do
  if ! command -v "$binary" >/dev/null 2>&1; then
    echo "missing required command: $binary" >&2
    exit 2
  fi
done
if [ "$MODE" != "fixture" ] && [ "$MODE" != "live" ]; then
  echo "GRAPHJIN_SEMANTIC_SMOKE_MODE must be fixture or live" >&2
  exit 2
fi
if [ "$MODE" = "live" ] && [ -z "$(printenv "$SEMANTIC_API_KEY_ENV" 2>/dev/null || true)" ]; then
  echo "live semantic smoke requires $SEMANTIC_API_KEY_ENV" >&2
  exit 2
fi
if [ "$RUN_AGENT" = true ] && [ "$MODE" != "fixture" ]; then
  echo "--agent currently requires deterministic fixture mode" >&2
  exit 2
fi
if ! docker info >/dev/null 2>&1; then
  echo "Docker is required and is not reachable" >&2
  exit 2
fi

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/gj-semantic-smoke.XXXXXX")"
CACHE_REL=".graphjin/semantic-smoke-$$"
CACHE_DIR="$DEMO_DIR/$CACHE_REL"
BASE_URL="http://127.0.0.1:$PORT"
SERVER_PID=""
FIXTURE_PID=""
MCP_SESSION_ID=""
MCP_INITIALIZED=false
CALL_SEQUENCE=0
SEMANTIC_USER_ID="semantic-smoke"
SEMANTIC_ACCOUNT_ID="semantic-account-one"

log() {
  printf '==> %s\n' "$*" >&2
}

pass() {
  printf 'ok  %s\n' "$*" >&2
}

fail() {
  printf 'not ok  %s\n' "$*" >&2
  if [ -n "${SERVER_LOG:-}" ] && [ -f "$SERVER_LOG" ]; then
    tail -n 120 "$SERVER_LOG" >&2 || true
  fi
  exit 1
}

stop_graphjin() {
  if [ -z "$SERVER_PID" ]; then
    return
  fi
  if kill -0 "$SERVER_PID" >/dev/null 2>&1; then
    kill -TERM "$SERVER_PID" >/dev/null 2>&1 || true
    local attempt
    for attempt in $(seq 1 160); do
      if ! kill -0 "$SERVER_PID" >/dev/null 2>&1; then
        break
      fi
      sleep 0.25
    done
  fi
  if kill -0 "$SERVER_PID" >/dev/null 2>&1; then
    kill -KILL "$SERVER_PID" >/dev/null 2>&1 || true
  fi
  wait "$SERVER_PID" >/dev/null 2>&1 || true
  SERVER_PID=""
}

cleanup() {
  set +e
  stop_graphjin
  if [ -n "$FIXTURE_PID" ]; then
    kill -TERM "$FIXTURE_PID" >/dev/null 2>&1 || true
    wait "$FIXTURE_PID" >/dev/null 2>&1 || true
  fi
  if [ "$KEEP_TMP" = true ]; then
    printf 'debug  semantic smoke workdir retained at %s (cache: %s)\n' "$TMP_DIR" "$CACHE_DIR" >&2
  else
    rm -rf "$CACHE_DIR" "$TMP_DIR"
  fi
}
trap cleanup EXIT INT TERM

if [ -z "$GRAPHJIN_BIN" ]; then
  if ! command -v go >/dev/null 2>&1; then
    fail "Go is required when --graphjin-bin is not supplied"
  fi
  GRAPHJIN_BIN="$TMP_DIR/graphjin"
  log "building GraphJin"
  (cd "$ROOT_DIR" && GOCACHE="${GOCACHE:-/tmp/go-build}" go build -o "$GRAPHJIN_BIN" ./cmd)
elif [ ! -x "$GRAPHJIN_BIN" ]; then
  fail "GraphJin binary is not executable: $GRAPHJIN_BIN"
fi

if [ "$MODE" = "fixture" ]; then
  FIXTURE_BIN="$TMP_DIR/embedding-fixture"
  log "building deterministic Ax-compatible embedding fixture"
  (cd "$ROOT_DIR" && GOCACHE="${GOCACHE:-/tmp/go-build}" go build -o "$FIXTURE_BIN" ./examples/coffee-roastery/tools/embedding-fixture)
  "$FIXTURE_BIN" --listen "127.0.0.1:$EMBEDDING_PORT" >"$TMP_DIR/embedding-fixture.log" 2>&1 &
  FIXTURE_PID=$!
  for _ in $(seq 1 80); do
    if curl -fsS "http://127.0.0.1:$EMBEDDING_PORT/health" >/dev/null 2>&1; then
      break
    fi
    if ! kill -0 "$FIXTURE_PID" >/dev/null 2>&1; then
      fail "embedding fixture exited during startup"
    fi
    sleep 0.25
  done
  if ! curl -fsS "http://127.0.0.1:$EMBEDDING_PORT/health" >/dev/null 2>&1; then
    fail "embedding fixture did not become ready"
  fi
  SEMANTIC_MODEL="coffee-semantic-smoke-v1"
  SEMANTIC_BASE_URL="http://127.0.0.1:$EMBEDDING_PORT/v1"
  SEMANTIC_API_KEY_ENV="OPENAI_API_KEY"
  export OPENAI_API_KEY="coffee-semantic-fixture"
fi

start_graphjin() {
  local semantic_enabled="$1"
  local label="$2"
  SERVER_LOG="$TMP_DIR/graphjin-$label.log"
  MCP_SESSION_ID=""
  MCP_INITIALIZED=false
  log "starting coffee-roastery demo ($label)"
  env \
    GO_ENV=dev \
    NO_COLOR=1 \
    GJ_HOST_PORT="127.0.0.1:$PORT" \
    GJ_AGENT_ENABLED="$RUN_AGENT" \
    GJ_AGENT_PROVIDER=openai \
    GJ_AGENT_MODEL=coffee-agent-smoke-v1 \
    GJ_AGENT_API_KEY_ENV=OPENAI_API_KEY \
    GJ_AGENT_BASE_URL="$SEMANTIC_BASE_URL" \
    GJ_AGENT_TIMEOUT_SECONDS="$TIMEOUT" \
    GJ_MCP_INCLUDE_TOOLS_WITH_AGENT=true \
    GJ_DISCOVERY_CACHE_ENABLED=true \
    GJ_DISCOVERY_CACHE_PATH="$CACHE_REL" \
    GJ_DISCOVERY_CACHE_REFRESH_INTERVAL=1h \
    GJ_CATALOG_SEARCH_SEMANTIC_ENABLED="$semantic_enabled" \
    GJ_CATALOG_SEARCH_SEMANTIC_PROVIDER="$SEMANTIC_PROVIDER" \
    GJ_CATALOG_SEARCH_SEMANTIC_EMBEDDING_MODEL="$SEMANTIC_MODEL" \
    GJ_CATALOG_SEARCH_SEMANTIC_API_KEY_ENV="$SEMANTIC_API_KEY_ENV" \
    GJ_CATALOG_SEARCH_SEMANTIC_BASE_URL="$SEMANTIC_BASE_URL" \
    GJ_CATALOG_SEARCH_SEMANTIC_DIMENSIONS=tiny \
    "$GRAPHJIN_BIN" serve --demo --path "$DEMO_DIR" >"$SERVER_LOG" 2>&1 &
  SERVER_PID=$!

  local deadline=$((SECONDS + TIMEOUT))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if curl -fsS "$BASE_URL/health" >/dev/null 2>&1; then
      pass "coffee-roastery demo is ready ($label)"
      return
    fi
    if ! kill -0 "$SERVER_PID" >/dev/null 2>&1; then
      fail "coffee-roastery demo exited during $label startup"
    fi
    sleep 1
  done
  fail "coffee-roastery demo did not become ready within ${TIMEOUT}s ($label)"
}

mcp_body_json() {
  local raw="$1"
  local output="$2"
  if head -c 6 "$raw" | grep -q '^event:'; then
    grep '^data:' "$raw" | sed 's/^data: //' | tail -1 >"$output"
  else
    cp "$raw" "$output"
  fi
}

mcp_initialize() {
  local raw="$TMP_DIR/mcp-initialize.raw"
  local headers="$TMP_DIR/mcp-initialize.headers"
  local body="$TMP_DIR/mcp-initialize.json"
  curl -fsS --max-time "$TIMEOUT" -D "$headers" -o "$raw" \
    -X POST "$BASE_URL/api/v1/mcp" \
    -H 'Content-Type: application/json' \
    -H 'Accept: application/json, text/event-stream' \
    -H "X-User-ID: $SEMANTIC_USER_ID" \
    -H 'X-User-Role: user' \
    -H "X-Account-ID: $SEMANTIC_ACCOUNT_ID" \
    --data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"semantic-smoke","version":"1.0"}}}'
  mcp_body_json "$raw" "$body"
  jq -e '(.error? | not) and (.result? != null)' "$body" >/dev/null || fail "MCP initialize failed"
  MCP_SESSION_ID="$(grep -i '^mcp-session-id:' "$headers" | tr -d '\r' | awk '{print $2}' | head -1 || true)"
  curl -fsS --max-time "$TIMEOUT" -o /dev/null \
    -X POST "$BASE_URL/api/v1/mcp" \
    -H 'Content-Type: application/json' \
    -H 'Accept: application/json, text/event-stream' \
    -H "X-User-ID: $SEMANTIC_USER_ID" \
    -H 'X-User-Role: user' \
    -H "X-Account-ID: $SEMANTIC_ACCOUNT_ID" \
    ${MCP_SESSION_ID:+-H "Mcp-Session-Id: $MCP_SESSION_ID"} \
    --data '{"jsonrpc":"2.0","method":"notifications/initialized"}' || true
  MCP_INITIALIZED=true
}

mcp_query_catalog() {
  local args="$1"
  local output="$2"
  local capability="${3:-}"
  if [ "$MCP_INITIALIZED" != true ]; then
    mcp_initialize
  fi
  CALL_SEQUENCE=$((CALL_SEQUENCE + 1))
  local raw="$TMP_DIR/mcp-call-$CALL_SEQUENCE.raw"
  local payload
  payload="$(jq -nc --argjson args "$args" '{jsonrpc:"2.0",id:2,method:"tools/call",params:{name:"query_catalog",arguments:$args}}')"
  curl -fsS --max-time "$TIMEOUT" -o "$raw" \
    -X POST "$BASE_URL/api/v1/mcp" \
    -H 'Content-Type: application/json' \
    -H 'Accept: application/json, text/event-stream' \
    -H "X-User-ID: $SEMANTIC_USER_ID" \
    -H 'X-User-Role: user' \
    -H "X-Account-ID: $SEMANTIC_ACCOUNT_ID" \
    ${MCP_SESSION_ID:+-H "Mcp-Session-Id: $MCP_SESSION_ID"} \
    --data "$payload"
  mcp_body_json "$raw" "$output"
  jq -e '(.error? | not) and (.result.structuredContent.cards? != null)' "$output" >/dev/null || fail "${capability:+[${capability}] }query_catalog failed"
}

semantic_graphql_as_identity() {
  local user="$1"
  local account="$2"
  local label="$3"
  local query="$4"
  local capability="${5:-}"
  local output="$TMP_DIR/graphql-$label.json"
  local payload http_code
  payload="$(jq -nc --arg query "$query" '{query:$query}')"
  http_code="$(curl -sS --max-time "$TIMEOUT" -o "$output" -w '%{http_code}' \
    -X POST "$BASE_URL/api/v1/graphql" \
    -H 'Content-Type: application/json' \
    -H "X-User-ID: $user" -H 'X-User-Role: user' -H "X-Account-ID: $account" \
    --data "$payload")"
  if [ "$http_code" -lt 200 ] || [ "$http_code" -ge 300 ] || ! jq -e '((.errors // []) | length) == 0' "$output" >/dev/null; then
    jq . "$output" >&2 2>/dev/null || cat "$output" >&2
    fail "${capability:+[${capability}] }GraphQL failed for $label"
  fi
  printf '%s\n' "$output"
}

# An alternate account gets an isolated stateful MCP session so the semantic
# scope assertion cannot inherit identity or cached transport state.
mcp_query_catalog_as_identity() {
  local user="$1"
  local account="$2"
  local args="$3"
  local output="$4"
  local label="$5"
  local capability="${6:-}"
  local init_raw="$TMP_DIR/mcp-$label-init.raw"
  local init_headers="$TMP_DIR/mcp-$label-init.headers"
  local raw="$TMP_DIR/mcp-$label-call.raw"
  local session_id payload
  curl -fsS --max-time "$TIMEOUT" -D "$init_headers" -o "$init_raw" \
    -X POST "$BASE_URL/api/v1/mcp" \
    -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
    -H "X-User-ID: $user" -H 'X-User-Role: user' -H "X-Account-ID: $account" \
    --data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"semantic-identity-smoke","version":"1.0"}}}'
  session_id="$(grep -i '^mcp-session-id:' "$init_headers" | tr -d '\r' | awk '{print $2}' | head -1 || true)"
  curl -fsS --max-time "$TIMEOUT" -o /dev/null \
    -X POST "$BASE_URL/api/v1/mcp" \
    -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
    -H "X-User-ID: $user" -H 'X-User-Role: user' -H "X-Account-ID: $account" \
    ${session_id:+-H "Mcp-Session-Id: $session_id"} \
    --data '{"jsonrpc":"2.0","method":"notifications/initialized"}' || true
  payload="$(jq -nc --argjson args "$args" '{jsonrpc:"2.0",id:2,method:"tools/call",params:{name:"query_catalog",arguments:$args}}')"
  curl -fsS --max-time "$TIMEOUT" -o "$raw" \
    -X POST "$BASE_URL/api/v1/mcp" \
    -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
    -H "X-User-ID: $user" -H 'X-User-Role: user' -H "X-Account-ID: $account" \
    ${session_id:+-H "Mcp-Session-Id: $session_id"} --data "$payload"
  mcp_body_json "$raw" "$output"
  if [ -n "$session_id" ]; then
    curl -fsS --max-time "$TIMEOUT" -o /dev/null -X DELETE "$BASE_URL/api/v1/mcp" \
      -H "X-User-ID: $user" -H 'X-User-Role: user' -H "X-Account-ID: $account" \
      -H "Mcp-Session-Id: $session_id" || true
  fi
  jq -e '(.error? | not) and (.result.structuredContent.cards? != null)' "$output" >/dev/null || fail "${capability:+[${capability}] }query_catalog failed for $label"
}

catalog_args() {
  jq -nc --arg search "$1" '{search:$search,where:{kind:{in:["table","relationship"]}},limit:20,explain:true}'
}

capture_suite() {
  local prefix="$1"
  mcp_query_catalog "$(catalog_args 'clients')" "$TMP_DIR/$prefix-clients.json"
  mcp_query_catalog "$(catalog_args 'purchases')" "$TMP_DIR/$prefix-purchases.json"
  mcp_query_catalog "$(catalog_args 'raw coffee inventory')" "$TMP_DIR/$prefix-inventory.json"
  mcp_query_catalog "$(catalog_args 'quality failures from recent roasting')" "$TMP_DIR/$prefix-quality.json"
  mcp_query_catalog "$(catalog_args 'clients and purchases')" "$TMP_DIR/$prefix-relationship.json"
  mcp_query_catalog "$(catalog_args 'employee payroll tax')" "$TMP_DIR/$prefix-unrelated.json"
}

table_rank() {
  jq -r --arg table "$2" '
    [.result.structuredContent.cards[]?.table_name] | index($table) as $rank |
    if $rank == null then "null" else ($rank + 1 | tostring) end
  ' "$1"
}

table_reason() {
  jq -r --arg table "$2" '
    (.result.structuredContent.cards[]? | select(.kind == "table" and .table_name == $table) | .id) as $id |
    .result.structuredContent.matches[$id].why // ""
  ' "$1" | head -1
}

wait_for_semantic_index() {
  local deadline=$((SECONDS + TIMEOUT))
  local probe="$TMP_DIR/semantic-ready-probe.json"
  while [ "$SECONDS" -lt "$deadline" ]; do
    mcp_query_catalog "$(catalog_args 'clients')" "$probe"
    if jq -e '[.result.structuredContent.matches[]?.why // "" | select(contains("semantic"))] | length > 0' "$probe" >/dev/null; then
      pass "semantic generation activated"
      return
    fi
    sleep 1
  done
  fail "semantic generation did not activate within ${TIMEOUT}s"
}

# Capability: ANNOTATION-SEMANTIC
# Contract: Approved account context becomes a separate semantic document that
# lifts its addressed entity only for that account, then disappears from the
# next generation after demotion without re-embedding catalog documents.
# Deeper coverage: TestApprovedAnnotationsAreSeparateAccountFilteredSemanticDocuments,
# TestAnnotationApprovalEmbedsOnlyTheNewDocument.
run_annotation_semantic_smoke() {
  log "checking account-filtered semantic annotation recall"

  local target_ref create_out annotation_id stats_before stats_after args result deadline foreign_result
  local stats_before_demote stats_after_demote demote_out
  target_ref="$(jq -r '[.result.structuredContent.cards[]? | select(.table_name == "production_orders")][0].id // empty' "$TMP_DIR/semantic-purchases.json")"
  [ -n "$target_ref" ] || fail "[ANNOTATION-SEMANTIC] production_orders target was not discovered"
  stats_before="$TMP_DIR/annotation-semantic-stats-before.json"
  stats_after="$TMP_DIR/annotation-semantic-stats-after.json"
  curl -fsS "http://127.0.0.1:$EMBEDDING_PORT/stats" >"$stats_before"

  create_out="$(semantic_graphql_as_identity "$SEMANTIC_USER_ID" "$SEMANTIC_ACCOUNT_ID" annotation-semantic-create "mutation { gj_artifacts(insert: { kind: \"annotation\", target_ref: \"${target_ref}\", content: \"Chargeback escrow lineage identifies the operations record used during dispute review.\" }) { id tier } }" ANNOTATION-SEMANTIC)"
  annotation_id="$(jq -r '[.data.gj_artifacts] | flatten | .[0].id' "$create_out")"
  semantic_graphql_as_identity "$SEMANTIC_USER_ID" "$SEMANTIC_ACCOUNT_ID" annotation-semantic-approve "mutation { gj_artifacts(where: { id: { eq: \"${annotation_id}\" } }, update: { tier: \"approved\" }) { id tier } }" ANNOTATION-SEMANTIC >/dev/null

  args="$(catalog_args 'chargeback escrow lineage')"
  result="$TMP_DIR/annotation-semantic-visible.json"
  deadline=$((SECONDS + TIMEOUT))
  while [ "$SECONDS" -lt "$deadline" ]; do
    mcp_query_catalog "$args" "$result" ANNOTATION-SEMANTIC
    if jq -e --arg id "$target_ref" '
      any(.result.structuredContent.cards[]?; .id == $id)
      and ((.result.structuredContent.matches[$id].why // "") | contains("approved organizational annotation recall"))
    ' "$result" >/dev/null; then
      break
    fi
    sleep 1
  done
  if ! jq -e --arg id "$target_ref" '
    any(.result.structuredContent.cards[]?; .id == $id)
    and ((.result.structuredContent.matches[$id].why // "") | contains("approved organizational annotation recall"))
  ' "$result" >/dev/null; then
    fail "[ANNOTATION-SEMANTIC] approved annotation did not lift its target"
  fi
  pass "[ANNOTATION-SEMANTIC] approved note lifts its target with an annotation explanation"

  curl -fsS "http://127.0.0.1:$EMBEDDING_PORT/stats" >"$stats_after"
  if ! jq -e --slurpfile before "$stats_before" '.inputs == ($before[0].inputs + 2)' "$stats_after" >/dev/null; then
    fail "[ANNOTATION-SEMANTIC] approval should embed one document and one query"
  fi
  pass "[ANNOTATION-SEMANTIC] approval embeds exactly one new document"

  foreign_result="$TMP_DIR/annotation-semantic-foreign.json"
  mcp_query_catalog_as_identity semantic-foreign semantic-account-two "$args" "$foreign_result" annotation-semantic-foreign ANNOTATION-SEMANTIC
  if jq -e --arg id "$target_ref" 'any(.result.structuredContent.cards[]?; .id == $id and ((.result.structuredContent.matches[$id].why // "") | contains("annotation")))' "$foreign_result" >/dev/null; then
    fail "[ANNOTATION-SEMANTIC] foreign account received annotation recall"
  fi
  pass "[ANNOTATION-SEMANTIC] foreign account cannot receive the lifted target"

  stats_before_demote="$TMP_DIR/annotation-semantic-stats-before-demote.json"
  stats_after_demote="$TMP_DIR/annotation-semantic-stats-after-demote.json"
  curl -fsS "http://127.0.0.1:$EMBEDDING_PORT/stats" >"$stats_before_demote"
  demote_out="$(semantic_graphql_as_identity "$SEMANTIC_USER_ID" "$SEMANTIC_ACCOUNT_ID" annotation-semantic-demote "mutation { gj_artifacts(where: { id: { eq: \"${annotation_id}\" } }, update: { tier: \"observed\" }) { id tier } }" ANNOTATION-SEMANTIC)"
  jq -e '([.data.gj_artifacts] | flatten | .[0]).tier == "observed"' "$demote_out" >/dev/null || fail "[ANNOTATION-SEMANTIC] demotion did not return observed tier"
  deadline=$((SECONDS + TIMEOUT))
  while [ "$SECONDS" -lt "$deadline" ]; do
    mcp_query_catalog "$args" "$result" ANNOTATION-SEMANTIC
    if ! jq -e --arg id "$target_ref" 'any(.result.structuredContent.cards[]?; .id == $id and ((.result.structuredContent.matches[$id].why // "") | contains("annotation")))' "$result" >/dev/null; then
      break
    fi
    sleep 1
  done
  if jq -e --arg id "$target_ref" 'any(.result.structuredContent.cards[]?; .id == $id and ((.result.structuredContent.matches[$id].why // "") | contains("annotation")))' "$result" >/dev/null; then
    fail "[ANNOTATION-SEMANTIC] demoted annotation remained in semantic recall"
  fi
  pass "[ANNOTATION-SEMANTIC] demotion removes target lifting in the next generation"
  curl -fsS "http://127.0.0.1:$EMBEDDING_PORT/stats" >"$stats_after_demote"
  if ! jq -e --slurpfile before "$stats_before_demote" '.inputs == $before[0].inputs' "$stats_after_demote" >/dev/null; then
    fail "[ANNOTATION-SEMANTIC] demotion unexpectedly embedded documents"
  fi
  pass "[ANNOTATION-SEMANTIC] demotion reuses existing vectors without embedding"

  semantic_graphql_as_identity "$SEMANTIC_USER_ID" "$SEMANTIC_ACCOUNT_ID" annotation-semantic-cleanup "mutation { gj_artifacts(delete: true, where: { id: { eq: \"${annotation_id}\" } }) { id } }" ANNOTATION-SEMANTIC >/dev/null
}

semantic_agent_request() {
  local user="$1"
  local account="$2"
  local instruction="$3"
  local output="$4"
  local capability="${5:-}"
  local payload http_code
  payload="$(jq -nc --arg instruction "$instruction" '{instruction:$instruction,max_steps:10,return_trace:false}')"
  http_code="$(curl -sS --max-time "$TIMEOUT" -o "$output" -w '%{http_code}' \
    -X POST "$BASE_URL/api/v1/agent" \
    -H 'Content-Type: application/json' -H "X-User-ID: $user" -H 'X-User-Role: user' -H "X-Account-ID: $account" \
    --data "$payload")"
  if [ "$http_code" -lt 200 ] || [ "$http_code" -ge 300 ]; then
    jq . "$output" >&2 2>/dev/null || cat "$output" >&2
    fail "${capability:+[${capability}] }agent request failed for $user"
  fi
}

# Capability: ANNOTATION-NOTICE, TASK-VERIFY-NOW
# Contract: Deferred drafts and failed declared checks stay visible on the next
# agent response instead of becoming silent server-side state.
# Deeper coverage: TestAnnotationLifecycleScopeCatalogMergeAndDemotion,
# TestTaskImmediateVerificationPassAndFail.
run_agent_state_notice_smoke() {
  log "checking deterministic agent state notices"
  local user="semantic-agent-smoke" account="$SEMANTIC_ACCOUNT_ID" target_ref task_out task_id note_out annotation_id fail_out output
  target_ref="$(jq -r '[.result.structuredContent.cards[]? | select(.table_name == "production_orders")][0].id // empty' "$TMP_DIR/semantic-purchases.json")"
  task_out="$(semantic_graphql_as_identity "$user" "$account" agent-notice-task "mutation { gj_task(insert: { goal: \"Failed verification notice fixture\" }) { id } }" TASK-VERIFY-NOW)"
  task_id="$(jq -r '[.data.gj_task] | flatten | .[0].id' "$task_out")"
  fail_out="$(semantic_graphql_as_identity "$user" "$account" agent-notice-task-fail "mutation { gj_task(where: { id: { eq: \"${task_id}\" } }, update: { status: \"closed\", outcome: \"Must not stick.\", verify_json: { saved_query_name: \"daily_roast_context\", expect: { path: \"production_orders\", op: \"count_le\", value: 0 } } }) { id status verify_status } }" TASK-VERIFY-NOW)"
  jq -e '([.data.gj_task] | flatten | .[0]) | .status == "open" and .verify_status == "failed"' "$fail_out" >/dev/null || fail "[TASK-VERIFY-NOW] notice fixture did not fail verification"
  note_out="$(semantic_graphql_as_identity "$user" "$account" agent-notice-annotation "mutation { gj_artifacts(insert: { kind: \"annotation\", target_ref: \"${target_ref}\", content: \"ANNOTATION-NOTICE deferred draft fixture.\" }) { id tier } }" ANNOTATION-NOTICE)"
  annotation_id="$(jq -r '[.data.gj_artifacts] | flatten | .[0].id' "$note_out")"
  output="$TMP_DIR/agent-state-notices.json"
  semantic_agent_request "$user" "$account" "Run the deterministic semantic discovery fixture and report the result." "$output" ANNOTATION-NOTICE
  if ! jq -e --arg task "$task_id" --arg note "$annotation_id" '
    .status == "answered"
    and any(.notices[]?; .kind == "task_verify_failed" and (.task_ids | index($task)) != null)
    and any(.notices[]?; .kind == "annotations_unshared" and (.annotation_ids | index($note)) != null)
  ' "$output" >/dev/null; then
    jq . "$output" >&2
    fail "[ANNOTATION-NOTICE] deterministic agent response omitted durable-state notices"
  fi
  pass "[TASK-VERIFY-NOW] failed verification is surfaced on the next agent response"
  pass "[ANNOTATION-NOTICE] observed draft is surfaced on the next agent response"
  semantic_graphql_as_identity "$user" "$account" agent-notice-annotation-cleanup "mutation { gj_artifacts(delete: true, where: { id: { eq: \"${annotation_id}\" } }) { id } }" ANNOTATION-NOTICE >/dev/null
  semantic_graphql_as_identity "$user" "$account" agent-notice-task-cleanup "mutation { gj_task(delete: true, where: { id: { eq: \"${task_id}\" } }) { id } }" TASK-VERIFY-NOW >/dev/null
}

# Capability: ANNOTATION-GUARD
# Contract: Even a scripted agent cannot publish an annotation in the same run
# that created it; the draft remains owner-only for later human confirmation.
# Deeper coverage: TestRunRequiresLaterUserConfirmationForAnnotationApproval.
run_annotation_agent_guard_smoke() {
  log "checking deterministic same-run annotation approval refusal"
  local user="semantic-annotation-guard" account="$SEMANTIC_ACCOUNT_ID" output drafts annotation_id existing_id
  # A failed prior smoke may have persisted its owner-scoped draft. Remove only
  # this sentinel's rows so the exactly-one assertion describes the current run.
  drafts="$(semantic_graphql_as_identity "$user" "$account" annotation-guard-preclean-read "query { gj_artifacts(search: \"ANNOTATION-GUARD-fixture-draft\", where: { kind: { eq: \"annotation\" } }) { id } }" ANNOTATION-GUARD)"
  while IFS= read -r existing_id; do
    [ -n "$existing_id" ] || continue
    semantic_graphql_as_identity "$user" "$account" annotation-guard-preclean "mutation { gj_artifacts(delete: true, where: { id: { eq: \"${existing_id}\" } }) { id } }" ANNOTATION-GUARD >/dev/null
  done < <(jq -r '.data.gj_artifacts[]?.id' "$drafts")
  output="$TMP_DIR/annotation-agent-guard.json"
  semantic_agent_request "$user" "$account" "ANNOTATION_GUARD_FIXTURE: create and approve the note now." "$output" ANNOTATION-GUARD
  if ! jq -e '.status == "blocked" and any(.errors[]?; .extensions.code == "annotation_tier_confirmation_required")' "$output" >/dev/null; then
    jq . "$output" >&2
    fail "[ANNOTATION-GUARD] same-run approval was not blocked by the agent protocol"
  fi
  pass "[ANNOTATION-GUARD] same-run annotation approval is refused"
  drafts="$(semantic_graphql_as_identity "$user" "$account" annotation-guard-draft "query { gj_artifacts(search: \"ANNOTATION-GUARD-fixture-draft\", where: { kind: { eq: \"annotation\" } }) { id tier content } }" ANNOTATION-GUARD)"
  if ! jq -e '(.data.gj_artifacts | length) == 1 and .data.gj_artifacts[0].tier == "observed"' "$drafts" >/dev/null; then
    jq . "$drafts" >&2
    fail "[ANNOTATION-GUARD] blocked run did not leave exactly one observed draft"
  fi
  pass "[ANNOTATION-GUARD] blocked run leaves the draft owner-only"
  annotation_id="$(jq -r '.data.gj_artifacts[0].id' "$drafts")"
  semantic_graphql_as_identity "$user" "$account" annotation-guard-cleanup "mutation { gj_artifacts(delete: true, where: { id: { eq: \"${annotation_id}\" } }) { id } }" ANNOTATION-GUARD >/dev/null
}

run_semantic_agent_smoke() {
  local before="$TMP_DIR/agent-stats-before.json"
  local after="$TMP_DIR/agent-stats-after.json"
  local output="$TMP_DIR/semantic-agent.json"
  local before_batches payload http_code

  curl -fsS "http://127.0.0.1:$EMBEDDING_PORT/stats" >"$before"
  before_batches="$(jq -r '.batch_sizes | length' "$before")"
  payload="$(jq -nc --arg instruction \
    'This is a discovery-only smoke evaluation. Treat the initial seed as incomplete: use the one adaptive semantic coverage batch to find clients, their purchases awaiting production, and the real relationship between them. Inspect the returned endpoint and relationship card ids before answering. Do not execute data queries.' \
    '{instruction:$instruction,max_steps:6,return_trace:false}')"

  http_code="$(curl -sS --max-time "$TIMEOUT" \
    -o "$output" \
    -w '%{http_code}' \
    -X POST "$BASE_URL/api/v1/agent" \
    -H 'Content-Type: application/json' \
    -H 'X-User-ID: semantic-agent-smoke' \
    -H 'X-User-Role: user' \
    -H "X-Account-ID: $SEMANTIC_ACCOUNT_ID" \
    --data "$payload")"
  if [ "$http_code" -lt 200 ] || [ "$http_code" -ge 300 ]; then
    jq . "$output" >&2 2>/dev/null || cat "$output" >&2
    fail "semantic agent returned HTTP $http_code"
  fi

  jq -e '
    .status == "answered" and
    ([.actions[]? | select(
      .tool == "query_catalog" and
      (.args.searches | length) == 3 and
      .status == "ok" and
      .summary.coverage_phrase_count == 3 and
      .summary.relationship_path_count >= 1 and
      ([.summary.catalog_ids[]? | select(contains("customers"))] | length) >= 1 and
      ([.summary.catalog_ids[]? | select(contains("production_orders"))] | length) >= 1
    )] | length) == 1 and
    ([.actions[]? | select(
      .tool == "query_catalog" and
      (.args.ids | length) >= 2 and
      .status == "ok"
    )] | length) >= 1 and
    (.evidence.protocol.catalog_detail_ids | length) >= 2 and
    (.evidence.protocol.executions | length) == 0
  ' "$output" >/dev/null || {
    jq . "$output" >&2
    fail "agent did not use one coverage batch, inspect its ids, and preserve the real path"
  }

  curl -fsS "http://127.0.0.1:$EMBEDDING_PORT/stats" >"$after"
  jq -e --argjson before_batches "$before_batches" --slurpfile before "$before" '
    .chat_requests > $before[0].chat_requests and
    .batch_sizes[$before_batches:] == [1, 2]
  ' "$after" >/dev/null || {
    printf 'before: ' >&2; jq -c . "$before" >&2
    printf 'after:  ' >&2; jq -c . "$after" >&2
    fail "agent coverage did not reuse the cached phrase and embed its two misses in one Ax request"
  }
  pass "REST agent used one three-phrase semantic batch, embedded two cache misses together, inspected returned ids, and kept joins catalog-grounded"
}

flow_graphql() {
  local label="$1"
  local query="$2"
  local output="$TMP_DIR/flow-$label.json"
  local payload http_code
  payload="$(jq -nc --arg query "$query" '{query:$query}')"
  http_code="$(curl -sS --max-time "$TIMEOUT" \
    -o "$output" \
    -w '%{http_code}' \
    -X POST "$BASE_URL/api/v1/graphql" \
    -H 'Content-Type: application/json' \
    -H 'X-User-ID: semantic-flow-smoke' \
    -H 'X-User-Role: user' \
    --data "$payload")"
  if [ "$http_code" -lt 200 ] || [ "$http_code" -ge 300 ] || ! jq -e '(.errors? // []) | length == 0' "$output" >/dev/null; then
    jq . "$output" >&2 2>/dev/null || cat "$output" >&2
    fail "watch flow GraphQL failed: $label"
  fi
  printf '%s\n' "$output"
}

run_watch_flow_smoke() {
  log "checking unified gj_watch flow review, final dispositions, and autonomous-action approval"
  local suffix verdict name query create watch_id flow_hash preview event_query event_file event_found delivery_clause
  local action_create action_watch_id action_hash action_review
  local digest_watch_id="" digest_member_id="" digest_events digest_drained=""
  suffix="$(date +%s)_$$"
  local watch_ids=()

  for verdict in notify digest discard; do
    name="smoke_flow_${verdict}_fixture_${suffix}"
    delivery_clause=""
    if [ "$verdict" = "digest" ]; then
      delivery_clause=', delivery_json: { kind: "inbox", digest: { window: "1m" } }'
    fi
    query="mutation { gj_watch(insert: { name: \"${name}\", query: \"subscription ${name} { roast_schedule(first: 25, after: \$cursor) { id status } roast_schedule_cursor }\"${delivery_clause}, enrich_json: { enabled: true, kind: \"flow\", flow: \"default_watch_triage\" } }) { id flow_hash flow_approval status approval enabled enrich_json delivery_json } }"
    create="$(flow_graphql "create-$verdict" "$query")"
    watch_id="$(jq -r '[.data.gj_watch] | flatten | .[0].id' "$create")"
    flow_hash="$(jq -r '[.data.gj_watch] | flatten | .[0].flow_hash' "$create")"
    jq -e '([.data.gj_watch] | flatten | .[0]) as $w | ($w.id | length) > 0 and ($w.flow_hash | length) > 0 and $w.flow_approval == "pending" and $w.status == "paused" and $w.approval == "pending" and $w.enabled == false' "$create" >/dev/null \
      || fail "new $verdict flow watch did not pause for preview"
    watch_ids+=("$watch_id")

    query="mutation { gj_watch(where: { id: { eq: \"${watch_id}\" } }, update: { flow_review_json: { decision: \"approve\", expected_flow_hash: \"${flow_hash}\", samples_json: [{ fixture: \"${verdict}_fixture\", temperature: 421 }] } }) { id flow_hash flow_approval flow_preview_json status approval enabled } }"
    preview="$(flow_graphql "preview-$verdict" "$query")"
    jq -e --arg verdict "$verdict" '
      ([.data.gj_watch] | flatten | .[0]) as $w
      | ($w.flow_preview_json | if type == "string" then fromjson else . end) as $p
      | $w.flow_approval == "approved" and $w.status == "active" and $w.enabled == true
        and $p.status == "ok" and $p.sample_count == 1
        and (if $verdict == "notify" then $p.notify_count == 1
             elif $verdict == "digest" then $p.digest_count == 1
             else $p.discard_count == 1 end)
    ' "$preview" >/dev/null || fail "$verdict flow preview returned the wrong disposition"

    event_query="query { gj_watch_event(where: { watch_id: { eq: \"${watch_id}\" } }, order_by: { created_at: desc }, limit: 1) { id watch_id delivery_status seen enrichment_json } }"
    event_found=""
    for _ in $(seq 1 80); do
      event_file="$(flow_graphql "event-${verdict}" "$event_query")"
      if jq -e --arg verdict "$verdict" '
        ([.data.gj_watch_event] | flatten | .[0]) as $e
        | ($e.enrichment_json | if type == "string" then fromjson else . end) as $enrichment
        | $e != null and $enrichment.verdict == $verdict
          and (if $verdict == "notify" then (["pending", "delivered"] | index($e.delivery_status)) != null and $e.seen == false
               elif $verdict == "digest" then $e.delivery_status == "digest_queued" and $e.seen == true
               else $e.delivery_status == "suppressed" and $e.seen == true end)
      ' "$event_file" >/dev/null; then
        event_found=1
        break
      fi
      sleep 0.5
    done
    [ -n "$event_found" ] || fail "$verdict flow event did not reach its final disposition"
    if [ "$verdict" = "digest" ]; then
      digest_watch_id="$watch_id"
      digest_member_id="$(jq -r '[.data.gj_watch_event] | flatten | .[0].id' "$event_file")"
    fi
  done

  event_query="query { gj_watch_event(where: { watch_id: { eq: \"${digest_watch_id}\" } }, order_by: { created_at: asc }, limit: 10) { id delivery_status seen data_json receipt_json enrichment_json } }"
  for _ in $(seq 1 200); do
    digest_events="$(flow_graphql "digest-drain" "$event_query")"
    if jq -e --arg member "$digest_member_id" '
      (.data.gj_watch_event // []) as $events
      | ([
          $events[]?
          | select(
              (.data_json | if type == "string" then fromjson else . end | .kind) == "digest"
            )
        ] | .[0]) as $digest
      | ([$events[]? | select(.id == $member)] | .[0]) as $member_event
      | $digest != null and $member_event != null
        and $digest.seen == false
        and (["pending", "delivered"] | index($digest.delivery_status)) != null
        and (
          ($digest.data_json | if type == "string" then fromjson else . end) as $data
          | $data.count >= 1 and ($data.event_ids | index($member)) != null
        )
        and $member_event.delivery_status == "digested"
        and (
          ($member_event.receipt_json | if type == "string" then fromjson else . end).digest_event_id
          == $digest.id
        )
    ' "$digest_events" >/dev/null; then
      digest_drained=1
      break
    fi
    sleep 0.5
  done
  [ -n "$digest_drained" ] || fail "digest queue did not drain into a synthetic unseen digest within 100s"
  pass "digest queue drained into one synthetic unseen digest and linked its member receipt"

  name="smoke_action_review_${suffix}"
  query="mutation { gj_watch(insert: { name: \"${name}\", query: \"subscription ${name} { roast_schedule(first: 25, after: \$cursor) { id status } roast_schedule_cursor }\", delivery_json: { kind: \"workflow\", name: \"customer_issue_triage\" } }) { id action_hash action_approval status approval enabled } }"
  action_create="$(flow_graphql "create-action" "$query")"
  action_watch_id="$(jq -r '[.data.gj_watch] | flatten | .[0].id' "$action_create")"
  action_hash="$(jq -r '[.data.gj_watch] | flatten | .[0].action_hash' "$action_create")"
  jq -e '([.data.gj_watch] | flatten | .[0]) as $w | ($w.action_hash | length) > 0 and $w.action_approval == "pending" and $w.status == "paused" and $w.enabled == false' "$action_create" >/dev/null \
    || fail "workflow delivery watch did not pause for action review"
  watch_ids+=("$action_watch_id")

  query="mutation { gj_watch(where: { id: { eq: \"${action_watch_id}\" } }, update: { action_review_json: { decision: \"approve\", expected_action_hash: \"${action_hash}\" } }) { id action_hash action_approval status approval enabled } }"
  action_review="$(flow_graphql "approve-action" "$query")"
  jq -e --arg hash "$action_hash" '
    ([.data.gj_watch] | flatten | .[0]) as $w
    | $w.action_hash == $hash and $w.action_approval == "approved"
      and $w.approval == "approved" and $w.status == "active" and $w.enabled == true
  ' "$action_review" >/dev/null || fail "confirmed action hash did not activate workflow delivery"

  for watch_id in "${watch_ids[@]}"; do
    flow_graphql "cleanup-$(printf '%s' "$watch_id" | tr ':/' '__')" "mutation { gj_watch(delete: true, where: { id: { eq: \"${watch_id}\" } }) { id } }" >/dev/null
  done
  pass "gj_watch reviewed flows and action delivery, routed notify/digest/discard, and drained the digest queue"
}

log "capturing lexical-only discovery baseline"
start_graphjin false lexical
capture_suite baseline
stop_graphjin

log "capturing semantic discovery results"
start_graphjin true semantic-cold
wait_for_semantic_index
capture_suite semantic

CASE_NAMES=(clients purchases inventory quality)
CASE_QUERIES=("clients" "purchases" "raw coffee inventory" "quality failures from recent roasting")
CASE_TARGETS=(customers production_orders green_lots qc_cupping_scores)
CASE_ROWS="$TMP_DIR/cases.jsonl"
: >"$CASE_ROWS"
IMPROVED=0

printf '\n%-38s %-22s %-10s %-10s\n' "query" "target" "lexical" "semantic"
printf '%-38s %-22s %-10s %-10s\n' "-----" "------" "-------" "--------"
for index in "${!CASE_NAMES[@]}"; do
  name="${CASE_NAMES[$index]}"
  query="${CASE_QUERIES[$index]}"
  target="${CASE_TARGETS[$index]}"
  baseline_file="$TMP_DIR/baseline-$name.json"
  semantic_file="$TMP_DIR/semantic-$name.json"
  baseline_rank="$(table_rank "$baseline_file" "$target")"
  semantic_rank="$(table_rank "$semantic_file" "$target")"
  reason="$(table_reason "$semantic_file" "$target")"
  if [ "$semantic_rank" = "null" ]; then
    fail "semantic query '$query' did not discover $target"
  fi
  if [[ "$reason" != *semantic* ]]; then
    fail "semantic query '$query' returned $target without a semantic explanation: $reason"
  fi
  if [ "$baseline_rank" = "null" ] || [ "$semantic_rank" -lt "$baseline_rank" ]; then
    IMPROVED=$((IMPROVED + 1))
  fi
  printf '%-38s %-22s %-10s %-10s\n' "$query" "$target" "$baseline_rank" "$semantic_rank"
  jq -nc \
    --arg query "$query" \
    --arg target "$target" \
    --argjson baseline_rank "$baseline_rank" \
    --argjson semantic_rank "$semantic_rank" \
    --arg reason "$reason" \
    '{query:$query,target:$target,lexical_rank:$baseline_rank,semantic_rank:$semantic_rank,reason:$reason}' >>"$CASE_ROWS"
done
if [ "$IMPROVED" -lt 3 ]; then
  fail "semantic search improved only $IMPROVED of 4 synonym/business-language discovery cases"
fi
pass "semantic search improved $IMPROVED of 4 business-language discovery cases"

relationship_file="$TMP_DIR/semantic-relationship.json"
if [ "$(table_rank "$relationship_file" customers)" = "null" ] || [ "$(table_rank "$relationship_file" production_orders)" = "null" ]; then
  fail "relationship intent did not return both customers and production_orders"
fi
if ! jq -e '
  .result.structuredContent as $result |
  [$result.cards[]? | select(.kind == "relationship") | .id as $id |
    $result.matches[$id].why // "" | select(contains("relationship path"))] | length > 0
' "$relationship_file" >/dev/null; then
  fail "relationship intent did not include a deterministic foreign-key path"
fi
pass "relationship intent returned both endpoints and the real foreign-key path"

if [ "$MODE" = "fixture" ]; then
  run_annotation_semantic_smoke
fi

if [ "$RUN_AGENT" = true ]; then
  log "checking the semantic-aware REST agent"
  run_semantic_agent_smoke
  run_agent_state_notice_smoke
  run_annotation_agent_guard_smoke
fi

baseline_ids="$(jq -c '[.result.structuredContent.cards[]?.id]' "$TMP_DIR/baseline-unrelated.json")"
semantic_ids="$(jq -c '[.result.structuredContent.cards[]?.id]' "$TMP_DIR/semantic-unrelated.json")"
if [ "$baseline_ids" != "$semantic_ids" ]; then
  fail "unrelated query injected semantic candidates: lexical=$baseline_ids semantic=$semantic_ids"
fi
pass "unrelated query stayed on the lexical result set"

if [ "$MODE" = "fixture" ]; then
  curl -fsS "http://127.0.0.1:$EMBEDDING_PORT/stats" >"$TMP_DIR/stats-cold.json"
  jq -e '.inputs > 4 and .max_batch_size > 1 and .max_batch_size <= 64' "$TMP_DIR/stats-cold.json" >/dev/null || fail "cold semantic build did not use bounded Ax batches"
  pass "cold semantic build used bounded batches"

  log "checking the coffee watch first-wave service coverage batch"
  first_wave_tests='^(TestCoffeeRoasteryServiceRuntimeCoverageBatch|TestWatchRetryBackoffSchedule|TestWatchRetryMaxFailuresDefault|TestWatchRunnerTransientErrorKeepsStatusActiveThenTerminates|TestWatchSubscriptionSessionExitsOnMemberDone|TestWatchRunnerResubscribesAfterMemberDone|TestNormalizeWatchAbsenceJSON|TestWatchAbsenceFiresOnceRearmsAndHonorsDowntimeGrace|TestWatchAbsenceChangesActionHashOnlyWhenConfigured|TestNormalizeWatchDeliveryPreservesDigest|TestWatchDigestFlushCoalescesAndIsIdempotent|TestWatchEventSnoozeSetClearAndDoesNotMarkSeen|TestWatchFirstWaveCapabilityRows|TestWatchedWatchIDsForRootsRequiresSafeConjunctiveFilter|TestValidateWatchDefinitionEnforcesWatchEventLoopFilter)$'
  (cd "$ROOT_DIR" && GOCACHE="${GOCACHE:-/tmp/go-build}" go test ./serv -run "$first_wave_tests" -count=1) || fail "coffee watch first-wave service coverage batch failed"
  pass "coffee smoke covered reconnect/retry, absence, digest drain, snooze, and safe rollup validation"

  run_watch_flow_smoke
fi

stop_graphjin

if [ "$MODE" = "fixture" ]; then
  curl -fsS "http://127.0.0.1:$EMBEDDING_PORT/stats" >"$TMP_DIR/stats-before-warm.json"
  start_graphjin true semantic-warm
  sleep 3
  curl -fsS "http://127.0.0.1:$EMBEDDING_PORT/stats" >"$TMP_DIR/stats-after-warm.json"
  if ! jq -e --slurpfile before "$TMP_DIR/stats-before-warm.json" '
    .requests == $before[0].requests and .inputs == $before[0].inputs
  ' "$TMP_DIR/stats-after-warm.json" >/dev/null; then
    fail "warm startup made embedding calls: before=$(jq -c . "$TMP_DIR/stats-before-warm.json") after=$(jq -c . "$TMP_DIR/stats-after-warm.json")"
  fi
  pass "warm startup made zero embedding calls"

  requests_before_exact="$(jq -r '.requests' "$TMP_DIR/stats-after-warm.json")"
  mcp_query_catalog "$(catalog_args 'production_orders')" "$TMP_DIR/exact.json"
  if [ "$(table_rank "$TMP_DIR/exact.json" production_orders)" != "1" ]; then
    fail "exact identifier was not preserved as top-one"
  fi
  requests_after_exact="$(curl -fsS "http://127.0.0.1:$EMBEDDING_PORT/stats" | jq -r '.requests')"
  if [ "$requests_before_exact" != "$requests_after_exact" ]; then
    fail "exact identifier unnecessarily called the embedding endpoint"
  fi
  pass "exact identifier stayed top-one without an embedding call"
  stop_graphjin
fi

if [ -z "$REPORT_PATH" ]; then
  REPORT_PATH="$TMP_DIR/semantic-smoke-report.json"
  KEEP_REPORT=false
else
  KEEP_REPORT=true
fi
jq -s \
  --arg mode "$MODE" \
  --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --argjson improved "$IMPROVED" \
  '{mode:$mode,generated_at:$generated_at,improved_cases:$improved,cases:.}' \
  "$CASE_ROWS" >"$REPORT_PATH"

printf '\n'
jq . "$REPORT_PATH"
if [ "$KEEP_REPORT" = true ]; then
  pass "report written to $REPORT_PATH"
fi
log "coffee roastery semantic discovery smoke passed"
