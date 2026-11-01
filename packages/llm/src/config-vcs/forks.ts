import { createHash } from "node:crypto";
import { existsSync } from "node:fs";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { git } from "./git-shell";
import { ensureConfigRepo } from "./index";
import { withRepoLock } from "./lock";

/**
 * CV4 — personal config layers as `user/<id>` refs in the org config
 * repo. The refs are written with a temporary index (never the working
 * tree, which stays the team checkout) and are never merged into main —
 * so offboarding can drop a person's entire git footprint by deleting
 * one ref and pruning (docs/DATA_LIFECYCLE.md §2/§3).
 */

export type UserConfigFile = { path: string; content: string };

/** Git-ref-safe branch name for a user id; no emails/names in refs. */
export function userConfigRef(userId: string): string {
  const slug = userId.replace(/[^a-zA-Z0-9_-]/g, "-").replace(/^-+|-+$/g, "");
  const safe =
    slug === userId
      ? slug
      : `${slug}-${createHash("sha256").update(userId).digest("hex").slice(0, 8)}`;
  return `refs/heads/user/${safe || "anon"}`;
}

/**
 * Commit a tree to the user's ref without touching the working tree.
 * mode 'replace' makes the provided files the entire tree (snapshot);
 * 'overlay' starts from the ref's current tree and lays the files on
 * top (single-artifact save). Returns the new sha, or null when the
 * tree is unchanged.
 */
export async function commitToUserRef(opts: {
  workspaceRoot: string;
  userId: string;
  files: UserConfigFile[];
  message: string;
  mode?: "replace" | "overlay";
}): Promise<string | null> {
  const root = resolve(opts.workspaceRoot);
  await ensureConfigRepo(root);
  return withRepoLock(root, async () => {
    const ref = userConfigRef(opts.userId);
    const parent = await git(root, ["rev-parse", "--verify", "--quiet", ref])
      .then((r) => r.stdout.trim() || null)
      .catch(() => null);
    const tmp = await mkdtemp(join(tmpdir(), "neko-gitidx-"));
    const env = { GIT_INDEX_FILE: join(tmp, "index") };
    try {
      if (opts.mode === "overlay" && parent) {
        await git(root, ["read-tree", parent], { env });
      } else {
        await git(root, ["read-tree", "--empty"], { env });
      }
      for (const file of opts.files) {
        const { stdout: blob } = await git(
          root,
          ["hash-object", "-w", "--stdin"],
          { env, input: file.content },
        );
        await git(
          root,
          [
            "update-index",
            "--add",
            "--cacheinfo",
            `100644,${blob.trim()},${file.path}`,
          ],
          { env },
        );
      }
      const { stdout: treeOut } = await git(root, ["write-tree"], { env });
      const tree = treeOut.trim();
      if (parent) {
        const { stdout: parentTree } = await git(root, [
          "rev-parse",
          `${parent}^{tree}`,
        ]);
        if (parentTree.trim() === tree) return null;
      }
      const { stdout: shaOut } = await git(root, [
        "commit-tree",
        tree,
        ...(parent ? ["-p", parent] : []),
        "-m",
        opts.message,
      ]);
      const sha = shaOut.trim();
      await git(root, ["update-ref", ref, sha]);
      return sha;
    } finally {
      await rm(tmp, { recursive: true, force: true });
    }
  });
}

const DURABLE_KINDS = [
  "business_rule",
  "preference",
  "metric_definition",
  "company_context",
] as const;

/**
 * Snapshot one member's personal layer (durable memories + personal
 * workflows) to their `user/<id>` ref and record the ref pointer.
 * Best-effort like every config-vcs hook.
 */
