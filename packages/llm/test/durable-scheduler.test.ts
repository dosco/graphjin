import { describe, expect, it } from "vitest";
import {
  advanceWorkflowSchedule,
  nextWorkflowFireAt,
} from "../src/workflows/durable-scheduler";

describe("durable workflow schedule cursor", () => {
  it("computes the first occurrence strictly after an enable boundary", () => {
    const next = nextWorkflowFireAt(
      "* * * * *",
      "UTC",
      new Date("2026-08-26T07:00:00.000Z"),
    );
    expect(next.toISOString()).toBe("2026-08-26T07:01:00.000Z");
  });

  it("respects the workflow timezone", () => {
    const next = nextWorkflowFireAt(
      "0 9 * * *",
      "America/New_York",
      new Date("2026-08-26T12:59:59.000Z"),
    );
    expect(next.toISOString()).toBe("2026-08-26T13:00:00.000Z");
  });

  it("coalesces downtime into the latest missed occurrence", () => {
    const plan = advanceWorkflowSchedule({
      cron: "0 * * * *",
      timezone: "UTC",
      nextFireAt: new Date("2026-08-26T09:00:00.000Z"),
      now: new Date("2026-08-26T12:15:00.000Z"),
      catchUpPolicy: "coalesce",
    });
    expect(plan.scheduledFor.map((date) => date.toISOString())).toEqual([
      "2026-08-26T12:00:00.000Z",
    ]);
    expect(plan.nextFireAt.toISOString()).toBe("2026-08-26T13:00:00.000Z");
    expect(plan.coalescedOccurrences).toBe(3);
  });

  it("replays a bounded prefix and leaves the cursor ready for another tick", () => {
    const plan = advanceWorkflowSchedule({
      cron: "0 * * * *",
      timezone: "UTC",
      nextFireAt: new Date("2026-08-26T09:00:00.000Z"),
      now: new Date("2026-08-26T12:15:00.000Z"),
      catchUpPolicy: "replay",
      replayLimit: 2,
    });
    expect(plan.scheduledFor.map((date) => date.toISOString())).toEqual([
      "2026-08-26T09:00:00.000Z",
      "2026-08-26T10:00:00.000Z",
    ]);
    expect(plan.nextFireAt.toISOString()).toBe("2026-08-26T11:00:00.000Z");
  });

  it("honors a replay limit of one", () => {
    const plan = advanceWorkflowSchedule({
      cron: "0 * * * *",
      timezone: "UTC",
      nextFireAt: new Date("2026-08-26T09:00:00.000Z"),
      now: new Date("2026-08-26T12:15:00.000Z"),
      catchUpPolicy: "replay",
      replayLimit: 1,
    });
    expect(plan.scheduledFor.map((date) => date.toISOString())).toEqual([
      "2026-08-26T09:00:00.000Z",
    ]);
    expect(plan.nextFireAt.toISOString()).toBe("2026-08-26T10:00:00.000Z");
  });

  it("does not move a cursor that is not due", () => {
    const next = new Date("2026-08-26T13:00:00.000Z");
    const plan = advanceWorkflowSchedule({
      cron: "0 * * * *",
      timezone: "UTC",
      nextFireAt: next,
      now: new Date("2026-08-26T12:15:00.000Z"),
      catchUpPolicy: "coalesce",
    });
    expect(plan.scheduledFor).toEqual([]);
    expect(plan.nextFireAt).toEqual(next);
  });
});
