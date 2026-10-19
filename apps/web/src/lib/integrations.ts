/**
 * Per-operator connector helpers (the `connect` capability), mirroring
 * the auth singleton helpers but non-singleton: each operator
 * authorises each connector independently. The web app drives the
 * OAuth dance; the worker runs the matching connector (for example the
 * built-in Scalekit workspace connector) and persists tokens under the
 * operator's slot in the secrets file. The `pluginName`/`pluginId` wire
 * fields keep their old names for compatibility; their values are
 * connector ids.
 */

import "server-only";
import { createHash, createHmac, randomBytes, timingSafeEqual } from "node:crypto";
import { cookies } from "next/headers";
import { publicBaseUrl } from "@/lib/public-url";

const CONNECT_COOKIE_NAME = "openneko_connect_state";
const CONNECT_TTL_SECONDS = 10 * 60;

export interface ConnectProvider {
  pluginId: string;
  pluginName: string;
  providerLabel: string;
  scopes: string[];
  flow: string;
  credentialScope: string;
}

export interface ConnectStatus {
  pluginName: string;
  connectedAt: string;
  scopes?: string[];
}

export interface ConnectorCredential {
  tokens: Record<string, unknown>;
  scopes?: string[];
  providerLabel?: string;
  connectedAt: string;
  refreshedAt?: string;
}

function workerAdminBase(): string {
  const raw = process.env.WORKER_ADMIN_URL ?? "http://127.0.0.1:4100";
  return raw.replace(/\/+$/, "");
}

function sessionSecret(): string {
  const secret = process.env.OPENNEKO_SESSION_SECRET;
  if (!secret || secret.length < 32) {
    throw new Error(
      "OPENNEKO_SESSION_SECRET must be set to a value at least 32 characters long",
    );
  }
  return secret;
}

export async function listConnectProviders(): Promise<ConnectProvider[]> {
  try {
    const res = await fetch(`${workerAdminBase()}/admin/connect/providers`, {
      cache: "no-store",
      signal: AbortSignal.timeout(2000),
    });
    if (!res.ok) return [];
    const body = (await res.json()) as { providers?: ConnectProvider[] };
    return body.providers ?? [];
  } catch {
    return [];
  }
}

export async function getOperatorConnectStatus(
  operatorId: string,
): Promise<ConnectStatus[]> {
  try {
    const res = await fetch(
      `${workerAdminBase()}/admin/connect/status/${encodeURIComponent(operatorId)}`,
      { cache: "no-store", signal: AbortSignal.timeout(2000) },
    );
    if (!res.ok) return [];
    const body = (await res.json()) as { connected?: ConnectStatus[] };
    return body.connected ?? [];
  } catch {
    return [];
  }
}

export async function getDeploymentConnectStatus(): Promise<ConnectStatus[]> {
  try {
    const res = await fetch(
      `${workerAdminBase()}/admin/connect/deployment/status`,
      { cache: "no-store", signal: AbortSignal.timeout(2000) },
    );
    if (!res.ok) return [];
    const body = (await res.json()) as { connected?: ConnectStatus[] };
    return body.connected ?? [];
  } catch {
    return [];
  }
}

export async function beginConnect(
  pluginName: string,
  params: {
    operatorId: string;
    redirectUri: string;
    state: string;
    scopes: string[];
    codeVerifier: string;
  },
): Promise<{ authorizationUrl: string; oauthState?: string }> {
  const res = await fetch(`${workerAdminBase()}/admin/connect/begin`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ pluginName, params }),
    // First call after install pays the sandbox cold-start plus MCP-OAuth
    // discovery + DCR round-trips — 15s is not enough on the first click.
    signal: AbortSignal.timeout(60_000),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new Error(
      `connect begin failed (${res.status}): ${text || res.statusText}`,
    );
  }
  return (await res.json()) as { authorizationUrl: string; oauthState?: string };
}

export async function completeConnect(
  pluginName: string,
  params: {
    operatorId: string;
    code: string;
    redirectUri: string;
    state: string;
    codeVerifier: string;
    scopes: string[];
    oauthState?: string;
  },
): Promise<ConnectorCredential> {
  const res = await fetch(`${workerAdminBase()}/admin/connect/complete`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ pluginName, params }),
    signal: AbortSignal.timeout(60_000),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new Error(
      `connect complete failed (${res.status}): ${text || res.statusText}`,
    );
  }
  const body = (await res.json()) as { credential: ConnectorCredential };
  return body.credential;
}

