import { mkdir } from "node:fs/promises";
import { createHash } from "node:crypto";
import { homedir, platform } from "node:os";
import { join } from "node:path";

import {
  and,
  data_source,
  db,
  desc,
  eq,
  inArray,
  like,
  llm_provider_config,
  or,
} from "@neko/db";
import { maybeDecryptSecret } from "./secrets";
import { AX_HARNESS_BINARY } from "./agent-runtime-contract";
import { isPrimaryProvider } from "./config";
import { getGoogleToken } from "./llm";
import {
  resolveProviderRuntime,
  type ProviderRuntime,
  type ProviderEndpoint,
} from "./provider-runtime";
import {
  ensureOpenShellProvider,
  verifyOpenShellGateway,
  type AgentRuntimeLaunchConfig,
} from "./work/sandbox-launcher";


type StoredRow = {
  provider: string;
  model: string | null;
  enabled: boolean;
  config: Record<string, unknown> | null;
  secrets: Record<string, unknown> | null;
};

async function loadProviderRow(
  orgId: string,
  scope: string,
): Promise<StoredRow | null> {
  try {
    const rows = await db()
      .select({
        provider: llm_provider_config.provider,
        model: llm_provider_config.model,
        enabled: llm_provider_config.enabled,
        config: llm_provider_config.config,
        secrets: llm_provider_config.secrets,
      })
      .from(llm_provider_config)
      .where(
        and(
          eq(llm_provider_config.org_id, orgId),
          eq(llm_provider_config.scope, scope),
        ),
      )
      .limit(1);
    return (rows[0] as StoredRow | undefined) ?? null;
  } catch {
    return null;
  }
}

function getHome(): string {
  return process.env.HOME || homedir();
}

function graphjinConfigPath(): string {
  const xdg = process.env.XDG_CONFIG_HOME?.trim();
  if (xdg) {
    return join(xdg, "graphjin", "client.json");
  }
  if (platform() === "darwin") {
    return join(getHome(), "Library", "Application Support", "graphjin", "client.json");
  }
  return join(getHome(), ".config", "graphjin", "client.json");
}

function deriveServerBase(graphqlUrl: string): string | null {
  try {
    const u = new URL(graphqlUrl);
    if (u.pathname.endsWith("/api/v1/graphql")) {
      return `${u.origin}${u.pathname.slice(0, -"/api/v1/graphql".length)}`;
    }
    return u.origin;
  } catch {
    return null;
  }
}

async function provisionGraphJin(orgId: string): Promise<void> {
  const rows = await db()
    .select({ graphql_url: data_source.graphql_url })
    .from(data_source)
    .where(eq(data_source.org_id, orgId))
    .orderBy(desc(data_source.is_default), data_source.created_at)
    .limit(1);
  const url = rows[0]?.graphql_url;
  if (!url) return;

  const server = deriveServerBase(url);
  if (!server) return;

  const path = graphjinConfigPath();
  await mkdir(join(path, ".."), { recursive: true });
  await atomicWriteFile(
    path,
    JSON.stringify(
      { server, token: "", expires_at: "0001-01-01T00:00:00Z" },
      null,
      2,
    ),
  );
}

export {
  SOURCES_SECRET_PLACEHOLDER,
  SOURCES_JWT_SECRET_RE,
  LEGACY_PACKAGED_DEMO_GRAPHJIN_CONFIG,
  migrateGraphjinSystemSource,
  patchGraphjinSourcesJwtSecret,
  shouldReconcileDemoSourceAuthMode,
} from "./graphjin/sources-config";
import { provisionGraphjinAgent } from "./graphjin/agent-settings";
import {
  atomicWriteFile,
  migrateGraphjinSystemSource,
  patchGraphjinSourcesJwtSecret,
  shouldReconcileDemoSourceAuthMode,
  reconcileGraphjinWritePolicy,
} from "./graphjin/sources-config";

/**
 * Sources-mode (agentic) bootstrap. Two halves, both idempotent:
 *
 * 1. When the deployment mounts the GraphJin config into the worker
 *    (OPENNEKO_GRAPHJIN_CONFIG, the demo compose does), ensure the
 *    per-org JWT secret in the seeded template matches this install's
 *    secret-key. Named local volumes can outlive a secret-key rotation,
 *    so this repairs stale concrete secrets as well as placeholders.
 *
 * 2. The packaged demo is known to run this JWT template, so reconcile its
 *    AdventureWorks row directly (including upgrades driven by the older
 *    v2.25 CLI). Other modes keep capability detection: probe the default
 *    source with a service token and only flip auth_mode 'none' to 'jwt' when
 *    it answers gj_catalog. Explicit development-header auth remains owned by
 *    the configured source; legacy servers remain 'none'.
 */
