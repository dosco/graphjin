import { listUploadedPacks, loadUploadedPack, storePackUpload, snapshotUploadedPack, type AvailablePack } from "./uploads.js";
import { parse as parseYaml } from "yaml";
import { extractValueAtPath, resolveWatcherVariables } from "@neko/llm/workflows";
import { mapSavedQueryMetric } from "../jobs/deterministic-metric.js";
import { bindPackQueries, declarativeGraphjinUpdate, declarativePackPermissions, installedPackPolicyEnabled, packPolicyControlsWrite, packVariables } from "./declarative.js";
import { createHmac, randomUUID } from "node:crypto";
import {
  cp,
  lstat,
  mkdir,
  readFile,
  readdir,
  rename,
  rm,
  writeFile,
} from "node:fs/promises";
import { basename, dirname, join, relative, resolve, sep } from "node:path";
import {
  localConfigPath,
  action_policy,
  and,
  data_source,
  db,
  desc,
  eq,
  inArray,
  metric,
  magento_attribute_classification,
  magento_store_control,
  pack_action_definition,
  pack_artifact,
  pack_install,
  pack_operation,
  processing_job,
  pool,
  sql,
  watcher,
  workflow_definition,
} from "@neko/db";
import { ensureOrgWorkspace } from "@neko/llm/work";
import { graphjinQuery, graphjinSigningSecret, mintGraphjinToken, resolvePackSource, verifyPackQueryTables, graphjinConfigPatchHash, type PackSourceSelection } from "@neko/llm/graphjin";
import {
  canonicalHash,
  DEFAULT_MAGENTO_ATTRIBUTE_CLASSIFICATIONS,
  DEFAULT_MAGENTO_DOMAIN_CONTROLS,
  loadSolutionPack,
  planPack,
  type PackArtifact,
  type PackPlan,
  type MagentoDomain,
  type SolutionPackBundle,
} from "@neko/packs";
import {
  readSecretsStore,
  writeSecretsStore,
} from "@neko/secrets";
import { notifyChannelsChanged, prepareChannelsForPack } from "../channels/runtime.js";
import { PackOAuthService, packOAuthBinding } from "./oauth.js";
import {
  assertOwnedPath,
  graphjinApiArtifacts,
  installGraphjinFiles,
  installSkills,
  pathExists,
  stageOwnedRemoval,
} from "./materialize.js";
import { assertNativeTargetsAvailable } from "./native-targets.js";
import { enqueuePackMetricRefreshes, runMagentoAnalyticsSmoke, runPackReadPreflight } from "./preflight.js";
import { applyPackGraphjinConfig, applyPackGraphjinConfigLive } from "./graphjin-config.js";
import { GRAPHJIN_NO_KEYSTORE_MESSAGE, readLiveGraphjinConfig, resolveGraphjinTarget, type GraphjinTarget } from "./graphjin-target.js";
import {
  runMagentoPreflight,
  type MagentoPreflightResult,
} from "./magento-preflight.js";
import {
  inspectPackArtifactCurrent,
  inspectInstalledPackArtifactCurrent,
  nativeArtifactStateHash,
  packArtifactLocator,
} from "./artifact-state.js";
import { packSecretSection, resolveInputs, resolveSecrets, secretEnvKey } from "./configuration.js";
import { magentoCapsFromInputs, magentoGraphjinUpdate } from "./magento-graphjin.js";
import { MagentoPackAdminService } from "./magento-admin.js";
import {
  artifactLocatorFromMetadata,
  artifactRecord,
  boundPackLocator,
  findSavedQuery,
  operatorReadinessDetail,
} from "./pack-artifacts.js";

export { resolveInputs } from "./configuration.js";

const PACK_ID = /^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/;
type PackInstallRequest = {
  deferConfiguration?: boolean;
  version?: string;
  reviewHash?: string;
  inputs?: Record<string, unknown>;
  dataSourceId?: string;
  sourceBindings?: Record<string, string>;
  secrets?: Record<string, string>;
  secretRefs?: Record<string, string>;
  idempotencyKey?: string;
  actorUserId?: string | null;
};

type PackUninstallRequest = {
  idempotencyKey?: string;
  actorUserId?: string | null;
};

type PackStatus = {
  packId: string;
  version: string;
  status: string;
  readiness: Record<string, { status: string; reason: string | null }>;
  installedAt: string | null;
  lastError: string | null;
  configuration: { required?: boolean; inputs: Record<string, unknown>; dataSourceId?: string; sourceBindings: Record<string, string> };
};

type PackDoctorResult = {
  packId: string;
  status: "ready" | "degraded" | "blocked";
  checks: Array<{
    id: string;
    status: "ready" | "optional" | "blocked";
    detail: string;
  }>;
};

type PackRuntime = {
  source?: PackSourceSelection;
  bindings: Record<string, string>;
  bindingHashes: Record<string, string>;
  readiness: string[];
  tables?: Array<Record<string, string>>;
};

function storedRuntime(config: Record<string, unknown>): PackRuntime | undefined {
  return config._runtime as PackRuntime | undefined;
}

function graphjinEndpoint(url: string): string {
  const clean = url.replace(/\/+$/, "");
  return clean.endsWith("/api/v1/graphql") ? clean : `${clean}/api/v1/graphql`;
}

/** Built-in channels the pack turns on, persisted on the install row. */
function packChannels(manifest: SolutionPackBundle["manifest"]): Record<string, unknown> {
  return manifest.channels?.length ? { _channels: [...manifest.channels] } : {};
}

/** Turn on the pack's channels: mint their generated secrets, then restart inbound. */
async function activatePackChannels(orgId: string, packId: string, channels: readonly string[] | undefined): Promise<void> {
  if (channels?.length) {
    await prepareChannelsForPack(packId, channels).catch((error) => {
      console.warn(`[packs] could not prepare channels for ${packId}: ${error instanceof Error ? error.message : String(error)}`);
    });
  }
  notifyChannelsChanged(orgId);
}

function packRoot(): string {
  return resolve(process.env.OPENNEKO_PACKS_DIR?.trim() || join(process.cwd(), "packs"));
}

export class PackService {
  private readonly oauth: PackOAuthService;
  private readonly magentoAdmin: MagentoPackAdminService;

  constructor(
    private readonly orgId: string,
    private readonly embeddedRoot = packRoot(),
    private readonly uploadedRoot?: string,
  ) {
    this.magentoAdmin = new MagentoPackAdminService(orgId);
    this.oauth = new PackOAuthService(orgId, {
      loadBundle: (packId, version) => this.bundle(packId, version),
      syncInstalledConnection: (packId, connectionKey, enabled) => this.syncInstalledOAuthConnection(packId, connectionKey, enabled),
    });
  }

  private uploadsRoot(): string { return this.uploadedRoot ?? join(dirname(localConfigPath()), "agents", "orgs", encodeURIComponent(this.orgId).replaceAll(".", "%2E"), "packs"); }

  private async bundle(packId: string, version?: string): Promise<AvailablePack> {
    if (!PACK_ID.test(packId)) throw new Error("invalid pack id");
    const bundle: AvailablePack = await pathExists(join(this.embeddedRoot, packId, "pack.yaml"))
      ? await loadSolutionPack(join(this.embeddedRoot, packId))
      : await loadUploadedPack(this.uploadsRoot(), this.orgId, packId, version);
    if (packId !== "magento") {
      for (const artifact of bundle.artifacts) {
        const value = artifact.kind === "source" ? artifactRecord(artifact) : {};
        if (artifact.kind === "source" && value.kind === "database" && !value.host) {
          artifact.targetRef = `${packId}.binding.${String(value.name)}`;
        }
      }
    }
    return bundle;
  }

  private async installedBundle(installation: typeof pack_install.$inferSelect): Promise<AvailablePack> {
    const bundle = await this.bundle(installation.pack_id, installation.source === "uploaded" ? installation.version : undefined);
    if (installation.source === "uploaded") {
      const pinned = installation.config._bundle as { contentHash?: string } | undefined;
      if (!bundle.upload || bundle.upload.contentHash !== pinned?.contentHash) throw new Error("installed pack contents do not match the approved version");
    }
    return bundle;
  }

  configureOAuth(packId: string, connectionKey: string, input: Record<string, unknown>) {
    return this.oauth.configure(packId, connectionKey, input);
  }

  oauthStatus(packId: string, connectionKey: string): Promise<Record<string, unknown>> {
    return this.oauth.status(packId, connectionKey);
  }

  beginOAuth(packId: string, connectionKey: string, input: Record<string, unknown>): Promise<{ authorizationUrl: string }> {
    return this.oauth.begin(packId, connectionKey, input);
  }

  completeOAuth(packId: string, connectionKey: string, input: Record<string, unknown>): Promise<Record<string, unknown>> {
    return this.oauth.complete(packId, connectionKey, input);
  }

  disconnectOAuth(packId: string, connectionKey: string): Promise<boolean> {
    return this.oauth.disconnect(packId, connectionKey);
  }

  private async syncInstalledOAuthConnection(packId: string, connectionKey: string, enabled: boolean): Promise<void> {
    const [installation] = await db().select().from(pack_install).where(and(
      eq(pack_install.org_id, this.orgId),
      eq(pack_install.pack_id, packId),
      eq(pack_install.status, "installed"),
    )).limit(1);
    if (!installation) return;
    const authored = await this.installedBundle(installation);
    if (!authored.manifest.artifacts.graphjin) return;
    const runtime = storedRuntime(installation.config);
    if (!runtime?.source) throw new Error(`installed pack ${packId} has no data source binding`);
    const bundle = bindPackQueries(authored, runtime.bindings).bundle;
    const connection = bundle.manifest.oauth.find((value) => value.key === connectionKey);
    if (!connection) throw new Error(`installed pack ${packId} does not declare OAuth connection ${connectionKey}`);
    const inputs = resolveInputs(bundle, Object.fromEntries(
      Object.entries(installation.config).filter(([key]) => bundle.manifest.inputs.some((value) => value.key === key)),
    ));
    const secrets = enabled ? await resolveSecrets(bundle, {}) : null;
    const source = await resolvePackSource(this.orgId, runtime.source);
    const graphjin = await this.graphjinTarget(source.graphqlUrl);
    const ownedSources = bundle.artifacts.filter((artifact) => artifact.kind === "source" && !runtime.bindings[artifact.key]);
    const connectionSources = ownedSources.filter(
      (artifact) => (artifact.content as { auth?: { token?: string } }).auth?.token === `{{secret.${connection.accessToken}}}`,
    );
    if (!enabled && connectionSources.length === 0) {
      throw new Error(`OAuth connection ${connectionKey} is not bound to an installed API source`);
    }
    const update = enabled
      ? declarativeGraphjinUpdate(bundle, inputs, secrets!.values, [], runtime.bindings)
      : {
          source_patches: connectionSources.map((artifact) => ({
              name: artifact.targetRef,
              read_only: true,
              access: { read: "blocked", write: "blocked", delete: "blocked" },
            })),
        };
    const ownedSourceNames = new Set(ownedSources.map((artifact) => artifact.targetRef));
    if (graphjin.mode === "api") {
      const apiArtifacts = enabled ? await graphjinApiArtifacts({ bundle, values: inputs }) : null;
      await applyPackGraphjinConfigLive({
        target: graphjin,
        update: enabled ? declarativeGraphjinUpdate(bundle, inputs, secrets!.values, [], runtime.bindings, apiArtifacts!) : update,
        ownedSourceNames,
        ownedSavedQueryNames: new Set(apiArtifacts?.savedQueries.map((query) => query.name) ?? []),
      });
    } else {
      await applyPackGraphjinConfig({
        endpoint: graphjinEndpoint(source.graphqlUrl),
        orgId: this.orgId,
        configFile: graphjin.configFile,
        update,
        ownedSourceNames,
      });
    }
    const connectedSourceNames = new Set(
      connectionSources.map((artifact) => String((artifact.content as Record<string, unknown>).name)),
    );
    const actionKinds = bundle.artifacts.flatMap((artifact) => {
      if (artifact.kind !== "action") return [];
      const value = artifactRecord(artifact);
      const adapter = value.adapter as Record<string, unknown> | undefined;
      return adapter?.kind === "graphjin_api_operation" && connectedSourceNames.has(String(adapter.source))
        ? [String(value.kind)]
        : [];
    });
    if (actionKinds.length > 0) {
      await db().update(pack_action_definition).set({
        readiness: enabled ? "ready" : "blocked",
        readiness_reason: enabled ? null : "oauth_not_connected",
        updated_at: new Date(),
      }).where(and(
        eq(pack_action_definition.org_id, this.orgId),
        inArray(pack_action_definition.kind, actionKinds),
      ));
    }
  }

