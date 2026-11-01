-- Document library: uploads promoted into a per-user library and
-- distilled by the librarian job into OKF concept docs.
--
-- library_document tracks each raw upload's lifecycle
-- (uploaded -> distilling -> cataloged | skipped | failed); the file
-- itself stays in the org workspace (uploads/<threadId>/...), which is
-- what concept sources cite.
--
-- library_concept is the distilled layer. NULL user_id = team layer
-- (approved, org-visible); non-NULL = the owner's personal layer,
-- invisible to everyone else — the same overlay rule as work_memory.
-- Team entry is explicit: share creates a draft with promoted_* lineage,
-- admin approval flips it to stable and appends to verified. The
-- embedding powers library search; vectors come from transformers.js
-- in-process, same as work_memory.

create table if not exists library_document (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  user_id text references app_user(id) on delete cascade,
  source_thread_id uuid references work_thread(id) on delete set null,
  filename text not null,
  relative_path text not null,
  content_hash text not null,
  size_bytes integer not null,
  status text not null default 'uploaded',
  skip_reason text,
  error text,
  distilled_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  constraint library_document_status_check check (
    status in ('uploaded', 'distilling', 'cataloged', 'skipped', 'failed')
  )
);

create index if not exists library_document_org_recent_idx
  on library_document (org_id, created_at desc);
create index if not exists library_document_org_user_idx
  on library_document (org_id, user_id, status);
create index if not exists library_document_org_hash_idx
  on library_document (org_id, content_hash);

create table if not exists library_concept (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  user_id text references app_user(id) on delete cascade,
  path text not null,
  type text not null,
  title text not null,
  description text,
  tags jsonb not null default '[]'::jsonb,
  body text not null,
  status text not null default 'draft',
  sources jsonb not null default '[]'::jsonb,
  generated_by text,
  generated_at timestamptz,
  verified jsonb not null default '[]'::jsonb,
  stale_after date,
  source_document_id uuid references library_document(id) on delete set null,
  promoted_from_id uuid,
  promoted_by text,
  promoted_at timestamptz,
  archived_at timestamptz,
  embedding vector(384),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  constraint library_concept_status_check check (
    status in ('draft', 'stable', 'deprecated')
  )
);

create index if not exists library_concept_org_layer_path_idx
  on library_concept (org_id, user_id, path);
create index if not exists library_concept_org_status_idx
  on library_concept (org_id, status, archived_at, updated_at desc);
create index if not exists library_concept_org_user_idx
  on library_concept (org_id, user_id, archived_at);

-- One path per (org, layer): expression index because NULL user_id rows
-- (team layer) would otherwise never collide under a plain unique index.
create unique index if not exists library_concept_org_layer_path_unique
  on library_concept (org_id, coalesce(user_id, ''), path)
  where archived_at is null;

-- Deliberately NO ivfflat index here (unlike work_memory 0014): an
-- ivfflat index trained on an empty table probes badly and silently
-- drops rows from top-N results on small corpora. Concept counts stay
-- well under the scale where exact scan hurts; add an ANN index (and
-- tune probes) if a deployment ever grows past that.
