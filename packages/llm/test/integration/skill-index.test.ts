// Ax finds skills by meaning: the index mirrors installed skills and search
// ranks them by cosine distance for the run's actor.

import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { db, eq, pool, skill_embedding } from "@neko/db";
import { createTestOrg, dbReachable, deleteTestOrg, uniqueOrgId } from "@neko/db/test-helpers";

vi.mock("../../src/embedding", async (importOriginal) => {
  const { fakeEmbedding } = await import("../helpers/fake-embedding");
  // Each topic is one axis, so texts that share a topic sit close together.
  const topics = [/spreadsheet|excel|xlsx|workbook/i, /word|document|docx|letter/i, /customer|reply|complaint/i];
  return fakeEmbedding(await importOriginal(), (text) => {
    const v = new Array<number>(768).fill(0.001);
    topics.forEach((topic, i) => {
      if (topic.test(text)) v[i] = 1;
    });
    return v;
  });
});

// Queuing the background job needs pg-boss; the test drains the backlog itself.
vi.mock("../../src/embedding-jobs", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../src/embedding-jobs")>()),
  requestEmbeddingBackfill: vi.fn(async () => true),
}));

import { searchSkillsForRun, syncSkillIndex } from "../../src/work/skill-index";
import { setSemanticSearchEnabledForTests } from "../../src/semantic-search";
import { drainEmbeddingBacklog } from "../helpers/fake-embedding";
import * as embedding from "../../src/embedding";
import { createWorkRun, createWorkThread } from "../../src/work/store";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

describeIfDb("skill index", () => {
  const orgId = uniqueOrgId("skills");
  let runId: string;

  beforeAll(async () => {
    await createTestOrg(orgId);
    const thread = await createWorkThread(orgId, "skills");
    const run = await createWorkRun(orgId, thread.id, "ax", { userId: null, role: "admin" });
    runId = run.id;
  });

  afterAll(async () => {
    setSemanticSearchEnabledForTests(null);
    await deleteTestOrg(orgId);
    await pool().end();
  });

  it("finds the guide closest in meaning and follows installs and removals", async () => {
    setSemanticSearchEnabledForTests(true);
    await syncSkillIndex(orgId, [
      { name: "xlsx", description: "Use this skill any time a spreadsheet file is the primary input or output" },
      { name: "docx", description: "Create and edit Word documents" },
      { name: "customer-reply", description: "Answer a customer complaint in the house voice" },
    ]);
    // Syncing never embeds inline; the background job indexes the rows.
    const synced = await db().select().from(skill_embedding).where(eq(skill_embedding.org_id, orgId));
    expect(synced.every((row) => row.embedding === null)).toBe(true);
    await drainEmbeddingBacklog();
    const rows = await db().select().from(skill_embedding).where(eq(skill_embedding.org_id, orgId));
    expect(rows).toHaveLength(3);
    expect(rows.every((row) => row.embedding !== null)).toBe(true);
    // Name as title, description as text, no prompt (task none).
    expect(embedding.embedSkillDocuments).toHaveBeenCalled();

    const excel = await searchSkillsForRun({ orgId, runId, query: "Make me an Excel file of stock", limit: 1 });
    expect(excel.map((s) => s.name)).toEqual(["xlsx"]);
    const reply = await searchSkillsForRun({ orgId, runId, query: "draft a reply to an upset customer", limit: 1 });
    expect(reply.map((s) => s.name)).toEqual(["customer-reply"]);

    await syncSkillIndex(orgId, [
      { name: "xlsx", description: "Use this skill any time a spreadsheet file is the primary input or output" },
      { name: "docx", description: "Draft letters as Word documents" },
    ]);
    const after = await db().select().from(skill_embedding).where(eq(skill_embedding.org_id, orgId));
    expect(after.map((row) => row.skill_name).sort()).toEqual(["docx", "xlsx"]);
    expect(after.find((row) => row.skill_name === "docx")?.document).toBe("docx\nDraft letters as Word documents");
  });

  it("searches by keyword over name and description when semantic search is off or failing", async () => {
    setSemanticSearchEnabledForTests(false);
    vi.mocked(embedding.embedSkillQuery).mockClear();
    const off = await searchSkillsForRun({ orgId, runId, query: "spreadsheet input", limit: 3 });
    expect(off.map((s) => s.name)).toEqual(["xlsx"]);
    expect(embedding.embedSkillQuery).not.toHaveBeenCalled();
    setSemanticSearchEnabledForTests(true);
    vi.mocked(embedding.embedSkillQuery).mockRejectedValueOnce(new Error("timeout"));
    const failing = await searchSkillsForRun({ orgId, runId, query: "draft letters", limit: 3 });
    expect(failing.map((s) => s.name)).toEqual(["docx"]);
  });

  it("finds nothing for an unknown run", async () => {
    const results = await searchSkillsForRun({ orgId, runId: "00000000-0000-0000-0000-000000000000", query: "excel" });
    expect(results).toEqual([]);
  });
});
