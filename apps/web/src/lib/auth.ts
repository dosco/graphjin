/**
 * SSO integration for the web app.
 *
 * Without a sign-in provider OpenNeko runs in single-operator mode —
 * fine for a laptop deployment, fatal for any org with an IdP. This
 * module wires the web's sign-in flow through the provider the admin
 * chose (Scalekit SSO or email-link sign-in) so an enterprise operator
 * gets standard OIDC SSO without the web knowing which IdP they use.
 *
 * Topology: the providers run in the worker, not in this Next process.
 * We reach them through the worker's admin HTTP endpoint on localhost
 * (loopback inside the deployment, never exposed). That gives us:
 *   - Secrets stay in the worker's encrypted secrets store.
 *   - The web process never needs the IdP client_secret.
 *
 * Session model: a stateless signed cookie. The session_id is the
 * app_user.id; an HMAC over `${user_id}.${expiresAt}` prevents
 * tampering and binds the cookie to OPENNEKO_SESSION_SECRET. Logging
 * a user out is a cookie delete; rotating the secret invalidates
 * every existing session globally.
 */

import { createHmac, randomBytes, timingSafeEqual } from "node:crypto";
import { deriveSigningSecret } from "@neko/secret-crypt";
import {
  readAuthGateMarker,
  registerAuthGateCacheReset,
  resetAuthGateCaches,
  writeAuthGateMarker,
  type PersistedAuthProvider,
} from "./auth-gate-marker";
import { cookies } from "next/headers";
import {
  and,
  app_user,
  getOrCreateSoloAdmin,
  isUnclaimedSoloEmail,
  db,
  eq,
  isNull,
  sql,
  ADMINISTRATORS_GROUP_SLUG,
  user_group_membership,
  GroupError,
  reconcileSignInGroups,
  user_group,
} from "@neko/db";
import { getOrgId } from "@/lib/db";
import { publicBaseUrl } from "@/lib/public-url";

export const SESSION_COOKIE_NAME = "openneko_session";
export const STATE_COOKIE_NAME = "openneko_sso_state";

/** Session lifetime — 12h. Re-auths after this; refresh-on-use is a v2. */
const SESSION_TTL_SECONDS = 60 * 60 * 12;
/** State cookie lifetime — short window for the user to complete the IdP dance. */
const STATE_TTL_SECONDS = 10 * 60;

export interface AuthProviderInfo {
  pluginName: string;
  providerLabel: string;
  /**
   * "manual": the provider's identity is self-asserted (magic link proves
   * mailbox possession only), so sign-in is restricted to users an admin
   * pre-provisioned. "automatic": IdP-attested — first sign-in may create
   * the app_user. Older workers omit the field; treat as "automatic".
   */
  provisioning?: "automatic" | "manual";
  /** Sign-in cannot start without an email (e.g. magic link). */
  loginHintRequired?: boolean;
}

/**
 * The chosen sign-in provider while it is not live yet, and why. Admin-only
 * surface (setup UI); the public status route reduces it to a label.
 */
export interface PendingAuthProviderInfo extends AuthProviderInfo {
  /** Required env keys still unset — names only, never values. */
  missingEnv: string[];
  /** Manual provisioning with zero active admin users provisioned. */
  needsAdminUser: boolean;
  /** Declared env keys that currently hold a value — names only. */
  configuredEnv: string[];
}

export interface AuthGateStatus {
  provider: AuthProviderInfo | null;
  pending: PendingAuthProviderInfo | null;
}

export interface AuthIdentity {
  sub: string;
  email: string;
  name?: string | null;
  orgId?: string | null;
  groups?: Array<string | { id: string; name?: string | null }>;
}

export interface SessionPayload {
  userId: string;
  email: string;
  name: string | null;
  expiresAt: number;
}

function workerAdminBase(): string {
  const raw = process.env.WORKER_ADMIN_URL ?? "http://127.0.0.1:4100";
  return raw.replace(/\/+$/, "");
}

