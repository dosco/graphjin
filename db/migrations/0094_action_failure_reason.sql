-- Executor failures and human rejections have distinct meanings.
ALTER TABLE action_request ADD COLUMN IF NOT EXISTS failure_reason text;

UPDATE action_request
SET failure_reason = COALESCE(failure_reason, rejection_reason),
    rejection_reason = NULL
WHERE status = 'failed' AND rejection_reason IS NOT NULL;
