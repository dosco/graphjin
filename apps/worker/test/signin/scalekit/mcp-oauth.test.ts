import { describe, expect, it } from "vitest";
import {
  createMcpOAuth,
  generatePkceVerifier,
  parseOauthState,
  pkceChallenge,
  serializeOauthState,
  McpOAuthError,
} from "../../../src/signin/scalekit/mcp-oauth";

function routeFetch(handlers: Record<string, { status?: number; body: unknown }>): {
  fetch: typeof fetch;
  calls: Array<{ url: string; init: RequestInit }>;
} {
  const calls: Array<{ url: string; init: RequestInit }> = [];
  const fn = (async (url: string, init: RequestInit) => {
    calls.push({ url, init });
    const u = new URL(url);
    const key = `${init.method ?? "GET"} ${u.pathname}`;
    const h = handlers[key];
    if (!h) {
      return new Response(JSON.stringify({ error: `unexpected ${key}` }), {
        status: 404,
        headers: { "Content-Type": "application/json" },
      });
    }
    return new Response(JSON.stringify(h.body), {
      status: h.status ?? 200,
      headers: { "Content-Type": "application/json" },
    });
  }) as unknown as typeof fetch;
  return { fetch: fn, calls };
}

describe("discoverResource", () => {
  it("fetches the protected-resource metadata and maps it", async () => {
    const { fetch, calls } = routeFetch({
      "GET /.well-known/oauth-protected-resource": {
        body: {
          resource: "https://mcp.scalekit.com/",
          authorization_servers: ["https://as.scalekit.com/resources/x", ""],
          scopes_supported: ["environment_read", 42, "organization_write"],
        },
      },
    });
    const oauth = createMcpOAuth({ fetchImpl: fetch });
    const resource = await oauth.discoverResource("https://mcp.scalekit.com");
    expect(calls[0]!.url).toBe(
      "https://mcp.scalekit.com/.well-known/oauth-protected-resource",
    );
    expect(resource.authorizationServers).toEqual([
      "https://as.scalekit.com/resources/x",
    ]);
    expect(resource.scopesSupported).toEqual([
      "environment_read",
      "organization_write",
    ]);
  });

  it("throws when no authorization server is advertised", async () => {
    const { fetch } = routeFetch({
      "GET /.well-known/oauth-protected-resource": { body: {} },
    });
    const oauth = createMcpOAuth({ fetchImpl: fetch });
    await expect(
      oauth.discoverResource("https://mcp.scalekit.com"),
    ).rejects.toThrow(/authorization_servers/);
  });
});

describe("discoverAuthorizationServer", () => {
  it("fetches metadata at /.well-known/oauth-authorization-server", async () => {
    const { fetch, calls } = routeFetch({
      "GET /as/.well-known/oauth-authorization-server": {
        body: {
          issuer: "https://as.scalekit.com",
          authorization_endpoint: "https://as.scalekit.com/authorize",
          token_endpoint: "https://as.scalekit.com/token",
          registration_endpoint: "https://as.scalekit.com/register",
        },
      },
    });
    const oauth = createMcpOAuth({ fetchImpl: fetch });
    const as = await oauth.discoverAuthorizationServer("https://mcp.test/as");
    expect(calls[0]!.url).toBe(
      "https://mcp.test/as/.well-known/oauth-authorization-server",
    );
    expect(as).toEqual({
      issuer: "https://as.scalekit.com",
      authorizationEndpoint: "https://as.scalekit.com/authorize",
      tokenEndpoint: "https://as.scalekit.com/token",
      registrationEndpoint: "https://as.scalekit.com/register",
    });
  });

  it("does not double-append the well-known path", async () => {
    const { fetch, calls } = routeFetch({
      "GET /as/.well-known/oauth-authorization-server": {
        body: {
          authorization_endpoint: "https://as.scalekit.com/authorize",
          token_endpoint: "https://as.scalekit.com/token",
        },
      },
    });
    const oauth = createMcpOAuth({ fetchImpl: fetch });
    await oauth.discoverAuthorizationServer(
      "https://mcp.test/as/.well-known/oauth-authorization-server",
    );
    expect(calls[0]!.url).toBe(
      "https://mcp.test/as/.well-known/oauth-authorization-server",
    );
  });

  it("falls back to the raw URL when the well-known path 404s", async () => {
    const { fetch, calls } = routeFetch({
      "GET /as/.well-known/oauth-authorization-server": {
        status: 404,
        body: { error: "not found" },
      },
      "GET /as": {
        body: {
          authorization_endpoint: "https://as.scalekit.com/authorize",
          token_endpoint: "https://as.scalekit.com/token",
        },
      },
    });
    const oauth = createMcpOAuth({ fetchImpl: fetch });
    const as = await oauth.discoverAuthorizationServer("https://mcp.test/as");
    expect(calls[1]!.url).toBe("https://mcp.test/as");
    expect(as.tokenEndpoint).toBe("https://as.scalekit.com/token");
  });

  it("throws when endpoints are missing", async () => {
    const { fetch } = routeFetch({
      "GET /as/.well-known/oauth-authorization-server": { body: { issuer: "x" } },
    });
    const oauth = createMcpOAuth({ fetchImpl: fetch });
    await expect(
      oauth.discoverAuthorizationServer("https://mcp.test/as"),
    ).rejects.toThrow(/missing endpoints/);
  });
});

