-- trial-sim-schema.sql
-- L3 scripted-scenario infrastructure for the AdventureWorks trial.
-- Idempotent: every CREATE is IF NOT EXISTS / OR REPLACE. Loaded by the
-- adventureworks-scenario-injector container on every start.
--
-- Everything lives in the `trial_sim` schema in the AdventureWorks DB so
-- `docker compose down -v` (which wipes adventureworks-db-data) also
-- wipes all sim state. No cross-DB plumbing.

\set ON_ERROR_STOP on

CREATE SCHEMA IF NOT EXISTS trial_sim;

-- One-row state table. installed_at drives the 48h offset-vs-cron cutover.
CREATE TABLE IF NOT EXISTS trial_sim.state (
  singleton    boolean     PRIMARY KEY DEFAULT true CHECK (singleton),
  installed_at timestamptz NOT NULL DEFAULT now(),
  paused       boolean     NOT NULL DEFAULT false
);

-- Added later — keeps existing installs forward-compatible without a
-- separate migration step. Read by advance-dates.sql to throttle shifts.
ALTER TABLE trial_sim.state
  ADD COLUMN IF NOT EXISTS advance_dates_at timestamptz;

-- Idempotency ledger. (scenario_id, window_key) PK makes every firing
-- exactly-once across restarts.
CREATE TABLE IF NOT EXISTS trial_sim.scenario_run (
  scenario_id  text        NOT NULL,
  window_key   text        NOT NULL,
  fired_at     timestamptz NOT NULL DEFAULT now(),
  triggered_by text        NOT NULL,
  PRIMARY KEY (scenario_id, window_key)
);

-- Queue for `docker compose exec ... fire <id>` requests. The scheduler
-- drains this every tick.
CREATE TABLE IF NOT EXISTS trial_sim.fire_request (
  id           bigserial   PRIMARY KEY,
  scenario_id  text        NOT NULL,
  requested_at timestamptz NOT NULL DEFAULT now(),
  consumed_at  timestamptz
);

-- Catalog of scenarios. SQL-backed instead of YAML so the injector image
-- stays pure postgres:16-alpine + sh (no yq dependency).
CREATE TABLE IF NOT EXISTS trial_sim.scenario (
  id                     text PRIMARY KEY,
  sql_path               text NOT NULL,
  initial_offset_minutes int,
  cron                   text,
  cron_timezone          text NOT NULL DEFAULT 'UTC',
  trips                  text,
  surfaces               text[],
  enabled                boolean NOT NULL DEFAULT true
);

