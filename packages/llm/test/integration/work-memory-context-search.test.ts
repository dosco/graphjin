// Integration tests for memory context search: pgvector when semantic search
// is on, Postgres full-text search when it is off or the service fails.
//
// We fake the embedding service so the suite loads no model, but
// hit a real Postgres so Drizzle column types and the SQL shape are
// actually exercised — that's the bug class that bit us twice in a row
// (rows.map / row.created_at.toISOString) on the first wiring.

import { afterAll, beforeEach, describe, expect, it, vi } from "vitest";
import { pool, db, eq, work_memory } from "@neko/db";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";

// Fake the embedding service with a deterministic 768-dim vector per text.
vi.mock("../../src/embedding", async (importOriginal) => {
  const { fakeEmbedding } = await import("../helpers/fake-embedding");
  return fakeEmbedding(await importOriginal());
});

import {
  formatWorkMemoryPromptContext,
  rememberWorkMemory as saveMemory,
  searchWorkMemoryByContext,
} from "../../src/work/memory";

import { drainEmbeddingBacklog } from "../helpers/fake-embedding";
import { setSemanticSearchEnabledForTests } from "../../src/semantic-search";
import * as embedding from "../../src/embedding";
async function rememberWorkMemory(input: Parameters<typeof saveMemory>[0]) {
  const memory = await saveMemory(input);
  await drainEmbeddingBacklog();
  return memory;
}

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[work-memory-context-search] skipping: Postgres unreachable.");
}

