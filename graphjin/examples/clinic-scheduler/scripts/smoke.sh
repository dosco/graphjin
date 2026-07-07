#!/usr/bin/env bash
set -euo pipefail

DEMO_NAME="clinic scheduler"
BASE_URL="${GRAPHJIN_URL:-http://localhost:8083}"
SMOKE_LOCKED_KIND="${SMOKE_LOCKED_KIND:-runbook}"
SMOKE_JWT_SECRET="${SMOKE_JWT_SECRET:-clinic-scheduler-demo-jwt-secret}"
SMOKE_USAGE_TEXT='Clinic scheduler GraphJin smoke suite.

Run after starting the demo server:
  graphjin serve --demo --path examples/clinic-scheduler

Usage:
  examples/clinic-scheduler/scripts/smoke.sh [--url URL] [--agent|--no-agent|--agent-eval] [--sampling]

Options:
  --url URL     GraphJin base URL. Defaults to GRAPHJIN_URL or http://localhost:8083.
  --agent       Require REST and MCP agent checks.
  --agent-eval  Run stricter open-ended agent protocol evals against REST.
  --sampling    Run the MCP sampling require-mode checks.
  --no-agent    Skip REST and MCP agent checks.'

# shellcheck source=../../lib/smoke-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/../../lib" && pwd)/smoke-common.sh"
smoke_parse_args "$@"

# --- domain agent runners (scripted waitlist discovery flow) -------------------

run_agent_rest_once() {
  local out="$TMP_DIR/agent-rest-$(date +%s%N).json"
  local payload
  payload="$(jq -n '{
    instruction:"Use the JavaScript runtime globals to run this exact discovery flow: const catalog = await query_catalog({kind:\"saved_query\"}); const detail = await query_catalog({id:\"saved_query:waitlist_context\"}); const result = await execute_saved_query({name:\"waitlist_context\"}); const entry = result.data.waitlist_entries.find(e => e.requested_specialty === \"cardiology\" && e.priority === \"high\"); const patient = result.data.patients.find(p => p.id === entry.patient_id); await final({status:\"answered\", answer:`${patient.name} tops the cardiology waitlist with ${entry.priority} priority.`, data:{entry, patient}, evidence:{saved_query:\"waitlist_context\", catalog_cards:catalog.cards, saved_query_detail:detail.cards}});",
    max_steps:10,
    return_trace:false
  }')"
  post_json "${BASE_URL%/}/api/v1/agent" "$payload" "$out"
  printf '%s\n' "$out"
}

run_agent_mcp_once() {
  mcp_tool ask_graphjin_agent '{
    "instruction":"Use the JavaScript runtime globals to run this exact discovery flow: const catalog = await query_catalog({kind:\"saved_query\"}); const detail = await query_catalog({id:\"saved_query:waitlist_context\"}); const result = await execute_saved_query({name:\"waitlist_context\"}); const entry = result.data.waitlist_entries.find(e => e.requested_specialty === \"cardiology\" && e.priority === \"high\"); const patient = result.data.patients.find(p => p.id === entry.patient_id); await final({status:\"answered\", answer:`${patient.name} tops the cardiology waitlist with ${entry.priority} priority.`, data:{entry, patient}, evidence:{saved_query:\"waitlist_context\", catalog_cards:catalog.cards, saved_query_detail:detail.cards}});",
    "max_steps":10,
    "return_trace":false
  }'
}

