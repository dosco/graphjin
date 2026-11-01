import { describe, expect, it } from "vitest";
import { metricCadenceMs, metricRefreshIsDue } from "../src/jobs/metric-schedule.js";

describe("metric refresh scheduling", () => {
  it("honors hourly, daily, and weekly cadences", () => {
    expect(metricCadenceMs("hourly")).toBe(60 * 60 * 1_000);
    expect(metricCadenceMs("daily")).toBe(24 * 60 * 60 * 1_000);
    expect(metricCadenceMs("weekly")).toBe(7 * 24 * 60 * 60 * 1_000);
  });

  it("refreshes only after the card cadence has elapsed", () => {
    const now = new Date("2026-08-20T12:00:00Z");
    expect(metricRefreshIsDue({
      cadence: "hourly",
      lastRefreshStatus: "ok",
      updatedAt: new Date("2026-08-20T10:59:59Z"),
      now,
    })).toBe(true);
    expect(metricRefreshIsDue({
      cadence: "daily",
      lastRefreshStatus: "ok",
      updatedAt: new Date("2026-08-20T10:59:59Z"),
      now,
    })).toBe(false);
  });

  it("does not enqueue a second refresh while one is pending", () => {
    expect(metricRefreshIsDue({
      cadence: "hourly",
      lastRefreshStatus: "pending",
      updatedAt: new Date("2026-08-01T00:00:00Z"),
      now: new Date("2026-08-20T12:00:00Z"),
    })).toBe(false);
  });

  it("lets the organization setting slow or stop refreshes, never speed them up", () => {
    const now = new Date("2026-08-20T12:00:00Z");
    const card = { cadence: "hourly", lastRefreshStatus: "ok", updatedAt: new Date("2026-08-19T11:00:00Z"), now };
    expect(metricRefreshIsDue({ ...card, orgRefresh: "card" })).toBe(true);
    expect(metricRefreshIsDue({ ...card, orgRefresh: "daily" })).toBe(true);
    expect(metricRefreshIsDue({ ...card, orgRefresh: "weekly" })).toBe(false);
    expect(metricRefreshIsDue({ ...card, orgRefresh: "off", updatedAt: new Date("2026-01-01T00:00:00Z") })).toBe(false);
    expect(metricRefreshIsDue({ ...card, cadence: "weekly", orgRefresh: "daily" })).toBe(false);
  });
});
