-- =========================================================
-- Extend llm_provider_config.scope to accept 'graphjin-config'
--
-- Stores the operator runtime switch for Ask/MCP-driven GraphJin source
-- configuration. The row's enabled flag and config.sourceConfigEnabled are
-- kept in sync by the admin UI.
--
-- Drops + re-adds the CHECK so the constraint list stays a single source of
-- truth. Idempotent: safe to re-run.
-- =========================================================

alter table llm_provider_config
  drop constraint if exists llm_provider_config_scope_check;

alter table llm_provider_config
  add constraint llm_provider_config_scope_check
  check (scope in ('primary', 'research', 'agent', 'install-policy', 'graphjin-config'));