async function provisionGraphjinSourcesMode(orgId: string): Promise<void> {
  const cfgPath = process.env.OPENNEKO_GRAPHJIN_CONFIG?.trim();
  let wroteConfig = false;
  let reconcileDemoAuthMode = false;
  if (cfgPath) {
    const { readFile } = await import("node:fs/promises");
    try {
      const raw = await readFile(cfgPath, "utf8");
      const { graphjinSigningSecretB64 } = await import("./graphjin/token");
      const migrated = migrateGraphjinSystemSource(raw);
      const patched = patchGraphjinSourcesJwtSecret(
        migrated.content,
        graphjinSigningSecretB64(orgId),
      );
      const policy = reconcileGraphjinWritePolicy(patched.content);
      reconcileDemoAuthMode = shouldReconcileDemoSourceAuthMode(
        cfgPath,
        policy.content,
        process.env.OPENNEKO_STACK_MODE,
      );
      if (migrated.changed || patched.changed || policy.changed) {
        await atomicWriteFile(cfgPath, policy.content);
        wroteConfig = true;
        console.log(
          `[host-provision] reconciled GraphJin agentic config in ${cfgPath}`,
        );
      }
    } catch (e) {
      console.warn(
        `[host-provision] GraphJin agentic config reconciliation failed: ${e instanceof Error ? e.message : e}`,
      );
    }
  }

  const [src] = await db()
    .select({
      id: data_source.id,
      authMode: data_source.auth_mode,
      graphqlUrl: data_source.graphql_url,
    })
    .from(data_source)
    .where(
      reconcileDemoAuthMode
        ? and(
            eq(data_source.org_id, orgId),
            // The seeded label ("AdventureWorks") is renamed by the setup
            // wizard's data-source save, which permanently disabled this
            // reconcile. The in-stack customer GraphJin URL is the stable
            // identity of the packaged demo's source, so match either.
            or(
              eq(data_source.label, "AdventureWorks"),
              like(data_source.graphql_url, "http://graphjin:8080/%"),
            ),
          )
        : eq(data_source.org_id, orgId),
    )
    .orderBy(desc(data_source.is_default), data_source.created_at)
    .limit(1);
  if (!src?.graphqlUrl) return;
  let authMode = src.authMode;
  if (reconcileDemoAuthMode && authMode !== "jwt") {
    await db()
      .update(data_source)
      .set({ auth_mode: "jwt", updated_at: new Date() })
      .where(eq(data_source.id, src.id));
    authMode = "jwt";
    console.log(
      "[host-provision] reconciled packaged demo data source auth_mode=jwt",
    );
  }
  // `development` is an explicit transport contract, not an unknown legacy
  // mode. A public gj_catalog response does not prove that the supplied JWT
  // was verified, so using it to rewrite development -> jwt makes subsequent
  // actor-scoped calls anonymous. Only the packaged JWT demo reconciliation
  // above may intentionally replace an explicit mode.
  if (authMode === "development") return;
  if (authMode === "jwt" && !wroteConfig) return;

  const { mintGraphjinToken } = await import("./graphjin/token");
  const token = mintGraphjinToken({ orgId, userId: null, role: "service" });
  // Only when WE just changed the config does the server need a reload
  // window (reload_on_config_change, observed ~10s on 3.18.37). An
  // in-stack GraphJin may simply still be starting alongside us, so give
  // it a few short attempts; an unmanaged external or legacy endpoint
  // gets a single quick probe — an unreachable one must not stall boot.
  const inStackGraphjin = /^https?:\/\/(graphjin|neko-graphjin|records-graphjin)[:/]/.test(
    src.graphqlUrl,
  );
  const maxAttempts = wroteConfig ? 6 : inStackGraphjin ? 4 : 1;
  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    try {
      const res = await fetch(src.graphqlUrl, {
        method: "POST",
        headers: {
          "content-type": "application/json",
          authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          query: `query { gj_catalog(id: "help:discovery") { id } }`,
        }),
        signal: AbortSignal.timeout(2_500),
      });
      if (res.ok) {
        const body = (await res.json()) as {
          data?: { gj_catalog?: unknown };
          errors?: unknown[];
        };
        // gj_catalog(id:) returns one object; list queries an array.
        const rows = body.data?.gj_catalog;
        const answered = Array.isArray(rows) ? rows.length > 0 : Boolean(rows);
        if (answered && !body.errors?.length) {
          if (authMode !== "jwt") {
            await db()
              .update(data_source)
              .set({ auth_mode: "jwt", updated_at: new Date() })
              .where(eq(data_source.id, src.id));
            console.log(
              `[host-provision] data source answers gj_catalog with an actor token — auth_mode=jwt (agentic mode on)`,
            );
          }
          return;
        }
      }
    } catch {
      // unreachable / not reloaded yet — retry below.
    }
    if (attempt < maxAttempts) await new Promise((r) => setTimeout(r, 3000));
  }
}

