import { createHash, randomUUID } from "node:crypto";
import { pool } from "@neko/db";
import { maybeDecryptSecret, maybeEncryptSecret } from "@neko/secret-crypt";
import { readSecretsStore } from "@neko/secrets";
import { buildPackAuthorizationUrl, exchangePackOAuthCode, fetchPackOAuthAccount, refreshPackOAuthToken, packConnectionHeader, type PackOAuthConnection, type PackOAuthTokens } from "@neko/packs/oauth-client";
import { verifyGraphjinToken } from "./token";

export type ConnectionActor = { orgId: string; userId: string };
export type ConnectionBinding = { installId: string; connectionKey: string; revision: string };
type Installation = { id: string; pack_id: string; config: Record<string, unknown> };
type ConnectionRow = { revision: string; credentials: string | null; account_id: string | null; account_label: string | null; pending_state_hash: string | null; pending_expires_at: Date | null };
const envKey = (key: string) => key.replace(/[^A-Za-z0-9]+/g, "_").toUpperCase();
const hash = (state: string) => createHash("sha256").update(state).digest("hex");

export function personalConnections(installation: Installation): PackOAuthConnection[] {
  return Array.isArray(installation.config._userOAuth) ? installation.config._userOAuth as PackOAuthConnection[] : [];
}

async function activeActor(actor: ConnectionActor): Promise<void> {
  const result = await pool().query("select 1 from app_user where id=$1 and org_id=$2 and disabled_at is null", [actor.userId, actor.orgId]);
  if (!result.rowCount) throw new Error("Sign in with an active account to manage personal connections");
}

async function installed(actor: ConnectionActor, packId: string) {
  await activeActor(actor);
  const result = await pool().query<Installation>("select id,pack_id,config from pack_install where org_id=$1 and pack_id=$2 and status='installed'", [actor.orgId, packId]);
  if (!result.rows[0]) throw new Error("This pack is not installed");
  return result.rows[0];
}

async function clientConfig(packId: string, connection: PackOAuthConnection) {
  const secrets = (await readSecretsStore())[`pack.${packId}`] ?? {};
  const clientId = secrets[`OAUTH_${envKey(connection.key)}_CLIENT_ID`] ?? "";
  const clientSecret = secrets[envKey(connection.clientSecret)] ?? "";
  return { clientId, clientSecret };
}

function declaration(installation: Installation, key: string) {
  const connection = personalConnections(installation).find(c => c.key === key);
  if (!connection) throw new Error("This personal connection is unavailable; ask an admin to upgrade the pack");
  return connection;
}

// The row remains as a tombstone on disconnect, so old approvals/callbacks never become valid again.
async function locked<T>(actor: ConnectionActor, installId: string, key: string, work: (row: ConnectionRow, save: (fields: Partial<ConnectionRow>) => Promise<void>) => Promise<T>): Promise<T> {
  const client = await pool().connect();
  try {
    await client.query("begin");
    const active = await client.query("select 1 from app_user u join pack_install i on i.org_id=u.org_id where u.id=$1 and u.org_id=$2 and u.disabled_at is null and i.id=$3 and i.status='installed' for share of u,i", [actor.userId, actor.orgId, installId]);
    if (!active.rowCount) throw new Error("The user or pack is no longer active");
    const ids = [actor.orgId, actor.userId, installId, key];
    await client.query("insert into pack_user_connection(org_id,user_id,pack_install_id,connection_key) values($1,$2,$3,$4) on conflict do nothing", ids);
    const result = await client.query<ConnectionRow>("select * from pack_user_connection where org_id=$1 and user_id=$2 and pack_install_id=$3 and connection_key=$4 for update", ids);
    const value = await work(result.rows[0]!, async fields => {
      const allowed = new Set(["revision", "credentials", "account_id", "account_label", "pending_state_hash", "pending_expires_at"]);
      const entries = Object.entries(fields);
      if (entries.some(([key]) => !allowed.has(key))) throw new Error("Invalid connection update");
      if (!entries.length) return;
      await client.query(`update pack_user_connection set ${entries.map(([key], n) => `${key}=$${n + 5}`).join(",")},updated_at=now() where org_id=$1 and user_id=$2 and pack_install_id=$3 and connection_key=$4`, [...ids, ...entries.map(([, value]) => value)]);
    });
    await client.query("commit");
    return value;
  } catch (error) {
    await client.query("rollback");
    throw error;
  } finally { client.release(); }
}

