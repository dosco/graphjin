-- SEC6 — memory integrity + TTL. integrity_hmac is an HMAC over the
-- row's identity-bearing fields with a per-org key derived from the
-- deployment secret; readers drop rows whose hash no longer matches
-- (DB-level tampering / poisoning). NULL = legacy row written before
-- SEC6, treated as unverified-but-trusted. expires_at drives the sweep
-- that archives short-lived kinds (thread_note) per DATA_LIFECYCLE §4.

ALTER TABLE work_memory
  ADD COLUMN IF NOT EXISTS integrity_hmac text,
  ADD COLUMN IF NOT EXISTS expires_at timestamptz;

CREATE INDEX IF NOT EXISTS work_memory_expiry_idx
  ON work_memory (expires_at)
  WHERE expires_at IS NOT NULL AND archived_at IS NULL;
