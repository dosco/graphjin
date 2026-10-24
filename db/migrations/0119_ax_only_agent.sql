-- Ax is OpenNeko's sole agent runtime; the Hermes backend has been removed.
-- Agent-scoped rows keep only runtime-wide settings such as globalCap.
-- Historical work_run.backend and skill_usage.source values are left as
-- recorded.

update llm_provider_config
set provider = 'ax',
    config = jsonb_set(coalesce(config, '{}'::jsonb), '{backend}', '"ax"'),
    updated_at = now()
where scope = 'agent';
