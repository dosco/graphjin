-- Resumable distillation: persist per-chunk progress for the librarian so a
-- large document that fails partway through is retried from the next chunk
-- instead of re-running every prior (billed) LLM call. The blob also records
-- the outline-selected section indices so a resume rebuilds the identical
-- chunk plan without repeating the outline pass. NULL when idle or complete.
ALTER TABLE library_document
  ADD COLUMN IF NOT EXISTS distill_checkpoint jsonb;
