#!/usr/bin/env bash
# Routine evaluation: how well an agent routine (a watch whose delivery is an
# agent turn) acts on its own. Each case borrows a support ticket, lets a
# routine watch fire on it, and checks what the routine did:
#
#   act cases       the routine should propose one specific change; after it
#                   is approved, the database must show the effect and a
#                   continuation turn must follow up.
#   no_action cases a benign ticket; the right answer is to propose nothing.
#   follow_up cases nothing should change now; the routine should schedule a
#                   check-back in its thread instead.
#
# Two case sets (--cases base|extended|all, default all):
#
#   base      one ticket, one decision the prompt spells out in full.
#   extended  what a routine needs beyond that, named in each case's need:
#               skill       the change is spelled out only in a skill the
#                           watch's owner saved
#               memory      a detail of the change is a saved memory
#               multi_fact  the decision rests on two facts about the
#                           ticket's account, checked against the database
#                           before the case runs
#               follow_up   the routine must come back later on its own
#
# Metrics (majority of repeats per case, base cases only, so they compare
# with earlier reports):
#   verdict_recall      act cases where the routine proposed a change
#   false_alarm_rate    no_action cases where it proposed one anyway
#   proposal_precision  approved proposals whose change touched exactly the
#                       right ticket: every ticket's state is read just before
#                       and just after approval, and only that one may differ
#   effect_correct      act cases whose approved change left the right state
#   continuation_rate   approved changes followed by a continuation turn
#
# by_need counts runs, not majorities, for every need: a run passes when an
# act case held a precise change that left the expected state, a no_action
# case answered and proposed nothing, or a follow_up case scheduled a
# check-back and proposed nothing. held and right_effect are reported apart,
# so a change proposed without the owner's skill or memory shows as held with
# the wrong effect rather than as no action.
#
# Each run normally gets its own watch and so its own empty thread. A routine
# in use fires again and again in one thread, and every firing after the
# first carries the earlier ones as conversation. --thread shared gives each
# routine prompt one thread for the whole run, so the cases that share a
# prompt interleave in it, repeat by repeat, the way a standing routine's
# acting and leave-alone events do. A skill or memory case gets new wording
# every run, so its thread also holds earlier wording the routine must not
# reuse.
#
# It sends real model traffic through the demo's configured provider. Run it
# against a live demo with write approvals on (the agentic default):
#
#   graphjin serve --demo --path examples/saas-ops
#   examples/saas-ops/scripts/routine-eval.sh --repeats 3 --report /tmp/routine.json
set -euo pipefail

REPEATS=3
REPORT=""
CASE_SET="all"
THREAD_MODE="fresh"
ARGS=()
while [ $# -gt 0 ]; do
  case "$1" in
    --repeats) REPEATS="$2"; shift 2 ;;
    --report) REPORT="$2"; shift 2 ;;
    --cases) CASE_SET="$2"; shift 2 ;;
    --thread) THREAD_MODE="$2"; shift 2 ;;
    *) ARGS+=("$1"); shift ;;
  esac
done
case "$CASE_SET" in
  base|extended|all) ;;
  *) echo "--cases must be base, extended or all, got ${CASE_SET}" >&2; exit 2 ;;
esac
case "$THREAD_MODE" in
  fresh|shared) ;;
  *) echo "--thread must be fresh or shared, got ${THREAD_MODE}" >&2; exit 2 ;;
esac

DEMO_NAME="saas ops routine eval"
BASE_URL="${GRAPHJIN_URL:-http://localhost:8083}"
SMOKE_JWT_SECRET="${SMOKE_JWT_SECRET:-saas-ops-demo-jwt-secret}"
SMOKE_USAGE_TEXT='Routine evaluation against a live saas-ops demo.

Usage:
  examples/saas-ops/scripts/routine-eval.sh [--url URL] [--repeats N] [--cases base|extended|all] [--thread fresh|shared] [--report FILE]'
# shellcheck source=../../lib/smoke-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/../../lib" && pwd)/smoke-common.sh"
smoke_parse_args "${ARGS[@]+"${ARGS[@]}"}"

