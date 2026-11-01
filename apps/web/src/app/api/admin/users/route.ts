/**
 * POST /api/admin/users — provision a user ahead of their first sign-in.
 *
 * Exists for auth providers with `provisioning: "manual"` (magic link):
 * possession of a mailbox never mints an account, so an admin creates
 * the row (email + role) first and the sign-in flow only works for
 * emails found here. Works equally under SSO for pre-assigning a role
 * before a user's first login (the sub attaches on that login).
 *
 * Body: { email: string, name?: string, role: "admin" | "member", addToDirectory?: boolean }
 * `addToDirectory` also creates the user in the directory provider (Scalekit).
 */

import { randomBytes } from "node:crypto";
import { NextRequest, NextResponse } from "next/server";
import { and, app_user, db, eq, sql, organization, isUnclaimedSoloEmail, setLocalAdministrator } from "@neko/db";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { getAuthGateStatus } from "@/lib/auth";
import { noSignInResponse } from "@/lib/user-admin-gate";
import { requestWorker } from "@/lib/groups-admin";

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export async function POST(request: NextRequest) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const gate = await getAuthGateStatus();
  if (!gate.provider && !gate.pending) return noSignInResponse();

  let body: { email?: unknown; name?: unknown; role?: unknown; updateSoloAccount?: unknown; addToDirectory?: unknown };
  try {
    body = await request.json();
  } catch {
    return NextResponse.json({ error: "invalid JSON body" }, { status: 400 });
  }

  const email =
    typeof body.email === "string" ? body.email.trim().toLowerCase() : "";
  if (!EMAIL_RE.test(email) || email.length > 320) {
    return NextResponse.json(
      { error: "a valid email address is required" },
      { status: 400 },
    );
  }
  const role = body.role === "admin" ? "admin" : body.role === "member" ? "member" : null;
  if (!role) {
    return NextResponse.json(
      { error: "role must be admin or member" },
      { status: 400 },
    );
  }
  const name =
    typeof body.name === "string" && body.name.trim().length > 0
      ? body.name.trim().slice(0, 200)
      : null;

  const orgId = await getOrgId();
  const [existing] = await db()
    .select({ id: app_user.id })
    .from(app_user)
    .where(
      and(
        eq(app_user.org_id, orgId),
        sql`lower(${app_user.email}) = ${email}`,
      ),
    )
    .limit(1);
  if (existing) {
    return NextResponse.json(
      { error: `a user with email ${email} already exists` },
      { status: 409 },
    );
  }

  let id = `usr_${randomBytes(9).toString("base64url")}`;
  try {
    const created = await db().transaction(async (tx) => {
      await tx.execute(sql`select pg_advisory_xact_lock(hashtext(${"openneko.app_user:" + orgId}))`);
      if (body.updateSoloAccount === true) {
        const [org] = await tx.select({ owner: organization.solo_admin_user_id }).from(organization)
          .where(eq(organization.id, orgId)).limit(1);
        const [owner] = await tx.select({ email: app_user.email, sub: app_user.sub }).from(app_user)
          .where(and(eq(app_user.org_id, orgId), eq(app_user.id, actor.userId!))).limit(1);
        if (org?.owner !== actor.userId || !owner || owner.sub || !isUnclaimedSoloEmail(owner.email) || role !== "admin") return false;
        id = actor.userId!;
        await tx.update(app_user).set({ email, name, updated_at: new Date() }).where(eq(app_user.id, id));
        return true;
      }
      await tx.insert(app_user).values({ id, sub: null, email, name, org_id: orgId });
      return true;
    });
    if (!created) return NextResponse.json({ error: "This account cannot be updated here. Reload the page." }, { status: 409 });
    // Administrators membership records the role; there is no role column.
    if (role === "admin") await setLocalAdministrator(orgId, id, true);
  } catch (e) {
    // app_user_org_email_unique: a concurrent provision (double-click,
    // second admin tab) won the race between our lookup and this insert.
    const code =
      (e as { code?: string })?.code ??
      ((e as { cause?: { code?: string } })?.cause?.code);
    if (code === "23505") {
      return NextResponse.json(
        { error: `a user with email ${email} already exists` },
        { status: 409 },
      );
    }
    throw e;
  }
  let directoryError: string | null = null;
  if (body.addToDirectory === true && body.updateSoloAccount !== true) {
    const result = await requestWorker("/admin/directory/users", { email, name }).catch((err: unknown) => ({
      status: 503,
      body: { error: err instanceof Error ? err.message : "worker is unavailable" },
    }));
    if (result.status >= 400) directoryError = (result.body as { error?: string }).error ?? `HTTP ${result.status}`;
  }
  return NextResponse.json(
    { user: { id, email, name, role }, ...(directoryError ? { directoryError } : {}) },
    { status: body.updateSoloAccount === true ? 200 : 201 },
  );
}
