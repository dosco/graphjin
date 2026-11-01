import { execFileSync } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { commitConfigChange } from "../src/config-vcs/index";
import { cp } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { checkRemoteUpdates } from "../src/config-vcs/updates";
import {
  bringInRemoteSkills,
  localOnlyPath,
  incomingSkillDiff,
  openPullRequest,
  previewPublish,
  publishItemDiff,
  parseRemoteUrl,
  publishContext,
  PullRequestNotOpenedError,
  skillTreesAt,
  validBranchName,
} from "../src/config-vcs/remote";

let root: string;
let orgRoot: string;
let bare: string;
let remoteUrl: URL;

const git = (cwd: string, ...args: string[]) => execFileSync("git", args, { cwd, encoding: "utf8" }).trim();
const remoteFiles = (ref: string) => git(bare, "ls-tree", "-r", "--name-only", ref).split("\n").filter(Boolean).sort();

async function write(path: string, content: string) {
  await mkdir(join(orgRoot, path, ".."), { recursive: true });
  await writeFile(join(orgRoot, path), content);
}

const publish = (mode: "push" | "pull_request" = "push") =>
  publishContext({ orgRoot, access: { url: remoteUrl }, mode, branch: "main", kinds: ["skills", "workflows"], now: new Date("2026-10-04T09:30:00Z") });

beforeEach(async () => {
  root = await mkdtemp(join(tmpdir(), "context-remote-"));
  orgRoot = join(root, "org");
  bare = join(root, "remote.git");
  git(root, "init", "--bare", "--initial-branch=main", bare);
  remoteUrl = pathToFileURL(bare);
  await write("skills/shortfall/SKILL.md", "# Shortfall\n");
  await write("workflows/daily.md", "# Daily\n");
  await write("memory/facts.md", "private\n");
  await commitConfigChange({ workspaceRoot: orgRoot, paths: ["skills", "workflows", "memory"], message: "Seed" });
});

afterEach(async () => {
  await rm(root, { recursive: true, force: true });
});

describe("publishContext", () => {
  it("creates the branch with only the chosen kinds", async () => {
    const result = await publish();
    expect(result).toMatchObject({ status: "pushed", branch: "main", detail: "Created main on the remote." });
    expect(remoteFiles("main")).toEqual(["skills/shortfall/SKILL.md", "workflows/daily.md"]);
  });

  it("reports no change when the remote already matches", async () => {
    await publish();
    expect((await publish()).status).toBe("unchanged");
  });

  it("builds on the remote tip and keeps files OpenNeko does not publish", async () => {
    await publish();
    const clone = join(root, "clone");
    git(root, "clone", "-q", bare, clone);
    await writeFile(join(clone, "README.md"), "Team notes\n");
    git(clone, "add", "README.md");
    git(clone, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "Add readme");
    git(clone, "push", "-q", "origin", "main");
    const remoteTip = git(bare, "rev-parse", "main");

    await write("skills/shortfall/SKILL.md", "# Shortfall v2\n");
    await commitConfigChange({ workspaceRoot: orgRoot, paths: ["skills"], message: "Edit" });
    const result = await publish();

    expect(result.status).toBe("pushed");
    expect(git(bare, "rev-parse", "main^")).toBe(remoteTip);
    expect(remoteFiles("main")).toEqual(["README.md", "skills/shortfall/SKILL.md", "workflows/daily.md"]);
    expect(git(bare, "show", "main:skills/shortfall/SKILL.md")).toBe("# Shortfall v2");
  });

  it("pushes a separate branch in pull request mode", async () => {
    await publish();
    await write("workflows/daily.md", "# Daily v2\n");
    await commitConfigChange({ workspaceRoot: orgRoot, paths: ["workflows"], message: "Edit" });
    const result = await publish("pull_request");

    expect(result).toMatchObject({ status: "pull_request", branch: "openneko/context-20261004-093000", link: null });
    expect(git(bare, "show", "openneko/context-20261004-093000:workflows/daily.md")).toBe("# Daily v2");
    expect(git(bare, "show", "main:workflows/daily.md")).toBe("# Daily");
  });

  it("keeps the token out of error messages", async () => {
    const token = "ghp_secretvalue123";
    const error = await publishContext({
      orgRoot, access: { url: pathToFileURL(join(root, "missing.git")), token }, mode: "push", branch: "main", kinds: ["skills"],
    }).catch((caught: Error) => caught);
    expect(error).toBeInstanceOf(Error);
    expect(String(error)).not.toContain(token);
    expect(String(error)).not.toContain(Buffer.from(`x-access-token:${token}`).toString("base64"));
  });
});

