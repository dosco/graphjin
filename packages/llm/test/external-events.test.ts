import { describe, expect, it, vi } from "vitest";
import {
  dispatchExternalEvent,
  externalEventFilterMatches,
} from "../src/workflows/external-events";
import {
  handleExternalEventMatch,
  type ExternalEventMatch,
} from "../src/workflows/match-handler";
import type { SubscriptionRecord } from "../src/workflows/store";

function fakeSubscription(
  overrides: Partial<SubscriptionRecord> = {},
): SubscriptionRecord {
  return {
    id: overrides.id ?? "sub-1",
    orgId: overrides.orgId ?? "org-1",
    workflowId: overrides.workflowId ?? "wf-1",
    sourceKind: overrides.sourceKind ?? "external_event",
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

function fakeEvent(
  overrides: Partial<ExternalEventMatch> = {},
): ExternalEventMatch {
  return {
    name: overrides.name ?? "invoice.paid",
    source: overrides.source ?? "@open-neko/plugin-stripe",
    payload: overrides.payload ?? { invoiceId: "inv-1" },
    ...(overrides.dedupeKey ? { dedupeKey: overrides.dedupeKey } : {}),
  };
}

const happyDeps = () => ({
  enqueue: vi.fn(async () => "job-1"),
  createObservation: vi.fn(async () => ({ id: "obs-1" }) as never),
  countWorkflowRunsForSubscription: vi.fn(async () => 0),
  countWorkflowRunsSince: vi.fn(async () => 0),
  getWorkflow: vi.fn(async () => null),
});

describe("externalEventFilterMatches", () => {
  it("empty filter matches everything", () => {
    expect(externalEventFilterMatches({}, fakeEvent())).toBe(true);
  });

  it("matches on name and source when present", () => {
    expect(
      externalEventFilterMatches({ name: "invoice.paid" }, fakeEvent()),
    ).toBe(true);
    expect(
      externalEventFilterMatches({ name: "invoice.voided" }, fakeEvent()),
    ).toBe(false);
    expect(
      externalEventFilterMatches(
        { source: "@open-neko/plugin-stripe" },
        fakeEvent(),
      ),
    ).toBe(true);
    expect(
      externalEventFilterMatches({ source: "other" }, fakeEvent()),
    ).toBe(false);
  });
});

describe("handleExternalEventMatch", () => {
  it("writes an observation and enqueues a run", async () => {
    const deps = happyDeps();
    const decision = await handleExternalEventMatch({
      subscription: fakeSubscription(),
      event: fakeEvent(),
      ...deps,
    });
    expect(decision).toEqual({
      action: "enqueued",
      observationId: "obs-1",
      jobId: "job-1",
    });
    expect(deps.createObservation).toHaveBeenCalledWith(
      expect.objectContaining({
        orgId: "org-1",
        consumerWorkflowId: "wf-1",
        title: "@open-neko/plugin-stripe: invoice.paid",
      }),
    );
    const [queue, payload, opts] = deps.enqueue.mock.calls[0] as unknown[];
    expect(queue).toBeTruthy();
    expect(payload).toMatchObject({
      orgId: "org-1",
      workflowId: "wf-1",
      triggerKind: "subscription",
      triggerPayload: expect.objectContaining({
        event_name: "invoice.paid",
        event_source: "@open-neko/plugin-stripe",
      }),
    });
    expect((opts as { singletonKey: string }).singletonKey).toMatch(/^sub-1:/);
  });

  it("same event twice yields the same idempotency key; dedupeKey overrides", async () => {
    const deps1 = happyDeps();
    await handleExternalEventMatch({
      subscription: fakeSubscription(),
      event: fakeEvent(),
      ...deps1,
    });
    const deps2 = happyDeps();
    await handleExternalEventMatch({
      subscription: fakeSubscription(),
      event: fakeEvent(),
      ...deps2,
    });
    const key1 = (deps1.enqueue.mock.calls[0]![2] as { singletonKey: string })
      .singletonKey;
    const key2 = (deps2.enqueue.mock.calls[0]![2] as { singletonKey: string })
      .singletonKey;
    expect(key1).toBe(key2);

    const deps3 = happyDeps();
    await handleExternalEventMatch({
      subscription: fakeSubscription(),
      event: fakeEvent({ dedupeKey: "custom" }),
      ...deps3,
    });
    expect(
      (deps3.enqueue.mock.calls[0]![2] as { singletonKey: string })
        .singletonKey,
    ).toBe("sub-1:custom");
  });

  it("drops when the subscription is at max_concurrent_runs", async () => {
    const deps = happyDeps();
    deps.countWorkflowRunsForSubscription = vi.fn(async () => 5);
    const decision = await handleExternalEventMatch({
      subscription: fakeSubscription({ maxConcurrentRuns: 5 }),
      event: fakeEvent(),
      ...deps,
    });
    expect(decision.action).toBe("dropped");
    expect(deps.enqueue).not.toHaveBeenCalled();
  });

  it("drops when the consumer workflow reached its daily budget", async () => {
    const deps = happyDeps();
    deps.getWorkflow = vi.fn(
      async () => ({ dailyRunBudget: 3 }) as never,
    );
    deps.countWorkflowRunsSince = vi.fn(async () => 3);
    const decision = await handleExternalEventMatch({
      subscription: fakeSubscription(),
      event: fakeEvent(),
      ...deps,
    });
    expect(decision.action).toBe("dropped");
    expect(deps.enqueue).not.toHaveBeenCalled();
  });
});

describe("dispatchExternalEvent", () => {
  it("fires only matching subscriptions in the org", async () => {
    const subs = [
      fakeSubscription({ id: "s-match", filter: { name: "invoice.paid" } }),
      fakeSubscription({ id: "s-other-name", filter: { name: "x" } }),
      fakeSubscription({ id: "s-other-org", orgId: "org-2" }),
      fakeSubscription({ id: "s-match-all", filter: {} }),
    ];
    const handleMatch = vi.fn(async () => ({
      action: "enqueued" as const,
      observationId: "obs",
      jobId: "job",
    }));
    const result = await dispatchExternalEvent(
      { orgId: "org-1", event: fakeEvent() },
      { listSubscriptions: async () => subs, handleMatch },
    );
    expect(result.matched).toBe(2);
    expect(result.enqueued).toBe(2);
    expect(handleMatch.mock.calls.map((c) => (c[0] as { subscription: SubscriptionRecord }).subscription.id)).toEqual([
      "s-match",
      "s-match-all",
    ]);
  });

  it("counts dropped decisions separately", async () => {
    const subs = [fakeSubscription({ id: "s1" })];
    const result = await dispatchExternalEvent(
      { orgId: "org-1", event: fakeEvent() },
      {
        listSubscriptions: async () => subs,
        handleMatch: async () => ({ action: "dropped", reason: "budget" }),
      },
    );
    expect(result.matched).toBe(1);
    expect(result.enqueued).toBe(0);
    expect(result.decisions[0]).toMatchObject({
      subscriptionId: "s1",
      action: "dropped",
    });
  });
});
