import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, describe, expect, it } from "vitest";
import {
  composeSkillTree,
  readLearnedOverlay,
  writeLearnedOverlay,
} from "../src/work/skill-overlay";
import {
  copySkillOverrides,
  fingerprintSkillTree,
} from "../src/work/workspace";

const MAGENTO_FULFILLMENT = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "../../../packs/magento/skills/magento-triage-fulfillment",
);

const cleanupPaths: string[] = [];

afterEach(async () => {
  while (cleanupPaths.length > 0) {
    const path = cleanupPaths.pop();
    if (path) await rm(path, { recursive: true, force: true });
  }
});

describe("composeSkillTree", () => {
  it("keeps Magento reference files and applies matching learned guidance", async () => {
    const dest = await mkdtemp(join(tmpdir(), "neko-compose-"));
    cleanupPaths.push(dest);
    const baseHash = await fingerprintSkillTree(MAGENTO_FULFILLMENT);
    const result = await composeSkillTree({
      baseDir: MAGENTO_FULFILLMENT,
      destDir: dest,
      overlay: {
        skillName: "magento-triage-fulfillment",
        baseHash,
        status: "applied",
        body: "Never return an item to its origin location.",
      },
    });
    expect(result).toBe("applied");
    expect(
      await readFile(join(dest, "references", "fulfillment-rules.md"), "utf8"),
    ).toMatch(/fulfill/i);
    const skill = await readFile(join(dest, "SKILL.md"), "utf8");
    expect(skill).toContain("Never return an item to its origin location.");
    expect(skill).toContain("openneko-learned:start");
    expect(skill).toContain("Read [fulfillment rules]");
  });

  it("does not hide new base files when the overlay hash is stale", async () => {
    const dest = await mkdtemp(join(tmpdir(), "neko-compose-stale-"));
    cleanupPaths.push(dest);
    const result = await composeSkillTree({
      baseDir: MAGENTO_FULFILLMENT,
      destDir: dest,
      overlay: {
        skillName: "magento-triage-fulfillment",
        baseHash: "stale-hash",
        status: "applied",
        body: "This must not appear.",
      },
    });
    expect(result).toBe("base-only");
    expect(
      await readFile(join(dest, "references", "fulfillment-rules.md"), "utf8"),
    ).toMatch(/fulfill/i);
    const skill = await readFile(join(dest, "SKILL.md"), "utf8");
    expect(skill).not.toContain("This must not appear.");
    expect(skill).not.toContain("openneko-learned:start");
  });
});

describe("copySkillOverrides overlay compose", () => {
  it("stages a matching overlay on a pack skill without dropping references", async () => {
    const org = await mkdtemp(join(tmpdir(), "neko-overlay-org-"));
    const stage = await mkdtemp(join(tmpdir(), "neko-overlay-stage-"));
    cleanupPaths.push(org, stage);
    const skillsRoot = join(org, "skills");
    const skillDir = join(skillsRoot, "magento-triage-fulfillment");
    await mkdir(skillDir, { recursive: true });
    await composeSkillTree({
      baseDir: MAGENTO_FULFILLMENT,
      destDir: skillDir,
      overlay: null,
    });
    const baseHash = await fingerprintSkillTree(skillDir);
    await writeLearnedOverlay(org, {
      skillName: "magento-triage-fulfillment",
      baseHash,
      status: "applied",
      body: "Each operation type once per item.",
    });
    expect(
      (await readLearnedOverlay(org, "magento-triage-fulfillment"))?.body,
    ).toContain("once per item");

    const copied = await copySkillOverrides(skillsRoot, join(stage, "skills"));
    expect(copied).toContain("magento-triage-fulfillment");
    const staged = join(stage, "skills", "magento-triage-fulfillment");
    expect(
      await readFile(join(staged, "references", "fulfillment-rules.md"), "utf8"),
    ).toMatch(/fulfill/i);
    expect(await readFile(join(staged, "SKILL.md"), "utf8")).toContain(
      "Each operation type once per item.",
    );
  });
});