  refreshOAuthConnections(): Promise<number> {
    return this.oauth.refreshDue();
  }

  async upload(bytes: Buffer, request: { actorUserId?: string | null; signal?: AbortSignal } = {}) {
    const embedded = await readdir(this.embeddedRoot, { withFileTypes: true }).catch((error: NodeJS.ErrnoException) => { if (error.code === "ENOENT") return []; throw error; });
    const reservedIds = ["magento", ...embedded.filter(entry => entry.isDirectory() && PACK_ID.test(entry.name)).map(entry => entry.name)];
    return storePackUpload({ root: this.uploadsRoot(), orgId: this.orgId, bytes, reservedIds, actorUserId: request.actorUserId, signal: request.signal });
  }

  private reviewHash(bundle: AvailablePack, plan: PackPlan, operation: string, request: PackInstallRequest, inputs: Record<string, unknown>, secrets: Record<string, string>, runtime: PackRuntime): string {
    const reviewedSecrets = Object.fromEntries(Object.entries(secrets).filter(([key]) =>
      bundle.manifest.secrets.find((secret) => secret.key === key)?.purpose !== "pack_oauth_token",
    ));
    return createHmac("sha256", graphjinSigningSecret(this.orgId)).update("pack-review/v1:").update(canonicalHash({
      orgId: this.orgId, actor: request.actorUserId ?? null, operation, plan,
      contentHash: bundle.upload?.contentHash ?? bundle.bundleHash, inputs, secrets: reviewedSecrets, runtime,
      secretRefs: request.secretRefs ?? {},
    })).digest("hex");
  }

  /** Where this org's pack changes reach GraphJin: its API, or the shared config folder. */
  private async graphjinTarget(graphqlUrl: string): Promise<GraphjinTarget> {
    return resolveGraphjinTarget({
      endpoint: graphjinEndpoint(graphqlUrl),
      orgId: this.orgId,
      configFile: process.env.OPENNEKO_GRAPHJIN_CONFIG,
    });
  }

  /** Fails review when the GraphJin says it cannot store the pack's secrets. */
  private async assertGraphjinKeystore(selection: NonNullable<PackRuntime["source"]>): Promise<void> {
    const target = await resolvePackSource(this.orgId, selection)
      .then((source) => this.graphjinTarget(source.graphqlUrl))
      .catch(() => null);
    if (target?.mode === "api" && target.keystoreConfigured === false) throw new Error(GRAPHJIN_NO_KEYSTORE_MESSAGE);
  }

  private async installedGraphjinTarget(config: Record<string, unknown>): Promise<GraphjinTarget | null> {
    const selection = storedRuntime(config)?.source;
    if (!selection) return null;
    try {
      const source = await resolvePackSource(this.orgId, selection);
      return await this.graphjinTarget(source.graphqlUrl);
    } catch {
      return null;
    }
  }

  async review(packId: string, request: PackInstallRequest = {}, operation: "install" | "configure" | "upgrade" = "install") {
    const [existing] = await db().select().from(pack_install).where(and(eq(pack_install.org_id, this.orgId), eq(pack_install.pack_id, packId))).orderBy(desc(pack_install.created_at)).limit(1);
    if (operation !== "install" && existing?.status !== "installed") throw new Error("pack must be installed before reviewing this operation");
    if (operation === "configure" && request.version && request.version !== existing?.version) throw new Error("configure cannot change the installed pack version");
    const bundle = operation === "configure" && existing ? await this.installedBundle(existing) : await this.bundle(packId, request.version);
    const prior = Object.fromEntries(Object.entries(existing?.config ?? {}).filter(([key]) => bundle.manifest.inputs.some(input => input.key === key)));
    const inputs = resolveInputs(bundle, { ...prior, ...request.inputs });
    const secrets = await resolveSecrets(bundle, request);
    const runtime = await this.runtime(bundle, request, storedRuntime(existing?.config ?? {}));
    if (packId !== "magento") {
      const bound = bindPackQueries(bundle, runtime.bindings);
      runtime.tables = [...new Map([...(storedRuntime(existing?.config ?? {})?.tables ?? []), ...bound.tables].map(table => [table.name, table])).values()];
      declarativeGraphjinUpdate(bound.bundle, inputs, secrets.values, [], runtime.bindings);
      if (bundle.manifest.artifacts.graphjin && Object.keys(secrets.values).length && runtime.source) {
        await this.assertGraphjinKeystore(runtime.source);
      }
    }
    const plan = await this.planBundle(bundle);
    await assertNativeTargetsAvailable({ orgId: this.orgId, bundle, plan });
    return { ...await this.inspect(packId, bundle.manifest.metadata.version), operation, inputs, runtime, plan,
      secrets: Object.keys(secrets.values), reviewHash: this.reviewHash(bundle, plan, operation, request, inputs, secrets.values, runtime) };
  }

  private async runtime(bundle: SolutionPackBundle, request: PackInstallRequest, prior?: PackRuntime): Promise<PackRuntime> {
    if (!bundle.manifest.artifacts.graphjin) {
      if (request.dataSourceId || Object.keys(request.sourceBindings ?? {}).length) throw new Error("this pack does not use a data source");
      if (prior?.source) throw new Error("removing GraphJin artifacts requires uninstall first");
      return { bindings: {}, bindingHashes: {}, readiness: Object.keys(bundle.manifest.health.readiness) };
    }
    await verifyPackQueryTables(prior?.tables);
    let source: PackSourceSelection;
    if (!request.dataSourceId && prior?.source) source = await resolvePackSource(this.orgId, prior.source);
    else {
      if (!request.dataSourceId && (bundle as AvailablePack).upload) throw new Error("select an enabled organization dataSourceId before installing a custom pack");
      const available = await db().select({ id: data_source.id, graphqlUrl: data_source.graphql_url, authMode: data_source.auth_mode, isDefault: data_source.is_default }).from(data_source)
        .where(and(eq(data_source.org_id, this.orgId), eq(data_source.enabled, true), ...(request.dataSourceId ? [eq(data_source.id, request.dataSourceId)] : [])))
        .orderBy(desc(data_source.is_default), data_source.created_at).limit(2);
      const selected = available[0];
      if (!request.dataSourceId && available.length > 1 && !selected?.isDefault) throw new Error("Choose a default data connection before installing this pack");
      if (!selected) throw new Error("selected pack data source is unavailable in this organization");
      source = { id: selected.id, graphqlUrl: selected.graphqlUrl, authMode: selected.authMode };
    }
    if (bundle.manifest.oauth.some(connection => connection.scope === "user") && source.authMode !== "jwt") throw new Error("Personal connections require a GraphJin data connection with JWT authentication");
    const bindingHashes: Record<string, string> = {};
    const references = bundle.artifacts.filter(artifact => artifact.kind === "source" && artifact.targetRef.startsWith(`${bundle.manifest.metadata.id}.binding.`));
    const bindings = request.sourceBindings ?? Object.fromEntries(Object.entries(prior?.bindings ?? {}).filter(([key]) => references.some(artifact => artifact.key === key)));
    if (Object.keys(bindings).some(key => !references.some(artifact => artifact.key === key))) throw new Error("unknown pack source binding");
    if (references.length) {
      const graphjin = await this.graphjinTarget(source.graphqlUrl);
      const config = graphjin.mode === "api"
        ? await readLiveGraphjinConfig(graphjin)
        : parseYaml(await readFile(graphjin.configFile, "utf8")) as { sources?: Array<Record<string, unknown>> };
      const live = await graphjinQuery<{ gj_config?: { sources?: Array<Record<string, unknown>> } }>({
        baseUrl: graphjinEndpoint(source.graphqlUrl), configurationOnly: true, headers: { authorization: `Bearer ${mintGraphjinToken({ orgId: this.orgId, userId: "pack-binding", role: "admin", ttlSeconds: 60 })}` },
        query: "query { gj_config(id: \"current\") { sources } }", signal: AbortSignal.timeout(30_000),
      });
      if (live.errors?.length || !Array.isArray(live.data?.gj_config?.sources)) throw new Error("cannot verify selected GraphJin sources");
      for (const artifact of references) {
        const name = bindings[artifact.key];
        const current = config.sources?.find(value => value.name === name);
        const running = live.data!.gj_config!.sources!.find(value => value.name === name);
        if (!name || !current || !running || current.kind !== "database" || current.read_only !== true || running.kind !== "database" || running.read_only !== true) throw new Error(`binding ${artifact.key} requires an existing read-only database source`);
        for (const key of ["name", "kind", "type", "host", "port", "dbname"]) {
          if (String(current[key] ?? "") !== String(running[key] ?? "")) throw new Error(`binding ${artifact.key} differs from the selected live source`);
        }
        const engines = bundle.manifest.compatibility.databases.map(value => value.engine);
        if (engines.length && !engines.includes(current.type as "postgres")) throw new Error(`binding ${artifact.key} database engine is incompatible`);
        bindingHashes[artifact.key] = graphjinConfigPatchHash(current);
        if (!request.sourceBindings && prior?.bindingHashes[artifact.key] && prior.bindingHashes[artifact.key] !== bindingHashes[artifact.key]) throw new Error(`binding ${artifact.key} changed; explicitly reconfigure it`);
      }
    }
    return { source, bindings, bindingHashes, readiness: Object.keys(bundle.manifest.health.readiness) };
  }

  private async replayIdempotentOperation(
    packId: string,
    idempotencyKey: string | undefined,
  ): Promise<PackStatus | null> {
    if (!idempotencyKey) return null;
    const [prior] = await db()
      .select({
        packId: pack_install.pack_id,
        status: pack_operation.status,
        error: pack_operation.error,
      })
      .from(pack_operation)
      .innerJoin(pack_install, eq(pack_install.id, pack_operation.pack_install_id))
      .where(
        and(
          eq(pack_operation.org_id, this.orgId),
          eq(pack_operation.idempotency_key, idempotencyKey),
        ),
      )
      .limit(1);
    if (!prior) return null;
    if (prior.packId !== packId) {
      throw new Error(`idempotency key was already used for pack ${prior.packId}`);
    }
    if (prior.status === "succeeded") {
      const status = await this.status(packId);
      if (!status) throw new Error(`pack ${packId} status is missing for completed operation`);
      return status;
    }
    if (prior.status === "failed") {
      throw new Error(prior.error || `the previous ${packId} operation failed`);
    }
    throw new Error(`the ${packId} operation for this idempotency key is still ${prior.status}`);
  }

