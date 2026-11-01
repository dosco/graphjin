import { EVERYONE_GROUP_SLUG, and, db, eq, groupHolds, inArray, user_group, workflow_run } from "@neko/db";

/**
 * A binding's audience is "*" (Everyone) or a group slug. The audience must
 * hold the output's workflow, and the watcher when a watcher fired the run.
 */
export async function audienceReceivesOutput(
  orgId: string,
  audience: string,
  workflowRunId: string,
): Promise<boolean> {
  const slug = audience === "*" || !audience ? EVERYONE_GROUP_SLUG : audience;
  const groups = await db()
    .select({ id: user_group.id, slug: user_group.slug })
    .from(user_group)
    .where(and(eq(user_group.org_id, orgId), inArray(user_group.slug, [slug, EVERYONE_GROUP_SLUG])));
  const group = groups.find((g) => g.slug === slug);
  const [run] = await db()
    .select({ workflowId: workflow_run.workflow_id, kind: workflow_run.trigger_kind, payload: workflow_run.trigger_payload })
    .from(workflow_run)
    .where(and(eq(workflow_run.org_id, orgId), eq(workflow_run.id, workflowRunId)))
    .limit(1);
  if (!group || !run) return false;
  // Every member of the audience is also in Everyone, so Everyone's grants count.
  const audienceHolds = async (type: "workflow" | "watcher", id: string) => {
    for (const g of groups) if (await groupHolds(orgId, g.id, type, id)) return true;
    return false;
  };
  if (!(await audienceHolds("workflow", run.workflowId))) return false;
  const watcherId = run.kind === "watcher" ? (run.payload as { watcherId?: unknown } | null)?.watcherId : null;
  return !watcherId || audienceHolds("watcher", String(watcherId));
}
