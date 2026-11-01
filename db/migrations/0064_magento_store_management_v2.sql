-- Magento pack V2: governed store controls, bulk change-sets, undo metadata,
-- auto-rule budgets, and the deliberately non-executable financial handoff.

CREATE TABLE IF NOT EXISTS action_changeset (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  action_request_id uuid REFERENCES action_request(id) ON DELETE SET NULL,
  inverse_of_id uuid REFERENCES action_changeset(id) ON DELETE SET NULL,
  domain text NOT NULL,
  scope jsonb NOT NULL DEFAULT '{}'::jsonb,
  operation_id text NOT NULL,
  risk_class smallint NOT NULL CHECK (risk_class BETWEEN 1 AND 2),
  status text NOT NULL DEFAULT 'draft'
    CHECK (status IN (
      'draft', 'previewed', 'pending_approval', 'approved', 'executing',
      'applied', 'partially_applied', 'reconcile_required', 'failed',
      'cancelled'
    )),
  summary text NOT NULL DEFAULT '',
  idempotency_key text NOT NULL,
  bulk_uuid text,
  cap_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
  projected_exposure numeric(18, 4),
  previewed_at timestamptz,
  approved_by_user_id text REFERENCES app_user(id) ON DELETE SET NULL,
  approved_at timestamptz,
  executed_at timestamptz,
  reconciled_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT action_changeset_org_idempotency_unique UNIQUE (org_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS action_changeset_org_status_idx
  ON action_changeset (org_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS action_changeset_request_idx
  ON action_changeset (action_request_id, created_at DESC);
CREATE INDEX IF NOT EXISTS action_changeset_inverse_idx
  ON action_changeset (inverse_of_id)
  WHERE inverse_of_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS action_changeset_row (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  changeset_id uuid NOT NULL REFERENCES action_changeset(id) ON DELETE CASCADE,
  row_index integer NOT NULL CHECK (row_index >= 0),
  entity_ref text NOT NULL,
  operation_id text NOT NULL,
  path jsonb NOT NULL DEFAULT '{}'::jsonb,
  query jsonb NOT NULL DEFAULT '{}'::jsonb,
  before_image jsonb NOT NULL DEFAULT '{}'::jsonb,
  after_image jsonb NOT NULL DEFAULT '{}'::jsonb,
  expected_current jsonb NOT NULL DEFAULT '{}'::jsonb,
  status text NOT NULL DEFAULT 'draft'
    CHECK (status IN (
      'draft', 'submitted', 'accepted', 'applied', 'failed',
      'drifted', 'reconcile_required', 'skipped'
    )),
  external_ref text,
  reconciled_image jsonb,
  error text,
  started_at timestamptz,
  finished_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT action_changeset_row_position_unique UNIQUE (changeset_id, row_index)
);

CREATE INDEX IF NOT EXISTS action_changeset_row_status_idx
  ON action_changeset_row (changeset_id, status, row_index);
CREATE INDEX IF NOT EXISTS action_changeset_row_entity_idx
  ON action_changeset_row (changeset_id, entity_ref);

ALTER TABLE action_execution
  ADD COLUMN IF NOT EXISTS changeset_id uuid REFERENCES action_changeset(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS action_execution_changeset_idx
  ON action_execution (changeset_id, created_at DESC)
  WHERE changeset_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS magento_store_control (
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  domain text NOT NULL
    CHECK (domain IN ('catalog', 'inventory', 'orders', 'promotions', 'content', 'customers')),
  risk_class smallint NOT NULL CHECK (risk_class BETWEEN 1 AND 2),
  enabled boolean NOT NULL DEFAULT true,
  auto_execute boolean NOT NULL DEFAULT false,
  caps jsonb NOT NULL DEFAULT '{}'::jsonb,
  scope jsonb NOT NULL DEFAULT '{}'::jsonb,
  updated_by_user_id text REFERENCES app_user(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, domain)
);

CREATE TABLE IF NOT EXISTS magento_attribute_classification (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  domain text NOT NULL,
  entity_type text NOT NULL,
  attribute text NOT NULL,
  risk_class smallint NOT NULL CHECK (risk_class BETWEEN 0 AND 2),
  category text NOT NULL CHECK (category IN ('financial', 'pii', 'content', 'operational')),
  rationale text NOT NULL DEFAULT '',
  reviewed boolean NOT NULL DEFAULT true,
  pack_default boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT magento_attribute_classification_identity_unique
    UNIQUE (org_id, domain, entity_type, attribute)
);

CREATE INDEX IF NOT EXISTS magento_attribute_classification_lookup_idx
  ON magento_attribute_classification (org_id, domain, entity_type);

CREATE TABLE IF NOT EXISTS magento_auto_rule (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  name text NOT NULL,
  instruction text NOT NULL,
  domain text NOT NULL,
  action_kind text NOT NULL,
  compiled_policy jsonb NOT NULL DEFAULT '{}'::jsonb,
  daily_cap integer NOT NULL CHECK (daily_cap > 0),
  cooldown_seconds integer NOT NULL DEFAULT 0 CHECK (cooldown_seconds >= 0),
  enabled boolean NOT NULL DEFAULT false,
  suspended_reason text,
  last_fired_at timestamptz,
  created_by_user_id text REFERENCES app_user(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT magento_auto_rule_org_name_unique UNIQUE (org_id, name)
);

CREATE INDEX IF NOT EXISTS magento_auto_rule_active_idx
  ON magento_auto_rule (org_id, enabled, domain);

CREATE TABLE IF NOT EXISTS magento_financial_handoff (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  action_request_id uuid REFERENCES action_request(id) ON DELETE SET NULL,
  kind text NOT NULL CHECK (kind IN ('online_refund', 'return_approval', 'financial_configuration', 'store_credit_over_cap')),
  entity_ref text NOT NULL,
  status text NOT NULL DEFAULT 'draft'
    CHECK (status IN ('draft', 'ready_for_human', 'completed_by_human', 'cancelled')),
  draft jsonb NOT NULL DEFAULT '{}'::jsonb,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  completed_by_user_id text REFERENCES app_user(id) ON DELETE SET NULL,
  completed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS magento_financial_handoff_org_status_idx
  ON magento_financial_handoff (org_id, status, created_at DESC);
