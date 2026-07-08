#!/usr/bin/env bash
set -euo pipefail

DEMO_NAME="pcb fab"
BASE_URL="${GRAPHJIN_URL:-http://localhost:8082}"
SMOKE_LOCKED_KIND="${SMOKE_LOCKED_KIND:-runbook}"
SMOKE_JWT_SECRET="${SMOKE_JWT_SECRET:-pcb-fab-demo-jwt-secret}"
USER_ROLE="${GRAPHJIN_SMOKE_USER_ROLE:-fab_planner}"
SMOKE_USAGE_TEXT='PCB fab GraphJin smoke suite.

Run after starting the demo server:
  graphjin serve --demo --path examples/pcb-fab

Usage:
  examples/pcb-fab/scripts/smoke.sh [--url URL] [--agent|--no-agent|--agent-eval] [--sampling]

Options:
  --url URL     GraphJin base URL. Defaults to GRAPHJIN_URL or http://localhost:8082.
  --agent       Require REST and MCP agent checks.
  --agent-eval  Run stricter open-ended agent protocol evals against REST.
  --sampling    Run the MCP sampling require-mode checks.
  --no-agent    Skip REST and MCP agent checks.'

MOCK_PID=""

smoke_extra_cleanup() {
  if [ -n "$MOCK_PID" ]; then
    kill "$MOCK_PID" >/dev/null 2>&1 || true
    wait "$MOCK_PID" >/dev/null 2>&1 || true
  fi
}

start_supplier_mock() {
  if curl -sS --max-time 2 http://localhost:8093/health >/dev/null 2>&1; then
    return
  fi
  go run ./examples/pcb-fab/mockapi >/tmp/pcb-fab-mockapi.log 2>&1 &
  MOCK_PID="$!"
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    if curl -sS --max-time 2 http://localhost:8093/health >/dev/null 2>&1; then
      return
    fi
    sleep 1
  done
  echo "supplier mock API did not become ready" >&2
  sed -n '1,120p' /tmp/pcb-fab-mockapi.log >&2 || true
  exit 1
}

# shellcheck source=../../lib/smoke-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/../../lib" && pwd)/smoke-common.sh"
smoke_parse_args "$@"
start_supplier_mock

# --- domain agent runners (scripted release-readiness discovery flow) ----------

run_agent_rest_once() {
  local out="$TMP_DIR/agent-rest-$(date +%s%N).json"
  local payload
  payload="$(jq -n '{
    instruction:"Use the JavaScript runtime globals to run this exact discovery flow: const catalog = await query_catalog({kind:\"saved_query\"}); const detail = await query_catalog({id:\"saved_query:fab_order_context\"}); const result = await execute_saved_query({name:\"fab_order_context\"}); const order = result.data.fab_orders.find(o => o.priority === 1); await final({status:\"answered\", answer:`${order.order_code} for ${order.customers.name} is the highest priority fab order and is still ${order.status}.`, data:{order}, evidence:{saved_query:\"fab_order_context\", catalog_cards:catalog.cards, saved_query_detail:detail.cards}});",
    max_steps:10,
    return_trace:false
  }')"
  post_json "${BASE_URL%/}/api/v1/agent" "$payload" "$out"
  printf '%s\n' "$out"
}

run_agent_mcp_once() {
  mcp_tool ask_graphjin_agent '{
    "instruction":"Use the JavaScript runtime globals to run this exact discovery flow: const catalog = await query_catalog({kind:\"saved_query\"}); const detail = await query_catalog({id:\"saved_query:fab_order_context\"}); const result = await execute_saved_query({name:\"fab_order_context\"}); const order = result.data.fab_orders.find(o => o.priority === 1); await final({status:\"answered\", answer:`${order.order_code} for ${order.customers.name} is the highest priority fab order and is still ${order.status}.`, data:{order}, evidence:{saved_query:\"fab_order_context\", catalog_cards:catalog.cards, saved_query_detail:detail.cards}});",
    "max_steps":10,
    "return_trace":false
  }'
}