clinic_agent_expr='
  (.status // .result.structuredContent.status) == "answered"
  and ((.answer // .result.structuredContent.answer // "") | test("Rosa Alvarez"; "i"))
  and ((.actions // .result.structuredContent.actions // []) | tostring | test("query_catalog"))
  and ((.evidence // .result.structuredContent.evidence // {}) | tostring | test("waitlist_context"))
'

# --- domain agent eval suite ---------------------------------------------------

run_agent_eval_suite() {
  local out

  log "checking open-ended agent discovery protocol evals"

  out="$(run_agent_rest_prompt "Use the JavaScript runtime globals to run this exact discovery flow: const catalog = await query_catalog({kind:\"saved_query\"}); const detail = await query_catalog({id:\"saved_query:waitlist_context\"}); const result = await execute_saved_query({name:\"waitlist_context\"}); const entry = result.data.waitlist_entries.find(e => e.requested_specialty === \"cardiology\" && e.priority === \"high\"); const patient = result.data.patients.find(p => p.id === entry.patient_id); await final({status:\"answered\", answer:patient.name + \" tops the cardiology waitlist with \" + entry.priority + \" priority.\", data:{entry, patient}, evidence:{saved_query:\"waitlist_context\", catalog_cards:catalog.cards, saved_query_detail:detail.cards}});")"
  assert_jq "$out" '
    .status == "answered"
    and (.answer | test("Rosa|cardiology"; "i"))
    and (.actions | tostring | test("query_catalog"))
    and (.actions | tostring | test("execute_saved_query"))
    and (.evidence | tostring | test("waitlist_context"))
  ' "agent eval: waitlist priority answered from saved-query evidence"

  out="$(run_agent_rest_prompt "Inventory the approved saved queries and workflows this clinic demo exposes. Do discovery only; do not execute anything.")"
  assert_jq "$out" '
    (.status == "answered" or .status == "blocked")
    and (.actions | tostring | test("query_catalog|graphql_help"))
    and ((.actions | tostring | test("execute_saved_query|execute_graphql")) | not)
  ' "agent eval: discovery-only inventory avoided execution"

  # Generic capability suites (shared lib).
  run_watch_fire_suite "subscription smoke_fire { appointments { id status } }"
  run_refusal_suite appointments
  run_watch_agent_suite \
    "Use the JavaScript runtime globals to run this exact watch creation flow: await query_catalog({id:\"help:security\"}); await query_catalog({id:\"help:runtime\"}); await query_catalog({id:\"help:watches\"}); const created = await execute_graphql({query:\"mutation { gj_watch(insert: { name: \\\"smoke_agent_watch\\\", query: \\\"subscription smoke_watch { appointments { id status } }\\\" }) { id name status enabled } }\"}); await final({status:\"answered\", answer:\"created smoke_agent_watch\", data:created, evidence:{catalog_ids:[\"help:security\",\"help:runtime\",\"help:watches\"]}});" \
    smoke_agent_watch \
    "subscription smoke_notice { appointments { id status } }"
  run_admin_root_suite
}

# --- base checks ---------------------------------------------------------------

log "checking GraphQL across app and graphjin sources"
multi_out="$(graphql multi-source 'query {
  patients(where: { name: { eq: "Rosa Alvarez" } }, limit: 1) { id name }
  providers(order_by: { id: asc }, limit: 1) { id name specialty }
  gj_catalog(where: { kind: { eq: "saved_query" } }, limit: 5) { name kind }
}')"
assert_jq "$multi_out" '.data.patients[0].name == "Rosa Alvarez"' "app source is queryable"
assert_jq "$multi_out" '[.data.gj_catalog[].name] | index("waitlist_context") != null' "graphjin catalog source is queryable"

log "checking REST saved queries"
schedule_out="$(rest_saved_query daily_schedule_context)"
assert_jq "$schedule_out" '(.data.appointments | length) > 0 and (.data.providers | length) > 0 and (.data.rooms | length) > 0' "daily_schedule_context spans schedule data"

waitlist_out="$(rest_saved_query waitlist_context)"
assert_jq "$waitlist_out" '[.data.waitlist_entries[] | select(.requested_specialty == "cardiology" and .priority == "high")] | length > 0' "waitlist_context includes the high-priority cardiology entry"

utilization_out="$(rest_saved_query utilization_context)"
assert_jq "$utilization_out" '(.data.no_show_events | length) > 0 and (.data.rooms | length) > 0' "utilization_context includes no-show telemetry"

log "checking workflow execution"
overbook_out="$(graphql workflow-overbook 'mutation {
  gj_workflow_execution(insert: {
    workflow_name: "overbook_check"
    variables: {
      rooms: [{ id: 1 }, { id: 2 }]
      appointments: [
        { room_id: 1 }, { room_id: 1 }, { room_id: 1 }, { room_id: 1 }, { room_id: 1 },
        { room_id: 1 }, { room_id: 1 }, { room_id: 1 }, { room_id: 1 }, { room_id: 2 }
      ]
    }
  }) {
    workflow_name
    status
    result_json
    error
  }
}')"
assert_jq "$overbook_out" '.data.gj_workflow_execution.status == "ok" and (.data.gj_workflow_execution.result_json | contains("overbook"))' "overbook_check workflow flags an overbooked room"

promotion_out="$(graphql workflow-promotion 'mutation {
  gj_workflow_execution(insert: {
    workflow_name: "waitlist_promotion"
    variables: {
      waitlist: [{ status: "waiting", priority: "high" }]
      cancellations: [{ appointment_id: 5 }]
    }
  }) {
    workflow_name
    status
    result_json
    error
  }
}')"
assert_jq "$promotion_out" '.data.gj_workflow_execution.status == "ok" and (.data.gj_workflow_execution.result_json | contains("promote"))' "waitlist_promotion workflow recommends promotion"

run_watch_lifecycle_suite "subscription smoke_watch { appointments { id status } }"
run_artifact_suite

log "checking MCP discovery surfaces"
catalog_out="$(mcp_tool query_catalog '{"kind":"saved_query","limit":10}')"
assert_jq "$catalog_out" '[.result.structuredContent.cards[].name] | index("daily_schedule_context") != null and index("waitlist_context") != null and index("utilization_context") != null' "MCP catalog exposes saved queries"

case "$RUN_AGENT" in
  never)
    log "skipping agent checks (--no-agent)"
    ;;
  auto)
    if agent_enabled; then
      log "checking REST and MCP agent endpoints"
      retry_agent_assert "REST agent answered from waitlist evidence" run_agent_rest_once "$clinic_agent_expr"
      retry_agent_assert "MCP agent answered from waitlist evidence" run_agent_mcp_once "$clinic_agent_expr"
    else
      log "skipping agent checks because /api/v1/agent is not enabled"
    fi
    ;;
  always)
    if ! agent_enabled; then
      fail "agent checks requested, but /api/v1/agent is not enabled"
    fi
    log "checking REST and MCP agent endpoints"
    retry_agent_assert "REST agent answered from waitlist evidence" run_agent_rest_once "$clinic_agent_expr"
    retry_agent_assert "MCP agent answered from waitlist evidence" run_agent_mcp_once "$clinic_agent_expr"
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

log "clinic scheduler smoke suite passed"
