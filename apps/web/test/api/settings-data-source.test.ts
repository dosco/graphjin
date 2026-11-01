/**
 * /api/settings/data-source contract tests.
 *
 * Calls the route handler directly with a synthetic NextRequest and asserts
 * the response shape + DB side effects. provisionHostConfig is mocked so
 * tests don't write to the dev's host config files.
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
import { data_source, db, eq, pool } from "@neko/db";
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
  console.warn("[api/settings/data-source] skipping: Postgres unreachable.");
}

describeIfDb("/api/settings/data-source", () => {
  let orgId: string;
  let GET: typeof import("@/app/api/settings/data-source/route").GET;
  let PUT: typeof import("@/app/api/settings/data-source/route").PUT;

  beforeAll(async () => {
    const mod = await import("@/app/api/settings/data-source/route");
    GET = mod.GET;
    PUT = mod.PUT;
  });

  beforeEach(async () => {
    orgId = uniqueOrgId("api-data-source");
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

  it("GET returns defaults for an org with no data_source row", async () => {
    const res = await callRoute(GET);
    expect(res.status).toBe(200);
    const body = res.body as { source: string; graphqlUrl: string; mcpUrl: string };
    expect(body.source).toBe("unset");
    expect(body.graphqlUrl).toBe("");
    expect(body.mcpUrl).toBe("");
  });

  it("PUT persists graphqlUrl + mcpUrl + label and calls provisionHostConfig", async () => {
    const res = await callRoute(PUT, {
      method: "PUT",
      body: {
        graphqlUrl: "http://localhost:8080/api/v1/graphql",
        mcpUrl: "http://localhost:8080/api/v1/mcp",
        label: "primary",
      },
    });
    expect(res.status).toBe(200);
    const body = res.body as { source: string; graphqlUrl: string };
    expect(body.source).toBe("org");
    expect(body.graphqlUrl).toBe("http://localhost:8080/api/v1/graphql");

    // Side effect: row landed in DB
    const rows = await db()
      .select({ url: data_source.graphql_url, mcp: data_source.mcp_url })
      .from(data_source)
      .where(eq(data_source.org_id, orgId));
    expect(rows[0]).toEqual({
      url: "http://localhost:8080/api/v1/graphql",
      mcp: "http://localhost:8080/api/v1/mcp",
    });

    // Side effect: provisionHostConfig invoked with the test org id.
    expect(mockProvisionHostConfig).toHaveBeenCalledWith(orgId);
  });

  it("PUT rejects invalid GraphQL URL with 400 and no DB write", async () => {
    const res = await callRoute(PUT, {
      method: "PUT",
      body: { graphqlUrl: "not a url" },
    });
    expect(res.status).toBe(400);
    expect((res.body as { error: string }).error).toMatch(/not a valid URL|http or https/i);

    const rows = await db()
      .select()
      .from(data_source)
      .where(eq(data_source.org_id, orgId));
    expect(rows).toHaveLength(0);
    expect(mockProvisionHostConfig).not.toHaveBeenCalled();
  });

  it("PUT rejects empty graphqlUrl", async () => {
    const res = await callRoute(PUT, {
      method: "PUT",
      body: { graphqlUrl: "" },
    });
    expect(res.status).toBe(400);
  });

  it("PUT updates an existing row in place (no duplicates)", async () => {
    await callRoute(PUT, {
      method: "PUT",
      body: { graphqlUrl: "http://a.example.com/graphql" },
    });
    await callRoute(PUT, {
      method: "PUT",
      body: { graphqlUrl: "http://b.example.com/graphql" },
    });
    const rows = await db()
      .select({ url: data_source.graphql_url })
      .from(data_source)
      .where(eq(data_source.org_id, orgId));
    expect(rows).toHaveLength(1);
    expect(rows[0].url).toBe("http://b.example.com/graphql");
  });
});
