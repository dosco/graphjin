import { randomBytes } from "node:crypto";
import { and, eq, isNull, sql } from "drizzle-orm";
import { db } from "./index";
import { ADMINISTRATORS_GROUP_SLUG, app_user, organization, user_group, user_group_membership, work_thread, workflow_run } from "./schema";

export function isUnclaimedSoloEmail(email: string): boolean {
  return email.endsWith("@solo.openneko.invalid");
}

async function readOwner(orgId: string, runner = db()) {
  const [row] = await runner.select({
    id: app_user.id, email: app_user.email, name: app_user.name,
    disabledAt: app_user.disabled_at,
  }).from(organization).innerJoin(app_user, eq(organization.solo_admin_user_id, app_user.id))
    .where(and(eq(organization.id, orgId), eq(app_user.org_id, orgId))).limit(1);
  if (!row) return row;
  return { ...row, role: (await administers(orgId, row.id, runner)) ? "admin" : "member" };
}

/** Administrators membership; migration 0079 dropped app_user.role. */
async function administers(orgId: string, userId: string, runner = db()): Promise<boolean> {
  const [row] = await runner.select({ id: user_group_membership.user_id })
    .from(user_group_membership)
    .innerJoin(user_group, eq(user_group.id, user_group_membership.group_id))
    .where(and(eq(user_group_membership.org_id, orgId), eq(user_group_membership.user_id, userId),
      eq(user_group.slug, ADMINISTRATORS_GROUP_SLUG))).limit(1);
  return Boolean(row);
}

async function joinAdministrators(orgId: string, userId: string, runner = db()): Promise<void> {
  await runner.execute(sql`
    insert into user_group_membership (org_id, group_id, user_id, source)
    select ${orgId}, g.id, ${userId}, 'local' from user_group g
    where g.org_id = ${orgId} and g.slug = ${ADMINISTRATORS_GROUP_SLUG}
    on conflict do nothing`);
}

/** Call only after the auth gate confirms this is a solo installation. */
export async function getOrCreateSoloAdmin(orgId: string) {
  const existing = await readOwner(orgId);
  if (existing) return existing.role === "admin" && !existing.disabledAt ? existing : null;
  return db().transaction(async (tx) => {
    await tx.execute(sql`select pg_advisory_xact_lock(hashtext(${"openneko.app_user:" + orgId}))`);
    const [org] = await tx.select({ owner: organization.solo_admin_user_id })
      .from(organization).where(eq(organization.id, orgId)).limit(1);
    if (!org) throw new Error("Organization not found");
    let ownerId = org.owner;
    if (!ownerId) {
      const candidates = await tx.select({ id: app_user.id }).from(app_user)
        .innerJoin(user_group_membership, eq(user_group_membership.user_id, app_user.id))
        .innerJoin(user_group, eq(user_group.id, user_group_membership.group_id))
        .where(and(eq(app_user.org_id, orgId), eq(user_group.slug, ADMINISTRATORS_GROUP_SLUG),
          isNull(app_user.sub), isNull(app_user.disabled_at)))
        .limit(2);
      // Adopt an unambiguous existing local admin. Otherwise persist a dedicated
      // local operator rather than attributing their work to an arbitrary user.
      ownerId = candidates.length === 1 ? candidates[0].id : `usr_${randomBytes(9).toString("base64url")}`;
      if (candidates.length !== 1) {
        await tx.insert(app_user).values({
          id: ownerId, org_id: orgId, name: "Solo administrator",
          email: `${ownerId}@solo.openneko.invalid`,
        });
      }
      await joinAdministrators(orgId, ownerId, tx);
      await tx.update(organization).set({ solo_admin_user_id: ownerId }).where(eq(organization.id, orgId));
      // These are exactly the unowned local chats visible to the old solo
      // principal. Move ownership atomically so assigning an ID cannot hide them.
      // Keep channel/workflow histories and already-owned chats untouched.
      await tx.update(work_thread).set({ created_by_user_id: ownerId }).where(and(
        eq(work_thread.org_id, orgId), eq(work_thread.channel, "web"),
        isNull(work_thread.created_by_user_id),
        sql`not exists (select 1 from ${workflow_run} where ${workflow_run.thread_id} = ${work_thread.id})`,
      ));
    }
    const [owner] = await tx.select({ id: app_user.id, email: app_user.email, name: app_user.name,
      disabledAt: app_user.disabled_at }).from(app_user)
      .where(and(eq(app_user.id, ownerId), eq(app_user.org_id, orgId))).limit(1);
    if (!owner || owner.disabledAt || !(await administers(orgId, owner.id, tx))) return null;
    return { ...owner, role: "admin" as const };
  });
}

/** SSO setup must collect a real mailbox before switching this owner to sign-in. */
export async function soloAdminNeedsEmail(orgId: string): Promise<boolean> {
  const owner = await readOwner(orgId);
  return Boolean(owner && isUnclaimedSoloEmail(owner.email));
}