  async list(): Promise<Array<{ id: string; name: string; version: string; installed: boolean; status: string; lastError: string | null; source: "embedded" | "uploaded" }>> {
    const entries = await readdir(this.embeddedRoot, { withFileTypes: true }).catch((error: NodeJS.ErrnoException) => { if (error.code === "ENOENT") return []; throw error; });
    const installations = await db()
      .select({ packId: pack_install.pack_id, status: pack_install.status, lastError: pack_install.last_error })
      .from(pack_install)
      .where(eq(pack_install.org_id, this.orgId));
    const installationByPack = new Map(installations.map((row) => [row.packId, row]));
    const packDirectories: string[] = [];
    for (const entry of entries) {
      if (!entry.isDirectory() || !PACK_ID.test(entry.name)) continue;
      try {
        const manifest = await lstat(join(this.embeddedRoot, entry.name, "pack.yaml"));
        if (manifest.isFile() || manifest.isSymbolicLink()) packDirectories.push(entry.name);
      } catch (error) {
        if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
      }
    }
    const bundles = [...await Promise.all(packDirectories.map((packId) => this.bundle(packId))), ...await listUploadedPacks(this.uploadsRoot(), this.orgId)];
    return bundles.map((bundle) => {
      const installation = installationByPack.get(bundle.manifest.metadata.id);
      return {
        id: bundle.manifest.metadata.id,
        source: bundle.upload ? "uploaded" as const : "embedded" as const,
        name: bundle.manifest.metadata.name,
        version: bundle.manifest.metadata.version,
        installed: installation?.status === "installed",
        status: installation?.status ?? "available",
        lastError: installation?.lastError ?? null,
      };
    });
  }

  async inspect(packId: string, version?: string): Promise<Record<string, unknown>> {
    const bundle = await this.bundle(packId, version);
    return {
      source: bundle.upload ? "uploaded" : "embedded",
      ...(bundle.upload ? { upload: bundle.upload } : {}),
      manifest: bundle.manifest,
      manifestHash: bundle.manifestHash,
      bundleHash: bundle.bundleHash,
      bindingRequirements: bundle.artifacts.filter(artifact => artifact.kind === "source" && artifactRecord(artifact).kind === "database" && !artifactRecord(artifact).host).map(artifact => ({ key: artifact.key, name: String(artifactRecord(artifact).name) })),
      permissions: packId !== "magento" ? declarativePackPermissions(bundle) : {
        database: "view-only reporting",
        apiWrite: "specific Magento changes require approval and must be enabled individually",
        customerPii: "available to authenticated read-only queries",
        paymentData: "operational fields available; credentials and raw gateway payloads blocked",
      },
    };
  }

  async plan(packId: string, version?: string): Promise<PackPlan> {
    return this.planBundle(await this.bundle(packId, version));
  }

  private async planBundle(bundle: SolutionPackBundle): Promise<PackPlan> {
    const packId = bundle.manifest.metadata.id;
    const [installation] = await db()
      .select({ id: pack_install.id, config: pack_install.config })
      .from(pack_install)
      .where(
        and(
          eq(pack_install.org_id, this.orgId),
          eq(pack_install.pack_id, packId),
        ),
      )
      .orderBy(desc(pack_install.created_at))
      .limit(1);
    const artifacts = installation
      ? await db().select().from(pack_artifact).where(eq(pack_artifact.pack_install_id, installation.id))
      : [];
    const desiredByIdentity = new Map(
      bundle.artifacts.map((artifact) => [`${artifact.kind}:${artifact.key}`, artifact]),
    );
    const needsGraphjin = artifacts.some((artifact) => ["source", "relationships", "spec", "saved_query"].includes(artifact.artifact_kind));
    const graphjin = installation && needsGraphjin ? await this.installedGraphjinTarget(installation.config ?? {}) : null;
    const installed = await Promise.all(artifacts.map(async (artifact) => {
      const desired = desiredByIdentity.get(`${artifact.artifact_kind}:${artifact.artifact_key}`);
      const metadata = artifact.metadata ?? {};
      const needsGraphjinConfig = ["source", "relationships", "spec", "saved_query"].includes(
        artifact.artifact_kind,
      );
      const inspected = graphjin || !needsGraphjinConfig
        ? await inspectInstalledPackArtifactCurrent({
            orgId: this.orgId,
            kind: artifact.artifact_kind as PackArtifact["kind"],
            targetRef: artifact.target_ref,
            metadata,
            graphjin,
            lastAppliedHash: artifact.last_applied_hash,
            ...(desired ? { fallbackArtifact: desired } : {}),
          })
        : null;
      const baseline = metadata.stateHashVersion === 1
        ? artifact.last_applied_hash
        : inspected ?? artifact.last_applied_hash;
      return {
        kind: artifact.artifact_kind as PackArtifact["kind"],
        key: artifact.artifact_key,
        targetRef: artifact.target_ref,
        desiredHash: artifact.desired_hash,
        lastAppliedHash: baseline,
        currentHash: inspected,
        ownership: artifact.ownership as "managed" | "modified" | "detached" | "retired",
      };
    }));
    return planPack(bundle, installed);
  }

  private async persistLegacyStateBaselines(
    installationId: string,
    plan: PackPlan,
    bundle: SolutionPackBundle,
  ): Promise<void> {
    const rows = await db().select().from(pack_artifact)
      .where(eq(pack_artifact.pack_install_id, installationId));
    const entries = new Map(plan.entries.map((entry) => [`${entry.kind}:${entry.key}`, entry]));
    const desired = new Map(bundle.artifacts.map((artifact) => [`${artifact.kind}:${artifact.key}`, artifact]));
    await db().transaction(async (tx) => {
      for (const row of rows) {
        if (row.metadata?.stateHashVersion === 1) continue;
        const entry = entries.get(`${row.artifact_kind}:${row.artifact_key}`);
        if (entry?.action !== "noop" || !entry.currentHash) continue;
        await tx.update(pack_artifact).set({
          last_applied_hash: entry.currentHash,
          metadata: {
            ...(row.metadata ?? {}),
            ...(desired.get(`${row.artifact_kind}:${row.artifact_key}`)
              ? { locator: packArtifactLocator(desired.get(`${row.artifact_kind}:${row.artifact_key}`)!) }
              : {}),
            stateHashVersion: 1,
          },
          updated_at: new Date(),
        }).where(eq(pack_artifact.id, row.id));
      }
    });
  }

  async status(packId: string): Promise<PackStatus | null> {
    const [installation] = await db()
      .select()
      .from(pack_install)
      .where(and(eq(pack_install.org_id, this.orgId), eq(pack_install.pack_id, packId)))
      .orderBy(desc(pack_install.created_at))
      .limit(1);
    if (!installation) return null;
    const artifacts = await db()
      .select({ readiness: pack_artifact.readiness, reason: pack_artifact.readiness_reason })
      .from(pack_artifact)
      .where(eq(pack_artifact.pack_install_id, installation.id));
    const readiness: PackStatus["readiness"] = installation.config._definitionOnly ? { setup: { status: "blocked", reason: "configuration_required" } } : {};
    for (const artifact of artifacts) {
      if (!artifact.reason?.startsWith("operator:")) continue;
      const operatorMatch = /^operator:([^:]+):(.*)$/.exec(artifact.reason ?? "");
      const capability = operatorMatch?.[1] ?? "operator";
      if (!readiness[capability] || artifact.readiness === "blocked") {
        readiness[capability] = {
          status: artifact.readiness,
          reason: operatorMatch?.[2] ?? artifact.reason?.replace(/^operator:/, "") ?? null,
        };
      }
    }
    for (const capability of storedRuntime(installation.config)?.readiness ?? []) {
      readiness[capability] ??= { status: installation.status === "installed" ? "ready" : "blocked", reason: installation.status === "installed" ? null : "pack_not_active" };
    }
    return {
      packId,
      version: installation.version,
      status: installation.status,
      readiness,
      installedAt: installation.installed_at?.toISOString() ?? null,
      lastError: installation.last_error,
      configuration: {
        required: installation.config._definitionOnly === true,
        inputs: Object.fromEntries(Object.entries(installation.config).filter(([key]) => !key.startsWith("_"))),
        dataSourceId: storedRuntime(installation.config)?.source?.id,
        sourceBindings: storedRuntime(installation.config)?.bindings ?? {},
      },
    };
  }

  async doctor(packId: string): Promise<PackDoctorResult> {
    const [installation] = await db()
      .select()
      .from(pack_install)
      .where(and(eq(pack_install.org_id, this.orgId), eq(pack_install.pack_id, packId)))
      .orderBy(desc(pack_install.created_at))
      .limit(1);
    if (!installation || installation.status === "removed") {
      return {
        packId,
        status: "blocked",
        checks: [{ id: "installation", status: "blocked", detail: "pack is not installed" }],
      };
    }

    const bundle = await this.installedBundle(installation);
    if (packId !== "magento") {
      try {
        const inputs = resolveInputs(bundle, Object.fromEntries(Object.entries(installation.config).filter(([key]) => bundle.manifest.inputs.some(input => input.key === key))));
        const secrets = await resolveSecrets(bundle, {});
        const runtime = await this.runtime(bundle, {}, storedRuntime(installation.config));
        declarativeGraphjinUpdate(bundle, inputs, secrets.values, [], runtime.bindings);
        const source = runtime.source;
        if (source) await runPackReadPreflight(bindPackQueries(bundle, runtime.bindings).bundle, source.graphqlUrl, this.orgId, inputs);
        return { packId, status: "ready", checks: [{ id: source ? "queries" : "configuration", status: "ready", detail: source ? "Pack queries and response mappings passed." : "Pack configuration passed. No data connection is required." }] };
      } catch {
        return { packId, status: "blocked", checks: [{ id: "queries", status: "blocked", detail: "Pack configuration or query preflight failed." }] };
      }
    }

    const checks: PackDoctorResult["checks"] = [];
    const declaredInputs = new Set(bundle.manifest.inputs.map((input) => input.key));
    const storedInputs = Object.fromEntries(
      Object.entries(installation.config as Record<string, unknown>)
        .filter(([key]) => declaredInputs.has(key)),
    );
    let preflight: MagentoPreflightResult | null = null;
    try {
      const inputs = resolveInputs(bundle, storedInputs);
      const resolvedSecrets = await resolveSecrets(bundle, {});
      preflight = await runMagentoPreflight({
        host: String(inputs["database.host"]),
        port: Number(inputs["database.port"]),
        database: String(inputs["database.name"]),
        username: resolvedSecrets.values["database.analytics_username"]!,
        password: resolvedSecrets.values["database.analytics_password"]!,
        tablePrefix: String(inputs["magento.table_prefix"] ?? ""),
        baseUrl: String(inputs["magento.base_url"]),
        storeCode: String(inputs["magento.store_code"] ?? "all"),
        integrationToken: resolvedSecrets.values["magento.integration_token"] ?? null,
        customersEnabled: Boolean(inputs["magento.customers_enabled"]),
      });
      checks.push({
        id: "analytics",
        status: "ready",
        detail: `${preflight.databaseType} ${preflight.databaseVersion}; SELECT-only grants verified`,
      });
      checks.push({
        id: "magento",
        status: "ready",
        detail: `${preflight.magentoVersion}; store IDs ${preflight.storeIds.join(", ")}`,
      });
    } catch (error) {
      checks.push({
        id: "analytics",
        status: "blocked",
        detail: error instanceof Error ? error.message : String(error),
      });
    }

    const selection = storedRuntime(installation.config)?.source;
    const bound = selection ? await resolvePackSource(this.orgId, selection) : null;
    const [fallback] = bound || !bundle.manifest.artifacts.graphjin ? [] : await db()
      .select({ graphqlUrl: data_source.graphql_url })
      .from(data_source)
      .where(and(eq(data_source.org_id, this.orgId), eq(data_source.enabled, true)))
      .orderBy(desc(data_source.is_default), data_source.created_at)
      .limit(1);
    const source = bound ?? fallback;
    const configFile = process.env.OPENNEKO_GRAPHJIN_CONFIG?.trim();
    if (!source?.graphqlUrl || !configFile || !(await pathExists(configFile))) {
      checks.push({
        id: "graphjin",
        status: "blocked",
        detail: "customer GraphJin endpoint/config volume is unavailable",
      });
    } else {
      try {
        const result = await graphjinQuery<{ gj_config?: { catalog_revision?: string } }>({
          baseUrl: graphjinEndpoint(source.graphqlUrl),
          headers: {
            authorization: `Bearer ${mintGraphjinToken({
              orgId: this.orgId,
              userId: "pack-doctor",
              role: "admin",
              ttlSeconds: 60,
            })}`,
          },
          query: 'query { gj_config(id: "current") { catalog_revision } }',
        });
        if (result.errors?.length || !result.data?.gj_config?.catalog_revision) {
          throw new Error(result.errors?.map((value) => value.message).join("; ") || "catalog revision unavailable");
        }
        await runMagentoAnalyticsSmoke(graphjinEndpoint(source.graphqlUrl), this.orgId);
        checks.push({
          id: "graphjin",
          status: "ready",
          detail: `catalog ${result.data.gj_config.catalog_revision}`,
        });
        checks.push({
          id: "analytics-query",
          status: "ready",
          detail: "Magento sales_order smoke query succeeded",
        });
      } catch (error) {
        checks.push({
          id: "graphjin",
          status: "blocked",
          detail: error instanceof Error ? error.message : String(error),
        });
      }
    }

    if (preflight) {
      for (const domain of ["catalog", "inventory", "orders", "promotions", "content", "customers"] as const) {
        const reason = preflight.operatorDomains[domain];
        checks.push({
          id: `changes-${domain}`,
          status: reason === "ready" ? "ready" : reason === "domain_disabled" ? "optional" : "blocked",
          detail: operatorReadinessDetail(reason),
        });
      }
      checks.push({
        id: "bulk-consumers",
        status: preflight.bulkConsumerReadiness === "ready" ? "ready" : "blocked",
        detail: preflight.bulkConsumerReadiness === "ready"
          ? "Magento async bulk consumers completed recently."
          : "Magento async bulk consumers are not reporting recent successful work.",
      });
    } else {
      checks.push({
        id: "changes",
        status: "optional",
        detail: operatorReadinessDetail(null),
      });
    }
    const requiredBlocked = checks.some(
      (check) => !check.id.startsWith("changes") && check.id !== "bulk-consumers" && check.status === "blocked",
    );
    const operatorBlocked = checks.some(
      (check) => (check.id.startsWith("changes-") || check.id === "bulk-consumers") && check.status === "blocked",
    );
    return {
      packId,
      status: requiredBlocked ? "blocked" : operatorBlocked ? "degraded" : "ready",
      checks,
    };
  }

