-- Semantic search moves from all-MiniLM-L6-v2 (384 dimensions) to
-- EmbeddingGemma 2 (768 dimensions), behind an admin switch that is off by
-- default (llm_provider_config scope 'semantic-search'; no row = off, so no
-- new table is needed).
--
-- MiniLM vectors cannot be compared with EmbeddingGemma vectors, so both
-- vector columns are cleared and retyped. A null vector is the embedding
-- backlog: it is indexed in the background only once an admin turns semantic
-- search on. Until then, and for rows not indexed yet, search uses the
-- full-text indexes below.
--
-- Neither column has a vector index today (0028 dropped the IVFFlat index on
-- work_memory; skill_embedding never had one). ALTER COLUMN TYPE rebuilds any
-- index an operator added, with the same access method and operator class.
--
-- Idempotent: a column already at 768 dimensions is left alone, so stored
-- EmbeddingGemma vectors survive a re-run.

create extension if not exists vector;

do $$
begin
  if exists (
    select 1 from pg_attribute
    where attrelid = 'public.work_memory'::regclass
      and attname = 'embedding' and not attisdropped and atttypmod <> 768
  ) then
    alter table public.work_memory
      alter column embedding type vector(768) using null::vector(768);
  end if;
  if exists (
    select 1 from pg_attribute
    where attrelid = 'public.skill_embedding'::regclass
      and attname = 'embedding' and not attisdropped and atttypmod <> 768
  ) then
    alter table public.skill_embedding
      alter column embedding type vector(768) using null::vector(768);
  end if;
end
$$;

-- Keyword search: used when semantic search is off, when the embedding
-- service fails or is slow, and for rows the backfill has not reached. The
-- expressions match the queries in packages/llm (work/memory.ts,
-- work/skill-index.ts) so the planner can use them.
create index if not exists work_memory_text_fts_idx
  on work_memory using gin (to_tsvector('english', text));
create index if not exists skill_embedding_document_fts_idx
  on skill_embedding using gin (to_tsvector('english', document));
