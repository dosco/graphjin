-- =========================================================
-- skill_embedding: the semantic index of each org's installed skills.
-- The embedding job queue fills a null vector. Idempotent: safe to re-run.
-- =========================================================

create table if not exists skill_embedding (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  skill_name text not null,
  document text not null default '',
  embedding vector(384),
  updated_at timestamptz not null default now(),
  unique (org_id, skill_name)
);

create index if not exists skill_embedding_pending_idx
  on skill_embedding (org_id) where embedding is null;
