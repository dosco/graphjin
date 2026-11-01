/**
 * /api/settings/skill-learn contract tests.
 *
 * Covers: persisted solo admin => 200 default off, non-admin => 403,
 * admin => 200 read + write, persist across GET, reject non-boolean.
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
import { app_user, setLocalAdministrator, getOrCreateSoloAdmin, db, eq, pool, skill_learn_org } from "@neko/db";
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
  console.warn("[api/settings/skill-learn] skipping: Postgres unreachable.");
}

describeIfDb("/api/settings/skill-learn", () => {
  let orgId: string;
  let GET: typeof import("@/app/api/settings/skill-learn/route").GET;
  let PATCH: typeof import("@/app/api/settings/skill-learn/route").PATCH;

  beforeAll(async () => {
    const mod = await import("@/app/api/settings/skill-learn/route");
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
    orgId = uniqueOrgId("api-skill-learn");
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

  it("GET returns learning off when no org row exists", async () => {
    mockGetCurrentUser.mockResolvedValue(await getOrCreateSoloAdmin(orgId));
    const res = await callRoute(GET);
    expect(res.status).toBe(200);
    expect(res.body).toEqual({ enabled: false, source: "default" });
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
      body: { enabled: true },
    });
    expect(res.status).toBe(403);
  });

  it("PATCH as admin persists the org flag", async () => {
    const userId = await seedUser("admin");
    mockGetCurrentUser.mockResolvedValue({
      id: userId,
      email: "admin@example.com",
      name: null,
    });
    const res = await callRoute(PATCH, {
      method: "PATCH",
      body: { enabled: true },
    });
    expect(res.status).toBe(200);
    expect(res.body).toEqual({ enabled: true, source: "org" });

    const readBack = await callRoute(GET);
    expect(readBack.body).toEqual({ enabled: true, source: "org" });

    const [row] = await db()
      .select({ enabled: skill_learn_org.enabled })
      .from(skill_learn_org)
      .where(eq(skill_learn_org.org_id, orgId))
      .limit(1);
    expect(row?.enabled).toBe(true);
  });

  it("PATCH can turn learning off after it was on", async () => {
    const userId = await seedUser("admin");
    mockGetCurrentUser.mockResolvedValue({
      id: userId,
      email: "admin@example.com",
      name: null,
    });
    await callRoute(PATCH, { method: "PATCH", body: { enabled: true } });
    const res = await callRoute(PATCH, {
      method: "PATCH",
      body: { enabled: false },
    });
    expect(res.status).toBe(200);
    expect(res.body).toEqual({ enabled: false, source: "org" });
  });

  it("PATCH rejects a non-boolean enabled value", async () => {
    const userId = await seedUser("admin");
    mockGetCurrentUser.mockResolvedValue({
      id: userId,
      email: "admin@example.com",
      name: null,
    });
    const res = await callRoute(PATCH, {
      method: "PATCH",
      body: { enabled: "yes" },
    });
    expect(res.status).toBe(400);
  });
});
