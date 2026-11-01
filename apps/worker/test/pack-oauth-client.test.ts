import { describe, expect, it, vi } from "vitest";
import {
  buildPackAuthorizationUrl,
  exchangePackOAuthCode,
  fetchPackOAuthAccount,
  refreshPackOAuthToken,
  type PackOAuthConnection,
} from "../src/packs/oauth-client.js";

const connection: PackOAuthConnection = {
  key: "workspace",
  providerLabel: "Google Workspace",
  authorizationUrl: "https://accounts.example.test/oauth",
  tokenUrl: "https://oauth.example.test/token",
  userInfoUrl: "https://openid.example.test/userinfo",
  clientIdInput: "workspace.client_id",
  clientSecret: "workspace.client_secret",
  accessToken: "workspace.access_token",
  refreshToken: "workspace.refresh_token",
  scope: "deployment",
  scopes: ["openid", "mail.read"],
  authorizationParams: { access_type: "offline", prompt: "consent" },
  accountIdField: "sub",
  accountLabelField: "email",
};

describe("pack OAuth client", () => {
  it("builds a PKCE authorization request with the reviewed scopes", () => {
    const url = new URL(buildPackAuthorizationUrl({
      connection,
      clientId: "client-id",
      redirectUri: "https://neko.example/api/pack-accounts/workspace/main/callback",
      state: "state-token",
      codeChallenge: "challenge",
    }));
    expect(url.origin + url.pathname).toBe(connection.authorizationUrl);
    expect(Object.fromEntries(url.searchParams)).toMatchObject({
      response_type: "code",
      client_id: "client-id",
      state: "state-token",
      code_challenge: "challenge",
      code_challenge_method: "S256",
      access_type: "offline",
      prompt: "consent",
      scope: "openid mail.read",
    });
  });

  it("exchanges and refreshes tokens without returning provider response bodies in errors", async () => {
    const exchangeFetch = vi.fn(async (_url: string | URL | Request, init?: RequestInit) => {
      expect(String(init?.body)).toContain("code_verifier=verifier");
      return new Response(JSON.stringify({ access_token: "access-1", refresh_token: "refresh-1", expires_in: 3600, scope: "openid mail.read" }), { status: 200 });
    });
    const exchanged = await exchangePackOAuthCode({
      connection, clientId: "id", clientSecret: "secret", code: "code", codeVerifier: "verifier",
      redirectUri: "https://neko.example/callback", fetchImpl: exchangeFetch as typeof fetch,
    });
    expect(exchanged).toMatchObject({ accessToken: "access-1", refreshToken: "refresh-1", scopes: ["openid", "mail.read"] });

    const refreshed = await refreshPackOAuthToken({
      connection, clientId: "id", clientSecret: "secret", refreshToken: "refresh-1", scopes: exchanged.scopes,
      fetchImpl: (async () => new Response(JSON.stringify({ access_token: "access-2", expires_in: 3600 }), { status: 200 })) as typeof fetch,
    });
    expect(refreshed).toMatchObject({ accessToken: "access-2", refreshToken: "refresh-1" });

    await expect(exchangePackOAuthCode({
      connection, clientId: "id", clientSecret: "secret", code: "bad", codeVerifier: "verifier",
      redirectUri: "https://neko.example/callback",
      fetchImpl: (async () => new Response(JSON.stringify({ error: "secret-provider-detail" }), { status: 400 })) as typeof fetch,
    })).rejects.toThrow("OAuth code exchange failed with HTTP 400");
  });

  it("binds the declared account identity fields", async () => {
    const account = await fetchPackOAuthAccount({
      connection,
      accessToken: "access",
      fetchImpl: (async (_url, init) => {
        expect(new Headers(init?.headers).get("authorization")).toBe("Bearer access");
        return new Response(JSON.stringify({ sub: "account-1", email: "owner@example.test" }), { status: 200 });
      }) as typeof fetch,
    });
    expect(account).toEqual({ id: "account-1", label: "owner@example.test" });
  });
});
