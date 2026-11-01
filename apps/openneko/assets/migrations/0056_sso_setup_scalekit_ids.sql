-- SSO setup gains the active Scalekit environment/organization identity
-- (Dev by default; Prod is an explicit opt-in later). Kept on sso_setup so
-- the poller + settings page agree on which environment they operate on.

alter table if exists sso_setup
  add column if not exists scalekit_environment_id text,
  add column if not exists scalekit_organization_id text,
  add column if not exists scalekit_environment_tier text;
