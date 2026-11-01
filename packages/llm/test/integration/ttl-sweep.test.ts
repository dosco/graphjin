import { afterAll, describe, expect, it } from "vitest";
import { and, db, eq, pool, workflow_output } from "@neko/db";
import { dbReachable, withTestOrg } from "@neko/db/test-helpers";
import { sweepStaleWorkflowOutputs } from "../../src/workflows/ttl-sweep";
import {
  createWorkflowRun,
  emitWorkflowOutput,
  saveWorkflow,
} from "../../src/workflows";
import { createWorkRun, createWorkThread } from "../../src/work/store";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[ttl-sweep] skipping: Postgres unreachable.");
}

async function setupOutput(
  orgId: string,
  freshnessTtlSeconds: number | null,
  createdAt?: Date,
) {
  const { workflow } = await saveWorkflow({
    orgId,
    name: `wf-${Math.random().toString(36).slice(2, 7)}`,
    steps: [{ id: "s1", description: "x" }],
  });
  const thread = await createWorkThread(orgId, workflow.name);
  const workRun = await createWorkRun(orgId, thread.id, "ax");
  const wfRun = await createWorkflowRun({
    orgId,
    workflowId: workflow.id,
    threadId: thread.id,
    workRunId: workRun.id,
    triggerKind: "manual",
  });
  const out = await emitWorkflowOutput({
    orgId,
    workflowRunId: wfRun.id,
    workRunId: workRun.id,
    kind: "finding",
    freshnessTtlSeconds,
  });
  if (createdAt) {
    await db()
      .update(workflow_output)
      .set({ created_at: createdAt })
      .where(eq(workflow_output.id, out.id));
  }
  return out;
}

describeIfDb("sweepStaleWorkflowOutputs", () => {
  afterAll(async () => {
    await pool().end();
  });

  it("deletes outputs whose freshness has expired past the grace window", async () => {
    await withTestOrg(async (orgId) => {
      // Created 2 hours ago with a 1-hour TTL → expired.
      const expired = await setupOutput(
        orgId,
        3600,
        new Date(Date.now() - 2 * 3600 * 1000),
      );
      await sweepStaleWorkflowOutputs({ graceSeconds: 60 });
      const remaining = await db()
        .select()
        .from(workflow_output)
        .where(
          and(
            eq(workflow_output.org_id, orgId),
            eq(workflow_output.id, expired.id),
          ),
        );
      expect(remaining).toHaveLength(0);
    });
  });

  it("keeps outputs with null freshness_ttl_seconds (intended permanent)", async () => {
    await withTestOrg(async (orgId) => {
      const permanent = await setupOutput(orgId, null);
      await sweepStaleWorkflowOutputs({ graceSeconds: 0 });
      const remaining = await db()
        .select()
        .from(workflow_output)
        .where(
          and(
            eq(workflow_output.org_id, orgId),
            eq(workflow_output.id, permanent.id),
          ),
        );
      expect(remaining).toHaveLength(1);
    });
  });

  it("keeps outputs still inside their freshness window", async () => {
    await withTestOrg(async (orgId) => {
      const fresh = await setupOutput(orgId, 3600);
      await sweepStaleWorkflowOutputs({ graceSeconds: 60 });
      const remaining = await db()
        .select()
        .from(workflow_output)
        .where(
          and(
            eq(workflow_output.org_id, orgId),
            eq(workflow_output.id, fresh.id),
          ),
        );
      expect(remaining).toHaveLength(1);
    });
  });

  it("keeps outputs inside the grace window even when freshness expired", async () => {
    await withTestOrg(async (orgId) => {
      // Created 61 minutes ago with a 60-min TTL → 1 min past freshness.
      // Sweep grace is 10 min → keep.
      const justExpired = await setupOutput(
        orgId,
        3600,
        new Date(Date.now() - 61 * 60 * 1000),
      );
      await sweepStaleWorkflowOutputs({ graceSeconds: 600 });
      const remaining = await db()
        .select()
        .from(workflow_output)
        .where(
          and(
            eq(workflow_output.org_id, orgId),
            eq(workflow_output.id, justExpired.id),
          ),
        );
      expect(remaining).toHaveLength(1);
    });
  });
});