  async magentoStoreManagement(): Promise<Record<string, unknown>> {
    return this.magentoAdmin.read();
  }

  async updateMagentoStoreManagement(input: Record<string, unknown>): Promise<Record<string, unknown>> {
    return this.magentoAdmin.update(input);
  }

  async install(packId: string, request: PackInstallRequest = {}): Promise<PackStatus> {
    return this.apply(packId, request, "install");
  }

  async configure(packId: string, request: PackInstallRequest = {}): Promise<PackStatus> {
    const current = await this.status(packId);
    if (!current || current.status !== "installed") {
      throw new Error(`pack ${packId} must be installed before it can be configured`);
    }
    return this.apply(packId, request, "configure");
  }

  async upgrade(packId: string, request: PackInstallRequest = {}): Promise<PackStatus> {
    const current = await this.status(packId);
    if (!current || current.status !== "installed") {
      throw new Error(`pack ${packId} must be installed before it can be upgraded`);
    }
    return this.apply(packId, request, "upgrade");
  }

  async uninstall(packId: string, request: PackUninstallRequest = {}): Promise<PackStatus> {
    const replay = await this.replayIdempotentOperation(packId, request.idempotencyKey);
    if (replay) return replay;
    let [installation] = await db().select().from(pack_install).where(and(
      eq(pack_install.org_id, this.orgId),
      eq(pack_install.pack_id, packId),
    )).orderBy(desc(pack_install.created_at)).limit(1);
    if (!installation) throw new Error(`pack ${packId} is not installed`);
    if (installation.status === "removed") {
      const removed = await this.status(packId);
      if (!removed) throw new Error(`pack ${packId} removal status is missing`);
      return removed;
    }

    const bundle = await this.installedBundle(installation);
    let usesGraphjin = Boolean(bundle.manifest.artifacts.graphjin || storedRuntime(installation.config)?.source);
    const client = await pool().connect();
    let operationId: string | null = null;
    let graphjinRestore: (() => Promise<void>) | null = null;
    let secretsRestore: (() => Promise<void>) | null = null;
    const stagedRemovals: Array<{
      restore: () => Promise<void>;
      commit: () => Promise<void>;
    }> = [];
    try {
      await client.query("SELECT pg_advisory_lock(hashtextextended($1, 0))", [`pack:${this.orgId}:${packId}`]);
      const lockedReplay = await this.replayIdempotentOperation(packId, request.idempotencyKey);
      if (lockedReplay) return lockedReplay;
      [installation] = await db().select().from(pack_install).where(and(
        eq(pack_install.org_id, this.orgId),
        eq(pack_install.pack_id, packId),
      )).orderBy(desc(pack_install.created_at)).limit(1);
      if (!installation) throw new Error(`pack ${packId} is not installed`);
      if (installation.status === "removed") {
        const removed = await this.status(packId);
        if (!removed) throw new Error(`pack ${packId} removal status is missing`);
        return removed;
      }
      if (["installing", "upgrading", "removing"].includes(installation.status)) {
        throw new Error(`pack ${packId} already has an operation in progress`);
      }
      usesGraphjin = usesGraphjin && !installation.config._definitionOnly;
      const plan = await this.planBundle(bundle);
      const conflicts = plan.entries.filter((entry) => entry.action === "conflict");
      if (conflicts.length > 0) {
        const conflictKeys = new Set(conflicts.map((entry) => `${entry.kind}:${entry.key}`));
        const rows = await db().select().from(pack_artifact)
          .where(eq(pack_artifact.pack_install_id, installation.id));
        await db().transaction(async (tx) => {
          for (const row of rows) {
            if (!conflictKeys.has(`${row.artifact_kind}:${row.artifact_key}`)) continue;
            await tx.update(pack_artifact).set({ ownership: "modified", updated_at: new Date() })
              .where(eq(pack_artifact.id, row.id));
          }
        });
        throw new Error(`pack uninstall has ${conflicts.length} drift conflict(s); modified artifacts were preserved`);
      }
      await db().update(pack_install).set({
        status: "removing",
        last_error: null,
        updated_at: new Date(),
      }).where(eq(pack_install.id, installation.id));
      const [operation] = await db().insert(pack_operation).values({
        pack_install_id: installation.id,
        org_id: this.orgId,
        operation_type: "uninstall",
        actor_user_id: request.actorUserId ?? null,
        status: "running",
        requested_version: installation.version,
        idempotency_key: request.idempotencyKey ?? randomUUID(),
        plan: plan as unknown as Record<string, unknown>,
        plan_hash: plan.hash,
        phase: "removing_graphjin",
        started_at: new Date(),
      }).returning({ id: pack_operation.id });
      operationId = operation!.id;
      await db().update(pack_install).set({ operation_id: operationId })
        .where(eq(pack_install.id, installation.id));

      await verifyPackQueryTables(storedRuntime(installation.config)?.tables);
      const selection = storedRuntime(installation.config)?.source;
      const bound = selection ? await resolvePackSource(this.orgId, selection) : null;
      const [fallback] = bound || !usesGraphjin ? [] : await db().select({ graphqlUrl: data_source.graphql_url })
        .from(data_source)
        .where(and(eq(data_source.org_id, this.orgId), eq(data_source.enabled, true)))
        .orderBy(desc(data_source.is_default), data_source.created_at)
        .limit(1);
      const source = bound ?? fallback;
      if (usesGraphjin && !source?.graphqlUrl) {
        throw new Error("The GraphJin data connection for this pack is unavailable");
      }
      const graphjin = usesGraphjin ? await this.graphjinTarget(source!.graphqlUrl) : null;
      const configFile = graphjin?.mode === "files" ? graphjin.configFile : "";
      const provenance = await db().select().from(pack_artifact)
        .where(eq(pack_artifact.pack_install_id, installation.id));
      const desiredByIdentity = new Map(
        bundle.artifacts.map((artifact) => [`${artifact.kind}:${artifact.key}`, artifact]),
      );
      const sourceNames = provenance
        .filter((artifact) => artifact.artifact_kind === "source" && !artifact.metadata?.borrowedSource)
        .map((artifact) => artifact.target_ref);
      // GraphJin 3.18 cannot remove a source while tables still reference it,
      // does not expose remove_tables through gj_config, and treats tables: []
      // as a no-op. Revoke every source capability instead. The disabled
      // source/table metadata is retained so uninstall is fail closed and a
      // later pack install can safely reclaim it.
      const revokedSources = sourceNames.map((name) => ({
        name,
        read_only: true,
        access: {
          read: "blocked",
          write: "blocked",
          delete: "blocked",
        },
      }));
      const removedSavedQueries = provenance.flatMap((artifact) => {
        const target = artifact.metadata?.materializedTarget;
        return typeof target === "string" && target.startsWith("graphjin:saved_query:")
          ? [target.slice("graphjin:saved_query:".length)]
          : [];
      });
      const revoked = graphjin?.mode === "api" ? await applyPackGraphjinConfigLive({
        target: graphjin,
        update: {
          source_patches: revokedSources,
          ...(removedSavedQueries.length ? { remove_saved_queries: removedSavedQueries } : {}),
        },
        ownedSourceNames: new Set(sourceNames),
      }) : usesGraphjin ? await applyPackGraphjinConfig({
        endpoint: graphjinEndpoint(source!.graphqlUrl),
        orgId: this.orgId,
        configFile,
        update: { source_patches: revokedSources },
        ownedSourceNames: new Set(sourceNames),
      }) : { restore: async () => {} };
      graphjinRestore = revoked.restore;

      const configRoot = dirname(configFile);
      const workspace = await ensureOrgWorkspace(this.orgId);
      for (const artifact of provenance) {
        const target = artifact.metadata?.materializedTarget;
        if (typeof target !== "string" || target.startsWith("graphjin:")) continue;
        if (artifact.artifact_kind === "spec" || artifact.artifact_kind === "saved_query") {
          stagedRemovals.push(
            await stageOwnedRemoval(assertOwnedPath(configRoot, target, "GraphJin pack file")),
          );
        } else if (artifact.artifact_kind === "skill") {
          stagedRemovals.push(
            await stageOwnedRemoval(assertOwnedPath(workspace.skillsRoot, target, "pack skill")),
          );
        }
      }

      const currentSecrets = await readSecretsStore();
      const secretSection = packSecretSection(packId);
      const nextSecrets = { ...currentSecrets };
      delete nextSecrets[secretSection];
      await writeSecretsStore(nextSecrets);
      secretsRestore = () => writeSecretsStore(currentSecrets);

      const retiredHashes = new Map<string, string>();
      // An api-mode spec lives inside its source, which uninstall revokes but
      // keeps, so the spec stays in GraphJin and keeps its live hash.
      for (const artifact of provenance.filter(
        (value) => value.artifact_kind === "source" || value.artifact_kind === "relationships" ||
          (value.artifact_kind === "spec" && String(value.metadata?.materializedTarget ?? "").startsWith("graphjin:spec:")),
      )) {
        const identity = `${artifact.artifact_kind}:${artifact.artifact_key}`;
        const current = await inspectInstalledPackArtifactCurrent({
          orgId: this.orgId,
          kind: artifact.artifact_kind as "source" | "relationships" | "spec",
          targetRef: artifact.target_ref,
          metadata: artifact.metadata ?? {},
          graphjin,
          ...(desiredByIdentity.get(identity)
            ? { fallbackArtifact: desiredByIdentity.get(identity)! }
            : {}),
        });
        if (!current) throw new Error(`pack GraphJin artifact ${identity} disappeared during uninstall`);
        retiredHashes.set(identity, current);
      }
      await db().transaction(async (tx) => {
        for (const artifact of provenance) {
          const identity = `${artifact.artifact_kind}:${artifact.artifact_key}`;
          const locator = artifactLocatorFromMetadata(
            artifact.metadata,
            desiredByIdentity.get(identity),
          );
          if (artifact.artifact_kind === "metric") {
            const role = String(locator.role ?? "");
            if (!role) throw new Error(`pack metric ${artifact.artifact_key} has no uninstall locator`);
            const [row] = await tx.select().from(metric).where(and(
              eq(metric.org_id, this.orgId),
              eq(metric.role, role),
              eq(metric.slug, String(locator.slug ?? artifact.target_ref)),
            )).limit(1);
            if (!row) throw new Error(`pack metric ${artifact.target_ref} disappeared during uninstall`);
            await tx.update(metric).set({ active: false, updated_at: new Date() }).where(eq(metric.id, row.id));
            retiredHashes.set(identity, nativeArtifactStateHash("metric", { ...row, active: false } as unknown as Record<string, unknown>));
          } else if (artifact.artifact_kind === "watcher") {
            const [row] = await tx.select().from(watcher).where(and(
              eq(watcher.org_id, this.orgId),
              eq(watcher.name, String(locator.name ?? artifact.target_ref)),
            )).limit(1);
            if (!row) throw new Error(`pack watcher ${artifact.target_ref} disappeared during uninstall`);
            await tx.update(watcher).set({ enabled: false, updated_at: new Date() }).where(eq(watcher.id, row.id));
            retiredHashes.set(identity, nativeArtifactStateHash("watcher", { ...row, enabled: false } as unknown as Record<string, unknown>));
          } else if (artifact.artifact_kind === "workflow") {
            const [row] = await tx.select().from(workflow_definition).where(and(
              eq(workflow_definition.org_id, this.orgId),
              eq(workflow_definition.owner_user_id, ""),
              eq(workflow_definition.name, String(locator.name ?? artifact.target_ref)),
            )).limit(1);
            if (!row) throw new Error(`pack workflow ${artifact.target_ref} disappeared during uninstall`);
            await tx.update(workflow_definition).set({
              enabled: false,
              cron_enabled: false,
              updated_at: new Date(),
            }).where(eq(workflow_definition.id, row.id));
            retiredHashes.set(identity, nativeArtifactStateHash("workflow", {
              ...row,
              enabled: false,
              cron_enabled: false,
            } as unknown as Record<string, unknown>));
          } else if (artifact.artifact_kind === "policy") {
            const [row] = await tx.select().from(action_policy).where(and(
              eq(action_policy.org_id, this.orgId),
              eq(action_policy.name, String(locator.name ?? artifact.target_ref)),
            )).limit(1);
            if (!row) throw new Error(`pack policy ${artifact.target_ref} disappeared during uninstall`);
            await tx.update(action_policy).set({ enabled: false, updated_at: new Date() })
              .where(eq(action_policy.id, row.id));
            retiredHashes.set(identity, nativeArtifactStateHash("policy", { ...row, enabled: false } as unknown as Record<string, unknown>));
          } else if (artifact.artifact_kind === "action") {
            const [row] = await tx.select().from(pack_action_definition).where(and(
              eq(pack_action_definition.org_id, this.orgId),
              eq(pack_action_definition.kind, String(locator.kind ?? artifact.target_ref)),
            )).limit(1);
            if (!row) throw new Error(`pack action ${artifact.target_ref} disappeared during uninstall`);
            await tx.update(pack_action_definition).set({
              enabled: false,
              readiness: "blocked",
              readiness_reason: "pack_uninstalled",
              updated_at: new Date(),
            }).where(eq(pack_action_definition.id, row.id));
            retiredHashes.set(identity, nativeArtifactStateHash("action", { ...row, enabled: false } as unknown as Record<string, unknown>));
          }

          const materializedHash = retiredHashes.get(identity) ?? canonicalHash({ retired: true, identity });
          await tx.update(pack_artifact).set({
            last_applied_hash: materializedHash,
            ownership: "retired",
            readiness: "not_applicable",
            readiness_reason: null,
            metadata: {
              ...(artifact.metadata ?? {}),
              stateHashVersion: 1,
              retiredMissing: !retiredHashes.has(identity),
              ...(
                artifact.artifact_kind === "source" || artifact.artifact_kind === "relationships"
                  ? { retainedForGraphjinCompatibility: true }
                  : {}
              ),
            },
            updated_at: new Date(),
          }).where(eq(pack_artifact.id, artifact.id));
        }
        await tx.execute(sql`delete from pack_user_connection where org_id=${this.orgId} and pack_install_id=${installation.id}`);
        await tx.update(pack_install).set({
          status: "removed",
          removed_at: new Date(),
          updated_at: new Date(),
        }).where(eq(pack_install.id, installation.id));
        await tx.update(pack_operation).set({
          status: "succeeded",
          phase: "complete",
          compensation_status: "not_required",
          completed_at: new Date(),
          updated_at: new Date(),
        }).where(eq(pack_operation.id, operationId!));
      });

      for (const removal of stagedRemovals) {
        await removal.commit().catch((error) => {
          console.warn(`[packs] uninstall cleanup warning: ${error instanceof Error ? error.message : String(error)}`);
        });
      }
      notifyChannelsChanged(this.orgId);
      const removed = await this.status(packId);
      if (!removed) throw new Error("pack status missing after uninstall");
      return removed;
    } catch (error) {
      let compensationFailed = false;
      if (secretsRestore) await secretsRestore().catch(() => { compensationFailed = true; });
      for (const removal of stagedRemovals.reverse()) {
        await removal.restore().catch(() => { compensationFailed = true; });
      }
      if (graphjinRestore) await graphjinRestore().catch(() => { compensationFailed = true; });
      const message = (error instanceof Error ? error.message : String(error)).slice(0, 1000);
      if (operationId) {
        await db().update(pack_operation).set({
          status: "failed",
          failure_phase: "uninstall",
          error: message,
          compensation_status: compensationFailed ? "failed" : "succeeded",
          completed_at: new Date(),
          updated_at: new Date(),
        }).where(eq(pack_operation.id, operationId)).catch(() => {});
      }
      await db().update(pack_install).set({
        status: compensationFailed ? "failed" : installation.status,
        operation_id: installation.operation_id,
        last_error: message,
        updated_at: new Date(),
      }).where(eq(pack_install.id, installation.id)).catch(() => {});
      throw error;
    } finally {
      await client.query("SELECT pg_advisory_unlock(hashtextextended($1, 0))", [`pack:${this.orgId}:${packId}`]).catch(() => {});
      client.release();
    }
  }

