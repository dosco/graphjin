import { afterAll, describe, expect, it } from "vitest";
import { pool } from "@neko/db";
import { dbReachable, withTestOrg } from "@neko/db/test-helpers";
import {
  createObservation,
  createSubscription,
  deleteSubscription,
  emitWorkflowOutput,
  getObservation,
  linkOutputSourceObservations,
  listEnabledSubscriptions,
  listObservationsForOutput,
  listSubscriptionsByWorkflow,
  saveWorkflow,
  setSubscriptionEnabled,
} from "../../src/workflows/store";
import { createWorkRun, createWorkThread } from "../../src/work/store";
import { createWorkflowRun } from "../../src/workflows/store";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn(
    "[subscription-store] skipping: metadata Postgres unreachable.",
  );
}

describeIfDb("subscription + observation store", () => {
  afterAll(async () => {
    await pool().end();
  });

  it("createSubscription + listSubscriptionsByWorkflow round-trip", async () => {
    await withTestOrg(async (orgId) => {
      const { workflow } = await saveWorkflow({
        orgId,
        name: "consumer-A",
        steps: [{ id: "s1", description: "react" }],
      });
      const sub = await createSubscription({
        orgId,
        workflowId: workflow.id,
        sourceKind: "workflow_output",
        filter: { scope: "apac_churn", mood: ["watch", "act"] },
      });
      expect(sub.enabled).toBe(true);
      expect(sub.sourceKind).toBe("workflow_output");

      const listed = await listSubscriptionsByWorkflow(orgId, workflow.id);
      expect(listed.map((s) => s.id)).toContain(sub.id);
    });
  });

  it("listEnabledSubscriptions filters by enabled flag and source_kind", async () => {
    await withTestOrg(async (orgId) => {
      const { workflow } = await saveWorkflow({
        orgId,
        name: "filtered-consumer",
        steps: [{ id: "s1", description: "react" }],
      });
      const live = await createSubscription({
        orgId,
        workflowId: workflow.id,
        sourceKind: "workflow_output",
      });
      const disabled = await createSubscription({
        orgId,
        workflowId: workflow.id,
        sourceKind: "workflow_output",
        enabled: false,
      });
      const enabledRows = await listEnabledSubscriptions({
        sourceKind: "workflow_output",
      });
      const ids = enabledRows.map((r) => r.id);
      expect(ids).toContain(live.id);
      expect(ids).not.toContain(disabled.id);
    });
  });

  it("setSubscriptionEnabled and deleteSubscription mutate as expected", async () => {
    await withTestOrg(async (orgId) => {
      const { workflow } = await saveWorkflow({
        orgId,
        name: "mut-consumer",
        steps: [{ id: "s1", description: "react" }],
      });
      const sub = await createSubscription({
        orgId,
        workflowId: workflow.id,
        sourceKind: "workflow_output",
      });
      await setSubscriptionEnabled(sub.id, false);
      const after = await listSubscriptionsByWorkflow(orgId, workflow.id);
      expect(after.find((s) => s.id === sub.id)?.enabled).toBe(false);
      await deleteSubscription(sub.id);
      const after2 = await listSubscriptionsByWorkflow(orgId, workflow.id);
      expect(after2.find((s) => s.id === sub.id)).toBeUndefined();
    });
  });

  it("createObservation links to source output and consumer workflow", async () => {
    await withTestOrg(async (orgId) => {
      const { workflow: producer } = await saveWorkflow({
        orgId,
        name: "producer",
        steps: [{ id: "s1", description: "produce" }],
      });
      const thread = await createWorkThread(orgId, producer.name);
      const workRun = await createWorkRun(orgId, thread.id, "ax");
      const wfRun = await createWorkflowRun({
        orgId,
        workflowId: producer.id,
        threadId: thread.id,
        workRunId: workRun.id,
        triggerKind: "manual",
      });
      const out = await emitWorkflowOutput({
        orgId,
        workflowRunId: wfRun.id,
        workRunId: workRun.id,
        kind: "finding",
        scope: "apac_churn",
        mood: "watch",
      });

      const { workflow: consumer } = await saveWorkflow({
        orgId,
        name: "consumer",
        steps: [{ id: "s1", description: "react" }],
      });
      const sub = await createSubscription({
        orgId,
        workflowId: consumer.id,
        sourceKind: "workflow_output",
      });

      const obs = await createObservation({
        orgId,
        sourceOutputId: out.id,
        consumerKind: "workflow",
        consumerWorkflowId: consumer.id,
        subscriptionId: sub.id,
        title: "noticed apac churn",
        mood: "act",
      });

      const fetched = await getObservation(orgId, obs.id);
      expect(fetched?.consumerWorkflowId).toBe(consumer.id);
      expect(fetched?.subscriptionId).toBe(sub.id);

      const forOutput = await listObservationsForOutput(orgId, out.id);
      expect(forOutput.map((o) => o.id)).toContain(obs.id);
    });
  });

  it("linkOutputSourceObservations records many-to-many lineage", async () => {
    await withTestOrg(async (orgId) => {
      const { workflow: a } = await saveWorkflow({
        orgId,
        name: "A",
        steps: [{ id: "s1", description: "produce" }],
      });
      const thread = await createWorkThread(orgId, a.name);
      const workRun = await createWorkRun(orgId, thread.id, "ax");
      const wfRun = await createWorkflowRun({
        orgId,
        workflowId: a.id,
        threadId: thread.id,
        workRunId: workRun.id,
        triggerKind: "manual",
      });
      const out1 = await emitWorkflowOutput({
        orgId,
        workflowRunId: wfRun.id,
        workRunId: workRun.id,
        kind: "finding",
      });
      const obs1 = await createObservation({
        orgId,
        sourceOutputId: out1.id,
        consumerKind: "workflow",
      });
      const obs2 = await createObservation({
        orgId,
        sourceOutputId: out1.id,
        consumerKind: "workflow",
      });
      await linkOutputSourceObservations(out1.id, [obs1.id, obs2.id]);
      // The join table has no public reader yet; we just verify the call
      // doesn't throw and is idempotent.
      await linkOutputSourceObservations(out1.id, [obs1.id, obs2.id]);
    });
  });
});
