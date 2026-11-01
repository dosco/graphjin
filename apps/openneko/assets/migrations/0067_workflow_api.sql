-- Opt-in per-workflow public API: one-time credentials, transactional
-- admission/idempotency, durable dispatch, typed run metadata, and bounded
-- terminal results. Plaintext tokens are never stored.

ALTER TABLE workflow_run
  ADD COLUMN IF NOT EXISTS execution_mode text,
  ADD COLUMN IF NOT EXISTS trigger_input_preview jsonb,
  ADD COLUMN IF NOT EXISTS telemetry_summary jsonb,
  ADD COLUMN IF NOT EXISTS terminal_result jsonb,
  ADD COLUMN IF NOT EXISTS result_artifact_path text,
  ADD COLUMN IF NOT EXISTS progress jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS queue_attempts integer NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS admitted_at timestamptz,
  ADD COLUMN IF NOT EXISTS result_expires_at timestamptz;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conname = 'workflow_run_execution_mode_check'
      AND conrelid = 'workflow_run'::regclass
  ) THEN
    ALTER TABLE workflow_run
      ADD CONSTRAINT workflow_run_execution_mode_check CHECK (
        (trigger_kind = 'api' AND execution_mode IN ('single', 'batch')) OR
        (trigger_kind <> 'api' AND execution_mode IS NULL)
      );
  END IF;
END $$;

CREATE TABLE IF NOT EXISTS workflow_api_access (
  workflow_id uuid PRIMARY KEY REFERENCES workflow_definition(id) ON DELETE CASCADE,
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  enabled boolean NOT NULL DEFAULT false,
  token_verifier text,
  token_prefix text,
  token_created_at timestamptz,
  token_rotated_at timestamptz,
  last_used_at timestamptz,
  request_limit_per_minute integer NOT NULL DEFAULT 30 CHECK (request_limit_per_minute BETWEEN 1 AND 600),
  poll_limit_per_minute integer NOT NULL DEFAULT 120 CHECK (poll_limit_per_minute BETWEEN 1 AND 1200),
  queue_cap integer NOT NULL DEFAULT 25 CHECK (queue_cap BETWEEN 1 AND 250),
  concurrency_cap integer NOT NULL DEFAULT 2 CHECK (concurrency_cap BETWEEN 1 AND 20),
  batch_max_records integer NOT NULL DEFAULT 1000 CHECK (batch_max_records BETWEEN 1 AND 10000),
  batch_chunk_size integer NOT NULL DEFAULT 100 CHECK (batch_chunk_size BETWEEN 1 AND 500),
  max_request_bytes integer NOT NULL DEFAULT 262144 CHECK (max_request_bytes BETWEEN 1024 AND 1048576),
  max_result_bytes integer NOT NULL DEFAULT 262144 CHECK (max_result_bytes BETWEEN 1024 AND 1048576),
  max_artifact_bytes integer NOT NULL DEFAULT 10485760 CHECK (max_artifact_bytes BETWEEN 1024 AND 52428800),
  max_runtime_seconds integer NOT NULL DEFAULT 600 CHECK (max_runtime_seconds BETWEEN 30 AND 1800),
  max_model_calls integer NOT NULL DEFAULT 8 CHECK (max_model_calls BETWEEN 1 AND 32),
  max_tool_calls integer NOT NULL DEFAULT 32 CHECK (max_tool_calls BETWEEN 1 AND 128),
  max_tokens_per_run integer NOT NULL DEFAULT 100000 CHECK (max_tokens_per_run BETWEEN 1000 AND 1000000),
  max_cost_micros_per_run integer NOT NULL DEFAULT 5000000 CHECK (max_cost_micros_per_run BETWEEN 1000 AND 100000000),
  rolling_window_seconds integer NOT NULL DEFAULT 86400 CHECK (rolling_window_seconds BETWEEN 3600 AND 604800),
  rolling_token_budget integer NOT NULL DEFAULT 250000 CHECK (rolling_token_budget BETWEEN 1000 AND 10000000),
  rolling_cost_micros_budget integer NOT NULL DEFAULT 10000000 CHECK (rolling_cost_micros_budget BETWEEN 1000 AND 1000000000),
  retention_hours integer NOT NULL DEFAULT 168 CHECK (retention_hours BETWEEN 1 AND 720),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT workflow_api_access_token_state CHECK (
    (enabled = false) OR
    (token_verifier IS NOT NULL AND token_prefix IS NOT NULL AND token_created_at IS NOT NULL)
  ),
  CONSTRAINT workflow_api_access_batch_bounds CHECK (batch_chunk_size <= batch_max_records),
  CONSTRAINT workflow_api_access_verifier_shape CHECK (
    token_verifier IS NULL OR token_verifier ~ '^[0-9a-f]{64}$'
  ),
  CONSTRAINT workflow_api_access_prefix_shape CHECK (
    token_prefix IS NULL OR token_prefix ~ '^onk_wf_[0-9a-f]{12}$'
  )
);

