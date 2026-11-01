import { mkdtemp, readFile, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { zipFixture } from "../../../../packages/packs/test/zip-fixture";

vi.mock("@neko/llm/config-vcs", () => ({ recordConfigChange: vi.fn(async () => {}) }));
const access = vi.hoisted(() => ({ admin: true }));
vi.mock("@/lib/admin-auth", () => ({
  requireAdminActor: async () => access.admin
    ? { role: "admin", userId: "test-admin" }
    : Response.json({ error: "admin only" }, { status: 403 }),
  isDenied: (value: unknown) => value instanceof Response,
}));
vi.mock("@/lib/db", () => ({ getOrgId: async () => "skill-archive-test" }));

const markdown = "---\nname: multi-file-demo\ndescription: Runs a demo using bundled files.\n---\nRead references/guide.md and run scripts/check.py.\n";

describe("skill archive import", () => {
  let home: string;
  const orgId = "skill-archive-test";

  beforeAll(async () => {
    home = await mkdtemp(join(tmpdir(), "skill-archive-home-"));
    process.env.HOME = home;
  });

  afterAll(async () => {
    await rm(home, { recursive: true, force: true });
  });

  it("imports and discovers all files in a packaged Agent Skill", async () => {
    const { importWorkSkillArchive } = await import("@/lib/skill-archive");
    const { ensureOrgWorkspace, listInstalledSkills } = await import("@neko/llm/work");
    const archive = zipFixture([
      { name: "multi-file-demo/" },
      { name: "multi-file-demo/SKILL.md", data: markdown },
      { name: "multi-file-demo/scripts/check.py", data: "print('ok')\n", mode: 0o100755 },
      { name: "multi-file-demo/references/guide.md", data: "# Guide\n" },
      { name: "multi-file-demo/assets/icon.png", data: Buffer.from([137, 80, 78, 71]) },
    ]);
    expect(await importWorkSkillArchive(orgId, archive)).toBe("multi-file-demo");
    const { skillsRoot } = await ensureOrgWorkspace(orgId);
    const skillRoot = join(skillsRoot, "multi-file-demo");
    expect(await readFile(join(skillRoot, "SKILL.md"), "utf8")).toBe(markdown);
    expect(await readFile(join(skillRoot, "references/guide.md"), "utf8")).toBe("# Guide\n");
    expect(await readFile(join(skillRoot, "assets/icon.png"))).toEqual(Buffer.from([137, 80, 78, 71]));
    expect((await stat(join(skillRoot, "scripts/check.py"))).mode & 0o111).not.toBe(0);
    expect((await listInstalledSkills(skillsRoot)).map(skill => skill.name)).toContain("multi-file-demo");

    await expect(importWorkSkillArchive(orgId, archive)).rejects.toMatchObject({ status: 409 });
    expect(await readFile(join(skillRoot, "SKILL.md"), "utf8")).toBe(markdown);
  });

  it("rejects unsafe paths and invalid definitions without installing them", async () => {
    const { importWorkSkillArchive } = await import("@/lib/skill-archive");
    const cases = [
      [{ name: "../escape/SKILL.md", data: markdown }],
      [{ name: "another-skill/SKILL.md", data: markdown }],
      [{ name: "multi-file-demo/SKILL.md", data: markdown }, { name: "other-skill/file.md", data: "x" }],
      [{ name: "bad-skill/SKILL.md", data: "# Missing frontmatter" }],
      [{ name: "bad-skill/SKILL.md", data: "---\nname: different\ndescription: Wrong name\n---\n" }],
      [{ name: "bad-skill/SKILL.md", data: markdown.replace("multi-file-demo", "bad-skill") },
        { name: "bad-skill/link", data: "../../escape", mode: 0o120777 }],
      [{ name: "bad-skill/SKILL.md", data: markdown.replace("multi-file-demo", "bad-skill") },
        { name: "bad-skill/Guide.md", data: "a" }, { name: "bad-skill/guide.md", data: "b" }],
    ];
    for (const entries of cases) {
      await expect(importWorkSkillArchive(orgId, zipFixture(entries))).rejects.toThrow();
    }
    await expect(importWorkSkillArchive(orgId, Buffer.from("not a zip"))).rejects.toThrow();
  });

  it("requires an admin and accepts a multi-file upload through the route", async () => {
    const { POST } = await import("@/app/api/work/skills/route");
    const archive = zipFixture([
      { name: "api-demo/SKILL.md", data: markdown.replace("multi-file-demo", "api-demo") },
      { name: "api-demo/scripts/run.py", data: "print('ready')\n" },
    ]);
    const request = () => {
      const form = new FormData();
      const fileBytes = new Uint8Array(archive.length);
      fileBytes.set(archive);
      form.set("file", new File([fileBytes], "api-demo.skill"));
      return new Request("http://localhost/api/work/skills", { method: "POST", body: form });
    };
    access.admin = false;
    expect((await POST(request())).status).toBe(403);
    access.admin = true;
    const response = await POST(request());
    expect(response.status).toBe(201);
    expect(await response.json()).toEqual({ name: "api-demo" });
    expect((await POST(request())).status).toBe(409);
  });
});
