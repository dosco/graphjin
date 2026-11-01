-- The owner is resolved lazily only after confirming auth is off. Never infer
-- a solo identity for an existing SSO installation during schema migration.
alter table organization add column if not exists solo_admin_user_id text
  references app_user(id) on delete set null;
