import { action_policy, and, db, eq, metric, pack_action_definition, watcher, workflow_definition } from "@neko/db";
import type { PackPlan, SolutionPackBundle } from "@neko/packs";

export async function assertNativeTargetsAvailable(input: { orgId: string; bundle: SolutionPackBundle; plan: PackPlan }): Promise<void> {
  const creates = new Set(input.plan.entries.filter((entry) => entry.action === "create").map((entry) => `${entry.kind}:${entry.key}`));
  for (const artifact of input.bundle.artifacts) {
    if (!creates.has(`${artifact.kind}:${artifact.key}`)) continue;
    const value = artifact.content && typeof artifact.content === "object" && !Array.isArray(artifact.content)
      ? artifact.content as Record<string, unknown> : null;
    let existing: { id: string } | undefined;
    if (artifact.kind === "metric" && value) {
      [existing] = await db().select({ id: metric.id }).from(metric).where(and(eq(metric.org_id, input.orgId), eq(metric.role, String(value.role)), eq(metric.slug, artifact.targetRef))).limit(1);
    } else if (artifact.kind === "workflow" && value) {
      [existing] = await db().select({ id: workflow_definition.id }).from(workflow_definition).where(and(eq(workflow_definition.org_id, input.orgId), eq(workflow_definition.owner_user_id, ""), eq(workflow_definition.name, String(value.name)))).limit(1);
    } else if (artifact.kind === "watcher" && value) {
      [existing] = await db().select({ id: watcher.id }).from(watcher).where(and(eq(watcher.org_id, input.orgId), eq(watcher.name, String(value.name)))).limit(1);
    } else if (artifact.kind === "policy" && value) {
      [existing] = await db().select({ id: action_policy.id }).from(action_policy).where(and(eq(action_policy.org_id, input.orgId), eq(action_policy.name, String(value.name)))).limit(1);
    } else if (artifact.kind === "action" && value) {
      [existing] = await db().select({ id: pack_action_definition.id }).from(pack_action_definition).where(and(eq(pack_action_definition.org_id, input.orgId), eq(pack_action_definition.kind, String(value.kind)))).limit(1);
    }
    if (existing) throw new Error(`${artifact.kind} target ${artifact.targetRef} already exists and is not owned by pack ${input.bundle.manifest.metadata.id}`);
  }
}