export async function listPackUserConnections(actor: ConnectionActor) {
  await activeActor(actor);
  const installs = await pool().query<Installation>("select id,pack_id,config from pack_install where org_id=$1 and status='installed' order by pack_id", [actor.orgId]);
  const rows = await pool().query<ConnectionRow & { pack_install_id: string; connection_key: string }>("select pack_install_id,connection_key,account_label,credentials is not null as connected from pack_user_connection where org_id=$1 and user_id=$2", [actor.orgId, actor.userId]);
  return Promise.all(installs.rows.flatMap(installation => personalConnections(installation).map(async connection => {
    const row = rows.rows.find(r => r.pack_install_id === installation.id && r.connection_key === connection.key) as (ConnectionRow & { connected: boolean }) | undefined;
    const config = await clientConfig(installation.pack_id, connection);
    return { packId: installation.pack_id, key: connection.key, providerLabel: connection.providerLabel, experience: connection.experience, scopes: connection.scopes, configured: Boolean(config.clientId && config.clientSecret && !installation.config._definitionOnly), connected: row?.connected ?? false, accountLabel: row?.account_label ?? null };
  })));
}
export type PackUserConnectionStatus = Awaited<ReturnType<typeof listPackUserConnections>>[number];

export async function beginPackUserConnection(actor: ConnectionActor, packId: string, key: string, input: { state: string; codeChallenge: string; redirectUri: string }) {
  const installation = await installed(actor, packId);
  const connection = declaration(installation, key);
  if (installation.config._definitionOnly) throw new Error("Ask an admin to finish configuring this pack first");
  const { clientId, clientSecret } = await clientConfig(packId, connection);
  if (!clientId || !clientSecret) throw new Error("Ask an admin to configure this connection first");
  const uri = new URL(input.redirectUri);
  if (uri.protocol !== "https:" && !(uri.protocol === "http:" && ["localhost", "127.0.0.1", "[::1]"].includes(uri.hostname))) throw new Error("OAuth callback must use HTTPS except on localhost");
  if (!input.state || !input.codeChallenge) throw new Error("OAuth state and PKCE challenge are required");
  await locked(actor, installation.id, key, async (_row, save) => {
    await save({ pending_state_hash: hash(input.state), pending_expires_at: new Date(Date.now() + 600_000), revision: randomUUID() });
  });
  return { authorizationUrl: buildPackAuthorizationUrl({ connection, clientId, ...input }) };
}

export async function completePackUserConnection(actor: ConnectionActor, packId: string, key: string, input: { state: string; code: string; codeVerifier: string; redirectUri: string }) {
  const installation = await installed(actor, packId);
  const connection = declaration(installation, key);
  // Consume before contacting the provider. Failure cannot make a callback replayable.
  const revision = await locked(actor, installation.id, key, async (row, save) => {
    if (!row.pending_state_hash || row.pending_state_hash !== hash(input.state) || !row.pending_expires_at || row.pending_expires_at.getTime() <= Date.now()) throw new Error("OAuth connection expired; connect again");
    await save({ pending_state_hash: null, pending_expires_at: null });
    return row.revision;
  });
  const config = await clientConfig(packId, connection);
  const tokens = await exchangePackOAuthCode({ connection, ...config, ...input });
  if (!tokens.refreshToken || connection.scopes.some(scope => !tokens.scopes.includes(scope))) throw new Error("Grant all requested permissions and offline access, then connect again");
  const account = await fetchPackOAuthAccount({ connection, accessToken: tokens.accessToken });
  await locked(actor, installation.id, key, async (row, save) => {
    if (row.revision !== revision) throw new Error("The connection changed; connect again");
    await save({ credentials: maybeEncryptSecret(JSON.stringify(tokens)), account_id: account.id, account_label: account.label });
  });
}

export async function disconnectPackUserConnection(actor: ConnectionActor, packId: string, key: string) {
  const installation = await installed(actor, packId);
  declaration(installation, key);
  await locked(actor, installation.id, key, async (_row, save) => {
    await save({ revision: randomUUID(), credentials: null, account_id: null, account_label: null, pending_state_hash: null, pending_expires_at: null });
  });
}

export async function packConnectionBindings(actor: ConnectionActor, installId: string): Promise<ConnectionBinding[]> {
  await activeActor(actor);
  const result = await pool().query<Installation>("select id,pack_id,config from pack_install where id=$1 and org_id=$2 and status='installed'", [installId, actor.orgId]);
  const installation = result.rows[0];
  if (!installation) throw new Error("The pack is no longer installed");
  return Promise.all(personalConnections(installation).map(connection => locked(actor, installId, connection.key, async row => {
    if (!row.credentials) throw new Error(`Connect ${connection.providerLabel} on Integrations before requesting this action`);
    return { installId, connectionKey: connection.key, revision: row.revision };
  })));
}

