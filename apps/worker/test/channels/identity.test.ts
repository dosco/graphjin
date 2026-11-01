import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import { and, app_user, channel_identity, db, eq, pool,
  setLocalAdministrator,
} from "@neko/db";
import { resolveChannelActor } from "../../src/channels/identity.js";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[channel-identity] skipping: Postgres unreachable.");
}

const PLUGIN = "@open-neko/channel-slack";

describeIfDb("resolveChannelActor (CH3)", () => {
  const orgId = uniqueOrgId("chid");
  let ada: string;
  let boss: string;

  beforeAll(async () => {
    await createTestOrg(orgId);
    ada = `${orgId}-ada`;
    boss = `${orgId}-boss`;
    await db()
      .insert(app_user)
      .values([
        { id: ada, email: "Ada@Example.com", org_id: orgId },
        { id: boss, email: "boss@example.com", org_id: orgId },
      ]);
    await setLocalAdministrator(orgId, boss, true);
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
    await pool().end();
  });

  it("no sender → anonymous member, no identity row", async () => {
    expect(await resolveChannelActor(orgId, PLUGIN, undefined)).toEqual({
      userId: null,
      role: "member",
    });
  });

  it("unknown sender → anonymous member + unverified identity row", async () => {
    const actor = await resolveChannelActor(orgId, PLUGIN, {
      id: "U001",
      workspaceId: "T1",
      displayName: "Stranger",
    });
    expect(actor).toEqual({ userId: null, role: "member" });
    const [row] = await db()
      .select()
      .from(channel_identity)
      .where(
        and(
          eq(channel_identity.org_id, orgId),
          eq(channel_identity.channel_user_id, "U001"),
        ),
      );
    expect(row).toMatchObject({
      status: "unverified",
      app_user_id: null,
      display_name: "Stranger",
      workspace_id: "T1",
    });
  });

  it("email match auto-links (case-insensitive) and acts as that user", async () => {
    const actor = await resolveChannelActor(orgId, PLUGIN, {
      id: "U002",
      workspaceId: "T1",
      email: "ada@example.com",
    });
    expect(actor).toEqual({ userId: ada, role: "member" });
    const [row] = await db()
      .select()
      .from(channel_identity)
      .where(
        and(
          eq(channel_identity.org_id, orgId),
          eq(channel_identity.channel_user_id, "U002"),
        ),
      );
    expect(row).toMatchObject({ status: "linked", app_user_id: ada });
    expect(row.verified_at).toBeTruthy();

    // Second message resolves via the link, no re-match needed.
    const again = await resolveChannelActor(orgId, PLUGIN, {
      id: "U002",
      workspaceId: "T1",
    });
    expect(again).toEqual({ userId: ada, role: "member" });
  });

  it("a linked admin acts as admin from the channel", async () => {
    await db().insert(channel_identity).values({
      org_id: orgId,
      channel_plugin: PLUGIN,
      workspace_id: "T1",
      channel_user_id: "U003",
      app_user_id: boss,
      status: "linked",
    });
    expect(
      await resolveChannelActor(orgId, PLUGIN, { id: "U003", workspaceId: "T1" }),
    ).toEqual({ userId: boss, role: "admin" });
  });

  it("a disabled linked user falls back to anonymous member", async () => {
    await db()
      .update(app_user)
      .set({ disabled_at: new Date() })
      .where(eq(app_user.id, boss));
    expect(
      await resolveChannelActor(orgId, PLUGIN, { id: "U003", workspaceId: "T1" }),
    ).toEqual({ userId: null, role: "member" });
    await db()
      .update(app_user)
      .set({ disabled_at: null })
      .where(eq(app_user.id, boss));
  });

  it("blocked identities are flagged so the inbound is dropped", async () => {
    await db().insert(channel_identity).values({
      org_id: orgId,
      channel_plugin: PLUGIN,
      workspace_id: "T1",
      channel_user_id: "U004",
      status: "blocked",
    });
    expect(
      await resolveChannelActor(orgId, PLUGIN, { id: "U004", workspaceId: "T1" }),
    ).toEqual({ userId: null, role: "member", blocked: true });
  });

  it("a disabled user is never auto-linked by email", async () => {
    await db()
      .update(app_user)
      .set({ disabled_at: new Date() })
      .where(eq(app_user.id, ada));
    const actor = await resolveChannelActor(orgId, PLUGIN, {
      id: "U005",
      workspaceId: "T1",
      email: "ada@example.com",
    });
    expect(actor).toEqual({ userId: null, role: "member" });
    await db()
      .update(app_user)
      .set({ disabled_at: null })
      .where(eq(app_user.id, ada));
  });
});
