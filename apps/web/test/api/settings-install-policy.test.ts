/**
 * /api/settings/install-policy contract tests.
 *
 * Covers: persisted solo admin => 200, non-admin => 403,
 * admin => 200 read + write, partial PATCH semantics.
 */

import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import { app_user, setLocalAdministrator, getOrCreateSoloAdmin, db, pool } from "@neko/db";
import { callRoute } from "../_helpers/route";

const { mockGetOrgId, mockGetCurrentUser } = vi.hoisted(() => ({
  mockGetOrgId: vi.fn(),
  mockGetCurrentUser: vi.fn(),
}));

vi.mock("@/lib/db", async () => {
  const actual = await vi.importActual<typeof import("@/lib/db")>("@/lib/db");
  return { ...actual, getOrgId: mockGetOrgId };
});

vi.mock("@/lib/auth", async () => {
  const actual = await vi.importActual<typeof import("@/lib/auth")>("@/lib/auth");
  return { ...actual, getCurrentUser: mockGetCurrentUser };
});

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[api/settings/install-policy] skipping: Postgres unreachable.");
}

describeIfDb("/api/settings/install-policy", () => {
  let orgId: string;
  let GET: typeof import("@/app/api/settings/install-policy/route").GET;
  let PATCH: typeof import("@/app/api/settings/install-policy/route").PATCH;

  beforeAll(async () => {
    const mod = await import("@/app/api/settings/install-policy/route");
    GET = mod.GET;
    PATCH = mod.PATCH;
  });

  async function seedUser(role: "admin" | "member"): Promise<string> {
    const id = `user-${role}-${Math.random().toString(36).slice(2, 8)}`;
    await db().insert(app_user).values({
      id,
      email: `${id}@example.com`,
      name: `${role}`,
      org_id: orgId,
    });
    if (role === "admin") await setLocalAdministrator(orgId, id, true);
    return id;
  }

  beforeEach(async () => {
    orgId = uniqueOrgId("api-install-policy");
    await createTestOrg(orgId);
    mockGetOrgId.mockResolvedValue(orgId);
  });

  afterEach(async () => {
    await deleteTestOrg(orgId);
    vi.clearAllMocks();
    vi.unstubAllEnvs();
  });

  afterAll(async () => {
    await pool().end();
  });

  it("GET returns DEFAULT_POLICY in persisted solo admin mode", async () => {
    mockGetCurrentUser.mockResolvedValue(await getOrCreateSoloAdmin(orgId));
    const res = await callRoute(GET);
    expect(res.status).toBe(200);
    const body = res.body as {
      source: "default" | "org";
      policy: { allowGitUrlInstalls: boolean };
    };
    expect(body.source).toBe("default");
    expect(body.policy).toEqual({ allowGitUrlInstalls: false });
  });

  it("GET returns 403 for signed-in non-admin users", async () => {
    const userId = await seedUser("member");
    mockGetCurrentUser.mockResolvedValue({
      id: userId,
      email: "member@example.com",
      name: null,
    });
    const res = await callRoute(GET);
    expect(res.status).toBe(403);
  });

  it("PATCH returns 403 for signed-in non-admin users", async () => {
    const userId = await seedUser("member");
    mockGetCurrentUser.mockResolvedValue({
      id: userId,
      email: "member@example.com",
      name: null,
    });
    const res = await callRoute(PATCH, {
      method: "PATCH",
      body: { allowGitUrlInstalls: true },
    });
    expect(res.status).toBe(403);
  });

  it("PATCH as admin persists the policy", async () => {
    const userId = await seedUser("admin");
    mockGetCurrentUser.mockResolvedValue({
      id: userId,
      email: "admin@example.com",
      name: null,
    });
    const res = await callRoute(PATCH, {
      method: "PATCH",
      body: { allowGitUrlInstalls: true },
    });
    expect(res.status).toBe(200);
    expect((res.body as { policy: { allowGitUrlInstalls: boolean } }).policy.allowGitUrlInstalls).toBe(true);

    const readBack = await callRoute(GET);
    expect((readBack.body as { policy: { allowGitUrlInstalls: boolean } }).policy.allowGitUrlInstalls).toBe(true);
  });

  it("PATCH ignores non-boolean values and unknown fields", async () => {
    const userId = await seedUser("admin");
    mockGetCurrentUser.mockResolvedValue({
      id: userId,
      email: "admin@example.com",
      name: null,
    });
    await callRoute(PATCH, {
      method: "PATCH",
      body: { allowGitUrlInstalls: "yes" as unknown as boolean, allowUnverified: true },
    });
    const res = await callRoute(GET);
    expect((res.body as { policy: unknown }).policy).toEqual({ allowGitUrlInstalls: false });
  });
});
