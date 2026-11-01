// OL8 — identical findings within 24h bump the original card's
// seen_count instead of stacking duplicates; different titles, kinds or
// workflows still create new cards.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { db, eq, pool, workflow_output } from "@neko/db";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import {
  createWorkflowRun,
  emitWorkflowOutput,
  saveWorkflow,
} from "../../src/workflows/store";
import { createWorkRun, createWorkThread } from "../../src/work/store";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[output-dedupe] skipping: Postgres unreachable.");
}

describeIfDb("OL8 workflow-output dedupe", () => {
  const orgId = uniqueOrgId("ol8");
  let workflowRunId: string;
  let secondRunId: string;
  let workRunId: string;

  beforeAll(async () => {
    await createTestOrg(orgId);
    const { workflow } = await saveWorkflow({
      orgId,
      name: "Churn watch",
      steps: [{ id: "s1", description: "watch" }],
    });
    const thread = await createWorkThread(orgId, "t");
    const workRun = await createWorkRun(orgId, thread.id, "ax");
    workRunId = workRun.id;
    workflowRunId = (
      await createWorkflowRun({
        orgId,
        workflowId: workflow.id,
        threadId: thread.id,
        workRunId,
        triggerKind: "manual",
      })
    ).id;
    const secondWorkRun = await createWorkRun(orgId, thread.id, "ax");
    secondRunId = (
      await createWorkflowRun({
        orgId,
        workflowId: workflow.id,
        threadId: thread.id,
        workRunId: secondWorkRun.id,
        triggerKind: "manual",
      })
    ).id;
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
    await pool().end();
  });

  it("the same finding from a later run bumps the original card", async () => {
    const first = await emitWorkflowOutput({
      orgId,
      workflowRunId,
      workRunId,
      kind: "finding",
      title: "Refund rate doubled",
      body: "Refunds at 4.2%",
      mood: "act",
    });
    expect(first.seenCount).toBe(1);

    const second = await emitWorkflowOutput({
      orgId,
      workflowRunId: secondRunId,
      workRunId,
      kind: "finding",
      title: "Refund rate doubled",
      body: "Refunds at 4.3%",
      mood: "act",
    });
    expect(second.id).toBe(first.id);
    expect(second.seenCount).toBe(2);
    expect(second.lastSeenAt.getTime()).toBeGreaterThanOrEqual(
      first.lastSeenAt.getTime(),
    );

    const rows = await db()
      .select()
      .from(workflow_output)
      .where(eq(workflow_output.org_id, orgId));
    expect(rows.filter((r) => r.title === "Refund rate doubled")).toHaveLength(1);
  });

  it("a different title is a new card; untitled outputs never dedupe", async () => {
    const other = await emitWorkflowOutput({
      orgId,
      workflowRunId,
      workRunId,
      kind: "finding",
      title: "Stock coverage below 14 days",
      mood: "act",
    });
    expect(other.seenCount).toBe(1);

    const blankA = await emitWorkflowOutput({
      orgId,
      workflowRunId,
      workRunId,
      kind: "log",
    });
    const blankB = await emitWorkflowOutput({
      orgId,
      workflowRunId,
      workRunId,
      kind: "log",
    });
    expect(blankB.id).not.toBe(blankA.id);
  });
});
