-- Extend llm_provider_config.scope to accept 'semantic-search': the admin
-- switch for meaning-based search, off when no row exists.

alter table llm_provider_config
  drop constraint if exists llm_provider_config_scope_check;

alter table llm_provider_config
  add constraint llm_provider_config_scope_check
  check (scope in ('primary', 'research', 'agent', 'install-policy', 'graphjin-config', 'graphjin-agent', 'public-url', 'semantic-search'));
