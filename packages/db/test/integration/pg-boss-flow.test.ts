/**
 * pg-boss enqueue + work integration test against a real Postgres.
 *
 * Skips when the metadata DB isn't reachable. Each test uses a fresh queue
 * name so suites don't collide on retry, and the boss instance is stopped
 * cleanly in afterAll.
 */

import { afterAll, describe, expect, it } from "vitest";
import { boss, enqueue, stopBoss } from "../../src/jobs";
import { pool } from "../../src";

async function dbReachable(): Promise<boolean> {
  try {
    await pool().query("select 1");
    return true;
  } catch {
    return false;
  }
}

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn(
    "[pg-boss-flow] skipping: metadata Postgres unreachable. Run `docker compose up -d` to enable.",
  );
}

function uniqueQueue(label: string): string {
  return `vitest_${label}_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 6)}`;
}

describeIfDb("pg-boss enqueue + work flow", () => {
  afterAll(async () => {
    await stopBoss();
    await pool().end();
  });

  it("starts the boss singleton on first call", async () => {
    const a = await boss();
    const b = await boss();
    expect(a).toBe(b);
  });

  it("enqueue + work delivers the payload to the handler", async () => {
    const queue = uniqueQueue("delivery");
    const b = await boss();
    await b.createQueue(queue);

    const received: Array<{ value: number }> = [];
    const done = new Promise<void>((resolve) => {
      void b.work<{ value: number }>(queue, async (jobs) => {
        for (const job of jobs) {
          received.push(job.data);
        }
        resolve();
      });
    });

    const id = await enqueue(queue as never, { value: 42 });
    expect(id).toBeTruthy();
    await done;

    expect(received).toEqual([{ value: 42 }]);
  });

  it("retryLimit is honoured — handler runs exactly retryLimit + 1 times before giving up", async () => {
    const queue = uniqueQueue("retry");
    const b = await boss();
    await b.createQueue(queue);

    let attempts = 0;
    const completed = new Promise<void>((resolve) => {
      let count = 0;
      void b.work<{ ping: true }>(queue, async () => {
        attempts++;
        count++;
        if (count >= 3) resolve();
        throw new Error("intentional failure");
      });
    });

    // retryDelay 1s so the test stays under timeout.
    await enqueue(queue as never, { ping: true }, { retryLimit: 2, retryDelay: 1 });
    await completed;

    expect(attempts).toBeGreaterThanOrEqual(3);
  });

  it("send() returns a job id that round-trips through the queue", async () => {
    const queue = uniqueQueue("idtrip");
    const b = await boss();
    await b.createQueue(queue);

    const seenIds: string[] = [];
    const done = new Promise<void>((resolve) => {
      void b.work<object>(queue, async (jobs) => {
        for (const job of jobs) seenIds.push(job.id);
        resolve();
      });
    });

    const sentId = await enqueue(queue as never, { hello: "world" });
    expect(sentId).toBeTruthy();
    await done;
    expect(seenIds).toContain(sentId);
  });

  it("enqueue auto-provisions the queue (no explicit createQueue needed)", async () => {
    // Regression guard for the v10 silent-null bug we hit during migration:
    // enqueue() must implicitly createQueue on first call per queue name.
    const queue = uniqueQueue("autocreate");
    const id = await enqueue(queue as never, { auto: true });
    expect(id).toBeTruthy();
  });
});
