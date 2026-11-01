import { afterEach, beforeEach, describe, expect, it, vi, type MockInstance } from "vitest";
import { NextResponse } from "next/server";
import { callRoute } from "../_helpers/route";

const { mockRequireAdminActor } = vi.hoisted(() => ({
  mockRequireAdminActor: vi.fn(),
}));

vi.mock("@/lib/admin-auth", () => ({
  requireAdminActor: mockRequireAdminActor,
  isDenied: (value: unknown) => value instanceof Response,
}));

let fetchMock: MockInstance;

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

beforeEach(() => {
  mockRequireAdminActor.mockResolvedValue({ userId: "admin-1", role: "admin" });
  fetchMock = vi.spyOn(globalThis, "fetch");
});

afterEach(() => {
  fetchMock.mockRestore();
  vi.clearAllMocks();
});

describe("/api/admin/packs", () => {
  it("bounds ZIP uploads and assigns the authenticated uploader", async () => {
    const { POST } = await import("@/app/api/admin/packs/upload/route");
    const request = (body: BodyInit) => new Request("http://localhost/api/admin/packs/upload", {
      method: "POST", headers: { "content-type": "application/zip", "x-openneko-actor": "forged" }, body,
    });
    mockRequireAdminActor.mockResolvedValueOnce(NextResponse.json({ error: "admin only" }, { status: 403 }));
    expect((await POST(request("ZIP"))).status).toBe(403);
    expect(fetchMock).not.toHaveBeenCalled();
    expect((await POST(request(new Uint8Array(16 * 1024 * 1024 + 1)))).status).toBe(413);
    expect(fetchMock).not.toHaveBeenCalled();
    fetchMock.mockResolvedValue(jsonResponse(200, { packId: "service-health" }));
    expect((await POST(request("ZIP"))).status).toBe(200);
    const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
    expect(new Headers(init.headers).get("x-openneko-actor")).toBe("admin-1");
    expect(Buffer.from(init.body as Uint8Array).toString()).toBe("ZIP");
  });
  it("proxies the embedded pack list", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { packs: [{ id: "magento" }] }));
    const { GET } = await import("@/app/api/admin/packs/route");
    const result = await callRoute(GET);
    expect(result.status).toBe(200);
    expect(result.body).toEqual({ packs: [{ id: "magento" }] });
    expect(String(fetchMock.mock.calls[0]?.[0])).toContain("/admin/packs");
  });

  it("keeps the worker route behind the admin gate", async () => {
    mockRequireAdminActor.mockResolvedValue(
      NextResponse.json({ error: "admin only" }, { status: 403 }),
    );
    const { GET } = await import("@/app/api/admin/packs/route");
    const result = await callRoute(GET);
    expect(result.status).toBe(403);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe("/api/admin/packs/[packId]/[action]", () => {
  it("proxies status reads", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { packId: "magento", status: "installed" }));
    const { GET } = await import("@/app/api/admin/packs/[packId]/[action]/route");
    const result = await callRoute(
      (request) => GET(request, { params: Promise.resolve({ packId: "magento", action: "status" }) }),
    );
    expect(result.status).toBe(200);
    expect(result.body).toMatchObject({ packId: "magento", status: "installed" });
  });

  it("adds a server-owned actor and idempotency key to writes", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { packId: "magento", status: "installed" }));
    const { POST } = await import("@/app/api/admin/packs/[packId]/[action]/route");
    const result = await callRoute(
      (request) => POST(request, { params: Promise.resolve({ packId: "magento", action: "install" }) }),
      {
        method: "POST",
        body: { actorUserId: "forged", inputs: { "database.host": "db" } },
      },
    );
    expect(result.status).toBe(200);
    const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
    const body = JSON.parse(String(init.body)) as Record<string, unknown>;
    expect(body.actorUserId).toBe("admin-1");
    expect(body.idempotencyKey).toEqual(expect.any(String));
  });

  it("uses the same authenticated actor for review and forwards exact approval on install", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { reviewHash: "exact-review" }));
    const { POST } = await import("@/app/api/admin/packs/[packId]/[action]/route");
    for (const action of ["review", "install"]) {
      const result = await callRoute(request => POST(request, { params: Promise.resolve({ packId: "service-health", action }) }), {
        method: "POST", body: { actorUserId: "forged", operation: "install", version: "0.1.0", reviewHash: "exact-review" },
      });
      expect(result.status).toBe(200);
      expect(JSON.parse(String((fetchMock.mock.lastCall?.[1] as RequestInit).body))).toMatchObject({ actorUserId: "admin-1", version: "0.1.0", reviewHash: "exact-review" });
    }
  });

  it("rejects unknown operations before contacting the worker", async () => {
    const { GET } = await import("@/app/api/admin/packs/[packId]/[action]/route");
    const result = await callRoute(
      (request) => GET(request, { params: Promise.resolve({ packId: "magento", action: "erase" }) }),
    );
    expect(result.status).toBe(404);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("rejects malformed JSON as a client error", async () => {
    const { POST } = await import("@/app/api/admin/packs/[packId]/[action]/route");
    const request = new Request("http://localhost/api/admin/packs/magento/install", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: "{broken",
    });
    const response = await POST(request as never, {
      params: Promise.resolve({ packId: "magento", action: "install" }),
    });
    expect(response.status).toBe(400);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("rejects oversized bodies even without a content-length header", async () => {
    const { POST } = await import("@/app/api/admin/packs/[packId]/[action]/route");
    const request = new Request("http://localhost/api/admin/packs/magento/install", {
      method: "POST",
      body: JSON.stringify({ value: "x".repeat(65 * 1024) }),
    });
    request.headers.delete("content-length");
    const response = await POST(request as never, {
      params: Promise.resolve({ packId: "magento", action: "install" }),
    });
    expect(response.status).toBe(413);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe("/api/admin/packs/magento/store-management", () => {
  it("proxies the admin control surface", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { controls: [{ domain: "catalog" }] }));
    const { GET } = await import("@/app/api/admin/packs/magento/store-management/route");
    const result = await callRoute(GET);
    expect(result.status).toBe(200);
    expect(result.body).toEqual({ controls: [{ domain: "catalog" }] });
    expect(String(fetchMock.mock.calls[0]?.[0])).toContain(
      "/admin/packs/magento/store-management",
    );
  });

  it("replaces a forged actor on domain and rule writes", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { controls: [] }));
    const { POST } = await import("@/app/api/admin/packs/magento/store-management/route");
    const result = await callRoute(POST, {
      method: "POST",
      body: { action: "update_domain", domain: "catalog", enabled: true, actorUserId: "forged" },
    });
    expect(result.status).toBe(200);
    const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
    expect(JSON.parse(String(init.body))).toMatchObject({
      action: "update_domain",
      domain: "catalog",
      enabled: true,
      actorUserId: "admin-1",
    });
  });

  it("keeps store controls behind the admin gate", async () => {
    mockRequireAdminActor.mockResolvedValue(
      NextResponse.json({ error: "admin only" }, { status: 403 }),
    );
    const { GET } = await import("@/app/api/admin/packs/magento/store-management/route");
    const result = await callRoute(GET);
    expect(result.status).toBe(403);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
