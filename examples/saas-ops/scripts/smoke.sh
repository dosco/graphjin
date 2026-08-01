#!/usr/bin/env bash
set -euo pipefail

DEMO_NAME="saas ops"
BASE_URL="${GRAPHJIN_URL:-http://localhost:8083}"
SMOKE_LOCKED_KIND="${SMOKE_LOCKED_KIND:-runbook}"
SMOKE_JWT_SECRET="${SMOKE_JWT_SECRET:-saas-ops-demo-jwt-secret}"
SMOKE_USAGE_TEXT='SaaS ops GraphJin smoke suite.

Run after starting the demo server:
  graphjin serve --demo --path examples/saas-ops

Usage:
  examples/saas-ops/scripts/smoke.sh [--url URL] [--agent|--no-agent|--agent-eval] [--missing-model-credentials]

Options:
  --url URL     GraphJin base URL. Defaults to GRAPHJIN_URL or http://localhost:8083.
  --agent       Require REST and MCP agent checks.
  --agent-eval  Run stricter open-ended agent protocol evals against REST.
  --missing-model-credentials  Check fail-closed MCP and REST behavior.
  --no-agent    Skip REST and MCP agent checks.'

# shellcheck source=../../lib/smoke-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/../../lib" && pwd)/smoke-common.sh"
smoke_parse_args "$@"

# --- domain agent runners (scripted churn-risk discovery flow) -----------------

run_agent_rest_once() {
  local out="$TMP_DIR/agent-rest-$(date +%s%N).json"
  local payload
  payload="$(jq -n '{
    instruction:"Use the JavaScript runtime globals to run this exact discovery flow: const catalog = await query_catalog({kind:\"saved_query\"}); const detail = await query_catalog({id:\"saved_query:churn_risk_context\"}); const result = await execute_saved_query({name:\"churn_risk_context\"}); const account = result.data.accounts.find(a => a.status === \"churn_risk\"); await final({status:\"answered\", answer:`${account.name} is the top churn risk, renewing on ${account.renewal_date}.`, data:{account}, evidence:{saved_query:\"churn_risk_context\", catalog_cards:catalog.cards, saved_query_detail:detail.cards}});",
    max_steps:10,
    return_trace:false
  }')"
  post_json "${BASE_URL%/}/api/v1/agent" "$payload" "$out"
  printf '%s\n' "$out"
}

run_agent_mcp_once() {
  mcp_tool ask_graphjin_agent '{
    "instruction":"Use the JavaScript runtime globals to run this exact discovery flow: const catalog = await query_catalog({kind:\"saved_query\"}); const detail = await query_catalog({id:\"saved_query:churn_risk_context\"}); const result = await execute_saved_query({name:\"churn_risk_context\"}); const account = result.data.accounts.find(a => a.status === \"churn_risk\"); await final({status:\"answered\", answer:`${account.name} is the top churn risk, renewing on ${account.renewal_date}.`, data:{account}, evidence:{saved_query:\"churn_risk_context\", catalog_cards:catalog.cards, saved_query_detail:detail.cards}});",
    "max_steps":10,
    "return_trace":false
  }'
}

