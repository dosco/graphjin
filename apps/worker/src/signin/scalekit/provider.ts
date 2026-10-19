// Scalekit: OIDC sign-in through Scalekit's hosted gateway (Okta, Entra ID,
// Google Workspace, … behind one integration), SCIM directory sync of one
// Scalekit organization, and deployment-scoped OAuth consent against the
// Scalekit workspace MCP server that the SSO setup page drives. Every
// function takes its settings explicitly; SignInService reads them from the
// `signin.scalekit` section of the secrets store.
import type { ConnectorCredential } from "@open-neko/plugin-install/secrets";
import type {
  ApplyDirectoryChangeParams,
  ApplyDirectoryChangeResult,
  DirectoryCapabilities,
  ListDirectoryParams,
  ListDirectoryResult,
} from "../../directory/types.js";
import type {
  AuthIdentity,
  BeginAuthParams,
  BeginConnectParams,
  CompleteAuthParams,
  CompleteConnectParams,
  SignInSetting,
} from "../types.js";
import { createScalekitClient, type ScalekitClient, type ScalekitUserinfo } from "./client.js";
import { createScalekitDirectory } from "./directory.js";
import { createMcpClient, joinTextContent, type McpToolClient } from "./mcp-client.js";
import {
  createMcpOAuth,
  generatePkceVerifier,
  parseOauthState,
  serializeOauthState,
  type OauthState,
} from "./mcp-oauth.js";

export type ScalekitEnv = Readonly<Record<string, string | undefined>>;

const DEFAULT_MCP_URL = "https://mcp.scalekit.com/";

export const SCALEKIT_SETTINGS: SignInSetting[] = [
  { key: "SCALEKIT_ENVIRONMENT_URL", required: true, secret: false },
  { key: "SCALEKIT_CLIENT_ID", required: true, secret: false },
  { key: "SCALEKIT_CLIENT_SECRET", required: true, secret: true },
  { key: "SCALEKIT_ORGANIZATION_ID", required: false, secret: false },
  { key: "SCALEKIT_ENVIRONMENT_ID", required: false, secret: false },
  { key: "SCALEKIT_MCP_URL", required: false, secret: false },
];

export const SCALEKIT_DIRECTORY: DirectoryCapabilities = {
  providerLabel: "Scalekit directory",
  read: { users: true, groups: true, memberships: true },
  write: { createUser: true, deactivateUser: false },
};

export const SCALEKIT_WORKSPACE_LABEL = "Scalekit workspace";

/**
 * Display list of OAuth scopes for the consent screen. The scopes actually
 * requested are discovered from the MCP server's protected resource metadata.
 */
export const SCALEKIT_WORKSPACE_SCOPES = ["wks:read", "wks:write", "env:read", "env:write", "org:read", "org:write"];

/** Test seam: inject fakes instead of constructing the real clients. */
export interface ScalekitOptions {
  createClient?: (env: ResolvedEnv) => ScalekitClient;
  createMcpOAuth?: () => ReturnType<typeof createMcpOAuth>;
  createDirectory?: (env: ResolvedEnv & { organizationId: string }) => ReturnType<typeof createScalekitDirectory>;
  createMcpClient?: (options: { url: string; accessToken: string }) => Promise<McpToolClient>;
}

export class ScalekitError extends Error {
  constructor(message: string, public override readonly cause?: unknown) {
    super(message);
    this.name = "ScalekitError";
  }
}

interface ResolvedEnv {
  environmentUrl: string;
  clientId: string;
  clientSecret: string;
}

function resolveEnv(env: ScalekitEnv): ResolvedEnv {
  const environmentUrl = env.SCALEKIT_ENVIRONMENT_URL;
  const clientId = env.SCALEKIT_CLIENT_ID;
  const clientSecret = env.SCALEKIT_CLIENT_SECRET;
  const missing: string[] = [];
  if (!environmentUrl) missing.push("SCALEKIT_ENVIRONMENT_URL");
  if (!clientId) missing.push("SCALEKIT_CLIENT_ID");
  if (!clientSecret) missing.push("SCALEKIT_CLIENT_SECRET");
  if (missing.length > 0) {
    throw new ScalekitError(`${missing.join(", ")} not set — enter them on the Single sign-on settings page`);
  }
  return { environmentUrl: environmentUrl!, clientId: clientId!, clientSecret: clientSecret! };
}

function resolveMcpUrl(env: ScalekitEnv): string {
  return (env.SCALEKIT_MCP_URL || DEFAULT_MCP_URL).trim().replace(/\/+$/, "");
}

function client(env: ScalekitEnv, options: ScalekitOptions): ScalekitClient {
  const resolved = resolveEnv(env);
  return (options.createClient ?? ((e) => createScalekitClient(e)))(resolved);
}

const mcpOAuth = (options: ScalekitOptions) => (options.createMcpOAuth ? options.createMcpOAuth() : createMcpOAuth());

