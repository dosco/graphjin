import { afterEach, describe, expect, it, vi } from "vitest";
import { applyPackGraphjinConfigLive, durablePackTables, unsupportedGraphjinConfigApi } from "../src/packs/graphjin-config";

describe("pack GraphJin config persistence", () => {
  it("keeps the live source-resolution hint out of durable YAML", () => {
    expect(durablePackTables([
      {
        name: "sales_order",
        source: "magento_analytics",
        database: "magento_analytics",
        read_only: true,
      },
      {
        name: "legacy_table",
        database: "legacy_database",
      },
    ])).toEqual([
      {
        name: "sales_order",
        source: "magento_analytics",
        read_only: true,
      },
      {
        name: "legacy_table",
        database: "legacy_database",
      },
    ]);
  });
});

describe("GraphJin without config preview", () => {
  it("explains a GraphJin that is too old for pack configuration", () => {
    const message = unsupportedGraphjinConfigApi(["field 'valid' is not a column or a function"]);
    expect(message).toContain("GraphJin on this OpenNeko install is too old to apply pack configuration");
    expect(message).toContain("needs GraphJin 3.18.35 or later");
    expect(message).toContain("field 'valid' is not a column or a function");
  });

  it("explains a GraphJin that cannot take pack files through its API", () => {
    const message = unsupportedGraphjinConfigApi(["column: 'gj_config.update_saved_queries' not found; available columns include: id"]);
    expect(message).toContain("cannot take pack saved queries and OpenAPI documents through its API");
  });

  it("tells the operator to set a keystore key when GraphJin has none", () => {
    const message = unsupportedGraphjinConfigApi([
      "secrets.keystore.key must be set before setting secret config values or reading encrypted secret refs (sources.forum_api); set secrets.keystore.key, for example with GJ_SECRETS_KEYSTORE_KEY, and retry",
    ]);
    expect(message).toContain("has no secrets keystore key");
    expect(message).toContain("GJ_SECRETS_KEYSTORE_KEY");
  });

  it("reads the keystore error code from a GraphQL error", () => {
    const message = unsupportedGraphjinConfigApi(["secret ref could not be read"], [
      { extensions: { code: "keystore_key_required" } },
    ]);
    expect(message).toContain("has no secrets keystore key");
  });

  it("leaves other GraphJin errors alone", () => {
    expect(unsupportedGraphjinConfigApi(["stale catalog revision", null, undefined])).toBeNull();
    expect(unsupportedGraphjinConfigApi(["field 'customer_name' is not a column or a function"])).toBeNull();
    expect(unsupportedGraphjinConfigApi(["boom"], [{ extensions: { code: "something_else" } }])).toBeNull();
  });
});


describe("live pack GraphJin apply", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("leaves existing relationships alone when the pack declares none", async () => {
    const mutations: string[] = [];
    vi.stubGlobal("fetch", vi.fn(async (_url: string, init: { body: string }) => {
      const body = JSON.parse(init.body) as { query: string; variables?: unknown };
      const respond = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "content-type": "application/json" } });
      if (!body.query.startsWith("mutation")) {
        return respond({ gj_config: { catalog_revision: "r1", sources: [], tables: [], relationships: [{ from: "a:s.t.c", to: "b:s.u.c" }], saved_queries: [] } });
      }
      mutations.push(JSON.stringify(body));
      return respond({ gj_config: { valid: true, applied: true, preview_id: "p1", catalog_revision: "r2", errors_json: "[]" } });
    }));

    await applyPackGraphjinConfigLive({
      target: { endpoint: "http://graphjin.test/api/v1/graphql", orgId: "org-1" },
      update: { update_sources: [{ name: "forum", kind: "api" }], relationships: [] },
    });

    expect(mutations).toHaveLength(2);
    for (const mutation of mutations) expect(mutation).not.toContain("relationships");
  });
});
