import { describe, expect, it } from "vitest";
import {
  GroupError,
  addLocalGroupMember,
  app_user,
  builtinGroupId,
  createUserGroup,
  db,
  deleteDataAccessRule,
  getGroupGrantsEnabled,
  grantItem,
  graphjinGroupClaims,
  listDataAccessRules,
  loadGroupGrantInputs,
  revokeItem,
  setGroupGrantsEnabled,
  upsertDataAccessRule,
} from "../../src";
import { dbReachable, withTestOrg } from "./_helpers";

const reachable = await dbReachable();

(reachable ? describe : describe.skip)("data access rules", () => {
  it("stores rules, applies them only for held data sources, and builds token claims", async () => {
    await withTestOrg(async (orgId) => {
      const finance = await createUserGroup(orgId, { name: "Finance" });
      const sales = await createUserGroup(orgId, { name: "Sales" });
      const everyone = await builtinGroupId(orgId, "everyone");
      const admins = await builtinGroupId(orgId, "administrators");
      await db().insert(app_user).values({ id: `${orgId}-ann`, org_id: orgId, email: "ann@example.test" });
      await addLocalGroupMember(orgId, finance.id, `${orgId}-ann`);

      expect(await getGroupGrantsEnabled(orgId)).toBe(false);
      await setGroupGrantsEnabled(orgId, true, null);
      expect(await getGroupGrantsEnabled(orgId)).toBe(true);

      const filter = { column: "region", op: "eq", value: "emea" };
      const rule = await upsertDataAccessRule(orgId, { groupId: finance.id, source: "shop", tableName: "orders", columns: ["id", "amount", "id"], rowFilter: filter });
      expect(rule).toMatchObject({ groupSlug: "finance", columns: ["id", "amount"], rowFilter: filter, tableSchema: "" });
      const updated = await upsertDataAccessRule(orgId, { groupId: finance.id, source: "shop", tableName: "orders", columns: ["id"], rowFilter: null });
      expect(updated.id).toBe(rule.id);
      expect(updated).toMatchObject({ columns: ["id"], rowFilter: null });
      await upsertDataAccessRule(orgId, { groupId: sales.id, source: "erp", tableName: "customers", columns: ["name"], rowFilter: null });
      await expect(upsertDataAccessRule(orgId, { groupId: admins, source: "shop", tableName: "orders", columns: ["id"], rowFilter: null })).rejects.toBeInstanceOf(GroupError);
      await expect(upsertDataAccessRule(orgId, { groupId: sales.id, source: "shop", tableName: "orders", columns: [" "], rowFilter: null })).rejects.toThrow("at least one column");

      await revokeItem(orgId, { groupId: everyone, itemType: "data_source", itemId: "*" });
      await grantItem(orgId, { groupId: finance.id, itemType: "data_source", itemId: "shop" });
      await grantItem(orgId, { groupId: sales.id, itemType: "api_operation", itemId: "payments:stripe:refund" });
      const inputs = await loadGroupGrantInputs(orgId, ["payments:stripe:refund", "payments:stripe:list"]);
      expect(inputs.rules.map((r) => `${r.groupSlug}:${r.source}.${r.tableName}`)).toEqual(["finance:shop.orders"]);
      expect([...inputs.apiOperationHolders]).toEqual([["payments:stripe:refund", ["sales"]]]);
      expect(inputs.groups.map((g) => g.slug)).toEqual(["everyone", "finance", "sales"]);

      expect(await graphjinGroupClaims(orgId, `${orgId}-ann`)).toEqual({ roles: ["og_finance"], groups: ["everyone", "finance"] });
      expect(await deleteDataAccessRule(orgId, rule.id)).toBe(true);
      expect(await graphjinGroupClaims(orgId, `${orgId}-ann`)).toEqual({ roles: [], groups: ["everyone", "finance"] });
      expect((await listDataAccessRules(orgId)).map((r) => r.groupName)).toEqual(["Sales"]);
    }, "data-access");
  });
});
