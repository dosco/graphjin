-- =========================================================
-- Rename "claude-sdk" agent backend identifier to "claude-agent".
--
-- The settings UI used to show "Claude Agent SDK" with internal id
-- "claude-sdk". The label is now "Claude Agent" with id "claude-agent" —
-- this migration converts existing rows so an org that picked the SDK
-- backend before the rename keeps working without re-running /setup.
--
-- Touches llm_provider_config rows where scope='agent':
--   - config.backend: 'claude-sdk' → 'claude-agent'
--   - config.claudeSdkCap key → claudeAgentCap (same value)
--
-- Idempotent: safe to re-run.
-- =========================================================

update llm_provider_config
   set config = jsonb_set(config, '{backend}', '"claude-agent"', true)
 where scope = 'agent'
   and config->>'backend' = 'claude-sdk';

update llm_provider_config
   set config = (config - 'claudeSdkCap')
              || jsonb_build_object('claudeAgentCap', config->'claudeSdkCap')
 where scope = 'agent'
   and config ? 'claudeSdkCap';