describe("remote settings validation", () => {
  it("accepts HTTPS and SSH repository addresses and rejects secrets or other schemes", () => {
    expect(parseRemoteUrl("https://github.com/acme/context.git").hostname).toBe("github.com");
    expect(parseRemoteUrl("git@github.com:acme/context.git").toString()).toBe("ssh://git@github.com/acme/context.git");
    expect(parseRemoteUrl("ssh://git@git.acme.local:2222/team/context.git").port).toBe("2222");
    expect(() => parseRemoteUrl("https://user:pw@github.com/acme/context.git")).toThrow("token field");
    expect(() => parseRemoteUrl("ssh://git:pw@github.com/acme/context.git")).toThrow("token field");
    expect(() => parseRemoteUrl("http://github.com/acme/context.git")).toThrow("https://");
    expect(validBranchName("main")).toBe(true);
    expect(validBranchName("../main")).toBe(false);
  });

  it("opens a GitHub pull request with the token", async () => {
    const fetchImpl = vi.fn(async () => new Response(JSON.stringify({ html_url: "https://github.com/acme/context/pull/7" }), { status: 201 }));
    const link = await openPullRequest({
      url: new URL("https://github.com/acme/context.git"), token: "t", head: "openneko/x", base: "main", title: "T", body: "B",
      fetchImpl: fetchImpl as unknown as typeof fetch,
    });
    expect(link).toBe("https://github.com/acme/context/pull/7");
    expect(fetchImpl).toHaveBeenCalledWith("https://api.github.com/repos/acme/context/pulls", expect.objectContaining({ method: "POST" }));
  });

  it("links to the host's new-request page when the token cannot open the request", async () => {
    const refused = async () => new Response(JSON.stringify({ message: "Resource not accessible by personal access token" }), { status: 403 });
    const github = openPullRequest({
      url: new URL("https://github.com/acme/context.git"), token: "t", head: "openneko/x", base: "main", title: "T", body: "B",
      fetchImpl: refused as unknown as typeof fetch,
    });
    await expect(github).rejects.toBeInstanceOf(PullRequestNotOpenedError);
    await expect(github).rejects.toMatchObject({
      message: expect.stringContaining("Pull requests: Read and write"),
      link: "https://github.com/acme/context/compare/main...openneko%2Fx?expand=1",
    });
    await expect(openPullRequest({
      url: new URL("ssh://git@gitlab.com/acme/context.git"), token: "t", head: "openneko/x", base: "main", title: "T", body: "B",
      fetchImpl: refused as unknown as typeof fetch,
    })).rejects.toMatchObject({
      link: "https://gitlab.com/acme/context/-/merge_requests/new?merge_request%5Bsource_branch%5D=openneko%2Fx&merge_request%5Btarget_branch%5D=main",
    });
  });
});

