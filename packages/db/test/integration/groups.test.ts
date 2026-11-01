import { describe, expect, it } from "vitest";
import {
  administratorUserIds,
  GroupError,
  addLocalGroupMember,
  app_user,
  builtinGroupId,
  createIdpGroupRule,
  createUserGroup,
  db,
  deleteIdpGroupRule,
  deleteUserGroup,
  eq,
  listGroupMembers,
  listIdpGroupRules,
  listUserGroups,
  reconcileDirectorySnapshot,
  reconcileSignInGroups,
  removeLocalGroupMember,
  resolveUserGroups,
  setLocalAdministrator,
  updateUserGroup,
} from "../../src";
import { dbReachable, withTestOrg } from "./_helpers";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

async function addUser(orgId: string, id: string, role = "member", extra: Partial<typeof app_user.$inferInsert> = {}) {
  await db().insert(app_user).values({ id: `${orgId}-${id}`, org_id: orgId, email: `${id}@example.test`, ...extra });
  // Administrators membership records who administers (migration 0079).
  if (role === "admin") await setLocalAdministrator(orgId, `${orgId}-${id}`, true);
  return `${orgId}-${id}`;
}

async function expectGroupError(promise: Promise<unknown>, code: string) {
  await expect(promise).rejects.toSatisfy((err: unknown) => err instanceof GroupError && err.code === code);
}