describe("registerClient (DCR)", () => {
  it("POSTs RFC 7591 registration and returns the client", async () => {
    const { fetch, calls } = routeFetch({
      "POST /register": {
        body: { client_id: "dcr-1", client_secret: "dcr-secret-1" },
      },
    });
    const oauth = createMcpOAuth({ fetchImpl: fetch });
    const client = await oauth.registerClient("https://mcp.test/register", {
      clientName: "openneko-plugin-scalekit",
      redirectUris: ["https://app.example.com/cb"],
    });
    expect(client).toEqual({ clientId: "dcr-1", clientSecret: "dcr-secret-1" });
    const body = JSON.parse(String(calls[0]!.init.body)) as Record<
      string,
      unknown
    >;
    expect(body.client_name).toBe("openneko-plugin-scalekit");
    expect(body.redirect_uris).toEqual(["https://app.example.com/cb"]);
    expect(body.grant_types).toEqual(["authorization_code", "refresh_token"]);
  });

  it("throws when no client_id is returned", async () => {
    const { fetch } = routeFetch({ "POST /register": { body: {} } });
    const oauth = createMcpOAuth({ fetchImpl: fetch });
    await expect(
      oauth.registerClient("https://mcp.test/register", {
        clientName: "x",
        redirectUris: [],
      }),
    ).rejects.toThrow(/client_id/);
  });
});

describe("buildAuthorizationUrl", () => {
  it("sets PKCE S256 params", () => {
    const oauth = createMcpOAuth();
    const verifier = generatePkceVerifier();
    const url = new URL(
      oauth.buildAuthorizationUrl({
        authorizationEndpoint: "https://as.scalekit.com/authorize",
        clientId: "dcr-1",
        redirectUri: "https://app.example.com/cb",
        state: "csrf",
        codeVerifier: verifier,
        scopes: ["environment_read", "organization_write"],
      }),
    );
    expect(url.searchParams.get("response_type")).toBe("code");
    expect(url.searchParams.get("client_id")).toBe("dcr-1");
    expect(url.searchParams.get("redirect_uri")).toBe(
      "https://app.example.com/cb",
    );
    expect(url.searchParams.get("state")).toBe("csrf");
    expect(url.searchParams.get("code_challenge")).toBe(
      pkceChallenge(verifier),
    );
    expect(url.searchParams.get("code_challenge_method")).toBe("S256");
    expect(url.searchParams.get("scope")).toBe(
      "environment_read organization_write",
    );
  });
});

