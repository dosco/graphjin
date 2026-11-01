// Integration test for the inbound ledger (dedup + dead-letter) + poll cursor.
// The guarantees here are DB-enforced — a UNIQUE index + ON CONFLICT atomic
// claim, an attempt counter, a dead-letter transition — so they're tested
// against real Postgres; a hand-rolled mock would test the mock, not the SQL.
//
// Self-skips when no DB is reachable (e.g. CI without the compose stack), so
// the suite stays green everywhere. inboundUpdateKey is pure and always runs.
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { db, eq, inbound_dedup, reconnectPool } from "@neko/db";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import {
  beginInboundUpdate,
  inboundUpdateKey,
  loadPollCursor,
  markInboundDone,
  pruneInboundDedup,
  recordInboundFailure,
  savePollCursor,
} from "../../src/channels/inbound-store";

describe("inboundUpdateKey (pure)", () => {
  it("is stable for the same value", () => {
    const u = { update_id: 42, message: { text: "hi", chat: { id: 7 } } };
    expect(inboundUpdateKey(u)).toBe(inboundUpdateKey({ ...u }));
  });

  it("is independent of key order (canonical JSON)", () => {
    const a = { update_id: 1, message: { text: "hi", chat: { id: 7 } } };
    const b = { message: { chat: { id: 7 }, text: "hi" }, update_id: 1 };
    expect(inboundUpdateKey(a)).toBe(inboundUpdateKey(b));
  });

  it("differs when content differs", () => {
    expect(inboundUpdateKey({ update_id: 1 })).not.toBe(inboundUpdateKey({ update_id: 2 }));
  });

  it("distinguishes nested + array differences", () => {
    expect(inboundUpdateKey({ a: [1, 2] })).not.toBe(inboundUpdateKey({ a: [2, 1] }));
    expect(inboundUpdateKey({ a: { b: 1 } })).not.toBe(inboundUpdateKey({ a: { b: 2 } }));
  });
});

describe("inbound ledger + cursor (real Postgres)", () => {
  let dbUp = false;
  let orgId = "";
  // Own a unique org per run. Borrowing the first existing row races a
  // concurrent test file that creates+deletes its own org, FK-breaking our
  // inserts; cascade FKs clean up our child rows on teardown.
  const plugin = `@test/inbound-store-${Math.random().toString(36).slice(2)}`;
  const keyOf = (id: number) => inboundUpdateKey({ update_id: id, plugin });

  beforeAll(async () => {
    dbUp = await dbReachable();
    if (dbUp) {
      orgId = uniqueOrgId("inbound-store");
      await createTestOrg(orgId);
    }
  });

  afterAll(async () => {
    if (dbUp) await deleteTestOrg(orgId);
    await reconnectPool();
  });

  it("claims once, allows a retry while pending, then dedups after done", async (ctx) => {
    if (!dbUp) ctx.skip();
    const key = keyOf(100);
    expect(await beginInboundUpdate(orgId, plugin, key)).toMatchObject({ proceed: true, attempts: 0, dead: false });
    // Still 'pending' (a crash before completion / a re-poll) ⇒ retry proceeds.
    expect(await beginInboundUpdate(orgId, plugin, key)).toMatchObject({ proceed: true, dead: false });
    await markInboundDone(orgId, plugin, key);
    // Now dispatched ⇒ a duplicate (restart re-poll, webhook retry) is skipped.
    expect(await beginInboundUpdate(orgId, plugin, key)).toMatchObject({ proceed: false, dead: false });
  });

  it("counts failures and dead-letters at the attempt cap, retaining payload + error", async (ctx) => {
    if (!dbUp) ctx.skip();
    const key = keyOf(200);
    expect(await beginInboundUpdate(orgId, plugin, key)).toMatchObject({ proceed: true, attempts: 0 });

    expect(await recordInboundFailure(orgId, plugin, key, 3, { update_id: 200 }, "boom-1")).toEqual({ dead: false, attempts: 1 });
    // A retry still proceeds and carries the running attempt count.
    expect(await beginInboundUpdate(orgId, plugin, key)).toMatchObject({ proceed: true, attempts: 1, dead: false });
    expect(await recordInboundFailure(orgId, plugin, key, 3, { update_id: 200 }, "boom-2")).toEqual({ dead: false, attempts: 2 });
    expect(await recordInboundFailure(orgId, plugin, key, 3, { update_id: 200 }, "boom-3")).toEqual({ dead: true, attempts: 3 });

    // Dead-lettered ⇒ skipped, never re-dispatched; payload + last error kept.
    expect(await beginInboundUpdate(orgId, plugin, key)).toMatchObject({ proceed: false, dead: true });
    const row = await db()
      .select({
        status: inbound_dedup.status,
        last_error: inbound_dedup.last_error,
        payload: inbound_dedup.payload,
      })
      .from(inbound_dedup)
      .where(eq(inbound_dedup.update_key, key))
      .limit(1);
    expect(row[0]).toMatchObject({ status: "dead", last_error: "boom-3", payload: { update_id: 200 } });
  });

  it("a successful dispatch after failures clears the retry (markInboundDone wins)", async (ctx) => {
    if (!dbUp) ctx.skip();
    const key = keyOf(250);
    await beginInboundUpdate(orgId, plugin, key);
    await recordInboundFailure(orgId, plugin, key, 30, { update_id: 250 }, "transient");
    await markInboundDone(orgId, plugin, key);
    expect(await beginInboundUpdate(orgId, plugin, key)).toMatchObject({ proceed: false, dead: false });
  });

  it("persists + resumes the poll cursor (upsert)", async (ctx) => {
    if (!dbUp) ctx.skip();
    expect(await loadPollCursor(orgId, plugin)).toBeUndefined();
    await savePollCursor(orgId, plugin, "offset-1");
    expect(await loadPollCursor(orgId, plugin)).toBe("offset-1");
    await savePollCursor(orgId, plugin, "offset-2");
    expect(await loadPollCursor(orgId, plugin)).toBe("offset-2");
  });

  it("prunes done rows past the TTL but keeps dead letters and fresh rows", async (ctx) => {
    if (!dbUp) ctx.skip();
    const old = new Date(Date.now() - 8 * 24 * 60 * 60 * 1000);
    const doneOld = keyOf(300);
    const deadOld = keyOf(301);
    const fresh = keyOf(302);
    await db().insert(inbound_dedup).values([
      { org_id: orgId, channel_plugin: plugin, update_key: doneOld, status: "done", created_at: old },
      { org_id: orgId, channel_plugin: plugin, update_key: deadOld, status: "dead", created_at: old },
    ]);
    await beginInboundUpdate(orgId, plugin, fresh);
    await markInboundDone(orgId, plugin, fresh);

    await pruneInboundDedup(Date.now());

    // Old 'done' pruned ⇒ claimable again; old 'dead' kept ⇒ still skipped;
    // fresh 'done' kept ⇒ still deduped.
    expect(await beginInboundUpdate(orgId, plugin, doneOld)).toMatchObject({ proceed: true });
    expect(await beginInboundUpdate(orgId, plugin, deadOld)).toMatchObject({ proceed: false, dead: true });
    expect(await beginInboundUpdate(orgId, plugin, fresh)).toMatchObject({ proceed: false, dead: false });
  });
});
