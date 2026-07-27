#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
COFFEE_DIR="$ROOT_DIR/examples/coffee-roastery"
SMOKE="$COFFEE_DIR/scripts/smoke.sh"
SEMANTIC="$COFFEE_DIR/scripts/semantic-smoke.sh"
README="$COFFEE_DIR/README.md"

KNOWN_IDS=(
  TASK-DECLARED
  TASK-CONTINUITY
  TASK-NEXT
  TASK-VERIFY-NOW
  TASK-VERIFY-LATER
  ANNOTATION-DRAFT
  ANNOTATION-SCOPE
  ANNOTATION-DETAIL
  ANNOTATION-MODERATE
  ANNOTATION-SEMANTIC
  ANNOTATION-NOTICE
  ANNOTATION-GUARD
)

known_id() {
  local candidate="$1"
  local known
  for known in "${KNOWN_IDS[@]}"; do
    [ "$candidate" = "$known" ] && return 0
  done
  return 1
}

check_function_contract() {
  local file="$1"
  local function_name="$2"
  local required_ids="$3"
  local line start block id
  line="$(grep -n "^${function_name}()" "$file" | head -1 | cut -d: -f1)"
  [ -n "$line" ] || { echo "missing smoke function: $function_name" >&2; return 1; }
  start=$((line > 14 ? line - 14 : 1))
  block="$(sed -n "${start},${line}p" "$file")"
  printf '%s\n' "$block" | grep -q '^# Capability:' || {
    echo "$function_name has no Capability contract comment" >&2
    return 1
  }
  for id in $required_ids; do
    printf '%s\n' "$block" | grep -q "$id" || {
      echo "$function_name contract comment is missing $id" >&2
      return 1
    }
  done
}

# These are the capability-owned suites added for declared tasks and catalog
# annotations. Keeping the mapping explicit makes new unlabeled suites fail the
# same static check that guards the README traceability matrix.
while IFS='|' read -r file function_name required_ids; do
  check_function_contract "$file" "$function_name" "$required_ids"
done <<EOF
$SMOKE|run_task_control_plane_suite|TASK-DECLARED TASK-NEXT TASK-VERIFY-NOW TASK-VERIFY-LATER
$SMOKE|run_task_verifier_deep_suite|TASK-VERIFY-LATER
$SMOKE|run_task_agent_eval_suite|TASK-CONTINUITY
$SMOKE|run_annotation_suite|ANNOTATION-DRAFT ANNOTATION-SCOPE ANNOTATION-DETAIL ANNOTATION-MODERATE
$SEMANTIC|run_annotation_semantic_smoke|ANNOTATION-SEMANTIC
$SEMANTIC|run_agent_state_notice_smoke|ANNOTATION-NOTICE TASK-VERIFY-NOW
$SEMANTIC|run_annotation_agent_guard_smoke|ANNOTATION-GUARD
EOF

while IFS= read -r label; do
  id="${label#[}"
  id="${id%]}"
  if ! known_id "$id"; then
    echo "unknown capability assertion label: $label" >&2
    exit 1
  fi
done < <(grep -hEo '\[(TASK|ANNOTATION)-[A-Z-]+\]' "$SMOKE" "$SEMANTIC" | sort -u)

for id in "${KNOWN_IDS[@]}"; do
  grep -q "\[$id\]" "$SMOKE" "$SEMANTIC" || {
    echo "capability has no smoke assertion label: $id" >&2
    exit 1
  }
  grep -q "| \`$id\` |" "$README" || {
    echo "README capability matrix is missing $id" >&2
    exit 1
  }
done

printf 'ok  capability smoke comments, labels, and README matrix are aligned\n'
