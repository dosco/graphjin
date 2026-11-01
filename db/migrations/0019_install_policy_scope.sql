-- =========================================================
-- Extend llm_provider_config.scope to accept 'install-policy'
--
-- The install policy gates the four trust-floor switches for
-- plugin + skill installs (allowUnverified, allowGitUrlInstalls,
-- allowedMarketplaces, allowSandboxedSkillEscape). Stored
-- alongside primary/research/agent scopes in llm_provider_config,
-- same single-row-per-org pattern. Config jsonb carries the
-- policy; secrets/provider/model fields are unused.
--
-- Drops + re-adds the CHECK so the constraint list stays a
-- single source of truth (rather than a chain of ADD CHECKs).
-- Idempotent: safe to re-run.
-- =========================================================

alter table llm_provider_config
  drop constraint if exists llm_provider_config_scope_check;

alter table llm_provider_config
  add constraint llm_provider_config_scope_check
  check (scope in ('primary', 'research', 'agent', 'install-policy'));