-- Time-windowed filters that the L1 trickle consults each tick. Lets a
-- scenario like "Germany revenue drop" say "exclude territory 8 until
-- 2026-05-14 13:00". Read by aw-sim-tick.sql.
CREATE TABLE IF NOT EXISTS trial_sim.active_filter (
  id           bigserial   PRIMARY KEY,
  scenario_id  text        NOT NULL,
  kind         text        NOT NULL,   -- 'exclude_territory' | 'boost_territory'
  payload      jsonb       NOT NULL,   -- {territory_id: 8, multiplier: 3, ...}
  expires_at   timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_active_filter_expires
  ON trial_sim.active_filter (expires_at);

-- Seed installed_at on first run (idempotent).
INSERT INTO trial_sim.state (singleton) VALUES (true)
ON CONFLICT (singleton) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Cron parsing
-- ---------------------------------------------------------------------------
-- cron_field_matches(field, value) — does a single cron field match an int?
-- Supports: *, single number, a-b, a,b,c, */n, a-b/n.

CREATE OR REPLACE FUNCTION trial_sim.cron_field_matches(field text, value int)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
  part text;
  rng  text[];
  step int;
  base text;
  lo   int;
  hi   int;
BEGIN
  IF field = '*' THEN RETURN true; END IF;

  IF field LIKE '%,%' THEN
    FOREACH part IN ARRAY string_to_array(field, ',') LOOP
      IF trial_sim.cron_field_matches(part, value) THEN RETURN true; END IF;
    END LOOP;
    RETURN false;
  END IF;

  IF field LIKE '%/%' THEN
    step := split_part(field, '/', 2)::int;
    base := split_part(field, '/', 1);
    IF base = '*' THEN
      RETURN (value % step) = 0;
    END IF;
    rng := string_to_array(base, '-');
    lo  := rng[1]::int;
    hi  := rng[2]::int;
    RETURN value BETWEEN lo AND hi AND ((value - lo) % step) = 0;
  END IF;

  IF field LIKE '%-%' THEN
    rng := string_to_array(field, '-');
    RETURN value BETWEEN rng[1]::int AND rng[2]::int;
  END IF;

  RETURN value = field::int;
END;
$$;

-- cron_previous(cron, tz, ref) — most recent cron occurrence ≤ ref, or NULL
-- within the lookback window. The scheduler only ever asks for "previous
-- within last 10 min", so we walk back minute-by-minute up to lookback.
--
-- Cron dom/dow special case: if BOTH are restricted, firing happens when
-- EITHER matches (POSIX cron semantics). If only one is restricted, AND.

CREATE OR REPLACE FUNCTION trial_sim.cron_previous(
  cron             text,
  tz               text,
  ref              timestamptz,
  lookback_minutes int DEFAULT 10
) RETURNS timestamptz LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
  parts     text[];
  m_field   text;
  h_field   text;
  dom_field text;
  mon_field text;
  dow_field text;
  candidate timestamptz;
  local_ts  timestamp;
  i         int;
  dom_match boolean;
  dow_match boolean;
  date_match boolean;
BEGIN
  parts := regexp_split_to_array(trim(cron), '\s+');
  IF array_length(parts, 1) <> 5 THEN
    RAISE EXCEPTION 'cron must have 5 fields, got: %', cron;
  END IF;
  m_field   := parts[1];
  h_field   := parts[2];
  dom_field := parts[3];
  mon_field := parts[4];
  dow_field := parts[5];

  FOR i IN 0..lookback_minutes LOOP
    candidate := date_trunc('minute', ref) - (i || ' minutes')::interval;
    local_ts  := candidate AT TIME ZONE tz;

    dom_match := trial_sim.cron_field_matches(dom_field, EXTRACT(DAY FROM local_ts)::int);
    dow_match := trial_sim.cron_field_matches(dow_field, EXTRACT(DOW FROM local_ts)::int);

    IF dom_field <> '*' AND dow_field <> '*' THEN
      date_match := dom_match OR dow_match;
    ELSE
      date_match := dom_match AND dow_match;
    END IF;

    IF trial_sim.cron_field_matches(m_field,   EXTRACT(MINUTE FROM local_ts)::int)
       AND trial_sim.cron_field_matches(h_field,   EXTRACT(HOUR   FROM local_ts)::int)
       AND trial_sim.cron_field_matches(mon_field, EXTRACT(MONTH  FROM local_ts)::int)
       AND date_match
    THEN
      RETURN candidate;
    END IF;
  END LOOP;

  RETURN NULL;
END;
$$;

-- ---------------------------------------------------------------------------
-- Scenario catalog seed
-- ---------------------------------------------------------------------------
-- Upserts let us add/edit scenarios in this file and reseed without
-- losing scenario_run history.

INSERT INTO trial_sim.scenario
  (id, sql_path, initial_offset_minutes, cron, cron_timezone, trips, surfaces)
VALUES
  ('germany-revenue-drop',    'germany-revenue-drop.sql',     20,   '0 10 * * 1',  'UTC',
   'revenue-drop-alert',      ARRAY['briefing','approvals','run']),
  ('mountain-200-spike',      'mountain-200-spike.sql',       50,   '0 14 * * 3',  'UTC',
   'inventory-watch',         ARRAY['briefing','run']),
  ('slow-ship-batch',         'slow-ship-batch.sql',          90,   '0 9 * * 5',   'UTC',
   'slow-ship-operations',    ARRAY['briefing']),
  ('cancellation-spike',      'cancellation-spike.sql',       120,  '0 16 * * 2',  'UTC',
   'quality-watch',           ARRAY['briefing']),
  ('france-takeover',         'france-takeover.sql',          150,  '0 22 * * 0',  'UTC',
   'top-territory-watch',     ARRAY['briefing','run']),
  ('approved-action-history', 'approved-action-history.sql',  NULL, '0 0 * * *',   'UTC',
   NULL,                      ARRAY[]::text[]),
  ('low-stock-reorder',       'low-stock-reorder.sql',        15,   '0 13 * * 4',  'UTC',
   'stock-reorder-watch',     ARRAY['briefing','run'])
ON CONFLICT (id) DO UPDATE SET
  sql_path               = EXCLUDED.sql_path,
  initial_offset_minutes = EXCLUDED.initial_offset_minutes,
  cron                   = EXCLUDED.cron,
  cron_timezone          = EXCLUDED.cron_timezone,
  trips                  = EXCLUDED.trips,
  surfaces               = EXCLUDED.surfaces;
