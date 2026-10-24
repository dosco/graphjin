// CV4 — git forks/branches. Personal config lives on user/<id> refs
// (never merged, deletable whole per DATA_LIFECYCLE §3); main's tree
// stays team-only; pull/adopt move content across layers as new
// commits/rows, never ref merges.

import { existsSync } from "node:fs";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import {
  app_user,
  config_change,
  config_ref,
  db,
  eq,
  and,
  memory_fork,
  pool,
  work_memory,
} from "@neko/db";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";

vi.mock("../../src/embedding", async (importOriginal) => {
  const { fakeEmbedding } = await import("../helpers/fake-embedding");
  return fakeEmbedding(await importOriginal());
});

import {
  commitToUserRef,
  dropUserConfigData,
  snapshotUserConfig,
  userConfigRef,
} from "../../src/config-vcs/forks";
import { git } from "../../src/config-vcs/git-shell";
import { ensureConfigRepo, listConfigHistory } from "../../src/config-vcs";
import { snapshotDurableMemories } from "../../src/config-vcs/snapshot";
import {
  listMemoryPullUpdates,
  applyMemoryPull,
  listWorkMemories,
  overrideWorkMemoryForUser,
  rememberWorkMemory,
} from "../../src/work/memory";
import {
  adoptWorkflowForTeam,
  getWorkflow,
  saveWorkflow,
} from "../../src/workflows/store";
import { getOrgAgentRoot } from "../../src/work/workspace";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[config-forks] skipping: Postgres unreachable.");
}

async function refTree(
  root: string,
  ref: string,
): Promise<string[]> {
  const { stdout } = await git(root, ["ls-tree", "-r", "--name-only", ref]);
  return stdout.split("\n").filter(Boolean);
}

describe("CV4 user refs (pure git)", () => {
  let root: string;

  beforeAll(async () => {
    root = await mkdtemp(join(tmpdir(), "config-forks-"));
  });

  afterAll(async () => {
    await rm(root, { recursive: true, force: true });
  });

  it("userConfigRef is git-safe and stable, with no raw unsafe ids", () => {
    expect(userConfigRef("u-123")).toBe("refs/heads/user/u-123");
    const odd = userConfigRef("a b@example.com");
    expect(odd).toMatch(/^refs\/heads\/user\/a-b-example-com-[0-9a-f]{8}$/);
    expect(userConfigRef("a b@example.com")).toBe(odd);
  });

  it("commits snapshots to the user ref without touching main", async () => {
    await ensureConfigRepo(root);
    const before = await listConfigHistory(root);

    const sha = await commitToUserRef({
      workspaceRoot: root,
      userId: "u-1",
      files: [{ path: "memory/preference/m1.md", content: "v1" }],
      message: "Personal config checkpoint",
      mode: "replace",
    });
    expect(sha).toMatch(/^[0-9a-f]{40}$/);
    expect(await refTree(root, "refs/heads/user/u-1")).toEqual([
      "memory/preference/m1.md",
    ]);
    // main history and working tree untouched.
    expect(await listConfigHistory(root)).toEqual(before);
    expect(existsSync(join(root, "memory"))).toBe(false);

    // Identical snapshot → no new commit.
    const again = await commitToUserRef({
      workspaceRoot: root,
      userId: "u-1",
      files: [{ path: "memory/preference/m1.md", content: "v1" }],
      message: "Personal config checkpoint",
      mode: "replace",
    });
    expect(again).toBeNull();
  });

  it("overlay mode lays files on top; replace mode rebuilds the tree", async () => {
    await commitToUserRef({
      workspaceRoot: root,
      userId: "u-2",
      files: [{ path: "memory/preference/a.md", content: "a" }],
      message: "checkpoint",
      mode: "replace",
    });
    await commitToUserRef({
      workspaceRoot: root,
      userId: "u-2",
      files: [{ path: "workflows/w.md", content: "w" }],
      message: "Added workflow: w",
      mode: "overlay",
    });
    expect(await refTree(root, "refs/heads/user/u-2")).toEqual([
      "memory/preference/a.md",
      "workflows/w.md",
    ]);
    await commitToUserRef({
      workspaceRoot: root,
      userId: "u-2",
      files: [{ path: "workflows/w.md", content: "w" }],
      message: "checkpoint",
      mode: "replace",
    });
    expect(await refTree(root, "refs/heads/user/u-2")).toEqual([
      "workflows/w.md",
    ]);
  });
});

