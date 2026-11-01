-- Drop pg-boss queue rows + partition tables for queues we no longer use.
--
-- vitest_*  : leftovers from earlier test runs against a shared dev DB.
--             Tests have since moved to ephemeral DBs but the queue rows
--             from the old runs persist in older installs.
-- work_auto_memory : removed when the auto-classifier was dropped in favor
--             of explicit `save:` and the agent-side neko_memory fence.
--
-- Idempotent: if a queue isn't present, the loop does nothing for it.
-- The explicit DROP TABLE is needed because pg-boss 10.x doesn't cascade
-- the partition table when a queue row is deleted from pgboss.queue.

DO $$
DECLARE
  q RECORD;
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.tables
     WHERE table_schema = 'pgboss' AND table_name = 'queue'
  ) THEN
    RETURN;
  END IF;

  FOR q IN
    SELECT name, partition_name FROM pgboss.queue
     WHERE name LIKE 'vitest_%' OR name = 'work_auto_memory'
  LOOP
    EXECUTE format('DROP TABLE IF EXISTS pgboss.%I', q.partition_name);
    DELETE FROM pgboss.queue WHERE name = q.name;
  END LOOP;
END
$$;