export async function disconnectConnector(
  pluginName: string,
  operatorId: string,
): Promise<boolean> {
  const res = await fetch(`${workerAdminBase()}/admin/connect/disconnect`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ pluginName, operatorId }),
    signal: AbortSignal.timeout(5000),
  });
  if (!res.ok) return false;
  const body = (await res.json()) as { removed?: boolean };
  return Boolean(body.removed);
}

/**
 * Deployment-scoped connect uses a reserved operator slot — every admin
 * sees the same org-wide credential.
 */
export function disconnectDeploymentConnector(
  pluginName: string,
): Promise<boolean> {
  return disconnectConnector(pluginName, DEPLOYMENT_CONNECT_OPERATOR_ID);
}

/** Reserved operator id for deployment-scoped connect flows. */
export const DEPLOYMENT_CONNECT_OPERATOR_ID = "deployment";

// ─── State cookie ──────────────────────────────────────────────────────

/**
 * Cookie payload encodes the data the callback needs to verify it's
 * looking at the original flow: the connector we asked to authorize,
 * the operator who started the flow, the state token the IdP echoes
 * back, the PKCE code_verifier paired with the challenge sent to the
 * IdP, and the path to send the browser back to on success.
 */
export interface ConnectStateCookiePayload {
  pluginName: string;
  operatorId: string;
  state: string;
  codeVerifier: string;
  returnPath: string;
  /** mcp-oauth: opaque state the connector returned from begin (DCR + PKCE + endpoints). */
  oauthState?: string;
}

export function newStateToken(): string {
  return randomBytes(24).toString("base64url");
}

export function newPkceVerifier(): string {
  // RFC 7636: 43-128 chars, [A-Z / a-z / 0-9 / "-" / "." / "_" / "~"].
  // 32 random bytes → 43-char base64url is the most common shape.
  return randomBytes(32).toString("base64url");
}

export function pkceChallenge(verifier: string): string {
  return createHash("sha256").update(verifier).digest("base64url");
}

export async function writeConnectStateCookie(
  payload: ConnectStateCookiePayload,
): Promise<void> {
  const encoded = JSON.stringify(payload);
  const b64 = Buffer.from(encoded, "utf8").toString("base64url");
  const mac = createHmac("sha256", sessionSecret()).update(b64).digest("base64url");
  const jar = await cookies();
  jar.set(CONNECT_COOKIE_NAME, `${b64}.${mac}`, {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "lax",
    path: "/",
    maxAge: CONNECT_TTL_SECONDS,
  });
}

export async function readAndClearConnectStateCookie(): Promise<ConnectStateCookiePayload | null> {
  const jar = await cookies();
  const raw = jar.get(CONNECT_COOKIE_NAME)?.value;
  if (!raw) return null;
  jar.delete(CONNECT_COOKIE_NAME);
  const split = raw.lastIndexOf(".");
  if (split <= 0) return null;
  const b64 = raw.slice(0, split);
  const mac = raw.slice(split + 1);
  const expected = createHmac("sha256", sessionSecret()).update(b64).digest("base64url");
  const a = Buffer.from(mac);
  const b = Buffer.from(expected);
  if (a.length !== b.length || !timingSafeEqual(a, b)) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(Buffer.from(b64, "base64url").toString("utf8"));
  } catch {
    return null;
  }
  if (!isStatePayload(parsed)) return null;
  return parsed;
}

function isStatePayload(value: unknown): value is ConnectStateCookiePayload {
  if (typeof value !== "object" || value === null) return false;
  const v = value as Record<string, unknown>;
  return (
    typeof v.pluginName === "string" &&
    typeof v.operatorId === "string" &&
    typeof v.state === "string" &&
    typeof v.codeVerifier === "string" &&
    typeof v.returnPath === "string" &&
    (v.oauthState === undefined || typeof v.oauthState === "string")
  );
}

/**
 * Build the absolute callback URL the IdP will redirect to. Computed
 * from the current request URL so dev and prod work with the same
 * code path. Operators that want a canonical host (e.g. behind a load
 * balancer that strips Host headers) set OPENNEKO_PUBLIC_URL.
 */
export function buildConnectCallbackUri(requestUrl: string, pluginName: string): string {
  return `${publicBaseUrl(requestUrl)}/api/integrations/connect/${encodeURIComponent(pluginName)}/callback`;
}