# id|need|expect|prompt|subject|severity|expected ticket state after approval
# @PHRASE@ is the wording the case's skill or memory carries, new every run.
CASES=(
  'duplicate_close|none|act|duplicate|DUPLICATE: invoice email sent twice|low|{"status":"resolved"}'
  'duplicate_close_urgent|none|act|duplicate|DUPLICATE: dashboard will not load|urgent|{"status":"resolved"}'
  'real_issue|none|no_action|duplicate|Dashboard will not load after login|urgent|'
  'near_miss_subject|none|no_action|duplicate|Is this a duplicate charge on my invoice?|normal|'
  'skill_note|skill|act|playbook|DUPLICATE: export job ran twice|normal|{"status":"resolved","resolution_note":"@PHRASE@"}'
  'skill_quiet|skill|no_action|playbook|Export job fails on large files|normal|'
  'memory_note|memory|act|memory|DUPLICATE: password reset email not arriving|normal|{"status":"resolved","resolution_note":"@PHRASE@"}'
  'multi_fact_flag|multi_fact|act|churn_failed|Billing contact needs to be updated|normal|{"status":"open","resolution_note":"priority-review"}'
  'multi_fact_quiet|multi_fact|no_action|starter_failed|Billing contact needs to be updated|normal|'
  'follow_up_recheck|follow_up|follow_up|recheck|Login page is slow this morning|normal|'
)

# Prompts are what a watch's owner would write: no tools, tables or steps.
routine_prompt() {
  case "$1" in
    duplicate) printf '%s' "$ROUTINE_DUPLICATE_PROMPT" ;;
    playbook) printf '%s' 'A support ticket this watch follows just changed. If its subject starts with "DUPLICATE:", close it the way our duplicate playbook says. Otherwise change nothing and say that no change is needed.' ;;
    memory) printf '%s' 'A support ticket this watch follows just changed. If its subject starts with "DUPLICATE:", it repeats an earlier ticket: propose setting its status to "resolved", with the resolution note we always use for duplicates. Otherwise change nothing and say that no change is needed.' ;;
    churn_failed) printf '%s' 'A support ticket this watch follows just changed. Flag it for priority review only if its account is at churn risk and also has at least one failed invoice. To flag it, propose setting its resolution_note to "priority-review". Otherwise change nothing and say that no change is needed.' ;;
    starter_failed) printf '%s' 'A support ticket this watch follows just changed. Flag it for priority review only if its account is on the starter plan and also has at least one failed invoice. To flag it, propose setting its resolution_note to "priority-review". Otherwise change nothing and say that no change is needed.' ;;
    recheck) printf '%s' 'A support ticket this watch follows just changed. Do not change it now. Check on it again in two hours, and if it is still open then, propose raising its severity to "urgent". Say when you will check back.' ;;
    *) echo "unknown routine prompt: $1" >&2; return 1 ;;
  esac
}

case_in_set() {
  case "$CASE_SET" in
    all) return 0 ;;
    base) [ "$1" = "none" ] ;;
    extended) [ "$1" != "none" ] ;;
  esac
}

# Skills and memories a case saved for the watch's owner: deleted after every
# run and again on exit. Check-backs a routine scheduled go with its thread
# (routine_cleanup).
EVAL_ARTIFACT_IDS=()
CASE_PHRASE=""
CASE_SETUP_ERROR=""

eval_cleanup() {
  local id
  for id in "${EVAL_ARTIFACT_IDS[@]-}"; do
    [ -n "$id" ] || continue
    graphql_vars eval-cleanup-artifact "mutation { gj_artifacts(delete: true, where: { id: { eq: \"${id}\" } }) { id } }" '{}' >/dev/null 2>&1 || true
  done
  EVAL_ARTIFACT_IDS=()
}

# With --thread shared each routine prompt answers in one thread this script
# owns: it sweeps the check-backs scheduled there and deletes the threads
# itself. SHARED_THREADS holds "prompt=thread" pairs.
ROUTINE_THREAD_ID=""
SHARED_THREADS=()