describeIfDb("work memory context search (pgvector)", () => {
  beforeEach(() => setSemanticSearchEnabledForTests(true));
  afterAll(async () => {
    setSemanticSearchEnabledForTests(null);
    await pool().end();
  });

  let orgId: string;

  beforeEach(async () => {
    orgId = uniqueOrgId("wm-ctx");
    await createTestOrg(orgId, "WM Ctx");
  });

  it("returns [] when no memories exist for the org", async () => {
    try {
      const results = await searchWorkMemoryByContext({
        orgId,
        query: "anything goes here",
        limit: 5,
      });
      expect(results).toEqual([]);
    } finally {
      await deleteTestOrg(orgId);
    }
  });

  it("returns [] when query is empty / whitespace-only", async () => {
    try {
      await rememberWorkMemory({
        orgId,
        text: "Some saved rule",
        kind: "business_rule",
        scope: "global",
      });
      expect(
        await searchWorkMemoryByContext({ orgId, query: "" }),
      ).toEqual([]);
      expect(
        await searchWorkMemoryByContext({ orgId, query: "   \n\t" }),
      ).toEqual([]);
    } finally {
      await deleteTestOrg(orgId);
    }
  });

  it("indexes the saved memory through the background job and finds the row by context", async () => {
    try {
      const memory = await rememberWorkMemory({
        orgId,
        text: "Always cite the table name in your reasoning",
        kind: "business_rule",
        scope: "global",
      });

      // Verify the embedding column got populated as a non-null pgvector.
      const stored = await db()
        .select({
          id: work_memory.id,
          embedding: work_memory.embedding,
        })
        .from(work_memory)
        .where(eq(work_memory.id, memory.id))
        .limit(1);
      expect(stored[0]?.embedding).toBeTruthy();

      const results = await searchWorkMemoryByContext({
        orgId,
        query: "what should I include in my reasoning",
        limit: 5,
      });

      expect(results).toHaveLength(1);
      expect(results[0].memory.id).toBe(memory.id);
      expect(results[0].memory.text).toBe(
        "Always cite the table name in your reasoning",
      );
      // Scalar score returned, finite number.
      expect(typeof results[0].score).toBe("number");
      expect(Number.isFinite(results[0].score)).toBe(true);
    } finally {
      await deleteTestOrg(orgId);
    }
  });

  it("returns Date objects for timestamp columns (regression: row.created_at.toISOString)", async () => {
    try {
      const memory = await rememberWorkMemory({
        orgId,
        text: "Date columns must be Date instances after search",
        kind: "business_rule",
        scope: "global",
      });

      const results = await searchWorkMemoryByContext({
        orgId,
        query: "date column shape check",
        limit: 5,
      });

      expect(results).toHaveLength(1);
      const m = results[0].memory;
      // The memory shape stringifies dates to ISO; this proves the SQL
      // path returned proper Date objects (the Drizzle column-mode path
      // would crash with .toISOString is not a function on raw strings).
      expect(typeof m.createdAt).toBe("string");
      expect(() => new Date(m.createdAt).toISOString()).not.toThrow();
      expect(typeof m.updatedAt).toBe("string");
      expect(memory.id).toBe(m.id);
    } finally {
      await deleteTestOrg(orgId);
    }
  });

  it("never returns memories from another org (org isolation)", async () => {
    const otherOrg = uniqueOrgId("wm-ctx-other");
    await createTestOrg(otherOrg, "Other org");
    try {
      await rememberWorkMemory({
        orgId: otherOrg,
        text: "Secret rule that must not leak",
        kind: "business_rule",
        scope: "global",
      });
      const results = await searchWorkMemoryByContext({
        orgId,
        query: "secret rule",
        limit: 5,
      });
      expect(results).toEqual([]);
    } finally {
      await deleteTestOrg(otherOrg);
      await deleteTestOrg(orgId);
    }
  });

  it("excludes archived memories from search results", async () => {
    try {
      // Insert two memories, then archive one directly via SQL update.
      const live = await rememberWorkMemory({
        orgId,
        text: "Live memory still available",
        kind: "business_rule",
        scope: "global",
      });
      const archived = await rememberWorkMemory({
        orgId,
        text: "Archived memory should not appear",
        kind: "business_rule",
        scope: "global",
      });
      await db()
        .update(work_memory)
        .set({ archived_at: new Date() })
        .where(eq(work_memory.id, archived.id));

      const results = await searchWorkMemoryByContext({
        orgId,
        query: "memory results",
        limit: 5,
      });
      expect(results.map((r) => r.memory.id)).toEqual([live.id]);
    } finally {
      await deleteTestOrg(orgId);
    }
  });

  it("formatWorkMemoryPromptContext merges context-relevant + core memories without duplication", async () => {
    try {
      const m = await rememberWorkMemory({
        orgId,
        text: "Treat this as durable context",
        kind: "business_rule",
        scope: "global",
        pinned: true,
      });
      const out = await formatWorkMemoryPromptContext(
        { orgId },
        { contextQuery: "find me a context-relevant memory", contextLimit: 5 },
      );
      // Memory text should appear exactly once in the merged section.
      const matches = out.split(m.text).length - 1;
      expect(matches).toBe(1);
      expect(out).toContain("durable context");
    } finally {
      await deleteTestOrg(orgId);
    }
  });

  it("searches by keyword without calling the embedding service when semantic search is off", async () => {
    try {
      setSemanticSearchEnabledForTests(false);
      const memory = await saveMemory({ orgId, text: "Refunds are netted against gross revenue", kind: "business_rule", scope: "global" });
      await saveMemory({ orgId, text: "Weekly reports go out on Monday", kind: "business_rule", scope: "global" });
      vi.mocked(embedding.embedMemoryQuery).mockClear();
      vi.mocked(embedding.embedMemoryDocuments).mockClear();
      await drainEmbeddingBacklog();
      const results = await searchWorkMemoryByContext({ orgId, query: "how are refunds handled in revenue?", limit: 5 });
      expect(results.map((r) => r.memory.id)).toEqual([memory.id]);
      expect(embedding.embedMemoryQuery).not.toHaveBeenCalled();
      expect(embedding.embedMemoryDocuments).not.toHaveBeenCalled();
    } finally {
      await deleteTestOrg(orgId);
    }
  });

  it("falls back to keyword search when the embedding service fails", async () => {
    try {
      const memory = await rememberWorkMemory({ orgId, text: "Inventory counts happen every Friday", kind: "business_rule", scope: "global" });
      vi.mocked(embedding.embedMemoryQuery).mockRejectedValueOnce(new Error("embedding service returned HTTP 503"));
      const results = await searchWorkMemoryByContext({ orgId, query: "when do we count inventory", limit: 5 });
      expect(results.map((r) => r.memory.id)).toEqual([memory.id]);
    } finally {
      await deleteTestOrg(orgId);
    }
  });

  it("finds memories the backfill has not indexed yet alongside indexed ones", async () => {
    try {
      const indexed = await rememberWorkMemory({ orgId, text: "Shipping cutoff is 3pm", kind: "business_rule", scope: "global" });
      setSemanticSearchEnabledForTests(false);
      const pending = await saveMemory({ orgId, text: "Shipping to Canada needs a customs form", kind: "business_rule", scope: "global" });
      setSemanticSearchEnabledForTests(true);
      const results = await searchWorkMemoryByContext({ orgId, query: "shipping customs", limit: 5 });
      expect(results.map((r) => r.memory.id).sort()).toEqual([indexed.id, pending.id].sort());
    } finally {
      await deleteTestOrg(orgId);
    }
  });

  it("applies the same layer visibility to keyword search as to vector search", async () => {
    try {
      setSemanticSearchEnabledForTests(false);
      await saveMemory({ orgId, text: "Team note about invoices", kind: "business_rule", scope: "global", userId: null });
      const archived = await saveMemory({ orgId, text: "Archived invoices rule", kind: "business_rule", scope: "global" });
      await db().update(work_memory).set({ archived_at: new Date() }).where(eq(work_memory.id, archived.id));
      const results = await searchWorkMemoryByContext({ orgId, query: "invoices", limit: 5 });
      expect(results.map((r) => r.memory.text)).toEqual(["Team note about invoices"]);
      const other = await searchWorkMemoryByContext({ orgId: "missing-org", query: "invoices", limit: 5 });
      expect(other).toEqual([]);
    } finally {
      await deleteTestOrg(orgId);
    }
  });
});
