import { describe, expect, it } from "vitest";
import {
  addLocalGroupMember,
  app_user,
  builtinGroupId,
  channel_identity,
  createUserGroup,
  db,
  grantItem,
  organization,
  eq,
  pool,
  revokeItem,
} from "@neko/db";
import { audienceReceivesOutput } from "../../src/channels/audience";
import { resolveChannelActor } from "../../src/channels/identity";

const reachable = await pool().query("select 1").then(() => true, () => false);

(reachable ? describe : describe.skip)("channel entitlements", () => {
  async function withOrg(fn: (orgId: string) => Promise<void>) {
    const orgId = `chan-ent-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`;
    await db().insert(organization).values({ id: orgId, name: "Channels" });
    try {
      await fn(orgId);
    } finally {
      await db().delete(organization).where(eq(organization.id, orgId));
    }
  }

  it("delivers an output only to audiences that hold its workflow and watcher", async () => {
    await withOrg(async (orgId) => {
      const finance = await createUserGroup(orgId, { name: "Finance" });
      const everyone = await builtinGroupId(orgId, "everyone");
      await revokeItem(orgId, { groupId: everyone, itemType: "workflow", itemId: "*" });
      const wf = await db().execute<{ id: string }>(`insert into workflow_definition (org_id, name) values ('${orgId}', 'Revenue') returning id`);
      const workflowId = wf.rows[0]!.id;
      const thread = await db().execute<{ id: string }>(`insert into work_thread (org_id, title) values ('${orgId}', 't') returning id`);
      const run = await db().execute<{ id: string }>(
        `insert into work_run (org_id, thread_id, backend, status) values ('${orgId}', '${thread.rows[0]!.id}', 'ax', 'completed') returning id`,
      );
      const wr = await db().execute<{ id: string }>(
        `insert into workflow_run (org_id, workflow_id, thread_id, work_run_id, trigger_kind, trigger_payload)
         values ('${orgId}', '${workflowId}', '${thread.rows[0]!.id}', '${run.rows[0]!.id}', 'watcher', '{"watcherId":"w-1"}') returning id`,
      );
      const runId = wr.rows[0]!.id;

      expect(await audienceReceivesOutput(orgId, "*", runId)).toBe(false);
      await grantItem(orgId, { groupId: finance.id, itemType: "workflow", itemId: workflowId });
      expect(await audienceReceivesOutput(orgId, "finance", runId)).toBe(true);
      await revokeItem(orgId, { groupId: everyone, itemType: "watcher", itemId: "*" });
      expect(await audienceReceivesOutput(orgId, "finance", runId)).toBe(false);
      await grantItem(orgId, { groupId: finance.id, itemType: "watcher", itemId: "w-1" });
      expect(await audienceReceivesOutput(orgId, "finance", runId)).toBe(true);
      expect(await audienceReceivesOutput(orgId, "administrators", runId)).toBe(true);
      expect(await audienceReceivesOutput(orgId, "missing-group", runId)).toBe(false);
    });
  });

  it("drops a linked sender who does not hold the channel", async () => {
    await withOrg(async (orgId) => {
      const userId = `${orgId}-ann`;
      await db().insert(app_user).values({ id: userId, org_id: orgId, email: "ann@example.test" });
      await db().insert(channel_identity).values({
        org_id: orgId, channel_plugin: "@open-neko/channel-slack", workspace_id: "T1", channel_user_id: "U1",
        app_user_id: userId, status: "linked",
      });
      const sender = { id: "U1", workspaceId: "T1" };
      expect(await resolveChannelActor(orgId, "@open-neko/channel-slack", sender)).toEqual({ userId, role: "member" });

      const everyone = await builtinGroupId(orgId, "everyone");
      await revokeItem(orgId, { groupId: everyone, itemType: "channel", itemId: "*" });
      expect(await resolveChannelActor(orgId, "@open-neko/channel-slack", sender)).toMatchObject({ userId: null, blocked: true });

      const sales = await createUserGroup(orgId, { name: "Sales" });
      await grantItem(orgId, { groupId: sales.id, itemType: "channel", itemId: "@open-neko/channel-slack" });
      await addLocalGroupMember(orgId, sales.id, userId);
      expect(await resolveChannelActor(orgId, "@open-neko/channel-slack", sender)).toEqual({ userId, role: "member" });
    });
  });
});
