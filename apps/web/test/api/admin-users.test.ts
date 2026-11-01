import { beforeEach, describe, expect, it, vi } from "vitest";

type Row = Record<string, unknown>;

const mocks = vi.hoisted(() => ({
  selectResults: [] as Row[][],
  inserted: [] as Row[],
  updates: [] as Row[],
  actorId: "admin-1" as string | null,
  setAdministrator: vi.fn(async (...args: [string, string, boolean]) => void args),
  administrators: new Set<string>(),
  signInProvider: { providerLabel: "Email link" } as unknown,
  pendingProvider: null as unknown,
  requestWorker: vi.fn(async (path: string, body?: unknown) => ({ status: 200, body: { path, body } })),
}));

vi.mock("@/lib/groups-admin", () => ({ requestWorker: mocks.requestWorker }));

vi.mock("@/lib/auth", () => ({
  getAuthGateStatus: async () => ({ provider: mocks.signInProvider, pending: mocks.pendingProvider }),
}));

vi.mock("@/lib/admin-auth", () => ({
  requireAdminActor: async () => ({ userId: mocks.actorId, role: "admin" }),
  isDenied: () => false,
}));

vi.mock("@/lib/db", () => ({
  getOrgId: async () => "org-1",
}));

vi.mock("@neko/db", () => {
  const chain = () => ({
    from: () => ({
      where: () => ({
        limit: async () => mocks.selectResults.shift() ?? [],
      }),
    }),
  });
  return {
    GroupError: class GroupError extends Error {
      constructor(public readonly code: string, message: string) {
        super(message);
      }
    },
    setLocalAdministrator: mocks.setAdministrator,
    administratorUserIds: async () => mocks.administrators,
    activeAdministratorIds: async () => [...mocks.administrators],
    organization: { solo_admin_user_id: "owner" },
    isUnclaimedSoloEmail: (email: string) => email.endsWith("@solo.openneko.invalid"),
    app_user: {
      id: "id",
      org_id: "org_id",
      email: "email",
      disabled_at: "disabled_at",
      $inferInsert: {},
    },
    db: () => ({
      transaction: async (fn: (tx: unknown) => Promise<unknown>) => fn({
        execute: async () => {}, select: chain,
        update: () => ({ set: (value: Row) => ({ where: async () => { mocks.updates.push(value); } }) }),
        insert: () => ({ values: async (values: Row) => { mocks.inserted.push(values); } }),
      }),
      select: chain,
      insert: () => ({
        values: async (values: Row) => {
          mocks.inserted.push(values);
        },
      }),
      update: () => ({
        set: (patch: Row) => ({
          where: async () => {
            mocks.updates.push(patch);
          },
        }),
      }),
    }),
    and: (...args: unknown[]) => args,
    eq: (...args: unknown[]) => args,
    ne: (...args: unknown[]) => args,
    isNull: (...args: unknown[]) => args,
    sql: () => "sql",
  };
});

import { POST } from "@/app/api/admin/users/route";
import { PATCH } from "@/app/api/admin/users/[userId]/route";

function postRequest(body: unknown): Request {
  return new Request("http://localhost/api/admin/users", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  });
}

function patchRequest(body: unknown): Request {
  return new Request("http://localhost/api/admin/users/usr_target", {
    method: "PATCH",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  });
}

const targetParams = { params: Promise.resolve({ userId: "usr_target" }) };

