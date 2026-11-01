import { mkdir, readdir, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { recordConfigChange } from "./index";

/**
 * CV0 memory snapshotter: serialize the org's durable, non-archived
 * memories to memory/<kind>/<id>.md (frontmatter + text — never
 * embeddings, never user-layer rows per docs/DATA_LIFECYCLE.md §3) and
 * commit. Runs on the worker's nightly sweep; cheap when nothing changed
 * (no-op commit is skipped).
 */
const DURABLE_KINDS = [
  "business_rule",
  "preference",
  "metric_definition",
  "company_context",
] as const;

export async function snapshotDurableMemories(
  orgId: string,
  workspaceRoot: string,
): Promise<void> {
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
        updatedAt: work_memory.updated_at,
      })
      .from(work_memory)
      .where(
        and(
          eq(work_memory.org_id, orgId),
          inArray(work_memory.kind, [...DURABLE_KINDS]),
          // CV2/CV4: team layer only — user-layer rows live exclusively
          // on `user/<id>` refs so they are deletable whole (§3).
          isNull(work_memory.user_id),
          isNull(work_memory.archived_at),
        ),
      );

    const memoryRoot = join(workspaceRoot, "memory");
    // Rewrite the snapshot tree wholesale so deletes/archives disappear.
    await rm(memoryRoot, { recursive: true, force: true });
    await mkdir(memoryRoot, { recursive: true });
    for (const row of rows) {
      const dir = join(memoryRoot, row.kind);
      await mkdir(dir, { recursive: true });
      const body = [
        "---",
        // OKF v0.2 conformance: `type` is the one required field.
        `type: ${row.kind}`,
        `id: ${row.id}`,
        `kind: ${row.kind}`,
        `pinned: ${row.pinned}`,
        `updated_at: ${row.updatedAt.toISOString()}`,
        "---",
        "",
        row.text,
        "",
      ].join("\n");
      await writeFile(join(dir, `${row.id}.md`), body, "utf8");
    }
    // Keep git from seeing an empty dir as "deleted everything" noise.
    const entries = await readdir(memoryRoot).catch(() => []);
    if (entries.length === 0) {
      await rm(memoryRoot, { recursive: true, force: true });
    }

    await recordConfigChange({
      workspaceRoot,
      orgId,
      paths: ["memory"],
      message: "Memory checkpoint",
    });
  } catch (err) {
    console.warn(
      `[config-vcs] memory snapshot failed: ${err instanceof Error ? err.message : err}`,
    );
  }
}
