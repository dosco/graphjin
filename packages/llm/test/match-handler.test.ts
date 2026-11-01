import { describe, expect, it, vi } from "vitest";
import {
  handleSourceChangeMatch,
  handleSubscriptionMatch,
} from "../src/workflows/match-handler";
import type { SubscriptionRecord } from "../src/workflows/store";
import type {
  SourceChangeMatch,
  WorkflowOutputMatch,
} from "../src/workflows/subscription-query";

function fakeSubscription(
  overrides: Partial<SubscriptionRecord> = {},
): SubscriptionRecord {
  return {
    id: overrides.id ?? "sub-1",
    orgId: overrides.orgId ?? "org-1",
    workflowId: overrides.workflowId ?? "wf-1",
    sourceKind: overrides.sourceKind ?? "workflow_output",
    filter: overrides.filter ?? {},
    enabled: overrides.enabled ?? true,
    debounceMs: overrides.debounceMs ?? 0,
    maxConcurrentRuns: overrides.maxConcurrentRuns ?? 5,
    maxChainDepthOverride: overrides.maxChainDepthOverride ?? null,
    idempotencyKeyTemplate: overrides.idempotencyKeyTemplate ?? null,
    createdAt: new Date(),
    updatedAt: new Date(),
  };
}

function fakeOutput(
  overrides: Partial<WorkflowOutputMatch> = {},
): WorkflowOutputMatch {
  return {
    id: overrides.id ?? "out-1",
    org_id: overrides.org_id ?? "org-1",
    workflow_run_id: overrides.workflow_run_id ?? "wfr-parent",
    kind: overrides.kind ?? "finding",
    scope: overrides.scope ?? "apac_churn",
    topic: overrides.topic ?? null,
    mood: overrides.mood ?? "watch",
    title: overrides.title ?? "Churn spike",
    created_at: overrides.created_at ?? "2026-05-13T12:00:00.000Z",
  };
}

describe("handleSubscriptionMatch", () => {
  it("drops on org mismatch", async () => {
    const enqueue = vi.fn();
    const createObservation = vi.fn();
    const decision = await handleSubscriptionMatch({
      subscription: fakeSubscription({ orgId: "org-A" }),
      output: fakeOutput({ org_id: "org-B" }),
      enqueue,
      createObservation,
    });
    expect(decision.action).toBe("dropped");
    expect(enqueue).not.toHaveBeenCalled();
    expect(createObservation).not.toHaveBeenCalled();
  });

  it("drops when chain depth exceeds the configured max", async () => {
    const enqueue = vi.fn();
    const createObservation = vi.fn();
    const decision = await handleSubscriptionMatch({
      subscription: fakeSubscription(),
      output: fakeOutput(),
      enqueue,
      createObservation,
      globalMaxChainDepth: 2,
      resolveProducingRunChainDepth: async () => 2,
    });
    expect(decision.action).toBe("dropped");
    if (decision.action === "dropped") {
      expect(decision.reason).toMatch(/chain depth/);
    }
    expect(enqueue).not.toHaveBeenCalled();
  });

  it("enqueues a fire job with idempotency key and lineage refs on happy path", async () => {
    const enqueue = vi.fn().mockResolvedValue("job-id-xyz");
    const createObservation = vi.fn().mockResolvedValue({
      id: "obs-1",
      orgId: "org-1",
    });
    const decision = await handleSubscriptionMatch({
      subscription: fakeSubscription(),
      output: fakeOutput(),
      enqueue: enqueue as never,
      createObservation: createObservation as never,
      countSubscriptionsMatchingOutput: async () => 0,
      countWorkflowRunsForSubscription: async () => 0,
      countWorkflowRunsSince: async () => 0,
      getWorkflow: async () => null,
      isWorkflowInAncestorChain: async () => false,
      resolveProducingRunChainDepth: async () => 0,
      globalMaxChainDepth: 8,
      globalMaxFanoutPerOutput: 32,
    });
    expect(decision.action).toBe("enqueued");
    if (decision.action === "enqueued") {
      expect(decision.observationId).toBe("obs-1");
      expect(decision.jobId).toBe("job-id-xyz");
    }
    expect(createObservation).toHaveBeenCalledTimes(1);
    expect(enqueue).toHaveBeenCalledTimes(1);

    const [, jobData, jobOpts] = enqueue.mock.calls[0];
    expect(jobData.triggeredBySubscriptionId).toBe("sub-1");
    expect(jobData.triggeredByOutputId).toBe("out-1");
    expect(jobData.triggeredByObservationId).toBe("obs-1");
    expect(jobOpts.singletonKey).toBe(
      "sub-1:out-1:2026-05-13T12:00:00.000Z",
    );
    expect(jobOpts.singletonHours).toBe(1);
  });

  it("honors a custom idempotencyKeyTemplate", async () => {
    const enqueue = vi.fn().mockResolvedValue("job-id");
    const createObservation = vi.fn().mockResolvedValue({ id: "obs-1" });
    await handleSubscriptionMatch({
      subscription: fakeSubscription({
        idempotencyKeyTemplate:
          "sub:{subscription_id}:out:{source_record_id}",
      }),
      output: fakeOutput(),
      enqueue: enqueue as never,
      createObservation: createObservation as never,
      countSubscriptionsMatchingOutput: async () => 0,
      countWorkflowRunsForSubscription: async () => 0,
      countWorkflowRunsSince: async () => 0,
      getWorkflow: async () => null,
      isWorkflowInAncestorChain: async () => false,
      resolveProducingRunChainDepth: async () => 0,
    });
    const jobOpts = enqueue.mock.calls[0][2];
    expect(jobOpts.singletonKey).toBe("sub:sub-1:out:out-1");
  });
});

