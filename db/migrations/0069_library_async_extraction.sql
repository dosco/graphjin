-- Durable asynchronous extraction state. Large binary documents are submitted
-- to the librarian and polled by follow-up pg-boss jobs. The normalized
-- Markdown path and active task checkpoint are temporary; hashes and extractor
-- fingerprint remain as compact provenance after successful distillation.
ALTER TABLE library_document
  ADD COLUMN IF NOT EXISTS extract_checkpoint jsonb,
  ADD COLUMN IF NOT EXISTS extracted_relative_path text,
  ADD COLUMN IF NOT EXISTS extracted_content_hash text,
  ADD COLUMN IF NOT EXISTS extractor_fingerprint text,
  ADD COLUMN IF NOT EXISTS extracted_at timestamptz;

ALTER TABLE library_document
  DROP CONSTRAINT IF EXISTS library_document_status_check;

ALTER TABLE library_document
  ADD CONSTRAINT library_document_status_check CHECK (
    status IN (
      'uploaded', 'extracting', 'extracted', 'distilling',
      'cataloged', 'skipped', 'failed'
    )
  );

CREATE INDEX IF NOT EXISTS library_document_async_recovery_idx
  ON library_document (status, updated_at);