describeIfDb("groups", () => {
  it("creates, renames and deletes custom groups and protects built-in groups", async () => {
    await withTestOrg(async (orgId) => {
      const finance = await createUserGroup(orgId, { name: "Finance Team", description: "Money" });
      expect(finance).toMatchObject({ slug: "finance-team", kind: "custom", memberCount: 0 });
      await expectGroupError(createUserGroup(orgId, { name: "finance team" }), "conflict");
      await expectGroupError(createUserGroup(orgId, { name: "Administrators!" }), "conflict");
      const other = await createUserGroup(orgId, { name: "Everyone else" });
      expect(other.slug).toBe("everyone-else");

      expect((await updateUserGroup(orgId, finance.id, { name: "Finance", description: null }))).toMatchObject({
        name: "Finance", slug: "finance-team", description: null,
      });
      const adminId = await builtinGroupId(orgId, "administrators");
      await expectGroupError(updateUserGroup(orgId, adminId, { name: "Admins" }), "builtin");
      await expectGroupError(deleteUserGroup(orgId, adminId), "builtin");
      await deleteUserGroup(orgId, finance.id);
      expect((await listUserGroups(orgId)).map((g) => g.slug)).toEqual(["administrators", "everyone", "everyone-else"]);
    }, "groups-crud");
  });

  it("adds local members, keeps an administrator, and treats Everyone as implicit", async () => {
    await withTestOrg(async (orgId) => {
      const owner = await addUser(orgId, "owner", "admin");
      const ann = await addUser(orgId, "ann");
      await addUser(orgId, "gone", "member", { disabled_at: new Date() });
      const admins = await builtinGroupId(orgId, "administrators");
      const everyone = await builtinGroupId(orgId, "everyone");
      const sales = await createUserGroup(orgId, { name: "Sales" });

      await addLocalGroupMember(orgId, sales.id, ann);
      await addLocalGroupMember(orgId, sales.id, ann);
      expect(await listGroupMembers(orgId, sales.id)).toEqual([
        { userId: ann, email: "ann@example.test", name: null, disabled: false, sources: ["local"] },
      ]);
      expect((await listGroupMembers(orgId, everyone)).map((m) => m.userId)).toEqual([ann, owner]);
      await expectGroupError(addLocalGroupMember(orgId, everyone, ann), "implicit");

      await expectGroupError(removeLocalGroupMember(orgId, admins, owner), "lockout");
      await expectGroupError(setLocalAdministrator(orgId, owner, false), "lockout");
      await setLocalAdministrator(orgId, ann, true);
      expect(await administratorUserIds(orgId)).toContain(ann);
      await setLocalAdministrator(orgId, owner, false);
      expect((await resolveUserGroups(orgId, owner)).administrator).toBe(false);
      expect(await resolveUserGroups(orgId, ann)).toMatchObject({
        administrator: true,
        slugs: ["administrators", "everyone", "sales"],
      });
    }, "groups-members");
  });

  it("maps IdP groups to OpenNeko groups through rules at sign-in and on rule changes", async () => {
    await withTestOrg(async (orgId) => {
      await addUser(orgId, "owner", "admin");
      const bea = await addUser(orgId, "bea", "member", { sub: "idp-bea" });
      const finance = await createUserGroup(orgId, { name: "Finance" });
      const admins = await builtinGroupId(orgId, "administrators");

      await reconcileSignInGroups({
        orgId, userId: bea, provider: "scalekit", tenantId: "t1",
        groups: [{ externalId: "g-fin", displayName: "Finance EU" }, { externalId: "g-it", displayName: "IT Admins" }],
      });
      expect((await resolveUserGroups(orgId, bea)).slugs).toEqual(["everyone"]);

      const idp = await db().execute<{ id: string; external_id: string }>(
        `select id, external_id from sso_group where org_id = '${orgId}' order by external_id`,
      );
      const [fin, it] = idp.rows;
      const rule = await createIdpGroupRule(orgId, { ssoGroupId: fin!.id, userGroupId: finance.id });
      await createIdpGroupRule(orgId, { ssoGroupId: it!.id, userGroupId: admins });
      expect(await resolveUserGroups(orgId, bea)).toMatchObject({ administrator: true, slugs: ["administrators", "everyone", "finance"] });
      expect((await resolveUserGroups(orgId, bea)).ssoGroupIds.sort()).toEqual([fin!.id, it!.id].sort());
      expect(await listGroupMembers(orgId, finance.id)).toEqual([
        expect.objectContaining({ userId: bea, sources: [`rule:${rule.id}`] }),
      ]);
      expect((await listIdpGroupRules(orgId)).map((r) => `${r.idpGroupName}->${r.userGroupName}`)).toEqual([
        "IT Admins->Administrators", "Finance EU->Finance",
      ]);
      await expectGroupError(setLocalAdministrator(orgId, bea, false), "conflict");

      await reconcileSignInGroups({ orgId, userId: bea, provider: "scalekit", tenantId: "t1", groups: [{ externalId: "g-it", displayName: "IT Admins" }] });
      expect((await resolveUserGroups(orgId, bea)).slugs).toEqual(["administrators", "everyone"]);

      await deleteIdpGroupRule(orgId, rule.id);
      await reconcileSignInGroups({ orgId, userId: bea, provider: "scalekit", tenantId: "t1", groups: [{ externalId: "g-fin", displayName: "Finance EU" }] });
      expect((await resolveUserGroups(orgId, bea)).slugs).toEqual(["everyone"]);
      expect(await administratorUserIds(orgId)).not.toContain(bea);
    }, "groups-rules");
  });

  it("refuses a sign-in sync that would remove the last administrator", async () => {
    await withTestOrg(async (orgId) => {
      const cal = await addUser(orgId, "cal", "member", { sub: "idp-cal" });
      await reconcileSignInGroups({ orgId, userId: cal, provider: "oidc", tenantId: "t", groups: [{ externalId: "admins", displayName: "admins" }] });
      const [idp] = (await db().execute<{ id: string }>(`select id from sso_group where org_id = '${orgId}'`)).rows;
      await createIdpGroupRule(orgId, { ssoGroupId: idp!.id, userGroupId: await builtinGroupId(orgId, "administrators") });
      expect((await resolveUserGroups(orgId, cal)).administrator).toBe(true);
      await expectGroupError(
        reconcileSignInGroups({ orgId, userId: cal, provider: "oidc", tenantId: "t", groups: [] }),
        "lockout",
      );
      expect((await resolveUserGroups(orgId, cal)).administrator).toBe(true);
    }, "groups-lockout");
  });

  it("syncs a directory snapshot without disabling local or solo users", async () => {
    await withTestOrg(async (orgId) => {
      const owner = await addUser(orgId, "owner", "admin", { email: "owner@acme.test" });
      const old = await addUser(orgId, "old", "member", { email: "old@acme.test", source: "scalekit" });
      const sales = await createUserGroup(orgId, { name: "Sales" });

      const snapshot = {
        orgId, provider: "scalekit", tenantId: "acme", createUsers: true,
        users: [
          { externalId: "u-owner", email: "OWNER@acme.test", active: true },
          { externalId: "u-dan", email: "dan@acme.test", name: "Dan", active: true },
          { externalId: "u-eve", email: "eve@acme.test", active: false },
        ],
        groups: [{ externalId: "g-sales", displayName: "Sales" }],
        memberships: [
          { userExternalId: "u-dan", groupExternalId: "g-sales" },
          { userExternalId: "u-owner", groupExternalId: "g-sales" },
        ],
      };
      const stats = await reconcileDirectorySnapshot(snapshot);
      expect(stats).toMatchObject({ usersCreated: 1, usersMatched: 1, usersDisabled: 1, memberships: 2, groups: 1 });
      const users = await db().select().from(app_user).where(eq(app_user.org_id, orgId));
      const dan = users.find((u) => u.email === "dan@acme.test")!;
      expect(dan).toMatchObject({ source: "scalekit", disabled_at: null });
      expect(users.find((u) => u.id === old)!.disabled_at).not.toBeNull();
      expect(users.find((u) => u.id === owner)!.disabled_at).toBeNull();
      expect(users.some((u) => u.email === "eve@acme.test")).toBe(false);

      const [idp] = (await db().execute<{ id: string }>(`select id from sso_group where org_id = '${orgId}'`)).rows;
      await createIdpGroupRule(orgId, { ssoGroupId: idp!.id, userGroupId: sales.id });
      expect((await listGroupMembers(orgId, sales.id)).map((m) => m.userId).sort()).toEqual([dan.id, owner].sort());

      await reconcileDirectorySnapshot({ ...snapshot, groups: [], memberships: [], users: [snapshot.users[0]!] });
      expect(await listGroupMembers(orgId, sales.id)).toEqual([]);
      const after = await db().select().from(app_user).where(eq(app_user.org_id, orgId));
      expect(after.find((u) => u.id === dan.id)!.disabled_at).not.toBeNull();
      expect(after.find((u) => u.id === owner)).toMatchObject({ disabled_at: null });
    }, "groups-directory");
  });
});