function fakeSourceChangeMatch(
  overrides: Partial<SourceChangeMatch> = {},
): SourceChangeMatch {
  return {
    table: overrides.table ?? "productinventory",
    primary_key: overrides.primary_key ?? { productid: 680, locationid: 6 },
    snapshot:
      overrides.snapshot ?? {
        productid: 680,
        locationid: 6,
        quantity: 12,
        modifieddate: "2026-05-23T10:00:00.000Z",
      },
    version_token:
      overrides.version_token === undefined
        ? "2026-05-23T10:00:00.000Z"
        : overrides.version_token,
  };
}

describe("handleSourceChangeMatch", () => {
  it("drops when the workflow recently wrote to the same (table, pk)", async () => {
    const enqueue = vi.fn();
    const createObservation = vi.fn();
    const writeSourceChangeLog = vi.fn();
    const decision = await handleSourceChangeMatch({
      subscription: fakeSubscription({ sourceKind: "source_change" }),
      match: fakeSourceChangeMatch(),
      dataSourceId: "ds-1",
      enqueue: enqueue as never,
      createObservation: createObservation as never,
      writeSourceChangeLog: writeSourceChangeLog as never,
      countWorkflowRunsForSubscription: async () => 0,
      countWorkflowRunsSince: async () => 0,
      getWorkflow: async () => null,
      hasRecentSourceWriteForWorkflow: async () => true,
    });
    expect(decision.action).toBe("dropped");
    if (decision.action === "dropped") {
      expect(decision.reason).toMatch(/recently wrote/);
    }
    expect(enqueue).not.toHaveBeenCalled();
    expect(createObservation).not.toHaveBeenCalled();
    expect(writeSourceChangeLog).not.toHaveBeenCalled();
  });

  it("drops when subscription is at max_concurrent_runs", async () => {
    const enqueue = vi.fn();
    const decision = await handleSourceChangeMatch({
      subscription: fakeSubscription({
        sourceKind: "source_change",
        maxConcurrentRuns: 3,
      }),
      match: fakeSourceChangeMatch(),
      dataSourceId: "ds-1",
      enqueue: enqueue as never,
      createObservation: vi.fn() as never,
      writeSourceChangeLog: vi.fn() as never,
      countWorkflowRunsForSubscription: async () => 3,
      countWorkflowRunsSince: async () => 0,
      getWorkflow: async () => null,
      hasRecentSourceWriteForWorkflow: async () => false,
    });
    expect(decision.action).toBe("dropped");
    if (decision.action === "dropped") {
      expect(decision.reason).toMatch(/max_concurrent_runs/);
    }
    expect(enqueue).not.toHaveBeenCalled();
  });

  it("enqueues fire + writes audit + composite-PK idempotency on happy path", async () => {
    const enqueue = vi.fn().mockResolvedValue("job-abc");
    const createObservation = vi.fn().mockResolvedValue({
      id: "obs-sc-1",
      orgId: "org-1",
    });
    const writeSourceChangeLog = vi.fn().mockResolvedValue(undefined);

    const decision = await handleSourceChangeMatch({
      subscription: fakeSubscription({ sourceKind: "source_change" }),
      match: fakeSourceChangeMatch(),
      dataSourceId: "ds-1",
      enqueue: enqueue as never,
      createObservation: createObservation as never,
      writeSourceChangeLog: writeSourceChangeLog as never,
      countWorkflowRunsForSubscription: async () => 0,
      countWorkflowRunsSince: async () => 0,
      getWorkflow: async () => null,
      hasRecentSourceWriteForWorkflow: async () => false,
    });

    expect(decision.action).toBe("enqueued");
    if (decision.action === "enqueued") {
      expect(decision.observationId).toBe("obs-sc-1");
      expect(decision.jobId).toBe("job-abc");
    }

    expect(createObservation).toHaveBeenCalledWith(
      expect.objectContaining({
        sourceOutputId: null,
        subscriptionId: "sub-1",
        title: expect.stringContaining("productinventory"),
      }),
    );

    expect(writeSourceChangeLog).toHaveBeenCalledWith(
      expect.objectContaining({
        orgId: "org-1",
        sourceId: "ds-1",
        tableName: "productinventory",
        changeKind: "subscription_match",
      }),
    );

    const [, jobData, jobOpts] = enqueue.mock.calls[0];
    expect(jobData.triggeredBySubscriptionId).toBe("sub-1");
    expect(jobData.triggeredByObservationId).toBe("obs-sc-1");
    expect(jobData.triggerPayload).toMatchObject({
      table: "productinventory",
      primary_key: { productid: 680, locationid: 6 },
    });

    // Idempotency key: ${sub.id}:${pkHash}:${versionToken}
    // pkHash is sha256(sorted entries).slice(0,16), version_token = match.version_token
    expect(jobOpts.singletonKey).toMatch(
      /^sub-1:[0-9a-f]{16}:2026-05-23T10:00:00\.000Z$/,
    );
    expect(jobOpts.singletonHours).toBe(1);
  });

  it("idempotency key uses 'none' when version_token is null", async () => {
    const enqueue = vi.fn().mockResolvedValue("job-id");
    const createObservation = vi
      .fn()
      .mockResolvedValue({ id: "obs-2", orgId: "org-1" });
    await handleSourceChangeMatch({
      subscription: fakeSubscription({ sourceKind: "source_change" }),
      match: fakeSourceChangeMatch({ version_token: null }),
      dataSourceId: "ds-1",
      enqueue: enqueue as never,
      createObservation: createObservation as never,
      writeSourceChangeLog: vi.fn() as never,
      countWorkflowRunsForSubscription: async () => 0,
      countWorkflowRunsSince: async () => 0,
      getWorkflow: async () => null,
      hasRecentSourceWriteForWorkflow: async () => false,
    });
    const jobOpts = enqueue.mock.calls[0][2];
    expect(jobOpts.singletonKey).toMatch(/^sub-1:[0-9a-f]{16}:none$/);
  });

  it("honors a custom idempotencyKeyTemplate with {primary_key}", async () => {
    const enqueue = vi.fn().mockResolvedValue("job-id");
    const createObservation = vi
      .fn()
      .mockResolvedValue({ id: "obs-3", orgId: "org-1" });
    await handleSourceChangeMatch({
      subscription: fakeSubscription({
        sourceKind: "source_change",
        idempotencyKeyTemplate:
          "reorder-{primary_key}-{source_version}",
      }),
      match: fakeSourceChangeMatch(),
      dataSourceId: "ds-1",
      enqueue: enqueue as never,
      createObservation: createObservation as never,
      writeSourceChangeLog: vi.fn() as never,
      countWorkflowRunsForSubscription: async () => 0,
      countWorkflowRunsSince: async () => 0,
      getWorkflow: async () => null,
      hasRecentSourceWriteForWorkflow: async () => false,
    });
    const jobOpts = enqueue.mock.calls[0][2];
    expect(jobOpts.singletonKey).toMatch(
      /^reorder-[0-9a-f]{16}-2026-05-23T10:00:00\.000Z$/,
    );
  });
});
