// Deterministic contracts of work memory and skills, kept from the retired
// OpenNeko eval suite (cases m01, m02, m03).

import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { afterAll, describe, expect, it } from "vitest";
import { pool } from "@neko/db";
import { createTestOrg, dbReachable, deleteTestOrg, uniqueOrgId } from "@neko/db/test-helpers";
import {
  archiveWorkMemory,
  findConflictingWorkMemories,
  parseSkillFrontmatter,
  rememberWorkMemory,
  searchWorkMemory,
  upsertWorkSkill,
  writeWorkSkill,
  type WorkMemory,
} from "@neko/llm/work";

const reachable = await dbReachable();

// The corpus is the ground truth: PLANTED facts are what a task needs, and the
// DECOY is plausible org memory that matching queries must rank below them.
const PLANTED_SLA = {
  kind: "business_rule",
  text: "Revenue alert SLA: flag any sales territory when its weekly order value drops below 18500 USD.",
} as const;
const PLANTED_DEFINITION = {
  kind: "metric_definition",
  text: "Active customer: a customer with at least one sales order in the trailing 12 months.",
} as const;
const DECOY = { kind: "preference", text: "The Friday team lunch budget is 240 USD and pizza is preferred." } as const;

const SLA_QUERY = "revenue alert SLA threshold";
const DEFINITION_QUERY = "active customer definition";
const MISS_QUERY = "quarterly carbon emissions audit cadence";
const DECOY_QUERY = "team lunch budget";

(reachable ? describe : describe.skip)("work memory contracts", () => {
  const orgId = uniqueOrgId("contract-memory");
  const strangerOrgId = `${orgId}b`;
  const ablatedOrgId = `${orgId}c`;
  let seeded: { planted: WorkMemory; definition: WorkMemory; decoy: WorkMemory };

  afterAll(async () => {
    for (const org of [orgId, strangerOrgId, ablatedOrgId]) await deleteTestOrg(org);
    await pool().end();
  });

  it("stores the corpus, normalizes scope and detects duplicates", async () => {
    await createTestOrg(orgId, "Work memory contract");
    const remember = (draft: { kind: string; text: string }) =>
      rememberWorkMemory({ orgId, kind: draft.kind as WorkMemory["kind"], scope: "global", text: draft.text, metadata: { source: "contract-fixture" } });
    seeded = { planted: await remember(PLANTED_SLA), definition: await remember(PLANTED_DEFINITION), decoy: await remember(DECOY) };
    expect(seeded.planted.text).toBe(PLANTED_SLA.text);
    expect(seeded.definition.text).toBe(PLANTED_DEFINITION.text);
    expect(seeded.decoy.text).toBe(DECOY.text);
    expect(seeded.planted).toMatchObject({ scope: "global", scopeId: null });
    const conflicts = await findConflictingWorkMemories({ orgId, text: PLANTED_SLA.text, kind: PLANTED_SLA.kind, scope: "global" }, 0.9);
    expect(conflicts[0]?.memory.id).toBe(seeded.planted.id);
  });

  it("ranks the planted fact first and a miss below a hit", async () => {
    const hit = await searchWorkMemory({ orgId, query: SLA_QUERY });
    const miss = await searchWorkMemory({ orgId, query: MISS_QUERY });
    // Search is recall-oriented: global memories carry a base score, so the
    // contract is ranking separation rather than absolute exclusion.
    const hitScore = hit.saved.find((result) => result.memory.id === seeded.planted.id)?.score;
    const decoyScore = hit.saved.find((result) => result.memory.id === seeded.decoy.id)?.score;
    expect(hit.saved.map((result) => result.memory.text)).toContain(PLANTED_SLA.text);
    expect(hit.saved[0]?.memory.id).toBe(seeded.planted.id);
    expect(hitScore).toBeDefined();
    expect(decoyScore === undefined || hitScore! > decoyScore).toBe(true);
    expect(miss.saved[0] === undefined || miss.saved[0].score < hitScore!).toBe(true);
    expect(hit.archives).toHaveLength(0);
    expect(miss.archives).toHaveLength(0);
    expect((await searchWorkMemory({ orgId, query: SLA_QUERY })).saved[0]!.memory.useCount).toBeGreaterThanOrEqual(1);
  });

  it("recovers the threshold and definition with memory, and nothing without it", async () => {
    const sla = await searchWorkMemory({ orgId, query: SLA_QUERY });
    const definition = await searchWorkMemory({ orgId, query: DEFINITION_QUERY });
    expect(/\b(\d{4,6}) USD\b/u.exec(sla.saved[0]?.memory.text ?? "")?.[1]).toBe("18500");
    expect(definition.saved[0]?.memory.id).toBe(seeded.definition.id);

    await createTestOrg(ablatedOrgId, "Work memory ablated");
    const ablatedSla = await searchWorkMemory({ orgId: ablatedOrgId, query: SLA_QUERY });
    const ablatedDefinition = await searchWorkMemory({ orgId: ablatedOrgId, query: DEFINITION_QUERY });
    expect(ablatedSla.saved.length + ablatedDefinition.saved.length).toBe(0);
    expect(ablatedSla.archives.length + ablatedDefinition.archives.length).toBe(0);
  });

  it("keeps memory inside its org and drops archived memory from search", async () => {
    await createTestOrg(strangerOrgId, "Work memory stranger");
    expect((await searchWorkMemory({ orgId: strangerOrgId, query: SLA_QUERY })).saved).toHaveLength(0);
    expect(await archiveWorkMemory(orgId, seeded.decoy.id, { reason: "contract isolation" })).toBe(true);
    const after = await searchWorkMemory({ orgId, query: DECOY_QUERY });
    expect(after.saved.map((result) => result.memory.text)).not.toContain(DECOY.text);
  });
});

describe("work skill contracts", () => {
  it("round-trips a skill, keeps its path on update and blocks escapes and bad names", async () => {
    const skillsRoot = await mkdtemp(join(tmpdir(), "openneko-contract-skills-"));
    try {
      const draft = {
        name: "Revenue Variance Report",
        description: "Build a quarterly revenue variance report grounded in the connected source.",
        body: "Use the read-only data path; cite every figure.",
        allowedTools: "Bash(python3:*)",
        metadata: { audience: "finance" },
      };
      const written = await writeWorkSkill(skillsRoot, draft);
      expect(written.name).toBe("revenue-variance-report");
      const frontmatter = parseSkillFrontmatter(await readFile(resolve(written.skillPath, "SKILL.md"), "utf8"));
      expect(frontmatter).toMatchObject({ name: "revenue-variance-report", description: draft.description, allowedTools: draft.allowedTools });
      const updated = await writeWorkSkill(skillsRoot, { ...draft, description: "Build a quarterly revenue variance report with citations." });
      expect(updated.skillPath).toBe(written.skillPath);
      expect(parseSkillFrontmatter(await readFile(resolve(updated.skillPath, "SKILL.md"), "utf8")).description).toBe(
        "Build a quarterly revenue variance report with citations.",
      );
      await expect(writeWorkSkill(skillsRoot, { ...draft, files: [{ path: "../escape.txt", content: "must never be written" }] })).rejects.toThrow();
      expect(() => upsertWorkSkill(skillsRoot, { ...draft, name: "!!!" })).toThrow();
    } finally {
      await rm(skillsRoot, { recursive: true, force: true });
    }
  });
});
