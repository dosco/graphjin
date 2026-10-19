// Sign-in, directory sync and the Scalekit workspace connection, in core.
// Two providers ship: Scalekit (IdP-attested SSO + SCIM directory) and
// magic-link (self-asserted email sign-in for pre-provisioned users). The
// admin picks one; its values live in the `signin.<id>` section of the
// secrets store and the choice in `signin` → PROVIDER. Until the chosen
// provider's required values are set the deployment stays in
// single-operator mode, so the admin is never locked out of setup.
import { randomBytes } from "node:crypto";
import type { ConnectorCredential } from "@neko/secrets";
import type {
  ApplyDirectoryChangeResult,
  DirectoryCapabilities,
  DirectoryChange,
  ListDirectoryResult,
} from "../directory/types.js";
import { directoryChangeAllowed } from "../directory/types.js";
import type { CoreSecrets } from "../secrets/core-secrets.js";
import {
  beginMagicLinkAuth,
  completeMagicLinkAuth,
  MAGIC_LINK_SETTINGS,
  type MagicLinkOptions,
} from "./magic-link/provider.js";
import {
  applyScalekitDirectoryChange,
  beginScalekitAuth,
  beginScalekitConnect,
  callScalekitTool,
  completeScalekitAuth,
  completeScalekitConnect,
  listScalekitDirectory,
  refreshScalekitConnect,
  scalekitCredentialNearExpiry,
  SCALEKIT_DIRECTORY,
  SCALEKIT_SETTINGS,
  SCALEKIT_WORKSPACE_LABEL,
  SCALEKIT_WORKSPACE_SCOPES,
  type ScalekitOptions,
} from "./scalekit/provider.js";
import type {
  AuthIdentity,
  BeginAuthParams,
  BeginConnectParams,
  CompleteAuthParams,
  CompleteConnectParams,
  SignInSetting,
} from "./types.js";

export type SignInProviderId = "scalekit" | "magic-link";

interface ProviderSpec {
  label: string;
  /** "manual": self-asserted identity — the web never auto-creates users. */
  provisioning: "automatic" | "manual";
  /** Sign-in cannot start without an email. */
  loginHintRequired: boolean;
  settings: SignInSetting[];
}

export const SIGNIN_PROVIDERS: Record<SignInProviderId, ProviderSpec> = {
  scalekit: { label: "Scalekit", provisioning: "automatic", loginHintRequired: false, settings: SCALEKIT_SETTINGS },
  "magic-link": { label: "Email link", provisioning: "manual", loginHintRequired: true, settings: MAGIC_LINK_SETTINGS },
};

/** Secrets-store section that records which provider is in use. */
export const SIGNIN_SECTION = "signin";

/** Reserved operator slot for the deployment-scoped workspace credential. */
export const DEPLOYMENT_CONNECT_SLOT = "deployment";

/** The package names these providers shipped under as plugins. */
export const LEGACY_SIGNIN_NAMES: Readonly<Record<string, SignInProviderId>> = {
  "@open-neko/plugin-scalekit": "scalekit",
  "@open-neko/plugin-magic-link": "magic-link",
};

export const signInSection = (id: SignInProviderId): string => `signin.${id}`;

export const isSignInProviderId = (value: string): value is SignInProviderId =>
  Object.prototype.hasOwnProperty.call(SIGNIN_PROVIDERS, value);

/** Snapshot of the chosen provider, shaped as the web's sign-in gate reads it. */
export interface AuthProviderInfo {
  pluginId: SignInProviderId;
  pluginName: SignInProviderId;
  providerLabel: string;
  provisioning: "automatic" | "manual";
  loginHintRequired: boolean;
}

export interface DirectoryProviderInfo {
  id: SignInProviderId;
  capabilities: DirectoryCapabilities;
}

export class SignInSettingsError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "SignInSettingsError";
  }
}

