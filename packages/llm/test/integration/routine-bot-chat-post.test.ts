import { afterAll, describe, expect, it } from "vitest";
import { and, app_user, bot, db, eq, isNull, pool, work_message } from "@neko/db";
import { dbReachable, withTestOrg } from "@neko/db/test-helpers";
import { createWorkRun, createWorkThread } from "../../src/work/store";
import {
  createWorkflowRun,
  emitWorkflowOutput,
  finishWorkflowRun,
  saveWorkflow,
} from "../../src/workflows/store";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

describeIfDb("routine runs post how they ended to the bot chat", () => {
  afterAll(async () => {
    await pool().end();
  });

  async function setup(orgId: string) {
    const userId = `${orgId}-owner`;
    await db().insert(app_user).values({ id: userId, email: "owner@example.com", org_id: orgId });
    const chat = await createWorkThread(orgId, "Helper", "web", userId);
    await db().insert(bot).values({ org_id: orgId, owner_user_id: userId, name: "Helper", coat: "ginger", pattern: "solid", thread_id: chat.id });
    const { workflow } = await saveWorkflow({
      orgId,
      name: "Order check",
      steps: [{ id: "s1", description: "Check the order" }],
      createdByThreadId: chat.id,
      ownerUserId: userId,
    });
    const startRun = async (threadId: string) => {
      const workRun = await createWorkRun(orgId, threadId, "hermes");
      const run = await createWorkflowRun({
        orgId,
        workflowId: workflow.id,
        threadId,
        workRunId: workRun.id,
        triggerKind: "api",
        executionMode: "single",
      });
      return { run, workRun };
    };
    const posted = () =>
      db()
        .select({ content: work_message.content })
        .from(work_message)
        .where(and(eq(work_message.thread_id, chat.id), isNull(work_message.run_id)));
    return { chat, startRun, posted };
  }

  it("posts the published result once, however often the run publishes it", async () => {
    await withTestOrg(async (orgId) => {
      const { startRun, posted } = await setup(orgId);
      const { run, workRun } = await startRun((await createWorkThread(orgId, "Order check", "workflow")).id);
      for (let i = 0; i < 2; i += 1) {
        await emitWorkflowOutput({ orgId, workflowRunId: run.id, workRunId: workRun.id, kind: "finding", title: "Order 7", body: "Order 7 ships Friday." });
      }
      expect(await posted()).toEqual([]);
      await finishWorkflowRun({ workflowRunId: run.id, status: "completed", summary: "Order 7 ships Friday." });
      expect(await posted()).toEqual([{ content: "**Order 7**\n\nOrder 7 ships Friday." }]);
    }, "routine-post");
  });

  it("posts the answer when nothing was published, and why a run failed", async () => {
    await withTestOrg(async (orgId) => {
      const { startRun, posted } = await setup(orgId);
      const done = await startRun((await createWorkThread(orgId, "Order check", "workflow")).id);
      await finishWorkflowRun({ workflowRunId: done.run.id, status: "completed", summary: "All orders ship on time." });
      const failed = await startRun((await createWorkThread(orgId, "Order check", "workflow")).id);
      await finishWorkflowRun({ workflowRunId: failed.run.id, status: "failed", error: "The data source timed out." });
      expect((await posted()).map((row) => row.content)).toEqual([
        "**Order check**\n\nAll orders ship on time.",
        "**Order check failed.**\n\nThe data source timed out.",
      ]);
    }, "routine-post");
  });

  it("posts nothing for a run that already lives in the bot chat", async () => {
    await withTestOrg(async (orgId) => {
      const { chat, startRun, posted } = await setup(orgId);
      const { run } = await startRun(chat.id);
      await finishWorkflowRun({ workflowRunId: run.id, status: "completed", summary: "All orders ship on time." });
      expect(await posted()).toEqual([]);
    }, "routine-post");
  });
});
