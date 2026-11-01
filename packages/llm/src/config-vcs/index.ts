import { existsSync } from "node:fs";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { basename, join, resolve } from "node:path";
import { git } from "./git-shell";
import { withRepoLock } from "./lock";

/**
 * CV0 — config-vcs: invisible auto-versioning. One git repo per org whose
 * working tree IS the org agents dir (~/.config/openneko/agents/orgs/<id>);
 * only the config artifacts (skills/, workflows/, memory/) are tracked.
 * Commits are system-authored — no personal identity in git, per
 * docs/DATA_LIFECYCLE.md §3. Failures are the caller's to swallow: a
 * versioning hiccup must never fail the artifact write itself.
 */

const TRACKED_IGNORE = `# config-vcs (CV0): version config artifacts only.
*
!.gitignore
!index.md
!skills/
!skills/**
!workflows/
!workflows/**
!memory/
!memory/**
!skill-overlays/
!skill-overlays/**
`;

// OKF bundle root: declares the version and catalogs the tracked trees.
// Static — the per-directory contents change, the shape doesn't.
const ROOT_INDEX = `---
okf_version: "0.2"
title: "OpenNeko org knowledge"
---

# OpenNeko org knowledge

- [skills/](skills/) — what the assistant knows how to do
- [workflows/](workflows/) — scheduled and saved workflows
- [memory/](memory/) — durable team memories
- [skill-overlays/](skill-overlays/) — additive learned guidance for skills
`;

export type ConfigCommit = {
  sha: string;
  message: string;
  date: string;
};

export async function readConfigHead(
  workspaceRoot: string,
): Promise<string | null> {
  const root = resolve(workspaceRoot);
  if (!existsSync(join(root, ".git"))) return null;
  try {
    const { stdout } = await git(root, ["rev-parse", "HEAD"]);
    const sha = stdout.trim();
    return sha || null;
  } catch {
    return null;
  }
}

export async function ensureConfigRepo(workspaceRoot: string): Promise<void> {
  const root = resolve(workspaceRoot);
  await withRepoLock(root, async () => {
    if (!existsSync(join(root, ".git"))) {
      await mkdir(root, { recursive: true });
      await git(root, ["init", "--initial-branch=main"]);
    }
    const ignorePath = join(root, ".gitignore");
    // Rewrite when stale too (not just missing) so repos initialized
    // before a tracked dir was added — e.g. library/ — pick it up.
    const current = existsSync(ignorePath)
      ? await readFile(ignorePath, "utf8").catch(() => null)
      : null;
    const indexPath = join(root, "index.md");
    const currentIndex = existsSync(indexPath)
      ? await readFile(indexPath, "utf8").catch(() => null)
      : null;
    if (current !== TRACKED_IGNORE || currentIndex !== ROOT_INDEX) {
      await writeFile(ignorePath, TRACKED_IGNORE, "utf8");
      await writeFile(indexPath, ROOT_INDEX, "utf8");
      await git(root, ["add", ".gitignore", "index.md"]);
      await git(root, [
        "commit",
        "-m",
        current === null
          ? "Initialize config versioning"
          : "Update tracked config paths",
      ]);
    }
  });
}

/**
 * Stage + commit the given paths (relative to the workspace root). Returns
 * the new HEAD sha, or null when there was nothing to commit.
 */
export async function commitConfigChange(opts: {
  workspaceRoot: string;
  paths: string[];
  message: string;
}): Promise<string | null> {
  const root = resolve(opts.workspaceRoot);
  await ensureConfigRepo(root);
  return withRepoLock(root, async () => {
    await git(root, ["add", "-A", "--", ...opts.paths]);
    const { stdout: status } = await git(root, [
      "status",
      "--porcelain",
      "--",
      ...opts.paths,
    ]);
    if (!status.trim()) return null;
    await git(root, ["commit", "-m", opts.message]);
    const { stdout } = await git(root, ["rev-parse", "HEAD"]);
    return stdout.trim();
  });
}