export interface SignInServiceOptions {
  scalekit?: ScalekitOptions;
  magicLink?: MagicLinkOptions;
}

export class SignInService {
  constructor(
    private readonly secrets: CoreSecrets,
    private readonly options: SignInServiceOptions = {},
  ) {}

  // ─── provider choice and values ─────────────────────────────────────

  selectedProvider(): SignInProviderId | null {
    const id = this.secrets.section(SIGNIN_SECTION).PROVIDER ?? "";
    return isSignInProviderId(id) ? id : null;
  }

  /** Chooses the sign-in provider (null turns sign-in off) and mints its generated values. */
  async selectProvider(id: SignInProviderId | null): Promise<void> {
    if (id !== null && !isSignInProviderId(id)) throw new SignInSettingsError(`unknown sign-in provider ${id}`);
    if (id) await this.backfillGenerated(id);
    if (this.selectedProvider() !== id) await this.secrets.setValues(SIGNIN_SECTION, { PROVIDER: id });
  }

  providerValues(id: SignInProviderId): Record<string, string> {
    return this.secrets.section(signInSection(id));
  }

  /** Writes (string) or deletes (null) declared values of one provider. */
  async setProviderValues(id: SignInProviderId, values: Record<string, string | null>): Promise<void> {
    const declared = new Set(SIGNIN_PROVIDERS[id].settings.map((s) => s.key));
    for (const key of Object.keys(values)) {
      if (!declared.has(key)) throw new SignInSettingsError(`"${key}" is not a ${SIGNIN_PROVIDERS[id].label} setting`);
    }
    await this.secrets.setValues(signInSection(id), values);
  }

  /** Mints any host-generated value (the magic-link HMAC key) the provider still lacks. */
  private async backfillGenerated(id: SignInProviderId): Promise<void> {
    const current = this.providerValues(id);
    const minted: Record<string, string> = {};
    for (const setting of SIGNIN_PROVIDERS[id].settings) {
      if (setting.autogenerate && !current[setting.key]) minted[setting.key] = randomBytes(48).toString("base64url");
    }
    if (Object.keys(minted).length > 0) await this.secrets.setValues(signInSection(id), minted);
  }

  // ─── sign-in gate (AuthHandlerSurface) ──────────────────────────────

  getAuthProvider(): AuthProviderInfo | null {
    const id = this.selectedProvider();
    if (!id) return null;
    const spec = SIGNIN_PROVIDERS[id];
    return {
      pluginId: id,
      pluginName: id,
      providerLabel: spec.label,
      provisioning: spec.provisioning,
      loginHintRequired: spec.loginHintRequired,
    };
  }

  /** Required values the chosen provider still lacks; null when none is chosen. */
  getAuthEnvGaps(): string[] | null {
    const id = this.selectedProvider();
    if (!id) return null;
    const values = this.providerValues(id);
    return SIGNIN_PROVIDERS[id].settings
      .filter((s) => s.required && !values[s.key])
      .map((s) => s.key);
  }

  authSignInReady(): boolean {
    const gaps = this.getAuthEnvGaps();
    return gaps !== null && gaps.length === 0;
  }

  /** Declared keys of the chosen provider that hold a value — names only. */
  getAuthConfiguredEnvKeys(): string[] {
    const id = this.selectedProvider();
    if (!id) return [];
    const values = this.providerValues(id);
    return SIGNIN_PROVIDERS[id].settings.map((s) => s.key).filter((key) => Boolean(values[key]));
  }

  getAuthDeclaredEnvKeys(): string[] {
    const id = this.selectedProvider();
    return id ? SIGNIN_PROVIDERS[id].settings.map((s) => s.key) : [];
  }

  async setAuthSecret(key: string, value: string | null): Promise<void> {
    const id = this.selectedProvider();
    if (!id) throw new SignInSettingsError("no sign-in provider is chosen");
    await this.setProviderValues(id, { [key]: value });
  }