function decryptSecrets(secrets: Record<string, unknown> | null): Record<string, string> {
  if (!secrets) return {};
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(secrets)) {
    const decrypted = maybeDecryptSecret(v);
    if (decrypted) out[k] = decrypted;
  }
  return out;
}

/**
 * Derive the agent-sandbox egress from the org's model config: the API host
 * (explicit base_url, else the provider default) and the env var the backend
 * reads. Egress is scoped to the connecting binary (/proc/<pid>/exe).
 */
function deriveAgentEgress(
  row: StoredRow,
): { endpoints: ProviderEndpoint[]; runtime: ProviderRuntime } {
  if (!isPrimaryProvider(row.provider)) {
    throw new Error(`Unsupported primary provider: ${row.provider}`);
  }
  const runtime = resolveProviderRuntime({
    provider: row.provider,
    model: row.model ?? "",
    config: row.config,
  });
  return {
    endpoints: [runtime.endpoint],
    runtime,
  };
}

/**
 * Self-configure the agent sandbox from the org's model config — derive the
 * egress (host/key-env; explicit env overrides win) and sync the
 * model key into a gateway-side provider so the proxy injects it on the
 * wire (the key never enters the box). Replaces the manual `openshell
 * provider create` + hand-set egress env.
 */
// Operator-supplied provider/host/key values win permanently; empty or
// unset means "derive from the org's provider config". These values are
// read-only. Persisted provider settings must never be copied into process.env:
// Next.js can evaluate route bundles at different times, causing one bundle to
// mistake another bundle's derived values for operator overrides.
const OPERATOR_AGENT_ENV = {
  provider: process.env.OPENNEKO_AGENT_MODEL_PROVIDER || "",
  host: process.env.OPENNEKO_AGENT_MODEL_HOST || "",
  keyEnv: process.env.OPENNEKO_AGENT_MODEL_KEY_ENV || "",
};

function modelHosts(value: string): ProviderEndpoint[] {
  return value
    .split(",")
    .map((entry) => entry.trim())
    .filter(Boolean)
    .map((entry) => {
      if (entry.includes("://")) {
        const url = new URL(entry);
        return {
          host: url.hostname,
          ...(url.port
            ? { port: Number(url.port) }
            : url.protocol === "http:"
              ? { port: 80 }
              : {}),
        };
      }
      const hostPort = /^(.*):(\d+)$/.exec(entry);
      return hostPort
        ? { host: hostPort[1]!, port: Number(hostPort[2]) }
        : { host: entry };
    });
}

function serializeModelEndpoint(endpoint: ProviderEndpoint): string {
  return endpoint.port ? `${endpoint.host}:${endpoint.port}` : endpoint.host;
}

export function gatewayProviderName(orgId: string): string {
  return `openneko-agent-${createHash("sha256").update(orgId).digest("hex").slice(0, 16)}`;
}

function agentRuntimeLaunchConfig(args: {
  orgId: string;
  derivedEndpoints?: ProviderEndpoint[];
  derivedKeyEnv?: string;
  hasApiKey: boolean;
  /** Versions the provider by credential, so a new key never changes one in use. */
  credentialFingerprint?: string;
}): AgentRuntimeLaunchConfig {
  const derivedEndpoints = args.derivedEndpoints ?? [];
  const keyEnv = OPERATOR_AGENT_ENV.keyEnv || args.derivedKeyEnv || "";
  // OpenShell 0.1.2 addresses profile credentials by their injected env var.
  const configuredCredential = process.env.OPENNEKO_AGENT_MODEL_CREDENTIAL;
  const credentialName =
    !configuredCredential || configuredCredential === "api_key"
      ? "MODEL_API_KEY"
      : configuredCredential;
  const modelProvider =
    OPERATOR_AGENT_ENV.provider ||
    (args.hasApiKey
      ? `${gatewayProviderName(args.orgId)}${args.credentialFingerprint ? `-${args.credentialFingerprint}` : ""}`
      : "");

  return {
    ...(modelProvider ? { modelProvider } : {}),
    modelHosts: OPERATOR_AGENT_ENV.host
      ? modelHosts(OPERATOR_AGENT_ENV.host)
      : derivedEndpoints,
    ...(keyEnv
      ? { keyAliases: [{ from: credentialName, to: keyEnv }] }
      : {}),
  };
}

