-- Extend llm_provider_config.scope to accept 'public-url': the address that
-- API callers use to reach OpenNeko. Workflow API examples start with it.

alter table llm_provider_config
  drop constraint if exists llm_provider_config_scope_check;

alter table llm_provider_config
  add constraint llm_provider_config_scope_check
  check (scope in ('primary', 'research', 'agent', 'install-policy', 'graphjin-config', 'graphjin-agent', 'public-url'));
