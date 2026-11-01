import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  resetWorkflowApiDispatcherForTesting,
  runWorkflowApiDispatcherTick,
} from "../src/workflow-api-dispatcher";

const now = new Date("2026-09-01T08:00:00.000Z");
const admission = {
  id: "admission-1",
  orgId: "org-1",
  workflowId: "workflow-1",
  workflowRunId: "workflow-run-1",
  workRunId: "work-run-1",
  threadId: "thread-1",
  executionMode: "single" as const,
  admittedAt: new Date("2026-09-01T07:59:58.000Z"),
  attempts: 2,
};

describe("workflow API durable dispatcher", () => {
  beforeEach(() => resetWorkflowApiDispatcherForTesting());

  it("recovers, expires, leases, enqueues, and acknowledges the outbox row", async () => {
    const dependencies = {
      recover: vi.fn(async () => 1),
      expire: vi.fn(async () => 2),
      lease: vi.fn(async () => [admission]),
      enqueue: vi.fn(async () => "queue-job-1"),
      markEnqueued: vi.fn(async () => undefined),
      release: vi.fn(async () => undefined),
    };

    await expect(
      runWorkflowApiDispatcherTick({ now, dependencies }),
    ).resolves.toEqual({ dispatched: 1, recovered: 1, expired: 2 });
    expect(dependencies.enqueue).toHaveBeenCalledWith(
      {
        orgId: "org-1",
        workflowId: "workflow-1",
        triggerKind: "api",
        apiAdmissionId: "admission-1",
        workflowRunId: "workflow-run-1",
        workRunId: "work-run-1",
        threadId: "thread-1",
        executionMode: "single",
        admittedAt: "2026-09-01T07:59:58.000Z",
        queueAttempt: 2,
      },
      admission,
    );
    expect(dependencies.markEnqueued).toHaveBeenCalledWith(
      "admission-1",
      "queue-job-1",
      now,
    );
    expect(dependencies.release).not.toHaveBeenCalled();
  });

  it("returns the lease to pending when the queue rejects delivery", async () => {
    const dependencies = {
      recover: vi.fn(async () => 0),
      expire: vi.fn(async () => 0),
      lease: vi.fn(async () => [admission]),
      enqueue: vi.fn(async () => null),
      markEnqueued: vi.fn(async () => undefined),
      release: vi.fn(async () => undefined),
    };

    await expect(
      runWorkflowApiDispatcherTick({ now, dependencies }),
    ).resolves.toEqual({ dispatched: 0, recovered: 0, expired: 0 });
    expect(dependencies.markEnqueued).not.toHaveBeenCalled();
    expect(dependencies.release).toHaveBeenCalledWith(
      "admission-1",
      "queue_rejected",
      now,
    );
  });
});
