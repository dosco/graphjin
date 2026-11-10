#!/usr/bin/env bash
# Routine evaluation: how well an agent routine (a watch whose delivery is an
# agent turn) acts on its own. Each case inserts a support ticket, lets a
# routine watch fire on it, and checks what the routine did:
#
#   act cases       the routine should propose one specific change; after it
#                   is approved, the database must show the effect and a
#                   continuation turn must follow up.
#   no_action cases a benign ticket; the right answer is to propose nothing.
#
# Metrics (majority of repeats per case):
#   verdict_recall      act cases where the routine proposed a change
#   false_alarm_rate    no_action cases where it proposed one anyway
#   proposal_precision  approved proposals whose change touched exactly the
#                       right ticket: every ticket's state is read just before
#                       and just after approval, and only that one may differ
#   effect_correct      act cases whose approved change left the right state
#   continuation_rate   approved changes followed by a continuation turn
#
# It sends real model traffic through the demo's configured provider. Run it
# against a live demo with write approvals on (the agentic default):
#
#   graphjin serve --demo --path examples/saas-ops
#   examples/saas-ops/scripts/routine-eval.sh --repeats 3 --report /tmp/routine.json
set -euo pipefail

REPEATS=3
REPORT=""
ARGS=()
while [ $# -gt 0 ]; do
  case "$1" in
    --repeats) REPEATS="$2"; shift 2 ;;
    --report) REPORT="$2"; shift 2 ;;
    *) ARGS+=("$1"); shift ;;
  esac
done

DEMO_NAME="saas ops routine eval"
BASE_URL="${GRAPHJIN_URL:-http://localhost:8083}"
SMOKE_JWT_SECRET="${SMOKE_JWT_SECRET:-saas-ops-demo-jwt-secret}"
SMOKE_USAGE_TEXT='Routine evaluation against a live saas-ops demo.

Usage:
  examples/saas-ops/scripts/routine-eval.sh [--url URL] [--repeats N] [--report FILE]'
# shellcheck source=../../lib/smoke-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/../../lib" && pwd)/smoke-common.sh"
smoke_parse_args "${ARGS[@]+"${ARGS[@]}"}"

# id|expect|subject|severity|expected status after approval
CASES=(
  "duplicate_close|act|DUPLICATE: invoice email sent twice|low|resolved"
  "duplicate_close_urgent|act|DUPLICATE: dashboard will not load|urgent|resolved"
  "real_issue|no_action|Dashboard will not load after login|urgent|"
  "near_miss_subject|no_action|Is this a duplicate charge on my invoice?|normal|"
)

# Every ticket's state, to see which ones an approved change touched.
TICKET_STATE_QUERY='query { support_tickets(order_by: { id: asc }, limit: 1000) { id subject severity status resolution_note resolved_at } }'

