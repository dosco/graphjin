-- Tracks the /work thread/run that produced (or last touched) an
-- action_policy so the rule detail page can offer "view conversation"
-- back to the originating chat. Mirrors the existing
-- workflow_definition.created_by_thread_id / created_by_run_id
-- columns introduced when workflows started being authored from chat.
--
-- Nullable: pre-existing rows and seeded defaults have no originating
-- thread; the column is filled going forward each time the agent
-- calls save_policy from /work (claude-agent backend) or emits a
-- neko_policy_save fence (hermes backend).
--
-- ON DELETE SET NULL so deleting a thread or run does not orphan or
-- delete the rule itself — the audit pointer drops, the rule survives.

ALTER TABLE action_policy
  ADD COLUMN created_by_thread_id uuid REFERENCES work_thread(id) ON DELETE SET NULL,
  ADD COLUMN created_by_run_id uuid REFERENCES work_run(id) ON DELETE SET NULL;
