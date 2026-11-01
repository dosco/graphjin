-- Audit trail for the document library, mirroring work_memory_event:
-- an append-only log of document and concept transitions (created,
-- distilled, skipped, failed, upserted, shared, approved, declined,
-- deprecated, archived, removed). bigserial is the ordering key.

create table if not exists library_event (
  id bigserial primary key,
  org_id text not null references organization(id) on delete cascade,
  document_id uuid references library_document(id) on delete set null,
  concept_id uuid references library_concept(id) on delete set null,
  user_id text,
  action text not null,
  payload jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now()
);

create index if not exists library_event_concept_idx
  on library_event (concept_id, id desc);
create index if not exists library_event_org_recent_idx
  on library_event (org_id, id desc);
