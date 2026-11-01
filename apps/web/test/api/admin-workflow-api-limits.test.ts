import { afterAll, afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createTestOrg, dbReachable, deleteTestOrg, uniqueOrgId } from "@neko/db/test-helpers";
import { pool } from "@neko/db";
import { callRoute } from "../_helpers/route";

const mocks = vi.hoisted(() => ({ orgId: "", admin: true }));
vi.mock("@/lib/db", async () => ({ ...(await vi.importActual<typeof import("@/lib/db")>("@/lib/db")), getOrgId: async () => mocks.orgId }));
vi.mock("@/lib/admin-auth", async () => {
  const { NextResponse } = await import("next/server");
  return {
    requireAdminActor: async () => mocks.admin ? { userId: null, role: "admin" } : NextResponse.json({ error: "admin only" }, { status: 403 }),
    isDenied: (value: unknown) => value instanceof Response,
  };
});

const reachable = await dbReachable();
(reachable ? describe : describe.skip)("admin workflow API limits", () => {
  beforeEach(async () => {
    mocks.orgId = uniqueOrgId("workflow-api-limits");
    mocks.admin = true;
    await createTestOrg(mocks.orgId);
  });
  afterEach(async () => {
    await deleteTestOrg(mocks.orgId);
  });
  afterAll(async () => { await pool().end(); });

  it("validates and persists the organization budget for admins", async () => {
    const route = await import("@/app/api/admin/workflow-api-limits/route");
    const invalid = await callRoute(route.PUT, { method: "PUT", body: { rollingTokenBudget: 0, rollingCostMicrosBudget: 50_000_000 } });
    expect(invalid.status).toBe(400);
    const saved = await callRoute(route.PUT, { method: "PUT", body: { rollingTokenBudget: 10_000_000, rollingCostMicrosBudget: 200_000_000 } });
    expect(saved).toMatchObject({ status: 200, body: { limits: { rollingTokenBudget: 10_000_000, rollingCostMicrosBudget: 200_000_000 } } });
    expect((await callRoute(route.GET)).body).toEqual(saved.body);
    mocks.admin = false;
    expect((await callRoute(route.GET)).status).toBe(403);
  });
});
