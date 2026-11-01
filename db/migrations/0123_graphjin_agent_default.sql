-- GraphJin's agent is now the default data path: with no graphjin-agent row,
-- agent runs ask GraphJin's agent. Orgs that already reach an external
-- GraphJin used its direct tools, and that GraphJin's agent may not be set up,
-- so they keep direct tools: save the admin's direct opt-in for every org
-- whose default data source is not the in-stack GraphJin. In-stack is the
-- `graphjin` service in Docker, or localhost in development. Orgs that saved a
-- choice keep it, and orgs without a data source take the new default.

insert into llm_provider_config (org_id, scope, provider, enabled)
select source.org_id, 'graphjin-agent', 'primary', false
from (
  select distinct on (org_id) org_id, graphql_url
  from data_source
  order by org_id, is_default desc, created_at
) source
where source.graphql_url !~* '^https?://(graphjin|localhost|127\.0\.0\.1)(:[0-9]+)?/'
on conflict (org_id, scope) do nothing;
