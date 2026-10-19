import { describe, expect, it } from "vitest";
import type { ConnectorCredential } from "@neko/secrets";
import {
  beginScalekitAuth,
  beginScalekitConnect,
  callScalekitTool,
  completeScalekitAuth,
  completeScalekitConnect,
  refreshScalekitConnect,
  scalekitCredentialNearExpiry,
  ScalekitError,
} from "../../../src/signin/scalekit/provider";
import type {
  ScalekitClient,
  ScalekitTokenResponse,
  ScalekitUserinfo,
} from "../../../src/signin/scalekit/client";
import {
  pkceChallenge,
  serializeOauthState,
  type TokenSet,
} from "../../../src/signin/scalekit/mcp-oauth";

const ENV = {
  SCALEKIT_ENVIRONMENT_URL: "https://foo.scalekit.com",
  SCALEKIT_CLIENT_ID: "c",
  SCALEKIT_CLIENT_SECRET: "s",
};

const MCP_URL = "https://mcp.scalekit.com";

function fakeClient(opts: {
  authorizationUrl?: string;
  tokens?: ScalekitTokenResponse;
  userinfo?: ScalekitUserinfo;
}): ScalekitClient {
  return {
    buildAuthorizationUrl({ redirectUri, state, loginHint }) {
      return (
        opts.authorizationUrl ??
        `https://foo.scalekit.com/oauth/authorize?state=${state}&redirect=${redirectUri}&hint=${loginHint ?? ""}`
      );
    },
    async exchangeCode() {
      return (
        opts.tokens ?? {
          access_token: "at-1",
          token_type: "Bearer",
          expires_in: 3600,
        }
      );
    },
    async fetchUserinfo() {
      return (
        opts.userinfo ?? {
          sub: "user-1",
          email: "amit@example.com",
          name: "Amit",
        }
      );
    },
  };
}

interface FakeOauthRecorder {
  discoverResourceCalls: string[];
  discoverAsCalls: string[];
  registerCalls: Array<{ registrationEndpoint: string; redirectUris: string[] }>;
  exchangeCalls: Array<Record<string, unknown>>;
  refreshCalls: Array<Record<string, unknown>>;
}

function fakeMcpOauth(overrides: {
  resource?: { authorizationServers: string[]; scopesSupported: string[] };
  as?: { authorizationEndpoint: string; tokenEndpoint: string; registrationEndpoint: string | null };
  registered?: { clientId: string; clientSecret: string | null };
  tokenSet?: TokenSet;
  refreshSet?: TokenSet;
  recorder?: FakeOauthRecorder;
}) {
  const recorder = overrides.recorder ?? {
    discoverResourceCalls: [],
    discoverAsCalls: [],
    registerCalls: [],
    exchangeCalls: [],
    refreshCalls: [],
  };
  return {
    async discoverResource(mcpUrl: string) {
      recorder.discoverResourceCalls.push(mcpUrl);
      return (
        overrides.resource ?? {
          authorizationServers: ["https://as.scalekit.com"],
          scopesSupported: ["environment_read", "organization_write"],
        }
      );
    },
    async discoverAuthorizationServer(asUrl: string) {
      recorder.discoverAsCalls.push(asUrl);
      return (
        overrides.as ?? {
          authorizationEndpoint: "https://as.scalekit.com/authorize",
          tokenEndpoint: "https://as.scalekit.com/token",
          registrationEndpoint: "https://as.scalekit.com/register",
        }
      );
    },
    async registerClient(
      registrationEndpoint: string,
      input: { clientName: string; redirectUris: string[] },
    ) {
      recorder.registerCalls.push({
        registrationEndpoint,
        redirectUris: input.redirectUris,
      });
      return (
        overrides.registered ?? {
          clientId: "dcr-client-1",
          clientSecret: null,
        }
      );
    },
    buildAuthorizationUrl(input: {
      authorizationEndpoint: string;
      clientId: string;
      redirectUri: string;
      state: string;
      codeVerifier: string;
      scopes: string[];
    }) {
      return `https://as.scalekit.com/authorize?client_id=${input.clientId}&state=${input.state}&scope=${encodeURIComponent(input.scopes.join(" "))}`;
    },
    async exchangeCode(input: Record<string, unknown>) {
      recorder.exchangeCalls.push(input);
      return (
        overrides.tokenSet ?? {
          access_token: "at-mgmt",
          refresh_token: "rt-mgmt",
          expires_in: 3600,
        }
      );
    },
    async refreshTokens(input: Record<string, unknown>) {
      recorder.refreshCalls.push(input);
      return (
        overrides.refreshSet ?? {
          access_token: "at-refreshed",
          refresh_token: "rt-rotated",
          expires_in: 3600,
        }
      );
    },
  };
}

