import { describe, expect, it } from "vitest";
import {
  assertReadOnlyGraphql,
  graphjinDevelopmentAuthHeaders,
} from "../src/work/control-plane";

describe("GraphJin job broker read gate", () => {
  it("accepts explicit and shorthand query operations", () => {
    expect(() =>
      assertReadOnlyGraphql("query { orders(limit: 1) { id } }"),
    ).not.toThrow();
    expect(() =>
      assertReadOnlyGraphql("{ gj_catalog(id: \"help:discovery\") { id } }"),
    ).not.toThrow();
  });

  it.each([
    "mutation { orders(insert: { id: 1 }) { id } }",
    "subscription { orders { id } }",
    "query { run(operation: \"mutation\") }",
  ])("rejects anything carrying a write/stream operation: %s", (query) => {
    expect(() => assertReadOnlyGraphql(query)).toThrow(/query operations only/);
  });

  it("rejects non-query documents", () => {
    expect(() => assertReadOnlyGraphql("fragment Fields on Order { id }")).toThrow(
      /explicit query operation/,
    );
  });
});

describe("GraphJin development auth", () => {
  it("forwards the Work actor identity used for source-aware API authorization", () => {
    expect(
      graphjinDevelopmentAuthHeaders(
        { userId: "user-1", role: "member" },
        "test",
      ),
    ).toEqual({
      "X-User-ID": "user-1",
      "X-User-Role": "member",
    });
    expect(
      graphjinDevelopmentAuthHeaders(
        { userId: null, role: "service" },
        "development",
      ),
    ).toEqual({
      "X-User-ID": "openneko-service",
      "X-User-Role": "service",
    });
  });

  it("cannot be enabled in production", () => {
    expect(() =>
      graphjinDevelopmentAuthHeaders(
        { userId: "user-1", role: "member" },
        "production",
      ),
    ).toThrow(/forbidden in production/);
  });
});
