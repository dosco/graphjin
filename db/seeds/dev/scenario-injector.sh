#!/bin/sh
# scenario-injector.sh — L3 trial sim scheduler + CLI.
#
# Default entrypoint: bootstraps the trial_sim schema in the
# AdventureWorks DB and runs a 5-min scheduler tick loop, firing
# scenarios when their offset (first 48h) or cron (after 48h) is due.
# Idempotency is enforced by INSERT-on-conflict into trial_sim.scenario_run.
#
# Subcommands (invoked via `docker compose exec`):
#   fire <id>   queue an immediate fire of scenario <id>
#   pause       pause the scheduler
#   resume      resume the scheduler
#   status      show installed_at, last firings, pending fire requests
#
# All commands talk to the AdventureWorks DB via the PG* env vars set by
# the adventureworks-scenario-injector compose service.

set -eu

SCRIPT_DIR=$(dirname "$0")
SCHEMA_SQL="${SCRIPT_DIR}/trial-sim-schema.sql"
ADVANCE_DATES_SQL="${SCRIPT_DIR}/advance-dates.sql"
SCENARIOS_DIR="${SCRIPT_DIR}/scenarios"
TICK_INTERVAL_SEC="${TRIAL_SIM_TICK_SEC:-300}"
LOG_PREFIX="[trial-sim]"

PSQL="psql -v ON_ERROR_STOP=1 -q -X"
PSQL_OUT="psql -v ON_ERROR_STOP=1 -q -X -At -F|"

log() { printf '%s %s\n' "$LOG_PREFIX" "$*"; }

bootstrap_schema() {
  log "applying $SCHEMA_SQL"
  $PSQL -f "$SCHEMA_SQL" >/dev/null
}

advance_dates() {
  log "applying $ADVANCE_DATES_SQL"
  $PSQL -f "$ADVANCE_DATES_SQL"
}

# fire_scenario <id> <window_key> <triggered_by>
fire_scenario() {
  id=$1
  window=$2
  src=$3

  inserted=$($PSQL_OUT <<SQL
INSERT INTO trial_sim.scenario_run (scenario_id, window_key, triggered_by)
VALUES ('$id', '$window', '$src')
ON CONFLICT (scenario_id, window_key) DO NOTHING
RETURNING 1;
SQL
)
  if [ -z "$inserted" ]; then
    return 0
  fi

  sql_path=$($PSQL_OUT -c "SELECT sql_path FROM trial_sim.scenario WHERE id='$id'")
  if [ -z "$sql_path" ]; then
    log "WARN scenario '$id' not in catalog; skipping"
    return 0
  fi
  scenario_file="$SCENARIOS_DIR/$sql_path"
  if [ ! -f "$scenario_file" ]; then
    log "WARN scenario file missing: $scenario_file"
    return 0
  fi

  log "firing $id (window=$window, source=$src)"
  $PSQL -f "$scenario_file" || log "WARN $id sql failed; continuing"
}

drain_fire_requests() {
  $PSQL_OUT -c "SELECT id, scenario_id FROM trial_sim.fire_request WHERE consumed_at IS NULL ORDER BY id" \
    | while IFS='|' read -r req_id sid; do
    [ -z "$req_id" ] && continue
    fire_scenario "$sid" "fire-$req_id" "fire_now"
    $PSQL -c "UPDATE trial_sim.fire_request SET consumed_at = now() WHERE id = $req_id" >/dev/null
  done
}

run_offset_firings() {
  $PSQL_OUT -c "
    SELECT s.id
    FROM trial_sim.scenario s, trial_sim.state st
    WHERE s.enabled
      AND s.initial_offset_minutes IS NOT NULL
      AND now() - st.installed_at >= make_interval(mins => s.initial_offset_minutes)
      AND NOT EXISTS (
        SELECT 1 FROM trial_sim.scenario_run r
        WHERE r.scenario_id = s.id AND r.window_key = 'offset'
      )
  " | while IFS='|' read -r id; do
    [ -z "$id" ] && continue
    fire_scenario "$id" "offset" "schedule"
  done
}

run_cron_firings() {
  $PSQL_OUT -c "
    WITH prev AS (
      SELECT s.id,
             trial_sim.cron_previous(s.cron, s.cron_timezone, now()) AS occ
      FROM trial_sim.scenario s, trial_sim.state st
      WHERE s.enabled
        AND s.cron IS NOT NULL
        AND now() - st.installed_at >= interval '48 hours'
    )
    SELECT id, to_char(occ, 'YYYY-MM-DD\"T\"HH24:MI') AS window_key
    FROM prev
    WHERE occ IS NOT NULL
      AND now() - occ < interval '10 minutes'
  " | while IFS='|' read -r id window; do
    [ -z "$id" ] && continue
    [ -z "$window" ] && continue
    fire_scenario "$id" "$window" "schedule"
  done
}

is_paused() {
  v=$($PSQL_OUT -c "SELECT paused FROM trial_sim.state")
  [ "$v" = "t" ]
}

tick() {
  if is_paused; then
    log "paused"
    return 0
  fi
  drain_fire_requests
  run_offset_firings
  run_cron_firings
}

cmd_loop() {
  bootstrap_schema
  advance_dates
  log "scheduler loop starting; tick=${TICK_INTERVAL_SEC}s"
  while true; do
    tick || log "WARN tick failed; continuing"
    sleep "$TICK_INTERVAL_SEC"
  done
}

cmd_fire() {
  if [ -z "${1:-}" ]; then
    echo "usage: $0 fire <scenario_id>" >&2
    exit 2
  fi
  $PSQL -c "INSERT INTO trial_sim.fire_request (scenario_id) VALUES ('$1')"
  log "queued fire for '$1' (runs on next tick, within ${TICK_INTERVAL_SEC}s)"
}

cmd_pause() {
  $PSQL -c "UPDATE trial_sim.state SET paused = true"
  log "paused"
}

cmd_resume() {
  $PSQL -c "UPDATE trial_sim.state SET paused = false"
  log "resumed"
}

cmd_tick() {
  # On-demand tick. Useful for demos ("fire-now" requests run within
  # seconds instead of waiting up to TRIAL_SIM_TICK_SEC) and for tests.
  tick
}

cmd_status() {
  log "state:"
  $PSQL -c "SELECT installed_at, paused FROM trial_sim.state"
  log "last firings per scenario:"
  $PSQL -c "
    SELECT s.id,
           r.last_fired,
           r.firings,
           CASE
             WHEN now() - st.installed_at < interval '48 hours' THEN 'offset-mode'
             ELSE 'cron-mode'
           END AS phase
    FROM trial_sim.scenario s
    LEFT JOIN (
      SELECT scenario_id, MAX(fired_at) AS last_fired, COUNT(*) AS firings
      FROM trial_sim.scenario_run GROUP BY scenario_id
    ) r ON r.scenario_id = s.id
    CROSS JOIN trial_sim.state st
    ORDER BY r.last_fired DESC NULLS LAST, s.id
  "
  log "pending fire requests:"
  $PSQL -c "
    SELECT id, scenario_id, requested_at, consumed_at
    FROM trial_sim.fire_request
    ORDER BY id DESC LIMIT 10
  "
}

case "${1:-loop}" in
  loop)   cmd_loop ;;
  tick)   cmd_tick ;;
  fire)   shift; cmd_fire "$@" ;;
  pause)  cmd_pause ;;
  resume) cmd_resume ;;
  status) cmd_status ;;
  *)      echo "usage: $0 {loop|tick|fire <id>|pause|resume|status}" >&2; exit 2 ;;
esac
