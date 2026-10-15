import type { PoolClient } from "pg";
import { describe, expect, it } from "vitest";
import { withSerializableTransaction } from "../src/workflows/api-admission";

function fakeClient(log: string[]): PoolClient {
  return {
    query: async (sql: string) => {
      log.push(sql);
      return { rows: [] };
    },
    release: () => log.push("release"),
  } as unknown as PoolClient;
}

const conflict = Object.assign(new Error("could not serialize access due to concurrent update"), { code: "40001" });

describe("workflow API serializable transaction", () => {
  it("retries serialization conflicts until the transaction commits", async () => {
    const log: string[] = [];
    let calls = 0;
    const result = await withSerializableTransaction(async () => {
      calls += 1;
      if (calls < 5) throw conflict;
      return "admitted";
    }, async () => fakeClient(log));
    expect(result).toBe("admitted");
    expect(calls).toBe(5);
    expect(log.filter((sql) => sql === "COMMIT")).toHaveLength(1);
    expect(log.filter((sql) => sql === "release")).toHaveLength(5);
  });

  it("answers 503 with Retry-After when conflicts continue", async () => {
    await expect(
      withSerializableTransaction(async () => {
        throw conflict;
      }, async () => fakeClient([])),
    ).rejects.toMatchObject({ code: "busy", status: 503, retryAfterSeconds: 1 });
  });

  it("rethrows other errors at once", async () => {
    let calls = 0;
    await expect(
      withSerializableTransaction(async () => {
        calls += 1;
        throw new Error("boom");
      }, async () => fakeClient([])),
    ).rejects.toThrow("boom");
    expect(calls).toBe(1);
  });
});
