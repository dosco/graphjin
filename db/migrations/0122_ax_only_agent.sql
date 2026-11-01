-- Ax Harness is OpenNeko's only agent runtime. The org-wide backend choice is
-- gone, so agent-scoped rows keep only runtime-wide settings such as globalCap
-- and name the runtime that reads them. Model/provider credentials remain in
-- their dedicated primary/research rows.

update llm_provider_config
set provider = 'ax',
    config = coalesce(config, '{}'::jsonb) - 'backend',
    updated_at = now()
where scope = 'agent'
  and (provider is distinct from 'ax' or coalesce(config, '{}'::jsonb) ? 'backend');

-- A run still waiting in the queue will execute on Ax, so attribute it to Ax.
-- Runs that already started or finished keep the backend that ran them.
update work_run
set backend = 'ax',
    updated_at = now()
where status = 'queued'
  and backend is distinct from 'ax';
