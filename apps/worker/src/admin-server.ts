/**
 * Admin HTTP handler for the worker process.
 *
 * Routes:
 *   GET  /health              → liveness + scheduler readiness
 *   GET  /health/scheduler    → scheduler progress diagnostics
 *   GET  /health/security     → process-local security subsystem health
 *   POST /admin/reconnect     → 202 + clean exit so the supervisor restarts
 *                               us with fresh DB credentials. Used by the
 *                               web app's /api/admin/change-password handler
 *                               after rotating the Postgres password — the
 *                               pg-boss singleton holds the old creds and
 *                               there's no clean way to re-register handlers
 *                               in-place against a fresh pool.
 *   GET  /admin/auth/status   → 200 + { provider: null | {pluginName, providerLabel}, pending }
 *                               Tells the web app which sign-in provider is
 *                               live so the sign-in page can render the
 *                               appropriate button.
 *   POST /admin/auth/provider → { provider: "scalekit" | "magic-link" | null }
 *                               chooses the sign-in provider.
 *   POST /admin/auth/begin    → 200 + { authorizationUrl }
 *                               Body: { redirectUri, state, loginHint? }
 *                               Starts sign-in with the chosen provider.
 *   POST /admin/auth/complete → 200 + { identity }
 *                               Body: { code, redirectUri, state }
 *                               Finishes sign-in with the chosen provider.
 *   GET  /admin/action-descriptors → built-in governed action kinds.
 *   GET  /admin/channels/settings → each built-in channel's declared settings
 *                               (secrets report only whether they are set).
 *   POST /admin/channels/settings → { channel, values: { KEY: string | null } }.
 *   GET  /admin/directory/status → directory provider and last sync state.
 *   POST /admin/directory/sync  → run a full directory sync now.
 *   POST /admin/directory/users → create a user in the identity provider ({ email, name? }).
 *   POST /admin/graphjin/group-grants → apply group grants now ({ immediate: true }) or in a batch.
 *   POST /admin/action-requests/create → persist + run worker-owned preflight
 *                               before returning an approval-card-safe id.
 *
 * Extracted from index.ts so the handler can be unit-tested without
 * booting pg-boss / the agent stack.
 */

import type { IncomingMessage, ServerResponse } from "node:http";
import type { ConnectorCredential } from "@neko/secrets";
import type {
  AuthIdentity,
  BeginConnectParams,
  CompleteConnectParams,
} from "./signin/types.js";
import { getAuditLoggingHealth } from "@neko/llm/workflows";
import { packFailurePayload, type PackErrorPhase } from "./packs/errors.js";

const PACK_FAILURE_NEXT_STEP: Record<PackErrorPhase, string> = {
  upload: "Correct the archive layout or manifest, then upload the pack again.",
  inspection: "Correct the reported manifest or artifact, then inspect the pack again.",
  review: "Correct the reported configuration or artifact, then review the pack again.",
  install: "Correct the reported configuration or artifact, then review and install the pack again.",
  configure: "Correct the reported configuration or artifact, then review and apply the change again.",
  upgrade: "Correct the reported configuration or artifact, then review and apply the upgrade again.",
  uninstall: "Check the reported pack state, then try removing the pack again.",
  oauth: "Check the OAuth client, redirect URI, and requested scopes, then connect the account again.",
  query_preflight: "Correct the reported query or source configuration, then review the pack again.",
};

function packFailure(error: unknown, phase: PackErrorPhase) {
  return packFailurePayload(error, phase, PACK_FAILURE_NEXT_STEP[phase]);
}

export interface AuthHandlerSurface {
  getAuthProvider(): {
    pluginName: string;
    providerLabel: string;
    provisioning?: "automatic" | "manual";
    loginHintRequired?: boolean;
  } | null;
  /**
   * Whether sign-in is actually usable (all required env vars set).
   * The web proxy gates on this, NOT on a provider merely being chosen — so the
   * admin isn't locked out of the setup UI before SSO is configured.
   */
  authSignInReady(): boolean;
  /**
   * Names of required env vars still unset (empty = env complete);
   * null/undefined when unavailable. Key names only, never values.
   */
  getAuthEnvGaps?(): string[] | null;
  /** Declared env keys currently holding a value — names only. */
  getAuthConfiguredEnvKeys?(): string[];
  /** All declared env keys — the allowlist for setAuthSecret. */
  getAuthDeclaredEnvKeys?(): string[];
  /**
   * ≥1 active admin app_user exists. Manual-provisioning providers
   * (magic link) must not go live before this — flipping the sign-in
   * gate with zero provisioned admins locks everyone out.
   */
  hasProvisionedAdmin?(): Promise<boolean>;
  /** A bootstrapped solo owner must supply a real email before sign-in activates. */
  soloAdminNeedsEmail?(): Promise<boolean>;
  /** Write (string) or delete (null) one value of the chosen provider. */
  setAuthSecret?(key: string, value: string | null): Promise<void>;
  /** Chooses the sign-in provider; null turns sign-in off. */
  selectProvider?(provider: string | null): Promise<void>;
  beginAuth(params: {
    redirectUri: string;
    state: string;
    loginHint?: string | null;
  }): Promise<{ authorizationUrl: string }>;
  completeAuth(params: {
    code: string;
    redirectUri: string;
    state: string;
  }): Promise<AuthIdentity>;
}

/**
 * Connector OAuth surface exposed to the web app (the Scalekit workspace
 * consent today). Credentials are persisted in the secrets file by the
 * worker, not by the web.
 */
export interface ConnectHandlerSurface {
  getConnectProviders(): Array<{
    pluginId: string;
    pluginName: string;
    providerLabel: string;
    scopes: string[];
    flow: string;
    credentialScope: string;
  }>;
  getOperatorConnectStatus(
    operatorId: string,
  ): Array<{ pluginName: string; connectedAt: string; scopes?: string[] }>;
  getDeploymentConnectStatus(): Array<{
    pluginName: string;
    connectedAt: string;
    scopes?: string[];
  }>;
  beginConnect(
    pluginName: string,
    params: BeginConnectParams,
  ): Promise<{ authorizationUrl: string; oauthState?: string }>;
  completeConnect(
    pluginName: string,
    params: CompleteConnectParams,
  ): Promise<ConnectorCredential>;
  refreshConnect(
    pluginName: string,
    operatorId: string,
  ): Promise<ConnectorCredential>;
  disconnect(pluginName: string, operatorId: string): Promise<boolean>;
}

/**
 * SSO admin-setup surface — the web app drives portal-link generation and
 * connection polling through this. Wired to the SsoSetupService in index.ts.
 */
export interface SsoSetupHandlerSurface {
  getStatus(orgId: string): Promise<{
    status: string;
    portalLink: string | null;
    portalLinkExpiresAt: string | null;
    provider: string | null;
    connection: { status: string; provider: string | null; enabled: boolean } | null;
    lastError: string | null;
    setupCompletedAt: string | null;
    environmentId: string | null;
    organizationId: string | null;
    environmentTier: string | null;
    signInConfigured: boolean;
  }>;
  setScalekitIds(
    orgId: string,
    input: { environmentId: string; organizationId: string; tier: string },
  ): Promise<void>;
  listEnvironments(orgId: string): Promise<Array<{
    id: string;
    name: string | null;
    tier: string | null;
    domain: string | null;
  }>>;
  listOrganizations(
    orgId: string,
    environmentId: string,
  ): Promise<Array<{ id: string; name: string | null }>>;
  getEnvironmentCredentials(
    orgId: string,
    environmentId: string,
  ): Promise<{ environmentUrl: string; clientId: string }>;
  setSignInCredentials(
    orgId: string,
    input: { environmentUrl: string; clientId: string; clientSecret: string },
  ): Promise<void>;
  generatePortalLink(
    orgId: string,
  ): Promise<{ portalLink: string; expiresAt: string | null }>;
  checkConnection(orgId: string): Promise<{
    status: string;
    connection: { status: string; provider: string | null; enabled: boolean } | null;
    lastError: string | null;
    setupCompletedAt: string | null;
  }>;
}

