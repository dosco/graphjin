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
import { and, db, eq, llm_provider_config, pool } from "@neko/db";
import { AGENT_DEFAULT_GLOBAL_CAP } from "@neko/llm";
import { callRoute } from "../_helpers/route";

const { mockGetOrgId, mockProvisionHostConfig } = vi.hoisted(() => ({
  mockGetOrgId: vi.fn(),
  mockProvisionHostConfig: vi.fn(),
}));

vi.mock("@/lib/db", async () => {
  const actual = await vi.importActual<typeof import("@/lib/db")>("@/lib/db");
  return { ...actual, getOrgId: mockGetOrgId };
});

vi.mock("@neko/llm", async () => {
  const actual = await vi.importActual<typeof import("@neko/llm")>("@neko/llm");
  return { ...actual, provisionHostConfig: mockProvisionHostConfig };
});

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) console.warn("[api/settings/agent] skipping: Postgres unreachable.");

describeIfDb("/api/settings/agent", () => {
  let orgId: string;
  let GET: typeof import("@/app/api/settings/agent/route").GET;
  let PUT: typeof import("@/app/api/settings/agent/route").PUT;

  beforeAll(async () => {
    ({ GET, PUT } = await import("@/app/api/settings/agent/route"));
  });

  beforeEach(async () => {
    mockGetOrgId.mockClear();
    mockProvisionHostConfig.mockClear();
    orgId = uniqueOrgId("api-agent");
    await createTestOrg(orgId);
    mockGetOrgId.mockResolvedValue(orgId);
    mockProvisionHostConfig.mockResolvedValue(undefined);
  });

  afterEach(async () => {
    await deleteTestOrg(orgId);
  });

  afterAll(async () => {
    await pool().end();
  });

  it("GET exposes the concurrency", async () => {
    const res = await callRoute(GET);
    expect(res.status).toBe(200);
    expect(res.body).toEqual({
      agent: {
        source: "default",
        globalCap: AGENT_DEFAULT_GLOBAL_CAP,
      },
      defaults: { globalCap: AGENT_DEFAULT_GLOBAL_CAP },
    });
  });

  it("PUT rejects a backend choice", async () => {
    const res = await callRoute(PUT, {
      method: "PUT",
      body: { backend: "removed-runtime", globalCap: 30 },
    });
    expect(res.status).toBe(400);
    expect(res.body).toEqual({ error: "Unsupported agent setting: backend" });
    expect(mockProvisionHostConfig).not.toHaveBeenCalled();
  });

  it("PUT persists globalCap", async () => {
    const res = await callRoute(PUT, {
      method: "PUT",
      body: { globalCap: 30 },
    });
    expect(res.status).toBe(200);
    expect(res.body).toEqual({ source: "org", globalCap: 30 });
    expect(mockProvisionHostConfig).toHaveBeenCalledWith(orgId);

    const [row] = await db()
      .select({ provider: llm_provider_config.provider, config: llm_provider_config.config })
      .from(llm_provider_config)
      .where(
        and(
          eq(llm_provider_config.org_id, orgId),
          eq(llm_provider_config.scope, "agent"),
        ),
      );
    expect(row).toEqual({ provider: "ax", config: { globalCap: 30 } });
  });

  it("PUT falls back for an invalid cap", async () => {
    const res = await callRoute(PUT, {
      method: "PUT",
      body: { globalCap: "invalid" },
    });
    expect(res.status).toBe(200);
    expect(res.body).toEqual({
      source: "org",
      globalCap: AGENT_DEFAULT_GLOBAL_CAP,
    });
  });
});
