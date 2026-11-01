import { mkdtemp, readFile, rm, writeFile, mkdir } from "node:fs/promises";
import { existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  commitConfigChange,
  ensureConfigRepo,
  listConfigHistory,
  readConfigHead,
  restoreConfigPath,
} from "../src/config-vcs";
import { writeWorkSkill } from "../src/work/skills";

let root: string;

beforeEach(async () => {
  root = await mkdtemp(join(tmpdir(), "config-vcs-"));
});

afterEach(async () => {
  await rm(root, { recursive: true, force: true });
});

describe("config-vcs (CV0)", () => {
  it("init is idempotent and tracks only config artifacts", async () => {
    await ensureConfigRepo(root);
    await ensureConfigRepo(root);
    expect(existsSync(join(root, ".git"))).toBe(true);
    // Untracked noise (knowledge/, uploads/) never enters a commit.
    await mkdir(join(root, "knowledge"), { recursive: true });
    await writeFile(join(root, "knowledge", "x.json"), "{}", "utf8");
    await mkdir(join(root, "skills", "s1"), { recursive: true });
    await writeFile(join(root, "skills", "s1", "SKILL.md"), "a", "utf8");
    const sha = await commitConfigChange({
      workspaceRoot: root,
      paths: ["."],
      message: "Added skill: s1",
    });
    expect(sha).toMatch(/^[0-9a-f]{40}$/);
    expect(await readConfigHead(root)).toBe(sha);
    const history = await listConfigHistory(root, "knowledge");
    expect(history).toHaveLength(0);

    await mkdir(join(root, "skill-overlays", "s1"), { recursive: true });
    await writeFile(
      join(root, "skill-overlays", "s1", "LEARNED.md"),
      "---\nbase_hash: abc\nstatus: applied\n---\nDo not loop.\n",
      "utf8",
    );
    const overlaySha = await commitConfigChange({
      workspaceRoot: root,
      paths: ["skill-overlays/s1"],
      message: "Added overlay: s1",
    });
    expect(overlaySha).toMatch(/^[0-9a-f]{40}$/);
    expect(await listConfigHistory(root, "skill-overlays/s1")).toHaveLength(1);
  });

  it("commit -> history -> restore round-trips an artifact", async () => {
    await mkdir(join(root, "skills", "s1"), { recursive: true });
    await writeFile(join(root, "skills", "s1", "SKILL.md"), "v1", "utf8");
    const sha1 = await commitConfigChange({
      workspaceRoot: root,
      paths: ["skills/s1"],
      message: "Added skill: s1",
    });
    await writeFile(join(root, "skills", "s1", "SKILL.md"), "v2", "utf8");
    await commitConfigChange({
      workspaceRoot: root,
      paths: ["skills/s1"],
      message: "Updated skill: s1",
    });

    const history = await listConfigHistory(root, "skills/s1");
    expect(history.map((h) => h.message)).toEqual([
      "Updated skill: s1",
      "Added skill: s1",
    ]);

    const restored = await restoreConfigPath({
      workspaceRoot: root,
      sha: sha1!,
      path: "skills/s1",
    });
    expect(restored).toBeTruthy();
    expect(await readFile(join(root, "skills", "s1", "SKILL.md"), "utf8")).toBe(
      "v1",
    );
    // Restore is a forward commit, not a reset.
    const after = await listConfigHistory(root, "skills/s1");
    expect(after).toHaveLength(3);
  });

  it("no-op commits return null", async () => {
    await mkdir(join(root, "skills"), { recursive: true });
    const sha = await commitConfigChange({
      workspaceRoot: root,
      paths: ["skills"],
      message: "nothing",
    });
    expect(sha).toBeNull();
  });

  it("concurrent commits serialize without corrupting the index", async () => {
    await Promise.all(
      Array.from({ length: 6 }, async (_, i) => {
        await mkdir(join(root, "skills", `s${i}`), { recursive: true });
        await writeFile(join(root, "skills", `s${i}`, "SKILL.md"), `${i}`, "utf8");
        await commitConfigChange({
          workspaceRoot: root,
          paths: [`skills/s${i}`],
          message: `Added skill: s${i}`,
        });
      }),
    );
    const history = await listConfigHistory(root);
    // init commit + 6 skill commits
    expect(history.length).toBe(7);
  });

  it("writeWorkSkill auto-versions into the workspace repo", async () => {
    const skillsRoot = join(root, "skills");
    await mkdir(skillsRoot, { recursive: true });
    await writeWorkSkill(skillsRoot, {
      name: "demo-skill",
      description: "d",
      body: "b",
    });
    const history = await listConfigHistory(root, "skills/demo-skill");
    expect(history.map((h) => h.message)).toEqual([
      "Updated skill: demo-skill",
    ]);
  });
});