describe("sign-in", () => {
  it("fails with the missing settings named when credentials are unset", async () => {
    await expect(
      beginScalekitAuth(
        { redirectUri: "https://app.example.com/cb", state: "x" },
        {},
        { createClient: () => ({} as ScalekitClient) },
      ),
    ).rejects.toThrow(ScalekitError);
    await expect(
      beginScalekitAuth({ redirectUri: "https://app.example.com/cb", state: "x" }, { ...ENV, SCALEKIT_ENVIRONMENT_URL: "" }),
    ).rejects.toThrow(/SCALEKIT_ENVIRONMENT_URL/);
  });

  it("begin returns the client-built URL", async () => {
    const result = await beginScalekitAuth(
      { redirectUri: "https://app.example.com/cb", state: "csrf" },
      ENV,
      {
        createClient: () =>
          fakeClient({
            authorizationUrl: "https://foo.scalekit.com/oauth/authorize?stub=1",
          }),
      },
    );
    expect(result.authorizationUrl).toBe(
      "https://foo.scalekit.com/oauth/authorize?stub=1",
    );
  });

  it("complete maps identity + groups", async () => {
    const result = await completeScalekitAuth(
      { code: "c", redirectUri: "https://app.example.com/cb", state: "s" },
      ENV,
      {
        createClient: () =>
          fakeClient({
            userinfo: {
              sub: "user-1",
              email: "amit@example.com",
              name: "Amit",
              organization_id: "org-1",
              groups: ["everyone"],
              roles: ["admin"],
            },
          }),
      },
    );
    expect(result).toEqual({
      sub: "user-1",
      email: "amit@example.com",
      name: "Amit",
      orgId: "org-1",
      groups: ["everyone", "admin"],
    });
  });
});

describe("workspace consent (mcp-oauth)", () => {
  it("begin discovers + registers + returns authorizationUrl with oauthState", async () => {
    const recorder: FakeOauthRecorder = {
      discoverResourceCalls: [],
      discoverAsCalls: [],
      registerCalls: [],
      exchangeCalls: [],
      refreshCalls: [],
    };
    const result = await beginScalekitConnect(
      {
        operatorId: "__deployment__",
        redirectUri: "https://app.example.com/api/integrations/connect/callback",
        state: "csrf-1",
        scopes: [],
      },
      {},
      { createMcpOAuth: () => fakeMcpOauth({ recorder }) },
    );
    expect(result.authorizationUrl).toContain("client_id=dcr-client-1");
    expect(result.oauthState).toBeTruthy();
    expect(recorder.discoverResourceCalls).toEqual([MCP_URL]);
    expect(recorder.discoverAsCalls).toEqual(["https://as.scalekit.com"]);
    expect(recorder.registerCalls).toEqual([
      {
        registrationEndpoint: "https://as.scalekit.com/register",
        redirectUris: [
          "https://app.example.com/api/integrations/connect/callback",
        ],
      },
    ]);
  });

  it("begin falls back to discovered scopes and generates a PKCE verifier", async () => {
    const recorder: FakeOauthRecorder = {
      discoverResourceCalls: [],
      discoverAsCalls: [],
      registerCalls: [],
      exchangeCalls: [],
      refreshCalls: [],
    };
    const result = await beginScalekitConnect(
      {
        operatorId: "__deployment__",
        redirectUri: "https://app.example.com/cb",
        state: "s",
        scopes: [],
      },
      {},
      { createMcpOAuth: () => fakeMcpOauth({ recorder }) },
    );
    const parsed = JSON.parse(
      Buffer.from(result.oauthState!, "base64url").toString("utf8"),
    ) as { codeVerifier: string; scopes: string[]; tokenEndpoint: string };
    expect(parsed.scopes).toEqual(["environment_read", "organization_write"]);
    expect(parsed.tokenEndpoint).toBe("https://as.scalekit.com/token");
    expect(parsed.codeVerifier.length).toBeGreaterThanOrEqual(43);
    expect(pkceChallenge(parsed.codeVerifier)).toMatch(/^[A-Za-z0-9_-]+$/);
  });

  it("complete exchanges the code and returns a refreshable credential", async () => {
    const oauthState = serializeOauthState({
      mcpUrl: MCP_URL,
      authorizationServerUrl: "https://as.scalekit.com",
      tokenEndpoint: "https://as.scalekit.com/token",
      clientId: "dcr-client-1",
      clientSecret: null,
      codeVerifier: "verifier-1234567890123456789012345678901234567890123",
      scopes: ["environment_read"],
      redirectUri: "https://app.example.com/cb",
    });
    const recorder: FakeOauthRecorder = {
      discoverResourceCalls: [],
      discoverAsCalls: [],
      registerCalls: [],
      exchangeCalls: [],
      refreshCalls: [],
    };
    const result = await completeScalekitConnect(
      {
        operatorId: "__deployment__",
        code: "auth-code",
        redirectUri: "https://app.example.com/cb",
        state: "s",
        scopes: [],
        oauthState,
      },
      { createMcpOAuth: () => fakeMcpOauth({ recorder }) },
    );
    expect(recorder.exchangeCalls[0]).toMatchObject({
      code: "auth-code",
      codeVerifier: "verifier-1234567890123456789012345678901234567890123",
    });
    const tokens = result.tokens as Record<string, unknown>;
    expect(tokens.access_token).toBe("at-mgmt");
    expect(tokens.refresh_token).toBe("rt-mgmt");
    expect(tokens.token_endpoint).toBe("https://as.scalekit.com/token");
    expect(tokens.client_id).toBe("dcr-client-1");
    expect(tokens.expires_at).toBeTypeOf("string");
  });

  it("refresh rotates the stored credential", async () => {
    const current: ConnectorCredential = {
      tokens: {
        access_token: "at-old",
        refresh_token: "rt-old",
        expires_at: "0",
        token_endpoint: "https://as.scalekit.com/token",
        client_id: "dcr-client-1",
        client_secret: "",
      },
      connectedAt: new Date().toISOString(),
    };
    const recorder: FakeOauthRecorder = {
      discoverResourceCalls: [],
      discoverAsCalls: [],
      registerCalls: [],
      exchangeCalls: [],
      refreshCalls: [],
    };
    const result = await refreshScalekitConnect(current, { createMcpOAuth: () => fakeMcpOauth({ recorder }) });
    expect(recorder.refreshCalls[0]).toMatchObject({
      refreshToken: "rt-old",
      clientId: "dcr-client-1",
    });
    const tokens = result.tokens as Record<string, unknown>;
    expect(tokens.access_token).toBe("at-refreshed");
    expect(tokens.refresh_token).toBe("rt-rotated");
    expect(result.refreshedAt).toBeTypeOf("string");
    expect(scalekitCredentialNearExpiry(current)).toBe(true);
    expect(scalekitCredentialNearExpiry(result)).toBe(false);
  });

  it("complete without oauthState throws a clear error", async () => {
    await expect(
      completeScalekitConnect(
        {
          operatorId: "__deployment__",
          code: "c",
          redirectUri: "https://app.example.com/cb",
          state: "s",
          scopes: [],
        },
        { createMcpOAuth: () => fakeMcpOauth({}) },
      ),
    ).rejects.toThrow(/oauthState/);
  });
});