CREATE INDEX IF NOT EXISTS workflow_api_access_org_idx
  ON workflow_api_access (org_id, enabled);

CREATE TABLE IF NOT EXISTS workflow_api_admission (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  workflow_id uuid NOT NULL REFERENCES workflow_definition(id) ON DELETE CASCADE,
  workflow_run_id uuid NOT NULL REFERENCES workflow_run(id) ON DELETE CASCADE,
  idempotency_hash text NOT NULL,
  payload_hash text NOT NULL,
  execution_mode text NOT NULL CHECK (execution_mode IN ('single', 'batch')),
  request_payload jsonb,
  input_file_path text,
  batch_contract jsonb,
  request_bytes integer NOT NULL,
  accepted_records integer,
  status text NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'dispatching', 'enqueued', 'running', 'completed', 'failed', 'cancelled', 'expired')),
  attempts integer NOT NULL DEFAULT 0,
  reserved_tokens integer NOT NULL DEFAULT 0,
  reserved_cost_micros integer NOT NULL DEFAULT 0,
  actual_tokens integer,
  actual_cost_micros integer,
  available_at timestamptz NOT NULL DEFAULT now(),
  lease_until timestamptz,
  queue_job_id text,
  last_error_code text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  dispatched_at timestamptz,
  started_at timestamptz,
  completed_at timestamptz,
  expires_at timestamptz NOT NULL,
  CONSTRAINT workflow_api_admission_idempotency_unique
    UNIQUE (org_id, workflow_id, idempotency_hash),
  CONSTRAINT workflow_api_admission_workflow_run_unique UNIQUE (workflow_run_id),
  CONSTRAINT workflow_api_admission_payload_location CHECK (
    (execution_mode = 'single' AND request_payload IS NOT NULL AND input_file_path IS NULL AND batch_contract IS NULL AND accepted_records IS NULL) OR
    (execution_mode = 'batch' AND request_payload IS NULL AND input_file_path IS NOT NULL AND batch_contract IS NOT NULL AND accepted_records BETWEEN 1 AND 10000)
  ),
  CONSTRAINT workflow_api_admission_request_bounds CHECK (request_bytes BETWEEN 2 AND 1048576),
  CONSTRAINT workflow_api_admission_attempts_nonnegative CHECK (attempts >= 0),
  CONSTRAINT workflow_api_admission_reservations_nonnegative CHECK (
    reserved_tokens >= 0 AND reserved_cost_micros >= 0 AND
    (actual_tokens IS NULL OR actual_tokens >= 0) AND
    (actual_cost_micros IS NULL OR actual_cost_micros >= 0)
  ),
  CONSTRAINT workflow_api_admission_hashes_shape CHECK (
    idempotency_hash ~ '^[0-9a-f]{64}$' AND
    payload_hash ~ '^[0-9a-f]{64}$'
  ),
  CONSTRAINT workflow_api_admission_expiry_check CHECK (expires_at > created_at)
);

CREATE INDEX IF NOT EXISTS workflow_api_admission_dispatch_idx
  ON workflow_api_admission (status, available_at);
CREATE INDEX IF NOT EXISTS workflow_api_admission_workflow_created_idx
  ON workflow_api_admission (workflow_id, created_at DESC);

CREATE TABLE IF NOT EXISTS workflow_api_rate_bucket (
  scope_kind text NOT NULL,
  scope_id text NOT NULL,
  operation text NOT NULL,
  window_start timestamptz NOT NULL,
  count integer NOT NULL DEFAULT 0,
  expires_at timestamptz NOT NULL,
  CONSTRAINT workflow_api_rate_bucket_count_check CHECK (count >= 0),
  CONSTRAINT workflow_api_rate_bucket_expiry_check CHECK (expires_at > window_start),
  CONSTRAINT workflow_api_rate_bucket_scope_check CHECK (
    scope_kind IN ('deployment', 'client', 'workflow', 'organization')
  ),
  CONSTRAINT workflow_api_rate_bucket_operation_check CHECK (
    operation IN ('edge', 'invoke', 'poll')
  ),
  CONSTRAINT workflow_api_rate_bucket_pk
    PRIMARY KEY (scope_kind, scope_id, operation, window_start)
);

CREATE INDEX IF NOT EXISTS workflow_api_rate_bucket_expiry_idx
  ON workflow_api_rate_bucket (expires_at);