describeIfDb("CV4 forks (DB-backed)", () => {
  const orgId = uniqueOrgId("cv4");
  let ada: string;
  let root: string;

  beforeAll(async () => {
    await createTestOrg(orgId);
    ada = `${orgId}-ada`;
    await db().insert(app_user).values({
      id: ada,
      email: "ada@example.com",
      org_id: orgId,
    });
    root = getOrgAgentRoot(orgId);
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
    await rm(root, { recursive: true, force: true }).catch(() => {});
    await pool().end();
    vi.unstubAllEnvs();
  });

  it("main memory snapshot excludes user-layer rows; user snapshot carries them", async () => {
    const team = await rememberWorkMemory({
      orgId,
      text: "Team durable fact",
      kind: "business_rule",
      scope: "global",
    });
    const personal = await rememberWorkMemory({
      orgId,
      userId: ada,
      text: "Ada durable fact",
      kind: "business_rule",
      scope: "global",
    });

    await snapshotDurableMemories(orgId, root);
    const { stdout } = await git(root, ["ls-tree", "-r", "--name-only", "HEAD"]);
    expect(stdout).toContain(`memory/business_rule/${team.id}.md`);
    expect(stdout).not.toContain(personal.id);

    const sha = await snapshotUserConfig(orgId, ada, root);
    expect(sha).toMatch(/^[0-9a-f]{40}$/);
    const userFiles = await refTree(root, userConfigRef(ada));
    expect(userFiles).toContain(`memory/business_rule/${personal.id}.md`);
    expect(userFiles).not.toContain(`memory/business_rule/${team.id}.md`);

    const [ref] = await db()
      .select()
      .from(config_ref)
      .where(
        and(
          eq(config_ref.org_id, orgId),
          eq(config_ref.scope, "user"),
          eq(config_ref.user_id, ada),
        ),
      );
    expect(ref?.commit_sha).toBe(sha);
  });

  it("personal workflow saves commit to the user ref, never main's tree", async () => {
    await saveWorkflow({
      orgId,
      name: "Ada Weekly Digest",
      steps: [{ id: "s1", description: "summarize" }],
      goal: "digest",
      ownerUserId: ada,
    });
    expect(existsSync(join(root, "workflows", "ada-weekly-digest.md"))).toBe(
      false,
    );
    expect(await refTree(root, userConfigRef(ada))).toContain(
      "workflows/ada-weekly-digest.md",
    );
    const changes = await db()
      .select()
      .from(config_change)
      .where(eq(config_change.org_id, orgId));
    const row = changes.find(
      (c) => c.artifact_ref === "Ada Weekly Digest" && c.scope === "user",
    );
    expect(row?.user_id).toBe(ada);
  });

  it("adopt copies a personal workflow into the team layer with lineage", async () => {
    const personal = (await saveWorkflow({
      orgId,
      name: "Churn Watch",
      steps: [{ id: "s1", description: "watch churn" }],
      goal: "alert on churn",
      ownerUserId: ada,
    })).workflow;

    const adopted = await adoptWorkflowForTeam(orgId, personal.id, null);
    expect(adopted.ownerUserId).toBe("");
    expect(adopted.parentId).toBe(personal.id);
    expect(existsSync(join(root, "workflows", "churn-watch.md"))).toBe(true);
    // The member's personal copy is untouched.
    expect((await getWorkflow(orgId, personal.id))?.ownerUserId).toBe(ada);

    const changes = await db()
      .select()
      .from(config_change)
      .where(
        and(eq(config_change.org_id, orgId), eq(config_change.status, "adopted")),
      );
    expect(changes.some((c) => c.artifact_ref === "Churn Watch")).toBe(true);
  });

  it("pull lists stale overrides and take_theirs unshadows the team row", async () => {
    const team = await rememberWorkMemory({
      orgId,
      text: "SLA is 24 hours",
      kind: "business_rule",
      scope: "global",
    });
    await overrideWorkMemoryForUser({
      orgId,
      userId: ada,
      memoryId: team.id,
      text: "SLA is 12 hours for my accounts",
    });
    // First override created the fork baseline; nothing stale yet.
    expect(await listMemoryPullUpdates(orgId, ada)).toEqual([]);

    // The team version moves on (admin edit, simulated directly).
    await db()
      .update(work_memory)
      .set({ text: "SLA is 48 hours", updated_at: new Date(Date.now() + 1000) })
      .where(eq(work_memory.id, team.id));

    const updates = await listMemoryPullUpdates(orgId, ada);
    expect(updates).toHaveLength(1);
    expect(updates[0].originId).toBe(team.originId);
    expect(updates[0].teamMemory?.text).toBe("SLA is 48 hours");

    const { applied } = await applyMemoryPull({
      orgId,
      userId: ada,
      decisions: [{ originId: team.originId!, choice: "take_theirs" }],
    });
    expect(applied).toBe(1);
    const adaView = await listWorkMemories(orgId, { userId: ada });
    expect(adaView.map((m) => m.id)).toContain(team.id);
    // Baseline advanced — nothing left to pull.
    expect(await listMemoryPullUpdates(orgId, ada)).toEqual([]);
  });

  it("dropUserConfigData removes the ref and the member's pointers", async () => {
    await snapshotUserConfig(orgId, ada, root);
    await dropUserConfigData({ orgId, userId: ada, workspaceRoot: root });
    await expect(
      git(root, ["rev-parse", "--verify", "--quiet", userConfigRef(ada)]),
    ).rejects.toThrow();
    const refs = await db()
      .select()
      .from(config_ref)
      .where(
        and(
          eq(config_ref.org_id, orgId),
          eq(config_ref.scope, "user"),
          eq(config_ref.user_id, ada),
        ),
      );
    expect(refs).toHaveLength(0);
    const forks = await db()
      .select()
      .from(memory_fork)
      .where(
        and(eq(memory_fork.org_id, orgId), eq(memory_fork.user_id, ada)),
      );
    expect(forks).toHaveLength(0);
  });
});