results="$TMP_DIR/routine-results.jsonl"
: >"$results"
for line in "${CASES[@]}"; do
  IFS='|' read -r id expect subject severity expected_status <<<"$line"
  for repeat in $(seq 1 "$REPEATS"); do
    log "routine case ${id} (run ${repeat}/${REPEATS})"
    routine_ticket_case "$ROUTINE_DUPLICATE_PROMPT" "${subject} [smoke $(date +%s)${RANDOM}]" "$severity" || true
    verdict="$(cat "$ROUTINE_VERDICT")"
    proposed="$(jq -r '.approval != null and .approval.status == "pending"' <<<"$verdict")"
    precise=null effect=false continued=false
    if [ "$proposed" = "true" ]; then
      ticket="$(jq -r '.ticket_id' <<<"$verdict")"
      if [ "$expect" = "act" ]; then
        # Precision is what the change did, not what it returned: a mutation
        # may return affected_rows or nothing that names the ticket.
        before="$(graphql_vars eval-before "$TICKET_STATE_QUERY" '{}')"
        approval_decide "$(jq -r '.approval.id' <<<"$verdict")" approve "$(jq -r '.approval.hash' <<<"$verdict")" "$TMP_DIR/eval-approve.json" >/dev/null
        after="$(graphql_vars eval-after "$TICKET_STATE_QUERY" '{}')"
        precise="$(jq -n --argjson t "$ticket" --slurpfile b "$before" --slurpfile a "$after" '
          ($b[0].data.support_tickets // []) as $before | ($a[0].data.support_tickets // []) as $after
          | ($before | map({key: (.id | tostring), value: .}) | from_entries) as $was
          | [$after[] | select(. != $was[.id | tostring]) | .id] as $changed
          | ($before | length) > 0 and ($before | length) == ($after | length) and $changed == [$t]
        ')"
        state="$(graphql_vars eval-state "query { support_tickets(where: { id: { eq: ${ticket} } }) { status } }" '{}')"
        effect="$(jq -r --arg s "$expected_status" '.data.support_tickets[0].status == $s' "$state")"
        messages="$(routine_wait_continuation "$(jq -r '.thread_id' <<<"$verdict")" "$(jq -r '.approval.id' <<<"$verdict")")"
        continued="$(jq -r --arg run "run_c_$(jq -r '.approval.id' <<<"$verdict")" '[.data.gj_thread_message[]? | select(.run_id == $run and .role == "assistant")] | length > 0' "$messages")"
      else
        approval_decide "$(jq -r '.approval.id' <<<"$verdict")" reject "$(jq -r '.approval.hash' <<<"$verdict")" "$TMP_DIR/eval-reject.json" >/dev/null
      fi
    fi
    # The routine's own reply says why a run proposed nothing: it judged no
    # change was needed (answered) or it failed (error, blocked).
    jq -nc --arg id "$id" --arg expect "$expect" --argjson repeat "$repeat" --arg delivery "$(jq -r '.delivery // "none"' <<<"$verdict")" \
      --argjson reply "$(jq -c '.reply // {}' <<<"$verdict")" \
      --argjson proposed "$proposed" --argjson precise "$precise" --argjson effect "$effect" --argjson continued "$continued" \
      '{case:$id, expect:$expect, repeat:$repeat, delivery:$delivery, reply_status:($reply.status // ""), reply:($reply.answer // ""), proposed:$proposed, precise:$precise, effect:$effect, continued:$continued}' >>"$results"
    routine_cleanup
  done
done

report="$(jq -s '
  def majority(f): (map(f) | map(select(.)) | length) * 2 > length;
  group_by(.case) as $cases
  | [ $cases[] | {case: .[0].case, expect: .[0].expect, runs: length,
        proposed: majority(.proposed),
        precise: (map(select(.proposed and .precise != null)) | if length == 0 then null else majority(.precise) end),
        effect: majority(.effect), continued: majority(.continued),
        consistent: ((map(.proposed) | unique | length) == 1)} ] as $verdicts
  | ($verdicts | map(select(.expect == "act"))) as $act
  | ($verdicts | map(select(.expect == "no_action"))) as $quiet
  | def rate(xs; f): if (xs | length) == 0 then null else ((xs | map(select(f)) | length) / (xs | length)) end;
  { generated_at: (now | todate),
    metrics: {
      verdict_recall: rate($act; .proposed),
      false_alarm_rate: rate($quiet; .proposed),
      proposal_precision: rate($verdicts | map(select(.proposed and .precise != null)); .precise),
      effect_correct: rate($act; .effect),
      continuation_rate: rate($act | map(select(.proposed)); .continued),
      consistency: rate($verdicts; .consistent)
    },
    reply_statuses: (map(.reply_status) | group_by(.) | map({(.[0]): length}) | add),
    cases: $verdicts,
    runs: . }
' "$results")"
if [ -n "$REPORT" ]; then
  printf '%s\n' "$report" >"$REPORT"
  log "wrote $REPORT"
fi
jq '.metrics' <<<"$report"
