import { EventEmitter } from "node:events";
import pg from "pg";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  guardPoolErrors,
  isDatabaseUnavailable,
  keepProcessThroughDatabaseRestarts,
  retryWhileDatabaseUnavailable,
} from "../src/pool-errors";

describe("guardPoolErrors", () => {
  it("logs an error from a checked-out client instead of throwing", () => {
    const pool = guardPoolErrors(new pg.Pool(), "test");
    const client = new EventEmitter() as unknown as pg.PoolClient;
    const log = vi.spyOn(console, "error").mockImplementation(() => {});
    pool.emit("acquire", client);
    pool.emit("acquire", client);
    expect(client.listenerCount("error")).toBe(1);
    expect(() => client.emit("error", new Error("Connection terminated unexpectedly"))).not.toThrow();
    expect(log).toHaveBeenCalledWith("[test] checked-out connection error:", "Connection terminated unexpectedly");
    pool.emit("release", undefined, client);
    expect(client.listenerCount("error")).toBe(0);
    expect(() => pool.emit("error", new Error("idle drop"), client)).not.toThrow();
    log.mockRestore();
  });
});

const recovering = Object.assign(new Error("the database system is in recovery mode"), { code: "57P03" });

describe("database outage helpers", () => {
  afterEach(() => vi.restoreAllMocks());

  it("tells a restarting database from a query fault", () => {
    expect(isDatabaseUnavailable(recovering)).toBe(true);
    expect(isDatabaseUnavailable(new Error("Connection terminated unexpectedly"))).toBe(true);
    expect(isDatabaseUnavailable(Object.assign(new Error("duplicate key"), { code: "23505" }))).toBe(false);
    expect(isDatabaseUnavailable(undefined)).toBe(false);
  });

  it("retries through a restart and fails a query fault at once", async () => {
    let calls = 0;
    const result = await retryWhileDatabaseUnavailable(async () => {
      calls += 1;
      if (calls < 3) throw recovering;
      return "saved";
    }, { baseMs: 1 });
    expect(result).toBe("saved");
    expect(calls).toBe(3);
    const fault = Object.assign(new Error("bad input"), { code: "22P02" });
    let faultCalls = 0;
    await expect(retryWhileDatabaseUnavailable(async () => { faultCalls += 1; throw fault; }, { baseMs: 1 })).rejects.toBe(fault);
    expect(faultCalls).toBe(1);
  });

  it("gives up after the last attempt", async () => {
    await expect(retryWhileDatabaseUnavailable(async () => { throw recovering; }, { attempts: 2, baseMs: 1 })).rejects.toBe(recovering);
  });

  it("logs an outage rejection and rethrows any other", () => {
    const on = vi.spyOn(process, "on");
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    keepProcessThroughDatabaseRestarts("test");
    const handler = on.mock.calls.find(([event]) => event === "unhandledRejection")![1] as (reason: unknown) => void;
    process.removeListener("unhandledRejection", handler);
    expect(() => handler(recovering)).not.toThrow();
    expect(error).toHaveBeenCalled();
    const other = new Error("bug");
    expect(() => handler(other)).toThrow(other);
  });
});
