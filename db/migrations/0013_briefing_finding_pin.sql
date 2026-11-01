-- Pin a workflow_output to the Briefing as a curated card.
--
-- Distinct from `dashboard_pin` (which pins a warehouse KPI metric):
-- this table tracks operator-curated promotions of *findings* — outputs
-- the operator decided are worth leading the Briefing with, regardless
-- of mood. The architecture's `briefing_card` concept maps here; we
-- skip the original wider `briefing_card` design in favor of this
-- focused pin table because v1 only needs "did the operator pin this
-- output?" not the broader "any human-facing observation derived from
-- an observation row."
--
-- Unique (org_id, output_id) keeps double-pin idempotent. ON DELETE
-- CASCADE on the output side means unpinning happens automatically
-- when an output is purged by the TTL sweep.

CREATE TABLE IF NOT EXISTS briefing_finding_pin (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id            text NOT NULL
                      REFERENCES organization(id) ON DELETE CASCADE,
  output_id         uuid NOT NULL
                      REFERENCES workflow_output(id) ON DELETE CASCADE,
  sort_order        integer NOT NULL DEFAULT 0,
  pinned_by_user_id text,
  pinned_at         timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, output_id)
);

CREATE INDEX IF NOT EXISTS briefing_finding_pin_org_idx
  ON briefing_finding_pin (org_id, sort_order, pinned_at DESC);
