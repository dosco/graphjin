import { and, db, eq, pack_action_definition } from "@neko/db";
import {
  registerActionRequestCreatedHook,
  type ActionRequestRecord,
} from "@neko/llm/workflows";

export type PackActionReadiness = {
  enabled: boolean;
  readiness: string;
  reason: string | null;
};

type PackActionLookup = (
  orgId: string,
  kind: string,
) => Promise<PackActionReadiness | null>;

const defaultLookup: PackActionLookup = async (orgId, kind) => {
  const [definition] = await db()
    .select({
      enabled: pack_action_definition.enabled,
      readiness: pack_action_definition.readiness,
      reason: pack_action_definition.readiness_reason,
    })
    .from(pack_action_definition)
    .where(
      and(
        eq(pack_action_definition.org_id, orgId),
        eq(pack_action_definition.kind, kind),
      ),
    )
    .limit(1);
  return definition ?? null;
};

export function assertPackActionReady(
  kind: string,
  definition: PackActionReadiness,
): void {
  if (definition.enabled && definition.readiness === "ready") return;
  const reason = definition.reason?.trim() || "the pack operator is unavailable";
  throw new Error(`pack action ${kind} is blocked: ${reason}`);
}

/**
 * Fail closed for action kinds owned by an installed pack. A pack action is
 * allowed to reach approval/execution only after its operator adapter reports
 * ready. Kinds that are not pack-owned continue through the normal action
 * pipeline unchanged.
 */
export function registerPackActionPreflight(
  lookup: PackActionLookup = defaultLookup,
): () => void {
  return registerActionRequestCreatedHook(async (request: ActionRequestRecord) => {
    const definition = await lookup(request.orgId, request.kind);
    if (definition) assertPackActionReady(request.kind, definition);
  });
}
