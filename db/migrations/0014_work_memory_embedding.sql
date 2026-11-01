-- Add a 384-dim embedding to work_memory so the agent can retrieve
-- memories by semantic context instead of keyword. Vectors come from
-- transformers.js (Xenova/all-MiniLM-L6-v2) computed in-process by the
-- worker on insert/update — no external embedding API.
--
-- Requires the pgvector image (compose.yml uses pgvector/pgvector:pg16).

CREATE EXTENSION IF NOT EXISTS vector;

ALTER TABLE work_memory
  ADD COLUMN IF NOT EXISTS embedding vector(384);

-- IVFFlat is fine for the small corpus we expect here (rules-flavored,
-- typically <10k rows). lists=100 is the sqrt(N) default for that scale.
-- Cosine is the natural distance for sentence embeddings; <=> operator.
CREATE INDEX IF NOT EXISTS work_memory_embedding_cosine_idx
  ON work_memory USING ivfflat (embedding vector_cosine_ops)
  WITH (lists = 100);
