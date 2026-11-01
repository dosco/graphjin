import { eq, heldItems, packsContaining, type EntitlementActor, type HeldItems, type ItemType } from "@neko/db";

/**
 * The entitlement actor for an agent run. A user run resolves through the
 * user's groups. A run without a user is a service run; a pack workflow is
 * limited to its pack's items, and any other service run is a system job.
 */
export async function runEntitlementActor(
  orgId: string,
  actor: { userId: string | null; role?: string | null },
  opts: { workflowId?: string | null } = {},
): Promise<EntitlementActor> {
  if (actor.userId) return { orgId, kind: "user", userId: actor.userId };
  // A run without a user but with a member snapshot is an unlinked channel
  // sender; it holds nothing. Only service runs act for the system.
  if (actor.role && actor.role !== "service" && actor.role !== "admin") return { orgId, kind: "anonymous" };
  if (opts.workflowId) {
    const [packId] = await packsContaining(orgId, { type: "workflow", id: opts.workflowId });
    if (packId) return { orgId, kind: "service", packId };
  }
  return { orgId, kind: "service" };
}

/** Held item ids for a run, or undefined when the actor holds every item. */
export async function runHeldItemIds(actor: EntitlementActor, type: ItemType): Promise<string[] | undefined> {
  const held = await heldItems(actor, type);
  return held === "*" ? undefined : [...held].sort();
}

/** Entitlement actor for a persisted work run; unknown runs hold nothing. */
export async function entitlementActorForRun(orgId: string, runId: string): Promise<EntitlementActor | null> {
  const { and, db, eq, work_run, workflow_run } = await import("@neko/db");
  const [run] = await db()
    .select({ userId: work_run.actor_user_id, role: work_run.actor_role })
    .from(work_run)
    .where(and(eq(work_run.id, runId), eq(work_run.org_id, orgId)))
    .limit(1);
  if (!run) return null;
  const [workflowRun] = run.userId
    ? []
    : await db()
        .select({ workflowId: workflow_run.workflow_id })
        .from(workflow_run)
        .where(and(eq(workflow_run.work_run_id, runId), eq(workflow_run.org_id, orgId)))
        .limit(1);
  return runEntitlementActor(orgId, { userId: run.userId, role: run.role }, { workflowId: workflowRun?.workflowId ?? null });
}

/**
 * A workflow filter for a run: the run's user sees org workflows they hold
 * and their own personal workflows. Without a run id nothing is filtered.
 */
export async function runWorkflowFilter(
  orgId: string,
  runId: string | null | undefined,
): Promise<(workflow: { id: string; ownerUserId?: string | null }) => boolean> {
  if (!runId) return () => true;
  const actor = await entitlementActorForRun(orgId, runId);
  if (!actor) return () => false;
  const held = await heldItems(actor, "workflow");
  const admin = actor.kind === "service" && !actor.packId;
  if (actor.kind === "anonymous") return () => false;
  return (workflow) => {
    const owner = workflow.ownerUserId ?? "";
    if (actor.kind === "user" && owner === actor.userId) return true;
    if (owner) return admin || held === "*";
    return held === "*" || held.has(workflow.id);
  };
}

/** Keeps action descriptors whose kind and, for plugin actions, whose integration the actor holds. */
export async function filterHeldActions<T extends { kind: string; pluginName?: string }>(
  actor: EntitlementActor,
  actions: readonly T[],
): Promise<T[]> {
  const [held, integrations] = await Promise.all([heldItems(actor, "action"), heldItems(actor, "integration")]);
  return actions.filter(
    (action) =>
      (held === "*" || held.has(action.kind)) &&
      (!action.pluginName || integrations === "*" || integrations.has(action.pluginName)),
  );
}

/**
 * Whether an action request needs the action item: every external action and
 * every pack action. Internal host actions such as memory writes do not.
 */
export async function actionKindIsGranted(orgId: string, kind: string, scope: string): Promise<boolean> {
  if (scope === "external") return true;
  const { and, db, pack_action_definition } = await import("@neko/db");
  const [row] = await db()
    .select({ kind: pack_action_definition.kind })
    .from(pack_action_definition)
    .where(and(eq(pack_action_definition.org_id, orgId), eq(pack_action_definition.kind, kind)))
    .limit(1);
  return Boolean(row);
}
