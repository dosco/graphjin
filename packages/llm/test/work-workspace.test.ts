import {
  access,
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import {
  RETIRED_BUILTIN_SKILLS,
  SKILL_ORIGIN_FILE,
  copySkillOverrides,
  ensureIsolatedJobWorkspace,
  ensureOrgWorkspace,
  ensureWorkWorkspace,
  fingerprintSkillTree,
  materializeBuiltinSkills,
  readSkillOrigin,
  resolveBuiltinSkillsRoot,
  unmodifiedBuiltinSkills,
} from "../src/work/workspace";

const cleanupPaths: string[] = [];
const ORIGINAL_NODE_ENV = process.env.NODE_ENV;

afterEach(async () => {
  while (cleanupPaths.length > 0) {
    const path = cleanupPaths.pop();
    if (path) await rm(path, { recursive: true, force: true });
  }
  delete process.env.HOME;
  delete process.env.OPENNEKO_AGENT_HOME;
  delete process.env.OPENNEKO_HOST_WEB_DEV;
  delete process.env.OPENNEKO_STACK_MODE;
  delete process.env.NEXT_PUBLIC_DEMO;
  delete process.env.DEMO;
  if (ORIGINAL_NODE_ENV === undefined) delete process.env.NODE_ENV;
  else process.env.NODE_ENV = ORIGINAL_NODE_ENV;
});

describe("work workspace", () => {
  it("self-locates built-in skills beside the standalone agent bundle", () => {
    expect(
      resolveBuiltinSkillsRoot(
        "file:///app/agent-entry.js",
        undefined,
        (candidate) => candidate === "/app/assets/builtin-skills",
      ),
    ).toBe("/app/assets/builtin-skills");
  });

  it("supports an isolated agent home in explicit host-web development mode", async () => {
    const agentHome = await mkdtemp(join(tmpdir(), "neko-agent-home-"));
    cleanupPaths.push(agentHome);
    process.env.OPENNEKO_AGENT_HOME = agentHome;
    process.env.OPENNEKO_HOST_WEB_DEV = "1";
    process.env.NODE_ENV = "development";

    const workspace = await ensureOrgWorkspace("org-test");
    expect(workspace.orgRoot).toBe(
      join(agentHome, ".config", "openneko", "agents", "orgs", "org-test"),
    );
  }, 30_000);

  it.each([
    ["production", { NODE_ENV: "production" }],
    ["demo stack", { NODE_ENV: "development", OPENNEKO_STACK_MODE: "demo" }],
    ["demo UI", { NODE_ENV: "development", NEXT_PUBLIC_DEMO: "true" }],
  ])("ignores the host-web agent home in %s mode", async (_label, mode) => {
    const home = await mkdtemp(join(tmpdir(), "neko-work-home-"));
    const agentHome = await mkdtemp(join(tmpdir(), "neko-agent-home-"));
    cleanupPaths.push(home, agentHome);
    process.env.HOME = home;
    process.env.OPENNEKO_AGENT_HOME = agentHome;
    process.env.OPENNEKO_HOST_WEB_DEV = "1";
    Object.assign(process.env, mode);

    const workspace = await ensureOrgWorkspace("org-test");
    expect(workspace.orgRoot).toBe(
      join(home, ".config", "openneko", "agents", "orgs", "org-test"),
    );
  }, 30_000);

  it("seeds built-in skills into the org workspace", async () => {
    const home = await mkdtemp(join(tmpdir(), "neko-work-home-"));
    cleanupPaths.push(home);
    process.env.HOME = home;

    const workspace = await ensureOrgWorkspace("org-test");
    const skills = await readdir(workspace.skillsRoot);
    expect(skills).not.toContain("records");
    expect(skills).toContain("graphjin-config");
    expect(skills).toContain("internal-comms");
  }, 30_000);

  it("creates per-thread and per-run directories", async () => {
    const home = await mkdtemp(join(tmpdir(), "neko-work-home-"));
    cleanupPaths.push(home);
    process.env.HOME = home;

    const workspace = await ensureWorkWorkspace("org-test", "thread-1", "run-1");
    expect(workspace.threadUploadsRoot).toContain("thread-1");
    expect(workspace.artifactRoot).toContain("run-1");
  }, 30_000);

  it("stages only custom and modified skill bodies, then fills built-ins from the image", async () => {
    const home = await mkdtemp(join(tmpdir(), "neko-work-home-"));
    const stage = await mkdtemp(join(tmpdir(), "neko-skills-stage-"));
    cleanupPaths.push(home, stage);
    process.env.HOME = home;

    const workspace = await ensureOrgWorkspace("org-test");
    const modified = join(workspace.skillsRoot, "graphjin-config", "SKILL.md");
    await writeFile(modified, `${await readFile(modified, "utf8")}\nOrg override.\n`);
    const custom = join(workspace.skillsRoot, "quarterly-review");
    await mkdir(custom, { recursive: true });
    await writeFile(
      join(custom, "SKILL.md"),
      "---\nname: quarterly-review\ndescription: Prepare quarterly reviews\n---\n",
    );

    const stagedSkills = join(stage, "skills");
    const copied = await copySkillOverrides(workspace.skillsRoot, stagedSkills);
    expect(copied).toContain("quarterly-review");
    expect(copied).toContain("graphjin-config");
    expect(copied).not.toContain("internal-comms");

    await materializeBuiltinSkills(stagedSkills);
    expect(await readdir(stagedSkills)).toContain("internal-comms");
    expect(await readFile(join(stagedSkills, "graphjin-config", "SKILL.md"), "utf8"))
      .toContain("Org override.");
  }, 30_000);

  it("lists seeded built-in skills until someone edits one", async () => {
    const skills = await mkdtemp(join(tmpdir(), "neko-builtin-skills-"));
    cleanupPaths.push(skills);
    await materializeBuiltinSkills(skills);
    const seeded = await unmodifiedBuiltinSkills(skills);
    expect(seeded).toContain("internal-comms");
    expect(seeded).toContain("graphjin-config");

    await writeFile(join(skills, "internal-comms", "SKILL.md"), "Edited by an admin.");
    await mkdir(join(skills, "quarterly-review"), { recursive: true });
    await writeFile(join(skills, "quarterly-review", "SKILL.md"), "Org skill.");
    const after = await unmodifiedBuiltinSkills(skills);
    expect(after).not.toContain("internal-comms");
    expect(after).not.toContain("quarterly-review");
    expect(after).toContain("graphjin-config");
  }, 30_000);

  it("stages and seeds only the skills the run's actor holds", async () => {
    const home = await mkdtemp(join(tmpdir(), "neko-work-home-"));
    const stage = await mkdtemp(join(tmpdir(), "neko-skills-stage-"));
    cleanupPaths.push(home, stage);
    process.env.HOME = home;

    const workspace = await ensureOrgWorkspace("org-test");
    for (const name of ["quarterly-review", "sales-playbook"]) {
      await mkdir(join(workspace.skillsRoot, name), { recursive: true });
      await writeFile(join(workspace.skillsRoot, name, "SKILL.md"), `---\nname: ${name}\ndescription: x\n---\n`);
    }

    const stagedSkills = join(stage, "skills");
    const allowed = ["quarterly-review", "internal-comms"];
    expect(await copySkillOverrides(workspace.skillsRoot, stagedSkills, [], allowed)).toEqual(["quarterly-review"]);
    await materializeBuiltinSkills(stagedSkills, allowed);
    expect((await readdir(stagedSkills)).sort()).toEqual(["internal-comms", "quarterly-review"]);

    const forced = join(stage, "forced");
    expect(await copySkillOverrides(workspace.skillsRoot, forced, ["internal-comms"], [])).toEqual(["internal-comms"]);
  }, 30_000);

  it("records a builtin seed origin and skips a stale unmodified seed", async () => {
    const home = await mkdtemp(join(tmpdir(), "neko-work-home-"));
    const stage = await mkdtemp(join(tmpdir(), "neko-skills-stage-"));
    cleanupPaths.push(home, stage);
    process.env.HOME = home;

    const workspace = await ensureOrgWorkspace("org-test");
    const commsDir = join(workspace.skillsRoot, "internal-comms");
    const origin = await readSkillOrigin(commsDir);
    expect(origin?.kind).toBe("builtin");
    expect(origin?.sourceHash).toBe(await fingerprintSkillTree(commsDir));

    await writeFile(
      join(commsDir, "SKILL.md"),
      `${await readFile(join(commsDir, "SKILL.md"), "utf8")}\nStale seed body.\n`,
    );
    await writeFile(
      join(commsDir, SKILL_ORIGIN_FILE),
      `${JSON.stringify({
        kind: "builtin",
        sourceHash: await fingerprintSkillTree(commsDir),
      })}\n`,
    );

    const stagedSkills = join(stage, "skills");
    const copied = await copySkillOverrides(workspace.skillsRoot, stagedSkills);
    expect(copied).not.toContain("internal-comms");

    await materializeBuiltinSkills(stagedSkills);
    expect(await readFile(join(stagedSkills, "internal-comms", "SKILL.md"), "utf8")).not.toContain(
      "Stale seed body.",
    );
  }, 30_000);

  it("removes untouched copies of retired built-in skills and keeps edited ones", async () => {
    const skills = await mkdtemp(join(tmpdir(), "neko-retired-skills-"));
    cleanupPaths.push(skills);
    const seed = async (name: string, body: string) => {
      const dir = join(skills, name);
      await mkdir(dir, { recursive: true });
      await writeFile(join(dir, "SKILL.md"), body);
      await writeFile(join(dir, SKILL_ORIGIN_FILE), `${JSON.stringify({ kind: "builtin", sourceHash: await fingerprintSkillTree(dir) })}\n`);
      return dir;
    };
    expect(RETIRED_BUILTIN_SKILLS).toContain("xlsx");
    await seed("xlsx", "Seeded copy.");
    const edited = await seed("pdf", "Seeded copy.");
    await writeFile(join(edited, "SKILL.md"), "Edited by an admin.");
    await mkdir(join(skills, "docx"), { recursive: true });
    await writeFile(join(skills, "docx", "SKILL.md"), "An org skill that took a retired name.");

    await materializeBuiltinSkills(skills);
    const names = await readdir(skills);
    expect(names).not.toContain("xlsx");
    expect(names).toContain("pdf");
    expect(names).toContain("docx");
    expect(names).toContain("internal-comms");
  }, 30_000);

  it("creates an empty disposable tree for non-interactive agent jobs", async () => {
    const isolated = await ensureIsolatedJobWorkspace("profile/job-1");
    const rootEntries = await readdir(isolated.workspace.orgRoot);

    expect(isolated.workspace.orgRoot).toContain("openneko-agent-profile_job-1-");
    expect(rootEntries.sort()).toEqual([
      "knowledge",
      "memory",
      "runs",
      "skills",
      "uploads",
    ]);
    expect(await readdir(isolated.workspace.memoryRoot)).toEqual([]);
    expect(await readdir(isolated.workspace.skillsRoot)).toEqual([]);

    await isolated.cleanup();
    await expect(access(isolated.workspace.orgRoot)).rejects.toThrow();
  });
});