function sessionSecret(): string {
  // OPENNEKO_SESSION_SECRET is the HMAC key for session cookies. When it
  // is not set (no compose file sets it), derive a stable per-install
  // secret from the deployment secret-key: consistent across restarts and
  // across web instances sharing the config volume — a process-random
  // fallback would invalidate sessions on every restart. An explicitly
  // set but too-short value is still a hard error: that is a
  // misconfiguration, not an absence.
  const secret = process.env.OPENNEKO_SESSION_SECRET;
  if (secret !== undefined && secret.length < 32) {
    throw new Error(
      "OPENNEKO_SESSION_SECRET must be at least 32 characters long for SSO sessions",
    );
  }
  if (secret) return secret;
  return deriveSigningSecret("session-cookie:v1").toString("base64");
}

/**
 * Ask the worker which sign-in provider is live. Cached for one second
 * to keep the sign-in page snappy under concurrent loads.
 */
let providerCache: { value: AuthGateStatus; at: number } | null = null;
const PROVIDER_CACHE_TTL_MS = 1000;

const NO_GATE: AuthGateStatus = { provider: null, pending: null };

/**
 * Full sign-in gate status: the live provider (null while setup is
 * incomplete) plus the pending provider with its missing pieces. Admin
 * surfaces consume `pending`; everything else should keep using
 * getAuthProvider().
 */
export async function getAuthGateStatus(): Promise<AuthGateStatus> {
  const now = Date.now();
  if (providerCache && now - providerCache.at < PROVIDER_CACHE_TTL_MS) {
    return providerCache.value;
  }
  try {
    const res = await fetch(`${workerAdminBase()}/admin/auth/status`, {
      method: "GET",
      cache: "no-store",
      signal: AbortSignal.timeout(2000),
    });
    if (!res.ok) {
      providerCache = { value: gateWhileWorkerUnavailable(), at: now };
      return providerCache.value;
    }
    const body = (await res.json()) as AuthGateStatus;
    providerCache = {
      value: { provider: body.provider ?? null, pending: body.pending ?? null },
      at: now,
    };
    writeAuthGateMarker({
      provider: (body.provider as PersistedAuthProvider | null) ?? null,
    });
    return providerCache.value;
  } catch {
    providerCache = { value: gateWhileWorkerUnavailable(), at: now };
    return providerCache.value;
  }
}

/**
 * Worker unreachable (booting, restarting, or slow): the last persisted
 * live answer decides. An install where SSO was ever live fails CLOSED —
 * synthesizing the persisted provider keeps requireAdminActor denying
 * session-less requests while valid session cookies verify locally. An
 * install that never configured SSO keeps the solo-operator behavior.
 */
function gateWhileWorkerUnavailable(): AuthGateStatus {
  const marker = readAuthGateMarker();
  if (marker?.provider) {
    return {
      provider: marker.provider as unknown as AuthProviderInfo,
      pending: null,
    };
  }
  return NO_GATE;
}

export async function getAuthProvider(): Promise<AuthProviderInfo | null> {
  return (await getAuthGateStatus()).provider;
}

registerAuthGateCacheReset(() => {
  providerCache = null;
});

/**
 * Clears every auth-gate cache (this module's, the proxy's, and the
 * persisted-marker copy) — SSO settings changes must be visible on the
 * very next request in all of them.
 */
export function _resetAuthProviderCache() {
  resetAuthGateCaches();
}

/**
 * Choose the sign-in provider ("scalekit" or "magic-link"); null turns
 * sign-in off. Admin routes only — callers must already be behind
 * requireAdminActor.
 */
export async function selectSignInProvider(
  provider: "scalekit" | "magic-link" | null,
): Promise<void> {
  const res = await fetch(`${workerAdminBase()}/admin/auth/provider`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ provider }),
    signal: AbortSignal.timeout(10_000),
  });
  if (!res.ok) {
    const body = (await res.json().catch(() => null)) as { error?: string } | null;
    throw new Error(body?.error ?? `sign-in provider change failed (${res.status})`);
  }
  _resetAuthProviderCache();
}

