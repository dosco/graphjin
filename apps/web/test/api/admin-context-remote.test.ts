import { afterAll, afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createTestOrg, dbReachable, deleteTestOrg, uniqueOrgId } from "@neko/db/test-helpers";
import { pool } from "@neko/db";
import { callRoute } from "../_helpers/route";

const mocks = vi.hoisted(() => ({ orgId: "", role: "admin" as "admin" | "member" }));

vi.mock("@/lib/db", async () => ({ ...(await vi.importActual<typeof import("@/lib/db")>("@/lib/db")), getOrgId: async () => mocks.orgId }));
vi.mock("@/lib/admin-auth", async () => {
  const { NextResponse } = await import("next/server");
  return {
    requireAdminActor: async () => (mocks.role === "admin" ? { userId: null, role: "admin" } : NextResponse.json({ error: "admin only" }, { status: 403 })),
    isDenied: (value: unknown) => value instanceof Response,
  };
});

const reachable = await dbReachable();

(reachable ? describe : describe.skip)("admin context remote routes", () => {
  beforeEach(async () => {
    mocks.orgId = uniqueOrgId("context-remote");
    mocks.role = "admin";
    await createTestOrg(mocks.orgId);
  });
  afterEach(async () => {
    await deleteTestOrg(mocks.orgId);
    vi.clearAllMocks();
  });
  afterAll(async () => {
    await pool().end();
  });

  it("saves, reads and removes the remote without returning the token", async () => {
    const route = await import("@/app/api/admin/context-remote/route");

    expect((await callRoute(route.GET)).body).toEqual({ remote: null });

    const saved = await callRoute(route.PUT, {
      method: "PUT",
      body: { url: "https://gitlab.com/acme/context.git", mode: "push", branch: "main", kinds: ["skills"], token: "glpat-secret" },
    });
    expect(saved.status).toBe(200);
    expect(saved.body).toMatchObject({ remote: { provider: "gitlab", mode: "push", hasToken: true } });
    expect(JSON.stringify(saved.body)).not.toContain("glpat-secret");

    const invalid = await callRoute(route.PUT, { method: "PUT", body: { url: "http://gitlab.com/a/b.git", mode: "push", branch: "main", kinds: ["skills"] } });
    expect(invalid).toMatchObject({ status: 400, body: { error: "The repository address must start with https:// or ssh://, or have the form git@host:org/repo.git." } });

    expect((await callRoute(route.DELETE, { method: "DELETE" })).body).toEqual({ remote: null });
  });

  it("refuses members", async () => {
    mocks.role = "member";
    const route = await import("@/app/api/admin/context-remote/route");
    const publish = await import("@/app/api/admin/context-remote/publish/route");
    const updates = await import("@/app/api/admin/context-remote/updates/route");
    const hostKey = await import("@/app/api/admin/context-remote/host-key/route");
    expect((await callRoute(route.GET)).status).toBe(403);
    expect((await callRoute(hostKey.POST, { method: "POST" })).status).toBe(403);
    expect((await callRoute(updates.GET)).status).toBe(403);
    expect((await callRoute(updates.POST, { method: "POST", body: { skills: [], packs: [] } })).status).toBe(403);
    expect((await callRoute(publish.POST, { method: "POST" })).status).toBe(403);
    expect((await callRoute(publish.GET)).status).toBe(403);
  });

  it("reports a publish or update check without a remote", async () => {
    const publish = await import("@/app/api/admin/context-remote/publish/route");
    const updates = await import("@/app/api/admin/context-remote/updates/route");
    expect(await callRoute(updates.GET)).toMatchObject({ status: 400, body: { error: "Connect a remote repository first." } });
    expect(await callRoute(publish.GET)).toMatchObject({ status: 400, body: { error: "Connect a remote repository first." } });
    expect(await callRoute(publish.POST, { method: "POST" })).toMatchObject({
      status: 400,
      body: { error: "Connect a remote repository first." },
    });
  });
});
