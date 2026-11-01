alter table workflow_definition
  add column if not exists network_hosts text[] not null default '{}';
