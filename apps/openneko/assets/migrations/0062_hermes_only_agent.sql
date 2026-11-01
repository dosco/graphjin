-- Hermes is OpenNeko's sole agent runtime. Agent-scoped rows retain only
-- runtime-wide settings such as globalCap; model/provider credentials remain
-- in their dedicated primary/research rows.

update llm_provider_config
set provider = 'hermes',
    model = null,
    config = coalesce(config, '{}'::jsonb)
      - 'backend'
      - 'claudeSdkCap'
      - 'claudeAgentCap',
    secrets = '{}'::jsonb,
    enabled = true,
    updated_at = now()
where scope = 'agent';
