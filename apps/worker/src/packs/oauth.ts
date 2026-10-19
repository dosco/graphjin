import type { SolutionPackBundle } from "@neko/packs";
import { and, db, eq, pack_install, pool } from "@neko/db";
import { readSecretsStore, writeSecretsStore } from "@neko/secrets";
import {
  buildPackAuthorizationUrl,
  exchangePackOAuthCode,
  fetchPackOAuthAccount,
  refreshPackOAuthToken,
  type PackOAuthConnection,
} from "./oauth-client.js";
import { packSecretSection, secretEnvKey } from "./configuration.js";

function oauthStateKey(connectionKey: string, field: string): string {
  return `OAUTH_${secretEnvKey(connectionKey)}_${field}`;
}

export function packOAuthBinding(
  connection: PackOAuthConnection,
  secrets: Record<string, string>,
): Record<string, unknown> | null {
  const accountId = secrets[oauthStateKey(connection.key, "ACCOUNT_ID")];
  const accountLabel = secrets[oauthStateKey(connection.key, "ACCOUNT_LABEL")];
  if (!accountId || !accountLabel) return null;
  return {
    key: connection.key,
    providerLabel: connection.providerLabel,
    accountId,
    accountLabel,
    scopes: (secrets[oauthStateKey(connection.key, "SCOPES")] ?? "").split(" ").filter(Boolean),
  };
}

type PackOAuthServiceOptions = {
  loadBundle(packId: string, version?: string): Promise<SolutionPackBundle>;
  syncInstalledConnection(packId: string, connectionKey: string, enabled: boolean): Promise<void>;
};

export class PackOAuthService {
  constructor(
    private readonly orgId: string,
    private readonly options: PackOAuthServiceOptions,
  ) {}

  private async connection(packId: string, connectionKey: string): Promise<PackOAuthConnection> {
    const [installation] = await db().select({ version: pack_install.version, source: pack_install.source })
      .from(pack_install).where(and(
        eq(pack_install.org_id, this.orgId), eq(pack_install.pack_id, packId), eq(pack_install.status, "installed"),
      )).limit(1);
    const bundle = await this.options.loadBundle(
      packId,
      installation?.source === "uploaded" ? installation.version : undefined,
    );
    const connection = bundle.manifest.oauth.find((value) => value.key === connectionKey);
    if (!connection) throw new Error(`pack ${packId} does not declare OAuth connection ${connectionKey}`);
    return connection;
  }

  async status(packId: string, connectionKey: string): Promise<Record<string, unknown>> {
    const connection = await this.connection(packId, connectionKey);
    const secrets = (await readSecretsStore())[packSecretSection(packId)] ?? {};
    const binding = connection.scope === "user" ? null : packOAuthBinding(connection, secrets);
    return {
      scope: connection.scope,
      configured: Boolean(secrets[oauthStateKey(connection.key, "CLIENT_ID")] && secrets[secretEnvKey(connection.clientSecret)]),
      key: connection.key,
      providerLabel: connection.providerLabel,
      clientId: secrets[oauthStateKey(connection.key, "CLIENT_ID")] ?? null,
      scopes: connection.scopes,
      connected: Boolean(binding && secrets[secretEnvKey(connection.refreshToken)]),
      account: binding ? { id: binding.accountId, label: binding.accountLabel } : null,
      expiresAt: secrets[oauthStateKey(connection.key, "EXPIRES_AT")] ?? null,
    };
  }

  async configure(packId: string, connectionKey: string, input: Record<string, unknown>) {
    const connection = await this.connection(packId, connectionKey);
    if (connection.scope === "user" && !await this.isInstalled(packId)) throw new Error("Install this pack before configuring its OAuth client");
    const current = await readSecretsStore();
    const section = packSecretSection(packId);
    const saved = current[section] ?? {};
    const clientId = typeof input.clientId === "string" ? input.clientId.trim() : "";
    const clientSecret = (typeof input.clientSecret === "string" ? input.clientSecret.trim() : "") || saved[secretEnvKey(connection.clientSecret)];
    if (!clientId || !clientSecret) throw new Error("OAuth client ID and client secret are required");
    if (connection.scope === "user" && (clientId !== saved[oauthStateKey(connection.key, "CLIENT_ID")] || clientSecret !== saved[secretEnvKey(connection.clientSecret)])) {
      await pool().query("delete from pack_user_connection c using pack_install i where c.pack_install_id=i.id and c.org_id=$1 and i.org_id=$1 and i.pack_id=$2 and c.connection_key=$3", [this.orgId, packId, connectionKey]);
    }
    await writeSecretsStore({ ...current, [section]: { ...saved, [oauthStateKey(connection.key, "CLIENT_ID")]: clientId, [secretEnvKey(connection.clientSecret)]: clientSecret } });
    return this.status(packId, connectionKey);
  }