/**
 * Write (string) or delete (null) values of the chosen sign-in provider,
 * restricted worker-side to its declared keys. Admin routes only —
 * callers must already be behind requireAdminActor.
 */
export async function setSignInSecrets(
  values: Record<string, string | null>,
): Promise<void> {
  const res = await fetch(`${workerAdminBase()}/admin/auth/secrets`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ values }),
    signal: AbortSignal.timeout(10_000),
  });
  if (!res.ok) {
    const body = (await res.json().catch(() => null)) as {
      error?: string;
    } | null;
    throw new Error(
      body?.error ?? `auth secrets write failed (${res.status})`,
    );
  }
  // The gate may have just flipped — drop the 1s cache so the next
  // status read reflects it immediately.
  _resetAuthProviderCache();
}

export async function beginAuth(params: {
  redirectUri: string;
  state: string;
  loginHint?: string | null;
}): Promise<{ authorizationUrl: string }> {
  const res = await fetch(`${workerAdminBase()}/admin/auth/begin`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(params),
    // Sending the email-link can take a while on a slow provider. Giving
    // up early sends no email at all, which reads to the person as silence.
    signal: AbortSignal.timeout(45_000),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new Error(
      `sign-in begin failed (${res.status}): ${text || res.statusText}`,
    );
  }
  return (await res.json()) as { authorizationUrl: string };
}

/**
 * Built-in governed action descriptors (records actions). /work hands
 * these to the agent's tool builder so it can register one MCP tool per
 * kind. Best-effort: an unreachable worker yields an empty list (the
 * agent simply won't have those tools that turn).
 */
export interface ActionDescriptor {
  kind: string;
  description: string;
  pluginName?: string;
  scope?: "external" | "internal";
  default_mode?:
    | "auto"
    | "ask"
    | "deny"
    | {
        external?: "auto" | "ask" | "deny";
        internal?: "auto" | "ask" | "deny";
      };
  example?: Record<string, unknown>;
}

export async function getActionDescriptors(): Promise<ActionDescriptor[]> {
  try {
    const res = await fetch(`${workerAdminBase()}/admin/action-descriptors`, {
      method: "GET",
      cache: "no-store",
      signal: AbortSignal.timeout(2000),
    });
    if (!res.ok) return [];
    const body = (await res.json()) as { descriptors?: ActionDescriptor[] };
    return body.descriptors ?? [];
  } catch {
    return [];
  }
}

export async function completeAuth(params: {
  code: string;
  redirectUri: string;
  state: string;
}): Promise<AuthIdentity> {
  const res = await fetch(`${workerAdminBase()}/admin/auth/complete`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(params),
    signal: AbortSignal.timeout(30_000),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new Error(
      `sign-in complete failed (${res.status}): ${text || res.statusText}`,
    );
  }
  const body = (await res.json()) as { identity: AuthIdentity };
  return body.identity;
}

/**
 * Mint a state token: random nonce paired with the path the user was
 * trying to reach. The path is stashed in the cookie too so callback
 * can redirect back without trusting an open redirect parameter.
 */
export function newStateToken(): string {
  return randomBytes(24).toString("base64url");
}

/**
 * Upsert `app_user` for an SSO identity. Match priority:
 *   1. existing row with the same `sub` (IdP-stable);
 *   2. existing row with the same `email` (initial migration when an
 *      operator first turns SSO on against a pre-existing email-only
 *      user) — `sub` is then attached for future logins.
 * Otherwise insert a new row in the only org — unless the provider
 * declares manual provisioning, in which case an unmatched identity is
 * rejected: possession of a mailbox must never mint an account.
 */
