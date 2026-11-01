import { afterAll, describe, expect, it, vi } from "vitest";
import { and, db, eq, observation, pool, workflow_run } from "@neko/db";
import { dbReachable, withTestOrg } from "@neko/db/test-helpers";
import {
  createSubscription,
  emitWorkflowOutput,
  saveWorkflow,
} from "../../src/workflows/store";
import { createWorkRun, createWorkThread } from "../../src/work/store";
import { createWorkflowRun } from "../../src/workflows/store";
import { handleSubscriptionMatch } from "../../src/workflows/match-handler";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn(
    "[subscription-chain] skipping: metadata Postgres unreachable.",
  );
}

describeIfDb("subscription-driven workflow chain", () => {
  afterAll(async () => {
    await pool().end();
  });

  it("subscription match writes observation + enqueues consumer fire job with lineage", async () => {
    await withTestOrg(async (orgId) => {
      const { workflow: producer } = await saveWorkflow({
        orgId,
        name: "producer-A",
        steps: [{ id: "s1", description: "produce a finding" }],
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
      const output = await emitWorkflowOutput({
        orgId,
        workflowRunId: wfRun.id,
        workRunId: workRun.id,
        kind: "finding",
        title: "Churn spike",
        scope: "apac_churn",
        mood: "watch",
      });

      const { workflow: consumer } = await saveWorkflow({
        orgId,
        name: "consumer-B",
        steps: [{ id: "s1", description: "investigate" }],
      });
      const sub = await createSubscription({
        orgId,
        workflowId: consumer.id,
        sourceKind: "workflow_output",
        filter: { scope: "apac_churn", mood: ["watch", "act"] },
      });

      const enqueue = vi.fn().mockResolvedValue("fake-job-id");

      const decision = await handleSubscriptionMatch({
        subscription: {
          id: sub.id,
          orgId,
          workflowId: consumer.id,
          sourceKind: "workflow_output",
          filter: sub.filter,
          enabled: true,
          debounceMs: 0,
          maxConcurrentRuns: 5,
          maxChainDepthOverride: null,
          idempotencyKeyTemplate: null,
          createdAt: sub.createdAt,
          updatedAt: sub.updatedAt,
        },
        output: {
          id: output.id,
          org_id: orgId,
          workflow_run_id: wfRun.id,
          kind: "finding",
          scope: "apac_churn",
          topic: null,
          mood: "watch",
          title: "Churn spike",
          created_at: output.createdAt.toISOString(),
        },
        enqueue: enqueue as never,
        countWorkflowRunsSince: async () => 0,
        isWorkflowInAncestorChain: async () => false,
        resolveProducingRunChainDepth: async () => 0,
      });

      expect(decision.action).toBe("enqueued");

      if (decision.action !== "enqueued") return;

      // Observation row written, linked to source output + subscription + consumer workflow
      const obsRow = await db()
        .select()
        .from(observation)
        .where(eq(observation.id, decision.observationId))
        .limit(1);
      expect(obsRow[0]?.source_output_id).toBe(output.id);
      expect(obsRow[0]?.consumer_workflow_id).toBe(consumer.id);
      expect(obsRow[0]?.subscription_id).toBe(sub.id);

      // Fire payload carries all three lineage FKs
      const [queueName, jobData, jobOpts] = enqueue.mock.calls[0];
      expect(queueName).toBe("workflow_run_fire");
      expect(jobData.workflowId).toBe(consumer.id);
      expect(jobData.triggeredBySubscriptionId).toBe(sub.id);
      expect(jobData.triggeredByOutputId).toBe(output.id);
      expect(jobData.triggeredByObservationId).toBe(decision.observationId);
      // Idempotency key dedups repeat firings of the same (sub × output × version)
      expect(jobOpts.singletonKey).toContain(sub.id);
      expect(jobOpts.singletonKey).toContain(output.id);
    });
  });

  it("drops the match when the producing run's chain depth + 1 exceeds the cap", async () => {
    await withTestOrg(async (orgId) => {
      const { workflow: producer } = await saveWorkflow({
        orgId,
        name: "deep-producer",
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
      // Push chain_depth high to trigger the cap.
      await db()
        .update(workflow_run)
        .set({ chain_depth: 8 })
        .where(and(eq(workflow_run.id, wfRun.id)));

      const output = await emitWorkflowOutput({
        orgId,
        workflowRunId: wfRun.id,
        workRunId: workRun.id,
        kind: "finding",
        scope: "loop_test",
      });

      const { workflow: consumer } = await saveWorkflow({
        orgId,
        name: "deep-consumer",
        steps: [{ id: "s1", description: "react" }],
      });
      const sub = await createSubscription({
        orgId,
        workflowId: consumer.id,
        sourceKind: "workflow_output",
      });

      const enqueue = vi.fn();

      const decision = await handleSubscriptionMatch({
        subscription: {
          id: sub.id,
          orgId,
          workflowId: consumer.id,
          sourceKind: "workflow_output",
          filter: {},
          enabled: true,
          debounceMs: 0,
          maxConcurrentRuns: 5,
          maxChainDepthOverride: null,
          idempotencyKeyTemplate: null,
          createdAt: sub.createdAt,
          updatedAt: sub.updatedAt,
        },
        output: {
          id: output.id,
          org_id: orgId,
          workflow_run_id: wfRun.id,
          kind: "finding",
          scope: "loop_test",
          topic: null,
          mood: null,
          title: "deep",
          created_at: output.createdAt.toISOString(),
        },
        enqueue: enqueue as never,
        globalMaxChainDepth: 8,
        countWorkflowRunsSince: async () => 0,
        isWorkflowInAncestorChain: async () => false,
        resolveProducingRunChainDepth: async () => 8,
      });

      expect(decision.action).toBe("dropped");
      expect(enqueue).not.toHaveBeenCalled();
    });
  });
});