describe("workspace tools", () => {
  function credential(overrides: Record<string, unknown> = {}): ConnectorCredential {
    return {
      tokens: {
        access_token: "at-fresh",
        refresh_token: "rt-1",
        expires_at: String(Date.now() + 3600_000),
        token_endpoint: "https://as.scalekit.com/token",
        client_id: "dcr-client-1",
        client_secret: "",
        ...overrides,
      },
      connectedAt: new Date().toISOString(),
    };
  }

  it("calls the MCP tool with the arguments and returns the text", async () => {
    const toolCalls: Array<{ name: string; args: Record<string, unknown>; url: string; token: string }> = [];
    const text = await callScalekitTool("list_environments", { pageToken: "1" }, credential(), {}, {
      createMcpClient: async ({ url, accessToken }) => ({
        async callTool(name, args) {
          toolCalls.push({ name, args, url, token: accessToken });
          return { content: [{ type: "text", text: "env_1 DEV" }] };
        },
        async close() {},
      }),
    });
    expect(text).toBe("env_1 DEV");
    expect(toolCalls).toEqual([{ name: "list_environments", args: { pageToken: "1" }, url: MCP_URL, token: "at-fresh" }]);
  });

  it("surfaces a tool error as an exception", async () => {
    await expect(
      callScalekitTool("list_environments", {}, credential(), {}, {
        createMcpClient: async () => ({
          callTool: async () => ({ content: [{ type: "text", text: "forbidden" }], isError: true }),
          close: async () => {},
        }),
      }),
    ).rejects.toThrow("forbidden");
  });

  it("throws a clear error when the workspace is not connected", async () => {
    await expect(
      callScalekitTool("list_environments", {}, null, {}, {
        createMcpClient: async () => ({ callTool: async () => ({ content: [] }), close: async () => {} }),
      }),
    ).rejects.toThrow(/not connected/);
  });
});
