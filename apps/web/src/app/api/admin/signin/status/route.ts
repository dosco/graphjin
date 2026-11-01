/**
 * GET /api/admin/signin/status — everything the sign-in settings card
 * needs in one read: the gate (live provider or pending-with-reasons)
 * plus provisioned-user counts. Admin-only: the pending block names
 * unset env vars, which the public /api/auth/status must not.
 */

import { NextResponse } from "next/server";
import { activeAdministratorIds, and, app_user, db, eq, isNull, isUnclaimedSoloEmail, sql } from "@neko/db";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getAuthGateStatus, _resetAuthProviderCache } from "@/lib/auth";
import { getOrgId } from "@/lib/db";

export async function GET() {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;

  // Settings page loads want the current truth, not the 1s cache.
  _resetAuthProviderCache();
  const gate = await getAuthGateStatus();

  const orgId = await getOrgId();
  const [administrators, [userCount]] = await Promise.all([
    activeAdministratorIds(orgId),
    db().select({ total: sql<number>`count(*)::int` }).from(app_user).where(eq(app_user.org_id, orgId)),
  ]);
  const [self] = actor.userId
    ? await db()
        .select({ email: app_user.email })
        .from(app_user)
        .where(and(eq(app_user.id, actor.userId), isNull(app_user.disabled_at)))
        .limit(1)
    : [];

  return NextResponse.json({
    ...gate,
    users: {
      total: userCount?.total ?? 0,
      activeAdmins: administrators.length,
    },
    /** The signed-in admin's own provisioned email, for test prefill. */
    selfEmail: self?.email ?? null,
    selfNeedsEmail: Boolean(self && isUnclaimedSoloEmail(self.email)),
  });
}