pcb_agent_expr='
  (.status // .result.structuredContent.status) == "answered"
  and ((.answer // .result.structuredContent.answer // "") | test("FO-260701|Lumen"; "i"))
  and ((.actions // .result.structuredContent.actions // []) | tostring | test("query_catalog"))
  and ((.evidence // .result.structuredContent.evidence // {}) | tostring | test("fab_order_context"))
'

# --- domain agent eval suite ---------------------------------------------------

run_agent_eval_suite() {
  local out

  log "checking open-ended agent discovery protocol evals"

  out="$(run_agent_rest_once)"
  assert_jq "$out" '
    .status == "answered"
    and (.answer | test("FO-260701|Lumen"; "i"))
    and (.actions | tostring | test("query_catalog"))
    and (.evidence | tostring | test("fab_order_context"))
  ' "agent eval: fab priority answered from saved-query evidence"

  out="$(run_agent_rest_prompt "Inventory the approved saved queries, workflows, code sources, API surfaces, and measurement data in this PCB demo. Do discovery only; do not execute anything.")"
  assert_jq "$out" '
    (.status == "answered" or .status == "blocked")
    and (.actions | tostring | test("query_catalog|graphql_help"))
    and ((.actions | tostring | test("execute_saved_query|execute_graphql")) | not)
  ' "agent eval: discovery-only inventory avoided execution"

  out="$(run_agent_rest_prompt "Use the JavaScript runtime globals to run this exact discovery flow: const yieldDetail = await query_catalog({id:\"saved_query:yield_context\"}); const dfmDetail = await query_catalog({id:\"saved_query:dfm_review_context\"}); const yieldResult = await execute_saved_query({name:\"yield_context\"}); const dfmResult = await execute_saved_query({name:\"dfm_review_context\"}); const weak = yieldResult.data.yield_by_layer_count.find(r => Number(r.layer_count) === 8 && Number(r.yield_pct) < 88); const topDefect = yieldResult.data.defect_pareto[0]; const risk = dfmResult.data.bom_items.find(i => i.sku === \"IC-AFE-8CH-MED\"); await final({status:\"answered\", answer:\`8-layer yield is \${weak.yield_pct}% with \${topDefect.station} as the top defect station; \${risk.sku} is \${risk.supplier_part.lifecycle} with \${risk.supplier_part.lead_time_days} day lead time.\`, data:{weak,topDefect,risk}, evidence:{saved_queries:[\"yield_context\",\"dfm_review_context\"], yield_detail:yieldDetail.cards, dfm_detail:dfmDetail.cards}});")"
  assert_jq "$out" '
    .status == "answered"
    and (.actions | tostring | test("query_catalog|execute_saved_query"))
    and (.answer | test("8|IC-AFE|allocation|plating"; "i"))
  ' "agent eval: yield and supplier risk discovered through approved surfaces"

  run_refusal_suite fab_orders
  run_watch_agent_suite \
    "Use the JavaScript runtime globals to run this exact watch creation flow: await query_catalog({id:\"help:security\"}); await query_catalog({id:\"help:runtime\"}); await query_catalog({id:\"help:watches\"}); const created = await execute_graphql({query:\"mutation { gj_watch(insert: { name: \\\"smoke_agent_watch\\\", query: \\\"subscription smoke_watch { fab_orders(first: 25, after: \\\$cursor) { id status } fab_orders_cursor }\\\" }) { id name status enabled } }\"}); await final({status:\"answered\", answer:\"created smoke_agent_watch\", data:created, evidence:{catalog_ids:[\"help:security\",\"help:runtime\",\"help:watches\"]}});" \
    smoke_agent_watch \
    "subscription smoke_notice { ncr_reports(first: 25, after: \$cursor) { id status } ncr_reports_cursor }"
  run_admin_root_suite
}

# --- base checks ---------------------------------------------------------------

log "checking GraphQL across mes, yield_warehouse, MongoDB, files, API, dfm_code, and graphjin"
multi_out="$(graphql multi-source 'query {
  customers(where: { name: { eq: "Lumen Medical Devices" } }, limit: 1) { id name priority_tier }
  fab_orders(where: { order_code: { eq: "FO-260701-ALPHA" } }, limit: 1) { id order_code priority status }
  yield_by_layer_count(where: { layer_count: { eq: 8 } }, order_by: { week_start: desc }, limit: 1) { id layer_count yield_pct }
  test_runs(where: { run_code: { eq: "TR-ALPHA-ICT-1" } }, limit: 1) { id order_id station status }
  test_measurements(where: { status: { eq: "fail" } }, limit: 5) { id run_id measurement status }
  design_files(where: { key: { ilike: "rev_b/%" } }, limit: 5) { key size }
  bom_items(where: { sku: { eq: "IC-AFE-8CH-MED" } }, limit: 1) {
    sku
    supplier_part {
      sku
      lifecycle
      inventory_qty
      lead_time_days
    }
  }
  gj_code(where: { name: { eq: "calculateImpedance" } }, limit: 5) {
    path
    code_context
  }
  gj_catalog(where: { kind: { eq: "saved_query" } }, limit: 5) { name kind }
}')"
assert_jq "$multi_out" '.data.customers[0].name == "Lumen Medical Devices"' "mes source is queryable"
assert_jq "$multi_out" '(.data.yield_by_layer_count[0].yield_pct | tonumber? // .data.yield_by_layer_count[0].yield_pct) < 88' "yield_warehouse source is queryable"
assert_jq "$multi_out" '.data.test_runs[0].status == "fail" and (.data.test_measurements | length) >= 2' "test_measurements MongoDB source is queryable"
assert_jq "$multi_out" '(.data.design_files | length) >= 2' "design_files source is queryable"
assert_jq "$multi_out" '.data.bom_items[0].supplier_part.lifecycle == "allocation"' "supplier_api OpenAPI join is queryable"
assert_jq "$multi_out" '(.data.gj_code | tostring | test("calculateImpedance|impedance_rules"))' "dfm_code source is queryable"
assert_jq "$multi_out" '[.data.gj_catalog[].name] | index("fab_order_context") != null' "graphjin catalog source is queryable"

log "checking REST saved queries"
order_out="$(rest_saved_query fab_order_context)"
assert_jq "$order_out" '[.data.fab_orders[] | select(.order_code == "FO-260701-ALPHA" and .priority == 1)] | length == 1' "fab_order_context includes the priority order"

yield_out="$(rest_saved_query yield_context)"
assert_jq "$yield_out" '[.data.yield_by_layer_count[] | select(.layer_count == 8 and ((.yield_pct | tonumber? // .yield_pct) < 88))] | length >= 1' "yield_context includes weak 8-layer yield"

dfm_out="$(rest_saved_query dfm_review_context)"
assert_jq "$dfm_out" '[.data.bom_items[] | select(.sku == "IC-AFE-8CH-MED" and .supplier_part.lifecycle == "allocation")] | length == 1' "dfm_review_context includes supplier API risk"

log "checking workflow execution"
dfm_workflow_out="$(graphql workflow-dfm 'mutation {
  gj_workflow_execution(insert: {
    workflow_name: "dfm_gate_check"
    variables: {
      order: { order_code: "FO-260701-ALPHA", status: "engineering_hold" }
      design: { target_impedance_ohms: 50 }
      bom: [{ sku: "IC-AFE-8CH-MED", criticality: "critical", lead_time_days: 21 }]
      measurements: [{ measurement: "impedance_lane_3", status: "fail" }]
    }
  }) {
    workflow_name
    status
    result_json
    error
  }
}')"
assert_jq "$dfm_workflow_out" '.data.gj_workflow_execution.status == "ok" and (.data.gj_workflow_execution.result_json | contains("hold CAM release"))' "dfm_gate_check workflow executed"

yield_workflow_out="$(graphql workflow-yield 'mutation {
  gj_workflow_execution(insert: {
    workflow_name: "yield_alert_triage"
    variables: {
      yields: [{ layer_count: 8, yield_pct: 83.04 }]
      defects: [{ defect_code: "PLATING-VOID", station: "plating", defect_count: 38 }]
    }
  }) {
    workflow_name
    status
    result_json
    error
  }
}')"
assert_jq "$yield_workflow_out" '.data.gj_workflow_execution.status == "ok" and (.data.gj_workflow_execution.result_json | contains("plating"))' "yield_alert_triage workflow executed"

release_workflow_out="$(graphql workflow-release 'mutation {
  gj_workflow_execution(insert: {
    workflow_name: "order_release_check"
    variables: {
      orders: [
        { order_code: "FO-260701-ALPHA", status: "engineering_hold", priority: 1 },
        { order_code: "FO-260702-BETA", status: "released", priority: 2 }
      ]
      wip: [{ order_code: "FO-260701-ALPHA", blocker: "DFM signoff" }]
      ncrs: []
    }
  }) {
    workflow_name
    status
    result_json
    error
  }
}')"
assert_jq "$release_workflow_out" '.data.gj_workflow_execution.status == "ok" and (.data.gj_workflow_execution.result_json | contains("FO-260702-BETA"))' "order_release_check workflow executed"

run_watch_lifecycle_suite "subscription smoke_watch { fab_orders(first: 25, after: \$cursor) { id status } fab_orders_cursor }"
run_watch_fire_suite "subscription smoke_fire { ncr_reports(first: 25, after: \$cursor) { id status } ncr_reports_cursor }"
run_artifact_suite

log "checking MCP discovery surfaces"
catalog_out="$(mcp_tool query_catalog '{"kind":"saved_query","limit":10}')"
assert_jq "$catalog_out" '[.result.structuredContent.cards[].name] | index("fab_order_context") != null and index("yield_context") != null and index("dfm_review_context") != null' "MCP catalog exposes saved queries"

workflow_catalog_out="$(mcp_tool query_catalog '{"kind":"workflow","limit":10}')"
assert_jq "$workflow_catalog_out" '[.result.structuredContent.cards[].name] | index("dfm_gate_check") != null and index("yield_alert_triage") != null and index("order_release_check") != null' "MCP catalog exposes workflows"

code_catalog_out="$(mcp_tool query_catalog '{"where":{"database_name":{"eq":"dfm_code"}},"limit":10}')"
assert_jq "$code_catalog_out" '(.result.structuredContent.cards | tostring | test("dfm_code")) and (.result.structuredContent.cards | tostring | test("gj_code")) and (.result.structuredContent.cards | tostring | test("code_context|symbols"))' "MCP catalog exposes dfm_code code discovery"

api_catalog_out="$(mcp_tool query_catalog '{"id":"source:supplier_api"}')"
assert_jq "$api_catalog_out" '.result.structuredContent.cards[0].id == "source:supplier_api" and .result.structuredContent.cards[0].kind == "source" and .result.structuredContent.cards[0].source == "supplier_api"' "MCP catalog exposes supplier API context"

case "$RUN_AGENT" in
  never)
    log "skipping agent checks (--no-agent)"
    ;;
  auto)
    if agent_enabled; then
      log "checking REST and MCP agent endpoints"
      retry_agent_assert "REST agent answered from fab order evidence" run_agent_rest_once "$pcb_agent_expr"
      retry_agent_assert "MCP agent answered from fab order evidence" run_agent_mcp_once "$pcb_agent_expr"
    else
      log "skipping agent checks because /api/v1/agent is not enabled"
    fi
    ;;
  always)
    if ! agent_enabled; then
      fail "agent checks requested, but /api/v1/agent is not enabled"
    fi
    log "checking REST and MCP agent endpoints"
    retry_agent_assert "REST agent answered from fab order evidence" run_agent_rest_once "$pcb_agent_expr"
    retry_agent_assert "MCP agent answered from fab order evidence" run_agent_mcp_once "$pcb_agent_expr"
    ;;
  *)
    fail "GRAPHJIN_AGENT_SMOKE must be auto, always, or never"
    ;;
esac

case "$RUN_AGENT_EVAL" in
  never)
    ;;
  always)
    if ! agent_enabled; then
      fail "agent eval requested, but /api/v1/agent is not enabled"
    fi
    run_agent_eval_suite
    run_role_skill_suite "Add a new admin role to the GraphJin config."
    ;;
  *)
    fail "GRAPHJIN_AGENT_EVAL must be always or never"
    ;;
esac

if [ "$RUN_SAMPLING" = "always" ]; then
  run_sampling_require_suite
fi

log "pcb fab smoke suite passed"
