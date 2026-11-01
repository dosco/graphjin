-- CV1 — workflow ownership. owner_user_id = '' is the org layer (every
-- existing workflow); a member's personal workflow carries their user id,
-- so the same name can exist per layer: unique moves from (org, name) to
-- (org, owner, name). origin_id is the stable identity across
-- copy/promote lineage (self for originals); parent_id points at the
-- workflow this one was forked/promoted from.

ALTER TABLE workflow_definition
  ADD COLUMN IF NOT EXISTS owner_user_id text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS origin_id uuid,
  ADD COLUMN IF NOT EXISTS parent_id uuid REFERENCES workflow_definition(id) ON DELETE SET NULL;

UPDATE workflow_definition SET origin_id = id WHERE origin_id IS NULL;

DROP INDEX IF EXISTS workflow_definition_org_name_unique;
CREATE UNIQUE INDEX IF NOT EXISTS workflow_definition_org_owner_name_unique
  ON workflow_definition (org_id, owner_user_id, name);
CREATE INDEX IF NOT EXISTS workflow_definition_org_owner_idx
  ON workflow_definition (org_id, owner_user_id);
