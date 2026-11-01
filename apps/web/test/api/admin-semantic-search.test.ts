/** /api/admin/semantic-search: admin-only read and save of the switch. */

import { NextResponse } from "next/server";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { callRoute } from "../_helpers/route";

const mocks = vi.hoisted(() => ({
  admin: true,
  get: vi.fn(async () => ({ enabled: false, waiting: 3 })),
  save: vi.fn(async (_orgId: string, enabled: boolean) => ({ enabled, graphjin: { status: "applied" } })),
}));

vi.mock("@/lib/admin-auth", () => ({
  requireAdminActor: async () =>
    mocks.admin ? { role: "admin" } : NextResponse.json({ error: "Admins only" }, { status: 403 }),
  isDenied: (value: unknown) => value instanceof Response,
}));
vi.mock("@/lib/db", () => ({ getOrgId: async () => "org-1" }));
vi.mock("@neko/llm/semantic-search", () => ({
  getSemanticSearchSetting: mocks.get,
  saveSemanticSearchSetting: mocks.save,
}));

import { GET, PUT } from "@/app/api/admin/semantic-search/route";

describe("/api/admin/semantic-search", () => {
  beforeEach(() => {
    mocks.admin = true;
    mocks.get.mockClear();
    mocks.save.mockClear();
  });

  it("reads the switch for admins", async () => {
    const res = await callRoute(GET);
    expect(res).toMatchObject({ status: 200, body: { enabled: false, waiting: 3 } });
  });

  it("saves a boolean and reports how GraphJin took it", async () => {
    const res = await callRoute(PUT, { method: "PUT", body: { enabled: true } });
    expect(res).toMatchObject({ status: 200, body: { enabled: true, graphjin: { status: "applied" } } });
    expect(mocks.save).toHaveBeenCalledWith("org-1", true);
  });

  it("rejects a non-boolean value", async () => {
    const res = await callRoute(PUT, { method: "PUT", body: { enabled: "yes" } });
    expect(res.status).toBe(400);
    expect(mocks.save).not.toHaveBeenCalled();
  });

  it("refuses non-admins", async () => {
    mocks.admin = false;
    expect((await callRoute(GET)).status).toBe(403);
    expect((await callRoute(PUT, { method: "PUT", body: { enabled: true } })).status).toBe(403);
    expect(mocks.save).not.toHaveBeenCalled();
  });
});
