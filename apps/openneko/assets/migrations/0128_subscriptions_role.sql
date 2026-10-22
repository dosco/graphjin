-- Workflow subscriptions are served by a GraphJin inside `openneko serve`,
-- which reads neko-db as this role. It may log in only once
-- graphjin-secret-init sets its password, which is derived from the
-- OpenNeko secret key, and it reads workflow_output and nothing else.
do $$
begin
  if not exists (select 1 from pg_roles where rolname = 'neko_subscriptions') then
    create role neko_subscriptions nologin;
  end if;
end
$$;

grant usage on schema public to neko_subscriptions;
grant select on table workflow_output to neko_subscriptions;