/** Plain-English history for the whole repo or one artifact path. */
export async function listConfigHistory(
  workspaceRoot: string,
  path?: string,
  limit = 50,
): Promise<ConfigCommit[]> {
  const root = resolve(workspaceRoot);
  if (!existsSync(join(root, ".git"))) return [];
  const args = [
    "log",
    `--max-count=${limit}`,
    "--pretty=format:%H%x09%cI%x09%s",
  ];
  if (path) args.push("--", path);
  const { stdout } = await git(root, args).catch(() => ({ stdout: "" }));
  return stdout
    .split("\n")
    .filter(Boolean)
    .map((line) => {
      const [sha, date, ...rest] = line.split("\t");
      return { sha: sha ?? "", date: date ?? "", message: rest.join("\t") };
    });
}

/**
 * Restore one artifact path to its state at `sha`, recorded as a NEW
 * commit (history only moves forward; no resets).
 */
export async function restoreConfigPath(opts: {
  workspaceRoot: string;
  sha: string;
  path: string;
}): Promise<string | null> {
  const root = resolve(opts.workspaceRoot);
  if (!existsSync(join(root, ".git"))) return null;
  return withRepoLock(root, async () => {
    await git(root, ["checkout", opts.sha, "--", opts.path]);
    await git(root, ["add", "-A", "--", opts.path]);
    const { stdout: status } = await git(root, [
      "status",
      "--porcelain",
      "--",
      opts.path,
    ]);
    if (!status.trim()) return null;
    await git(root, [
      "commit",
      "-m",
      `Restored ${basename(opts.path)} to ${opts.sha.slice(0, 8)}`,
    ]);
    const { stdout } = await git(root, ["rev-parse", "HEAD"]);
    return stdout.trim();
  });
}

/**
 * Best-effort hook used by artifact writers: commit + record the team
 * ref pointer. Never throws — a versioning failure must not fail the
 * write that triggered it.
 */
export async function recordConfigChange(opts: {
  workspaceRoot: string;
  orgId?: string;
  paths: string[];
  message: string;
  /** CV4: also write a config_change audit row (attribution lives in the DB, never in git). */
  artifactKind?: string;
  artifactRef?: string;
  actorUserId?: string | null;
}): Promise<string | null> {
  try {
    const sha = await commitConfigChange(opts);
    if (!sha) return null;
    const orgId = opts.orgId ?? basename(resolve(opts.workspaceRoot));
    await upsertTeamRef(orgId, sha);
    if (opts.artifactKind && opts.artifactRef) {
      await insertConfigChangeRow({
        orgId,
        artifactKind: opts.artifactKind,
        artifactRef: opts.artifactRef,
        actorUserId: opts.actorUserId ?? null,
        commitSha: sha,
        summary: opts.message,
      });
    }
    return sha;
  } catch (err) {
    console.warn(
      `[config-vcs] versioning failed (write succeeded): ${err instanceof Error ? err.message : err}`,
    );
    return null;
  }
}

export async function insertConfigChangeRow(input: {
  orgId: string;
  artifactKind: string;
  artifactRef: string;
  actorUserId?: string | null;
  commitSha?: string | null;
  summary?: string;
  scope?: "team" | "user";
  userId?: string;
  status?: string;
}): Promise<void> {
  try {
    const { db, config_change } = await import("@neko/db");
    await db().insert(config_change).values({
      org_id: input.orgId,
      artifact_kind: input.artifactKind,
      artifact_ref: input.artifactRef,
      scope: input.scope ?? "team",
      user_id: input.userId ?? "",
      actor_user_id: input.actorUserId ?? null,
      commit_sha: input.commitSha ?? null,
      summary: input.summary ?? "",
      status: input.status ?? "recorded",
    });
  } catch (err) {
    console.warn(
      `[config-vcs] config_change insert failed: ${err instanceof Error ? err.message : err}`,
    );
  }
}

async function upsertTeamRef(orgId: string, sha: string): Promise<void> {
  try {
    const { db, config_ref, sql } = await import("@neko/db");
    await db()
      .insert(config_ref)
      .values({ org_id: orgId, scope: "team", commit_sha: sha })
      .onConflictDoUpdate({
        target: [config_ref.org_id, config_ref.scope, config_ref.user_id],
        set: { commit_sha: sha, updated_at: new Date() },
      });
    void sql;
  } catch (err) {
    console.warn(
      `[config-vcs] config_ref update failed (commit persisted): ${err instanceof Error ? err.message : err}`,
    );
  }
}
