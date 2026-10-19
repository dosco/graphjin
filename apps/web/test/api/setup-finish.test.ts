/**
 * /settings/finish contract tests. Asserts the endpoint refuses to flip
 * setup_complete_at when prerequisites aren't met.
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
  seedDataSource,
  seedProvider,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import { app_user, setLocalAdministrator, getOrCreateSoloAdmin, db, eq, organization, pool } from "@neko/db";
import { callRoute } from "../_helpers/route";

const { mockGetOrgId, mockGetCurrentUser, mockRequireAgentRuntimeReady } = vi.hoisted(() => ({
  mockGetOrgId: vi.fn(),
  mockGetCurrentUser: vi.fn(),
  mockRequireAgentRuntimeReady: vi.fn(),
}));

vi.mock("@/lib/db", async () => {
  const actual = await vi.importActual<typeof import("@/lib/db")>("@/lib/db");
  return { ...actual, getOrgId: mockGetOrgId };
});

vi.mock("@/lib/auth", async () => {
  const actual = await vi.importActual<typeof import("@/lib/auth")>("@/lib/auth");
  return { ...actual, getCurrentUser: mockGetCurrentUser };
});

vi.mock("@/lib/agent-runtime-readiness", async () => {
  const actual = await vi.importActual<
    typeof import("@/lib/agent-runtime-readiness")
  >("@/lib/agent-runtime-readiness");
  return {
    ...actual,
    requireAgentRuntimeReady: mockRequireAgentRuntimeReady,
  };
});

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[api/setup-finish] skipping: Postgres unreachable.");
}

async function readSetupCompleteAt(orgId: string): Promise<Date | null> {
  const rows = await db()
    .select({ ts: organization.setup_complete_at })
    .from(organization)
    .where(eq(organization.id, orgId));
  return rows[0]?.ts ?? null;
}

describeIfDb("/settings/finish", () => {
  let orgId: string;
  let POST: typeof import("@/app/settings/finish/route").POST;

  beforeAll(async () => {
    const mod = await import("@/app/settings/finish/route");
    POST = mod.POST;
  });

  beforeEach(async () => {
    orgId = uniqueOrgId("api-finish");
    await createTestOrg(orgId);
    mockGetOrgId.mockResolvedValue(orgId);
    mockGetCurrentUser.mockResolvedValue(await getOrCreateSoloAdmin(orgId));
    mockRequireAgentRuntimeReady.mockResolvedValue(undefined);
  });

  afterEach(async () => {
    await deleteTestOrg(orgId);
    vi.clearAllMocks();
  });

  afterAll(async () => {
    await pool().end();
  });

  async function seedUser(role: "admin" | "member"): Promise<string> {
    const id = `finish-${role}-${Math.random().toString(36).slice(2, 8)}`;
    await db().insert(app_user).values({
      id,
      email: `${id}@example.com`,
      name: role,
      org_id: orgId,
    });
    if (role === "admin") await setLocalAdministrator(orgId, id, true);
    return id;
  }

  it("rejects signed-in non-admin users", async () => {
    const userId = await seedUser("member");
    mockGetCurrentUser.mockResolvedValue({
      id: userId,
      email: "member@example.com",
      name: null,
    });

    const res = await callRoute(POST, { method: "POST" });
    expect(res.status).toBe(403);
    expect(await readSetupCompleteAt(orgId)).toBeNull();
  });

  it("rejects when no data source is configured", async () => {
    const res = await callRoute(POST, { method: "POST" });
    expect(res.status).toBe(400);
    expect((res.body as { error: string }).error).toMatch(/Data source/);
    expect(await readSetupCompleteAt(orgId)).toBeNull();
  });

  it("rejects when data source exists but no primary provider", async () => {
    await seedDataSource(orgId);
    const res = await callRoute(POST, { method: "POST" });
    expect(res.status).toBe(400);
    expect((res.body as { error: string }).error).toMatch(/provider/i);
    expect(await readSetupCompleteAt(orgId)).toBeNull();
  });

  it("flips setup_complete_at when both prerequisites are met", async () => {
    await seedDataSource(orgId);
    await seedProvider(orgId, {
      scope: "primary",
      provider: "anthropic",
      model: "claude-opus-4-7",
      secrets: { apiKey: "sk-ant" },
    });
    const res = await callRoute(POST, { method: "POST" });
    expect(res.status).toBe(200);
    expect((res.body as { ok: boolean }).ok).toBe(true);

    const ts = await readSetupCompleteAt(orgId);
    expect(ts).toBeInstanceOf(Date);
    expect(mockRequireAgentRuntimeReady).toHaveBeenCalledWith(orgId);
  });

  it("does not finish setup when the agent runtime is unavailable", async () => {
    const { AgentRuntimeUnavailableError } = await import(
      "@/lib/agent-runtime-readiness"
    );
    await seedDataSource(orgId);
    await seedProvider(orgId, {
      scope: "primary",
      provider: "anthropic",
      model: "claude-opus-4-7",
      secrets: { apiKey: "sk-ant" },
    });
    mockRequireAgentRuntimeReady.mockRejectedValueOnce(
      new AgentRuntimeUnavailableError(),
    );

    const res = await callRoute(POST, { method: "POST" });

    expect(res.status).toBe(503);
    expect(res.body).toMatchObject({ code: "agent_runtime_unavailable" });
    expect((res.body as { error: string }).error).toContain("openneko serve");
    expect(await readSetupCompleteAt(orgId)).toBeNull();
  });
});