  async begin(packId: string, connectionKey: string, input: Record<string, unknown>): Promise<{ authorizationUrl: string }> {
    const connection = await this.connection(packId, connectionKey);
    if (connection.scope === "user") throw new Error("Connect your personal account on Integrations");
    const current = await readSecretsStore();
    const section = packSecretSection(packId);
    const saved = current[section] ?? {};
    const clientId = (typeof input.clientId === "string" ? input.clientId.trim() : "") || saved[oauthStateKey(connection.key, "CLIENT_ID")] || "";
    const clientSecret = (typeof input.clientSecret === "string" ? input.clientSecret.trim() : "") || saved[secretEnvKey(connection.clientSecret)] || "";
    const redirectUri = typeof input.redirectUri === "string" ? input.redirectUri : "";
    const state = typeof input.state === "string" ? input.state : "";
    const codeChallenge = typeof input.codeChallenge === "string" ? input.codeChallenge : "";
    if (!clientId || !clientSecret || !redirectUri || !state || !codeChallenge) {
      throw new Error("OAuth client ID, client secret, redirect URI, state and PKCE challenge are required");
    }
    const callback = new URL(redirectUri);
    if (callback.protocol !== "https:" && !(callback.protocol === "http:" && ["localhost", "127.0.0.1", "::1"].includes(callback.hostname))) {
      throw new Error("OAuth callback must use HTTPS except on localhost");
    }
    await writeSecretsStore({
      ...current,
      [section]: {
        ...(current[section] ?? {}),
        [oauthStateKey(connection.key, "CLIENT_ID")]: clientId,
        [secretEnvKey(connection.clientSecret)]: clientSecret,
      },
    });
    return { authorizationUrl: buildPackAuthorizationUrl({ connection, clientId, redirectUri, state, codeChallenge }) };
  }

  async complete(packId: string, connectionKey: string, input: Record<string, unknown>): Promise<Record<string, unknown>> {
    const connection = await this.connection(packId, connectionKey);
    if (connection.scope === "user") throw new Error("Connect your personal account on Integrations");
    const code = typeof input.code === "string" ? input.code : "";
    const codeVerifier = typeof input.codeVerifier === "string" ? input.codeVerifier : "";
    const redirectUri = typeof input.redirectUri === "string" ? input.redirectUri : "";
    if (!code || !codeVerifier || !redirectUri) throw new Error("OAuth code, verifier and redirect URI are required");
    const current = await readSecretsStore();
    const section = packSecretSection(packId);
    const secrets = current[section] ?? {};
    const clientId = secrets[oauthStateKey(connection.key, "CLIENT_ID")] ?? "";
    const clientSecret = secrets[secretEnvKey(connection.clientSecret)] ?? "";
    if (!clientId || !clientSecret) throw new Error("OAuth client configuration is missing; start the connection again");
    const tokens = await exchangePackOAuthCode({ connection, clientId, clientSecret, code, codeVerifier, redirectUri });
    if (!tokens.refreshToken) throw new Error("OAuth provider did not return a refresh token; grant offline access and consent again");
    const missingScopes = connection.scopes.filter((scope) => !tokens.scopes.includes(scope));
    if (missingScopes.length > 0) {
      throw new Error(`OAuth consent did not grant required scope(s): ${missingScopes.join(", ")}`);
    }
    const account = await fetchPackOAuthAccount({ connection, accessToken: tokens.accessToken });
    const installed = await this.isInstalled(packId);
    const nextPackSecrets = {
      ...secrets,
      [secretEnvKey(connection.accessToken)]: tokens.accessToken,
      [secretEnvKey(connection.refreshToken)]: tokens.refreshToken,
      [oauthStateKey(connection.key, "EXPIRES_AT")]: tokens.expiresAt,
      [oauthStateKey(connection.key, "SCOPES")]: tokens.scopes.join(" "),
      [oauthStateKey(connection.key, "ACCOUNT_ID")]: account.id,
      [oauthStateKey(connection.key, "ACCOUNT_LABEL")]: account.label,
      ...(installed ? { [oauthStateKey(connection.key, "SYNC_PENDING")]: "true" } : {}),
    };
    await writeSecretsStore({ ...current, [section]: nextPackSecrets });
    let finalPackSecrets = nextPackSecrets;
    if (installed) {
      await this.options.syncInstalledConnection(packId, connection.key, true);
      const latest = await readSecretsStore();
      finalPackSecrets = { ...(latest[section] ?? {}) };
      delete finalPackSecrets[oauthStateKey(connection.key, "SYNC_PENDING")];
      await writeSecretsStore({ ...latest, [section]: finalPackSecrets });
    }
    await this.bindInstallation(packId, connection, finalPackSecrets);
    return this.status(packId, connectionKey);
  }

