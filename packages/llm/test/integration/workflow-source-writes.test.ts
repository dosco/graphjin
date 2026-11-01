import { afterAll, describe, expect, it } from "vitest";
import { db, eq, pool, workflow_run } from "@neko/db";
import { dbReachable, withTestOrg } from "@neko/db/test-helpers";
import { createWorkRun, createWorkThread } from "../../src/work/store";
import {
  appendWorkflowRunSourceWrite,
  createWorkflowRun,
  hasRecentSourceWriteForWorkflow,
  saveWorkflow,
} from "../../src/workflows/store";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[workflow-source-writes] skipping: metadata Postgres unreachable.");
}

describeIfDb("workflow source-write recording", () => {
  afterAll(async () => {
    await pool().end();
  });

  it("atomically de-duplicates replayed row identities for cycle checks", async () => {
    await withTestOrg(async (orgId) => {
      const { workflow } = await saveWorkflow({
        orgId,
        name: "records source writer",
        steps: [{ id: "s1", description: "update a record" }],
      });
      const thread = await createWorkThread(orgId, workflow.name);
      const workRun = await createWorkRun(orgId, thread.id, "ax");
      const run = await createWorkflowRun({
        orgId,
        workflowId: workflow.id,
        threadId: thread.id,
        workRunId: workRun.id,
        triggerKind: "manual",
      });
      const write = {
        orgId,
        workflowRunId: run.id,
        table: "crm__contact",
        primaryKey: { id: "contact-1" },
      };

      await expect(
        Promise.all(Array.from({ length: 8 }, () => appendWorkflowRunSourceWrite(write))),
      ).resolves.toEqual(Array.from({ length: 8 }, () => true));

      const [stored] = await db()
        .select({ sourceWrites: workflow_run.source_writes })
        .from(workflow_run)
        .where(eq(workflow_run.id, run.id));
      expect(stored.sourceWrites).toEqual([
        { table: "crm__contact", primary_key: { id: "contact-1" } },
      ]);
      await expect(
        hasRecentSourceWriteForWorkflow({
          workflowId: workflow.id,
          table: "crm__contact",
          primaryKey: { id: "contact-1" },
          sinceMs: 60_000,
        }),
      ).resolves.toBe(true);
      await expect(
        appendWorkflowRunSourceWrite({ ...write, orgId: "another-org" }),
      ).resolves.toBe(false);
    });
  });
});
