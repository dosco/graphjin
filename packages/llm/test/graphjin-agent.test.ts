import { afterEach, describe, expect, it, vi } from "vitest";
import {
  askGraphjinAgent,
  graphjinAgentEndpoint,
} from "../src/graphjin/agent";

describe("GraphJin server agent client", () => {
  afterEach(() => vi.restoreAllMocks());

  it("derives agent endpoints from GraphQL, MCP, namespace, and root URLs", () => {
    expect(graphjinAgentEndpoint("http://openneko-serve:8080/api/v1/graphql")).toBe(
      "http://openneko-serve:8080/api/v1/agent",
    );
    expect(
      graphjinAgentEndpoint("https://gj.example/api/v1/mcp", true),
    ).toBe("https://gj.example/api/v1/agent/status");
    expect(
      graphjinAgentEndpoint("https://gj.example/api/v1/ns/acme/graphql"),
    ).toBe("https://gj.example/api/v1/ns/acme/agent");
    expect(graphjinAgentEndpoint("https://gj.example/graphjin/"))
      .toBe("https://gj.example/graphjin/api/v1/agent");
  });

  it("uses only the bearer JWT for role authority", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ status: "answered", answer: "redacted" }), {
        status: 200,
        headers: { "content-type": "application/json" },
      }),
    );

    const response = await askGraphjinAgent({
      baseUrl: "http://openneko-serve:8080/api/v1/graphql",
      token: "admin-jwt",
      request: { instruction: "Explain the current configuration." },
    });

    expect(response.answer).toBe("redacted");
    const [, init] = fetchMock.mock.calls[0]!;
    expect(new Headers(init?.headers).get("authorization")).toBe(
      "Bearer admin-jwt",
    );
    expect(new Headers(init?.headers).has("x-role")).toBe(false);
  });
});
