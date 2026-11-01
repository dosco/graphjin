import { afterAll, afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createTestOrg, dbReachable, deleteTestOrg, uniqueOrgId } from "@neko/db/test-helpers";
import { app_user, db, pool, setLocalAdministrator } from "@neko/db";
import { callRoute } from "../_helpers/route";

const mocks = vi.hoisted(() => ({ orgId: "", role: "admin" as "admin" | "member", worker: vi.fn() }));

vi.mock("@/lib/db", async () => ({ ...(await vi.importActual<typeof import("@/lib/db")>("@/lib/db")), getOrgId: async () => mocks.orgId }));
vi.mock("@/lib/admin-auth", async () => {
  const { NextResponse } = await import("next/server");
  return {
    requireAdminActor: async () => (mocks.role === "admin" ? { userId: null, role: "admin" } : NextResponse.json({ error: "admin only" }, { status: 403 })),
    isDenied: (value: unknown) => value instanceof Response,
  };
});
vi.mock("@/lib/groups-admin", async () => ({
  ...(await vi.importActual<typeof import("@/lib/groups-admin")>("@/lib/groups-admin")),
  scheduleGroupGrantsApply: mocks.worker,
}));

const reachable = await dbReachable();

(reachable ? describe : describe.skip)("admin group routes", () => {
  beforeEach(async () => {
    mocks.orgId = uniqueOrgId("admin-groups");
    mocks.role = "admin";
    await createTestOrg(mocks.orgId);
    await db().insert(app_user).values([
      { id: `${mocks.orgId}-owner`, org_id: mocks.orgId, email: "owner@example.test" },
      { id: `${mocks.orgId}-ann`, org_id: mocks.orgId, email: "ann@example.test" },
    ]);
    await setLocalAdministrator(mocks.orgId, `${mocks.orgId}-owner`, true);
  });
  afterEach(async () => {
    await deleteTestOrg(mocks.orgId);
    vi.clearAllMocks();
  });
  afterAll(async () => {
    await pool().end();
  });

  it("creates a group, manages members and grants, and reports effective access", async () => {
    const groups = await import("@/app/api/admin/groups/route");
    const group = await import("@/app/api/admin/groups/[groupId]/route");
    const members = await import("@/app/api/admin/groups/[groupId]/members/route");
    const grants = await import("@/app/api/admin/item-grants/route");
    const access = await import("@/app/api/admin/users/[userId]/access/route");

    const created = await callRoute(groups.POST, { method: "POST", body: { name: "Finance", description: "Money" } });
    expect(created.status).toBe(201);
    const groupId = (created.body as { group: { id: string; slug: string } }).group.id;
    expect((await callRoute(groups.POST, { method: "POST", body: { name: "finance" } })).status).toBe(409);

    const params = { params: Promise.resolve({ groupId }) };
    expect((await callRoute((req) => members.POST(req, params), { method: "POST", body: { userId: `${mocks.orgId}-ann` } })).status).toBe(200);
    expect((await callRoute(grants.POST, { method: "POST", body: { groupId, itemType: "skill", itemId: "docx" } })).body).toEqual({ created: true });
    expect((await callRoute(grants.POST, { method: "POST", body: { groupId, itemType: "data_source", itemId: "shop" } })).status).toBe(200);
    expect(mocks.worker).toHaveBeenCalledTimes(1);

    const detail = await callRoute((req) => group.GET(req, params));
    expect(detail.body).toMatchObject({
      group: { name: "Finance", slug: "finance", memberCount: 1 },
      members: [{ email: "ann@example.test", sources: ["local"] }],
      grants: [{ itemType: "data_source", itemId: "shop" }, { itemType: "skill", itemId: "docx" }],
    });

    const holders = await callRoute(grants.GET, { query: { itemType: "skill", itemId: "docx" } });
    expect((holders.body as { holders: Array<{ name: string }> }).holders.map((h) => h.name)).toEqual(["Administrators", "Everyone", "Finance"]);

    const annAccess = await callRoute((req) => access.GET(req, { params: Promise.resolve({ userId: `${mocks.orgId}-ann` }) }));
    expect(annAccess.body).toMatchObject({ administrator: false, groupSlugs: ["everyone", "finance"] });

    expect((await callRoute(grants.DELETE, { method: "DELETE", body: { groupId, itemType: "skill", itemId: "docx" } })).body).toEqual({ removed: true });
    expect((await callRoute((req) => members.DELETE(req, params), { method: "DELETE", body: { userId: `${mocks.orgId}-ann` } })).body).toEqual({ stillMember: false });
    expect((await callRoute((req) => group.DELETE(req, params), { method: "DELETE" })).status).toBe(200);
  });

  it("protects Administrators and refuses to remove the last administrator", async () => {
    const groups = await import("@/app/api/admin/groups/route");
    const group = await import("@/app/api/admin/groups/[groupId]/route");
    const members = await import("@/app/api/admin/groups/[groupId]/members/route");
    const list = (await callRoute(groups.GET)).body as { groups: Array<{ id: string; slug: string }> };
    const admins = list.groups.find((g) => g.slug === "administrators")!;
    const params = { params: Promise.resolve({ groupId: admins.id }) };
    expect((await callRoute((req) => group.DELETE(req, params), { method: "DELETE" })).status).toBe(400);
    const lockout = await callRoute((req) => members.DELETE(req, params), { method: "DELETE", body: { userId: `${mocks.orgId}-owner` } });
    expect(lockout.status).toBe(409);
    mocks.role = "member";
    expect((await callRoute(groups.GET)).status).toBe(403);
  });

  it("validates data access rules and row filters", async () => {
    const groups = await import("@/app/api/admin/groups/route");
    const rules = await import("@/app/api/admin/data-access/route");
    const ruleRoute = await import("@/app/api/admin/data-access/[ruleId]/route");
    const groupId = ((await callRoute(groups.POST, { method: "POST", body: { name: "Sales" } })).body as { group: { id: string } }).group.id;

    const bad = await callRoute(rules.POST, { method: "POST", body: { groupId, source: "shop", tableName: "orders", columns: ["id"], rowFilter: { column: "id", op: "like", value: 1 } } });
    expect(bad.status).toBe(400);
    expect((bad.body as { error: string }).error).toContain("op must be one of");

    const saved = await callRoute(rules.POST, { method: "POST", body: { groupId, source: "shop", tableName: "orders", columns: ["id", "amount"], rowFilter: { column: "region", op: "eq", value: "emea" } } });
    expect(saved.status).toBe(201);
    const ruleId = (saved.body as { rule: { id: string } }).rule.id;
    expect((await callRoute(rules.GET, { query: { groupId } })).body).toMatchObject({ enabled: false, rules: [{ tableName: "orders", columns: ["id", "amount"] }] });
    expect((await callRoute((req) => ruleRoute.DELETE(req, { params: Promise.resolve({ ruleId }) }), { method: "DELETE" })).status).toBe(200);
    expect(mocks.worker).toHaveBeenCalledTimes(2);
  });
});