/** The only credential bridge: verified internal actor, exact registered endpoint, no anonymous/admin fallback. */
export async function packUserConnectionHeaders(baseUrl: string, headers: Record<string, string>, expected?: ConnectionBinding[]): Promise<Record<string, string>> {
  const safe = Object.fromEntries(Object.entries(headers).filter(([key]) => !key.toLowerCase().startsWith("x-openneko-pack-")));
  const unavailable = () => { if (expected?.length) throw new Error("The approved pack connection is no longer available"); return safe; };
  const authorization = new Headers(safe).get("authorization");
  if (!authorization?.startsWith("Bearer ")) return unavailable();
  const token = authorization.slice(7);
  let orgId: unknown;
  try { orgId = JSON.parse(Buffer.from(token.split(".")[1] ?? "", "base64url").toString()).org_id; } catch { return unavailable(); }
  if (typeof orgId !== "string") return unavailable();
  const claims = verifyGraphjinToken(token, orgId);
  if (!claims) return unavailable();
  const installs = await pool().query<Installation>(`select i.id,i.pack_id,i.config from pack_install i join data_source d on d.org_id=i.org_id and d.id::text=i.config->'_runtime'->'source'->>'id' where i.org_id=$1 and i.status<>'removed' and d.enabled and d.auth_mode='jwt' and d.graphql_url=i.config->'_runtime'->'source'->>'graphqlUrl' and d.auth_mode=i.config->'_runtime'->'source'->>'authMode' and (d.graphql_url=$2 or d.mcp_url=$2)`, [orgId, baseUrl]);
  if (!installs.rows.length) return unavailable();
  // Legacy OAuth installs have no explicit account-scope snapshot. Reinstalling
  // records the manifest's user/deployment choice before credentials can be used.
  if (installs.rows.some(i => !Array.isArray(i.config._userOAuth) && i.config._oauth && Object.keys(i.config._oauth as object).length)) throw new Error("Ask an admin to upgrade the OAuth pack to confirm its account scope");
  const personal = installs.rows.flatMap(i => personalConnections(i).map(c => ({ installation: i, connection: c })));
  if (!personal.length) return unavailable();
  if (typeof claims.sub !== "string" || claims.sub === "service") return unavailable();
  const actor = { orgId, userId: claims.sub };
  await activeActor(actor);
  const matched = new Set<string>();
  for (const { installation, connection } of personal) {
    const binding = expected?.find(b => b.installId === installation.id && b.connectionKey === connection.key);
    if (binding) matched.add(`${binding.installId}:${binding.connectionKey}`);
    let accessToken: string | null;
    try { accessToken = await locked(actor, installation.id, connection.key, async (row, save) => {
      if (binding && (binding.revision !== row.revision || !row.credentials)) throw new Error("The approved account connection changed; request approval again");
      if (!row.credentials) return null;
      let tokens = JSON.parse(maybeDecryptSecret(row.credentials)) as PackOAuthTokens;
      if (!Number.isFinite(Date.parse(tokens.expiresAt)) || Date.parse(tokens.expiresAt) <= Date.now() + 60_000) {
        if (!tokens.refreshToken) throw new Error("Reconnect your account on Integrations");
        const refreshed = await refreshPackOAuthToken({ connection, ...await clientConfig(installation.pack_id, connection), refreshToken: tokens.refreshToken, scopes: tokens.scopes });
        if (connection.scopes.some(scope => !refreshed.scopes.includes(scope))) throw new Error("Reconnect your account to grant the required permissions");
        tokens = { ...refreshed, refreshToken: refreshed.refreshToken ?? tokens.refreshToken };
        await save({ credentials: maybeEncryptSecret(JSON.stringify(tokens)) });
      }
      return tokens.accessToken;
    });
    } catch (error) {
      if (binding) throw error;
      // An unavailable connection must not break queries against other sources.
      continue;
    }
    if (accessToken) safe[packConnectionHeader(installation.pack_id, connection.key)] = accessToken;
  }
  if (expected?.some(b => !matched.has(`${b.installId}:${b.connectionKey}`))) throw new Error("The approved pack connection is no longer available");
  return safe;
}
