-- =========================================================
-- Agent backend + admin-setup gate
--
-- Adds organization.setup_complete_at to mark when admin onboarding
-- (data source + agent backend + primary provider) is finished. The
-- /onboarding (business profile) wizard reads this column to decide
-- whether to redirect to /setup.
--
-- Adds a CHECK constraint on llm_provider_config.scope for the new
-- 'agent' scope (in addition to existing 'primary' / 'research').
-- 'agent' rows store { backend: 'hermes' | 'claude-agent' } in the
-- config jsonb. secrets/model are unused at this scope — the agent
-- pulls Anthropic creds from the primary scope when backend=claude-agent.
--
-- Backfills existing orgs that already have a data source + an enabled
-- primary provider; they skip /setup on next visit.
-- Idempotent: safe to re-run.
-- =========================================================

alter table organization
  add column if not exists setup_complete_at timestamptz;

do $$
begin
  if not exists (
    select 1 from pg_constraint
     where conname = 'llm_provider_config_scope_check'
  ) then
    alter table llm_provider_config
      add constraint llm_provider_config_scope_check
      check (scope in ('primary', 'research', 'agent'));
  end if;
end$$;

update organization o
   set setup_complete_at = now()
 where setup_complete_at is null
   and exists (select 1 from data_source where org_id = o.id)
   and exists (
     select 1 from llm_provider_config
      where org_id = o.id and scope = 'primary' and enabled
   );
