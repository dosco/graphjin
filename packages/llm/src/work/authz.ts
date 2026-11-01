import type { RunActor } from "./store";

/**
 * K2 — the central authorization seam. Two roles by design (admin |
 * member; service = machine principal): admin has the org, member has
 * the personal layer + non-admin reads, service reads only. Enforced
 * server-side at the data/route layer — never UI-only. Everything
 * touching org data via GraphJin is GraphJin RBAC's job (GJ4); this
 * seam governs OpenNeko-native artifacts and decisions.
 */
export class ForbiddenError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ForbiddenError";
  }
}

export type AuthzAction = "read" | "write" | "approve";

export type AuthzResource =
  /** Approving/rejecting an action_request; approverRole comes from the
   *  matched policy (null = any human). */
  | { kind: "action_approval"; approverRole: string | null }
  /** Org-level configuration: rules, data sources, install policy, … */
  | { kind: "org_settings" }
  /** A personally-owned artifact (CV1/CV2 layers). */
  | { kind: "personal"; ownerUserId: string | null };

export function can(
  actor: RunActor,
  action: AuthzAction,
  resource: AuthzResource,
): boolean {
  if (actor.role === "service") return action === "read";
  if (actor.role === "admin") return true;
  switch (resource.kind) {
    case "org_settings":
      return action === "read";
    case "action_approval": {
      if (action === "read") return true;
      const required = resource.approverRole;
      return required === null || required === "member";
    }
    case "personal":
      return (
        action === "read" ||
        (resource.ownerUserId !== null &&
          resource.ownerUserId === actor.userId)
      );
  }
}

export function assertCan(
  actor: RunActor,
  action: AuthzAction,
  resource: AuthzResource,
  what: string = resource.kind,
): void {
  if (!can(actor, action, resource)) {
    throw new ForbiddenError(
      `${actor.role}${actor.userId ? ` ${actor.userId}` : ""} may not ${action} ${what}`,
    );
  }
}
