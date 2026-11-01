import { afterAll, describe, expect, it } from "vitest";
import { pool } from "@neko/db";
import { dbReachable, withTestOrg } from "@neko/db/test-helpers";
import {
  createObservation,
  createWorkflowRun,
  emitWorkflowOutput,
  saveWorkflow,
} from "../../src/workflows/store";
import { createWorkRun, createWorkThread } from "../../src/work/store";
import { isWorkflowInAncestorChain } from "../../src/workflows/cycle-detection";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[cycle-detection-integration] skipping: Postgres unreachable.");
}

describeIfDb("isWorkflowInAncestorChain", () => {
  afterAll(async () => {
    await pool().end();
  });

  it("returns false for a run with no chain ancestors", async () => {
    await withTestOrg(async (orgId) => {
      const { workflow } = await saveWorkflow({
        orgId,
        name: "lonely",
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
      const cycle = await isWorkflowInAncestorChain(
        wfRun.id,
        "00000000-0000-0000-0000-000000000000",
      );
      expect(cycle).toBe(false);
    });
  });

  it("detects when the consumer workflow already appears in the chain", async () => {
    await withTestOrg(async (orgId) => {
      // Build A → B chain by hand: A's run produces an output; B's run
      // is fired by a subscription, observation links them.
      const { workflow: a } = await saveWorkflow({
        orgId,
        name: "A",
        steps: [{ id: "s1", description: "produce" }],
      });
      const { workflow: b } = await saveWorkflow({
        orgId,
        name: "B",
        steps: [{ id: "s1", description: "react" }],
      });

      const threadA = await createWorkThread(orgId, a.name);
      const workRunA = await createWorkRun(orgId, threadA.id, "ax");
      const runA = await createWorkflowRun({
        orgId,
        workflowId: a.id,
        threadId: threadA.id,
        workRunId: workRunA.id,
        triggerKind: "manual",
      });
      const outputA = await emitWorkflowOutput({
        orgId,
        workflowRunId: runA.id,
        workRunId: workRunA.id,
        kind: "finding",
        scope: "shared",
      });
      const obsForB = await createObservation({
        orgId,
        sourceOutputId: outputA.id,
        consumerKind: "workflow",
        consumerWorkflowId: b.id,
      });

      const threadB = await createWorkThread(orgId, b.name);
      const workRunB = await createWorkRun(orgId, threadB.id, "ax");
      const runB = await createWorkflowRun({
        orgId,
        workflowId: b.id,
        threadId: threadB.id,
        workRunId: workRunB.id,
        triggerKind: "subscription",
        chainDepth: 1,
        triggeredByObservationId: obsForB.id,
        triggeredByOutputId: outputA.id,
      });

      // Now if B produces an output and a subscription on A would match
      // it, would firing A close a cycle? Walk from B's run looking for A.
      const cycle = await isWorkflowInAncestorChain(runB.id, a.id);
      expect(cycle).toBe(true);

      // Walking for a third workflow (C) that's NOT in the chain returns false.
      const noCycle = await isWorkflowInAncestorChain(runB.id, "00000000-0000-0000-0000-000000000001");
      expect(noCycle).toBe(false);
    });
  });
});