async function provisionOpenShellRuntime(
  orgId: string,
): Promise<AgentRuntimeLaunchConfig> {
  const row = await loadProviderRow(orgId, "primary");
  if (!row || !row.enabled) {
    return agentRuntimeLaunchConfig({ orgId, hasApiKey: false });
  }

  const { endpoints, runtime } = deriveAgentEgress(row);
  const apiKey =
    runtime.credentialSource === "google-adc"
      ? await getGoogleToken()
      : runtime.credentialSource === "stored-api-key"
        ? decryptSecrets(row.secrets).apiKey
        : undefined;
  const launchConfig = agentRuntimeLaunchConfig({
    orgId,
    derivedEndpoints: endpoints,
    derivedKeyEnv: runtime.keyEnv,
    hasApiKey: Boolean(apiKey),
    ...(apiKey ? { credentialFingerprint: createHash("sha256").update(apiKey).digest("hex").slice(0, 8) } : {}),
  });

  if (!apiKey) return launchConfig;
  // The gateway can be restarting exactly while we register the provider
  // (deploys recreate it alongside the worker). A single failed attempt
  // used to be swallowed upstream, leaving a healthy worker with no
  // provider on the gateway and every channel run broken until a restart.
  let lastError: unknown;
  for (let attempt = 1; attempt <= 3; attempt++) {
    try {
      await ensureOpenShellProvider({
        providerName: launchConfig.modelProvider ?? "openneko-agent",
        apiKey,
        ...(OPERATOR_AGENT_ENV.provider ? {} : { family: gatewayProviderName(orgId) }),
        gatewayName: process.env.OPENSHELL_GATEWAY || undefined,
        gatewayEndpoint: process.env.OPENSHELL_GATEWAY_ENDPOINT || undefined,
      });
      lastError = undefined;
      break;
    } catch (e) {
      lastError = e;
      if (attempt < 3) {
        console.warn(
          `[host-provision] OpenShell provider sync attempt ${attempt} failed; retrying: ${e instanceof Error ? e.message : e}`,
        );
        const base = Number(process.env.OPENNEKO_PROVISION_RETRY_BASE_MS ?? 2_000);
        await new Promise((resolve) => setTimeout(resolve, attempt * base));
      }
    }
  }
  if (lastError !== undefined) throw lastError;
  console.log(
    `[host-provision] OpenShell agent runtime self-configured: provider="${launchConfig.modelProvider ?? ""}" egress="${launchConfig.modelHosts?.map(serializeModelEndpoint).join(",") ?? ""}" binary="${AX_HARNESS_BINARY}" keyEnv=${launchConfig.keyAliases?.[0]?.to ?? ""}`,
  );
  return launchConfig;
}

/**
 * Readiness gate for any operation that is about to launch an agent sandbox.
 * This deliberately performs a fresh gateway RPC instead of consulting a
 * process-local flag: the gateway may have failed or been omitted from a
 * Compose invocation after the web/worker process itself became healthy.
 *
 * Once connectivity is proven, re-sync the org's model provider so the exact
 * path onboarding will use is configured before work is enqueued.
 */
export async function verifyAgentRuntimeReady(orgId: string): Promise<void> {
  await verifyOpenShellGateway({
    gatewayName: process.env.OPENSHELL_GATEWAY || undefined,
    gatewayEndpoint: process.env.OPENSHELL_GATEWAY_ENDPOINT || undefined,
  });
  await provisionOpenShellRuntime(orgId);
}

type HostProvisionState = {
  appliedRevision?: string;
  launchConfig?: AgentRuntimeLaunchConfig;
  tail: Promise<AgentRuntimeLaunchConfig>;
};

const provisionedOrgs = new Map<string, HostProvisionState>();

type ProvisionHostConfigOptions = {
  requireOpenShellSync?: boolean;
};

/** Build a content revision without retaining provider secrets in process
 * state. Every setting that changes the files or sandbox launch contract is
 * covered, including same-provider key rotation. */