// ─── sign-in ───────────────────────────────────────────────────────────

export async function beginScalekitAuth(
  params: BeginAuthParams,
  env: ScalekitEnv,
  options: ScalekitOptions = {},
): Promise<{ authorizationUrl: string }> {
  if (!params.redirectUri) throw new ScalekitError("params.redirectUri is required");
  if (!params.state) throw new ScalekitError("params.state is required");
  const authorizationUrl = client(env, options).buildAuthorizationUrl({
    redirectUri: params.redirectUri,
    state: params.state,
    loginHint: params.loginHint ?? null,
  });
  return { authorizationUrl };
}

export async function completeScalekitAuth(
  params: CompleteAuthParams,
  env: ScalekitEnv,
  options: ScalekitOptions = {},
): Promise<AuthIdentity> {
  if (!params.code) throw new ScalekitError("params.code is required");
  if (!params.redirectUri) throw new ScalekitError("params.redirectUri is required");
  if (!params.state) throw new ScalekitError("params.state is required");
  const scalekit = client(env, options);
  const tokens = await scalekit.exchangeCode({ code: params.code, redirectUri: params.redirectUri });
  return toIdentity(await scalekit.fetchUserinfo(tokens.access_token));
}

function toIdentity(info: ScalekitUserinfo): AuthIdentity {
  const email = info.email ?? "";
  if (!email) {
    // OpenNeko keys app_user off email; an IdP that doesn't return one is
    // not usable here.
    throw new ScalekitError("Scalekit userinfo returned no email — check the IdP's released scopes");
  }
  const displayName = info.name ?? [info.given_name, info.family_name].filter(Boolean).join(" ").trim();
  return {
    sub: info.sub,
    email,
    name: displayName && displayName.length > 0 ? displayName : null,
    orgId: info.organization_id ?? null,
    groups: collectGroups(info),
  };
}

function collectGroups(info: ScalekitUserinfo): string[] {
  // Downstream IdPs surface membership under `groups` (Okta), `roles`
  // (Entra), or both; the union keeps role mapping to one list.
  const out = new Set<string>();
  for (const g of info.groups ?? []) if (typeof g === "string" && g) out.add(g);
  for (const r of info.roles ?? []) if (typeof r === "string" && r) out.add(r);
  return [...out];
}

// ─── directory sync ────────────────────────────────────────────────────

function directory(env: ScalekitEnv, options: ScalekitOptions) {
  const resolved = resolveEnv(env);
  const organizationId = env.SCALEKIT_ORGANIZATION_ID?.trim();
  if (!organizationId) {
    throw new ScalekitError("SCALEKIT_ORGANIZATION_ID not set — select the organization on the Single sign-on settings page");
  }
  return (options.createDirectory ?? createScalekitDirectory)({ ...resolved, organizationId });
}

export async function listScalekitDirectory(
  params: ListDirectoryParams,
  env: ScalekitEnv,
  options: ScalekitOptions = {},
): Promise<ListDirectoryResult> {
  return directory(env, options).list(params);
}

export async function applyScalekitDirectoryChange(
  params: ApplyDirectoryChangeParams,
  env: ScalekitEnv,
  options: ScalekitOptions = {},
): Promise<ApplyDirectoryChangeResult> {
  return directory(env, options).apply(params);
}

// ─── workspace consent (deployment-scoped MCP-OAuth) ───────────────────

/**
 * Begin the MCP-OAuth-2.1 dance: discover the protected resource + AS
 * metadata, register the client (DCR), build the PKCE authorization URL,
 * and serialize the in-flight state into oauthState for the web to echo
 * back on complete.
 */
export async function beginScalekitConnect(
  params: BeginConnectParams,
  env: ScalekitEnv,
  options: ScalekitOptions = {},
): Promise<{ authorizationUrl: string; oauthState: string }> {
  if (!params.redirectUri) throw new ScalekitError("params.redirectUri is required");
  if (!params.state) throw new ScalekitError("params.state is required");
  const oauth = mcpOAuth(options);
  const mcpUrl = resolveMcpUrl(env);
  const resource = await oauth.discoverResource(mcpUrl);
  const asUrl = resource.authorizationServers[0];
  if (!asUrl) throw new ScalekitError(`Scalekit MCP server at ${mcpUrl} advertised no authorization server`);
  const as = await oauth.discoverAuthorizationServer(asUrl);
  let clientId = "";
  let clientSecret: string | null = null;
  if (as.registrationEndpoint) {
    const registered = await oauth.registerClient(as.registrationEndpoint, {
      clientName: "openneko-scalekit",
      redirectUris: [params.redirectUri],
    });
    clientId = registered.clientId;
    clientSecret = registered.clientSecret;
  }
  if (!clientId) throw new ScalekitError("Scalekit MCP authorization server did not support client registration (DCR)");
  const codeVerifier = params.codeVerifier ?? generatePkceVerifier();
  // The authorization server's discovered scopes are the truth: requesting
  // less than the full menu yields a token that fails per-tool scope checks.
  const scopes = resource.scopesSupported;
  const authorizationUrl = oauth.buildAuthorizationUrl({
    authorizationEndpoint: as.authorizationEndpoint,
    clientId,
    redirectUri: params.redirectUri,
    state: params.state,
    codeVerifier,
    scopes,
  });
  const oauthState: OauthState = {
    mcpUrl,
    authorizationServerUrl: asUrl,
    tokenEndpoint: as.tokenEndpoint,
    clientId,
    clientSecret,
    codeVerifier,
    scopes,
    redirectUri: params.redirectUri,
  };
  return { authorizationUrl, oauthState: serializeOauthState(oauthState) };
}