describe("updates from the remote", () => {
  const PACK_FIXTURE = fileURLToPath(new URL("../../../apps/worker/test/fixtures/service-health", import.meta.url));

  async function remoteCommit(change: (clone: string) => Promise<void>) {
    const clone = join(root, `clone-${Math.random().toString(36).slice(2)}`);
    git(root, "clone", "-q", bare, clone);
    await change(clone);
    git(clone, "add", "-A");
    git(clone, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "Remote change");
    git(clone, "push", "-q", "origin", "main");
  }

  const check = (skillBases: Record<string, string>, uploadedPacks = new Map<string, Map<string, string>>()) =>
    checkRemoteUpdates({ orgRoot, access: { url: remoteUrl }, branch: "main", skillBases, uploadedPacks });

  it("offers a skill that changed only in the repository, and brings it in", async () => {
    const published = await publish();
    const bases = await skillTreesAt(orgRoot, published.sha!);
    await remoteCommit((clone) => writeFile(join(clone, "skills/shortfall/SKILL.md"), "# Shortfall from upstream\n"));

    const updates = await check(bases);
    expect(updates.skills).toEqual([expect.objectContaining({ name: "shortfall", status: "update" })]);

    const sha = await bringInRemoteSkills({ orgRoot, tip: updates.tip!, names: ["shortfall"], message: "Brought in shortfall" });
    expect(sha).toMatch(/^[0-9a-f]{40}$/);
    expect(git(orgRoot, "show", "HEAD:skills/shortfall/SKILL.md")).toBe("# Shortfall from upstream");
    expect(git(orgRoot, "log", "-1", "--pretty=%s")).toBe("Brought in shortfall");
  });

  it("flags a skill changed in both places, and ignores one changed only in OpenNeko", async () => {
    const published = await publish();
    const bases = await skillTreesAt(orgRoot, published.sha!);
    await write("skills/shortfall/SKILL.md", "# Local edit\n");
    await commitConfigChange({ workspaceRoot: orgRoot, paths: ["skills"], message: "Local" });
    expect((await check(bases)).skills).toEqual([]);

    await remoteCommit((clone) => writeFile(join(clone, "skills/shortfall/SKILL.md"), "# Remote edit\n"));
    expect((await check(bases)).skills).toEqual([expect.objectContaining({ name: "shortfall", status: "both_changed" })]);
  });

  it("lists a new pack, skips an uploaded version, and rejects changed content under the same version", async () => {
    await publish();
    // Git keeps no empty folders, so a pack in a repository leaves out empty artifact folders.
    const pack = join(root, "service-health");
    await cp(PACK_FIXTURE, pack, { recursive: true });
    const manifest = await readFile(join(pack, "pack.yaml"), "utf8");
    await writeFile(join(pack, "pack.yaml"), manifest.replace("  actions: actions\n", "").replace("  policies: policies\n", ""));
    await remoteCommit((clone) => cp(pack, join(clone, "packs/service-health"), { recursive: true }));

    const fresh = await check({});
    expect(fresh.packs).toEqual([expect.objectContaining({ id: "service-health", version: "0.1.0", status: "new_pack" })]);

    const { hashPackFiles } = await import("@neko/packs");
    const contentHash = await hashPackFiles(pack);
    expect((await check({}, new Map([["service-health", new Map([["0.1.0", contentHash]])]]))).packs).toEqual([]);
    expect((await check({}, new Map([["service-health", new Map([["0.1.0", "other"]])]]))).packs).toEqual([
      expect.objectContaining({ status: "invalid", detail: expect.stringContaining("Raise the version") }),
    ]);
  });
});

