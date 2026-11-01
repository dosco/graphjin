import { mkdir, mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, describe, expect, it } from "vitest";
import { detectSkillUse } from "../src/work/skill-usage";
import { composeSkillTree } from "../src/work/skill-overlay";
import { runSkillLearn } from "../src/work/skill-learn";
import { fingerprintSkillTree } from "../src/work/workspace";

const PACK = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "../../../packs/magento/skills",
);
const REFUNDS = join(PACK, "magento-investigate-refunds");
const FULFILL = join(PACK, "magento-triage-fulfillment");

const cleanup: string[] = [];
afterEach(async () => {
  while (cleanup.length > 0) {
    const path = cleanup.pop();
    if (path) await rm(path, { recursive: true, force: true });
  }
});

describe("Magento refunds and fulfillment skill paths", () => {
  it("records SKILL.md reads for both Magento skills", () => {
    expect(
      detectSkillUse({
        type: "tool_start",
        id: "r1",
        name: "read",
        input: {
          locations: [{ path: "/org/skills/magento-investigate-refunds/SKILL.md" }],
        },
      }),
    ).toEqual({ name: "magento-investigate-refunds", source: "read" });
    expect(
      detectSkillUse({
        type: "tool_start",
        id: "f1",
        name: "read",
        input: {
          locations: [{ path: "/org/skills/magento-triage-fulfillment/SKILL.md" }],
        },
      }),
    ).toEqual({ name: "magento-triage-fulfillment", source: "read" });
  });

  it("composes learned guidance without dropping Magento references", async () => {
    const dest = await mkdtemp(join(tmpdir(), "neko-m2-"));
    cleanup.push(dest);
    const refundsDest = join(dest, "refunds");
    const fulfillDest = join(dest, "fulfill");
    const fulfillStale = join(dest, "fulfill-stale");
    const refundsHash = await fingerprintSkillTree(REFUNDS);
    const fulfillHash = await fingerprintSkillTree(FULFILL);

    expect(
      await composeSkillTree({
        baseDir: REFUNDS,
        destDir: refundsDest,
        overlay: {
          skillName: "magento-investigate-refunds",
          baseHash: refundsHash,
          status: "applied",
          body: "Prefer parent items when product structures would double-count.",
        },
      }),
    ).toBe("applied");
    expect(
      await readFile(join(refundsDest, "references", "refunds-and-cancellations.md"), "utf8"),
    ).toMatch(/credit memo/i);
    expect(await readFile(join(refundsDest, "SKILL.md"), "utf8")).toContain(
      "Prefer parent items",
    );

    expect(
      await composeSkillTree({
        baseDir: FULFILL,
        destDir: fulfillDest,
        overlay: {
          skillName: "magento-triage-fulfillment",
          baseHash: fulfillHash,
          status: "applied",
          body: "Never return an item to its origin location.",
        },
      }),
    ).toBe("applied");
    expect(
      await readFile(join(fulfillDest, "references", "fulfillment-rules.md"), "utf8"),
    ).toMatch(/fulfill/i);

    expect(
      await composeSkillTree({
        baseDir: FULFILL,
        destDir: fulfillStale,
        overlay: {
          skillName: "magento-triage-fulfillment",
          baseHash: "stale",
          status: "applied",
          body: "hide-me",
        },
      }),
    ).toBe("base-only");
    const staleSkill = await readFile(join(fulfillStale, "SKILL.md"), "utf8");
    expect(staleSkill).not.toContain("hide-me");
    expect(
      await readFile(join(fulfillStale, "references", "fulfillment-rules.md"), "utf8"),
    ).toMatch(/fulfill/i);
  });

  it("applies a refunds lesson to LEARNED.md and leaves pack SKILL.md unchanged", async () => {
    const orgRoot = await mkdtemp(join(tmpdir(), "neko-m2-org-"));
    cleanup.push(orgRoot);
    const skillDir = join(orgRoot, "skills", "magento-investigate-refunds");
    await mkdir(skillDir, { recursive: true });
    await composeSkillTree({
      baseDir: REFUNDS,
      destDir: skillDir,
      overlay: null,
    });
    const original = await readFile(join(skillDir, "SKILL.md"), "utf8");
    const result = await runSkillLearn({
      orgId: "0cbb7a3e-002a-4aea-8309-fd6900752d68",
      orgRoot,
      skillName: "magento-investigate-refunds",
      orgEnabled: true,
      skillEnabled: true,
      settlementMs: 0,
      minRepeats: 5,
      currentBaseHash: await fingerprintSkillTree(skillDir),
      usages: Array.from({ length: 5 }, (_, i) => ({
        runId: `run-${i}`,
        skillName: "magento-investigate-refunds",
        contentHash: "same",
        createdAt: new Date(0),
      })),
      propose: async () => ({
        lesson: "parent items",
        rationale: "credit memo child rows double-count",
        learnedMarkdown:
          "Prefer parent items when product structures would double-count.",
        evidenceRunIds: ["run-0", "run-1", "run-2", "run-3", "run-4"],
      }),
    });
    expect(result.decision).toBe("applied");
    expect(result.trace.lesson).toBe("parent items");
    expect(await readFile(join(skillDir, "SKILL.md"), "utf8")).toBe(original);
    expect(
      await readFile(
        join(orgRoot, "skill-overlays", "magento-investigate-refunds", "LEARNED.md"),
        "utf8",
      ),
    ).toContain("Prefer parent items");
  });
});
