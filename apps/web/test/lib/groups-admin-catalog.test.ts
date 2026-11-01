import { describe, expect, it, vi } from "vitest";

const graphjinQuery = vi.fn();

vi.mock("server-only", () => ({}));
vi.mock("@neko/llm/graphjin", () => ({ graphjinQuery, mintGraphjinToken: () => "token" }));
vi.mock("@neko/db", () => {
  const chain = {
    select: () => chain,
    from: () => chain,
    where: () => chain,
    orderBy: () => chain,
    limit: async () => [{ graphqlUrl: "http://graphjin.test/api/v1/graphql" }],
  };
  return { db: () => chain, and: () => null, eq: () => null, desc: () => null, isNull: () => null, ne: () => null, data_source: {}, channel_identity: {}, library_concept: {}, metric: {}, operator_profile: {}, pack_action_definition: {}, pack_install: {} };
});
vi.mock("@/lib/auth", () => ({ getActionDescriptors: vi.fn() }));
vi.mock("@/lib/integrations", () => ({ listConnectProviders: vi.fn() }));
vi.mock("@/lib/work-files", () => ({ listWorkSkills: vi.fn() }));

const { catalogWhere, graphjinCatalog } = await import("@/lib/groups-admin");

describe("catalogWhere", () => {
  it("sends one condition without an and", () => {
    expect(catalogWhere("database")).toBe('{ kind: { eq: "database" } }');
  });

  it("joins the kind and source conditions with and", () => {
    expect(catalogWhere("column", "adventureworks")).toBe('{ and: [{ kind: { eq: "column" } }, { database_name: { eq: "adventureworks" } }] }');
  });
});

describe("graphjinCatalog", () => {
  it("reads every page, so a large schema is not cut off", async () => {
    const page = (offset: number, count: number) => ({
      data: { gj_catalog: Array.from({ length: count }, (_, i) => ({ id: `c${offset + i}`, table_name: "orders", column_name: `c${offset + i}` })) },
    });
    graphjinQuery.mockReset();
    graphjinQuery.mockResolvedValueOnce(page(0, 1000)).mockResolvedValueOnce(page(1000, 7));
    const rows = await graphjinCatalog("org-1", "column", "adventureworks");
    expect(rows).toHaveLength(1007);
    expect(graphjinQuery.mock.calls[0][0].query).toContain("offset: 0");
    expect(graphjinQuery.mock.calls[1][0].query).toContain("offset: 1000");
  });
});