saas_agent_expr='
  (.status // .result.structuredContent.status) == "answered"
  and ((.answer // .result.structuredContent.answer // "") | test("Meridian"; "i"))
  and ((.actions // .result.structuredContent.actions // []) | tostring | test("query_catalog"))
  and ((.evidence // .result.structuredContent.evidence // {}) | tostring | test("churn_risk_context"))
'

# --- domain agent eval suite ---------------------------------------------------

run_agent_eval_suite() {
  local out

  log "checking open-ended agent discovery protocol evals"

  out="$(run_agent_rest_prompt "Use the JavaScript runtime globals to run this exact discovery flow: const catalog = await query_catalog({kind:\"saved_query\"}); const detail = await query_catalog({id:\"saved_query:churn_risk_context\"}); const result = await execute_saved_query({name:\"churn_risk_context\"}); const account = result.data.accounts.find(a => a.status === \"churn_risk\"); await final({status:\"answered\", answer:account.name + \" is the top churn risk, renewing on \" + account.renewal_date + \".\", data:{account}, evidence:{saved_query:\"churn_risk_context\", catalog_cards:catalog.cards, saved_query_detail:detail.cards}});")"
  assert_jq "$out" '
    .status == "answered"
    and (.answer | test("Meridian|churn"; "i"))
    and (.actions | tostring | test("query_catalog"))
    and (.actions | tostring | test("execute_saved_query"))
    and (.evidence | tostring | test("churn_risk_context"))
  ' "agent eval: churn risk answered from saved-query evidence"

  out="$(run_agent_rest_prompt "Inventory the approved saved queries and workflows this SaaS ops demo exposes. Do discovery only; do not execute anything.")"
  assert_jq "$out" '
    (.status == "answered" or .status == "blocked")
    and (.actions | tostring | test("query_catalog|graphql_help"))
    and ((.actions | tostring | test("execute_saved_query|execute_graphql")) | not)
  ' "agent eval: discovery-only inventory avoided execution"

  # Generic capability suites (shared lib).
  run_refusal_suite support_tickets
  run_watch_agent_suite \
    "Use the JavaScript runtime globals to run this exact watch creation flow: await query_catalog({id:\"help:security\"}); await query_catalog({id:\"help:runtime\"}); await query_catalog({id:\"help:watches\"}); const created = await execute_graphql({query:\"mutation { gj_watch(insert: { name: \\\"smoke_agent_watch\\\", query: \\\"subscription smoke_watch { support_tickets(first: 25, after: \\\$cursor) { id status } support_tickets_cursor }\\\" }) { id name status enabled } }\"}); await final({status:\"answered\", answer:\"created smoke_agent_watch\", data:created, evidence:{catalog_ids:[\"help:security\",\"help:runtime\",\"help:watches\"]}});" \
    smoke_agent_watch \
    "subscription smoke_notice { support_tickets(first: 25, after: \$cursor) { id status } support_tickets_cursor }"
  run_admin_root_suite
}

# --- base checks ---------------------------------------------------------------

log "checking GraphQL across app and graphjin sources"
multi_out="$(graphql multi-source 'query {
  accounts(where: { name: { eq: "Meridian Robotics" } }, limit: 1) { id name status }
  subscriptions(order_by: { id: asc }, limit: 1) { id plan status }
  gj_catalog(where: { kind: { eq: "saved_query" } }, limit: 5) { name kind }
}')"
assert_jq "$multi_out" '.data.accounts[0].name == "Meridian Robotics"' "app source is queryable"
assert_jq "$multi_out" '[.data.gj_catalog[].name] | index("churn_risk_context") != null' "graphjin catalog source is queryable"

log "checking REST saved queries"
churn_out="$(rest_saved_query churn_risk_context)"
assert_jq "$churn_out" '(.data.accounts | length) > 0 and (.data.invoices | length) > 0 and (.data.usage_events | length) > 0' "churn_risk_context spans churn signals"

mrr_out="$(rest_saved_query mrr_summary_context)"
assert_jq "$mrr_out" '(.data.subscriptions | length) > 0 and (.data.accounts | length) > 0' "mrr_summary_context spans revenue data"

sla_out="$(rest_saved_query ticket_sla_context)"
assert_jq "$sla_out" '[.data.support_tickets[] | select(.severity == "urgent")] | length > 0' "ticket_sla_context includes the urgent breached ticket"

log "checking workflow execution"
sla_wf_out="$(graphql workflow-sla 'mutation {
  gj_workflow_execution(insert: {
    workflow_name: "sla_breach_check"
    variables: {
      tickets: [
        { id: 1, severity: "urgent", status: "open", sla_due_at: "2020-01-01T00:00:00Z" },
        { id: 2, severity: "normal", status: "resolved", sla_due_at: "2020-01-02T00:00:00Z" }
      ]
      reference_time: "2030-01-01T00:00:00Z"
    }
  }) {
    workflow_name
    status
    result_json
    error
  }
}')"
assert_jq "$sla_wf_out" '.data.gj_workflow_execution.status == "ok" and (.data.gj_workflow_execution.result_json | contains("breach"))' "sla_breach_check workflow flags a breached ticket"

dunning_out="$(graphql workflow-dunning 'mutation {
  gj_workflow_execution(insert: {
    workflow_name: "dunning_retry_check"
    variables: {
      invoices: [
        { id: 1, status: "failed", attempts: 1 },
        { id: 2, status: "paid", attempts: 1 }
      ]
    }
  }) {
    workflow_name
    status
    result_json
    error
  }
}')"
assert_jq "$dunning_out" '.data.gj_workflow_execution.status == "ok" and (.data.gj_workflow_execution.result_json | contains("retry"))' "dunning_retry_check workflow recommends a retry"

run_watch_lifecycle_suite "subscription smoke_watch { support_tickets(first: 25, after: \$cursor) { id status } support_tickets_cursor }"
run_watch_fire_suite "subscription smoke_fire { support_tickets(first: 25, after: \$cursor) { id status } support_tickets_cursor }"
run_artifact_suite

log "checking MCP discovery surfaces"
catalog_out="$(mcp_tool query_catalog '{"kind":"saved_query","limit":10}')"
assert_jq "$catalog_out" '[.result.structuredContent.cards[].name] | index("churn_risk_context") != null and index("mrr_summary_context") != null and index("ticket_sla_context") != null' "MCP catalog exposes saved queries"

case "$RUN_AGENT" in
  never)
    log "skipping agent checks (--no-agent)"
    ;;
  auto)
    if agent_enabled; then
      log "checking REST and MCP agent endpoints"
      retry_agent_assert "REST agent answered from churn evidence" run_agent_rest_once "$saas_agent_expr"
      retry_agent_assert "MCP agent answered from churn evidence" run_agent_mcp_once "$saas_agent_expr"
    else
      log "skipping agent checks because /api/v1/agent is not enabled"
    fi
    ;;
  always)
    if ! agent_enabled; then
      fail "agent checks requested, but /api/v1/agent is not enabled"
    fi
    log "checking REST and MCP agent endpoints"
    retry_agent_assert "REST agent answered from churn evidence" run_agent_rest_once "$saas_agent_expr"
    retry_agent_assert "MCP agent answered from churn evidence" run_agent_mcp_once "$saas_agent_expr"
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

if [ "$RUN_MODEL_CREDENTIALS" = "always" ]; then
  run_model_credentials_suite
fi

log "saas ops smoke suite passed"
