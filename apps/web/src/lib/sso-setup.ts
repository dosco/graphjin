// SSO admin-setup client — mirrors auth.ts's worker bridge. The web app
// reaches the worker's /admin/sso/setup/* endpoints over loopback; the worker
// drives the Scalekit workspace MCP tools (portal link + connections).

export type SsoSetupStatus =
  | "not_started"
  | "awaiting_portal"
  | "polling"
  | "connected"
  | "failed";

export interface SsoSetupConnection {
  status: string;
  provider: string | null;
  enabled: boolean;
}

export interface SsoSetupStatusResult {
  status: SsoSetupStatus;
  portalLink: string | null;
  portalLinkExpiresAt: string | null;
  provider: string | null;
  connection: SsoSetupConnection | null;
  lastError: string | null;
  setupCompletedAt: string | null;
  environmentId: string | null;
  organizationId: string | null;
  environmentTier: string | null;
  signInConfigured: boolean;
}

export interface SsoSetupCheckResult {
  status: SsoSetupStatus;
  connection: SsoSetupConnection | null;
  lastError: string | null;
  setupCompletedAt: string | null;
}

function workerAdminBase(): string {
  const raw = process.env.WORKER_ADMIN_URL ?? "http://127.0.0.1:4100";
  return raw.replace(/\/+$/, "");
}

async function readError(res: Response, label: string): Promise<Error> {
  const text = await res.text().catch(() => "");
  let detail = text;
  try {
    const parsed = JSON.parse(text) as { error?: string };
    if (parsed.error) detail = parsed.error;
  } catch {
    // non-JSON body — use raw text.
  }
  return new Error(`${label} failed (${res.status}): ${detail || res.statusText}`);
}

async function postJson(
  path: string,
  body: Record<string, unknown>,
  label: string,
): Promise<unknown> {
  const res = await fetch(`${workerAdminBase()}${path}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
    signal: AbortSignal.timeout(20_000),
  });
  if (!res.ok) throw await readError(res, label);
  return res.json();
}

export async function getSsoSetupStatus(
  orgId: string,
): Promise<SsoSetupStatusResult> {
  const res = await fetch(
    `${workerAdminBase()}/admin/sso/setup/status?orgId=${encodeURIComponent(orgId)}`,
    { method: "GET", cache: "no-store", signal: AbortSignal.timeout(2000) },
  );
  if (!res.ok) throw await readError(res, "SSO setup status");
  return (await res.json()) as SsoSetupStatusResult;
}

export async function setSsoScalekitIds(
  orgId: string,
  input: { environmentId: string; organizationId: string; tier: string },
): Promise<void> {
  await postJson(
    "/admin/sso/setup/ids",
    { orgId, ...input },
    "SSO environment selection",
  );
}

export interface SsoEnvironmentRow {
  id: string;
  name: string | null;
  tier: string | null;
  domain: string | null;
}

export interface SsoOrganizationRow {
  id: string;
  name: string | null;
}

export async function listSsoEnvironments(
  orgId: string,
): Promise<SsoEnvironmentRow[]> {
  const res = await fetch(
    `${workerAdminBase()}/admin/sso/setup/environments?orgId=${encodeURIComponent(orgId)}`,
    { method: "GET", cache: "no-store", signal: AbortSignal.timeout(30_000) },
  );
  if (!res.ok) throw await readError(res, "SSO environment list");
  const body = (await res.json()) as { environments?: SsoEnvironmentRow[] };
  return body.environments ?? [];
}

export async function listSsoOrganizations(
  orgId: string,
  environmentId: string,
): Promise<SsoOrganizationRow[]> {
  const res = await fetch(
    `${workerAdminBase()}/admin/sso/setup/organizations?orgId=${encodeURIComponent(orgId)}&environmentId=${encodeURIComponent(environmentId)}`,
    { method: "GET", cache: "no-store", signal: AbortSignal.timeout(30_000) },
  );
  if (!res.ok) throw await readError(res, "SSO organization list");
  const body = (await res.json()) as { organizations?: SsoOrganizationRow[] };
  return body.organizations ?? [];
}

export async function getSsoEnvironmentCredentials(
  orgId: string,
  environmentId: string,
): Promise<{ environmentUrl: string; clientId: string }> {
  const res = await fetch(
    `${workerAdminBase()}/admin/sso/setup/credentials-info?orgId=${encodeURIComponent(orgId)}&environmentId=${encodeURIComponent(environmentId)}`,
    { method: "GET", cache: "no-store", signal: AbortSignal.timeout(30_000) },
  );
  if (!res.ok) throw await readError(res, "SSO credentials fetch");
  return (await res.json()) as { environmentUrl: string; clientId: string };
}

export async function setSsoSignInCredentials(
  orgId: string,
  input: { environmentUrl: string; clientId: string; clientSecret: string },
): Promise<void> {
  await postJson(
    "/admin/sso/setup/credentials",
    { orgId, ...input },
    "SSO sign-in credentials",
  );
}

export async function generateSsoPortalLink(
  orgId: string,
): Promise<{ portalLink: string; expiresAt: string | null }> {
  const result = await postJson(
    "/admin/sso/setup/generate-portal-link",
    { orgId },
    "SSO portal link generation",
  );
  return result as { portalLink: string; expiresAt: string | null };
}

export async function checkSsoConnection(
  orgId: string,
): Promise<SsoSetupCheckResult> {
  const result = await postJson(
    "/admin/sso/setup/check",
    { orgId },
    "SSO connection check",
  );
  return result as SsoSetupCheckResult;
}
