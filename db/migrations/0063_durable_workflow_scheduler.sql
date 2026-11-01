-- Durable cron scheduling: persisted cursor + transactional firing outbox +
-- scheduler heartbeat. The worker computes the first next_fire_at after boot;
-- migrations deliberately do not replay historical gaps.

CREATE TABLE IF NOT EXISTS workflow_schedule_state (
  workflow_id uuid PRIMARY KEY REFERENCES workflow_definition(id) ON DELETE CASCADE,
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  cron text NOT NULL,
  cron_timezone text NOT NULL,
  definition_updated_at timestamptz NOT NULL,
  next_fire_at timestamptz NOT NULL,
  catch_up_policy text NOT NULL DEFAULT 'coalesce'
    CHECK (catch_up_policy IN ('coalesce', 'replay')),
  last_materialized_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS workflow_schedule_state_due_idx
  ON workflow_schedule_state (next_fire_at);
CREATE INDEX IF NOT EXISTS workflow_schedule_state_org_idx
  ON workflow_schedule_state (org_id);

CREATE TABLE IF NOT EXISTS workflow_schedule_firing (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  workflow_id uuid NOT NULL REFERENCES workflow_definition(id) ON DELETE CASCADE,
  scheduled_for timestamptz NOT NULL,
  status text NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'dispatching', 'enqueued', 'running', 'completed', 'cancelled')),
  attempts integer NOT NULL DEFAULT 0,
  available_at timestamptz NOT NULL DEFAULT now(),
  lease_until timestamptz,
  queue_job_id text,
  workflow_run_id uuid REFERENCES workflow_run(id) ON DELETE SET NULL,
  last_error text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  dispatched_at timestamptz,
  completed_at timestamptz,
  CONSTRAINT workflow_schedule_firing_workflow_scheduled_unique
    UNIQUE (workflow_id, scheduled_for)
);

CREATE UNIQUE INDEX IF NOT EXISTS workflow_schedule_firing_workflow_run_unique
  ON workflow_schedule_firing (workflow_run_id)
  WHERE workflow_run_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS workflow_schedule_firing_pending_idx
  ON workflow_schedule_firing (status, available_at);
CREATE INDEX IF NOT EXISTS workflow_schedule_firing_workflow_created_idx
  ON workflow_schedule_firing (workflow_id, created_at DESC);

CREATE TABLE IF NOT EXISTS workflow_scheduler_health (
  id text PRIMARY KEY,
  last_started_at timestamptz,
  last_succeeded_at timestamptz,
  last_error_at timestamptz,
  last_error text,
  last_materialized_count integer NOT NULL DEFAULT 0,
  last_dispatched_count integer NOT NULL DEFAULT 0,
  updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO workflow_scheduler_health (id)
VALUES ('cron')
ON CONFLICT (id) DO NOTHING;
