-- =========================================================
-- Drop organization.plan.
--
-- Speculative SaaS-billing scaffolding that nothing in the app reads or
-- writes outside the auto-org seed. Removed to keep the schema honest;
-- if/when billing tiers land they'll come back with the feature, not
-- before it.
--
-- Idempotent: safe to re-run.
-- =========================================================

alter table organization drop column if exists plan;
