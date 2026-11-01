/**
 * /api/settings/provider contract tests. Round-trips the primary scope
 * with secret encryption and asserts the cross-section coupling rule.
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
import { pool } from "@neko/db";
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

if (!reachable) {
  console.warn("[api/settings/provider] skipping: Postgres unreachable.");
}

describeIfDb("/api/settings/provider", () => {
  let orgId: string;
  let GET: typeof import("@/app/api/settings/provider/route").GET;
  let PUT: typeof import("@/app/api/settings/provider/route").PUT;

  beforeAll(async () => {
    const mod = await import("@/app/api/settings/provider/route");
    GET = mod.GET;
    PUT = mod.PUT;
  });

  beforeEach(async () => {
    orgId = uniqueOrgId("api-provider");
    await createTestOrg(orgId);
    mockGetOrgId.mockResolvedValue(orgId);
    mockProvisionHostConfig.mockResolvedValue(undefined);
  });

  afterEach(async () => {
    await deleteTestOrg(orgId);
    vi.clearAllMocks();
  });

  afterAll(async () => {
    await pool().end();
  });

  it("GET returns default primary + research payload", async () => {
    const res = await callRoute(GET);
    expect(res.status).toBe(200);
    const body = res.body as {
      primary: { source: string };
      research: { source: string };
    };
    expect(body.primary.source).toBe("default");
    expect(body.research.source).toBe("default");
  });

  it("PUT primary persists + calls provisionHostConfig", async () => {
    const res = await callRoute(PUT, {
      method: "PUT",
      body: {
        scope: "primary",
        provider: "anthropic",
        model: "claude-opus-4-7",
        enabled: true,
        config: {},
        secrets: { apiKey: "sk-ant-test" },
      },
    });
    expect(res.status).toBe(200);
    expect(mockProvisionHostConfig).toHaveBeenCalledWith(orgId);

    // Read back via GET — secret comes back masked, never plaintext.
    const get = await callRoute(GET);
    const body = get.body as {
      primary: { source: string; provider: string; secretStatus: Record<string, string> };
    };
    expect(body.primary.source).toBe("org");
    expect(body.primary.provider).toBe("anthropic");
    expect(body.primary.secretStatus.apiKey).not.toBe("sk-ant-test");
    expect(body.primary.secretStatus.apiKey).toMatch(/[•*]/);
  });

  it("PUT research with provider=disabled does NOT call provisionHostConfig", async () => {
    const res = await callRoute(PUT, {
      method: "PUT",
      body: {
        scope: "research",
        provider: "disabled",
        model: "",
        enabled: false,
        config: {},
        secrets: {},
      },
    });
    expect(res.status).toBe(200);
    // Only primary writes trigger host re-provisioning.
    expect(mockProvisionHostConfig).not.toHaveBeenCalled();
  });

  it("allows primary provider changes for the agent runtime", async () => {
    const res = await callRoute(PUT, {
      method: "PUT",
      body: {
        scope: "primary",
        provider: "anthropic",
        model: "claude-opus-4-7",
        enabled: true,
        config: {},
        secrets: { apiKey: "sk-ant" },
      },
    });
    expect(res.status).toBe(200);
  });
});
