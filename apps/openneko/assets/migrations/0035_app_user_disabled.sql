-- ADM1 — user lifecycle from chat. disabled_at marks a deactivated user:
-- sign-in rejects them and session validation treats their cookie as
-- dead (the host-side seam the plugins repo's SCIM work will call).

ALTER TABLE app_user
  ADD COLUMN IF NOT EXISTS disabled_at timestamptz;