/**
 * Channel (frontend) surface. The built-in channels (Slack, Telegram, HUD)
 * run in-process; a pack turns each one on. `ingestInbound` hands webhooks
 * to the channel delivery module (verify → parse → dispatch to the existing
 * agent entry points).
 */
export interface ChannelHandlerSurface {
  getChannelProviders(): Promise<Array<{
    id: string;
    providerLabel: string;
    directions: string[];
    ingress: string;
  }>>;
  deliver(
    channelId: string,
    recipient: Record<string, unknown>,
    events: unknown[],
  ): Promise<{ delivered: boolean; ref?: string }>;
  ingestInbound(
    channelId: string,
    headers: Record<string, string>,
    body: string,
  ): Promise<{ ok: boolean; dispatched: number }>;
  /** Each channel's declared settings; secrets report only whether they are set. */
  settings?(): Promise<unknown[]>;
  setSettings?(channelId: string, values: Record<string, string | null>): Promise<void>;
}

/**
 * Install-policy surface exposed to the CLI (via the worker admin
 * port) for the install-time enforcement check. The CLI calls
 * `GET /admin/install-policy` before installing a community skill from
 * a git URL and refuses when the policy disallows it.
 */
export interface InstallPolicyHandlerSurface {
  getInstallPolicy(): Promise<{ allowGitUrlInstalls: boolean }>;
}

export interface PacksHandlerSurface {
  /** Skills and packs that changed in the org's context repository. */
  checkContextUpdates(): Promise<unknown>;
  upload(bytes: Buffer, input: { actorUserId?: string | null; signal?: AbortSignal }): Promise<unknown>;
  review(packId: string, input: Record<string, unknown>, operation: "install" | "configure" | "upgrade"): Promise<unknown>;
  list(): Promise<unknown>;
  inspect(packId: string, version?: string): Promise<unknown>;
  plan(packId: string, version?: string): Promise<unknown>;
  status(packId: string): Promise<unknown>;
  doctor(packId: string): Promise<unknown>;
  install(packId: string, input: Record<string, unknown>): Promise<unknown>;
  configure(packId: string, input: Record<string, unknown>): Promise<unknown>;
  upgrade(packId: string, input: Record<string, unknown>): Promise<unknown>;
  uninstall(packId: string, input: Record<string, unknown>): Promise<unknown>;
  configureOAuth(packId: string, connectionKey: string, input: Record<string, unknown>): Promise<Record<string, unknown>>;
  oauthStatus(packId: string, connectionKey: string): Promise<unknown>;
  beginOAuth(packId: string, connectionKey: string, input: Record<string, unknown>): Promise<unknown>;
  completeOAuth(packId: string, connectionKey: string, input: Record<string, unknown>): Promise<unknown>;
  disconnectOAuth(packId: string, connectionKey: string): Promise<unknown>;
  magentoStoreManagement(): Promise<unknown>;
  updateMagentoStoreManagement(input: Record<string, unknown>): Promise<unknown>;
}

export interface SchedulerHealthSurface {
  getHealth(): {
    status: "starting" | "ok" | "degraded";
    running: boolean;
    stale: boolean;
    startedAt: string;
    lastAttemptAt: string | null;
    lastSuccessAt: string | null;
    lastErrorAt: string | null;
    lastError: string | null;
    consecutiveFailures: number;
    materialized: number;
    dispatched: number;
    recovered: number;
  };
}

/**
 * Built-in governed action kinds with their seeded
 * approval mode. The web process's /work route and rules page read them
 * here; the adapters live in the worker.
 */
export interface ActionsHandlerSurface {
  getActionDescriptors(): Array<{
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
  }>;
}

export type AdminHandlerOptions = {
  /**
   * Called from POST /admin/reconnect after responding 202. Defaults to
   * `process.exit(0)`; tests pass a spy.
   */
  exit?: (code?: number) => void;
  /**
   * Delay (ms) between sending the 202 and calling exit. Default 100ms —
   * enough for the response to flush to the caller. Set to 0 in tests.
   */
  exitDelayMs?: number;
  /** The independent durable scheduler's process-local progress signal. */
  scheduler?: SchedulerHealthSurface | null;
  /**
   * Auth surface — wired to the SignInService. Absent in tests, in which
   * case /admin/auth/* routes report no provider or return 503.
   */
  auth?: AuthHandlerSurface | null;
  /**
   * Built-in action descriptors. Absent in tests, in which case
   * /admin/action-descriptors returns an empty array.
   */
  actions?: ActionsHandlerSurface | null;
  /**
   * Connector surface — wired to the SignInService (Scalekit workspace).
   * Absent in tests, in which case /admin/connect/* routes return 503.
   */
  connect?: ConnectHandlerSurface | null;
  /**
   * SSO admin-setup surface — wired to the SsoSetupService. Absent in
   * tests, in which case /admin/sso/setup/* routes return 503.
   */
  ssoSetup?: SsoSetupHandlerSurface | null;
  /**
   * Channel surface — wired to the built-in channel runtime and delivery
   * module. Absent in tests, in which case channel routes return 503 / empty.
   */
  channels?: ChannelHandlerSurface | null;
  /**
   * Install-policy reader. When absent, /admin/install-policy returns the
   * most-restrictive default so the CLI refuses git-URL skill installs.
   */
  installPolicy?: InstallPolicyHandlerSurface | null;
  /**
   * External-event ingress (OL3): watchers and integrations POST
   * /admin/events/external to fire external_event subscriptions.
   */
  events?: ExternalEventHandlerSurface | null;
  watcherEvents?: WatcherEventsHandlerSurface | null;
  /** Trusted web→worker action creation so worker-owned preflight hooks run. */
  actionRequests?: ActionRequestHandlerSurface | null;
  /** Shared first-party and uploaded solution-pack lifecycle. */
  packs?: PacksHandlerSurface | null;
  /** Directory provider status and on-demand sync. */
  directory?: DirectoryHandlerSurface | null;
  /** Writes group grants into the GraphJin config, now or batched. */
  groupGrants?: GroupGrantsHandlerSurface | null;
};

export interface GroupGrantsHandlerSurface {
  apply(): Promise<unknown>;
  schedule(): void;
  enable(actorUserId: string | null): Promise<unknown>;
  disable(actorUserId: string | null): Promise<unknown>;
  apiOperations(): Promise<string[]>;
}

export interface DirectoryHandlerSurface {
  status(): Promise<unknown>;
  sync(): Promise<unknown>;
  createUser(input: { email: string; name: string | null }): Promise<unknown>;
}

export interface ActionRequestHandlerSurface {
  create(input: Record<string, unknown>): Promise<{ id: string; status: string }>;
}

export interface ExternalEventHandlerSurface {
  dispatchExternal(input: {
    orgId: string;
    name: string;
    source: string | null;
    payload: Record<string, unknown>;
    dedupeKey?: string;
  }): Promise<{ matched: number; enqueued: number }>;
}

export interface WatcherEventsHandlerSurface {
  /** True when rawBody carries a valid signature for this GraphJin watch. */
  verify(rawBody: string, signature: string | undefined, watchId: string): boolean;
  receive(input: {
    watchId: string;
    eventId: string;
    event: Record<string, unknown>;
  }): Promise<{ accepted: boolean; fired?: boolean }>;
}

