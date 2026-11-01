import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SubscriptionRecord } from "../src/workflows/store";

const subscribeMock = vi.fn();
const listEnabledMock = vi.fn();

vi.mock("../src/graphjin/client", () => ({
  graphjinSubscribe: (args: unknown) => subscribeMock(args),
}));

vi.mock("../src/workflows/store", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../src/workflows/store")>();
  return {
    ...actual,
    listEnabledSubscriptions: () => listEnabledMock(),
  };
});

import { startSubscriptionManager } from "../src/workflows/subscription-manager";

function fakeSub(overrides: Partial<SubscriptionRecord> = {}): SubscriptionRecord {
  return {
    id: overrides.id ?? "sub-x",
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

describe("startSubscriptionManager — transport resolver", () => {
  beforeEach(() => {
    subscribeMock.mockReset();
    listEnabledMock.mockReset();
    subscribeMock.mockReturnValue({ stop: vi.fn(), ready: Promise.resolve() });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("routes workflow_output to neko-graphjin URL and source_change to data-source URL", async () => {
    listEnabledMock.mockResolvedValueOnce([
      fakeSub({
        id: "sub-wfo",
        sourceKind: "workflow_output",
        filter: { scope: "apac_churn" },
      }),
      fakeSub({
        id: "sub-src",
        sourceKind: "source_change",
        filter: {
          table: "productinventory",
          where: { quantity: { lt: 50 } },
          primary_key: ["productid", "locationid"],
        },
      }),
    ]);

    const resolveTransport = vi.fn(async (sub: SubscriptionRecord) =>
      sub.sourceKind === "source_change"
        ? { baseUrl: "http://data-source-graphjin:8080" }
        : { baseUrl: "http://neko-graphjin:8089" },
    );

    const mgr = startSubscriptionManager({
      resolveTransport,
      onMatch: vi.fn(),
    });
    await mgr.ready;

    expect(subscribeMock).toHaveBeenCalledTimes(2);
    const calls = subscribeMock.mock.calls.map((c) => c[0]);
    const wfoCall = calls.find((c) =>
      c.query.includes("WorkflowOutputMatch"),
    );
    const srcCall = calls.find((c) =>
      c.query.includes("SourceChangeMatch"),
    );
    expect(wfoCall?.baseUrl).toBe("http://neko-graphjin:8089");
    expect(srcCall?.baseUrl).toBe("http://data-source-graphjin:8080");

    await mgr.stop();
  });

  it("skips and logs when filter is invalid for source_change", async () => {
    listEnabledMock.mockResolvedValueOnce([
      fakeSub({
        id: "sub-bad",
        sourceKind: "source_change",
        filter: { table: "productinventory" }, // missing primary_key
      }),
    ]);
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});

    const mgr = startSubscriptionManager({
      resolveTransport: async () => ({ baseUrl: "http://x" }),
      onMatch: vi.fn(),
    });
    await mgr.ready;

    expect(subscribeMock).not.toHaveBeenCalled();
    expect(warnSpy).toHaveBeenCalledWith(
      expect.stringContaining("skipping subscription sub-bad"),
    );

    warnSpy.mockRestore();
    await mgr.stop();
  });

  it("emits source_change events via onMatch with parsed match payload", async () => {
    listEnabledMock.mockResolvedValueOnce([
      fakeSub({
        id: "sub-src",
        sourceKind: "source_change",
        filter: {
          table: "productinventory",
          where: {},
          primary_key: ["productid", "locationid"],
          version_column: "modifieddate",
        },
      }),
    ]);

    let pushedOnNext:
      | ((msg: unknown) => Promise<void> | void)
      | undefined;
    subscribeMock.mockImplementation((args: { onNext: (m: unknown) => unknown }) => {
      pushedOnNext = args.onNext;
      return { stop: vi.fn(), ready: Promise.resolve() };
    });

    const onMatch = vi.fn();
    const mgr = startSubscriptionManager({
      resolveTransport: async () => ({ baseUrl: "http://x" }),
      onMatch,
    });
    await mgr.ready;

    expect(pushedOnNext).toBeDefined();
    await pushedOnNext!({
      data: {
        productinventory: [
          {
            productid: 680,
            locationid: 6,
            quantity: 12,
            modifieddate: "2026-05-23T10:00:00.000Z",
          },
        ],
      },
    });

    expect(onMatch).toHaveBeenCalledTimes(1);
    const event = onMatch.mock.calls[0]![0];
    expect(event.kind).toBe("source_change");
    expect(event.match.table).toBe("productinventory");
    expect(event.match.primary_key).toEqual({ productid: 680, locationid: 6 });
    expect(event.match.version_token).toBe("2026-05-23T10:00:00.000Z");

    await mgr.stop();
  });

  it("invokes onError when resolveTransport rejects without crashing the manager", async () => {
    listEnabledMock.mockResolvedValueOnce([
      fakeSub({ id: "sub-fail", sourceKind: "source_change", filter: {
        table: "productinventory",
        primary_key: ["productid"],
      } }),
    ]);
    const onError = vi.fn();

    const mgr = startSubscriptionManager({
      resolveTransport: async () => {
        throw new Error("no data_source for org");
      },
      onMatch: vi.fn(),
      onError,
    });
    await mgr.ready;

    expect(onError).toHaveBeenCalledTimes(1);
    expect(onError.mock.calls[0]![0].message).toContain("no data_source");
    expect(subscribeMock).not.toHaveBeenCalled();

    await mgr.stop();
  });
});