  private async apply(
    packId: string,
    request: PackInstallRequest,
    operationType: "install" | "configure" | "upgrade",
  ): Promise<PackStatus> {
    const replay = await this.replayIdempotentOperation(packId, request.idempotencyKey);
    if (replay) return replay;
    const firstPartyMagento = packId === "magento";
    const secretSection = packSecretSection(packId);
    const client = await pool().connect();
    let cleanupBundle: (() => Promise<void>) | undefined;
    let priorInstallation: typeof pack_install.$inferSelect | undefined;
    let installationId: string | null = null;
    let operationId: string | null = null;
    let graphjinRestore: (() => Promise<void>) | null = null;
    let filesRestore: (() => Promise<void>) | null = null;
    let skillRestore: (() => Promise<void>) | null = null;
    let skillCommit: (() => Promise<void>) | null = null;
    let retiredRestore: (() => Promise<void>) | null = null;
    let retiredCommit: (() => Promise<void>) | null = null;
    let secretsRestore: (() => Promise<void>) | null = null;
    try {
      await client.query("SELECT pg_advisory_lock(hashtextextended($1, 0))", [`pack:${this.orgId}:${packId}`]);
      const lockedReplay = await this.replayIdempotentOperation(packId, request.idempotencyKey);
      if (lockedReplay) return lockedReplay;
      const [existing] = await db()
        .select()
        .from(pack_install)
        .where(and(eq(pack_install.org_id, this.orgId), eq(pack_install.pack_id, packId)))
        .orderBy(desc(pack_install.created_at))
        .limit(1);
      priorInstallation = existing;
      if (operationType !== "install" && existing?.status !== "installed") {
        throw new Error(`pack ${packId} must be installed before it can be ${operationType}d`);
      }
      if (existing && ["installing", "upgrading", "removing"].includes(existing.status)) {
        throw new Error(`pack ${packId} already has an operation in progress`);
      }

      if (operationType === "configure" && request.version && request.version !== existing?.version) throw new Error("configure cannot change the installed pack version; review an upgrade instead");
      let authoredBundle = operationType === "configure" && existing ? await this.installedBundle(existing) : await this.bundle(packId, request.version);
      if (authoredBundle.upload) {
        const snapshot = await snapshotUploadedPack(this.uploadsRoot(), authoredBundle);
        // Keep normalized binding targets from the validated catalog bundle.
        snapshot.bundle.artifacts.forEach(artifact => { artifact.targetRef = authoredBundle.artifacts.find(value => value.kind === artifact.kind && value.key === artifact.key)!.targetRef; });
        authoredBundle = snapshot.bundle;
        cleanupBundle = snapshot.cleanup;
      }
      if (request.deferConfiguration) {
        if (operationType !== "install" || authoredBundle.upload) throw new Error("Only built-in packs support installation before configuration");
        if (request.version && request.version !== authoredBundle.manifest.metadata.version) throw new Error("The built-in pack version changed; reload the pack list");
        if (existing?.status === "installed") return (await this.status(packId))!;
        if (existing && existing.status !== "removed") throw new Error("Resolve the previous pack operation before installing again");
        await db().transaction(async tx => {
          const [created] = await tx.insert(pack_install).values({
            org_id: this.orgId, pack_id: packId, version: authoredBundle.manifest.metadata.version,
            manifest_hash: authoredBundle.manifestHash, source: "embedded", status: "installed",
            installed_by_user_id: request.actorUserId ?? null, installed_at: new Date(),
            config: { _definitionOnly: true, _userOAuth: authoredBundle.manifest.oauth.filter(connection => connection.scope === "user"), ...packChannels(authoredBundle.manifest) },
          }).returning({ id: pack_install.id });
          await tx.insert(pack_operation).values({
            org_id: this.orgId, pack_install_id: created!.id, operation_type: "install",
            actor_user_id: request.actorUserId ?? null, status: "succeeded", phase: "definition_installed",
            requested_version: authoredBundle.manifest.metadata.version, idempotency_key: request.idempotencyKey ?? randomUUID(),
            plan_hash: authoredBundle.manifestHash, completed_at: new Date(),
          });
        });
        await activatePackChannels(this.orgId, packId, authoredBundle.manifest.channels);
        return (await this.status(packId))!;
      }
      let bundle: SolutionPackBundle = authoredBundle;
      const runtime = await this.runtime(bundle, request, storedRuntime(existing?.config ?? {}));
      if (!firstPartyMagento) {
        const bound = bindPackQueries(bundle, runtime.bindings);
        bundle = bound.bundle;
        // Retain alias ownership across upgrades, like the existing source/table metadata.
        runtime.tables = [...new Map([...(storedRuntime(existing?.config ?? {})?.tables ?? []), ...bound.tables].map(table => [table.name, table])).values()];
      }
      const source = runtime.source;
      const declaredInputs = new Set(bundle.manifest.inputs.map((input) => input.key));
      const priorInputs = Object.fromEntries(
        Object.entries((existing?.config ?? {}) as Record<string, unknown>)
          .filter(([key]) => declaredInputs.has(key)),
      );
      const inputs = resolveInputs(bundle, { ...priorInputs, ...(request.inputs ?? {}) });
      const resolvedSecrets = await resolveSecrets(bundle, request);
      const plan = await this.planBundle(authoredBundle);
      const reviewHash = this.reviewHash(authoredBundle, plan, operationType, request, inputs, resolvedSecrets.values, runtime);
      if ((authoredBundle.upload || request.reviewHash) && request.reviewHash !== reviewHash) throw new Error("pack review is missing or stale; review the exact bundle, configuration, and plan before installing");
      const preflight = firstPartyMagento ? await runMagentoPreflight({
        host: String(inputs["database.host"]),
        port: Number(inputs["database.port"]),
        database: String(inputs["database.name"]),
        username: resolvedSecrets.values["database.analytics_username"]!,
        password: resolvedSecrets.values["database.analytics_password"]!,
        tablePrefix: String(inputs["magento.table_prefix"] ?? ""),
        baseUrl: String(inputs["magento.base_url"]),
        storeCode: String(inputs["magento.store_code"] ?? "all"),
        integrationToken: resolvedSecrets.values["magento.integration_token"] ?? null,
        customersEnabled: Boolean(inputs["magento.customers_enabled"]),
      }) : null;
      if (preflight) {
        inputs["database.type"] = preflight.databaseType;
        inputs["magento.table_prefix"] = preflight.tablePrefix;
        inputs["magento.base_currency"] = inputs["magento.base_currency"] ?? preflight.baseCurrency;
        inputs["magento.timezone"] = inputs["magento.timezone"] ?? preflight.timezone;
      }
      // Validate declarations before writing installation or secret state.
      if (!preflight) declarativeGraphjinUpdate(bundle, inputs, resolvedSecrets.values, [], runtime.bindings);

      const conflicts = plan.entries.filter((entry) => entry.action === "conflict");
      if (conflicts.length > 0) {
        throw new Error(`pack install has ${conflicts.length} drift conflict(s); run pack plan for details`);
      }
      const storedSecrets = resolvedSecrets.store[secretSection] ?? {};
      const secretsChanged = Object.entries(resolvedSecrets.values).some(
        ([key, value]) => storedSecrets[secretEnvKey(key)] !== value,
      ) || [...resolvedSecrets.cleared].some((key) => storedSecrets[secretEnvKey(key)] !== undefined);
      const priorNormalizedInputs = existing ? resolveInputs(bundle, priorInputs) : null;
      const configChanged = !priorNormalizedInputs ||
        canonicalHash(priorNormalizedInputs) !== canonicalHash(inputs) ||
        canonicalHash(storedRuntime(existing?.config ?? {}) ?? null) !== canonicalHash(runtime);
      if (
        existing?.status === "installed" &&
        existing.version === bundle.manifest.metadata.version &&
        existing.manifest_hash === bundle.manifestHash &&
        plan.entries.every((entry) => entry.action === "noop") &&
        !configChanged &&
        !secretsChanged
      ) {
        await this.persistLegacyStateBaselines(existing.id, plan, bundle);
        const current = await this.status(packId);
        if (!current) throw new Error(`pack ${packId} status is missing after no-op plan`);
        return current;
      }
      const priorArtifacts = existing
        ? await db().select({
            id: pack_artifact.id,
            kind: pack_artifact.artifact_kind,
            key: pack_artifact.artifact_key,
            targetRef: pack_artifact.target_ref,
            metadata: pack_artifact.metadata,
          }).from(pack_artifact).where(eq(pack_artifact.pack_install_id, existing.id))
        : [];
      const retireIdentities = new Set(
        plan.entries
          .filter((entry) => entry.action === "retire")
          .map((entry) => `${entry.kind}:${entry.key}`),
      );
      const retiredArtifacts = priorArtifacts.filter((artifact) =>
        retireIdentities.has(`${artifact.kind}:${artifact.key}`),
      );
      await assertNativeTargetsAvailable({ orgId: this.orgId, bundle, plan });
      const ownedFileTargets = new Set(
        priorArtifacts.filter((artifact) => artifact.kind !== "skill").flatMap((artifact) => {
          const target = artifact.metadata?.materializedTarget;
          return typeof target === "string" ? [target] : [];
        }),
      );
      const ownedSkillTargets = new Set(
        priorArtifacts.filter((artifact) => artifact.kind === "skill").flatMap((artifact) => {
          const target = artifact.metadata?.materializedTarget;
          return typeof target === "string" ? [target] : [];
        }),
      );
      const ownedSourceNames = new Set(
        priorArtifacts
          .filter((artifact) => artifact.kind === "source")
          .map((artifact) => artifact.targetRef),
      );
      if (existing) {
        installationId = existing.id;
        await db().update(pack_install).set({
          version: bundle.manifest.metadata.version,
          status: operationType === "upgrade" ? "upgrading" : "installing",
          manifest_hash: bundle.manifestHash,
          source: authoredBundle.upload ? "uploaded" : "embedded",
          config: { ...inputs, _runtime: runtime, ...(authoredBundle.upload ? { _bundle: authoredBundle.upload } : {}) },
          last_error: null,
          removed_at: null,
          updated_at: new Date(),
        }).where(eq(pack_install.id, existing.id));
      } else {
        const [created] = await db().insert(pack_install).values({
          org_id: this.orgId,
          pack_id: packId,
          version: bundle.manifest.metadata.version,
          status: "installing",
          manifest_hash: bundle.manifestHash,
          source: authoredBundle.upload ? "uploaded" : "embedded",
          config: { ...inputs, _runtime: runtime, ...(authoredBundle.upload ? { _bundle: authoredBundle.upload } : {}) },
          installed_by_user_id: request.actorUserId ?? null,
        }).returning({ id: pack_install.id });
        installationId = created!.id;
      }
      const [operation] = await db().insert(pack_operation).values({
        pack_install_id: installationId,
        org_id: this.orgId,
        operation_type: operationType,
        actor_user_id: request.actorUserId ?? null,
        status: "running",
        requested_version: bundle.manifest.metadata.version,
        idempotency_key: request.idempotencyKey ?? randomUUID(),
        plan: { ...plan, inputs, runtime, reviewHash },
        plan_hash: canonicalHash({ ...plan, inputs, runtime, reviewHash }),
        phase: "preflight_complete",
        started_at: new Date(),
      }).returning({ id: pack_operation.id });
      operationId = operation!.id;
      await db().update(pack_install).set({ operation_id: operationId }).where(eq(pack_install.id, installationId));

      const nextPackSecrets = { ...storedSecrets };
      for (const key of resolvedSecrets.cleared) delete nextPackSecrets[secretEnvKey(key)];
      for (const [key, value] of Object.entries(resolvedSecrets.values)) {
        nextPackSecrets[secretEnvKey(key)] = value;
      }
      const nextSecrets = {
        ...resolvedSecrets.store,
        [secretSection]: nextPackSecrets,
      };
      await writeSecretsStore(nextSecrets);
      secretsRestore = () => writeSecretsStore(resolvedSecrets.store);

      if (bundle.manifest.artifacts.graphjin && !source?.graphqlUrl) {
        throw new Error("Choose a GraphJin data connection before installing this pack");
      }
      const graphjin = bundle.manifest.artifacts.graphjin ? await this.graphjinTarget(source!.graphqlUrl) : null;
      if (graphjin?.mode === "api" && preflight) {
        throw new Error("The Magento pack needs the GraphJin whose config folder OpenNeko manages. Connect OpenNeko to that GraphJin, then install the pack again.");
      }
      const configFile = graphjin?.mode === "files" ? graphjin.configFile : "";
      const retiredSavedQueryNames: string[] = [];
      const retiredRemovals: Array<{
        restore: () => Promise<void>;
        commit: () => Promise<void>;
      }> = [];
      const configRoot = dirname(configFile);
      const workspace = retiredArtifacts.some((artifact) => artifact.kind === "skill")
        ? await ensureOrgWorkspace(this.orgId)
        : null;
      try {
        for (const artifact of retiredArtifacts) {
          const target = artifact.metadata?.materializedTarget;
          if (typeof target !== "string") continue;
          if (target.startsWith("graphjin:saved_query:")) {
            retiredSavedQueryNames.push(target.slice("graphjin:saved_query:".length));
          } else if (target.startsWith("graphjin:")) {
            continue;
          } else if (artifact.kind === "spec" || artifact.kind === "saved_query") {
            retiredRemovals.push(
              await stageOwnedRemoval(assertOwnedPath(configRoot, target, "GraphJin pack file")),
            );
          } else if (artifact.kind === "skill" && workspace) {
            retiredRemovals.push(
              await stageOwnedRemoval(assertOwnedPath(workspace.skillsRoot, target, "pack skill")),
            );
          }
        }
      } catch (error) {
        for (const removal of retiredRemovals.reverse()) await removal.restore().catch(() => {});
        throw error;
      }
      retiredRestore = async () => {
        for (const removal of retiredRemovals.reverse()) await removal.restore();
      };
      retiredCommit = async () => {
        for (const removal of retiredRemovals) await removal.commit();
      };
      const desiredSourceNames = new Set(
        bundle.artifacts.filter((artifact) => artifact.kind === "source").map((artifact) => artifact.targetRef),
      );
      const retiredSourceNames = retiredArtifacts
        .filter((artifact) => artifact.kind === "source" && !artifact.metadata?.borrowedSource && !desiredSourceNames.has(artifact.targetRef))
        .map((artifact) => artifact.targetRef);
      await db().update(pack_operation).set({ phase: "graphjin_files", updated_at: new Date() }).where(eq(pack_operation.id, operationId));
      const apiArtifacts = graphjin?.mode === "api" ? await graphjinApiArtifacts({ bundle, values: inputs }) : null;
      const files = apiArtifacts
        ? { targets: apiArtifacts.targets, restore: async () => {} }
        : await installGraphjinFiles({
            bundle,
            configFile,
            values: inputs,
            ownedTargets: ownedFileTargets,
          });
      filesRestore = files.restore;

      await this.runtime(authoredBundle, {}, { ...runtime, tables: storedRuntime(existing?.config ?? {})?.tables });
      const ownedTableNames = new Set((storedRuntime(existing?.config ?? {})?.tables ?? []).map(table => table.name!));
      const applied = graphjin?.mode === "api" ? await applyPackGraphjinConfigLive({
        target: graphjin,
        update: {
          // A GraphJin without auth sees every caller as anonymous, so an
          // "authenticated" source would be unreachable there.
          ...declarativeGraphjinUpdate(bundle, inputs, resolvedSecrets.values, retiredSourceNames, runtime.bindings, apiArtifacts!, graphjin.anonymous ? "public" : "authenticated"),
          tables: (runtime.tables ?? []).map(table => ({ ...table, database: table.source })),
          ...(retiredSavedQueryNames.length ? { remove_saved_queries: retiredSavedQueryNames } : {}),
        },
        ownedSourceNames,
        ownedTableNames,
        ownedSavedQueryNames: new Set([...ownedFileTargets].filter(value => value.startsWith("graphjin:saved_query:")).map(value => value.slice("graphjin:saved_query:".length))),
      }) : bundle.manifest.artifacts.graphjin ? await applyPackGraphjinConfig({
        endpoint: graphjinEndpoint(source!.graphqlUrl),
        orgId: this.orgId,
        configFile,
        update: preflight
          ? magentoGraphjinUpdate(inputs, resolvedSecrets.values, preflight, bundle, retiredSourceNames)
          : { ...declarativeGraphjinUpdate(bundle, inputs, resolvedSecrets.values, retiredSourceNames, runtime.bindings), tables: (runtime.tables ?? []).map(table => ({ ...table, database: table.source })) },
        ownedSourceNames,
        ...(!preflight ? { ownedTableNames } : {}),
      }) : { restore: async () => {} };
      graphjinRestore = applied.restore;
      if (preflight) await runMagentoAnalyticsSmoke(graphjinEndpoint(source!.graphqlUrl), this.orgId);
      else if (source) await runPackReadPreflight(bundle, source!.graphqlUrl, this.orgId, inputs);

      const retiredHashes = new Map<string, string>();
      for (const artifact of retiredArtifacts.filter(
        (value) => value.kind === "source" || value.kind === "relationships",
      )) {
        const current = await inspectInstalledPackArtifactCurrent({
          orgId: this.orgId,
          kind: artifact.kind as "source" | "relationships",
          targetRef: artifact.targetRef,
          metadata: artifact.metadata ?? {},
          graphjin,
        });
        if (current) retiredHashes.set(`${artifact.kind}:${artifact.key}`, current);
      }

      const skills = await installSkills({ orgId: this.orgId, bundle, ownedTargets: ownedSkillTargets });
      skillRestore = skills.restore;
      skillCommit = skills.commit;

      const materializedHashes = new Map<string, string>();
      for (const artifact of bundle.artifacts.filter((value) =>
        value.kind === "source" ||
        value.kind === "relationships" ||
        value.kind === "spec" ||
        value.kind === "saved_query" ||
        value.kind === "skill"
      )) {
        const target = files.targets.get(`${artifact.kind}:${artifact.key}`) ??
          skills.targets.get(`${artifact.kind}:${artifact.key}`) ?? artifact.targetRef;
        const currentHash = await inspectPackArtifactCurrent({
          orgId: this.orgId,
          artifact,
          metadata: { materializedTarget: target, locator: boundPackLocator(bundle, artifact, runtime.bindings), borrowedSource: Boolean(runtime.bindings[artifact.key]) },
          graphjin,
        });
        if (!currentHash) throw new Error(`pack artifact ${artifact.key} was not materialized`);
        materializedHashes.set(`${artifact.kind}:${artifact.key}`, currentHash);
      }

      await this.runtime(authoredBundle, {}, runtime);
      await db().transaction(async (tx) => {
        for (const artifact of retiredArtifacts) {
          const identity = `${artifact.kind}:${artifact.key}`;
          const locatorValue = artifact.metadata?.locator;
          const locator = locatorValue && typeof locatorValue === "object" && !Array.isArray(locatorValue)
            ? locatorValue as Record<string, unknown>
            : {};
          if (artifact.kind === "metric") {
            const role = String(locator.role ?? "");
            if (!role) throw new Error(`pack metric ${artifact.key} has no retirement locator`);
            const [row] = await tx.select().from(metric).where(and(
              eq(metric.org_id, this.orgId),
              eq(metric.role, role),
              eq(metric.slug, String(locator.slug ?? artifact.targetRef)),
            )).limit(1);
            if (!row) throw new Error(`pack metric ${artifact.targetRef} disappeared during upgrade`);
            await tx.update(metric).set({ active: false, updated_at: new Date() }).where(eq(metric.id, row.id));
            retiredHashes.set(identity, nativeArtifactStateHash("metric", { ...row, active: false } as unknown as Record<string, unknown>));
          } else if (artifact.kind === "workflow") {
            const [row] = await tx.select().from(workflow_definition).where(and(
              eq(workflow_definition.org_id, this.orgId),
              eq(workflow_definition.owner_user_id, ""),
              eq(workflow_definition.name, String(locator.name ?? artifact.targetRef)),
            )).limit(1);
            if (!row) throw new Error(`pack workflow ${artifact.targetRef} disappeared during upgrade`);
            await tx.update(workflow_definition).set({
              enabled: false,
              cron_enabled: false,
              updated_at: new Date(),
            }).where(eq(workflow_definition.id, row.id));
            retiredHashes.set(identity, nativeArtifactStateHash("workflow", {
              ...row,
              enabled: false,
              cron_enabled: false,
            } as unknown as Record<string, unknown>));
          } else if (artifact.kind === "watcher") {
            const [row] = await tx.select().from(watcher).where(and(
              eq(watcher.org_id, this.orgId),
              eq(watcher.name, String(locator.name ?? artifact.targetRef)),
            )).limit(1);
            if (!row) throw new Error(`pack watcher ${artifact.targetRef} disappeared during upgrade`);
            await tx.update(watcher).set({ enabled: false, updated_at: new Date() }).where(eq(watcher.id, row.id));
            retiredHashes.set(identity, nativeArtifactStateHash("watcher", { ...row, enabled: false } as unknown as Record<string, unknown>));
          } else if (artifact.kind === "policy") {
            const [row] = await tx.select().from(action_policy).where(and(
              eq(action_policy.org_id, this.orgId),
              eq(action_policy.name, String(locator.name ?? artifact.targetRef)),
            )).limit(1);
            if (!row) throw new Error(`pack policy ${artifact.targetRef} disappeared during upgrade`);
            await tx.update(action_policy).set({ enabled: false, updated_at: new Date() }).where(eq(action_policy.id, row.id));
            retiredHashes.set(identity, nativeArtifactStateHash("policy", { ...row, enabled: false } as unknown as Record<string, unknown>));
          } else if (artifact.kind === "action") {
            const [row] = await tx.select().from(pack_action_definition).where(and(
              eq(pack_action_definition.org_id, this.orgId),
              eq(pack_action_definition.kind, String(locator.kind ?? artifact.targetRef)),
            )).limit(1);
            if (!row) throw new Error(`pack action ${artifact.targetRef} disappeared during upgrade`);
            await tx.update(pack_action_definition).set({
              enabled: false,
              readiness: "blocked",
              readiness_reason: "removed_from_pack",
              updated_at: new Date(),
            }).where(eq(pack_action_definition.id, row.id));
            retiredHashes.set(identity, nativeArtifactStateHash("action", { ...row, enabled: false } as unknown as Record<string, unknown>));
          }

          const retiredHash = retiredHashes.get(identity) ?? canonicalHash({ retired: true, identity });
          await tx.update(pack_artifact).set({
            last_applied_hash: retiredHash,
            ownership: "retired",
            readiness: "not_applicable",
            readiness_reason: null,
            metadata: {
              ...(artifact.metadata ?? {}),
              stateHashVersion: 1,
              retiredMissing: !retiredHashes.has(identity),
              ...(
                artifact.kind === "source" || artifact.kind === "relationships"
                  ? { retainedForGraphjinCompatibility: true }
                  : {}
              ),
            },
            updated_at: new Date(),
          }).where(eq(pack_artifact.id, artifact.id));
        }

        if (preflight) {
          const caps = magentoCapsFromInputs(inputs);
          for (const control of DEFAULT_MAGENTO_DOMAIN_CONTROLS) {
            const enabled = control.domain === "customers"
              ? Boolean(inputs["magento.customers_enabled"])
              : control.enabled;
            await tx.insert(magento_store_control).values({
              org_id: this.orgId,
              domain: control.domain,
              risk_class: control.riskClass,
              enabled,
              auto_execute: false,
              caps,
              scope: { stores: preflight.scopes },
            }).onConflictDoUpdate({
              target: [magento_store_control.org_id, magento_store_control.domain],
              set: {
                enabled,
                caps,
                scope: { stores: preflight.scopes },
                updated_at: new Date(),
              },
            });
          }
          for (const classification of DEFAULT_MAGENTO_ATTRIBUTE_CLASSIFICATIONS) {
            await tx.insert(magento_attribute_classification).values({
              org_id: this.orgId,
              domain: classification.domain,
              entity_type: classification.entityType,
              attribute: classification.attribute,
              risk_class: classification.riskClass,
              category: classification.category,
              rationale: classification.rationale,
              reviewed: true,
              pack_default: true,
            }).onConflictDoUpdate({
              target: [
                magento_attribute_classification.org_id,
                magento_attribute_classification.domain,
                magento_attribute_classification.entity_type,
                magento_attribute_classification.attribute,
              ],
              set: {
                risk_class: classification.riskClass,
                category: classification.category,
                rationale: classification.rationale,
                reviewed: true,
                pack_default: true,
                updated_at: new Date(),
              },
            });
          }

        }

        const workflows = new Map<string, string>();
        for (const artifact of bundle.artifacts.filter((value) => value.kind === "workflow")) {
          const value = artifactRecord(artifact);
          const name = String(value.name);
          const [existingWorkflow] = await tx.select({ id: workflow_definition.id }).from(workflow_definition)
            .where(and(eq(workflow_definition.org_id, this.orgId), eq(workflow_definition.owner_user_id, ""), eq(workflow_definition.name, name))).limit(1);
          const schedule = value.schedule as Record<string, unknown> | null;
          const rowValues = {
            description: String(value.description ?? ""),
            enabled: Boolean(value.enabled),
            status: String(value.status),
            goal: String(value.goal),
            cron: schedule ? String(schedule.cron) : null,
            cron_timezone: String(inputs[String(schedule?.timezoneInput)] ?? schedule?.timezoneInput ?? "UTC"),
            cron_enabled: Boolean(schedule?.enabled),
            network_hosts: Array.isArray(value.networkHosts)
              ? value.networkHosts.map((host) => String(host))
              : [],
            output_contract: value.outputContract as Record<string, unknown>,
            updated_at: new Date(),
          };
          const [row] = existingWorkflow
            ? await tx.update(workflow_definition).set(rowValues).where(eq(workflow_definition.id, existingWorkflow.id)).returning({ id: workflow_definition.id })
            : await tx.insert(workflow_definition).values({ org_id: this.orgId, owner_user_id: "", name, ...rowValues }).returning({ id: workflow_definition.id });
          workflows.set(artifact.key, row!.id);
          materializedHashes.set(
            `${artifact.kind}:${artifact.key}`,
            nativeArtifactStateHash("workflow", { name, ...rowValues }),
          );
        }

        for (const artifact of bundle.artifacts.filter((value) => value.kind === "metric")) {
          const value = artifactRecord(artifact);
          const execution = value.execution as Record<string, unknown>;
          const definition = {
            ...value,
            execution: {
              ...execution,
              document: findSavedQuery(bundle, String(execution.query)),
              ...(!preflight ? { variables: packVariables(execution.variables, inputs) } : {}),
              runtime: preflight ? {
                storeIds: preflight.storeIds,
                windowDays: 30,
                staleAfterSeconds: 2 * 60 * 60,
                agedAfterSeconds: 2 * 24 * 60 * 60,
                stockThreshold: 5,
                currency: String(inputs["magento.base_currency"] ?? "USD"),
                timezone: String(inputs["magento.timezone"] ?? "UTC"),
              } : {},
            },
          };
          const set = {
            source: `pack:${packId}`,
            title: String(value.title),
            description: String(value.description),
            why: String(value.calculationNote),
            chart_hint: String(value.chartHint),
            unit: String(value.unit),
            direction_good: String(value.directionGood),
            cadence: String(value.cadence),
            active: true,
            execution_mode: "saved_query",
            definition_json: definition,
            definition_version: 1,
            definition_hash: canonicalHash(definition),
            updated_at: new Date(),
          };
          await tx.insert(metric).values({
            org_id: this.orgId,
            role: String(value.role),
            slug: String(value.targetRef),
            ...set,
          }).onConflictDoUpdate({
            target: [metric.org_id, metric.role, metric.slug],
            set,
          });
          materializedHashes.set(
            `${artifact.kind}:${artifact.key}`,
            nativeArtifactStateHash("metric", { role: String(value.role), slug: String(value.targetRef), ...set }),
          );
        }

        for (const artifact of bundle.artifacts.filter((value) => value.kind === "watcher")) {
          const value = artifactRecord(artifact);
          const workflowId = workflows.get(String(value.workflow));
          if (!workflowId) throw new Error(`watcher ${artifact.key} references missing workflow ${String(value.workflow)}`);
          const query = findSavedQuery(bundle, String(value.query));
          const set = {
            workflow_id: workflowId,
            description: String(value.description),
            enabled: Boolean(value.enabled),
            query,
            value_path: String(value.valuePath),
            op: String(value.operator),
            threshold: value.threshold,
            cadence_seconds: Number(value.cadenceSeconds),
            debounce_seconds: Number(value.debounceSeconds),
            cooldown_seconds: Number(value.cooldownSeconds),
            dedupe_key: String(value.dedupeKey),
            activation: String(value.activation),
            variables_json: preflight ? {
              from: { kind: "seconds_ago", seconds: 30 * 24 * 60 * 60 },
              to: { kind: "now" },
              staleBefore: { kind: "seconds_ago", seconds: 2 * 60 * 60 },
              olderThan: { kind: "seconds_ago", seconds: 2 * 24 * 60 * 60 },
              storeIds: { kind: "literal", value: preflight.storeIds },
              threshold: { kind: "literal", value: 5 },
            } : packVariables(value.variables, inputs),
            severity: String(value.severity),
            updated_at: new Date(),
          };
          await tx.insert(watcher).values({ org_id: this.orgId, name: String(value.name), ...set })
            .onConflictDoUpdate({ target: [watcher.org_id, watcher.name], set });
          materializedHashes.set(
            `${artifact.kind}:${artifact.key}`,
            nativeArtifactStateHash("watcher", { name: String(value.name), ...set }),
          );
        }

        for (const artifact of bundle.artifacts.filter((value) => value.kind === "policy")) {
          const value = artifactRecord(artifact);
          const [existingPolicy] = await tx.select({ id: action_policy.id, enabled: action_policy.enabled }).from(action_policy)
            .where(and(eq(action_policy.org_id, this.orgId), eq(action_policy.name, String(value.name)))).limit(1);
          const set = {
            description: String(value.description),
            applies_to_kinds: value.appliesToKinds as string[],
            applies_to_scopes: value.appliesToScopes as string[],
            mode: value.mode === "ask"
              ? "approval_required"
              : value.mode === "auto"
                ? "auto_approve"
                : value.mode === "draft"
                  ? "draft_only"
                  : String(value.mode),
            allowed_targets: value.allowedTargets as Record<string, unknown>,
            limits: value.limits as Record<string, unknown>,
            priority: Number(value.priority),
            enabled: installedPackPolicyEnabled({
              declared: Boolean(value.enabled),
              controlsWrite: packPolicyControlsWrite(bundle, value),
              ...(existingPolicy ? { existing: existingPolicy.enabled } : {}),
            }),
            updated_at: new Date(),
          };
          if (existingPolicy) await tx.update(action_policy).set(set).where(eq(action_policy.id, existingPolicy.id));
          else await tx.insert(action_policy).values({ org_id: this.orgId, name: String(value.name), ...set });
          materializedHashes.set(
            `${artifact.kind}:${artifact.key}`,
            nativeArtifactStateHash("policy", { name: String(value.name), ...set }),
          );
        }

        for (const artifact of bundle.artifacts.filter((value) => value.kind === "action")) {
          const value = artifactRecord(artifact);
          const readinessValue = value.readiness as Record<string, unknown> | undefined;
          const domain = String(readinessValue?.domain ?? "") as MagentoDomain;
          const adapter = value.adapter as Record<string, unknown> | undefined;
          const reason = adapter?.kind === "graphjin_api_operation"
            ? "ready"
            : adapter?.kind === "magento_financial_handoff"
            ? "ready"
            : preflight?.operatorDomains[domain] ?? preflight?.operatorReadiness ?? "unsupported_adapter";
          const actionReady = reason === "ready";
          await tx.insert(pack_action_definition).values({
            org_id: this.orgId,
            kind: String(value.kind),
            description: String(value.description),
            definition: value,
            definition_hash: artifact.hash,
            readiness: actionReady ? "ready" : "blocked",
            readiness_reason: actionReady ? null : reason,
            enabled: true,
          }).onConflictDoUpdate({
            target: [pack_action_definition.org_id, pack_action_definition.kind],
            set: {
              description: String(value.description),
              definition: value,
              definition_hash: artifact.hash,
              readiness: actionReady ? "ready" : "blocked",
              readiness_reason: actionReady ? null : reason,
              enabled: true,
              updated_at: new Date(),
            },
          });
          materializedHashes.set(
            `${artifact.kind}:${artifact.key}`,
            nativeArtifactStateHash("action", {
              kind: String(value.kind),
              description: String(value.description),
              definition: value,
              definition_hash: artifact.hash,
              enabled: true,
            }),
          );
        }

        for (const artifact of bundle.artifacts) {
          const operatorArtifact = firstPartyMagento && (
            artifact.key === "source.magento_operator" ||
            artifact.kind === "spec" ||
            artifact.kind === "action" ||
            (artifact.kind === "saved_query" && basename(artifact.path).startsWith("action_")));
          const value = artifact.kind === "action" ? artifactRecord(artifact) : null;
          const actionDomain = String(
            (value?.readiness as Record<string, unknown> | undefined)?.domain ?? "",
          ) as MagentoDomain;
          const actionAdapter = value?.adapter as Record<string, unknown> | undefined;
          const operatorReason = actionAdapter?.kind === "magento_financial_handoff"
            ? "ready"
            : actionDomain
              ? preflight?.operatorDomains[actionDomain]
              : preflight?.operatorReadiness;
          const readiness = operatorArtifact && operatorReason !== "ready" ? "blocked" : "ready";
          const reason = operatorArtifact
            ? `operator:${actionDomain || "operator"}:${operatorReason}`
            : null;
          const target = files.targets.get(`${artifact.kind}:${artifact.key}`) ??
            skills.targets.get(`${artifact.kind}:${artifact.key}`) ?? artifact.targetRef;
          const materializedHash = materializedHashes.get(`${artifact.kind}:${artifact.key}`);
          if (!materializedHash) throw new Error(`pack artifact ${artifact.key} has no materialized state hash`);
          await tx.insert(pack_artifact).values({
            pack_install_id: installationId!,
            org_id: this.orgId,
            artifact_kind: artifact.kind,
            artifact_key: artifact.key,
            target_ref: artifact.targetRef,
            desired_hash: artifact.hash,
            last_applied_hash: materializedHash,
            ownership: "managed",
            readiness,
            readiness_reason: reason,
            metadata: {
              path: artifact.path,
              materializedTarget: target,
              locator: boundPackLocator(bundle, artifact, runtime.bindings), borrowedSource: Boolean(runtime.bindings[artifact.key]),
              stateHashVersion: 1,
            },
          }).onConflictDoUpdate({
            target: [pack_artifact.org_id, pack_artifact.artifact_kind, pack_artifact.target_ref],
            set: {
              pack_install_id: installationId!,
              artifact_key: artifact.key,
              desired_hash: artifact.hash,
              last_applied_hash: materializedHash,
              ownership: "managed",
              readiness,
              readiness_reason: reason,
              metadata: {
                path: artifact.path,
                materializedTarget: target,
                locator: boundPackLocator(bundle, artifact, runtime.bindings), borrowedSource: Boolean(runtime.bindings[artifact.key]),
                stateHashVersion: 1,
              },
              updated_at: new Date(),
            },
          });
        }
        await tx.execute(sql`delete from pack_user_connection where org_id=${this.orgId} and pack_install_id=${installationId!}`);
        await tx.update(pack_install).set({
          status: "installed",
          config: {
            ...inputs,
            _runtime: runtime,
            _userOAuth: bundle.manifest.oauth.filter(connection => connection.scope === "user"),
            ...(bundle.manifest.oauth.length > 0 ? {
              _oauth: Object.fromEntries(bundle.manifest.oauth.flatMap((connection) => {
                const binding = connection.scope === "user" ? null : packOAuthBinding(connection, nextPackSecrets);
                return binding ? [[connection.key, binding]] : [];
              })),
            } : {}),
            ...(authoredBundle.upload ? { _bundle: authoredBundle.upload } : {}),
            ...(preflight ? { magentoVersion: preflight.magentoVersion } : {}),
            ...packChannels(bundle.manifest),
          },
          installed_at: new Date(),
          updated_at: new Date(),
        }).where(eq(pack_install.id, installationId!));
        await tx.update(pack_operation).set({
          status: "succeeded",
          phase: "complete",
          compensation_status: "not_required",
          completed_at: new Date(),
          updated_at: new Date(),
        }).where(eq(pack_operation.id, operationId!));
      });

      await skillCommit?.().catch((error) => {
        console.warn(
          `[packs] installed ${packId}, but could not remove the skill backup: ${error instanceof Error ? error.message : String(error)}`,
        );
      });
      await retiredCommit?.().catch((error) => {
        console.warn(
          `[packs] installed ${packId}, but could not remove a retired-artifact backup: ${error instanceof Error ? error.message : String(error)}`,
        );
      });
      await enqueuePackMetricRefreshes(this.orgId, bundle).catch((error) => {
        console.warn(`[packs] initial metric refresh setup failed: ${error instanceof Error ? error.message : String(error)}`);
      });
      await activatePackChannels(this.orgId, packId, bundle.manifest.channels);
      const status = await this.status(packId);
      if (!status) throw new Error("pack status missing after install");
      return status;
    } catch (error) {
      let compensationFailed = false;
      for (const restore of [skillRestore, graphjinRestore, filesRestore, retiredRestore, secretsRestore]) {
        if (!restore) continue;
        await restore().catch(() => {
          compensationFailed = true;
        });
      }
      const message = (error instanceof Error ? error.message : String(error)).slice(0, 1000);
      if (operationId) {
        await db().update(pack_operation).set({
          status: "failed",
          failure_phase: "apply",
          error: message,
          compensation_status: compensationFailed ? "failed" : "succeeded",
          completed_at: new Date(),
          updated_at: new Date(),
        }).where(eq(pack_operation.id, operationId)).catch(() => {});
      }
      if (installationId) {
        const restorePrior =
          (priorInstallation?.status === "installed" || priorInstallation?.status === "removed") &&
          !compensationFailed;
        await db().update(pack_install).set(restorePrior ? {
          status: priorInstallation!.status,
          version: priorInstallation!.version,
          source: priorInstallation!.source,
          manifest_hash: priorInstallation!.manifest_hash,
          config: priorInstallation!.config,
          operation_id: priorInstallation!.operation_id,
          installed_at: priorInstallation!.installed_at,
          removed_at: priorInstallation!.removed_at,
          last_error: message,
          updated_at: new Date(),
        } : { status: "failed", last_error: message, updated_at: new Date() })
          .where(eq(pack_install.id, installationId)).catch(() => {});
      }
      throw error;
    } finally {
      await client.query("SELECT pg_advisory_unlock(hashtextextended($1, 0))", [`pack:${this.orgId}:${packId}`]).catch(() => {});
      client.release();
      await cleanupBundle?.();
    }
  }
}
