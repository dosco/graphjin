// MCP-OAuth-2.1 helpers for the Scalekit workspace connector. Hand-rolled
// over plain REST so the bundled runner stays small and testable. The flow:
//   GET {mcp}/.well-known/oauth-protected-resource → authorization_servers + scopes_supported
//   GET {as}                                        → authorization/token/registration endpoints
//   POST {registration_endpoint}  (RFC 7591 DCR)    → client_id (+ client_secret?)
//   authorization URL (PKCE S256) → user consents in browser
//   POST {token_endpoint} authorization_code+code_verifier → access+refresh tokens
//   POST {token_endpoint} refresh_token → rotated tokens

import { createHash, randomBytes } from "node:crypto";

const DEFAULT_TIMEOUT_MS = 20_000;

export class McpOAuthError extends Error {
  constructor(
    message: string,
    public readonly status: number | null,
    public override readonly cause?: unknown,
  ) {
    super(message);
    this.name = "McpOAuthError";
  }
}

export interface ProtectedResourceMetadata {
  resource: string;
  authorizationServers: string[];
  scopesSupported: string[];
}

export interface AuthorizationServerMetadata {
  issuer: string;
  authorizationEndpoint: string;
  tokenEndpoint: string;
  registrationEndpoint: string | null;
}

export interface RegisteredClient {
  clientId: string;
  clientSecret: string | null;
}

export interface TokenSet {
  access_token: string;
  refresh_token: string | null;
  expires_in: number | null;
}

export interface OauthState {
  mcpUrl: string;
  authorizationServerUrl: string;
  tokenEndpoint: string;
  clientId: string;
  clientSecret: string | null;
  codeVerifier: string;
  scopes: string[];
  redirectUri: string;
}

export interface McpOAuthOptions {
  fetchImpl?: typeof fetch;
  timeoutMs?: number;
}

