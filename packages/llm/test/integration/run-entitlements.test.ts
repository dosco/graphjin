import { describe, expect, it } from "vitest";
import { createTestOrg, dbReachable, deleteTestOrg, uniqueOrgId } from "@neko/db/test-helpers";
import {
  addLocalGroupMember,
  app_user,
  builtinGroupId,
  createUserGroup,
  db,
  grantItem,
  revokeItem,
  setLocalAdministrator,
} from "@neko/db";
import { inProcessControlPlane } from "../../src/work/control-plane";
import { entitlementActorForRun, filterHeldActions, runHeldItemIds } from "../../src/work/entitlement-scope";
import { createWorkRun, createWorkThread } from "../../src/work/store";
import { saveWorkflow } from "../../src/workflows/store";
import { listWorkMemories, rememberWorkMemory } from "../../src/work/memory";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

async function withOrg(fn: (orgId: string) => Promise<void>) {
  const orgId = uniqueOrgId("run-ent");
  await createTestOrg(orgId);
  try {
    await fn(orgId);
  } finally {
    await deleteTestOrg(orgId);
  }
}

describeIfDb("run entitlements", () => {
  it("limits workflow tools and skills to what the run's user holds", async () => {
    await withOrg(async (orgId) => {
      const everyone = await builtinGroupId(orgId, "everyone");
      await revokeItem(orgId, { groupId: everyone, itemType: "workflow", itemId: "*" });
      await revokeItem(orgId, { groupId: everyone, itemType: "skill", itemId: "*" });
      const finance = await createUserGroup(orgId, { name: "Finance" });
      await db().insert(app_user).values([
        { id: `${orgId}-ann`, org_id: orgId, email: "ann@example.test" },
        { id: `${orgId}-boss`, org_id: orgId, email: "boss@example.test" },
      ]);
      await addLocalGroupMember(orgId, finance.id, `${orgId}-ann`);
      await setLocalAdministrator(orgId, `${orgId}-boss`, true);

      const revenue = (await saveWorkflow({ orgId, name: "Daily revenue check", steps: [] })).workflow;
      const promos = (await saveWorkflow({ orgId, name: "Promotions", steps: [] })).workflow;
      const personal = (await saveWorkflow({ orgId, name: "Mine", steps: [], ownerUserId: `${orgId}-ann` })).workflow;
      await grantItem(orgId, { groupId: finance.id, itemType: "workflow", itemId: revenue.id });
      await grantItem(orgId, { groupId: finance.id, itemType: "skill", itemId: "docx" });

      const thread = await createWorkThread(orgId, "t");
      const annRun = await createWorkRun(orgId, thread.id, "ax", { userId: `${orgId}-ann`, role: "member" });
      const bossRun = await createWorkRun(orgId, thread.id, "ax", { userId: `${orgId}-boss`, role: "admin" });

      const annList = await inProcessControlPlane.listWorkflowsWithTriggers({ orgId, runId: annRun.id });
      expect(annList.workflows.map((w) => w.name).sort()).toEqual(["Daily revenue check", "Mine"]);
      expect((await inProcessControlPlane.listWorkflowsWithTriggers({ orgId, runId: bossRun.id })).total).toBe(3);

      expect(await inProcessControlPlane.deleteWorkflow({ orgId, workflowId: promos.id, runId: annRun.id })).toEqual({ found: false, name: null });
      await expect(
        inProcessControlPlane.saveWorkflowWithTrigger({ orgId, name: "Promotions", steps: [], createdByRunId: annRun.id }),
      ).rejects.toThrow("cannot change it");
      expect((await inProcessControlPlane.deleteWorkflow({ orgId, workflowId: personal.id, runId: annRun.id })).found).toBe(true);

      const annActor = await entitlementActorForRun(orgId, annRun.id);
      expect(await runHeldItemIds(annActor!, "skill")).toEqual(["docx"]);
      expect(await runHeldItemIds((await entitlementActorForRun(orgId, bossRun.id))!, "skill")).toBeUndefined();
      expect(await entitlementActorForRun(orgId, "00000000-0000-0000-0000-000000000000")).toBeNull();
      const channelRun = await createWorkRun(orgId, thread.id, "ax", { userId: null, role: "member" });
      const anonymous = (await entitlementActorForRun(orgId, channelRun.id))!;
      expect(anonymous.kind).toBe("anonymous");
      expect(await runHeldItemIds(anonymous, "skill")).toEqual([]);
      expect((await inProcessControlPlane.listWorkflowsWithTriggers({ orgId, runId: channelRun.id })).total).toBe(0);
      const serviceRun = await createWorkRun(orgId, thread.id, "ax", { userId: null, role: "service" });
      expect((await entitlementActorForRun(orgId, serviceRun.id))!.kind).toBe("service");
    });
  });

  it("refuses external action requests and filters action descriptors the run's user does not hold", async () => {
    await withOrg(async (orgId) => {
      await db().insert(app_user).values({ id: `${orgId}-ann`, org_id: orgId, email: "ann@example.test" });
      const everyone = await builtinGroupId(orgId, "everyone");
      await revokeItem(orgId, { groupId: everyone, itemType: "action", itemId: "*" });
      await grantItem(orgId, { groupId: everyone, itemType: "action", itemId: "send_slack_message" });
      const thread = await createWorkThread(orgId, "t");
      const run = await createWorkRun(orgId, thread.id, "ax", { userId: `${orgId}-ann`, role: "member" });
      const actor = (await entitlementActorForRun(orgId, run.id))!;

      const descriptors = [{ kind: "send_slack_message", description: "" }, { kind: "manage_inventory", description: "" }];
      expect((await filterHeldActions(actor, descriptors)).map((d) => d.kind)).toEqual(["send_slack_message"]);

      await expect(inProcessControlPlane.createActionRequest({
        orgId, workRunId: run.id, scope: "external", kind: "manage_inventory", status: "pending_approval", intent: "x",
      })).rejects.toThrow("not available to this run");
      const allowed = await inProcessControlPlane.createActionRequest({
        orgId, workRunId: run.id, scope: "external", kind: "send_slack_message", status: "pending_approval", intent: "x",
      });
      expect(allowed.id).toBeTruthy();
      const internal = await inProcessControlPlane.createActionRequest({
        orgId, workRunId: run.id, scope: "internal", kind: "memory_write", status: "pending_approval", intent: "x",
      });
      expect(internal.id).toBeTruthy();
    });
  });

  it("refuses saved queries the run's user does not hold", async () => {
    await withOrg(async (orgId) => {
      await db().insert(app_user).values({ id: `${orgId}-ann`, org_id: orgId, role: "member", email: "ann@example.test" });
      const everyone = await builtinGroupId(orgId, "everyone");
      await revokeItem(orgId, { groupId: everyone, itemType: "saved_query", itemId: "*" });
      const thread = await createWorkThread(orgId, "t");
      const run = await createWorkRun(orgId, thread.id, "ax", { userId: `${orgId}-ann`, role: "member" });
      const result = await inProcessControlPlane.callGraphjinTool({
        orgId, runId: run.id, name: "execute_saved_query", arguments: { name: "average_order_value" },
      });
      expect(result).toMatchObject({ isError: true, content: [{ text: 'Saved query "average_order_value" is not available to this run.' }] });

      await revokeItem(orgId, { groupId: everyone, itemType: "data_source", itemId: "*" });
      await expect(inProcessControlPlane.listGraphjinTools({ orgId, runId: run.id })).rejects.toThrow("No data source is available");
      await expect(inProcessControlPlane.queryGraphjinRead({ orgId, runId: run.id, query: "query { x }" })).rejects.toThrow("No data source is available");
      expect(await inProcessControlPlane.askGraphjinDataAgent({ orgId, runId: run.id, instruction: "revenue" })).toMatchObject({ denied: true });
    });
  });

  it("hides team global memories from users without the team memory grant", async () => {
    await withOrg(async (orgId) => {
      await db().insert(app_user).values({ id: `${orgId}-ann`, org_id: orgId, role: "member", email: "ann@example.test" });
      await rememberWorkMemory({ orgId, userId: null, kind: "business_rule", scope: "global", text: "Fiscal year starts in April" });
      await rememberWorkMemory({ orgId, userId: `${orgId}-ann`, kind: "preference", scope: "global", text: "Ann likes tables" });
      const texts = async (teamMemory: "*" | Set<string>) =>
        (await listWorkMemories(orgId, { userId: `${orgId}-ann`, teamMemory })).map((m) => m.text).sort();
      expect(await texts("*")).toEqual(["Ann likes tables", "Fiscal year starts in April"]);
      expect(await texts(new Set(["global"]))).toEqual(["Ann likes tables", "Fiscal year starts in April"]);
      expect(await texts(new Set(["database:erp"]))).toEqual(["Ann likes tables"]);
      await revokeItem(orgId, { groupId: await builtinGroupId(orgId, "everyone"), itemType: "team_memory", itemId: "*" });
      expect(await listWorkMemories(orgId, { userId: null, teamMemory: new Set() })).toEqual([]);
    });
  });
});