export async function upsertUserFromIdentity(
  identity: AuthIdentity,
): Promise<{ id: string; email: string; name: string | null }> {
  const orgId = await getOrgId();
  // Use the full gate status so the manual-provisioning backstop holds
  // even while the provider is still pending (not yet live).
  const gate = await getAuthGateStatus();
  const providerInfo = gate.provider ?? gate.pending;
  const provider = providerInfo?.pluginName ?? "oidc";
  // Serialize sign-ins per org. Two concurrent first sign-ins (two tabs,
  // an IdP callback retry) could each miss both lookups AND both pass the
  // has-admin check — minting duplicate identities and duplicate admins,
  // after which later sign-ins threw permanently. Under the lock the
  // second caller simply finds the first's row; the unique indexes from
  // migration 0061 are the loud backstop should the lock ever be bypassed.
  const resolved = await db().transaction(async (tx) => {
    await tx.execute(
      sql`select pg_advisory_xact_lock(hashtext(${"openneko.app_user:" + orgId}))`,
    );
    // Primary lookup: sub. Anything matching wins, regardless of email
    // changes (people get married, change addresses — sub doesn't).
    const bySub = await tx
      .select({
        id: app_user.id,
        email: app_user.email,
        name: app_user.name,
      })
      .from(app_user)
      .where(and(eq(app_user.org_id, orgId), eq(app_user.sub, identity.sub)))
      .limit(1);
    if (bySub[0]) {
      await tx
        .update(app_user)
        .set({
          email: identity.email,
          name: identity.name ?? null,
          last_login_at: new Date(),
          updated_at: new Date(),
        })
        .where(eq(app_user.id, bySub[0].id));
      return { id: bySub[0].id, name: identity.name ?? null };
    }
    // Migration lookup: same email, no sub yet. Attach the sub.
    const byEmail = await tx
      .select({
        id: app_user.id,
        email: app_user.email,
        name: app_user.name,
        sub: app_user.sub,
      })
      .from(app_user)
      .where(and(eq(app_user.org_id, orgId), sql`lower(${app_user.email}) = ${identity.email.trim().toLowerCase()}`))
      .limit(1);
    if (byEmail[0] && !byEmail[0].sub) {
      await tx
        .update(app_user)
        .set({
          sub: identity.sub,
          name: identity.name ?? byEmail[0].name ?? null,
          last_login_at: new Date(),
          updated_at: new Date(),
        })
        .where(eq(app_user.id, byEmail[0].id));
      return { id: byEmail[0].id, name: identity.name ?? byEmail[0].name ?? null };
    }
    if (byEmail[0]) {
      // Same email, *different* sub already on file. Refuse rather than
      // silently take over — the operator should resolve this manually
      // (likely two IdP accounts pointing at the same mailbox).
      throw new Error(
        `app_user.email ${identity.email} is already bound to a different SSO subject; remove the row or update sub manually before re-attempting`,
      );
    }
    if (providerInfo?.provisioning === "manual") {
      // Self-asserted identity (e.g. magic link) with no pre-provisioned
      // user. The begin route already drops unknown emails silently; this
      // is the defense-in-depth backstop.
      throw new Error(
        `no user is provisioned for ${identity.email} — ask an administrator to add you`,
      );
    }
    // Brand new user. Bootstrap the first active admin so installing an SSO
    // plugin cannot leave an org with no administrator.
    const newId = `usr_${randomBytes(9).toString("base64url")}`;
    await tx.insert(app_user).values({
      id: newId,
      sub: identity.sub,
      email: identity.email,
      name: identity.name ?? null,
      org_id: orgId,
      source: provider,
      last_login_at: new Date(),
    });
    // An IdP group name never grants administration; an IdP rule or an
    // administrator does. The first account of an org with no administrator
    // still gets one, so a sign-in plugin cannot leave an org locked out.
    if (!(await orgHasActiveAdmin(orgId, tx))) {
      await tx.execute(sql`
        insert into user_group_membership (org_id, group_id, user_id, source)
        select ${orgId}, g.id, ${newId}, 'local' from user_group g
        where g.org_id = ${orgId} and g.slug = ${ADMINISTRATORS_GROUP_SLUG}
        on conflict do nothing`);
    }
    return { id: newId, name: identity.name ?? null };
  });
  await syncIdentityGroups({ orgId, userId: resolved.id, identity });
  return {
    id: resolved.id,
    email: identity.email,
    name: resolved.name,
  };
}


