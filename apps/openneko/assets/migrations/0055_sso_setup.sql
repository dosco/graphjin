-- SSO setup state machine + configurable group→role/persona mapping.
--
-- sso_setup tracks the one-time Scalekit/IdP admin flow: the agent (or the
-- /settings/sso page) generates an admin portal link, the operator configures
-- the downstream IdP, and a poller reads connection state back until the
-- connection reports COMPLETED. One row per org (single-tenant today).
--
-- sso_group_mapping lets the operator override the default admin/admins/owners
-- heuristic: a matched group drives app_user.role and the auto-provisioned
-- per-user persona.

create table if not exists sso_setup (
  org_id text primary key references organization(id) on delete cascade,
  status text not null default 'not_started',
  portal_link text,
  portal_link_expires_at timestamptz,
  provider text,
  last_polled_at timestamptz,
  last_error text,
  setup_completed_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  constraint sso_setup_status_check check (
    status in ('not_started', 'awaiting_portal', 'polling', 'connected', 'failed')
  )
);

create table if not exists sso_group_mapping (
  org_id text not null references organization(id) on delete cascade,
  provider text not null,
  group_external_id text not null,
  role text not null,
  persona_role_template text,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  primary key (org_id, provider, group_external_id),
  constraint sso_group_mapping_role_check check (role in ('admin', 'member'))
);