async function hostConfigRevision(
  orgId: string,
): Promise<{ revision: string; refreshCredential: boolean }> {
  const [providers, sources] = await Promise.all([
    db()
      .select({
        id: llm_provider_config.id,
        scope: llm_provider_config.scope,
        provider: llm_provider_config.provider,
        model: llm_provider_config.model,
        enabled: llm_provider_config.enabled,
        config: llm_provider_config.config,
        secrets: llm_provider_config.secrets,
        updatedAt: llm_provider_config.updated_at,
      })
      .from(llm_provider_config)
      .where(
        and(
          eq(llm_provider_config.org_id, orgId),
          inArray(llm_provider_config.scope, ["primary", "agent", "graphjin-agent"]),
        ),
      )
      .orderBy(llm_provider_config.scope),
    db()
      .select({
        id: data_source.id,
        graphqlUrl: data_source.graphql_url,
        mcpUrl: data_source.mcp_url,
        authMode: data_source.auth_mode,
        enabled: data_source.enabled,
        isDefault: data_source.is_default,
        updatedAt: data_source.updated_at,
      })
      .from(data_source)
      .where(eq(data_source.org_id, orgId))
      .orderBy(desc(data_source.is_default), data_source.created_at),
  ]);

  return {
    revision: createHash("sha256")
      .update(JSON.stringify({ providers, sources }))
      .digest("hex"),
    // Vertex access tokens are short-lived. Re-mint and replace the
    // gateway-side credential for every run instead of memoizing it like a
    // static API key. The token remains outside the sandbox.
    refreshCredential: providers.some(
      (provider) => provider.scope === "primary" && provider.provider === "vertex",
    ),
  };
}

/**
 * Worker-owned provisioning gate for every agent-backed job.
 *
 * A successful pass is reused only while the persisted host configuration is
 * unchanged. This matters because provider settings are saved by the web
 * process, whose environment mutations cannot reach an already-running
 * worker. Calls for one org are serialized so a key rotation or provider
 * family switch cannot race two gateway provider syncs.
 */
export async function ensureHostConfigProvisioned(
  orgId: string,
): Promise<AgentRuntimeLaunchConfig> {
  const requested = await hostConfigRevision(orgId);
  let state = provisionedOrgs.get(orgId);
  if (!state) {
    state = {
      tail: Promise.resolve(
        agentRuntimeLaunchConfig({ orgId, hasApiKey: false }),
      ),
    };
    provisionedOrgs.set(orgId, state);
  }
  if (
    state.appliedRevision === requested.revision &&
    state.launchConfig &&
    !requested.refreshCredential
  ) {
    return state.launchConfig;
  }

  const provisionState = state;
  const run = provisionState.tail.catch(() =>
    agentRuntimeLaunchConfig({ orgId, hasApiKey: false }),
  ).then(async () => {
    // Re-read after earlier callers finish. A settings save may have landed
    // while this call was queued, and only the newest revision should win.
    const current = await hostConfigRevision(orgId);
    if (
      provisionState.appliedRevision === current.revision &&
      provisionState.launchConfig &&
      !current.refreshCredential
    ) {
      return provisionState.launchConfig;
    }
    const launchConfig = await provisionHostConfig(orgId, {
      requireOpenShellSync: true,
    });
    provisionState.appliedRevision = current.revision;
    provisionState.launchConfig = launchConfig;
    return launchConfig;
  });
  provisionState.tail = run;
  return run;
}

export async function provisionHostConfig(
  orgId: string,
  options: ProvisionHostConfigOptions = {},
): Promise<AgentRuntimeLaunchConfig> {
  try {
    await provisionGraphJin(orgId);
  } catch (e) {
    console.warn(
      `[host-provision] graphjin write failed: ${e instanceof Error ? e.message : e}`,
    );
  }

  try {
    await provisionGraphjinSourcesMode(orgId);
  } catch (e) {
    console.warn(
      `[host-provision] sources-mode provision failed: ${e instanceof Error ? e.message : e}`,
    );
  }

  try {
    await provisionGraphjinAgent(orgId);
  } catch (e) {
    console.warn(
      `[host-provision] GraphJin agent provision failed: ${e instanceof Error ? e.message : e}`,
    );
  }

  let openShellError: unknown;
  let launchConfig = agentRuntimeLaunchConfig({ orgId, hasApiKey: false });
  try {
    launchConfig = await provisionOpenShellRuntime(orgId);
  } catch (e) {
    openShellError = e;
    console.warn(
      `[host-provision] OpenShell runtime provision failed: ${e instanceof Error ? e.message : e}`,
    );
  }

  if (openShellError && options.requireOpenShellSync) {
    throw openShellError;
  }
  return launchConfig;
}