  async disconnect(packId: string, connectionKey: string): Promise<boolean> {
    const connection = await this.connection(packId, connectionKey);
    if (connection.scope === "user") throw new Error("Connect your personal account on Integrations");
    const current = await readSecretsStore();
    const section = packSecretSection(packId);
    const secrets = current[section];
    if (!secrets) return false;
    const next = { ...secrets };
    const keys = [
      secretEnvKey(connection.accessToken), secretEnvKey(connection.refreshToken),
      oauthStateKey(connection.key, "EXPIRES_AT"), oauthStateKey(connection.key, "SCOPES"),
      oauthStateKey(connection.key, "ACCOUNT_ID"), oauthStateKey(connection.key, "ACCOUNT_LABEL"),
    ];
    const removed = keys.some((key) => Object.hasOwn(next, key));
    if (removed && await this.isInstalled(packId)) {
      await this.options.syncInstalledConnection(packId, connection.key, false);
    }
    for (const key of keys) delete next[key];
    delete next[oauthStateKey(connection.key, "SYNC_PENDING")];
    await writeSecretsStore({ ...current, [section]: next });
    await this.bindInstallation(packId, connection, next);
    return removed;
  }

  private async isInstalled(packId: string): Promise<boolean> {
    const [installation] = await db().select({ id: pack_install.id }).from(pack_install).where(and(
      eq(pack_install.org_id, this.orgId), eq(pack_install.pack_id, packId), eq(pack_install.status, "installed"),
    )).limit(1);
    return Boolean(installation);
  }

  private async bindInstallation(packId: string, connection: PackOAuthConnection, secrets: Record<string, string>): Promise<boolean> {
    const [installation] = await db().select().from(pack_install).where(and(
      eq(pack_install.org_id, this.orgId), eq(pack_install.pack_id, packId), eq(pack_install.status, "installed"),
    )).limit(1);
    if (!installation) return false;
    const current = installation.config._oauth && typeof installation.config._oauth === "object"
      ? installation.config._oauth as Record<string, unknown> : {};
    const binding = packOAuthBinding(connection, secrets);
    const next = { ...current };
    if (binding) next[connection.key] = binding;
    else delete next[connection.key];
    await db().update(pack_install).set({ config: { ...installation.config, _oauth: next }, updated_at: new Date() })
      .where(eq(pack_install.id, installation.id));
    return true;
  }

  async refreshDue(): Promise<number> {
    const installations = await db().select().from(pack_install).where(and(
      eq(pack_install.org_id, this.orgId), eq(pack_install.status, "installed"),
    ));
    let refreshed = 0;
    const failures: Error[] = [];
    for (const installation of installations) {
      try {
        const bundle = await this.options.loadBundle(installation.pack_id, installation.source === "uploaded" ? installation.version : undefined);
        for (const connection of bundle.manifest.oauth) {
          if (connection.scope === "user") continue;
          const store = await readSecretsStore();
          const section = packSecretSection(installation.pack_id);
          const secrets = store[section] ?? {};
          const expiresAt = Date.parse(secrets[oauthStateKey(connection.key, "EXPIRES_AT")] ?? "");
          const pending = secrets[oauthStateKey(connection.key, "SYNC_PENDING")] === "true";
          if (!pending && Number.isFinite(expiresAt) && expiresAt > Date.now() + 5 * 60_000) continue;
          const clientId = String(installation.config[connection.clientIdInput] ?? secrets[oauthStateKey(connection.key, "CLIENT_ID")] ?? "");
          const clientSecret = secrets[secretEnvKey(connection.clientSecret)] ?? "";
          const refreshToken = secrets[secretEnvKey(connection.refreshToken)] ?? "";
          if (!clientId || !clientSecret || !refreshToken) continue;
          if (pending && Number.isFinite(expiresAt) && expiresAt > Date.now() + 5 * 60_000) {
            await this.options.syncInstalledConnection(installation.pack_id, connection.key, true);
            const latest = await readSecretsStore();
            const latestPack = { ...(latest[section] ?? {}) };
            delete latestPack[oauthStateKey(connection.key, "SYNC_PENDING")];
            await writeSecretsStore({ ...latest, [section]: latestPack });
            continue;
          }
          const tokens = await refreshPackOAuthToken({
            connection,
            clientId,
            clientSecret,
            refreshToken,
            scopes: (secrets[oauthStateKey(connection.key, "SCOPES")] ?? connection.scopes.join(" ")).split(" ").filter(Boolean),
          });
          await writeSecretsStore({
            ...store,
            [section]: {
              ...secrets,
              [secretEnvKey(connection.accessToken)]: tokens.accessToken,
              [secretEnvKey(connection.refreshToken)]: tokens.refreshToken ?? refreshToken,
              [oauthStateKey(connection.key, "EXPIRES_AT")]: tokens.expiresAt,
              [oauthStateKey(connection.key, "SCOPES")]: tokens.scopes.join(" "),
              [oauthStateKey(connection.key, "SYNC_PENDING")]: "true",
            },
          });
          await this.options.syncInstalledConnection(installation.pack_id, connection.key, true);
          const latest = await readSecretsStore();
          const latestPack = { ...(latest[section] ?? {}) };
          delete latestPack[oauthStateKey(connection.key, "SYNC_PENDING")];
          await writeSecretsStore({ ...latest, [section]: latestPack });
          refreshed++;
        }
      } catch (error) {
        failures.push(error instanceof Error ? error : new Error(String(error)));
      }
    }
    if (failures.length > 0) throw new AggregateError(failures, `${failures.length} pack OAuth refresh operation(s) failed`);
    return refreshed;
  }
}