export function createAdminHandler(opts: AdminHandlerOptions = {}) {
  const exit = opts.exit ?? ((code = 0) => process.exit(code));
  const exitDelayMs = opts.exitDelayMs ?? 100;
  const scheduler = opts.scheduler ?? null;
  const auth = opts.auth ?? null;
  const actions = opts.actions ?? null;
  const connect = opts.connect ?? null;
  const ssoSetup = opts.ssoSetup ?? null;
  const channels = opts.channels ?? null;
  const installPolicy = opts.installPolicy ?? null;
  const events = opts.events ?? null;
  const watcherEvents = opts.watcherEvents ?? null;
  const actionRequests = opts.actionRequests ?? null;
  const packs = opts.packs ?? null;
  const directory = opts.directory ?? null;
  const groupGrants = opts.groupGrants ?? null;

  return function handle(req: IncomingMessage, res: ServerResponse) {
    if (req.method === "GET" && req.url === "/health") {
      const health = scheduler?.getHealth();
      const ready = health === undefined || health.status === "ok";
      res.writeHead(ready ? 200 : 503).end(ready ? "ok" : health.status);
      return;
    }
    if (req.method === "GET" && req.url === "/health/scheduler") {
      if (!scheduler) {
        json(res, 503, {
          status: "unavailable",
          error: "scheduler health is not configured",
        });
        return;
      }
      const health = scheduler.getHealth();
      json(res, health.status === "ok" ? 200 : 503, health);
      return;
    }
    if (req.method === "GET" && req.url === "/health/security") {
      const auditLogging = getAuditLoggingHealth();
      json(res, 200, {
        status: auditLogging.healthy ? "ok" : "degraded",
        auditLogging,
      });
      return;
    }
    if (req.method === "POST" && req.url === "/admin/reconnect") {
      res.writeHead(202).end("restarting");
      console.log(
        "[worker] /admin/reconnect received — exiting for clean restart",
      );
      setTimeout(() => exit(0), exitDelayMs);
      return;
    }
    if (req.method === "GET" && req.url === "/admin/auth/status") {
      void handleAuthStatus(res, auth);
      return;
    }
    if (req.method === "POST" && req.url === "/admin/auth/secrets") {
      void handleAuthSecrets(req, res, auth);
      return;
    }
    if (req.method === "POST" && req.url === "/admin/auth/provider") {
      void handleAuthProvider(req, res, auth);
      return;
    }
    if (req.method === "POST" && req.url === "/admin/auth/begin") {
      void handleAuthBegin(req, res, auth);
      return;
    }
    if (req.method === "POST" && req.url === "/admin/auth/complete") {
      void handleAuthComplete(req, res, auth);
      return;
    }
    if (req.method === "GET" && req.url?.split("?")[0] === "/admin/sso/setup/status") {
      void handleSsoSetupStatus(req, res, ssoSetup);
      return;
    }
    if (
      req.method === "GET" &&
      req.url?.split("?")[0] === "/admin/sso/setup/environments"
    ) {
      void handleSsoSetupEnvironments(req, res, ssoSetup);
      return;
    }
    if (
      req.method === "GET" &&
      req.url?.split("?")[0] === "/admin/sso/setup/organizations"
    ) {
      void handleSsoSetupOrganizations(req, res, ssoSetup);
      return;
    }
    if (
      req.method === "GET" &&
      req.url?.split("?")[0] === "/admin/sso/setup/credentials-info"
    ) {
      void handleSsoSetupCredentialsInfo(req, res, ssoSetup);
      return;
    }
    if (req.method === "POST" && req.url === "/admin/sso/setup/ids") {
      void handleSsoSetupIds(req, res, ssoSetup);
      return;
    }
    if (req.method === "POST" && req.url === "/admin/sso/setup/credentials") {
      void handleSsoSetupCredentials(req, res, ssoSetup);
      return;
    }
    if (
      req.method === "POST" &&
      req.url === "/admin/sso/setup/generate-portal-link"
    ) {
      void handleSsoSetupGeneratePortalLink(req, res, ssoSetup);
      return;
    }
    if (req.method === "POST" && req.url === "/admin/sso/setup/check") {
      void handleSsoSetupCheck(req, res, ssoSetup);
      return;
    }
    if (req.method === "GET" && req.url === "/admin/action-descriptors") {
      json(res, 200, { descriptors: actions?.getActionDescriptors() ?? [] });
      return;
    }
    if (req.url === "/admin/graphjin/api-operations" && req.method === "GET") {
      void (async () => {
        if (!groupGrants) return json(res, 503, { error: "group grants are not configured" });
        try {
          json(res, 200, { operations: await groupGrants.apiOperations() });
        } catch (err) {
          json(res, 500, { error: err instanceof Error ? err.message : String(err) });
        }
      })();
      return;
    }
    if (req.url?.startsWith("/admin/graphjin/group-grants") && req.method === "POST") {
      void (async () => {
        if (!groupGrants) return json(res, 503, { error: "group grants are not configured" });
        try {
          const body = (await readJson(req)) as { immediate?: unknown; actorUserId?: unknown };
          const actorUserId = typeof body.actorUserId === "string" ? body.actorUserId : null;
          if (req.url === "/admin/graphjin/group-grants/enable") return json(res, 200, await groupGrants.enable(actorUserId));
          if (req.url === "/admin/graphjin/group-grants/disable") return json(res, 200, await groupGrants.disable(actorUserId));
          if (req.url !== "/admin/graphjin/group-grants") return json(res, 404, { error: "not found" });
          if (body.immediate === true) return json(res, 200, await groupGrants.apply());
          groupGrants.schedule();
          json(res, 202, { scheduled: true });
        } catch (err) {
          json(res, 500, { error: err instanceof Error ? err.message : String(err) });
        }
      })();
      return;
    }
    if (req.url === "/admin/directory/status" || req.url === "/admin/directory/sync" || req.url === "/admin/directory/users") {
      void handleDirectory(req, res, directory);
      return;
    }
    if (req.method === "GET" && req.url === "/admin/connect/providers") {
      handleConnectProviders(res, connect);
      return;
    }
    if (req.method === "GET" && req.url === "/admin/connect/deployment/status") {
      handleDeploymentConnectStatus(res, connect);
      return;
    }
    const statusMatch = req.method === "GET" && req.url?.startsWith("/admin/connect/status/");
    if (statusMatch) {
      handleConnectStatus(res, connect, req.url!);
      return;
    }
    if (req.method === "POST" && req.url === "/admin/connect/begin") {
      void handleConnectBegin(req, res, connect);
      return;
    }
    if (req.method === "POST" && req.url === "/admin/connect/complete") {
      void handleConnectComplete(req, res, connect);
      return;
    }
    if (req.method === "POST" && req.url === "/admin/connect/refresh") {
      void handleConnectRefresh(req, res, connect);
      return;
    }
    if (req.method === "POST" && req.url === "/admin/connect/disconnect") {
      void handleConnectDisconnect(req, res, connect);
      return;
    }
    if (req.method === "POST" && req.url === "/admin/events/external") {
      void handleExternalEvent(req, res, events);
      return;
    }
    if (req.method === "POST" && req.url === "/admin/events/watcher") {
      void handleWatcherEvent(req, res, watcherEvents);
      return;
    }
    if (
      req.method === "POST" &&
      req.url === "/admin/action-requests/create"
    ) {
      void handleActionRequestCreate(req, res, actionRequests);
      return;
    }
    if (req.method === "GET" && req.url === "/admin/install-policy") {
      void handleInstallPolicy(res, installPolicy);
      return;
    }
    if (req.method === "GET" && req.url === "/admin/packs") {
      void handlePacksList(res, packs);
      return;
    }
    if (req.method === "GET" && req.url === "/admin/context-remote/updates") {
      void handleContextUpdates(res, packs);
      return;
    }
    if (req.method === "POST" && req.url === "/admin/packs/upload") {
      void handlePackUpload(req, res, packs);
      return;
    }
    if (req.method === "GET" && req.url === "/admin/packs/magento/store-management") {
      void handleMagentoStoreManagementRead(res, packs);
      return;
    }
    if (req.method === "POST" && req.url === "/admin/packs/magento/store-management") {
      void handleMagentoStoreManagementWrite(req, res, packs);
      return;
    }
    const packPath = (req.url ?? "").split(/[?#]/, 1)[0] ?? "";
    const packOAuthRoute = /^\/admin\/packs\/([^/]+)\/oauth\/([^/]+)\/(status|configure|begin|complete|disconnect)$/.exec(packPath);
    if (packOAuthRoute) {
      let packId: string;
      let connectionKey: string;
      try {
        packId = decodeURIComponent(packOAuthRoute[1]!);
        connectionKey = decodeURIComponent(packOAuthRoute[2]!);
      } catch {
        json(res, 400, { error: "pack OAuth path contains invalid URL encoding" });
        return;
      }
      const action = packOAuthRoute[3]!;
      if (req.method === "GET" && action === "status") {
        void handlePackOAuth(res, packs, packId, connectionKey, action, {});
        return;
      }
      if (req.method === "POST" && action !== "status") {
        void readJson(req)
          .then((body) => handlePackOAuth(
            res,
            packs,
            packId,
            connectionKey,
            action as "begin" | "complete" | "disconnect",
            body && typeof body === "object" ? body as Record<string, unknown> : {},
          ))
          .catch(() => json(res, 400, { error: "request body must be JSON" }));
        return;
      }
    }
    const packRoute = /^\/admin\/packs\/([^/]+)(?:\/(inspect|plan|status|doctor|review|install|configure|upgrade|uninstall))?$/.exec(
      packPath,
    );
    if (packRoute) {
      let packId: string;
      try {
        packId = decodeURIComponent(packRoute[1]!);
      } catch {
        json(res, 400, { error: "pack id contains invalid URL encoding" });
        return;
      }
      const action = packRoute[2] ?? "inspect";
      if (req.method === "GET" && !["review", "install", "configure", "upgrade", "uninstall"].includes(action)) {
        void handlePackRead(res, packs, packId, action as "inspect" | "plan" | "status" | "doctor", new URL(req.url!, "http://localhost").searchParams.get("version") ?? undefined);
        return;
      }
      if (req.method === "POST" && ["review", "install", "configure", "upgrade", "uninstall"].includes(action)) {
        void handlePackApply(
          req,
          res,
          packs,
          packId,
          action as "review" | "install" | "configure" | "upgrade" | "uninstall",
        );
        return;
      }
    }
    if (req.method === "GET" && req.url === "/admin/channels/providers") {
      void handleChannelProviders(res, channels);
      return;
    }
    if (req.url === "/admin/channels/settings") {
      void handleChannelSettings(req, res, channels);
      return;
    }
    if (req.method === "POST" && req.url?.startsWith("/admin/channels/")) {
      const m = /^\/admin\/channels\/([^/]+)\/deliver(?:[?#]|$)/.exec(req.url);
      if (m) {
        void handleChannelDeliver(req, res, decodeURIComponent(m[1]!), channels);
        return;
      }
    }
    if (req.method === "POST" && req.url?.startsWith("/channels/")) {
      const m = /^\/channels\/([^/]+)\/inbound(?:[?#]|$)/.exec(req.url);
      if (m) {
        void handleChannelInbound(req, res, decodeURIComponent(m[1]!), channels);
        return;
      }
    }
    res.writeHead(404).end();
  };
}

async function handlePackOAuth(
  res: ServerResponse,
  packs: PacksHandlerSurface | null,
  packId: string,
  connectionKey: string,
  action: "status" | "configure" | "begin" | "complete" | "disconnect",
  input: Record<string, unknown>,
): Promise<void> {
  if (!packs) { json(res, 503, { error: "solution-pack service unavailable" }); return; }
  try {
    const result = action === "status"
      ? await packs.oauthStatus(packId, connectionKey)
      : action === "configure"
        ? await packs.configureOAuth(packId, connectionKey, input)
      : action === "begin"
        ? await packs.beginOAuth(packId, connectionKey, input)
        : action === "complete"
          ? await packs.completeOAuth(packId, connectionKey, input)
          : { disconnected: await packs.disconnectOAuth(packId, connectionKey) };
    json(res, 200, result);
  } catch (error) {
    json(res, 400, packFailure(error, "oauth"));
  }
}

async function handleContextUpdates(res: ServerResponse, packs: PacksHandlerSurface | null): Promise<void> {
  if (!packs) { json(res, 503, { error: "solution-pack service unavailable" }); return; }
  try {
    json(res, 200, { updates: await packs.checkContextUpdates() });
  } catch (error) {
    json(res, 400, { error: error instanceof Error ? error.message : String(error) });
  }
}

async function handlePackUpload(req: IncomingMessage, res: ServerResponse, packs: PacksHandlerSurface | null): Promise<void> {
  if (!packs) { json(res, 503, { error: "solution-pack service unavailable" }); return; }
  if (req.headers["content-type"]?.split(";", 1)[0]?.trim() !== "application/zip") {
    json(res, 415, { error: "upload a ZIP with Content-Type application/zip" }); return;
  }
  const controller = new AbortController();
  const abort = () => { if (!res.writableEnded) controller.abort(); };
  res.once("close", abort);
  try {
    const bytes = await readBody(req, 16 * 1024 * 1024);
    const actor = req.headers["x-openneko-actor"];
    if (Array.isArray(actor)) throw new Error("invalid upload actor");
    json(res, 200, await packs.upload(bytes, { actorUserId: actor ? decodeURIComponent(actor) : null, signal: controller.signal }));
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    json(res, message === "body too large" ? 413 : 400, packFailure(error, "upload"));
  } finally { res.off("close", abort); }
}

async function handlePacksList(
  res: ServerResponse,
  packs: PacksHandlerSurface | null,
): Promise<void> {
  if (!packs) {
    json(res, 503, { error: "solution-pack service unavailable" });
    return;
  }
  try {
    json(res, 200, { packs: await packs.list() });
  } catch (error) {
    json(res, 500, { error: error instanceof Error ? error.message : String(error) });
  }
}

async function handleMagentoStoreManagementRead(
  res: ServerResponse,
  packs: PacksHandlerSurface | null,
): Promise<void> {
  if (!packs) {
    json(res, 503, { error: "solution-pack service unavailable" });
    return;
  }
  try {
    json(res, 200, await packs.magentoStoreManagement());
  } catch (error) {
    json(res, 400, packFailure(error, "inspection"));
  }
}

async function handleMagentoStoreManagementWrite(
  req: IncomingMessage,
  res: ServerResponse,
  packs: PacksHandlerSurface | null,
): Promise<void> {
  if (!packs) {
    json(res, 503, { error: "solution-pack service unavailable" });
    return;
  }
  const body = await readJson(req).catch(() => null);
  if (!body || typeof body !== "object" || Array.isArray(body)) {
    json(res, 400, { error: "request body must be a JSON object" });
    return;
  }
  try {
    json(res, 200, await packs.updateMagentoStoreManagement(body as Record<string, unknown>));
  } catch (error) {
    json(res, 400, packFailure(error, "inspection"));
  }
}

async function handlePackRead(
  res: ServerResponse,
  packs: PacksHandlerSurface | null,
  packId: string,
  action: "inspect" | "plan" | "status" | "doctor",
  version?: string,
): Promise<void> {
  if (!packs) {
    json(res, 503, { error: "solution-pack service unavailable" });
    return;
  }
  try {
    const result = version && (action === "inspect" || action === "plan") ? await packs[action](packId, version) : await packs[action](packId);
    if (action === "status" && result === null) {
      json(res, 404, { error: `pack ${packId} is not installed` });
      return;
    }
    json(res, 200, result);
  } catch (error) {
    json(res, 400, packFailure(error, "inspection"));
  }
}

async function handlePackApply(
  req: IncomingMessage,
  res: ServerResponse,
  packs: PacksHandlerSurface | null,
  packId: string,
  action: "review" | "install" | "configure" | "upgrade" | "uninstall",
): Promise<void> {
  if (!packs) {
    json(res, 503, { error: "solution-pack service unavailable" });
    return;
  }
  const body = await readJson(req).catch(() => null);
  if (!body || typeof body !== "object" || Array.isArray(body)) {
    json(res, 400, { error: "request body must be a JSON object" });
    return;
  }
  try {
    const input = body as Record<string, unknown>;
    if (action === "review") {
      const operation = input.operation ?? "install";
      if (operation !== "install" && operation !== "configure" && operation !== "upgrade") throw new Error("invalid pack review operation");
      json(res, 200, await packs.review(packId, input, operation));
    } else json(res, 200, await packs[action](packId, input));
  } catch (error) {
    json(res, 400, packFailure(error, action === "review" ? "review" : action));
  }
}

async function handleActionRequestCreate(
  req: IncomingMessage,
  res: ServerResponse,
  actionRequests: ActionRequestHandlerSurface | null,
): Promise<void> {
  if (!actionRequests) {
    json(res, 503, { error: "action request preflight is not ready" });
    return;
  }
  try {
    const body = await readJson(req);
    if (!body || typeof body !== "object" || Array.isArray(body)) {
      json(res, 400, { error: "request body must be a JSON object" });
      return;
    }
    json(
      res,
      200,
      await actionRequests.create(body as Record<string, unknown>),
    );
  } catch (err) {
    json(res, 500, { error: err instanceof Error ? err.message : String(err) });
  }
}

async function handleInstallPolicy(
  res: ServerResponse,
  installPolicy: InstallPolicyHandlerSurface | null,
) {
  if (!installPolicy) {
    // No reader wired → return defaults (most-restrictive). The CLI
    // will treat this as "no privileged install paths allowed".
    json(res, 200, {
      policy: { allowGitUrlInstalls: false },
      source: "default",
    });
    return;
  }
  try {
    const policy = await installPolicy.getInstallPolicy();
    json(res, 200, { policy, source: "org" });
  } catch (err) {
    json(res, 500, { error: err instanceof Error ? err.message : String(err) });
  }
}

const MAX_SETTING_LENGTH = 8192;

async function handleDirectory(
  req: IncomingMessage,
  res: ServerResponse,
  directory: DirectoryHandlerSurface | null,
) {
  if (!directory) {
    json(res, 503, { error: "directory is not configured" });
    return;
  }
  const sync = req.url === "/admin/directory/sync";
  const users = req.url === "/admin/directory/users";
  if (req.method !== (sync || users ? "POST" : "GET")) {
    json(res, 405, { error: "method not allowed" });
    return;
  }
  try {
    if (users) {
      const body = (await readJson(req).catch(() => null)) as { email?: unknown; name?: unknown } | null;
      const email = typeof body?.email === "string" ? body.email.trim().toLowerCase() : "";
      if (!email.includes("@")) {
        json(res, 400, { error: "a valid email address is required" });
        return;
      }
      const name = typeof body?.name === "string" && body.name.trim() ? body.name.trim() : null;
      json(res, 200, await directory.createUser({ email, name }));
      return;
    }
    json(res, 200, sync ? { stats: await directory.sync() } : await directory.status());
  } catch (err) {
    const code = (err as { code?: string }).code;
    const status = code === "no_provider" ? 404 : code === "running" || code === "lockout" ? 409 : code === "unsupported" ? 400 : 500;
    json(res, status, { error: err instanceof Error ? err.message : String(err), code: code ?? null });
  }
}

async function handleAuthStatus(
  res: ServerResponse,
  auth: AuthHandlerSurface | null,
) {
  if (!auth) {
    json(res, 200, { provider: null, pending: null });
    return;
  }
  const info = auth.getAuthProvider();
  if (!info) {
    json(res, 200, { provider: null, pending: null });
    return;
  }
  // Only surface the provider once sign-in is truly usable — before
  // that the deployment stays in single-operator mode so the admin can
  // reach the setup UI. Usable means: every required env var is set,
  // AND (for manual-provisioning providers like magic link) at least
  // one active admin user is provisioned. Any bootstrapped solo owner must
  // also have a real email before switching to sign-in — otherwise the gate
  // would lock everyone out with no way back in.
  const missingEnv =
    auth.getAuthEnvGaps?.() ?? (auth.authSignInReady() ? [] : null);
  const envComplete = missingEnv !== null && missingEnv.length === 0;
  const manual = (info.provisioning ?? "automatic") === "manual";
  const needsAdminUser = (await auth.soloAdminNeedsEmail?.()) === true ||
    (manual && !(await (auth.hasProvisionedAdmin?.() ?? Promise.resolve(true))));
  const ready = envComplete && !needsAdminUser;
  const summary = {
    pluginName: info.pluginName,
    providerLabel: info.providerLabel,
    provisioning: info.provisioning ?? "automatic",
    loginHintRequired: info.loginHintRequired ?? false,
  };
  json(res, 200, {
    provider: ready ? summary : null,
    pending: ready
      ? null
      : {
          ...summary,
          missingEnv: missingEnv ?? [],
          needsAdminUser,
          configuredEnv: auth.getAuthConfiguredEnvKeys?.() ?? [],
        },
  });
}

/**
 * POST /admin/auth/provider — choose the sign-in provider. Body:
 * { provider: "scalekit" | "magic-link" | null }. The provider goes live
 * once its required values are set; null turns sign-in off.
 */
async function handleAuthProvider(
  req: IncomingMessage,
  res: ServerResponse,
  auth: AuthHandlerSurface | null,
) {
  if (!auth?.selectProvider) {
    json(res, 503, { error: "sign-in is unavailable" });
    return;
  }
  const body = (await readJson(req).catch(() => null)) as { provider?: unknown } | null;
  const provider = body?.provider;
  if (provider !== null && provider !== "scalekit" && provider !== "magic-link") {
    json(res, 400, { error: "provider must be scalekit, magic-link, or null" });
    return;
  }
  try {
    await auth.selectProvider(provider);
    json(res, 200, { ok: true });
  } catch (err) {
    json(res, 500, { error: err instanceof Error ? err.message : String(err) });
  }
}

/**
 * POST /admin/auth/secrets — write or delete values of the chosen
 * sign-in provider. Body: { values: { KEY: string | null } }.
 * Keys are restricted to the provider's declared values; loopback-only like
 * every admin route, and the web re-gates it behind an admin session.
 */
async function handleAuthSecrets(
  req: IncomingMessage,
  res: ServerResponse,
  auth: AuthHandlerSurface | null,
) {
  if (!auth?.setAuthSecret) {
    json(res, 503, { error: "auth secrets surface unavailable" });
    return;
  }
  const body = await readJson(req).catch(() => null);
  const values = (body as { values?: unknown } | null)?.values;
  if (!values || typeof values !== "object" || Array.isArray(values)) {
    json(res, 400, { error: "values (object) is required" });
    return;
  }
  const declared = new Set(auth.getAuthDeclaredEnvKeys?.() ?? []);
  const entries = Object.entries(values as Record<string, unknown>);
  if (entries.length === 0) {
    json(res, 400, { error: "values must contain at least one key" });
    return;
  }
  for (const [key, value] of entries) {
    if (!declared.has(key)) {
      json(res, 400, { error: `"${key}" is not a declared value of the sign-in provider` });
      return;
    }
    if (value !== null && (typeof value !== "string" || value.length === 0)) {
      json(res, 400, { error: `"${key}" must be a non-empty string or null` });
      return;
    }
  }
  try {
    for (const [key, value] of entries) {
      await auth.setAuthSecret(key, value as string | null);
    }
    json(res, 200, { ok: true });
  } catch (err) {
    json(res, 500, { error: err instanceof Error ? err.message : String(err) });
  }
}

async function handleExternalEvent(
  req: IncomingMessage,
  res: ServerResponse,
  events: ExternalEventHandlerSurface | null,
) {
  if (!events) {
    json(res, 503, { error: "external-event ingress unavailable" });
    return;
  }
  const body = await readJson(req).catch(() => null);
  if (!body || typeof body !== "object") {
    json(res, 400, { error: "request body must be JSON" });
    return;
  }
  const { orgId, name, source, payload, dedupeKey } = body as Record<
    string,
    unknown
  >;
  if (typeof orgId !== "string" || !orgId) {
    json(res, 400, { error: "orgId (string) is required" });
    return;
  }
  if (typeof name !== "string" || !name) {
    json(res, 400, { error: "name (string) is required" });
    return;
  }
  try {
    const result = await events.dispatchExternal({
      orgId,
      name,
      source: typeof source === "string" && source ? source : null,
      payload:
        payload && typeof payload === "object" && !Array.isArray(payload)
          ? (payload as Record<string, unknown>)
          : {},
      ...(typeof dedupeKey === "string" && dedupeKey ? { dedupeKey } : {}),
    });
    json(res, 200, result);
  } catch (err) {
    json(res, 500, {
      error: err instanceof Error ? err.message : String(err),
    });
  }
}

async function handleWatcherEvent(
  req: IncomingMessage,
  res: ServerResponse,
  watcherEvents: WatcherEventsHandlerSurface | null,
) {
  if (!watcherEvents) {
    json(res, 503, { error: "watcher event ingress unavailable" });
    return;
  }
  const rawBody = await readText(req).catch(() => null);
  let payload: Record<string, unknown> | null = null;
  try {
    const parsed = rawBody === null ? null : (JSON.parse(rawBody) as unknown);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      payload = parsed as Record<string, unknown>;
    }
  } catch {
    payload = null;
  }
  const watch = payload?.watch as Record<string, unknown> | undefined;
  const event = payload?.event as Record<string, unknown> | undefined;
  const watchId = watch && typeof watch === "object" ? watch.id : undefined;
  // The signing secret is per watch, so the watch id comes first; nothing
  // acts on the body until the signature checks out.
  if (
    rawBody === null ||
    typeof watchId !== "string" ||
    !watchId ||
    !watcherEvents.verify(
      rawBody,
      typeof req.headers["x-graphjin-signature"] === "string"
        ? req.headers["x-graphjin-signature"]
        : undefined,
      watchId,
    )
  ) {
    json(res, 401, { error: "invalid watcher event signature" });
    return;
  }
  const eventId = event && typeof event === "object" ? event.id : undefined;
  if (
    !event ||
    typeof eventId !== "string" ||
    !eventId ||
    event.watch_id !== watchId ||
    req.headers["idempotency-key"] !== eventId
  ) {
    json(res, 400, { error: "watcher event identity is invalid" });
    return;
  }
  try {
    json(res, 202, await watcherEvents.receive({ watchId, eventId, event }));
  } catch (error) {
    json(res, 500, {
      error: error instanceof Error ? error.message : String(error),
    });
  }
}

async function handleAuthBegin(
  req: IncomingMessage,
  res: ServerResponse,
  auth: AuthHandlerSurface | null,
) {
  if (!auth) {
    json(res, 503, { error: "sign-in is unavailable" });
    return;
  }
  const body = await readJson(req).catch(() => null);
  if (!body || typeof body !== "object") {
    json(res, 400, { error: "request body must be JSON" });
    return;
  }
  const { redirectUri, state, loginHint } = body as Record<string, unknown>;
  if (typeof redirectUri !== "string" || !redirectUri) {
    json(res, 400, { error: "redirectUri (string) is required" });
    return;
  }
  if (typeof state !== "string" || !state) {
    json(res, 400, { error: "state (string) is required" });
    return;
  }
  try {
    const result = await auth.beginAuth({
      redirectUri,
      state,
      loginHint:
        typeof loginHint === "string" && loginHint.length > 0
          ? loginHint
          : null,
    });
    json(res, 200, { authorizationUrl: result.authorizationUrl });
  } catch (err) {
    json(res, 500, {
      error: err instanceof Error ? err.message : String(err),
    });
  }
}

async function handleAuthComplete(
  req: IncomingMessage,
  res: ServerResponse,
  auth: AuthHandlerSurface | null,
) {
  if (!auth) {
    json(res, 503, { error: "sign-in is unavailable" });
    return;
  }
  const body = await readJson(req).catch(() => null);
  if (!body || typeof body !== "object") {
    json(res, 400, { error: "request body must be JSON" });
    return;
  }
  const { code, redirectUri, state } = body as Record<string, unknown>;
  if (typeof code !== "string" || !code) {
    json(res, 400, { error: "code (string) is required" });
    return;
  }
  if (typeof redirectUri !== "string" || !redirectUri) {
    json(res, 400, { error: "redirectUri (string) is required" });
    return;
  }
  if (typeof state !== "string" || !state) {
    json(res, 400, { error: "state (string) is required" });
    return;
  }
  try {
    const identity = await auth.completeAuth({ code, redirectUri, state });
    json(res, 200, { identity });
  } catch (err) {
    json(res, 500, {
      error: err instanceof Error ? err.message : String(err),
    });
  }
}

// ─── SSO admin setup ────────────────────────────────────────────────────

function ssoSetupOrgId(body: unknown, res: ServerResponse): string | null {
  const orgId = (body as Record<string, unknown> | null)?.orgId;
  if (typeof orgId !== "string" || !orgId) {
    json(res, 400, { error: "orgId (string) is required" });
    return null;
  }
  return orgId;
}

async function handleSsoSetupStatus(
  req: IncomingMessage,
  res: ServerResponse,
  ssoSetup: SsoSetupHandlerSurface | null,
) {
  if (!ssoSetup) {
    json(res, 503, { error: "SSO setup is unavailable" });
    return;
  }
  const url = new URL(req.url ?? "/", "http://localhost");
  const orgId = url.searchParams.get("orgId") ?? "";
  if (!orgId) {
    json(res, 400, { error: "orgId query parameter is required" });
    return;
  }
  try {
    json(res, 200, await ssoSetup.getStatus(orgId));
  } catch (err) {
    json(res, 500, { error: err instanceof Error ? err.message : String(err) });
  }
}

async function handleSsoSetupEnvironments(
  req: IncomingMessage,
  res: ServerResponse,
  ssoSetup: SsoSetupHandlerSurface | null,
) {
  if (!ssoSetup) {
    json(res, 503, { error: "SSO setup is unavailable" });
    return;
  }
  const url = new URL(req.url ?? "/", "http://localhost");
  const orgId = url.searchParams.get("orgId") ?? "";
  if (!orgId) {
    json(res, 400, { error: "orgId query parameter is required" });
    return;
  }
  try {
    json(res, 200, { environments: await ssoSetup.listEnvironments(orgId) });
  } catch (err) {
    json(res, 500, { error: err instanceof Error ? err.message : String(err) });
  }
}

async function handleSsoSetupOrganizations(
  req: IncomingMessage,
  res: ServerResponse,
  ssoSetup: SsoSetupHandlerSurface | null,
) {
  if (!ssoSetup) {
    json(res, 503, { error: "SSO setup is unavailable" });
    return;
  }
  const url = new URL(req.url ?? "/", "http://localhost");
  const orgId = url.searchParams.get("orgId") ?? "";
  const environmentId = url.searchParams.get("environmentId") ?? "";
  if (!orgId || !environmentId) {
    json(res, 400, {
      error: "orgId and environmentId query parameters are required",
    });
    return;
  }
  try {
    json(res, 200, {
      organizations: await ssoSetup.listOrganizations(orgId, environmentId),
    });
  } catch (err) {
    json(res, 500, { error: err instanceof Error ? err.message : String(err) });
  }
}

async function handleSsoSetupCredentialsInfo(
  req: IncomingMessage,
  res: ServerResponse,
  ssoSetup: SsoSetupHandlerSurface | null,
) {
  if (!ssoSetup) {
    json(res, 503, { error: "SSO setup is unavailable" });
    return;
  }
  const url = new URL(req.url ?? "/", "http://localhost");
  const orgId = url.searchParams.get("orgId") ?? "";
  const environmentId = url.searchParams.get("environmentId") ?? "";
  if (!orgId || !environmentId) {
    json(res, 400, {
      error: "orgId and environmentId query parameters are required",
    });
    return;
  }
  try {
    json(res, 200, await ssoSetup.getEnvironmentCredentials(orgId, environmentId));
  } catch (err) {
    json(res, 500, { error: err instanceof Error ? err.message : String(err) });
  }
}

async function handleSsoSetupGeneratePortalLink(
  req: IncomingMessage,
  res: ServerResponse,
  ssoSetup: SsoSetupHandlerSurface | null,
) {
  if (!ssoSetup) {
    json(res, 503, { error: "SSO setup is unavailable" });
    return;
  }
  const body = await readJson(req).catch(() => null);
  const orgId = ssoSetupOrgId(body, res);
  if (!orgId) return;
  try {
    json(res, 200, await ssoSetup.generatePortalLink(orgId));
  } catch (err) {
    json(res, 500, { error: err instanceof Error ? err.message : String(err) });
  }
}

async function handleSsoSetupIds(
  req: IncomingMessage,
  res: ServerResponse,
  ssoSetup: SsoSetupHandlerSurface | null,
) {
  if (!ssoSetup) {
    json(res, 503, { error: "SSO setup is unavailable" });
    return;
  }
  const body = (await readJson(req).catch(() => null)) as Record<
    string,
    unknown
  > | null;
  const orgId = ssoSetupOrgId(body, res);
  if (!orgId) return;
  const { environmentId, organizationId, tier } = body ?? {};
  if (typeof environmentId !== "string" || !environmentId) {
    json(res, 400, { error: "environmentId (string) is required" });
    return;
  }
  if (typeof organizationId !== "string" || !organizationId) {
    json(res, 400, { error: "organizationId (string) is required" });
    return;
  }
  try {
    await ssoSetup.setScalekitIds(orgId, {
      environmentId,
      organizationId,
      tier: typeof tier === "string" && tier ? tier : "dev",
    });
    json(res, 200, { ok: true });
  } catch (err) {
    json(res, 500, { error: err instanceof Error ? err.message : String(err) });
  }
}

async function handleSsoSetupCredentials(
  req: IncomingMessage,
  res: ServerResponse,
  ssoSetup: SsoSetupHandlerSurface | null,
) {
  if (!ssoSetup) {
    json(res, 503, { error: "SSO setup is unavailable" });
    return;
  }
  const body = (await readJson(req).catch(() => null)) as Record<
    string,
    unknown
  > | null;
  const orgId = ssoSetupOrgId(body, res);
  if (!orgId) return;
  const { environmentUrl, clientId, clientSecret } = body ?? {};
  if (typeof environmentUrl !== "string" || !environmentUrl) {
    json(res, 400, { error: "environmentUrl (string) is required" });
    return;
  }
  if (typeof clientId !== "string" || !clientId) {
    json(res, 400, { error: "clientId (string) is required" });
    return;
  }
  if (typeof clientSecret !== "string" || !clientSecret) {
    json(res, 400, { error: "clientSecret (string) is required" });
    return;
  }
  try {
    await ssoSetup.setSignInCredentials(orgId, {
      environmentUrl,
      clientId,
      clientSecret,
    });
    json(res, 200, { ok: true });
  } catch (err) {
    json(res, 500, { error: err instanceof Error ? err.message : String(err) });
  }
}

async function handleSsoSetupCheck(
  req: IncomingMessage,
  res: ServerResponse,
  ssoSetup: SsoSetupHandlerSurface | null,
) {
  if (!ssoSetup) {
    json(res, 503, { error: "SSO setup is unavailable" });
    return;
  }
  const body = await readJson(req).catch(() => null);
  const orgId = ssoSetupOrgId(body, res);
  if (!orgId) return;
  try {
    json(res, 200, await ssoSetup.checkConnection(orgId));
  } catch (err) {
    json(res, 500, { error: err instanceof Error ? err.message : String(err) });
  }
}

// ─── Connect (per-operator OAuth) ──────────────────────────────────────

function handleConnectProviders(
  res: ServerResponse,
  connect: ConnectHandlerSurface | null,
) {
  if (!connect) {
    json(res, 200, { providers: [] });
    return;
  }
  json(res, 200, { providers: connect.getConnectProviders() });
}

function handleConnectStatus(
  res: ServerResponse,
  connect: ConnectHandlerSurface | null,
  url: string,
) {
  if (!connect) {
    json(res, 200, { connected: [] });
    return;
  }
  const operatorId = decodeURIComponent(
    url.slice("/admin/connect/status/".length).split(/[?#]/, 1)[0] ?? "",
  );
  if (!operatorId) {
    json(res, 400, { error: "operatorId path segment required" });
    return;
  }
  json(res, 200, { connected: connect.getOperatorConnectStatus(operatorId) });
}

function handleDeploymentConnectStatus(
  res: ServerResponse,
  connect: ConnectHandlerSurface | null,
) {
  if (!connect) {
    json(res, 200, { connected: [] });
    return;
  }
  json(res, 200, { connected: connect.getDeploymentConnectStatus() });
}

async function handleConnectBegin(
  req: IncomingMessage,
  res: ServerResponse,
  connect: ConnectHandlerSurface | null,
) {
  if (!connect) {
    json(res, 503, { error: "connectors are unavailable" });
    return;
  }
  const body = (await readJson(req).catch(() => null)) as Record<string, unknown> | null;
  if (!body || typeof body !== "object") {
    json(res, 400, { error: "request body must be JSON" });
    return;
  }
  const pluginName = body.pluginName;
  const params = body.params;
  if (typeof pluginName !== "string" || !pluginName) {
    json(res, 400, { error: "pluginName (string) is required" });
    return;
  }
  if (!params || typeof params !== "object") {
    json(res, 400, { error: "params (object) is required" });
    return;
  }
  try {
    const result = await connect.beginConnect(pluginName, params as BeginConnectParams);
    json(res, 200, {
      authorizationUrl: result.authorizationUrl,
      ...(result.oauthState ? { oauthState: result.oauthState } : {}),
    });
  } catch (err) {
    json(res, 500, { error: err instanceof Error ? err.message : String(err) });
  }
}

async function handleConnectComplete(
  req: IncomingMessage,
  res: ServerResponse,
  connect: ConnectHandlerSurface | null,
) {
  if (!connect) {
    json(res, 503, { error: "connectors are unavailable" });
    return;
  }
  const body = (await readJson(req).catch(() => null)) as Record<string, unknown> | null;
  if (!body || typeof body !== "object") {
    json(res, 400, { error: "request body must be JSON" });
    return;
  }
  const pluginName = body.pluginName;
  const params = body.params;
  if (typeof pluginName !== "string" || !pluginName) {
    json(res, 400, { error: "pluginName (string) is required" });
    return;
  }
  if (!params || typeof params !== "object") {
    json(res, 400, { error: "params (object) is required" });
    return;
  }
  try {
    const credential = await connect.completeConnect(
      pluginName,
      params as CompleteConnectParams,
    );
    json(res, 200, { credential });
  } catch (err) {
    json(res, 500, { error: err instanceof Error ? err.message : String(err) });
  }
}

async function handleConnectRefresh(
  req: IncomingMessage,
  res: ServerResponse,
  connect: ConnectHandlerSurface | null,
) {
  if (!connect) {
    json(res, 503, { error: "connectors are unavailable" });
    return;
  }
  const body = (await readJson(req).catch(() => null)) as Record<string, unknown> | null;
  if (!body || typeof body !== "object") {
    json(res, 400, { error: "request body must be JSON" });
    return;
  }
  const pluginName = body.pluginName;
  const operatorId = body.operatorId;
  if (typeof pluginName !== "string" || !pluginName) {
    json(res, 400, { error: "pluginName (string) is required" });
    return;
  }
  if (typeof operatorId !== "string" || !operatorId) {
    json(res, 400, { error: "operatorId (string) is required" });
    return;
  }
  try {
    const credential = await connect.refreshConnect(pluginName, operatorId);
    json(res, 200, { credential });
  } catch (err) {
    json(res, 500, { error: err instanceof Error ? err.message : String(err) });
  }
}

async function handleConnectDisconnect(
  req: IncomingMessage,
  res: ServerResponse,
  connect: ConnectHandlerSurface | null,
) {
  if (!connect) {
    json(res, 503, { error: "connectors are unavailable" });
    return;
  }
  const body = (await readJson(req).catch(() => null)) as Record<string, unknown> | null;
  if (!body || typeof body !== "object") {
    json(res, 400, { error: "request body must be JSON" });
    return;
  }
  const pluginName = body.pluginName;
  const operatorId = body.operatorId;
  if (typeof pluginName !== "string" || !pluginName) {
    json(res, 400, { error: "pluginName (string) is required" });
    return;
  }
  if (typeof operatorId !== "string" || !operatorId) {
    json(res, 400, { error: "operatorId (string) is required" });
    return;
  }
  try {
    const removed = await connect.disconnect(pluginName, operatorId);
    json(res, 200, { removed });
  } catch (err) {
    json(res, 500, { error: err instanceof Error ? err.message : String(err) });
  }
}

// ─── Channel (frontend) ────────────────────────────────────────────────

async function handleChannelProviders(
  res: ServerResponse,
  channels: ChannelHandlerSurface | null,
) {
  try {
    json(res, 200, { providers: (await channels?.getChannelProviders()) ?? [] });
  } catch (err) {
    json(res, 500, { error: err instanceof Error ? err.message : String(err) });
  }
}

const CHANNEL_ID = /^[a-z][a-z0-9-]{0,63}$/;

async function handleChannelSettings(
  req: IncomingMessage,
  res: ServerResponse,
  channels: ChannelHandlerSurface | null,
) {
  try {
    if (req.method === "GET") {
      if (!channels?.settings) return json(res, 503, { error: "channel subsystem disabled" });
      return json(res, 200, { channels: await channels.settings() });
    }
    if (req.method === "POST") {
      if (!channels?.setSettings) return json(res, 503, { error: "channel subsystem disabled" });
      const body = (await readJson(req).catch(() => null)) as { channel?: unknown; values?: unknown } | null;
      const channel = typeof body?.channel === "string" ? body.channel : "";
      const values = body?.values;
      if (!CHANNEL_ID.test(channel)) return json(res, 400, { error: "channel must be a channel id" });
      if (!values || typeof values !== "object" || Array.isArray(values) || Object.keys(values).length === 0) {
        return json(res, 400, { error: "values must map at least one setting to a string or null" });
      }
      for (const value of Object.values(values)) {
        if (value !== null && (typeof value !== "string" || value.length === 0 || value.length > MAX_SETTING_LENGTH)) {
          return json(res, 400, { error: `each value must be null or a string of 1 to ${MAX_SETTING_LENGTH} characters` });
        }
      }
      await channels.setSettings(channel, values as Record<string, string | null>);
      return json(res, 200, { ok: true });
    }
    return json(res, 405, { error: "method not allowed" });
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    const status = err instanceof Error && err.name === "ChannelSettingsError" ? 400 : 500;
    return json(res, status, { error: message });
  }
}

async function handleChannelDeliver(
  req: IncomingMessage,
  res: ServerResponse,
  pluginName: string,
  channels: ChannelHandlerSurface | null,
) {
  if (!channels) {
    json(res, 503, { error: "channel subsystem disabled" });
    return;
  }
  const body = (await readJson(req).catch(() => null)) as Record<string, unknown> | null;
  if (!body || typeof body !== "object") {
    json(res, 400, { error: "request body must be JSON" });
    return;
  }
  if (!body.recipient || typeof body.recipient !== "object") {
    json(res, 400, { error: "recipient (object) is required" });
    return;
  }
  if (!Array.isArray(body.events)) {
    json(res, 400, { error: "events (array) is required" });
    return;
  }
  try {
    const result = await channels.deliver(
      pluginName,
      body.recipient as Record<string, unknown>,
      body.events,
    );
    json(res, 200, result);
  } catch (err) {
    json(res, 500, { error: err instanceof Error ? err.message : String(err) });
  }
}

async function handleChannelInbound(
  req: IncomingMessage,
  res: ServerResponse,
  pluginName: string,
  channels: ChannelHandlerSurface | null,
) {
  if (!channels) {
    json(res, 503, { error: "channel subsystem disabled" });
    return;
  }
  const rawBody = await readText(req).catch(() => null);
  if (rawBody == null) {
    json(res, 400, { error: "could not read request body" });
    return;
  }
  const headers: Record<string, string> = {};
  for (const [k, v] of Object.entries(req.headers)) {
    if (typeof v === "string") headers[k] = v;
    else if (Array.isArray(v) && v.length > 0) headers[k] = v[0]!;
  }
  try {
    const result = await channels.ingestInbound(pluginName, headers, rawBody);
    json(res, 200, result);
  } catch (err) {
    json(res, 500, { error: err instanceof Error ? err.message : String(err) });
  }
}

async function readText(req: IncomingMessage): Promise<string> {
  const chunks: Buffer[] = [];
  for await (const chunk of req) {
    chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk as string));
    if (Buffer.concat(chunks).length > 256 * 1024) {
      throw new Error("body too large");
    }
  }
  return Buffer.concat(chunks).toString("utf8");
}

function json(res: ServerResponse, status: number, body: unknown) {
  const payload = JSON.stringify(body);
  res.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Content-Length": Buffer.byteLength(payload).toString(),
  });
  res.end(payload);
}

async function readJson(req: IncomingMessage): Promise<unknown> {
  const text = (await readBody(req, 64 * 1024)).toString("utf8");
  return text ? JSON.parse(text) : {};
}

async function readBody(req: IncomingMessage, limit: number): Promise<Buffer> {
  const chunks: Buffer[] = [];
  let size = 0;
  // Keep the response socket available for a useful 413, including chunked bodies.
  for await (const chunk of req.iterator({ destroyOnReturn: false })) {
    const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk as string);
    size += bytes.length;
    if (size > limit) {
      req.resume();
      throw new Error("body too large");
    }
    chunks.push(bytes);
  }
  return Buffer.concat(chunks, size);
}
