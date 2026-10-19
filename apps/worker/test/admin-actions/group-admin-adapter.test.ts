import { describe, expect, it, vi } from "vitest";
import { app_user, db, eq, listDataAccessRules, listGroupItemGrants, listGroupMembers, listUserGroups, organization, pool } from "@neko/db";

const captured = vi.hoisted(() => ({ adapters: new Map<string, (input: { request: Record<string, unknown> }) => Promise<unknown>>() }));
vi.mock("@neko/llm/workflows", async (original) => ({
  ...(await original<Record<string, unknown>>()),
  registerActionAdapter: (kind: string, adapter: (input: { request: Record<string, unknown> }) => Promise<unknown>) => captured.adapters.set(kind, adapter),
}));
import { registerGroupAdminAdapter } from "../../src/admin-actions/adapters";

const reachable = await pool().query("select 1").then(() => true, () => false);

(reachable ? describe : describe.skip)("group_admin adapter", () => {
  it("applies approved group, member, grant and data access changes", async () => {
    const orgId = `group-admin-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`;
    await db().insert(organization).values({ id: orgId, name: "Group admin" });
    await db().insert(app_user).values({ id: `${orgId}-ann`, org_id: orgId, email: "ann@example.test" });
    const onGraphjin = vi.fn();
    registerGroupAdminAdapter(onGraphjin);
    const run = (payload: Record<string, unknown>) =>
      captured.adapters.get("group_admin")!({ request: { id: "ar-1", orgId, actorUserId: null, payload } }) as Promise<{ result: Record<string, unknown> }>;
    try {
      const created = await run({ action: "create_group", name: "Finance" });
      const groupId = String(created.result.groupId);
      await run({ action: "add_member", groupId, userId: `${orgId}-ann` });
      await run({ action: "grant_item", groupId, itemType: "skill", itemId: "docx" });
      await run({ action: "grant_item", groupId, itemType: "data_source", itemId: "shop" });
      const rule = await run({
        action: "set_table_access", groupId, source: "shop", table: "public.orders", columns: ["id", "amount"],
        rowFilter: { column: "region", op: "eq", value: "emea" },
      });

      expect((await listGroupMembers(orgId, groupId)).map((m) => m.userId)).toEqual([`${orgId}-ann`]);
      expect(await listGroupItemGrants(orgId, groupId)).toMatchObject([{ itemType: "data_source" }, { itemType: "skill" }]);
      expect(await listDataAccessRules(orgId, groupId)).toMatchObject([{ tableSchema: "public", tableName: "orders", columns: ["id", "amount"] }]);
      expect(onGraphjin).toHaveBeenCalledTimes(2);

      await expect(run({ action: "set_table_access", groupId, source: "shop", table: "orders", columns: ["id"], rowFilter: { column: "id", op: "like", value: 1 } })).rejects.toThrow("op must be one of");
      await expect(run({ action: "grant_item", groupId, itemType: "nothing", itemId: "x" })).rejects.toThrow("unknown item type");
      await run({ action: "remove_table_access", ruleId: rule.result.ruleId });
      await run({ action: "delete_group", groupId });
      expect((await listUserGroups(orgId)).map((g) => g.slug)).toEqual(["administrators", "everyone"]);
    } finally {
      await db().delete(organization).where(eq(organization.id, orgId));
    }
  });
});
