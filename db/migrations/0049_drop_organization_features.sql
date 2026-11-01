-- =========================================================
-- Drop organization.features.
--
-- Speculative open-core entitlement scaffolding: a comma-separated
-- feature-key gate that nothing reads now that per-feature gating is
-- removed. OpenNeko ships Apache-2.0 with no paywalled features.
--
-- Idempotent: safe to re-run.
-- =========================================================

alter table organization drop column if exists features;
