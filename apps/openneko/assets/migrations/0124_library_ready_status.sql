-- Library uploads now live in the customer GraphJin's neko_library file
-- source, which searches and reads them directly. A stored file is ready as
-- soon as it is written; there is no extraction or distillation step.
ALTER TABLE library_document
  DROP CONSTRAINT IF EXISTS library_document_status_check;

ALTER TABLE library_document
  ADD CONSTRAINT library_document_status_check CHECK (
    status IN (
      'uploaded', 'extracting', 'extracted', 'distilling',
      'cataloged', 'skipped', 'failed', 'ready'
    )
  );