async function orgHasActiveAdmin(
  orgId: string,
  runner: Pick<ReturnType<typeof db>, "select">= db(),
): Promise<boolean> {
  const [admin] = await runner
    .select({ id: app_user.id })
    .from(app_user)
    .innerJoin(user_group_membership, eq(user_group_membership.user_id, app_user.id))
    .innerJoin(user_group, eq(user_group.id, user_group_membership.group_id))
    .where(
      and(
        eq(app_user.org_id, orgId),
        eq(user_group.slug, ADMINISTRATORS_GROUP_SLUG),
        isNull(app_user.disabled_at),
      ),
    )
    .limit(1);
  return Boolean(admin);
}


function normalizedIdentityGroups(identity: AuthIdentity): Array<{
  externalId: string;
  displayName: string | null;
}> {
  const groups = new Map<string, string | null>();
  for (const group of identity.groups ?? []) {
    const externalId = (typeof group === "string" ? group : group.id).trim();
    if (!externalId) continue;
    const displayName = typeof group === "string"
      ? group
      : group.name?.trim() || null;
    groups.set(externalId, displayName);
  }
  return [...groups].map(([externalId, displayName]) => ({
    externalId,
    displayName,
  }));
}

/**
 * Sign-in claim sync. Records the user's IdP groups and applies IdP rules.
 * A sync that would remove the last administrator is skipped so sign-in
 * still succeeds with the previous groups.
 */
async function syncIdentityGroups(input: {
  orgId: string;
  userId: string;
  identity: AuthIdentity;
}): Promise<void> {
  const provider = (await getAuthProvider())?.pluginName ?? "oidc";
  const tenantId = input.identity.orgId?.trim() || input.orgId;
  try {
    await reconcileSignInGroups({
      orgId: input.orgId,
      userId: input.userId,
      provider,
      tenantId,
      groups: normalizedIdentityGroups(input.identity),
    });
  } catch (err) {
    if (err instanceof GroupError && err.code === "lockout") {
      console.warn(`[auth] kept previous groups for ${input.userId}: ${err.message}`);
      return;
    }
    throw err;
  }
}

export function encodeSession(payload: SessionPayload): string {
  const body = `${payload.userId}.${payload.expiresAt}`;
  const mac = createHmac("sha256", sessionSecret()).update(body).digest("base64url");
  return `${body}.${mac}`;
}

export function decodeSession(value: string): SessionPayload | null {
  const parts = value.split(".");
  if (parts.length !== 3) return null;
  const [userId, expiresAtRaw, mac] = parts;
  if (!userId || !expiresAtRaw || !mac) return null;
  const body = `${userId}.${expiresAtRaw}`;
  const expected = createHmac("sha256", sessionSecret())
    .update(body)
    .digest("base64url");
  const a = Buffer.from(mac);
  const b = Buffer.from(expected);
  if (a.length !== b.length) return null;
  if (!timingSafeEqual(a, b)) return null;
  const expiresAt = Number.parseInt(expiresAtRaw, 10);
  if (!Number.isFinite(expiresAt)) return null;
  if (expiresAt < Math.floor(Date.now() / 1000)) return null;
  return { userId, expiresAt, email: "", name: null };
}

export async function writeSessionCookie(userId: string) {
  const expiresAt = Math.floor(Date.now() / 1000) + SESSION_TTL_SECONDS;
  const token = encodeSession({ userId, expiresAt, email: "", name: null });
  const jar = await cookies();
  jar.set(SESSION_COOKIE_NAME, token, {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "lax",
    path: "/",
    maxAge: SESSION_TTL_SECONDS,
  });
}

export async function clearSessionCookie() {
  const jar = await cookies();
  jar.delete(SESSION_COOKIE_NAME);
}

