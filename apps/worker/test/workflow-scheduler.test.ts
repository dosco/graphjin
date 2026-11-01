import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  LeasedWorkflowFiring,
  MaterializeScheduleResult,
} from "@neko/llm/workflows";
import {
  getWorkflowSchedulerHealth,
  resetWorkflowSchedulerHealthForTesting,
  runDurableWorkflowSchedulerTick,
  type WorkflowSchedulerDependencies,
} from "../src/workflow-scheduler";

const materializeResult: MaterializeScheduleResult = {
  acquired: true,
  activeWorkflows: 1,
  materialized: 1,
  coalescedOccurrences: 0,
  budgetSkipped: 0,
  invalidSchedules: 0,
};

const firing: LeasedWorkflowFiring = {
  id: "8f212cad-481d-45b0-972e-88269052f0c2",
  orgId: "org-test",
  workflowId: "d3aaf2d2-804b-4fe4-ae7f-98e574536804",
  scheduledFor: new Date("2026-08-26T07:00:00.000Z"),
  attempts: 1,
};

function dependencies(
  overrides: Partial<WorkflowSchedulerDependencies> = {},
): WorkflowSchedulerDependencies {
  return {
    materialize: vi.fn(async () => materializeResult),
    recover: vi.fn(async () => 0),
    lease: vi.fn(async () => [firing]),
    enqueue: vi.fn(async () => "job-1"),
    markEnqueued: vi.fn(async () => undefined),
    releaseDispatch: vi.fn(async () => undefined),
    recordSuccess: vi.fn(async () => undefined),
    recordFailure: vi.fn(async () => undefined),
    ...overrides,
  };
}

describe("independent durable workflow scheduler", () => {
  beforeEach(() => {
    resetWorkflowSchedulerHealthForTesting();
    vi.restoreAllMocks();
  });

  it("materializes, leases, and dispatches a ledger firing", async () => {
    const deps = dependencies();
    const now = new Date("2026-08-26T07:00:13.000Z");
    const health = await runDurableWorkflowSchedulerTick({
      now,
      dependencies: deps,
    });

    expect(deps.enqueue).toHaveBeenCalledWith(
      expect.objectContaining({
        orgId: firing.orgId,
        workflowId: firing.workflowId,
        triggerKind: "cron",
        scheduleFiringId: firing.id,
        triggerPayload: {
          firingTime: "2026-08-26T07:00:00.000Z",
          scheduleFiringId: firing.id,
        },
      }),
      firing,
    );
    expect(deps.markEnqueued).toHaveBeenCalledWith(firing.id, "job-1", now);
    expect(deps.recordSuccess).toHaveBeenCalledWith({ dispatched: 1, now });
    expect(health).toMatchObject({
      status: "ok",
      materialized: 1,
      dispatched: 1,
      recovered: 0,
    });
  });

  it("treats a null queue id as a failed delivery", async () => {
    vi.spyOn(console, "warn").mockImplementation(() => undefined);
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    const deps = dependencies({ enqueue: vi.fn(async () => null) });
    const health = await runDurableWorkflowSchedulerTick({
      dependencies: deps,
    });
    expect(deps.markEnqueued).not.toHaveBeenCalled();
    expect(deps.releaseDispatch).toHaveBeenCalledWith(
      firing.id,
      expect.objectContaining({
        message: "pg-boss did not accept the workflow firing",
      }),
      expect.any(Date),
    );
    expect(health.status).toBe("degraded");
  });

  it("returns a failed dispatch to the outbox and degrades health", async () => {
    vi.spyOn(console, "warn").mockImplementation(() => undefined);
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    const failure = new Error("pg-boss unavailable");
    const deps = dependencies({
      enqueue: vi.fn(async () => {
        throw failure;
      }),
    });

    const health = await runDurableWorkflowSchedulerTick({
      dependencies: deps,
    });

    expect(deps.releaseDispatch).toHaveBeenCalledWith(
      firing.id,
      failure,
      expect.any(Date),
    );
    expect(deps.recordSuccess).not.toHaveBeenCalled();
    expect(deps.recordFailure).toHaveBeenCalled();
    expect(health).toMatchObject({
      status: "degraded",
      consecutiveFailures: 1,
      lastError: "1/1 workflow firing dispatches failed",
    });
  });

  it("reports a formerly healthy loop as stale", async () => {
    const now = new Date("2026-08-26T07:00:00.000Z");
    await runDurableWorkflowSchedulerTick({
      now,
      dependencies: dependencies({ lease: vi.fn(async () => []) }),
    });
    expect(
      getWorkflowSchedulerHealth(new Date("2026-08-26T07:02:01.000Z")),
    ).toMatchObject({ status: "degraded", stale: true });
  });
});