describe("publish and compare rules for skills", () => {
  async function remoteCommit(change: (clone: string) => Promise<void>) {
    const clone = join(root, `clone-${Math.random().toString(36).slice(2)}`);
    git(root, "clone", "-q", bare, clone);
    await change(clone);
    git(clone, "add", "-A");
    git(clone, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "Remote change");
    git(clone, "push", "-q", "origin", "main");
  }
  const publishSkills = (opts: { excludeSkills?: string[]; skillBases?: Record<string, string> } = {}) =>
    publishContext({ orgRoot, access: { url: remoteUrl }, mode: "push", branch: "main", kinds: ["skills"], ...opts });

  it("treats paths with a dot segment as local-only", () => {
    expect(localOnlyPath("skills/a/.client-agent-assets.json")).toBe(true);
    expect(localOnlyPath("skills/a/references/x.md")).toBe(false);
  });

  it("publishes only the org's own skill files", async () => {
    await write("skills/shortfall/.client-agent-assets.json", "{}");
    await write("skills/.port-manifest.json", "{}");
    await write("skills/xlsx/SKILL.md", "# Built-in\n");
    await commitConfigChange({ workspaceRoot: orgRoot, paths: ["skills"], message: "Markers" });

    await publishSkills({ excludeSkills: ["xlsx"] });
    expect(remoteFiles("main")).toEqual(["skills/shortfall/SKILL.md"]);
  });

  it("keeps a file only the repository has, until OpenNeko removes one it had synced", async () => {
    await publishSkills();
    await remoteCommit(async (clone) => {
      await mkdir(join(clone, "skills/shortfall/runtimes/reckon"), { recursive: true });
      await writeFile(join(clone, "skills/shortfall/runtimes/reckon/SKILL.md"), "# Reckon\n");
    });
    await write("skills/shortfall/SKILL.md", "# Edited\n");
    await commitConfigChange({ workspaceRoot: orgRoot, paths: ["skills"], message: "Edit" });

    await publishSkills();
    expect(remoteFiles("main")).toContain("skills/shortfall/runtimes/reckon/SKILL.md");

    await write("skills/shortfall/notes.md", "temporary\n");
    await commitConfigChange({ workspaceRoot: orgRoot, paths: ["skills"], message: "Add notes" });
    const withNotes = await publishSkills();
    const remoteTrees = await skillTreesAt(orgRoot, withNotes.sha!);
    const localTrees = await skillTreesAt(orgRoot, withNotes.head!);
    await rm(join(orgRoot, "skills/shortfall/notes.md"));
    await commitConfigChange({ workspaceRoot: orgRoot, paths: ["skills"], message: "Remove notes" });
    await publishSkills({ skillBases: { shortfall: { remote: remoteTrees.shortfall!, local: localTrees.shortfall! } } });
    expect(remoteFiles("main")).not.toContain("skills/shortfall/notes.md");
    expect(remoteFiles("main")).toContain("skills/shortfall/runtimes/reckon/SKILL.md");
  });

  it("finds no difference from deploy markers or repository-only files, and records the skill as in sync", async () => {
    await publishSkills();
    await remoteCommit(async (clone) => {
      await mkdir(join(clone, "skills/shortfall/runtimes"), { recursive: true });
      await writeFile(join(clone, "skills/shortfall/runtimes/x.md"), "x\n");
    });
    await write("skills/shortfall/.client-agent-assets.json", "{}");
    await commitConfigChange({ workspaceRoot: orgRoot, paths: ["skills"], message: "Marker" });

    const updates = await checkRemoteUpdates({ orgRoot, access: { url: remoteUrl }, branch: "main", skillBases: {}, uploadedPacks: new Map() });
    expect(updates.skills).toEqual([]);
    expect(Object.keys(updates.inSync)).toEqual(["shortfall"]);
    expect(updates.inSync.shortfall).toEqual({ remote: expect.any(String), local: expect.any(String) });
  });

  it("offers a repository change as an update after a sync, despite repository-only files", async () => {
    await publishSkills();
    await remoteCommit(async (clone) => {
      await mkdir(join(clone, "skills/shortfall/runtimes/reckon"), { recursive: true });
      await writeFile(join(clone, "skills/shortfall/runtimes/reckon/SKILL.md"), "# Reckon\n");
    });
    const first = await checkRemoteUpdates({ orgRoot, access: { url: remoteUrl }, branch: "main", skillBases: {}, uploadedPacks: new Map() });
    expect(first.skills).toEqual([]);
    const bases = first.inSync;

    await remoteCommit((clone) => writeFile(join(clone, "skills/shortfall/SKILL.md"), "# Upstream edit\n"));
    const second = await checkRemoteUpdates({ orgRoot, access: { url: remoteUrl }, branch: "main", skillBases: bases, uploadedPacks: new Map() });
    expect(second.skills).toEqual([expect.objectContaining({ name: "shortfall", status: "update" })]);

    await write("skills/shortfall/SKILL.md", "# Local edit\n");
    await commitConfigChange({ workspaceRoot: orgRoot, paths: ["skills"], message: "Local" });
    const third = await checkRemoteUpdates({ orgRoot, access: { url: remoteUrl }, branch: "main", skillBases: bases, uploadedPacks: new Map() });
    expect(third.skills).toEqual([expect.objectContaining({ name: "shortfall", status: "both_changed" })]);
  });

  it("keeps local-only files when bringing in the repository's skill", async () => {
    await publishSkills();
    await write("skills/shortfall/.client-agent-assets.json", "{}");
    await commitConfigChange({ workspaceRoot: orgRoot, paths: ["skills"], message: "Marker" });
    await remoteCommit((clone) => writeFile(join(clone, "skills/shortfall/SKILL.md"), "# Upstream\n"));
    const updates = await checkRemoteUpdates({ orgRoot, access: { url: remoteUrl }, branch: "main", skillBases: {}, uploadedPacks: new Map() });

    await bringInRemoteSkills({ orgRoot, tip: updates.tip!, names: ["shortfall"], message: "Bring in" });
    expect(git(orgRoot, "show", "HEAD:skills/shortfall/SKILL.md")).toBe("# Upstream");
    expect(git(orgRoot, "show", "HEAD:skills/shortfall/.client-agent-assets.json")).toBe("{}");
  });
});