export async function snapshotUserConfig(
  orgId: string,
  userId: string,
  workspaceRoot: string,
): Promise<string | null> {
  try {
    const { db, work_memory, and, eq, inArray, isNull } = await import(
      "@neko/db"
    );
    const rows = await db()
      .select({
        id: work_memory.id,
        kind: work_memory.kind,
        text: work_memory.text,
        pinned: work_memory.pinned,
        suppressed: work_memory.suppressed,
        overridesOriginId: work_memory.overrides_origin_id,
        updatedAt: work_memory.updated_at,
      })
      .from(work_memory)
      .where(
        and(
          eq(work_memory.org_id, orgId),
          eq(work_memory.user_id, userId),
          inArray(work_memory.kind, [...DURABLE_KINDS]),
          isNull(work_memory.archived_at),
        ),
      );
    const files: UserConfigFile[] = rows.map((row) => ({
      path: `memory/${row.kind}/${row.id}.md`,
      content: [
        "---",
        `type: ${row.kind}`,
        `id: ${row.id}`,
        `kind: ${row.kind}`,
        `pinned: ${row.pinned}`,
        ...(row.overridesOriginId
          ? [`overrides_origin_id: ${row.overridesOriginId}`]
          : []),
        ...(row.suppressed ? ["suppressed: true"] : []),
        `updated_at: ${row.updatedAt.toISOString()}`,
        "---",
        "",
        row.text,
        "",
      ].join("\n"),
    }));
    const { personalWorkflowFiles } = await import("../workflows/store");
    files.push(...(await personalWorkflowFiles(orgId, userId)));

    const sha = await commitToUserRef({
      workspaceRoot,
      userId,
      files,
      message: "Personal config checkpoint",
      mode: "replace",
    });
    if (sha) await upsertUserRefPointer(orgId, userId, sha);
    return sha;
  } catch (err) {
    console.warn(
      `[config-vcs] user snapshot failed: ${err instanceof Error ? err.message : err}`,
    );
    return null;
  }
}

/** Snapshot every member who has personal config in this org. */
export async function snapshotUserConfigsForOrg(
  orgId: string,
  workspaceRoot: string,
): Promise<void> {
  try {
    const { db, work_memory, workflow_definition, and, eq, isNull, ne, sql } =
      await import("@neko/db");
    const memoryOwners = await db()
      .selectDistinct({ userId: work_memory.user_id })
      .from(work_memory)
      .where(
        and(
          eq(work_memory.org_id, orgId),
          sql`${work_memory.user_id} IS NOT NULL`,
          isNull(work_memory.archived_at),
        ),
      );
    const workflowOwners = await db()
      .selectDistinct({ userId: workflow_definition.owner_user_id })
      .from(workflow_definition)
      .where(
        and(
          eq(workflow_definition.org_id, orgId),
          ne(workflow_definition.owner_user_id, ""),
        ),
      );
    const owners = new Set<string>();
    for (const row of [...memoryOwners, ...workflowOwners]) {
      if (row.userId) owners.add(row.userId);
    }
    for (const userId of owners) {
      await snapshotUserConfig(orgId, userId, workspaceRoot);
    }
  } catch (err) {
    console.warn(
      `[config-vcs] org user snapshots failed: ${err instanceof Error ? err.message : err}`,
    );
  }
}

/**
 * GDPR offboarding (DATA_LIFECYCLE §2): drop the member's whole git
 * footprint — delete their ref, expire reflogs, prune unreachable
 * blobs — and remove their DB pointers.
 */
export async function dropUserConfigData(opts: {
  orgId: string;
  userId: string;
  workspaceRoot: string;
}): Promise<void> {
  const root = resolve(opts.workspaceRoot);
  if (existsSync(join(root, ".git"))) {
    await withRepoLock(root, async () => {
      await git(root, ["update-ref", "-d", userConfigRef(opts.userId)]).catch(
        () => {},
      );
      await git(root, ["reflog", "expire", "--expire=now", "--all"]).catch(
        () => {},
      );
      await git(root, ["gc", "--prune=now", "--quiet"]).catch(() => {});
    });
  }
  const { db, config_ref, memory_fork, and, eq } = await import("@neko/db");
  await db()
    .delete(config_ref)
    .where(
      and(
        eq(config_ref.org_id, opts.orgId),
        eq(config_ref.scope, "user"),
        eq(config_ref.user_id, opts.userId),
      ),
    );
  await db()
    .delete(memory_fork)
    .where(
      and(
        eq(memory_fork.org_id, opts.orgId),
        eq(memory_fork.user_id, opts.userId),
      ),
    );
}

async function upsertUserRefPointer(
  orgId: string,
  userId: string,
  sha: string,
): Promise<void> {
  try {
    const { db, config_ref } = await import("@neko/db");
    await db()
      .insert(config_ref)
      .values({ org_id: orgId, scope: "user", user_id: userId, commit_sha: sha })
      .onConflictDoUpdate({
        target: [config_ref.org_id, config_ref.scope, config_ref.user_id],
        set: { commit_sha: sha, updated_at: new Date() },
      });
  } catch (err) {
    console.warn(
      `[config-vcs] user config_ref update failed (commit persisted): ${err instanceof Error ? err.message : err}`,
    );
  }
}