describe("exchangeCode / refreshTokens", () => {
  it("exchanges the code with PKCE and parses tokens", async () => {
    const { fetch, calls } = routeFetch({
      "POST /token": {
        body: {
          access_token: "at-1",
          refresh_token: "rt-1",
          expires_in: 3600,
        },
      },
    });
    const oauth = createMcpOAuth({ fetchImpl: fetch });
    const tokens = await oauth.exchangeCode({
      tokenEndpoint: "https://mcp.test/token",
      clientId: "dcr-1",
      clientSecret: null,
      code: "code-1",
      redirectUri: "https://app.example.com/cb",
      codeVerifier: "verifier",
    });
    expect(tokens).toEqual({
      access_token: "at-1",
      refresh_token: "rt-1",
      expires_in: 3600,
    });
    const body = String(calls[0]!.init.body);
    expect(body).toContain("grant_type=authorization_code");
    expect(body).toContain("code_verifier=verifier");
    expect(body).toContain("client_id=dcr-1");
  });

  it("refreshes with the refresh grant", async () => {
    const { fetch, calls } = routeFetch({
      "POST /token": {
        body: { access_token: "at-2", refresh_token: "rt-2" },
      },
    });
    const oauth = createMcpOAuth({ fetchImpl: fetch });
    const tokens = await oauth.refreshTokens({
      tokenEndpoint: "https://mcp.test/token",
      clientId: "dcr-1",
      clientSecret: null,
      refreshToken: "rt-1",
    });
    expect(tokens.access_token).toBe("at-2");
    const body = String(calls[0]!.init.body);
    expect(body).toContain("grant_type=refresh_token");
    expect(body).toContain("refresh_token=rt-1");
  });

  it("throws when no access_token is returned", async () => {
    const { fetch } = routeFetch({ "POST /token": { body: {} } });
    const oauth = createMcpOAuth({ fetchImpl: fetch });
    await expect(
      oauth.exchangeCode({
        tokenEndpoint: "https://mcp.test/token",
        clientId: "dcr-1",
        clientSecret: null,
        code: "c",
        redirectUri: "https://app.example.com/cb",
        codeVerifier: "v",
      }),
    ).rejects.toThrow(/no access_token/);
  });

  it("surfaces HTTP errors as McpOAuthError", async () => {
    const { fetch } = routeFetch({
      "POST /token": { status: 401, body: { error: "invalid_grant" } },
    });
    const oauth = createMcpOAuth({ fetchImpl: fetch });
    await expect(
      oauth.refreshTokens({
        tokenEndpoint: "https://mcp.test/token",
        clientId: "dcr-1",
        clientSecret: null,
        refreshToken: "rt-1",
      }),
    ).rejects.toBeInstanceOf(McpOAuthError);
  });
});

describe("oauthState round-trip", () => {
  it("serializes and parses", () => {
    const state = {
      mcpUrl: "https://mcp.scalekit.com",
      authorizationServerUrl: "https://as.scalekit.com",
      tokenEndpoint: "https://as.scalekit.com/token",
      clientId: "dcr-1",
      clientSecret: null,
      codeVerifier: "verifier-1",
      scopes: ["environment_read"],
      redirectUri: "https://app.example.com/cb",
    };
    const raw = serializeOauthState(state);
    expect(parseOauthState(raw)).toEqual(state);
  });

  it("rejects missing state", () => {
    expect(() => parseOauthState(undefined)).toThrow(/oauthState/);
  });

  it("rejects malformed state", () => {
    expect(() => parseOauthState("!!!!not-json")).toThrow(McpOAuthError);
    expect(() =>
      parseOauthState(Buffer.from("{}").toString("base64url")),
    ).toThrow(/missing required fields/);
  });
});

describe("PKCE helpers", () => {
  it("generates RFC 7636 verifiers and matching challenges", () => {
    const a = generatePkceVerifier();
    const b = generatePkceVerifier();
    expect(a).not.toBe(b);
    expect(a).toMatch(/^[A-Za-z0-9_-]{43}$/);
    expect(pkceChallenge(a)).toMatch(/^[A-Za-z0-9_-]{43}$/);
  });
});
