// Ax finds skills by meaning: the index mirrors installed skills and search
// ranks them by cosine distance for the run's actor.

import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { db, eq, pool, skill_embedding } from "@neko/db";
import { createTestOrg, dbReachable, deleteTestOrg, uniqueOrgId } from "@neko/db/test-helpers";

// Each topic is one axis, so texts that share a topic sit close together.
const TOPICS = [/spreadsheet|excel|xlsx|workbook/i, /word|document|docx|letter/i, /customer|reply|complaint/i];
vi.mock("../../src/embedding", async () => ({
  EMBEDDING_DIM: 384,
  embedText: vi.fn(async (text: string) => {
    const v = new Array<number>(384).fill(0.001);
    TOPICS.forEach((topic, i) => {
      if (topic.test(text)) v[i] = 1;
    });
    return v;
  }),
  vectorLiteral: (vec: number[]) => `[${vec.join(",")}]`,
}));

import { searchSkillsForRun, syncSkillIndex } from "../../src/work/skill-index";
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
    await deleteTestOrg(orgId);
    await pool().end();
  });

  it("finds the guide closest in meaning and follows installs and removals", async () => {
    await syncSkillIndex(orgId, [
      { name: "xlsx", description: "Use this skill any time a spreadsheet file is the primary input or output" },
      { name: "docx", description: "Create and edit Word documents" },
      { name: "customer-reply", description: "Answer a customer complaint in the house voice" },
    ]);
    const rows = await db().select().from(skill_embedding).where(eq(skill_embedding.org_id, orgId));
    expect(rows).toHaveLength(3);
    expect(rows.every((row) => row.embedding !== null)).toBe(true);

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

  it("finds nothing for an unknown run", async () => {
    const results = await searchSkillsForRun({ orgId, runId: "00000000-0000-0000-0000-000000000000", query: "excel" });
    expect(results).toEqual([]);
  });
});
