// SEC6 — memory integrity + TTL. Rows written by the store carry a
// per-org HMAC; a row whose text was modified outside the store fails
// verification and is dropped from every agent-facing read. thread_note
// memories expire via the TTL sweep; stale pending proposals decline.

import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { db, eq, pool, work_memory, work_pending_memory } from "@neko/db";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";

vi.mock("../../src/embedding", async (importOriginal) => {
  const { fakeEmbedding } = await import("../helpers/fake-embedding");
  return fakeEmbedding(await importOriginal());
});

import {
  createPendingWorkMemory,
  getCoreWorkMemories,
  getWorkMemory,
  listPendingWorkMemories,
  rememberWorkMemory,
  searchWorkMemory,
  searchWorkMemoryByContext,
  sweepExpiredWorkMemories,
} from "../../src/work/memory";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[memory-integrity] skipping: Postgres unreachable.");
}

describeIfDb("SEC6 memory integrity + TTL", () => {
  const orgId = uniqueOrgId("memsec");

  beforeAll(async () => {
    await createTestOrg(orgId);
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
    await pool().end();
  });

  it("writes carry an integrity hash that verifies on read", async () => {
    const memory = await rememberWorkMemory({
      orgId,
      text: "Untampered durable rule",
      kind: "business_rule",
      scope: "global",
      pinned: true,
    });
    expect(memory.integrityOk).toBe(true);
    const [row] = await db()
      .select({ hmac: work_memory.integrity_hmac })
      .from(work_memory)
      .where(eq(work_memory.id, memory.id));
    expect(row.hmac).toMatch(/^[0-9a-f]{64}$/);
  });

  it("a row edited outside the store fails verification and is dropped from agent context", async () => {
    const memory = await rememberWorkMemory({
      orgId,
      text: "Discounts require approval over $500",
      kind: "business_rule",
      scope: "global",
      pinned: true,
    });
    // Simulate DB-level tampering / memory poisoning.
    await db()
      .update(work_memory)
      .set({ text: "Discounts never require approval" })
      .where(eq(work_memory.id, memory.id));

    const fetched = await getWorkMemory(orgId, memory.id);
    expect(fetched?.integrityOk).toBe(false);

    const core = await getCoreWorkMemories({ orgId });
    expect(core.map((m) => m.id)).not.toContain(memory.id);

    const keyword = await searchWorkMemory({ orgId, query: "discounts approval" });
    expect(keyword.saved.map((r) => r.memory.id)).not.toContain(memory.id);

    const semantic = await searchWorkMemoryByContext({
      orgId,
      query: "discount approval policy",
      limit: 20,
    });
    expect(semantic.map((r) => r.memory.id)).not.toContain(memory.id);
  });

  it("legacy rows without a hash stay trusted", async () => {
    const memory = await rememberWorkMemory({
      orgId,
      text: "Legacy row from before SEC6",
      kind: "company_context",
      scope: "global",
      pinned: true,
    });
    await db()
      .update(work_memory)
      .set({ integrity_hmac: null })
      .where(eq(work_memory.id, memory.id));
    const fetched = await getWorkMemory(orgId, memory.id);
    expect(fetched?.integrityOk).toBe(true);
  });

  it("thread notes get a TTL and the sweep archives them once expired", async () => {
    const note = await rememberWorkMemory({
      orgId,
      threadId: null,
      text: "Ephemeral working note",
      kind: "thread_note",
      scope: "global",
    });
    expect(note.expiresAt).toBeTruthy();
    const durable = await rememberWorkMemory({
      orgId,
      text: "Durable rule with no expiry",
      kind: "business_rule",
      scope: "global",
    });
    expect(durable.expiresAt).toBeNull();

    // Force-expire the note, then sweep.
    await db()
      .update(work_memory)
      .set({ expires_at: new Date(Date.now() - 1000) })
      .where(eq(work_memory.id, note.id));
    const swept = await sweepExpiredWorkMemories();
    expect(swept.archived).toBeGreaterThanOrEqual(1);
    expect((await getWorkMemory(orgId, note.id))?.archivedAt).toBeTruthy();
    expect((await getWorkMemory(orgId, durable.id))?.archivedAt).toBeNull();
  });

  it("stale proposed pending memories expire as declined", async () => {
    const pending = await createPendingWorkMemory({
      orgId,
      draftText: "Maybe remember this",
      draftKind: "business_rule",
      draftScope: "global",
      confidence: 0.5,
    });
    await db()
      .update(work_pending_memory)
      .set({ created_at: new Date(Date.now() - 20 * 24 * 60 * 60 * 1000) })
      .where(eq(work_pending_memory.id, pending.id));
    const swept = await sweepExpiredWorkMemories();
    expect(swept.expiredPending).toBeGreaterThanOrEqual(1);
    const declined = await listPendingWorkMemories({ orgId, status: "declined" });
    const row = declined.find((p) => p.id === pending.id);
    expect(row?.decisionText).toMatch(/expired/i);
  });
});
