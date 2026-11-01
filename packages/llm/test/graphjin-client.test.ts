import { afterEach, describe, expect, it, vi } from "vitest";
import { graphjinQuery } from "../src/graphjin/client";

describe("graphjinQuery", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("POSTs to the given GraphQL endpoint with role header and parses JSON response", async () => {
    const responseBody = { data: { workflow_definition: [{ id: "x1" }] } };
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify(responseBody), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    const result = await graphjinQuery<{
      workflow_definition: Array<{ id: string }>;
    }>({
      // baseUrl is the full GraphQL endpoint — graphjinQuery does not append a path.
      baseUrl: "http://127.0.0.1:8089/api/v1/graphql",
      query: "{ workflow_definition { id } }",
      role: "admin",
    });

    expect(fetchSpy).toHaveBeenCalledTimes(1);
    const [url, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("http://127.0.0.1:8089/api/v1/graphql");
    expect(init.method).toBe("POST");
    expect((init.headers as Record<string, string>)["X-Role"]).toBe("admin");
    expect(JSON.parse(String(init.body))).toMatchObject({
      query: "{ workflow_definition { id } }",
      variables: {},
    });
    expect(result.data?.workflow_definition[0].id).toBe("x1");
  });

  it("throws when GraphJin returns a non-2xx response", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response("internal error", { status: 500 }),
    );
    await expect(
      graphjinQuery({
        baseUrl: "http://127.0.0.1:8089",
        query: "{ broken }",
      }),
    ).rejects.toThrow(/500/);
  });

  it("normalizes GraphJin string errors", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ errors: ["blocked for role user"] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    const result = await graphjinQuery({
      baseUrl: "http://127.0.0.1:8089/api/v1/graphql",
      query: "query { gj_config(id: \"current\") { id } }",
    });

    expect(result.data).toBeNull();
    expect(result.errors).toEqual([{ message: "blocked for role user" }]);
  });
});