  private requireProvider(): SignInProviderId {
    const id = this.selectedProvider();
    if (!id) throw new Error("no sign-in provider is chosen — set one up under Admin → Settings");
    return id;
  }

  async beginAuth(params: BeginAuthParams): Promise<{ authorizationUrl: string }> {
    const id = this.requireProvider();
    const values = this.providerValues(id);
    return id === "scalekit"
      ? beginScalekitAuth(params, values, this.options.scalekit)
      : beginMagicLinkAuth(params, values, this.options.magicLink);
  }

  async completeAuth(params: CompleteAuthParams): Promise<AuthIdentity> {
    const id = this.requireProvider();
    const values = this.providerValues(id);
    return id === "scalekit"
      ? completeScalekitAuth(params, values, this.options.scalekit)
      : completeMagicLinkAuth(params, values, this.options.magicLink);
  }

  // ─── directory sync (DirectorySource) ───────────────────────────────

  /** Scalekit supplies the directory while it is the sign-in provider. */
  getDirectoryProvider(): DirectoryProviderInfo | null {
    return this.selectedProvider() === "scalekit" ? { id: "scalekit", capabilities: SCALEKIT_DIRECTORY } : null;
  }

  listDirectory(cursor: string | null): Promise<ListDirectoryResult> {
    if (!this.getDirectoryProvider()) throw new Error("no directory provider is configured");
    return listScalekitDirectory({ cursor }, this.providerValues("scalekit"), this.options.scalekit);
  }

  async applyDirectoryChange(change: DirectoryChange): Promise<ApplyDirectoryChangeResult> {
    const provider = this.getDirectoryProvider();
    if (!provider) throw new Error("no directory provider is configured");
    if (!directoryChangeAllowed(provider.capabilities, change)) {
      throw new Error(`${provider.capabilities.providerLabel} does not accept "${change.op}"`);
    }
    return applyScalekitDirectoryChange({ change }, this.providerValues("scalekit"), this.options.scalekit);
  }

  // ─── Scalekit workspace connection (ConnectHandlerSurface) ──────────

  getConnectProviders(): Array<{
    pluginId: string;
    pluginName: string;
    providerLabel: string;
    scopes: string[];
    flow: string;
    credentialScope: string;
  }> {
    return [
      {
        pluginId: "scalekit",
        pluginName: "scalekit",
        providerLabel: SCALEKIT_WORKSPACE_LABEL,
        scopes: SCALEKIT_WORKSPACE_SCOPES,
        flow: "mcp-oauth",
        credentialScope: "deployment",
      },
    ];
  }

  /** No connector is per-operator; the workspace credential is deployment-wide. */
  getOperatorConnectStatus(_operatorId: string): Array<{ pluginName: string; connectedAt: string; scopes?: string[] }> {
    return [];
  }

  getDeploymentConnectStatus(): Array<{ pluginName: string; connectedAt: string; scopes?: string[] }> {
    const credential = this.workspaceCredential();
    return credential ? [{ pluginName: "scalekit", connectedAt: credential.connectedAt, scopes: credential.scopes }] : [];
  }

  private requireConnector(name: string): void {
    if (name !== "scalekit") throw new Error(`unknown connector ${name}`);
  }

  private workspaceCredential(): ConnectorCredential | null {
    return this.secrets.operatorCredential(DEPLOYMENT_CONNECT_SLOT, "scalekit");
  }

  async beginConnect(name: string, params: BeginConnectParams): Promise<{ authorizationUrl: string; oauthState?: string }> {
    this.requireConnector(name);
    return beginScalekitConnect(params, this.providerValues("scalekit"), this.options.scalekit);
  }

  async completeConnect(name: string, params: CompleteConnectParams): Promise<ConnectorCredential> {
    this.requireConnector(name);
    const credential = await completeScalekitConnect(params, this.options.scalekit);
    await this.secrets.setOperatorCredential(DEPLOYMENT_CONNECT_SLOT, "scalekit", credential);
    return credential;
  }

