import { describe, expect, it, vi } from "vitest";
import { ensureQueueExists } from "../src/pg-boss-helpers";

describe("ensureQueueExists", () => {
  it("calls createQueue with the right options on first boot", async () => {
    const create = vi.fn(async () => undefined);
    await ensureQueueExists(create, "metric_refresh");
    expect(create).toHaveBeenCalledWith("metric_refresh", {
      name: "metric_refresh",
      expireInSeconds: 600,
    });
  });

  it("respects a custom expireInSeconds", async () => {
    const create = vi.fn(async () => undefined);
    await ensureQueueExists(create, "metric_refresh", 1234);
    expect(create).toHaveBeenCalledWith("metric_refresh", {
      name: "metric_refresh",
      expireInSeconds: 1234,
    });
  });

  it("swallows pg error 42P07 'relation already exists' (idempotency on reboot)", async () => {
    const err = Object.assign(new Error("relation foo already exists"), {
      code: "42P07",
    });
    const create = vi.fn(async () => {
      throw err;
    });
    await expect(
      ensureQueueExists(create, "metric_refresh"),
    ).resolves.toBeUndefined();
  });

  it("rethrows any non-42P07 error so real failures still surface", async () => {
    const err = Object.assign(new Error("connection refused"), {
      code: "ECONNREFUSED",
    });
    const create = vi.fn(async () => {
      throw err;
    });
    await expect(
      ensureQueueExists(create, "metric_refresh"),
    ).rejects.toBe(err);
  });

  it("rethrows errors that lack a `code` field (treats as unknown failure)", async () => {
    const err = new Error("plain error with no code");
    const create = vi.fn(async () => {
      throw err;
    });
    await expect(
      ensureQueueExists(create, "metric_refresh"),
    ).rejects.toBe(err);
  });
});