describe("previewPublish", () => {
  const preview = (excludeSkills: string[] = []) =>
    previewPublish({ orgRoot, access: { url: remoteUrl }, branch: "main", kinds: ["skills", "workflows"], excludeSkills });

  it("lists every item before the first publish, and nothing after it", async () => {
    expect(await preview()).toEqual([
      { kind: "skills", name: "shortfall", added: 1, changed: 0, removed: 0 },
      { kind: "workflows", name: "daily", added: 1, changed: 0, removed: 0 },
    ]);
    await publish();
    expect(await preview()).toEqual([]);
  });

  it("lists a changed skill with its file counts, and leaves out excluded skills and local-only files", async () => {
    await publish();
    await write("skills/shortfall/SKILL.md", "# Edited\n");
    await write("skills/shortfall/references/notes.md", "notes\n");
    await write("skills/shortfall/.marker.json", "{}");
    await write("skills/xlsx/SKILL.md", "# Built-in\n");
    await commitConfigChange({ workspaceRoot: orgRoot, paths: ["skills"], message: "Edit" });

    expect(await preview(["xlsx"])).toEqual([{ kind: "skills", name: "shortfall", added: 1, changed: 1, removed: 0 }]);
    expect(git(bare, "show", "main:skills/shortfall/SKILL.md")).toBe("# Shortfall");
  });
});

describe("change diffs", () => {
  it("shows the lines a publish would send for one skill, without local-only files", async () => {
    await publish();
    await write("skills/shortfall/SKILL.md", "# Shortfall v2\n");
    await write("skills/shortfall/.marker.json", "{}");
    await commitConfigChange({ workspaceRoot: orgRoot, paths: ["skills"], message: "Edit" });

    const { diff, truncated } = await publishItemDiff({ orgRoot, access: { url: remoteUrl }, branch: "main", kinds: ["skills"], kind: "skills", name: "shortfall" });
    expect(truncated).toBe(false);
    expect(diff).toContain("-# Shortfall");
    expect(diff).toContain("+# Shortfall v2");
    expect(diff).not.toContain(".marker.json");
    await expect(publishItemDiff({ orgRoot, access: { url: remoteUrl }, branch: "main", kinds: ["skills"], kind: "skills", name: "../x" }))
      .rejects.toThrow("Unknown item.");
  });

  it("shows the repository's version of a skill against OpenNeko's", async () => {
    await publish();
    await write("skills/shortfall/.marker.json", "{}");
    await commitConfigChange({ workspaceRoot: orgRoot, paths: ["skills"], message: "Marker" });
    const clone = join(root, "clone-diff");
    git(root, "clone", "-q", bare, clone);
    await writeFile(join(clone, "skills/shortfall/SKILL.md"), "# From the repository\n");
    git(clone, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qam", "Remote");
    git(clone, "push", "-q", "origin", "main");
    await checkRemoteUpdates({ orgRoot, access: { url: remoteUrl }, branch: "main", skillBases: {}, uploadedPacks: new Map() });

    const { diff } = await incomingSkillDiff({ orgRoot, branch: "main", name: "shortfall" });
    expect(diff).toContain("+# From the repository");
    expect(diff).not.toContain(".marker.json");
  });
});

