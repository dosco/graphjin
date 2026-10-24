import { afterAll, describe, expect, it } from "vitest";
import { pool } from "@neko/db";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import {
  createActionRequest,
  createObservation,
  createSubscription,
  createWorkflowRun,
  emitWorkflowOutput,
  getActionRequest,
  getObservation,
  getWorkflow,
  getWorkflowByOrgName,
  listActionRequests,
  listObservationsForOutput,
  listSubscriptionsByWorkflow,
  listWorkflows,
  saveWorkflow,
} from "../../src/workflows";
import { createWorkRun, createWorkThread } from "../../src/work/store";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[multi-org-isolation] skipping: Postgres unreachable.");
}

describeIfDb("multi-org isolation", () => {
  afterAll(async () => {
    await pool().end();
  });

  it("listing endpoints scope to org_id — org B never sees org A's data", async () => {
    const orgA = uniqueOrgId("org-a");
    const orgB = uniqueOrgId("org-b");
    await createTestOrg(orgA, "Org A");
    await createTestOrg(orgB, "Org B");

    try {
      // Populate org A end-to-end.
      const { workflow: wfA } = await saveWorkflow({
        orgId: orgA,
        name: "secrets-workflow",
        steps: [{ id: "s1", description: "x" }],
      });
      const threadA = await createWorkThread(orgA, "secret thread");
      const workRunA = await createWorkRun(orgA, threadA.id, "ax");
      const wfRunA = await createWorkflowRun({
        orgId: orgA,
        workflowId: wfA.id,
        threadId: threadA.id,
        workRunId: workRunA.id,
        triggerKind: "manual",
      });
      const outputA = await emitWorkflowOutput({
        orgId: orgA,
        workflowRunId: wfRunA.id,
        workRunId: workRunA.id,
        kind: "finding",
        scope: "private",
      });
      const subA = await createSubscription({
        orgId: orgA,
        workflowId: wfA.id,
        sourceKind: "workflow_output",
      });
      const obsA = await createObservation({
        orgId: orgA,
        sourceOutputId: outputA.id,
        consumerKind: "workflow",
        consumerWorkflowId: wfA.id,
        subscriptionId: subA.id,
      });
      const actA = await createActionRequest({
        orgId: orgA,
        workflowRunId: wfRunA.id,
        scope: "external",
        kind: "send_webhook",
        payload: { url: "https://example/A" },
        status: "pending_approval",
        summary: "secret action for org A",
      });

      // Org B sees nothing of org A's via list paths.
      expect((await listWorkflows(orgB)).map((w) => w.id)).not.toContain(wfA.id);
      expect(await getWorkflow(orgB, wfA.id)).toBeNull();
      expect(await getWorkflowByOrgName(orgB, "secrets-workflow")).toBeNull();
      expect(
        (await listSubscriptionsByWorkflow(orgB, wfA.id)).map((s) => s.id),
      ).not.toContain(subA.id);
      expect(await getObservation(orgB, obsA.id)).toBeNull();
      expect(
        (await listObservationsForOutput(orgB, outputA.id)).map((o) => o.id),
      ).not.toContain(obsA.id);
      expect(await getActionRequest(orgB, actA.id)).toBeNull();
      expect(
        (await listActionRequests({ orgId: orgB })).map((r) => r.id),
      ).not.toContain(actA.id);

      // Org A still sees its own data — proves the filter is on org_id,
      // not just dropping everything.
      expect((await listWorkflows(orgA)).map((w) => w.id)).toContain(wfA.id);
      expect((await getWorkflow(orgA, wfA.id))?.id).toBe(wfA.id);
      expect(
        (await listSubscriptionsByWorkflow(orgA, wfA.id)).map((s) => s.id),
      ).toContain(subA.id);
      expect((await getObservation(orgA, obsA.id))?.id).toBe(obsA.id);
      expect((await getActionRequest(orgA, actA.id))?.id).toBe(actA.id);
    } finally {
      await deleteTestOrg(orgA);
      await deleteTestOrg(orgB);
    }
  });
});
