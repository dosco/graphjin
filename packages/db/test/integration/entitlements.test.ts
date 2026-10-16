import { describe, expect, it } from "vitest";
import {
  ITEM_TYPES,
  setLocalAdministrator,
  addLocalGroupMember,
  app_user,
  createUserGroup,
  db,
  effectiveAccess,
  eq,
  filterHeld,
  GroupError,
  grantItem,
  grantItems,
  heldItems,
  holds,
  builtinGroupId,
  listGroupItemGrants,
  revokeItem,
  whoHolds,
} from "../../src";
import { createTestOrg, dbReachable, deleteTestOrg, uniqueOrgId, withTestOrg } from "./_helpers";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

async function user(orgId: string, id: string, role = "member") {
  await db().insert(app_user).values({ id: `${orgId}-${id}`, org_id: orgId, email: `${id}@example.test` });
  if (role === "admin") await setLocalAdministrator(orgId, `${orgId}-${id}`, true);
  return { orgId, kind: "user" as const, userId: `${orgId}-${id}` };
}

describeIfDb("entitlements", () => {
  it("does not reuse a cached grant index after an org id is deleted and recreated", async () => {
    const orgId = uniqueOrgId("recreate");
    const grantDataSource = async () => {
      await createTestOrg(orgId, "Recreated");
      await revokeItem(orgId, { groupId: await builtinGroupId(orgId, "everyone"), itemType: "data_source", itemId: "*" });
      const actor = await user(orgId, "m");
      const group = await createUserGroup(orgId, { name: "Readers" });
      await addLocalGroupMember(orgId, group.id, actor.userId);
      await grantItem(orgId, { groupId: group.id, itemType: "data_source", itemId: "src-1" });
      return actor;
    };
    try {
      const first = await grantDataSource();
      expect(await heldItems(first, "data_source")).toContain("src-1");
      await deleteTestOrg(orgId);
      const second = await grantDataSource();
      expect(await heldItems(second, "data_source")).toContain("src-1");
    } finally {
      await deleteTestOrg(orgId);
    }
  });

  it("grants multiple selected items once and audits only new grants", async () => {
    await withTestOrg(async (orgId) => {
      const group = await createUserGroup(orgId, { name: "Metric readers" });
      expect(await grantItems(orgId, {
        groupId: group.id, itemType: "metric", itemIds: ["alpha", "beta", "alpha"],
      })).toEqual({ created: 2 });
      expect(await grantItems(orgId, {
        groupId: group.id, itemType: "metric", itemIds: ["beta", "gamma"],
      })).toEqual({ created: 1 });
      expect((await listGroupItemGrants(orgId, group.id))
        .filter((grant) => grant.itemType === "metric")
        .map((grant) => grant.itemId).sort()).toEqual(["alpha", "beta", "gamma"]);
      const audit = await db().execute<{ item_id: string }>(
        `select item_id from item_grant_audit where org_id = '${orgId}' and item_type = 'metric'`,
      );
      expect(audit.rows.map((row) => row.item_id).sort()).toEqual(["alpha", "beta", "gamma"]);
      await expect(grantItems(orgId, {
        groupId: group.id, itemType: "metric", itemIds: ["valid", ""],
      })).rejects.toBeInstanceOf(GroupError);
      expect((await listGroupItemGrants(orgId, group.id)).some((grant) => grant.itemId === "valid")).toBe(false);
    }, "ent-bulk");
  });

  it("gives Everyone every item type by default so no user loses access", async () => {
    await withTestOrg(async (orgId) => {
      const everyone = await builtinGroupId(orgId, "everyone");
      const grants = await listGroupItemGrants(orgId, everyone);
      expect(grants.map((g) => g.itemType).sort()).toEqual(ITEM_TYPES.filter((t) => t !== "api_operation").sort());
      expect(new Set(grants.map((g) => g.itemId))).toEqual(new Set(["*"]));
      const ann = await user(orgId, "ann");
      expect(await holds(ann, "skill", "docx")).toMatchObject({ allowed: true, via: [everyone] });
      expect(await heldItems(ann, "workflow")).toBe("*");
    }, "ent-parity");
  });

  it("narrows access to granted items, unions groups, and applies revocation at the next check", async () => {
    await withTestOrg(async (orgId) => {
      const everyone = await builtinGroupId(orgId, "everyone");
      await revokeItem(orgId, { groupId: everyone, itemType: "skill", itemId: "*" });
      const finance = await createUserGroup(orgId, { name: "Finance" });
      const sales = await createUserGroup(orgId, { name: "Sales" });
      await grantItem(orgId, { groupId: finance.id, itemType: "skill", itemId: "docx", actorUserId: null });
      await grantItem(orgId, { groupId: sales.id, itemType: "skill", itemId: "magento-run-promotions" });
      expect(await grantItem(orgId, { groupId: sales.id, itemType: "skill", itemId: "magento-run-promotions" })).toEqual({ created: false });

      const ann = await user(orgId, "ann");
      const boss = await user(orgId, "boss", "admin");
      await addLocalGroupMember(orgId, finance.id, ann.userId);
      expect(await holds(ann, "skill", "docx")).toMatchObject({ allowed: true, via: [finance.id] });
      expect((await holds(ann, "skill", "magento-run-promotions")).allowed).toBe(false);
      const held = await heldItems(ann, "skill");
      expect(held).toEqual(new Set(["docx"]));
      expect(filterHeld(held, ["docx", "pptx"], (s) => s)).toEqual(["docx"]);

      await addLocalGroupMember(orgId, sales.id, ann.userId);
      expect(await heldItems(ann, "skill")).toEqual(new Set(["docx", "magento-run-promotions"]));
      await revokeItem(orgId, { groupId: finance.id, itemType: "skill", itemId: "docx" });
      expect((await holds(ann, "skill", "docx")).allowed).toBe(false);

      expect(await holds(boss, "skill", "anything")).toMatchObject({ allowed: true, via: ["administrators"] });
      expect(await heldItems(boss, "skill")).toBe("*");
      await expect(grantItem(orgId, { groupId: await builtinGroupId(orgId, "administrators"), itemType: "skill", itemId: "x" }))
        .rejects.toBeInstanceOf(GroupError);
      await expect(grantItem(orgId, { groupId: sales.id, itemType: "dashboards" as never, itemId: "x" })).rejects.toThrow("unknown item type");

      const audit = await db().execute<{ action: string; item_id: string }>(
        `select action, item_id from item_grant_audit where org_id = '${orgId}' and item_type = 'skill' order by id`,
      );
      expect(audit.rows).toEqual([
        { action: "revoke", item_id: "*" },
        { action: "grant", item_id: "docx" },
        { action: "grant", item_id: "magento-run-promotions" },
        { action: "revoke", item_id: "docx" },
      ]);
    }, "ent-narrow");
  });

  it("lets a collection grant cover its concepts and reports holders and effective access", async () => {
    await withTestOrg(async (orgId) => {
      const everyone = await builtinGroupId(orgId, "everyone");
      await revokeItem(orgId, { groupId: everyone, itemType: "library_collection", itemId: "*" });
      await revokeItem(orgId, { groupId: everyone, itemType: "library_concept", itemId: "*" });
      const finance = await createUserGroup(orgId, { name: "Finance" });
      await grantItem(orgId, { groupId: finance.id, itemType: "library_collection", itemId: "revenue/" });
      const ann = await user(orgId, "ann");
      await addLocalGroupMember(orgId, finance.id, ann.userId);

      const concept = (path: string) => [{ type: "library_collection" as const, id: path.slice(0, path.lastIndexOf("/") + 1) }];
      expect((await holds(ann, "library_concept", "c1", { parents: concept("revenue/net") })).allowed).toBe(true);
      expect((await holds(ann, "library_concept", "c2", { parents: concept("policies/refunds") })).allowed).toBe(false);

      expect((await whoHolds(orgId, "library_collection", "revenue/")).map((h) => `${h.name}:${h.via}`)).toEqual([
        "Administrators:administrators",
        "Finance:item",
      ]);
      expect((await whoHolds(orgId, "skill", "docx")).map((h) => `${h.name}:${h.via}`)).toEqual([
        "Administrators:administrators",
        "Everyone:all",
      ]);
      const access = await effectiveAccess(orgId, ann.userId);
      expect(access.administrator).toBe(false);
      expect(access.items.find((i) => i.itemType === "library_collection")).toEqual({
        itemType: "library_collection", itemId: "revenue/", groups: [{ groupId: finance.id, name: "Finance" }],
      });
    }, "ent-library");
  });

  it("expands a pack grant to the items the pack installs, including later additions", async () => {
    await withTestOrg(async (orgId) => {
      const everyone = await builtinGroupId(orgId, "everyone");
      for (const type of ["skill", "workflow", "pack"] as const) await revokeItem(orgId, { groupId: everyone, itemType: type, itemId: "*" });
      const ops = await createUserGroup(orgId, { name: "Ops" });
      await grantItem(orgId, { groupId: ops.id, itemType: "pack", itemId: "magento" });
      const ann = await user(orgId, "ann");
      await addLocalGroupMember(orgId, ops.id, ann.userId);

      const install = await db().execute<{ id: string }>(
        `insert into pack_install (org_id, pack_id, version, status, manifest_hash) values ('${orgId}', 'magento', '1.0.0', 'installed', 'h') returning id`,
      );
      const installId = install.rows[0]!.id;
      const artifact = (kind: string, ref: string) => db().execute(
        `insert into pack_artifact (pack_install_id, org_id, artifact_kind, artifact_key, target_ref, desired_hash, last_applied_hash)
         values ('${installId}', '${orgId}', '${kind}', '${kind}:${ref}', '${ref}', 'h', 'h')`,
      );
      await artifact("skill", "magento-check-inventory");
      const wf = await db().execute<{ id: string }>(
        `insert into workflow_definition (org_id, name) values ('${orgId}', 'Daily revenue check') returning id`,
      );
      await artifact("workflow", "Daily revenue check");

      expect(await heldItems(ann, "skill")).toEqual(new Set(["magento-check-inventory"]));
      expect(await heldItems(ann, "workflow")).toEqual(new Set([wf.rows[0]!.id]));
      expect(await holds(ann, "workflow", wf.rows[0]!.id)).toMatchObject({ allowed: true, via: [ops.id] });
      expect((await holds(ann, "skill", "docx")).allowed).toBe(false);

      await artifact("skill", "magento-run-promotions");
      expect((await holds(ann, "skill", "magento-run-promotions")).allowed).toBe(true);
      expect((await whoHolds(orgId, "skill", "magento-run-promotions")).map((h) => h.via)).toEqual(["administrators", "pack:magento"]);

      const service = { orgId, kind: "service" as const, packId: "magento" };
      expect((await holds(service, "skill", "magento-check-inventory")).allowed).toBe(true);
      expect((await holds(service, "skill", "docx")).allowed).toBe(false);
      expect((await holds({ orgId, kind: "service" }, "skill", "docx")).allowed).toBe(true);

      await db().execute(`update pack_install set status = 'removed' where id = '${installId}'`);
      expect((await holds(ann, "skill", "magento-check-inventory")).allowed).toBe(false);
    }, "ent-pack");
  });

  it("stops holding items when a user is disabled", async () => {
    await withTestOrg(async (orgId) => {
      const ann = await user(orgId, "ann");
      expect((await holds(ann, "skill", "docx")).allowed).toBe(true);
      await db().update(app_user).set({ disabled_at: new Date() }).where(eq(app_user.id, ann.userId));
      expect((await holds(ann, "skill", "docx")).allowed).toBe(false);
    }, "ent-disabled");
  });
});