export async function writeStateCookie(state: string, returnPath: string) {
  // State + intended landing path are kept server-side via a signed
  // cookie so the callback can verify both without an extra DB lookup
  // and without trusting the redirect_uri query string.
  const payload = `${state}|${encodeReturnPath(returnPath)}`;
  const mac = createHmac("sha256", sessionSecret()).update(payload).digest("base64url");
  const value = `${payload}.${mac}`;
  const jar = await cookies();
  jar.set(STATE_COOKIE_NAME, value, {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "lax",
    path: "/",
    maxAge: STATE_TTL_SECONDS,
  });
}

export async function readAndClearStateCookie(): Promise<
  { state: string; returnPath: string } | null
> {
  const jar = await cookies();
  const raw = jar.get(STATE_COOKIE_NAME)?.value;
  if (!raw) return null;
  jar.delete(STATE_COOKIE_NAME);
  const split = raw.lastIndexOf(".");
  if (split <= 0) return null;
  const payload = raw.slice(0, split);
  const mac = raw.slice(split + 1);
  const expected = createHmac("sha256", sessionSecret())
    .update(payload)
    .digest("base64url");
  const a = Buffer.from(mac);
  const b = Buffer.from(expected);
  if (a.length !== b.length || !timingSafeEqual(a, b)) return null;
  const pipe = payload.indexOf("|");
  if (pipe <= 0) return null;
  const state = payload.slice(0, pipe);
  const returnPath = decodeReturnPath(payload.slice(pipe + 1));
  return { state, returnPath };
}

function encodeReturnPath(p: string): string {
  // Whitelist to internal paths only. An attacker who controls the
  // sign-in link cannot smuggle an external redirect through the
  // state cookie.
  if (!p.startsWith("/") || p.startsWith("//")) return "/";
  return Buffer.from(p).toString("base64url");
}

function decodeReturnPath(encoded: string): string {
  try {
    const raw = Buffer.from(encoded, "base64url").toString("utf8");
    if (!raw.startsWith("/") || raw.startsWith("//")) return "/";
    return raw;
  } catch {
    return "/";
  }
}

/**
 * Resolve the local solo account, or the SSO session (cookie → DB lookup → user). Returns
 * null when no session, an expired session, or a session whose user
 * row has been deleted (IT deprovisioned them in the IdP and a
 * background sweep deleted the row). Pages calling this can choose
 * to redirect to /signin or render an unauthenticated view.
 */
export async function getCurrentUser(): Promise<{
  id: string;
  email: string;
  name: string | null;
} | null> {
  // Solo authentication still resolves to a real account. Never infer a
  // another user as the operator once SSO is live.
  if (!(await getAuthProvider())) {
    const owner = await getOrCreateSoloAdmin(await getOrgId());
    return owner ? { id: owner.id, email: isUnclaimedSoloEmail(owner.email) ? "" : owner.email, name: owner.name } : null;
  }
  const jar = await cookies();
  const token = jar.get(SESSION_COOKIE_NAME)?.value;
  if (!token) return null;
  const session = decodeSession(token);
  if (!session) return null;
  const rows = await db()
    .select({
      id: app_user.id,
      email: app_user.email,
      name: app_user.name,
    })
    .from(app_user)
    // ADM1: a deactivated user's cookie is dead, not just their sign-in.
    .where(and(eq(app_user.id, session.userId), isNull(app_user.disabled_at)))
    .limit(1);
  if (!rows[0]) return null;
  return rows[0];
}

/**
 * Build the absolute callback URL the IdP will redirect to. We compute
 * it from the incoming request rather than env so dev and prod work
 * with the same code. Operators wanting to force a canonical host can
 * set OPENNEKO_PUBLIC_URL (e.g. behind a load balancer that strips
 * Host headers).
 */
export function buildRedirectUri(requestUrl: string): string {
  return `${publicBaseUrl(requestUrl)}/api/auth/callback`;
}
