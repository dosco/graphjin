import { createHash } from "node:crypto";
import type { SolutionPackManifest } from "./manifest.js";

export type PackOAuthConnection = SolutionPackManifest["oauth"][number];

export type PackOAuthTokens = {
  accessToken: string;
  refreshToken?: string;
  expiresAt: string;
  scopes: string[];
};

function formBody(values: Record<string, string>): URLSearchParams {
  return new URLSearchParams(Object.entries(values).filter(([, value]) => value !== ""));
}

async function providerJson(
  response: Response,
  operation: string,
): Promise<Record<string, unknown>> {
  if (!response.ok) {
    throw new Error(`${operation} failed with HTTP ${response.status}`);
  }
  const value = await response.json().catch(() => null);
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${operation} returned an invalid JSON response`);
  }
  return value as Record<string, unknown>;
}

export function buildPackAuthorizationUrl(input: {
  connection: PackOAuthConnection;
  clientId: string;
  redirectUri: string;
  state: string;
  codeChallenge: string;
}): string {
  const url = new URL(input.connection.authorizationUrl);
  for (const [key, value] of Object.entries(input.connection.authorizationParams ?? {})) {
    url.searchParams.set(key, value);
  }
  url.searchParams.set("response_type", "code");
  url.searchParams.set("client_id", input.clientId);
  url.searchParams.set("redirect_uri", input.redirectUri);
  url.searchParams.set("scope", input.connection.scopes.join(" "));
  url.searchParams.set("state", input.state);
  url.searchParams.set("code_challenge", input.codeChallenge);
  url.searchParams.set("code_challenge_method", "S256");
  return url.toString();
}

function parseTokens(
  value: Record<string, unknown>,
  fallbackRefreshToken = "",
  fallbackScopes: string[] = [],
): PackOAuthTokens {
  if (typeof value.access_token !== "string" || !value.access_token) {
    throw new Error("OAuth token response did not contain an access token");
  }
  const expiresIn = Number(value.expires_in);
  if (!Number.isFinite(expiresIn) || expiresIn <= 0) {
    throw new Error("OAuth token response did not contain a valid expiry");
  }
  const scope = typeof value.scope === "string"
    ? value.scope.split(/\s+/).filter(Boolean)
    : fallbackScopes;
  return {
    accessToken: value.access_token,
    ...(typeof value.refresh_token === "string" && value.refresh_token
      ? { refreshToken: value.refresh_token }
      : fallbackRefreshToken
        ? { refreshToken: fallbackRefreshToken }
        : {}),
    expiresAt: new Date(Date.now() + expiresIn * 1_000).toISOString(),
    scopes: scope,
  };
}

export async function exchangePackOAuthCode(input: {
  connection: PackOAuthConnection;
  clientId: string;
  clientSecret: string;
  code: string;
  codeVerifier: string;
  redirectUri: string;
  fetchImpl?: typeof fetch;
}): Promise<PackOAuthTokens> {
  const fetchImpl = input.fetchImpl ?? fetch;
  const response = await fetchImpl(input.connection.tokenUrl, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: formBody({
      grant_type: "authorization_code",
      client_id: input.clientId,
      client_secret: input.clientSecret,
      code: input.code,
      code_verifier: input.codeVerifier,
      redirect_uri: input.redirectUri,
    }),
    signal: AbortSignal.timeout(30_000),
  });
  return parseTokens(await providerJson(response, "OAuth code exchange"), "", input.connection.scopes);
}

export async function refreshPackOAuthToken(input: {
  connection: PackOAuthConnection;
  clientId: string;
  clientSecret: string;
  refreshToken: string;
  scopes: string[];
  fetchImpl?: typeof fetch;
}): Promise<PackOAuthTokens> {
  const fetchImpl = input.fetchImpl ?? fetch;
  const response = await fetchImpl(input.connection.tokenUrl, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: formBody({
      grant_type: "refresh_token",
      client_id: input.clientId,
      client_secret: input.clientSecret,
      refresh_token: input.refreshToken,
    }),
    signal: AbortSignal.timeout(30_000),
  });
  return parseTokens(
    await providerJson(response, "OAuth token refresh"),
    input.refreshToken,
    input.scopes,
  );
}

export async function fetchPackOAuthAccount(input: {
  connection: PackOAuthConnection;
  accessToken: string;
  fetchImpl?: typeof fetch;
}): Promise<{ id: string; label: string }> {
  const fetchImpl = input.fetchImpl ?? fetch;
  const response = await fetchImpl(input.connection.userInfoUrl, {
    headers: { Authorization: `Bearer ${input.accessToken}` },
    signal: AbortSignal.timeout(30_000),
  });
  const value = await providerJson(response, "OAuth account lookup");
  const id = value[input.connection.accountIdField];
  const label = value[input.connection.accountLabelField];
  if (typeof id !== "string" || !id || typeof label !== "string" || !label) {
    throw new Error("OAuth account response did not contain the declared identity fields");
  }
  return { id, label };
}

export function packConnectionHeader(packId: string, connectionKey: string): string {
  return `X-OpenNeko-Pack-${createHash("sha256").update(JSON.stringify([packId, connectionKey])).digest("hex").slice(0, 24)}`;
}
