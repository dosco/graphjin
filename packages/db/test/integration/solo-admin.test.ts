import { expect, it } from "vitest";
import { ADMINISTRATORS_GROUP_SLUG, and, app_user, db, eq, getOrCreateSoloAdmin, isUnclaimedSoloEmail, soloAdminNeedsEmail, user_group, user_group_membership, work_thread, work_run, workflow_definition, workflow_run,
  setLocalAdministrator,
} from "../../src";
import { dbReachable, withTestOrg } from "./_helpers";
const reachable = await dbReachable();

it.skipIf(!reachable)("upgrades a userless installation once under concurrent requests and retains its owner as users are added", async () => {
  await withTestOrg(async (orgId) => {
    const [oldChat] = await db().insert(work_thread).values({ org_id: orgId, channel: "web", title: "Existing solo chat" }).returning();
    const [channelChat] = await db().insert(work_thread).values({ org_id: orgId, channel: "telegram", title: "Channel chat" }).returning();
    const [workflowChat] = await db().insert(work_thread).values({ org_id: orgId, channel: "web" }).returning();
    const [definition] = await db().insert(workflow_definition).values({ org_id: orgId, name: "Scheduled job" }).returning();
    const [run] = await db().insert(work_run).values({ org_id: orgId, thread_id: workflowChat.id, backend: "ax", actor_role: "service" }).returning();
    await db().insert(workflow_run).values({ org_id: orgId, workflow_id: definition.id, thread_id: workflowChat.id, work_run_id: run.id, trigger_kind: "cron" });
    const owners = await Promise.all(Array.from({ length: 8 }, () => getOrCreateSoloAdmin(orgId)));
    const owner = owners[0]!;
    expect(new Set(owners.map((row) => row?.id)).size).toBe(1);
    const chats = await db().select().from(work_thread).where(eq(work_thread.org_id, orgId));
    expect(chats.find(row => row.id === oldChat.id)?.created_by_user_id).toBe(owner.id);
    expect(chats.find(row => row.id === channelChat.id)?.created_by_user_id).toBeNull();
    expect(chats.find(row => row.id === workflowChat.id)?.created_by_user_id).toBeNull();
    expect(isUnclaimedSoloEmail(owner.email)).toBe(true);
    const admins = await db().select({ userId: user_group_membership.user_id }).from(user_group_membership)
      .innerJoin(user_group, eq(user_group.id, user_group_membership.group_id))
      .where(and(eq(user_group_membership.org_id, orgId), eq(user_group.slug, ADMINISTRATORS_GROUP_SLUG)));
    expect(admins).toEqual([{ userId: owner.id }]);
    expect(await soloAdminNeedsEmail(orgId)).toBe(true);
    await db().insert(app_user).values({ id: `${orgId}-other`, org_id: orgId, email: "other@example.test" });
    expect((await getOrCreateSoloAdmin(orgId))?.id).toBe(owner.id);
    await db().update(app_user).set({ email: "owner@example.test" }).where(eq(app_user.id, owner.id));
    expect(await soloAdminNeedsEmail(orgId)).toBe(false);
    expect((await getOrCreateSoloAdmin(orgId))?.id).toBe(owner.id);
    await db().update(app_user).set({ disabled_at: new Date() }).where(eq(app_user.id, owner.id));
    expect(await getOrCreateSoloAdmin(orgId)).toBeNull();
  }, "solo-empty");
});

it.skipIf(!reachable)("adopts an existing local admin and never chooses an arbitrary admin in ambiguous legacy data", async () => {
  await withTestOrg(async (orgId) => {
    const originalId = `${orgId}-admin`;
    await db().insert(app_user).values({ id: originalId, org_id: orgId, email: "owner@example.test" });
    await setLocalAdministrator(orgId, originalId, true);
    const [personalChat] = await db().insert(work_thread).values({ org_id: orgId, created_by_user_id: originalId }).returning();
    expect((await getOrCreateSoloAdmin(orgId))?.id).toBe(originalId);
    expect((await db().select().from(work_thread).where(eq(work_thread.id, personalChat.id)))[0].created_by_user_id).toBe(originalId);
    expect(await soloAdminNeedsEmail(orgId)).toBe(false);
    await db().insert(app_user).values({ id: `${orgId}-member`, org_id: orgId, email: "member@example.test" });
    expect((await getOrCreateSoloAdmin(orgId))?.id).toBe(originalId);
  }, "solo-existing");
  await withTestOrg(async (orgId) => {
    for (const id of ["a", "b"]) {
      await db().insert(app_user).values({ id: `${orgId}-${id}`, org_id: orgId, email: `${id}@example.test` });
      await setLocalAdministrator(orgId, `${orgId}-${id}`, true);
    }
    const [otherChat] = await db().insert(work_thread).values({ org_id: orgId, created_by_user_id: `${orgId}-a` }).returning();
    const owner = await getOrCreateSoloAdmin(orgId);
    expect((await db().select().from(work_thread).where(eq(work_thread.id, otherChat.id)))[0].created_by_user_id).toBe(`${orgId}-a`);
    expect(owner?.id).not.toBe(`${orgId}-a`);
    expect(owner?.id).not.toBe(`${orgId}-b`);
    const rows = await db().select().from(app_user).where(and(eq(app_user.org_id, orgId), eq(app_user.id, owner!.id)));
    expect(rows).toHaveLength(1);
  }, "solo-ambiguous");
});
