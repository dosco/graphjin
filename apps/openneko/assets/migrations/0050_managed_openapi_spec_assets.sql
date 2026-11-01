-- Managed, organization-scoped OpenAPI documents. Import is admin-only and
-- activation occurs only inside the approved source_config_admin worker.

create table if not exists openapi_spec_asset (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  created_by_user_id text references app_user(id) on delete set null,
  source_type text not null check (source_type in ('url', 'upload')),
  original_name text not null,
  source_url text,
  content text not null,
  checksum_sha256 text not null,
  document_format text not null default 'yaml' check (document_format in ('yaml', 'json')),
  title text not null,
  version text,
  base_url text not null,
  server_urls text[] not null default '{}',
  auth_schemes text[] not null default '{}',
  warnings text[] not null default '{}',
  operation_count integer not null default 0,
  read_operation_count integer not null default 0,
  mutating_operation_count integer not null default 0,
  status text not null default 'staged' check (status in ('staged', 'active', 'superseded')),
  source_name text,
  activated_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists openapi_spec_asset_org_created_idx
  on openapi_spec_asset (org_id, created_at desc);

create index if not exists openapi_spec_asset_org_checksum_idx
  on openapi_spec_asset (org_id, checksum_sha256);