describe("POST /api/admin/users", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.selectResults = [];
    mocks.inserted = [];
    mocks.updates = [];
    mocks.actorId = "admin-1";
    mocks.administrators = new Set();
    mocks.signInProvider = { providerLabel: "Email link" };
    mocks.pendingProvider = null;
  });

  it("adds the solo owner's email in place without creating another account", async () => {
    mocks.selectResults = [[], [{ owner: "admin-1" }], [{ email: "local@solo.openneko.invalid", sub: null }]];
    const res = await POST(postRequest({ email: "owner@example.com", role: "admin", updateSoloAccount: true }) as never);
    expect(res.status).toBe(200);
    expect((await res.json()).user.id).toBe("admin-1");
    expect(mocks.updates[0]).toMatchObject({ email: "owner@example.com" });
    expect(mocks.inserted).toEqual([]);
    mocks.selectResults = [[], [{ owner: "someone-else" }], [{ email: "local@solo.openneko.invalid", sub: null }]];
    expect((await POST(postRequest({ email: "another@example.com", role: "admin", updateSoloAccount: true }) as never)).status).toBe(409);
  });

  it("provisions a user with a lowercased email and no sub", async () => {
    mocks.selectResults = [[]];
    const res = await POST(
      postRequest({ email: "New.Person@Company.COM", role: "member" }) as never,
    );
    expect(res.status).toBe(201);
    expect(mocks.inserted).toHaveLength(1);
    expect(mocks.inserted[0]).toMatchObject({
      email: "new.person@company.com",
      sub: null,
      org_id: "org-1",
    });
    expect(String(mocks.inserted[0].id)).toMatch(/^usr_/);
  });

  it("creates the user in the directory when asked and reports a directory failure", async () => {
    mocks.selectResults = [[]];
    const res = await POST(postRequest({ email: "Dee@Company.com", name: "Dee", role: "member", addToDirectory: true }) as never);
    expect(res.status).toBe(201);
    expect(mocks.requestWorker).toHaveBeenCalledWith("/admin/directory/users", { email: "dee@company.com", name: "Dee" });
    expect((await res.json()).directoryError).toBeUndefined();

    mocks.selectResults = [[]];
    mocks.requestWorker.mockResolvedValueOnce({ status: 400, body: { error: "Scalekit does not create users" } } as never);
    const failed = await POST(postRequest({ email: "eve@company.com", role: "member", addToDirectory: true }) as never);
    expect(failed.status).toBe(201);
    expect(await failed.json()).toMatchObject({ user: { email: "eve@company.com" }, directoryError: "Scalekit does not create users" });

    mocks.selectResults = [[]];
    await POST(postRequest({ email: "fay@company.com", role: "member" }) as never);
    expect(mocks.requestWorker).toHaveBeenCalledTimes(2);
  });

  it("rejects duplicates with 409", async () => {
    mocks.selectResults = [[{ id: "usr_existing" }]];
    const res = await POST(
      postRequest({ email: "dupe@company.com", role: "member" }) as never,
    );
    expect(res.status).toBe(409);
    expect(mocks.inserted).toHaveLength(0);
  });

  it("refuses to add a person when sign-in is not set up", async () => {
    mocks.signInProvider = null;
    // Nobody can sign in, so the row would promise an account that cannot
    // exist, and the form would collect a real address to do it.
    const res = await POST(postRequest({ email: "visitor@gmail.com", role: "admin" }) as never);
    expect(res.status).toBe(409);
    expect(await res.json()).toMatchObject({ error: expect.stringContaining("set up sign-in") });
    expect(mocks.inserted).toHaveLength(0);
  });

  it("refuses to claim the solo account before a plugin exists", async () => {
    mocks.signInProvider = null;
    const res = await POST(postRequest({ email: "owner@company.com", role: "admin", updateSoloAccount: true }) as never);
    expect(res.status).toBe(409);
    expect(mocks.updates).toHaveLength(0);
  });

  it("claims the solo admin while the installed provider is pending", async () => {
    mocks.signInProvider = null;
    mocks.pendingProvider = { providerLabel: "Email link" };
    mocks.selectResults = [[], [{ owner: "admin-1" }], [{ email: "local@solo.openneko.invalid", sub: null }]];
    const res = await POST(postRequest({ email: "owner@company.com", role: "admin", updateSoloAccount: true }) as never);
    expect(res.status).toBe(200);
    expect(mocks.updates[0]).toMatchObject({ email: "owner@company.com" });
  });

  it("rejects invalid emails and roles", async () => {
    expect(
      (await POST(postRequest({ email: "not-an-email", role: "member" }) as never))
        .status,
    ).toBe(400);
    expect(
      (await POST(postRequest({ email: "a@b.co", role: "owner" }) as never))
        .status,
    ).toBe(400);
    expect(mocks.inserted).toHaveLength(0);
  });
});

describe("PATCH /api/admin/users/[userId]", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.selectResults = [];
    mocks.inserted = [];
    mocks.updates = [];
    mocks.actorId = "admin-1";
    mocks.administrators = new Set();
    mocks.signInProvider = { providerLabel: "Email link" };
    mocks.pendingProvider = null;
  });

  it("refuses to demote the last active admin", async () => {
    mocks.selectResults = [[{ id: "usr_target", disabledAt: null }]];
    mocks.administrators = new Set(["usr_target"]);
    const res = await PATCH(
      patchRequest({ role: "member" }) as never,
      targetParams as never,
    );
    expect(res.status).toBe(409);
    expect(mocks.updates).toHaveLength(0);
  });

  it("refuses to disable the last active admin", async () => {
    mocks.selectResults = [[{ id: "usr_target", disabledAt: null }]];
    mocks.administrators = new Set(["usr_target"]);
    const res = await PATCH(
      patchRequest({ disabled: true }) as never,
      targetParams as never,
    );
    expect(res.status).toBe(409);
    expect(mocks.updates).toHaveLength(0);
  });

  it("demotes an admin when another active admin remains", async () => {
    mocks.selectResults = [[{ id: "usr_target", disabledAt: null }]];
    mocks.administrators = new Set(["usr_target", "usr_other_admin"]);
    const res = await PATCH(
      patchRequest({ role: "member" }) as never,
      targetParams as never,
    );
    expect(res.status).toBe(200);
    expect(mocks.setAdministrator).toHaveBeenCalledWith(expect.any(String), "usr_target", false);
    expect(mocks.updates).toHaveLength(0);
  });

  it("disables and re-enables a member without consulting the admin count", async () => {
    mocks.selectResults = [
      [{ id: "usr_target", disabledAt: null }],
    ];
    const res = await PATCH(
      patchRequest({ disabled: true }) as never,
      targetParams as never,
    );
    expect(res.status).toBe(200);
    expect(mocks.updates[0].disabled_at).toBeInstanceOf(Date);

    mocks.selectResults = [[{ id: "usr_target", disabledAt: new Date() }]];
    const res2 = await PATCH(
      patchRequest({ disabled: false }) as never,
      targetParams as never,
    );
    expect(res2.status).toBe(200);
    expect(mocks.updates[1].disabled_at).toBeNull();
  });

  it("404s on an unknown user and 400s on an empty patch", async () => {
    mocks.selectResults = [[]];
    const res = await PATCH(
      patchRequest({ role: "member" }) as never,
      targetParams as never,
    );
    expect(res.status).toBe(404);

    const res2 = await PATCH(
      patchRequest({}) as never,
      targetParams as never,
    );
    expect(res2.status).toBe(400);
  });
});