# shared_thread <prompt> sets ROUTINE_THREAD_ID to the prompt's thread,
# creating it on first use. It appends to SHARED_THREADS, so never call it in
# $(...).
shared_thread() {
  local key="$1" pair out id
  ROUTINE_THREAD_ID=""
  for pair in "${SHARED_THREADS[@]-}"; do
    if [ "${pair%%=*}" = "$key" ]; then
      ROUTINE_THREAD_ID="${pair#*=}"
      return 0
    fi
  done
  out="$(graphql_vars eval-thread "mutation { gj_thread(insert: { title: \"smoke routine eval ${key} $(date +%s)_$$\" }) { id } }" '{}')"
  id="$(jq -r '[.data.gj_thread] | flatten | .[0].id // empty' "$out" 2>/dev/null || true)"
  if [ -z "$id" ]; then
    echo "could not create a shared thread through gj_thread: $(jq -c '.errors' "$out" 2>/dev/null || true)" >&2
    return 1
  fi
  SHARED_THREADS+=("${key}=${id}")
  ROUTINE_THREAD_ID="$id"
}

shared_thread_cleanup() {
  local pair id
  for pair in "${SHARED_THREADS[@]-}"; do
    id="${pair#*=}"
    [ -n "$id" ] || continue
    routine_sweep_followups "$id" || true
    graphql_vars eval-cleanup-thread "mutation { gj_thread(delete: true, where: { id: { eq: \"${id}\" } }) { id } }" '{}' >/dev/null 2>&1 || true
  done
  SHARED_THREADS=()
  ROUTINE_THREAD_ID=""
}
smoke_extra_cleanup() {
  eval_cleanup
  shared_thread_cleanup
}

# case_setup <need> saves the skill or memory a case depends on, as the
# identity that owns the watch, and sets CASE_PHRASE to the wording it
# carries. It appends to the cleanup arrays, so never call it in $(...).
# CASE_SETUP_ERROR says why a case could not be set up; the run is then
# recorded as failed with that reason, never skipped.
case_setup() {
  local need="$1" stamp out id
  CASE_PHRASE=""
  CASE_SETUP_ERROR=""
  stamp="$(date +%s)_$$_${RANDOM}"
  case "$need" in
    skill)
      CASE_PHRASE="smoke-merged-${RANDOM}${RANDOM}"
      out="$(graphql_vars eval-skill "mutation { gj_artifacts(insert: { kind: \"skill\", name: \"smoke duplicate playbook ${stamp}\", description: \"Our duplicate playbook: how we close duplicate support tickets\", content: \"When a support ticket repeats an earlier one, set its status to resolved and set its resolution_note to exactly: ${CASE_PHRASE}\" }) { id } }" '{}')"
      ;;
    memory)
      CASE_PHRASE="smoke-merged-${RANDOM}${RANDOM}"
      out="$(graphql_vars eval-memory "mutation { gj_artifacts(insert: { kind: \"memory\", content: \"When a duplicate support ticket is closed, its resolution note must read exactly: ${CASE_PHRASE}\" }) { id } }" '{}')"
      ;;
    *) return 0 ;;
  esac
  id="$(jq -r '[.data.gj_artifacts] | flatten | .[0].id // empty' "$out" 2>/dev/null || true)"
  if [ -z "$id" ]; then
    CASE_SETUP_ERROR="could not save the ${need} through gj_artifacts: $(jq -r '[.errors[]?.message] | join("; ")' "$out" 2>/dev/null || true)"
    return 0
  fi
  EVAL_ARTIFACT_IDS+=("$id")
}

# multi_fact_truth <prompt> prints act or no_action for the ticket the next
# case will borrow, read from the database as the same identity the routine
# runs as, so the expected verdict follows the seed instead of assuming it.
multi_fact_truth() {
  local key="$1" row account acct failed
  row="$(graphql_vars eval-oracle-ticket 'query { support_tickets(where: { status: { eq: "open" } }, order_by: { id: asc }, limit: 1) { id account_id } }' '{}')"
  account="$(jq -r '.data.support_tickets[0].account_id // empty' "$row" 2>/dev/null || true)"
  [ -n "$account" ] || { printf 'unknown'; return 0; }
  acct="$(graphql_vars eval-oracle-account "query { accounts(where: { id: { eq: ${account} } }) { status plan } }" '{}')"
  failed="$(graphql_vars eval-oracle-failed "query { invoices(where: { account_id: { eq: ${account} }, status: { eq: \"failed\" } }, limit: 1) { id } }" '{}')"
  jq -rn --arg key "$key" --slurpfile a "$acct" --slurpfile f "$failed" '
    ($a[0].data.accounts // []) as $accounts | (($f[0].data.invoices // []) | length > 0) as $failed
    | if ($accounts | length) != 1 then "unknown"
      elif $key == "churn_failed" then (if $accounts[0].status == "churn_risk" and $failed then "act" else "no_action" end)
      elif $key == "starter_failed" then (if $accounts[0].plan == "starter" and $failed then "act" else "no_action" end)
      else "unknown" end' 2>/dev/null || printf 'unknown'
}

# has_followup <thread> prints true when a check-back is scheduled in a
# routine's thread: a one-time watch named followup_<thread>_...
has_followup() {
  local thread="$1" out
  [ -n "$thread" ] || { printf 'false'; return 0; }
  out="$(graphql_vars eval-followups 'query { gj_watch(limit: 500) { id name } }' '{}')"
  jq -r --arg p "followup_${thread}_" '[.data.gj_watch[]? | select((.name // "") | startswith($p))] | length > 0' "$out" 2>/dev/null || printf 'false'
}

# Every ticket's state, to see which ones an approved change touched.
TICKET_STATE_QUERY='query { support_tickets(order_by: { id: asc }, limit: 1000) { id subject severity status resolution_note resolved_at } }'

results="$TMP_DIR/routine-results.jsonl"
: >"$results"
# Repeats are the outer loop, so a shared thread sees its cases interleaved.
for repeat in $(seq 1 "$REPEATS"); do
  for line in "${CASES[@]}"; do
    IFS='|' read -r id need expect prompt_key subject severity expected <<<"$line"
    case_in_set "$need" || continue
    prompt="$(routine_prompt "$prompt_key")"
    log "routine case ${id} (run ${repeat}/${REPEATS})"
    case_setup "$need"
    if [ "$THREAD_MODE" = "shared" ] && [ -z "$CASE_SETUP_ERROR" ]; then
      shared_thread "$prompt_key" || CASE_SETUP_ERROR="could not create the routine's shared thread through gj_thread"
    fi
    if [ -z "$CASE_SETUP_ERROR" ] && [ "$need" = "multi_fact" ]; then
      truth="$(multi_fact_truth "$prompt_key" || true)"
      if [ "$truth" != "$expect" ]; then
        CASE_SETUP_ERROR="the data no longer makes this case ${expect}: read from the database, it is ${truth:-unknown}"
      fi
    fi
    want="${expected//@PHRASE@/$CASE_PHRASE}"
    [ -n "$want" ] || want='{}'
    verdict='{}' proposed=false precise=null effect=false continued=false followup=false observed='{}'
    if [ -n "$CASE_SETUP_ERROR" ]; then
      echo "routine case ${id}: ${CASE_SETUP_ERROR}" >&2
    else
      routine_ticket_case "$prompt" "${subject} [smoke $(date +%s)${RANDOM}]" "$severity" || true
      verdict="$(cat "$ROUTINE_VERDICT")"
      thread="$(jq -r '.thread_id // ""' <<<"$verdict")"
      proposed="$(jq -r '.approval != null and .approval.status == "pending"' <<<"$verdict")"
      followup="$(has_followup "$thread")"
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
          state="$(graphql_vars eval-state "query { support_tickets(where: { id: { eq: ${ticket} } }) { status resolution_note severity } }" '{}')"
          observed="$(jq -c '.data.support_tickets[0] // {}' "$state")"
          effect="$(jq -n --argjson row "$observed" --argjson want "$want" '($want | length) > 0 and ([$want | to_entries[] | $row[.key] == .value] | all)')"
          messages="$(routine_wait_continuation "$thread" "$(jq -r '.approval.id' <<<"$verdict")")"
          continued="$(jq -r --arg run "run_c_$(jq -r '.approval.id' <<<"$verdict")" '[.data.gj_thread_message[]? | select(.run_id == $run and .role == "assistant")] | length > 0' "$messages")"
        else
          approval_decide "$(jq -r '.approval.id' <<<"$verdict")" reject "$(jq -r '.approval.hash' <<<"$verdict")" "$TMP_DIR/eval-reject.json" >/dev/null
        fi
      fi
    fi
    # The routine's own reply says why a run proposed nothing: it judged no
    # change was needed (answered) or it failed (error, blocked).
    jq -nc --arg id "$id" --arg need "$need" --arg expect "$expect" --argjson repeat "$repeat" --arg delivery "$(jq -r '.delivery // "none"' <<<"$verdict")" \
      --argjson reply "$(jq -c '.reply // {}' <<<"$verdict")" --arg setup "$CASE_SETUP_ERROR" \
      --argjson proposed "$proposed" --argjson precise "$precise" --argjson effect "$effect" --argjson continued "$continued" \
      --argjson followup "$followup" --argjson want "$want" --argjson observed "$observed" \
      '{case:$id, need:$need, expect:$expect, repeat:$repeat, delivery:$delivery, reply_status:($reply.status // ""), reply:($reply.answer // ""),
        proposed:$proposed, precise:$precise, effect:$effect, continued:$continued, followup:$followup, expected:$want, observed:$observed, setup_error:$setup,
        pass: (if $setup != "" then false
               elif $expect == "act" then ($proposed and $precise == true and $effect)
               elif $expect == "no_action" then (($proposed | not) and ($reply.status == "answered"))
               elif $expect == "follow_up" then ($followup and ($proposed | not))
               else false end)}' >>"$results"
    routine_cleanup
    eval_cleanup
    # One run's check-back must not count for the next run in the same thread.
    [ -z "$ROUTINE_THREAD_ID" ] || routine_sweep_followups "$ROUTINE_THREAD_ID"
  done
done
shared_thread_cleanup

report="$(jq -s --arg set "$CASE_SET" --arg thread "$THREAD_MODE" '
  def majority(f): (map(f) | map(select(.)) | length) * 2 > length;
  def rate(xs; f): if (xs | length) == 0 then null else ((xs | map(select(f)) | length) / (xs | length)) end;
  group_by(.case) as $cases
  | [ $cases[] | {case: .[0].case, need: .[0].need, expect: .[0].expect, runs: length,
        passed: (map(select(.pass)) | length), pass: majority(.pass),
        proposed: majority(.proposed),
        precise: (map(select(.proposed and .precise != null)) | if length == 0 then null else majority(.precise) end),
        effect: majority(.effect), continued: majority(.continued), followup: majority(.followup),
        consistent: ((map(.proposed) | unique | length) == 1)} ] as $verdicts
  | ($verdicts | map(select(.need == "none"))) as $base
  | ($base | map(select(.expect == "act"))) as $act
  | ($base | map(select(.expect == "no_action"))) as $quiet
  | { generated_at: (now | todate),
      case_set: $set,
      thread: $thread,
      metrics: {
        verdict_recall: rate($act; .proposed),
        false_alarm_rate: rate($quiet; .proposed),
        proposal_precision: rate($base | map(select(.proposed and .precise != null)); .precise),
        effect_correct: rate($act; .effect),
        continuation_rate: rate($act | map(select(.proposed)); .continued),
        consistency: rate($base; .consistent)
      },
      by_need: (group_by(.need) | map({need: .[0].need, runs: length,
        passed: (map(select(.pass)) | length), pass_rate: rate(.; .pass),
        act_runs: (map(select(.expect == "act")) | length),
        held: (map(select(.expect == "act" and .proposed)) | length),
        right_effect: (map(select(.expect == "act" and .effect)) | length),
        false_alarms: (map(select(.expect != "act" and .proposed)) | length),
        follow_ups: (map(select(.followup)) | length),
        setup_errors: (map(select(.setup_error != "")) | length)})),
      reply_statuses: (map(.reply_status) | group_by(.) | map({(.[0]): length}) | add),
      cases: $verdicts,
      runs: . }
' "$results")"
if [ -n "$REPORT" ]; then
  printf '%s\n' "$report" >"$REPORT"
  log "wrote $REPORT"
fi
jq '{metrics, by_need}' <<<"$report"
