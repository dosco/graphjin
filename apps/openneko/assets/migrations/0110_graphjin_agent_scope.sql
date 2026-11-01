-- =========================================================
-- Extend llm_provider_config.scope to accept 'graphjin-agent'
--
-- Org-wide opt-in for GraphJin's server-side agent. enabled turns it on.
-- provider 'primary' reuses the primary provider and model; any other
-- provider carries its own model, config and encrypted key.
--
-- Drops + re-adds the CHECK so the constraint list stays a single source of
-- truth. Idempotent: safe to re-run.
-- =========================================================

alter table llm_provider_config
  drop constraint if exists llm_provider_config_scope_check;

alter table llm_provider_config
  add constraint llm_provider_config_scope_check
  check (scope in ('primary', 'research', 'agent', 'install-policy', 'graphjin-config', 'graphjin-agent'));