/**
 * Exchange the authorization code using the echoed oauthState, returning a
 * credential whose tokens carry everything refresh needs.
 */
export async function completeScalekitConnect(
  params: CompleteConnectParams,
  options: ScalekitOptions = {},
): Promise<ConnectorCredential> {
  if (!params.code) throw new ScalekitError("params.code is required");
  const state = parseOauthState(params.oauthState);
  const tokens = await mcpOAuth(options).exchangeCode({
    tokenEndpoint: state.tokenEndpoint,
    clientId: state.clientId,
    clientSecret: state.clientSecret,
    code: params.code,
    redirectUri: params.redirectUri || state.redirectUri,
    codeVerifier: params.codeVerifier ?? state.codeVerifier,
  });
  return {
    tokens: {
      access_token: tokens.access_token,
      refresh_token: tokens.refresh_token ?? null,
      expires_at: tokens.expires_in != null ? String(Date.now() + tokens.expires_in * 1000) : null,
      token_endpoint: state.tokenEndpoint,
      client_id: state.clientId,
      client_secret: state.clientSecret ?? "",
    },
    scopes: state.scopes.length > 0 ? state.scopes : undefined,
    providerLabel: SCALEKIT_WORKSPACE_LABEL,
    connectedAt: new Date().toISOString(),
  };
}

/** Rotate the deployment's refresh token; the caller persists the result. */
export async function refreshScalekitConnect(
  current: ConnectorCredential,
  options: ScalekitOptions = {},
): Promise<ConnectorCredential> {
  const t = current.tokens as Record<string, unknown>;
  const tokenEndpoint = str(t.token_endpoint);
  const clientId = str(t.client_id);
  const refreshToken = str(t.refresh_token);
  if (!tokenEndpoint || !clientId || !refreshToken) {
    throw new ScalekitError("stored credential is missing token_endpoint/client_id/refresh_token — reconnect the workspace");
  }
  const tokens = await mcpOAuth(options).refreshTokens({
    tokenEndpoint,
    clientId,
    clientSecret: strOrNull(t.client_secret),
    refreshToken,
  });
  return {
    ...current,
    tokens: {
      ...t,
      access_token: tokens.access_token,
      refresh_token: tokens.refresh_token ?? refreshToken,
      expires_at: tokens.expires_in != null ? String(Date.now() + tokens.expires_in * 1000) : t.expires_at,
    },
    refreshedAt: new Date().toISOString(),
  };
}

/** Whether a stored credential expires within the next minute. */
export function scalekitCredentialNearExpiry(credential: ConnectorCredential, nowMs = Date.now()): boolean {
  const expiresAt = num((credential.tokens as Record<string, unknown>).expires_at);
  return expiresAt !== null && expiresAt <= nowMs + 60_000;
}

/**
 * Call one Scalekit workspace MCP tool with the deployment credential and
 * return its text. Throws when the workspace is not connected or the tool
 * reports an error.
 */
export async function callScalekitTool(
  tool: string,
  args: Record<string, unknown>,
  credential: ConnectorCredential | null,
  env: ScalekitEnv,
  options: ScalekitOptions = {},
): Promise<string> {
  const accessToken = str((credential?.tokens as Record<string, unknown> | undefined)?.access_token);
  if (!accessToken) {
    throw new ScalekitError("Scalekit workspace is not connected — authorize it on the Single sign-on settings page first");
  }
  const make = options.createMcpClient ?? ((opts) => createMcpClient(opts));
  const mcp = await make({ url: resolveMcpUrl(env), accessToken });
  try {
    const result = await mcp.callTool(tool, args);
    const text = joinTextContent(result);
    if (result.isError === true) throw new ScalekitError(text || `Scalekit tool ${tool} failed`);
    if (!text) throw new ScalekitError(`Scalekit tool ${tool} returned no result — is the workspace connected?`);
    return text;
  } finally {
    await mcp.close().catch(() => {});
  }
}

function str(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function strOrNull(value: unknown): string | null {
  return typeof value === "string" && value.length > 0 ? value : null;
}

function num(value: unknown): number | null {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string" && /^\d+$/.test(value)) return Number(value);
  return null;
}