export function createMcpOAuth(options: McpOAuthOptions = {}) {
  const fetchImpl = options.fetchImpl ?? fetch;
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;

  async function call<T>(
    url: string,
    init: RequestInit,
    description: string,
  ): Promise<T> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    let response: Response;
    try {
      response = await fetchImpl(url, { ...init, signal: controller.signal });
    } catch (err) {
      if ((err as Error)?.name === "AbortError") {
        throw new McpOAuthError(
          `MCP OAuth ${description} timed out after ${timeoutMs}ms`,
          null,
          "timeout",
        );
      }
      throw new McpOAuthError(
        `MCP OAuth ${description} network error: ${err instanceof Error ? err.message : String(err)}`,
        null,
        err,
      );
    } finally {
      clearTimeout(timer);
    }
    const text = await response.text().catch(() => "");
    let body: unknown;
    if (text) {
      try {
        body = JSON.parse(text);
      } catch (err) {
        throw new McpOAuthError(
          `MCP OAuth ${description} returned non-JSON (HTTP ${response.status}): ${text.slice(0, 200)}`,
          response.status,
          err,
        );
      }
    }
    if (!response.ok) {
      throw new McpOAuthError(
        `MCP OAuth ${description} returned HTTP ${response.status}`,
        response.status,
      );
    }
    return (body ?? {}) as T;
  }

  return {
    async discoverResource(mcpUrl: string): Promise<ProtectedResourceMetadata> {
      const url = new URL(
        "/.well-known/oauth-protected-resource",
        normalizeMcpUrl(mcpUrl),
      ).toString();
      const body = await call<{
        resource?: string;
        authorization_servers?: unknown;
        scopes_supported?: unknown;
      }>(
        url,
        { method: "GET", headers: { Accept: "application/json" } },
        "resource discovery",
      );
      const servers = Array.isArray(body.authorization_servers)
        ? body.authorization_servers.filter(
            (s): s is string => typeof s === "string" && s.length > 0,
          )
        : [];
      if (servers.length === 0) {
        throw new McpOAuthError(
          "protected resource metadata had no authorization_servers",
          null,
        );
      }
      const scopes = Array.isArray(body.scopes_supported)
        ? body.scopes_supported.filter(
            (s): s is string => typeof s === "string" && s.length > 0,
          )
        : [];
      return {
        resource: body.resource ?? "",
        authorizationServers: servers,
        scopesSupported: scopes,
      };
    },

    async discoverAuthorizationServer(
      asUrl: string,
    ): Promise<AuthorizationServerMetadata> {
      const base = asUrl.replace(/\/+$/, "");
      // RFC 9728: authorization server metadata is fetched at
      // {issuer}/.well-known/oauth-authorization-server. Some hosts
      // advertise the full metadata URL already; accept that too.
      const wellKnown = base.endsWith("/.well-known/oauth-authorization-server")
        ? base
        : `${base}/.well-known/oauth-authorization-server`;
      let body = await call<{
        issuer?: string;
        authorization_endpoint?: string;
        token_endpoint?: string;
        registration_endpoint?: string;
      }>(
        wellKnown,
        { method: "GET", headers: { Accept: "application/json" } },
        "authorization server discovery",
      ).catch((err) => {
        if (err instanceof McpOAuthError && err.status === 404 && wellKnown !== asUrl) {
          return null;
        }
        throw err;
      });
      if (body === null) {
        body = await call<{
          issuer?: string;
          authorization_endpoint?: string;
          token_endpoint?: string;
          registration_endpoint?: string;
        }>(
          asUrl,
          { method: "GET", headers: { Accept: "application/json" } },
          "authorization server discovery (raw URL fallback)",
        );
      }
      if (!body.authorization_endpoint || !body.token_endpoint) {
        throw new McpOAuthError(
          "authorization server metadata missing endpoints",
          null,
        );
      }
      return {
        issuer: body.issuer ?? "",
        authorizationEndpoint: body.authorization_endpoint,
        tokenEndpoint: body.token_endpoint,
        registrationEndpoint: body.registration_endpoint ?? null,
      };
    },

    async registerClient(
      registrationEndpoint: string,
      input: { clientName: string; redirectUris: string[] },
    ): Promise<RegisteredClient> {
      const body = await call<{
        client_id?: string;
        client_secret?: string;
      }>(
        registrationEndpoint,
        {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            Accept: "application/json",
          },
          body: JSON.stringify({
            client_name: input.clientName,
            redirect_uris: input.redirectUris,
            grant_types: ["authorization_code", "refresh_token"],
            response_types: ["code"],
            token_endpoint_auth_method: "none",
          }),
        },
        "dynamic client registration",
      );
      if (!body.client_id) {
        throw new McpOAuthError(
          "DCR response had no client_id",
          null,
        );
      }
      return {
        clientId: body.client_id,
        clientSecret: body.client_secret ?? null,
      };
    },

    buildAuthorizationUrl(input: {
      authorizationEndpoint: string;
      clientId: string;
      redirectUri: string;
      state: string;
      codeVerifier: string;
      scopes: string[];
    }): string {
      const url = new URL(input.authorizationEndpoint);
      url.searchParams.set("response_type", "code");
      url.searchParams.set("client_id", input.clientId);
      url.searchParams.set("redirect_uri", input.redirectUri);
      url.searchParams.set("state", input.state);
      url.searchParams.set("code_challenge", pkceChallenge(input.codeVerifier));
      url.searchParams.set("code_challenge_method", "S256");
      if (input.scopes.length > 0) {
        url.searchParams.set("scope", input.scopes.join(" "));
      }
      return url.toString();
    },

    async exchangeCode(input: {
      tokenEndpoint: string;
      clientId: string;
      clientSecret: string | null;
      code: string;
      redirectUri: string;
      codeVerifier: string;
    }): Promise<TokenSet> {
      const body = new URLSearchParams();
      body.set("grant_type", "authorization_code");
      body.set("client_id", input.clientId);
      body.set("code", input.code);
      body.set("redirect_uri", input.redirectUri);
      body.set("code_verifier", input.codeVerifier);
      return tokenResponse(
        await call<{
          access_token?: string;
          refresh_token?: string | null;
          expires_in?: number | null;
        }>(
          input.tokenEndpoint,
          {
            method: "POST",
            headers: {
              "Content-Type": "application/x-www-form-urlencoded",
              Accept: "application/json",
            },
            body: body.toString(),
          },
          "code exchange",
        ),
      );
    },

    async refreshTokens(input: {
      tokenEndpoint: string;
      clientId: string;
      clientSecret: string | null;
      refreshToken: string;
    }): Promise<TokenSet> {
      const body = new URLSearchParams();
      body.set("grant_type", "refresh_token");
      body.set("client_id", input.clientId);
      body.set("refresh_token", input.refreshToken);
      return tokenResponse(
        await call<{
          access_token?: string;
          refresh_token?: string | null;
          expires_in?: number | null;
        }>(
          input.tokenEndpoint,
          {
            method: "POST",
            headers: {
              "Content-Type": "application/x-www-form-urlencoded",
              Accept: "application/json",
            },
            body: body.toString(),
          },
          "token refresh",
        ),
      );
    },
  };
}