  async refreshConnect(name: string, _operatorId: string): Promise<ConnectorCredential> {
    this.requireConnector(name);
    const current = this.workspaceCredential();
    if (!current) throw new Error("the Scalekit workspace is not connected");
    const credential = await refreshScalekitConnect(current, this.options.scalekit);
    await this.secrets.setOperatorCredential(DEPLOYMENT_CONNECT_SLOT, "scalekit", credential);
    return credential;
  }

  async disconnect(name: string, _operatorId: string): Promise<boolean> {
    // Orphaned credentials of a removed connector stay cleanable.
    return this.secrets.setOperatorCredential(DEPLOYMENT_CONNECT_SLOT, name, null);
  }

  /**
   * Calls one Scalekit workspace tool for the SSO setup flow, refreshing
   * the deployment credential first when it is about to expire.
   */
  async callWorkspaceTool(tool: string, args: Record<string, unknown>): Promise<string> {
    let credential = this.workspaceCredential();
    if (credential && scalekitCredentialNearExpiry(credential)) {
      try {
        credential = await this.refreshConnect("scalekit", DEPLOYMENT_CONNECT_SLOT);
      } catch (err) {
        console.warn(`[signin] Scalekit workspace token refresh failed: ${err instanceof Error ? err.message : err}`);
      }
    }
    return callScalekitTool(tool, args, credential, this.providerValues("scalekit"), this.options.scalekit);
  }

  // ─── upgrade from the sign-in plugins ───────────────────────────────

  /**
   * One-time move of the values and workspace credential the sign-in plugins
   * stored under their package names, and of the provider choice they implied
   * (an installed auth plugin was the provider). Returns the legacy names moved.
   */
  async migrateLegacy(): Promise<string[]> {
    const moved: string[] = [];
    const found = new Map<SignInProviderId, boolean>();
    for (const [legacy, id] of Object.entries(LEGACY_SIGNIN_NAMES)) {
      const old = this.secrets.section(legacy);
      if (Object.keys(old).length === 0) continue;
      const declared = new Set(SIGNIN_PROVIDERS[id].settings.map((s) => s.key));
      const current = this.providerValues(id);
      const carried = Object.fromEntries(
        Object.entries(old).filter(([key]) => declared.has(key) && !current[key]),
      );
      const cleared = Object.fromEntries(Object.keys(old).map((key) => [key, null]));
      if (Object.keys(carried).length > 0) await this.secrets.setValues(signInSection(id), carried);
      await this.secrets.setValues(legacy, cleared);
      moved.push(legacy);
      found.set(id, SIGNIN_PROVIDERS[id].settings.every((s) => !s.required || s.autogenerate || Boolean(old[s.key])));
    }
    // Only one auth plugin could be installed; when both left values behind,
    // the one with complete values was the live provider.
    const implied: SignInProviderId | null =
      [...found].find(([, ready]) => ready)?.[0] ?? [...found.keys()][0] ?? null;
    const legacyCredential = this.secrets.operatorCredential(DEPLOYMENT_CONNECT_SLOT, "@open-neko/plugin-scalekit");
    if (legacyCredential) {
      if (!this.workspaceCredential()) {
        await this.secrets.setOperatorCredential(DEPLOYMENT_CONNECT_SLOT, "scalekit", legacyCredential);
      }
      await this.secrets.setOperatorCredential(DEPLOYMENT_CONNECT_SLOT, "@open-neko/plugin-scalekit", null);
      if (!moved.includes("@open-neko/plugin-scalekit")) moved.push("@open-neko/plugin-scalekit");
    }
    if (implied && !this.selectedProvider()) await this.selectProvider(implied);
    else if (this.selectedProvider()) await this.backfillGenerated(this.selectedProvider()!);
    return moved;
  }
}