function tokenResponse(raw: {
  access_token?: string;
  refresh_token?: string | null;
  expires_in?: number | null;
}): TokenSet {
  if (!raw.access_token) {
    throw new McpOAuthError("token endpoint returned no access_token", null);
  }
  return {
    access_token: raw.access_token,
    refresh_token: raw.refresh_token ?? null,
    expires_in: typeof raw.expires_in === "number" ? raw.expires_in : null,
  };
}

function normalizeMcpUrl(raw: string): string {
  const trimmed = raw.trim().replace(/\/+$/, "");
  if (!trimmed) {
    throw new McpOAuthError("SCALEKIT_MCP_URL is empty", null);
  }
  if (!/^https:\/\//i.test(trimmed)) {
    throw new McpOAuthError(
      `SCALEKIT_MCP_URL must be an https URL (got ${trimmed})`,
      null,
    );
  }
  return trimmed;
}

export function generatePkceVerifier(): string {
  // RFC 7636: 43-128 chars. 32 random bytes → 43-char base64url.
  return randomBytes(32).toString("base64url");
}

export function pkceChallenge(verifier: string): string {
  return createHash("sha256").update(verifier, "utf8").digest("base64url");
}

function base64Url(bytes: Uint8Array): string {
  return Buffer.from(bytes).toString("base64url");
}

export function serializeOauthState(state: OauthState): string {
  return Buffer.from(JSON.stringify(state), "utf8").toString("base64url");
}

export function parseOauthState(raw: string | undefined): OauthState {
  if (!raw) {
    throw new McpOAuthError(
      "missing oauthState — begin_connect was not run (or its state was lost)",
      null,
    );
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(Buffer.from(raw, "base64url").toString("utf8"));
  } catch (err) {
    throw new McpOAuthError("oauthState is not valid base64url JSON", null, err);
  }
  const v = parsed as Record<string, unknown>;
  if (
    typeof v.tokenEndpoint !== "string" ||
    !v.tokenEndpoint ||
    typeof v.clientId !== "string" ||
    !v.clientId ||
    typeof v.codeVerifier !== "string" ||
    !v.codeVerifier
  ) {
    throw new McpOAuthError("oauthState is missing required fields", null);
  }
  return {
    mcpUrl: typeof v.mcpUrl === "string" ? v.mcpUrl : "",
    authorizationServerUrl:
      typeof v.authorizationServerUrl === "string"
        ? v.authorizationServerUrl
        : "",
    tokenEndpoint: v.tokenEndpoint,
    clientId: v.clientId,
    clientSecret: typeof v.clientSecret === "string" ? v.clientSecret : null,
    codeVerifier: v.codeVerifier,
    scopes: Array.isArray(v.scopes)
      ? v.scopes.filter((s): s is string => typeof s === "string")
      : [],
    redirectUri: typeof v.redirectUri === "string" ? v.redirectUri : "",
  };
}
