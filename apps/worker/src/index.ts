import { prepareSandboxCapacity, closeSandboxPools } from "@neko/llm/work/sandbox-launcher";
import { withStartupTrace, startupPhase, startupEvent } from "@neko/telemetry/startup";
import { keepProcessThroughDatabaseRestarts, soloAdminNeedsEmail } from "@neko/db";
import { dispatchEmbeddingJobs, resolveAgentBackendId, runEmbeddingIndexJob, type EmbeddingIndexPayload } from "@neko/llm";
import "dotenv/config";

import { randomUUID } from "node:crypto";
import { readFile, realpath, stat } from "node:fs/promises";
import { createServer } from "node:http";
import { join, resolve, sep } from "node:path";
import pg from "pg";
import type PgBoss from "pg-boss";
import {
  boss,
  enqueue,
  QUEUE,
  type ActionExecutePayload,
  type ChannelDeliverPayload,
  type LibraryExtractPayload,
  type LibraryDistillPayload,
  type SkillLearnPayload,
  type ProcessingJobPayload,
  type RecordsIdentityLinkPayload,
  type RecordsImportPayload,
  type RecordsBackupVerifyPayload,
  type RecordsOpsWatchPayload,
  type RecordsWatchEvaluatePayload,
  type RecordsWatchSweepPayload,
  type RecordsSalesforceCutoverPayload,
  type RecordsSalesforceExportPayload,
  type RecordsSalesforceSyncPayload,
  type WorkflowRunFirePayload,
  type WorkRunPayload,
} from "@neko/db/jobs";
import { buildRecordsPoolConfig } from "@neko/db/records-migrate";
import {
  RecordBackfillExecutor,
  RecordsAccessAdmin,
  mintRecordsGraphjinToken,
  normalizeRecordImportSourcePath,
  RecordRegistry,
  RecordImportExecutor,
  RecordOwnerBackfillExecutor,
  recordsGraphjinSigningSecret,
  recordsWatchGraphjinSigningSecret,
  recordsWatchWebhookSecret,
  RecordsGraphjinClient,
  RecordWriteExecutor,
} from "@neko/records";
import {
  createAdminHandler,
  type RecordsImportHandlerSurface,
  type SsoSetupHandlerSurface,
} from "./admin-server.js";
import {
  SsoSetupService,
  startSsoSetupPoller,
} from "./sso/sso-setup-service.js";
import {
  applyGroupGrants,
  disableGroupGrants,
  enableGroupGrants,
  scheduleGroupGrantsApply,
} from "./graphjin/group-grants-service.js";
import {
  createDirectoryUser,
  DIRECTORY_SYNC_CRON,
  directoryStatus,
  runDirectorySync,
} from "./directory/directory-service.js";
import {
  and,
  app_user,
  data_source,
  app_state,
  db,
  desc,
  eq,
  activeAdministratorIds,
  getOrgId,
  guardPoolErrors,
  isNull,
  metric,
  pool,
  processing_job,
} from "@neko/db";
import { loadMetricRefreshSettings } from "@neko/llm/spend";
import {
  agentTurnTimeoutMs,
  cancelAllAgents,
  metricRefreshConcurrency,
  prefetchKnowledgeForOrg,
  provisionHostConfig,
  resolveAgentConcurrency,
  UpstreamProviderError,
  verifyAiCredentials,
} from "@neko/llm";
import {
  getDataSourceForOrg,
  getWorkflowRunChainDepth,
  approveActionRequest,
  createActionRequest,
  dispatchExternalEvent,
  getActionRequest,
  handleSourceChangeMatch,
  handleSubscriptionMatch,
  registerBuiltinAdapters,
  registerFallbackActionAdapterResolver,
  seedDefaultActionPolicies,
  seedPluginActionPolicies,
  syncPluginActionTargetSpecs,
  startSubscriptionManager,
  type DataSourceContext,
  type PluginActionSeed,
} from "@neko/llm/workflows";
import { resolveDeclarativePackActionAdapter, registerPackConnectionPreflight } from "./packs/declarative-action-runtime.js";
import { ensureOrgWorkspace, getOrgAgentRoot, reportDeploymentProfile, setAgentSlotLimit } from "@neko/llm/work";
import { checkOrgUpdates } from "@neko/llm/config-vcs/updates";
import { ensureQueueExists } from "./pg-boss-helpers.js";
import { PluginRegistry } from "./plugins/plugin-registry.js";
import { setPluginRegistryInstance } from "./plugins/registry-instance.js";
import {
  ingestInboundWebhook,
  registerChannelOutputDelivery,
  runChannelDelivery,
} from "./channels/delivery.js";
import { startChannelInbound } from "./channels/inbound-poll.js";
import type PgBossLib from "pg-boss";
import { runBusinessProfileBuild } from "./jobs/business-profile-build.js";
import { runIndustryInsightsBuild } from "./jobs/industry-insights-build.js";
import { runBootstrapMetricsBuild } from "./jobs/bootstrap-metrics-build.js";
import { runMetricRefresh } from "./jobs/metric-refresh.js";
import { metricRefreshIsDue } from "./jobs/metric-schedule.js";
import { runWorkRun } from "./jobs/work-run.js";
import { runWorkflowRunFire } from "./jobs/workflow-run-fire.js";
import { runWorkflowOutputTtlSweep } from "./jobs/workflow-output-ttl-sweep.js";
import { runActionExecute } from "./jobs/action-execute.js";
import { runRecordsImport } from "./jobs/records-import.js";
import { sweepExpiredLibraryTransientState } from "./jobs/library-cleanup.js";
import { runLibraryExtractJob } from "./jobs/library-extract.js";
import { recoverUnqueuedLibraryUploads } from "./jobs/library-recovery.js";
import { runLibraryDistillJob } from "./jobs/library-distill.js";
import { runSkillLearnJob } from "./jobs/skill-learn.js";
import { runRecordsIdentityLink } from "./jobs/records-identity-link.js";
import { runRecordsBackupVerification } from "./jobs/records-backup-verify.js";
import { seedOpenNekoOpsWorkflow } from "./jobs/records-ops-finding.js";
import {
  createRecordsOpsWatchDependencies,
  runRecordsOpsWatch,
} from "./jobs/records-ops-watch.js";
import { runRecordsSalesforceExport } from "./jobs/records-salesforce-export.js";
import { runRecordsSalesforceCutover } from "./jobs/records-salesforce-cutover.js";
import { runRecordsSalesforceSync } from "./jobs/records-salesforce-sync.js";
import {
  defaultSalesforceSyncSweepDependencies,
  runRecordsSalesforceSyncSweep,
} from "./jobs/records-salesforce-sync-sweep.js";
import {
  reconcileStaleProcessingJobs,
  reconcileStaleRuns,
} from "./reconciler.js";
import {
  includeRecordActionDescriptors,
  recordActionRequestSourceWrite,
  registerRecordActionAdapters,
} from "./records/adapters.js";
import { registerRecordSchemaActions } from "./records/schema-adapters.js";
import { registerRecordImportActions } from "./records/import-adapters.js";
import { registerRecordIdentityActions } from "./records/identity-adapters.js";
import { registerRecordSalesforceActions } from "./records/salesforce-adapters.js";
import { registerRecordArtifactImportActions } from "./records/artifact-import-adapters.js";
import { registerRecordBackfillAction } from "./records/backfill-adapters.js";
import { registerRecordAccessActions } from "./records/access-adapters.js";
import { refreshArtifactImportState } from "./records/artifact-import-state.js";
import {
  createRecordsSchemaRuntime,
  resolveRecordsWatchWebhookUrl,
} from "./records/schema-runtime.js";
import { createRecordsStorageMonitor } from "./records/storage-health.js";
import { createRecordsCliImportBridge } from "./records/cli-import.js";
import { cleanupRecordsManagedStaging } from "./records/staging-hygiene.js";
import {
  createRecordsStarterWatchSeeder,
  enqueueRecordsWatchFallbackSweep,
  reconcileRecordsNativeWatchDeliveries,
  receiveRecordsNativeWatchEvent,
} from "./records/starter-watches.js";
import { createRecordsWatchEvaluator } from "./jobs/records-watch-evaluate.js";
import {
  buildRecordsSingleImportReport,
  publishRecordsAppCreationSummary,
  publishRecordsImportReport,
} from "./jobs/records-lifecycle-finding.js";
import {
  initializeWorkerTelemetry,
  shutdownWorkerTelemetry,
} from "./telemetry.js";
import {
  getWorkflowSchedulerHealth,
  startDurableWorkflowScheduler,
} from "./workflow-scheduler.js";
import { startWorkflowApiDispatcher } from "./workflow-api-dispatcher.js";
import { PackService } from "./packs/service.js";
import { registerPackActionPreflight } from "./packs/action-preflight.js";
import { registerMagentoV2Runtime } from "./packs/magento-v2-runtime.js";

process.env.OPENNEKO_SANDBOX_OWNER ||= "worker";
keepProcessThroughDatabaseRestarts("worker");

const PORT: number = 4100;
const MAX_JOB_RETRIES: number = 2;

const RECONCILE_SWEEP_INTERVAL_MS: number = 60_000;
// Must exceed the agent turn budget (+ exec margin), or the sweep cancels
// legitimately long runs as zombies — the run row's updated_at is not
// touched while the turn streams.
const RECONCILE_SWEEP_MIN_AGE_MS: number = Math.max(
  660_000,
  agentTurnTimeoutMs() + 240_000,
);
const SCHEDULED_REFRESH_HOURS: number = 1;

if (initializeWorkerTelemetry()) {
  console.log("[telemetry] OTLP trace export enabled");
}

const ADMIN_ORG_ID = await getOrgId();
const packService = new PackService(ADMIN_ORG_ID);
const recordsPool = guardPoolErrors(new pg.Pool({
  ...buildRecordsPoolConfig(),
  application_name: "openneko-worker-records",
}), "records-db");
const recordsRegistry = new RecordRegistry(recordsPool);
const recordsOpsWatchDependencies = createRecordsOpsWatchDependencies(recordsPool);
const recordsGraphjin = new RecordsGraphjinClient({
  baseUrl:
    process.env.OPENNEKO_RECORDS_GRAPHJIN_URL ?? "http://127.0.0.1:8090",
});
const recordsServiceToken = (orgId: string) =>
  mintRecordsGraphjinToken({
    secret: recordsGraphjinSigningSecret(orgId),
    orgId,
    userId: "records-service",
    role: "service",
  });
const recordsWatchGraphjin = new RecordsGraphjinClient({
  baseUrl:
    process.env.OPENNEKO_RECORDS_WATCH_GRAPHJIN_URL ??
    "http://127.0.0.1:8091",
});
const recordsWatchServiceToken = (orgId: string) =>
  mintRecordsGraphjinToken({
    secret: recordsWatchGraphjinSigningSecret(orgId),
    orgId,
    userId: "records-watch-service",
    role: "service",
  });
const recordsWatchWebhookUrl = resolveRecordsWatchWebhookUrl();
const seedRecordsStarterWatches = createRecordsStarterWatchSeeder({
  graphjin: recordsWatchGraphjin,
  token: recordsWatchServiceToken,
  webhookUrl: recordsWatchWebhookUrl,
});
const evaluateRecordsStarterWatch = createRecordsWatchEvaluator({
  graphjin: recordsWatchGraphjin,
  token: recordsWatchServiceToken,
  registry: recordsRegistry,
});
const recordsStorageMonitor = createRecordsStorageMonitor();

async function readRecordImportSource(orgId: string, sourcePath: string): Promise<Uint8Array> {
  const relativePath = normalizeRecordImportSourcePath(sourcePath);
  const workspace = await ensureOrgWorkspace(orgId);
  const root = await realpath(workspace.orgRoot);
  const file = await realpath(resolve(root, relativePath));
  if (file !== root && !file.startsWith(`${root}${sep}`)) {
    throw new Error("records import source escapes its organization workspace");
  }
  const metadata = await stat(file);
  if (!metadata.isFile()) throw new Error("records import source is not a regular file");
  if (metadata.size > 256 * 1024 * 1024) {
    throw new Error("records import source exceeds 256 MiB");
  }
  return readFile(file);
}

const recordsWriteExecutor = new RecordWriteExecutor({
  pool: recordsPool,
  graphjin: recordsGraphjin,
  serviceToken: recordsServiceToken,
  leaseOwner: `worker:${process.pid}:${randomUUID()}`,
  recordSourceWrite: recordActionRequestSourceWrite,
  assertWritesAllowed: async () => {
    await recordsStorageMonitor.assertInteractiveWritesAllowed();
  },
  appWriteMode: async (orgId, appId) => {
    const rows = await db()
      .select({ config: app_state.config })
      .from(app_state)
      .where(
        and(eq(app_state.org_id, orgId), eq(app_state.app_id, appId)),
      )
      .limit(1);
    const mode = rows[0]?.config?.mode;
    return mode === "mirror" || mode === "cutting_over" || mode === "primary"
      ? mode
      : null;
  },
});
const recordsBackfillExecutor = new RecordBackfillExecutor({
  pool: recordsPool,
  graphjin: recordsGraphjin,
  serviceToken: recordsServiceToken,
  leaseOwner: `worker-backfill:${process.pid}:${randomUUID()}`,
  recordSourceWrite: recordActionRequestSourceWrite,
  assertWritesAllowed: async () => {
    await recordsStorageMonitor.assertInteractiveWritesAllowed();
  },
});
const recordsImportExecutor = new RecordImportExecutor({
  pool: recordsPool,
  graphjin: recordsGraphjin,
  serviceToken: recordsServiceToken,
  leaseOwner: `worker-import:${process.pid}:${randomUUID()}`,
  readSource: readRecordImportSource,
});
const recordsOwnerBackfillExecutor = new RecordOwnerBackfillExecutor({
  pool: recordsPool,
  graphjin: recordsGraphjin,
  serviceToken: recordsServiceToken,
  leaseOwner: `worker-identity:${process.pid}:${randomUUID()}`,
});

// SEC8: state the security posture once at boot; hardened warns when
// the model host is a public cloud API (on-prem LLM is client-provided).
reportDeploymentProfile();

async function markRunning(processingJobId: string) {
  await db()
    .update(processing_job)
    .set({ status: "running", started_at: new Date(), updated_at: new Date() })
    .where(eq(processing_job.id, processingJobId));
}

async function markSucceeded(processingJobId: string) {
  await db()
    .update(processing_job)
    .set({
      status: "succeeded",
      finished_at: new Date(),
      error: null,
      updated_at: new Date(),
    })
    .where(eq(processing_job.id, processingJobId));
}

async function markFailed(processingJobId: string, error: string) {
  await db()
    .update(processing_job)
    .set({
      status: "failed",
      finished_at: new Date(),
      error,
      updated_at: new Date(),
    })
    .where(eq(processing_job.id, processingJobId));
}

function makeHandler<P extends ProcessingJobPayload>(
  fn: (processingJobId: string, orgId: string, payload: P) => Promise<void>,
) {
  return async (jobs: PgBoss.Job<P>[]) => {
    const results = await Promise.allSettled(
      jobs.map(job => withStartupTrace({ jobId: job.data.processingJobId, runId: job.data.processingJobId }, () => startupPhase("job.dispatch", async () => {
        if (Number.isFinite(job.data.queuedAt)) startupEvent("queue.wait", { durationMs: Math.max(0, Date.now() - job.data.queuedAt!), basis: "since_enqueue_including_retries", jobKind: job.name });
        const { processingJobId, orgId } = job.data;
        console.log(
          `[worker] running ${job.name} job=${processingJobId} org=${orgId}`,
        );
        await startupPhase("job.mark_running", () => markRunning(processingJobId));
        try {
          await fn(processingJobId, orgId, job.data);
          await startupPhase("job.mark_succeeded", () => markSucceeded(processingJobId));
          console.log(`[worker] job ${processingJobId} succeeded`);
        } catch (e) {
          const msg = e instanceof Error ? e.message : String(e);
          await startupPhase("job.mark_failed", () => markFailed(processingJobId, msg));
          if (e instanceof UpstreamProviderError) {
            console.warn(
              `[worker] job ${processingJobId} upstream provider unavailable; skipping pg-boss retry: ${msg}`,
            );
            return;
          }
          console.warn(
            `[worker] job ${processingJobId} attempt failed; pg-boss may retry: ${msg}`,
          );
          if (e instanceof Error && e.stack) console.warn(e.stack);
          throw e;
        }
      }))),
    );
    const firstFailure = results.find((r) => r.status === "rejected");
    if (firstFailure && firstFailure.status === "rejected") {
      throw firstFailure.reason;
    }
  };
}

async function runMetricRefreshSweep() {
  const cards = await db()
    .select({
      id: metric.id,
      org_id: metric.org_id,
      cadence: metric.cadence,
      last_refresh_status: metric.last_refresh_status,
      updated_at: metric.updated_at,
    })
    .from(metric)
    .where(eq(metric.active, true));
  const orgRefresh = await loadMetricRefreshSettings(pool());
  const now = new Date();
  const due = cards.filter((card) => metricRefreshIsDue({
    cadence: card.cadence,
    lastRefreshStatus: card.last_refresh_status,
    updatedAt: card.updated_at,
    orgRefresh: orgRefresh.get(card.org_id),
    now,
  }));
  if (due.length === 0) {
    console.log("[worker] scheduled sweep: no metrics are due");
    return;
  }

  let enqueued = 0;
  for (const card of due) {
    const inserted = await db()
      .insert(processing_job)
      .values({
        org_id: card.org_id,
        kind: "metric_refresh",
        status: "queued",
        trigger: "scheduled",
        trigger_payload: { metricId: card.id },
      })
      .returning({ id: processing_job.id });
    const processingJobId = inserted[0]?.id;
    if (!processingJobId) continue;
    await db().update(metric).set({
      last_refresh_status: "pending",
      last_refresh_error: null,
      last_refresh_job_id: processingJobId,
      updated_at: now,
    }).where(eq(metric.id, card.id));
    await enqueue(QUEUE.METRIC_REFRESH, {
      processingJobId,
      orgId: card.org_id,
    });
    enqueued++;
  }
  console.log(`[worker] scheduled sweep: enqueued ${enqueued} metric_refresh job(s)`);
}

// The plugin registry is constructed below before the server starts
// taking requests; we expose its auth surface via a closure so the
// admin handler can lazily reach a freshly-installed auth plugin
// without a server restart.
let pluginRegistry: PluginRegistry | null = null;
let recordsImportAdminSurface: RecordsImportHandlerSurface | null = null;
let ssoSetupService: SsoSetupService | null = null;
let actionRequestPreflightReady = false;
function recordsImportSurface(): RecordsImportHandlerSurface {
  if (!recordsImportAdminSurface) {
    throw new Error("records import CLI bridge is not ready");
  }
  return recordsImportAdminSurface;
}
function ssoSetupSurface(): SsoSetupHandlerSurface {
  return {
    getStatus: (orgId) => {
      if (!ssoSetupService) throw new Error("SSO setup service not ready");
      return ssoSetupService.getStatus(orgId);
    },
    setScalekitIds: (orgId, input) => {
      if (!ssoSetupService) throw new Error("SSO setup service not ready");
      return ssoSetupService.setScalekitIds(orgId, input);
    },
    listEnvironments: (orgId) => {
      if (!ssoSetupService) throw new Error("SSO setup service not ready");
      return ssoSetupService.listEnvironments(orgId);
    },
    listOrganizations: (orgId, environmentId) => {
      if (!ssoSetupService) throw new Error("SSO setup service not ready");
      return ssoSetupService.listOrganizations(orgId, environmentId);
    },
    getEnvironmentCredentials: (orgId, environmentId) => {
      if (!ssoSetupService) throw new Error("SSO setup service not ready");
      return ssoSetupService.getEnvironmentCredentials(orgId, environmentId);
    },
    setSignInCredentials: (orgId, input) => {
      if (!ssoSetupService) throw new Error("SSO setup service not ready");
      return ssoSetupService.setSignInCredentials(orgId, input);
    },
    generatePortalLink: (orgId) => {
      if (!ssoSetupService) throw new Error("SSO setup service not ready");
      return ssoSetupService.generatePortalLink(orgId);
    },
    checkConnection: (orgId) => {
      if (!ssoSetupService) throw new Error("SSO setup service not ready");
      return ssoSetupService.checkConnection(orgId);
    },
  };
}
const server = createServer(
  createAdminHandler({
    scheduler: { getHealth: getWorkflowSchedulerHealth },
    auth: {
      getAuthProvider: () => pluginRegistry?.getAuthProvider() ?? null,
      authSignInReady: () => pluginRegistry?.authSignInReady() ?? false,
      getAuthEnvGaps: () => pluginRegistry?.getAuthEnvGaps() ?? null,
      getAuthConfiguredEnvKeys: () =>
        pluginRegistry?.getAuthConfiguredEnvKeys() ?? [],
      getAuthDeclaredEnvKeys: () =>
        pluginRegistry?.getAuthDeclaredEnvKeys() ?? [],
      soloAdminNeedsEmail: async () => soloAdminNeedsEmail(await getOrgId()),
      hasProvisionedAdmin: async () => (await activeAdministratorIds(await getOrgId())).length > 0,
      setAuthSecret: async (key, value) => {
        if (!pluginRegistry) {
          throw new Error("plugin registry not initialised");
        }
        const provider = pluginRegistry.getAuthProvider();
        if (!provider) throw new Error("no auth plugin installed");
        if (value === null) {
          await pluginRegistry.deletePluginSecret(provider.pluginName, key);
        } else {
          await pluginRegistry.setPluginSecret(provider.pluginName, key, value);
        }
      },
      beginAuth: (params) => {
        if (!pluginRegistry) {
          throw new Error("plugin registry not initialised");
        }
        return pluginRegistry.beginAuth(params);
      },
      completeAuth: (params) => {
        if (!pluginRegistry) {
          throw new Error("plugin registry not initialised");
        }
        return pluginRegistry.completeAuth(params);
      },
    },
    plugins: {
      status: () =>
        pluginRegistry?.status() ?? {
          loaded: [],
          skipped: [],
          flagged: [],
          kinds: [],
          vmsRunning: 0,
          authProvider: null,
          directoryProvider: null,
          channels: [],
        },
      getRegisteredActionDescriptors: () =>
        includeRecordActionDescriptors(
          pluginRegistry?.getRegisteredActionDescriptors() ?? [],
        ),
      settings: () => pluginRegistry?.pluginSettings() ?? [],
      setSettings: async (plugin, values) => {
        if (!pluginRegistry) throw new Error("plugin registry not initialised");
        await pluginRegistry.setPluginSettings(plugin, values);
      },
      catalog: async () => {
        const { createMarketplaceClient, OFFICIAL_MARKETPLACE_URL, pickInstallVersion } = await import("@open-neko/plugin-install");
        const installed = new Set((pluginRegistry?.pluginSettings() ?? []).map((p) => p.name));
        try {
          const marketplace = await createMarketplaceClient().fetch(OFFICIAL_MARKETPLACE_URL);
          return {
            available: (marketplace.plugins ?? [])
              .filter((p) => !installed.has(p.name) && p.versions.some((v) => !v.yanked && !v.draft))
              .map((p) => ({
                name: p.name,
                title: p.title || p.name,
                description: p.description ?? "",
                version: pickInstallVersion(p).version,
              })),
          };
        } catch (err) {
          return { available: [], error: `marketplace unavailable: ${err instanceof Error ? err.message : String(err)}` };
        }
      },
      install: async (name) => {
        const { getInstallPolicyForOrg } = await import("@neko/db");
        const { installFromOfficialMarketplace } = await import("./plugins/manage-adapters.js");
        const result = await installFromOfficialMarketplace({
          spec: name,
          repoRoot: process.cwd(),
          policy: await getInstallPolicyForOrg(ADMIN_ORG_ID),
          deferMissingEnv: true,
        });
        await pluginRegistry?.refresh();
        return { name: result.name, version: result.version, envMissing: result.envMissing };
      },
    },
    actionRequests: {
      create: async (input) => {
        if (!actionRequestPreflightReady) {
          throw new Error("action request preflight is not ready");
        }
        const request = await createActionRequest(
          input as Parameters<typeof createActionRequest>[0],
        );
        return { id: request.id, status: request.status };
      },
    },
    events: {
      dispatchExternal: async (input) => {
        const result = await dispatchExternalEvent({
          orgId: input.orgId,
          event: {
            name: input.name,
            source: input.source,
            payload: input.payload,
            ...(input.dedupeKey ? { dedupeKey: input.dedupeKey } : {}),
          },
        });
        return { matched: result.matched, enqueued: result.enqueued };
      },
    },
    recordsWatches: {
      secret: recordsWatchWebhookSecret(ADMIN_ORG_ID),
      dispatch: receiveRecordsNativeWatchEvent,
    },
    recordsImports: {
      staging: () => recordsImportSurface().staging(),
      prepare: (input) => recordsImportSurface().prepare(input),
      start: (requestId) => recordsImportSurface().start(requestId),
    },
    installPolicy: {
      getInstallPolicy: async () => {
        const { getInstallPolicyForOrg } = await import("@neko/db");
        return getInstallPolicyForOrg(ADMIN_ORG_ID);
      },
    },
    groupGrants: {
      apply: async () => applyGroupGrants(await getOrgId()),
      schedule: () => {
        void getOrgId().then((orgId) => scheduleGroupGrantsApply(orgId));
      },
      enable: async (actorUserId) => enableGroupGrants(await getOrgId(), actorUserId),
      disable: async (actorUserId) => disableGroupGrants(await getOrgId(), actorUserId),
      apiOperations: async () => {
        const configFile = process.env.OPENNEKO_GRAPHJIN_CONFIG?.trim();
        if (!configFile) return [];
        const { readFile } = await import("node:fs/promises");
        const { listConfigApiOperations } = await import("@neko/llm/graphjin");
        return listConfigApiOperations(await readFile(configFile, "utf8"));
      },
    },
    directory: {
      status: async () => directoryStatus(pluginRegistry, await getOrgId()),
      sync: async () => {
        if (!pluginRegistry) throw new Error("plugin registry not initialised");
        return runDirectorySync(pluginRegistry, await getOrgId());
      },
      createUser: async (input) => {
        if (!pluginRegistry) throw new Error("plugin registry not initialised");
        return createDirectoryUser(pluginRegistry, input);
      },
    },
    packs: {
      checkContextUpdates: async () => {
        const orgId = await getOrgId();
        return checkOrgUpdates(orgId, getOrgAgentRoot(orgId));
      },
      upload: (bytes, input) => packService.upload(bytes, input),
      review: (packId, input, operation) => packService.review(packId, input, operation),
      list: () => packService.list(),
      inspect: (packId, version) => packService.inspect(packId, version),
      plan: (packId, version) => packService.plan(packId, version),
      status: (packId) => packService.status(packId),
      doctor: (packId) => packService.doctor(packId),
      install: (packId, input) => packService.install(packId, input),
      configure: (packId, input) => packService.configure(packId, input),
      upgrade: (packId, input) => packService.upgrade(packId, input),
      uninstall: (packId, input) => packService.uninstall(packId, input),
      configureOAuth: (packId, connectionKey, input) => packService.configureOAuth(packId, connectionKey, input),
      oauthStatus: (packId, connectionKey) => packService.oauthStatus(packId, connectionKey),
      beginOAuth: (packId, connectionKey, input) => packService.beginOAuth(packId, connectionKey, input),
      completeOAuth: (packId, connectionKey, input) => packService.completeOAuth(packId, connectionKey, input),
      disconnectOAuth: (packId, connectionKey) => packService.disconnectOAuth(packId, connectionKey),
      magentoStoreManagement: () => packService.magentoStoreManagement(),
      updateMagentoStoreManagement: (input) => packService.updateMagentoStoreManagement(input),
    },
    connect: {
      getConnectProviders: () => pluginRegistry?.getConnectProviders() ?? [],
      getOperatorConnectStatus: (operatorId) =>
        pluginRegistry?.getOperatorConnectStatus(operatorId) ?? [],
      getDeploymentConnectStatus: () =>
        pluginRegistry?.getDeploymentConnectStatus() ?? [],
      beginConnect: (pluginName, params) => {
        if (!pluginRegistry) throw new Error("plugin registry not initialised");
        return pluginRegistry.beginConnect(pluginName, params);
      },
      completeConnect: (pluginName, params) => {
        if (!pluginRegistry) throw new Error("plugin registry not initialised");
        return pluginRegistry.completeConnect(pluginName, params);
      },
      refreshConnect: (pluginName, operatorId) => {
        if (!pluginRegistry) throw new Error("plugin registry not initialised");
        return pluginRegistry.refreshConnect(pluginName, operatorId);
      },
      disconnect: (pluginName, operatorId) => {
        if (!pluginRegistry) throw new Error("plugin registry not initialised");
        return pluginRegistry.disconnect(pluginName, operatorId);
      },
    },
    ssoSetup: ssoSetupSurface(),
    channels: {
      getChannelProviders: () => pluginRegistry?.getChannelProviders() ?? [],
      deliver: (pluginName, recipient, events) => {
        if (!pluginRegistry) throw new Error("plugin registry not initialised");
        return pluginRegistry.deliverOnChannel(pluginName, recipient, events);
      },
      ingestInbound: (pluginName, headers, body) =>
        ingestInboundWebhook(ADMIN_ORG_ID, pluginName, headers, body),
    },
  }),
);

try {
  await verifyAiCredentials();
  console.log("[worker] startup credential check complete");
} catch (e) {
  console.warn(
    "[worker] startup credential check failed; continuing so settings can be configured.",
  );
  console.warn(e instanceof Error ? e.message : e);
}

await provisionHostConfig(ADMIN_ORG_ID);
console.log(
  `[worker] host config provisioned from DB (data_source + llm_provider_config)`,
);

const recordsSchemaRuntime = await createRecordsSchemaRuntime({
  pool: recordsPool,
  orgId: ADMIN_ORG_ID,
  enableWatchPlane: true,
  watchWebhookAllow: recordsWatchWebhookUrl
    ? [recordsWatchWebhookUrl]
    : [],
});
if (!recordsWatchWebhookUrl) {
  console.warn(
    "[worker] records native-watch webhook unavailable; scheduled fallback remains active",
  );
}
console.log(
  `[worker] records schema runtime ready (reconciled=${recordsSchemaRuntime.reconciliation.projected.length}, failed=${recordsSchemaRuntime.reconciliation.failed.length})`,
);
// records-watch-graphjin deliberately exits until its generated config
// exists and can still be mid-restart during an upgrade. This reconcile is
// idempotent and only matters for installs with active bindings, so it must
// never kill boot: an unguarded throw here crash-looped the whole worker
// (with Docker's growing restart backoff) whenever the watch plane was a
// few seconds behind. Failures retry in the background instead.
const reconcileWatchDeliveries = async (): Promise<void> => {
  const rebound = await reconcileRecordsNativeWatchDeliveries({
    graphjin: recordsWatchGraphjin,
    token: recordsWatchServiceToken,
    webhookUrl: recordsWatchWebhookUrl,
  });
  if (rebound > 0) {
    console.log(
      `[worker] rebound ${rebound} durable records watch delivery target(s)`,
    );
  }
};
try {
  await reconcileWatchDeliveries();
} catch (e) {
  console.warn(
    `[worker] records watch delivery reconcile failed at boot; retrying in background: ${e instanceof Error ? e.message : e}`,
  );
  void (async () => {
    for (let attempt = 1; attempt <= 5; attempt++) {
      await new Promise((resolve) => setTimeout(resolve, attempt * 5_000));
      try {
        await reconcileWatchDeliveries();
        console.log("[worker] records watch delivery reconcile recovered");
        return;
      } catch {
        // next attempt
      }
    }
    console.warn(
      "[worker] records watch delivery reconcile did not recover; existing deliveries keep their previous targets until the next restart",
    );
  })();
}

await seedDefaultActionPolicies(ADMIN_ORG_ID);
registerBuiltinAdapters();
registerFallbackActionAdapterResolver(resolveDeclarativePackActionAdapter);
registerRecordActionAdapters(recordsWriteExecutor);
registerRecordAccessActions(new RecordsAccessAdmin(recordsPool));
registerRecordBackfillAction(recordsBackfillExecutor);
registerRecordIdentityActions(recordsOwnerBackfillExecutor);
const unregisterRecordSalesforcePreflight = registerRecordSalesforceActions({
  enqueueExport: (payload) =>
    enqueue(QUEUE.RECORDS_SALESFORCE_EXPORT, payload, {
      retryLimit: 8,
      retryDelay: 60,
      retryBackoff: true,
      singletonKey: `records-salesforce-export:${payload.exportJobId}`,
    }),
  enqueueSync: (payload) =>
    enqueue(QUEUE.RECORDS_SALESFORCE_SYNC, payload, {
      retryLimit: 8,
      retryDelay: 60,
      retryBackoff: true,
      singletonKey: `records-salesforce-sync:${payload.processingJobId}`,
    }),
  enqueueCutover: (payload) =>
    enqueue(QUEUE.RECORDS_SALESFORCE_CUTOVER, payload, {
      retryLimit: 8,
      retryDelay: 60,
      retryBackoff: true,
      singletonKey: `records-salesforce-cutover:${payload.processingJobId}`,
    }),
});
const unregisterRecordSchemaPreflight = registerRecordSchemaActions({
  planner: recordsSchemaRuntime.planner,
  saga: recordsSchemaRuntime.saga,
});
const unregisterRecordImportPreflight = registerRecordImportActions({
  pool: recordsPool,
  readSource: readRecordImportSource,
  enqueueImport: (payload) =>
    enqueue(QUEUE.RECORDS_IMPORT, payload, {
      retryLimit: 5,
      retryDelay: 15,
      singletonKey: `records-import:${payload.importRunId}`,
    }),
});
const unregisterRecordArtifactImportPreflight = registerRecordArtifactImportActions({
  pool: recordsPool,
  planner: recordsSchemaRuntime.planner,
  saga: recordsSchemaRuntime.saga,
  readSource: readRecordImportSource,
  enqueueImport: (payload) =>
    enqueue(QUEUE.RECORDS_IMPORT, payload, {
      retryLimit: 5,
      retryDelay: 15,
      singletonKey: `records-import:${payload.importRunId}`,
    }),
});
const unregisterPackActionPreflight = registerPackActionPreflight();
const unregisterPackConnectionPreflight = registerPackConnectionPreflight();
const unregisterMagentoV2Runtime = registerMagentoV2Runtime();
// Only now may the web process create action requests through this worker:
// every worker-owned preflight hook is registered and will finish before the
// request id is returned for an approval card.
actionRequestPreflightReady = true;
recordsImportAdminSurface = createRecordsCliImportBridge({
  orgId: ADMIN_ORG_ID,
  workspaceForOrg: ensureOrgWorkspace,
  registry: recordsRegistry,
  createRequest: createActionRequest,
  getRequest: getActionRequest,
  approveRequest: approveActionRequest,
  enqueueAction: (payload) => enqueue(QUEUE.ACTION_EXECUTE, payload),
});
// ADM3: execute approved chat-proposed plugin installs/uninstalls.
{
  const {
    registerChannelAdminAdapter,
    registerDataSourceAdminAdapter,
    registerSourceConfigAdminAdapter,
    registerPluginManagementAdapters,
    registerUserAdminAdapter,
    registerGroupAdminAdapter,
  } = await import("./plugins/manage-adapters.js");
  registerUserAdminAdapter();
  registerGroupAdminAdapter((orgId) => scheduleGroupGrantsApply(orgId));
  registerChannelAdminAdapter();
  registerDataSourceAdminAdapter();
  registerSourceConfigAdminAdapter();
  registerPluginManagementAdapters({
    repoRoot: process.cwd(),
    getInstallPolicy: async () => {
      const { getInstallPolicyForOrg } = await import("@neko/db");
      return getInstallPolicyForOrg(ADMIN_ORG_ID);
    },
  });
}
console.log("[worker] action policies seeded and built-in adapters registered");

// SEC3: pick the secret residency from local config — Infisical-backed
// env bags when configured, the enc:v1 local file otherwise.
const secretsResolver = await (async () => {
  const { readLocalConfig } = await import("@neko/db");
  const cfg = readLocalConfig().secrets;
  if (cfg?.backend === "infisical" && cfg.infisical) {
    const { InfisicalSecretsResolver } = await import(
      "@open-neko/plugin-install"
    );
    console.log(
      `[worker] secrets backend: infisical (${cfg.infisical.siteUrl}, project ${cfg.infisical.projectId})`,
    );
    return new InfisicalSecretsResolver(cfg.infisical);
  }
  return undefined; // registry defaults to the file resolver
})();

pluginRegistry = new PluginRegistry({
  repoRoot: process.cwd(),
  pluginInstallDir: process.env.OPENNEKO_PLUGIN_INSTALL_DIR || undefined,
  workRoot: `${process.env.HOME ?? "/tmp"}/.openneko/plugins`,
  ...(secretsResolver ? { secretsResolver } : {}),
  loadInstallPolicy: async () => {
    const { getInstallPolicyForOrg } = await import("@neko/db");
    return getInstallPolicyForOrg(ADMIN_ORG_ID);
  },
  onManifestRefresh: async (entries) => {
    const seeds: PluginActionSeed[] = [];
    for (const entry of entries) {
      for (const decl of entry.capabilities.action?.kinds ?? []) {
        seeds.push({
          pluginName: entry.name,
          kind: decl.kind,
          description: decl.description,
          default_mode: decl.default_mode,
        });
      }
    }
    const { created, skipped } = await seedPluginActionPolicies(
      ADMIN_ORG_ID,
      seeds,
    );
    await syncPluginActionTargetSpecs(
      ADMIN_ORG_ID,
      entries.flatMap((entry) =>
        (entry.capabilities.action?.kinds ?? []).flatMap((decl) =>
          decl.targets ? [{ pluginName: entry.name, kind: decl.kind, spec: decl.targets }] : [],
        ),
      ),
    );
    if (created > 0) {
      console.log(
        `[worker] seeded ${created} plugin action_policy row(s) (${skipped} already present or non-auto)`,
      );
    }
  },
});
await pluginRegistry.start();
setPluginRegistryInstance(pluginRegistry);
ssoSetupService = new SsoSetupService(() => pluginRegistry);
startSsoSetupPoller({ service: ssoSetupService, orgId: ADMIN_ORG_ID });
registerChannelOutputDelivery();
await seedOpenNekoOpsWorkflow(ADMIN_ORG_ID);
{
  const s = pluginRegistry.status();
  if (s.loaded.length > 0) {
    console.log(
      `[worker] plugin registry: ${s.loaded.length} plugin(s), ${s.kinds.length} action kind(s) registered (VMs lazy-spawn on first use)`,
    );
  } else {
    console.log(`[worker] plugin registry: no plugins installed`);
  }
  if (s.authProvider) {
    console.log(`[worker] auth provider plugin active: ${s.authProvider}`);
  }
  for (const skipped of s.skipped) {
    console.warn(`[worker] plugin skipped ${skipped.name}: ${skipped.reason}`);
  }
}

{
  const sources = await db()
    .select({ mcp_url: data_source.mcp_url })
    .from(data_source)
    .where(eq(data_source.org_id, ADMIN_ORG_ID))
    .orderBy(desc(data_source.is_default), data_source.created_at)
    .limit(1);
  const mcpUrl = sources[0]?.mcp_url;
  if (mcpUrl) {
    const workspace = await ensureOrgWorkspace(ADMIN_ORG_ID);
    const refresh = await prefetchKnowledgeForOrg(
      ADMIN_ORG_ID,
      workspace.knowledgeRoot,
    );
    if (refresh.ok) {
      const totalBytes = refresh.files.reduce((n, f) => n + f.bytes, 0);
      console.log(
        `[worker] knowledge pack prefetched at ${workspace.knowledgeRoot} (${refresh.files.length} files, ${totalBytes}B)`,
      );
    } else {
      console.warn(
        `[worker] knowledge pack prefetch failed (${refresh.error}); agents will refresh lazily on first run`,
      );
    }
  } else {
    console.warn(
      "[worker] no data_source.mcp_url configured; skipping knowledge pack prefetch",
    );
  }
}

// Web and worker share the agent-home volume. Only this sweep checks revisions;
// warm turns read the last complete snapshot without waiting on GraphJin.
let knowledgeRefreshRunning = false;
const refreshKnowledgeCaches = async () => {
  if (knowledgeRefreshRunning) return;
  knowledgeRefreshRunning = true;
  try {
    const sources = await db().selectDistinct({ orgId: data_source.org_id })
      .from(data_source).where(eq(data_source.auth_mode, "jwt"));
    for (const { orgId } of sources) {
      const result = await withStartupTrace({ requestId: `knowledge-refresh:${orgId}:${Date.now()}` }, () => startupPhase("knowledge.background_refresh", () => prefetchKnowledgeForOrg(orgId, join(getOrgAgentRoot(orgId), "knowledge"), { refresh: true })));
      if (!result.ok) console.warn(`[worker] knowledge cache refresh failed for ${orgId}: ${result.error}`);
    }
  } catch (error) {
    console.warn("[worker] knowledge cache sweep failed:", error);
  } finally { knowledgeRefreshRunning = false; }
};
const knowledgeRefreshTimer = setInterval(() => { void refreshKnowledgeCaches(); }, 60_000);
knowledgeRefreshTimer.unref();

const concurrency = await resolveAgentConcurrency(ADMIN_ORG_ID);
// Every agent job on this host, from any queue, shares one cap.
setAgentSlotLimit(concurrency.globalCap);
console.log(
  `[worker] concurrency: globalCap=${concurrency.globalCap} (configure in /admin/settings/agent; restart required)`,
);

// GraphJin URL — the worker is a client of neko-graphjin, the OpenNeko
// metadata GraphJin service. Default targets localhost so `pnpm dev`
// works against a compose-up'd neko-graphjin (port 8089 exposed). The
// containerized deploy sets OPENNEKO_GRAPHJIN_URL=http://neko-graphjin:8089
// in compose.yml so service-DNS lookup wins there. Customer-data
// graphjin (used by the agent CLI path) is a separate service on
// port 8080.
// Normalize any GraphJin base (origin or full) to the full GraphQL endpoint.
// The subscription/query clients treat baseUrl as the complete
// `/api/v1/graphql` URL and never append a path, so both the env-configured
// neko-graphjin URL (often just an origin) and the data_source URLs (already
// full) must land on the same shape.
function toGraphqlEndpoint(url: string): string {
  const trimmed = url.replace(/\/+$/, "");
  return trimmed.endsWith("/api/v1/graphql")
    ? trimmed
    : `${trimmed}/api/v1/graphql`;
}

const GRAPHJIN_URL = toGraphqlEndpoint(
  process.env.OPENNEKO_GRAPHJIN_URL ?? "http://127.0.0.1:8089",
);
console.log(`[worker] neko graphjin client targeting ${GRAPHJIN_URL}`);

if (process.env.OPENNEKO_AGENT_IMAGE) {
  try { await prepareSandboxCapacity(undefined, await ensureOrgWorkspace(ADMIN_ORG_ID), await resolveAgentBackendId(ADMIN_ORG_ID)); }
  catch (error) { console.error("[sandbox] startup preparation failed", error); }
}
const b = await boss();

{
  const summary = await reconcileStaleProcessingJobs();
  if (
    summary.succeeded + summary.failed + summary.requeued + summary.lost >
    0
  ) {
    console.log(
      `[worker] reconciled processing_job rows on boot: succeeded=${summary.succeeded} failed=${summary.failed} requeued=${summary.requeued} lost=${summary.lost}`,
    );
  }
  // At boot nothing this worker tracks is running yet, so any "running" run
  // it owns is a zombie left by a hard restart. Web chat runs belong to the
  // web process; the periodic sweep covers them if that process dies.
  const runs = await reconcileStaleRuns({ workerOwnedOnly: true });
  if (runs.cancelled > 0) {
    console.log(
      `[worker] cancelled ${runs.cancelled} stale running run(s) on boot`,
    );
  }
}

for (const name of Object.values(QUEUE)) {
  await ensureQueueExists((qName, opts) => b.createQueue(qName, opts), name);
}

await b.work(
  QUEUE.BUSINESS_PROFILE_BUILD,
  makeHandler<ProcessingJobPayload>(async (jobId, orgId) => {
    await runBusinessProfileBuild(jobId, orgId);
  }),
);

await b.work(
  QUEUE.INDUSTRY_INSIGHTS_BUILD,
  makeHandler<ProcessingJobPayload>(async (jobId, orgId) => {
    await runIndustryInsightsBuild(jobId, orgId);
  }),
);

await b.work(
  QUEUE.BOOTSTRAP_METRICS_BUILD,
  makeHandler<ProcessingJobPayload>(async (jobId, orgId) => {
    await runBootstrapMetricsBuild(jobId, orgId);
  }),
);

const workRunHandler = makeHandler<WorkRunPayload>(
  async (jobId, orgId, payload) => {
    await runWorkRun(jobId, orgId, {
      runId: payload.runId,
      threadId: payload.threadId,
      message: payload.message,
      channel: payload.channel,
      channelPlugin: payload.channelPlugin,
      recipient: payload.recipient,
    });
  },
);
for (let i = 0; i < concurrency.globalCap; i++) {
  await b.work(
    QUEUE.WORK_RUN,
    { batchSize: 1, pollingIntervalSeconds: 0.5 },
    workRunHandler,
  );
}

const metricRefreshHandler = makeHandler<ProcessingJobPayload>(
  async (jobId, orgId) => {
    await runMetricRefresh(jobId, orgId);
  },
);
const metricRefreshCap = metricRefreshConcurrency(
  concurrency.globalCap,
  process.env.OPENNEKO_METRIC_REFRESH_CONCURRENCY,
);
console.log(`[worker] concurrency: metric refresh=${metricRefreshCap}`);
for (let i = 0; i < metricRefreshCap; i++) {
  await b.work(
    QUEUE.METRIC_REFRESH,
    { batchSize: 1, pollingIntervalSeconds: 0.5 },
    metricRefreshHandler,
  );
}

await b.work(QUEUE.METRIC_REFRESH_SCHEDULED_SWEEP, async () => {
  await runMetricRefreshSweep();
  // SEC6: archive expired memories + stale pending proposals first so
  // the checkpoint below snapshots the post-TTL state.
  try {
    const { sweepExpiredWorkMemories } = await import("@neko/llm/work");
    const swept = await sweepExpiredWorkMemories();
    if (swept.archived || swept.expiredPending) {
      console.log(
        `[worker] memory TTL sweep: archived ${swept.archived}, expired ${swept.expiredPending} pending`,
      );
    }
  } catch (e) {
    console.warn(
      `[worker] memory TTL sweep failed: ${e instanceof Error ? e.message : e}`,
    );
  }
  // Library staleness: stable concepts past stale_after become
  // deprecated, and affected orgs get their team bundle re-materialized.
  try {
    const { sweepStaleLibraryConcepts } = await import("@neko/llm/work");
    const { materializeTeamLibrary } = await import("@neko/llm");
    const swept = await sweepStaleLibraryConcepts();
    if (swept.deprecated > 0) {
      console.log(`[worker] library staleness sweep: deprecated ${swept.deprecated}`);
      for (const orgId of swept.orgIds) {
        await materializeTeamLibrary(orgId);
      }
    }
  } catch (e) {
    console.warn(
      `[worker] library staleness sweep failed: ${e instanceof Error ? e.message : e}`,
    );
  }
  // CV0: nightly memory checkpoint into the org config repo; CV4 also
  // checkpoints each member's personal layer onto their user/<id> ref.
  try {
    const { getOrgAgentRoot } = await import("@neko/llm/work");
    const { snapshotDurableMemories, snapshotUserConfigsForOrg } =
      await import("@neko/llm/config-vcs");
    await snapshotDurableMemories(ADMIN_ORG_ID, getOrgAgentRoot(ADMIN_ORG_ID));
    await snapshotUserConfigsForOrg(ADMIN_ORG_ID, getOrgAgentRoot(ADMIN_ORG_ID));
  } catch (e) {
    console.warn(
      `[worker] memory checkpoint failed: ${e instanceof Error ? e.message : e}`,
    );
  }
});

await b.work(QUEUE.WORKFLOW_CRON_SWEEP, async () => {
  // SEC7: behavioral thresholds ride the minute tick so a runaway agent
  // alerts within its window, not at the nightly sweep.
  try {
    const { profileBehaviorThresholds, runBehaviorSweep } = await import(
      "@neko/llm/work"
    );
    await runBehaviorSweep(ADMIN_ORG_ID, profileBehaviorThresholds());
  } catch (e) {
    console.warn(
      `[worker] behavior sweep failed: ${e instanceof Error ? e.message : e}`,
    );
  }
  // OL4: condition watchers poll on the same tick (each watcher's own
  // cadence gates how often its query actually runs).
  try {
    const { sweepWatchers } = await import("@neko/llm/workflows");
    await sweepWatchers(ADMIN_ORG_ID);
  } catch (e) {
    console.warn(
      `[worker] watcher sweep failed: ${e instanceof Error ? e.message : e}`,
    );
  }
});

await b.work(
  QUEUE.WORKFLOW_RUN_FIRE,
  { batchSize: 1, pollingIntervalSeconds: 0.5 },
  async (jobs: PgBossLib.Job<WorkflowRunFirePayload>[]) => {
    for (const job of jobs) {
      try {
        await runWorkflowRunFire(job.data);
      } catch (e) {
        console.warn(
          `[workflow-run-fire] job ${job.id} failed; pg-boss may retry: ${e instanceof Error ? e.message : e}`,
        );
        throw e;
      }
    }
  },
);

await b.work(
  QUEUE.ACTION_EXECUTE,
  { batchSize: 1, pollingIntervalSeconds: 0.5 },
  async (jobs: PgBossLib.Job<ActionExecutePayload>[]) => {
    for (const job of jobs) {
      try {
        await runActionExecute(job.data, {
          onAppCreated: async (created) => {
            const starterWatches = await seedRecordsStarterWatches(created);
            await publishRecordsAppCreationSummary({
              ...created,
              seededWatchKeys: starterWatches.seeded,
            });
          },
        });
      } catch (e) {
        console.warn(
          `[action-execute] job ${job.id} failed; pg-boss may retry: ${e instanceof Error ? e.message : e}`,
        );
        throw e;
      }
    }
  },
);

await b.work(
  QUEUE.RECORDS_IMPORT,
  { batchSize: 1, pollingIntervalSeconds: 0.5 },
  async (jobs: PgBossLib.Job<RecordsImportPayload>[]) => {
    for (const job of jobs) {
      try {
        const outcome = await runRecordsImport(recordsImportExecutor, recordsPool, job.data, {
          leaseOwner: `records-import-job:${job.id}`,
        });
        const artifact = await refreshArtifactImportState(recordsPool, {
          orgId: job.data.orgId,
          importRunId: job.data.importRunId,
          graphjin: recordsGraphjin,
          serviceToken: recordsServiceToken(job.data.orgId),
          onReportReady: publishRecordsImportReport,
        });
        if (!artifact.matched) {
          let cleanup: Record<string, unknown> | undefined;
          if (outcome.report?.status === "succeeded") {
            try {
              cleanup = await cleanupRecordsManagedStaging({
                orgId: job.data.orgId,
                sourcePath: outcome.sourcePath,
              });
            } catch (error) {
              cleanup = {
                status: "failed",
                completed_at: new Date().toISOString(),
                error: (error instanceof Error ? error.message : String(error)).slice(
                  0,
                  2_000,
                ),
              };
            }
          }
          await publishRecordsImportReport({
            orgId: job.data.orgId,
            report: buildRecordsSingleImportReport({
              appId: outcome.appId,
              importRunId: job.data.importRunId,
              report: outcome.report,
              terminalError: outcome.terminalError,
              ...(cleanup ? { cleanup } : {}),
            }),
          });
        }
      } catch (e) {
        console.warn(
          `[records-import] job ${job.id} failed; pg-boss may retry: ${e instanceof Error ? e.message : e}`,
        );
        throw e;
      }
    }
  },
);

await b.work(QUEUE.EMBEDDING_INDEX, { batchSize: 1, pollingIntervalSeconds: 0.5 },
  async (jobs: PgBossLib.Job<EmbeddingIndexPayload>[]) => {
    for (const job of jobs) await runEmbeddingIndexJob(job.data);
  });
await b.work(QUEUE.EMBEDDING_SWEEP, { batchSize: 1, pollingIntervalSeconds: 1 },
  async () => { await dispatchEmbeddingJobs(); });
await b.schedule(QUEUE.EMBEDDING_SWEEP, '* * * * *', {}, { retryLimit: 3, retryDelay: 15 });
await b.send(QUEUE.EMBEDDING_SWEEP, {}, { singletonKey: 'boot', singletonSeconds: 60 });

await b.work(
  QUEUE.LIBRARY_EXTRACT,
  { batchSize: 1, pollingIntervalSeconds: 1 },
  async (jobs: PgBossLib.Job<LibraryExtractPayload>[]) => {
    for (const job of jobs) {
      await runLibraryExtractJob(job.data);
    }
  },
);

await b.work(
  QUEUE.LIBRARY_DISTILL,
  { batchSize: 1, pollingIntervalSeconds: 1 },
  async (jobs: PgBossLib.Job<LibraryDistillPayload>[]) => {
    for (const job of jobs) {
      try {
        await runLibraryDistillJob(job.data);
      } catch (e) {
        console.warn(
          `[library-distill] job ${job.id} failed; pg-boss may retry: ${e instanceof Error ? e.message : e}`,
        );
        throw e;
      }
    }
  },
);

await b.work(
  QUEUE.SKILL_LEARN,
  { batchSize: 1, pollingIntervalSeconds: 2 },
  async (jobs: PgBossLib.Job<SkillLearnPayload>[]) => {
    for (const job of jobs) {
      try {
        await runSkillLearnJob(job.data);
      } catch (e) {
        console.warn(
          `[skill-learn] job ${job.id} failed; pg-boss may retry: ${e instanceof Error ? e.message : e}`,
        );
        throw e;
      }
    }
  },
);

await b.work(
  QUEUE.RECORDS_IDENTITY_LINK,
  { batchSize: 1, pollingIntervalSeconds: 0.5 },
  async (jobs: PgBossLib.Job<RecordsIdentityLinkPayload>[]) => {
    for (const job of jobs) {
      try {
        await runRecordsIdentityLink(recordsPool, job.data);
      } catch (e) {
        console.warn(
          `[records-identity-link] job ${job.id} failed; pg-boss may retry: ${e instanceof Error ? e.message : e}`,
        );
        throw e;
      }
    }
  },
);

await b.work(
  QUEUE.RECORDS_BACKUP_VERIFY,
  { batchSize: 1, pollingIntervalSeconds: 1 },
  async (jobs: PgBossLib.Job<RecordsBackupVerifyPayload>[]) => {
    for (const job of jobs) {
      await runRecordsBackupVerification(job.data.orgId);
    }
  },
);

await b.work(
  QUEUE.RECORDS_OPS_WATCH,
  { batchSize: 1, pollingIntervalSeconds: 1 },
  async (jobs: PgBossLib.Job<RecordsOpsWatchPayload>[]) => {
    for (const job of jobs) {
      await runRecordsOpsWatch(job.data.orgId, recordsOpsWatchDependencies);
    }
  },
);

await b.work(
  QUEUE.RECORDS_WATCH_EVALUATE,
  { batchSize: 1, pollingIntervalSeconds: 0.5 },
  async (jobs: PgBossLib.Job<RecordsWatchEvaluatePayload>[]) => {
    for (const job of jobs) {
      await evaluateRecordsStarterWatch(job.data);
    }
  },
);

await b.work(
  QUEUE.RECORDS_WATCH_SWEEP,
  { batchSize: 1, pollingIntervalSeconds: 1 },
  async (jobs: PgBossLib.Job<RecordsWatchSweepPayload>[]) => {
    for (const job of jobs) {
      await enqueueRecordsWatchFallbackSweep(job.data.orgId);
    }
  },
);

await b.work(
  QUEUE.RECORDS_SALESFORCE_EXPORT,
  { batchSize: 1, pollingIntervalSeconds: 0.5 },
  async (jobs: PgBossLib.Job<RecordsSalesforceExportPayload>[]) => {
    for (const job of jobs) {
      try {
        await runRecordsSalesforceExport(job.data);
      } catch (e) {
        console.warn(
          `[records-salesforce-export] job ${job.id} failed; pg-boss may retry: ${e instanceof Error ? e.message : e}`,
        );
        throw e;
      }
    }
  },
);

await b.work(
  QUEUE.RECORDS_SALESFORCE_SYNC,
  { batchSize: 1, pollingIntervalSeconds: 0.5 },
  async (jobs: PgBossLib.Job<RecordsSalesforceSyncPayload>[]) => {
    for (const job of jobs) {
      try {
        await runRecordsSalesforceSync(recordsWriteExecutor, recordsPool, job.data);
      } catch (e) {
        console.warn(
          `[records-salesforce-sync] job ${job.id} failed; pg-boss may retry: ${e instanceof Error ? e.message : e}`,
        );
        throw e;
      }
    }
  },
);

await b.work(
  QUEUE.RECORDS_SALESFORCE_CUTOVER,
  { batchSize: 1, pollingIntervalSeconds: 0.5 },
  async (jobs: PgBossLib.Job<RecordsSalesforceCutoverPayload>[]) => {
    for (const job of jobs) {
      try {
        await runRecordsSalesforceCutover(recordsWriteExecutor, recordsPool, job.data);
      } catch (e) {
        console.warn(
          `[records-salesforce-cutover] job ${job.id} failed; pg-boss may retry: ${e instanceof Error ? e.message : e}`,
        );
        throw e;
      }
    }
  },
);

await b.work(QUEUE.RECORDS_SALESFORCE_SYNC_SWEEP, async () => {
  const summary = await runRecordsSalesforceSyncSweep({
    ...defaultSalesforceSyncSweepDependencies,
    enqueue: (payload) =>
      enqueue(QUEUE.RECORDS_SALESFORCE_SYNC, payload, {
        retryLimit: 8,
        retryDelay: 60,
        retryBackoff: true,
        singletonKey: `records-salesforce-sync:${payload.processingJobId}`,
      }),
  });
  if (summary.queued > 0) {
    console.log(`[records-salesforce-sync] scheduled ${summary.queued} delta run(s)`);
  }
});

await b.work(
  QUEUE.CHANNEL_DELIVER,
  { batchSize: 1, pollingIntervalSeconds: 0.5 },
  async (jobs: PgBossLib.Job<ChannelDeliverPayload>[]) => {
    for (const job of jobs) {
      try {
        await runChannelDelivery(job.data);
      } catch (e) {
        console.warn(
          `[channel-deliver] job ${job.id} failed; pg-boss may retry: ${e instanceof Error ? e.message : e}`,
        );
        throw e;
      }
    }
  },
);

await b.work(QUEUE.DIRECTORY_SYNC, async () => {
  if (!pluginRegistry?.getDirectoryProvider()) return;
  try {
    const stats = await runDirectorySync(pluginRegistry, await getOrgId());
    console.log(`[directory-sync] ${JSON.stringify(stats)}`);
  } catch (e) {
    console.warn(`[directory-sync] failed: ${e instanceof Error ? e.message : e}`);
  }
});
await b.schedule(QUEUE.DIRECTORY_SYNC, DIRECTORY_SYNC_CRON, {}, { tz: "UTC", retryLimit: 0 });

await b.schedule(QUEUE.WORKFLOW_CRON_SWEEP, "* * * * *", {}, {
  tz: "UTC",
  retryLimit: 1,
  retryDelay: 15,
});
console.log("[worker] scheduled behavior + watcher auxiliary sweep every minute");

await b.schedule(QUEUE.RECORDS_SALESFORCE_SYNC_SWEEP, "* * * * *", {}, {
  tz: "UTC",
  retryLimit: 1,
  retryDelay: 15,
});
console.log("[worker] scheduled Salesforce delta sync sweep every minute");

await b.schedule(
  QUEUE.RECORDS_BACKUP_VERIFY,
  "0 3 * * 0",
  { orgId: ADMIN_ORG_ID },
  {
    tz: "UTC",
    retryLimit: 2,
    retryDelay: 300,
    retryBackoff: true,
  },
);
console.log("[worker] scheduled whole-deployment restore verification weekly");

await b.schedule(
  QUEUE.RECORDS_OPS_WATCH,
  "*/5 * * * *",
  { orgId: ADMIN_ORG_ID },
  {
    tz: "UTC",
    retryLimit: 2,
    retryDelay: 30,
  },
);
console.log("[worker] scheduled records substrate watcher every five minutes");

await b.schedule(
  QUEUE.RECORDS_WATCH_SWEEP,
  "*/15 * * * *",
  { orgId: ADMIN_ORG_ID },
  {
    tz: "UTC",
    retryLimit: 2,
    retryDelay: 30,
  },
);
console.log("[worker] scheduled records starter-watch fallback every fifteen minutes");

await b.work(QUEUE.WORKFLOW_OUTPUT_TTL_SWEEP, async () => {
  await runWorkflowOutputTtlSweep();
});

await b.schedule(QUEUE.WORKFLOW_OUTPUT_TTL_SWEEP, "0 * * * *", {}, {
  tz: "UTC",
  retryLimit: 1,
  retryDelay: 60,
});
console.log("[worker] scheduled workflow_output ttl sweep hourly");

type CachedDataSource = { ctx: DataSourceContext; expiresAt: number };
const DATA_SOURCE_CACHE_MS = 60_000;
const dataSourceCache = new Map<string, CachedDataSource>();

async function loadDataSourceContext(orgId: string): Promise<DataSourceContext> {
  const now = Date.now();
  const cached = dataSourceCache.get(orgId);
  if (cached && cached.expiresAt > now) return cached.ctx;
  const ctx = await getDataSourceForOrg(orgId);
  if (!ctx) throw new Error(`no data_source configured for org ${orgId}`);
  dataSourceCache.set(orgId, { ctx, expiresAt: now + DATA_SOURCE_CACHE_MS });
  return ctx;
}

const subscriptionManager = startSubscriptionManager({
  resolveTransport: async (sub) => {
    if (sub.sourceKind === "source_change") {
      const ctx = await loadDataSourceContext(sub.orgId);
      // GraphJin serves subscriptions on the same endpoint as queries, so
      // drive off graphql_url (the URL that's kept reachable per deploy mode).
      // subscription_url drifts — it isn't rewritten alongside graphql_url
      // when the host is provisioned (e.g. compose service-DNS vs host
      // localhost), which left source_change subs pointing at an
      // unresolvable host.
      return { baseUrl: toGraphqlEndpoint(ctx.graphqlUrl) };
    }
    return { baseUrl: GRAPHJIN_URL };
  },
  refreshIntervalMs: 60_000,
  onMatch: async (event) => {
    if (event.kind === "workflow_output") {
      const decision = await handleSubscriptionMatch({
        subscription: event.subscription,
        output: event.output,
        resolveProducingRunChainDepth: getWorkflowRunChainDepth,
      });
      if (decision.action === "dropped") {
        console.log(
          `[subscription-manager] dropped match sub=${event.subscription.id} output=${event.output.id}: ${decision.reason}`,
        );
      } else {
        console.log(
          `[subscription-manager] enqueued sub=${event.subscription.id} output=${event.output.id} obs=${decision.observationId}`,
        );
      }
      return;
    }
    if (event.kind === "source_change") {
      const ctx = await loadDataSourceContext(event.subscription.orgId);
      const decision = await handleSourceChangeMatch({
        subscription: event.subscription,
        match: event.match,
        dataSourceId: ctx.id,
      });
      const pk = JSON.stringify(event.match.primary_key);
      if (decision.action === "dropped") {
        console.log(
          `[subscription-manager] dropped source_change sub=${event.subscription.id} ${event.match.table}:${pk}: ${decision.reason}`,
        );
      } else {
        console.log(
          `[subscription-manager] enqueued source_change sub=${event.subscription.id} ${event.match.table}:${pk} obs=${decision.observationId}`,
        );
      }
      return;
    }
  },
  onError: (err, sub) => {
    console.warn(
      `[subscription-manager] error${sub ? ` sub=${sub.id}` : ""}: ${err.message}`,
    );
  },
});

subscriptionManager.ready
  .then(() => {
    console.log(
      `[worker] subscription manager ready (${subscriptionManager.activeSubscriptionIds().length} active)`,
    );
  })
  .catch((err) => {
    console.warn(
      `[subscription-manager] initial connect failed: ${err instanceof Error ? err.message : err}`,
    );
  });


if (SCHEDULED_REFRESH_HOURS > 0) {
  const cron =
    SCHEDULED_REFRESH_HOURS === 1
      ? "0 * * * *"
      : `0 */${SCHEDULED_REFRESH_HOURS} * * *`;
  await b.schedule(QUEUE.METRIC_REFRESH_SCHEDULED_SWEEP, cron, {}, {
    tz: "UTC",
    retryLimit: MAX_JOB_RETRIES,
    retryDelay: 30,
  });
  console.log(
    `[worker] scheduled metric refresh sweep: cron="${cron}" (every ${SCHEDULED_REFRESH_HOURS}h)`,
  );
}

// Cron workflow delivery owns its timer and persisted cursor. It deliberately
// does not ride on pg-boss's in-process schedule polling: queue timers remain
// useful for auxiliary maintenance, but cannot be the source of truth for a
// business schedule that must catch up after downtime.
const workflowScheduler = await startDurableWorkflowScheduler();
const workflowApiDispatcher = await startWorkflowApiDispatcher();

const reconcileTimer = setInterval(() => {
  reconcileStaleProcessingJobs({ minAgeMs: RECONCILE_SWEEP_MIN_AGE_MS })
    .then((s) => {
      const total = s.succeeded + s.failed + s.requeued + s.lost;
      if (total > 0) {
        console.log(
          `[worker] reconcile sweep: succeeded=${s.succeeded} failed=${s.failed} requeued=${s.requeued} lost=${s.lost}`,
        );
      }
    })
    .catch((e) => {
      console.warn(
        `[worker] reconcile sweep failed: ${e instanceof Error ? e.message : e}`,
      );
    });
  reconcileStaleRuns({ minAgeMs: RECONCILE_SWEEP_MIN_AGE_MS })
    .then((s) => {
      if (s.cancelled > 0) {
        console.log(
          `[worker] reconcile sweep: cancelled ${s.cancelled} stale running run(s)`,
        );
      }
    })
    .catch((e) => {
      console.warn(
        `[worker] stale-run sweep failed: ${e instanceof Error ? e.message : e}`,
      );
    });
}, RECONCILE_SWEEP_INTERVAL_MS);
reconcileTimer.unref();

const libraryCleanup = () => {
  sweepExpiredLibraryTransientState()
    .then((count) => {
      if (count > 0) {
        console.log(`[library-cleanup] removed ${count} expired transient state(s)`);
      }
    })
    .catch((error) => {
      console.warn(
        `[library-cleanup] sweep failed: ${error instanceof Error ? error.message : error}`,
      );
    });
};
libraryCleanup();
const libraryCleanupTimer = setInterval(libraryCleanup, 6 * 60 * 60 * 1_000);
libraryCleanupTimer.unref();

const libraryRecovery = () => {
  recoverUnqueuedLibraryUploads()
    .then((count) => {
      if (count > 0) {
        console.log(`[library-recovery] enqueued ${count} stranded upload(s)`);
      }
    })
    .catch((error) => {
      console.warn(
        `[library-recovery] sweep failed: ${error instanceof Error ? error.message : error}`,
      );
    });
};
libraryRecovery();
const libraryRecoveryTimer = setInterval(libraryRecovery, 60_000);
libraryRecoveryTimer.unref();

const packOAuthRefresh = () => {
  packService.refreshOAuthConnections()
    .then((count) => {
      if (count > 0) console.log(`[packs] refreshed ${count} OAuth connection(s)`);
    })
    .catch((error) => {
      console.warn(`[packs] OAuth refresh failed: ${error instanceof Error ? error.message : String(error)}`);
    });
};
void packOAuthRefresh();
const packOAuthRefreshTimer = setInterval(packOAuthRefresh, 60_000);
packOAuthRefreshTimer.unref();

server.listen(PORT, () => {
  console.log(
    `[worker] pg-boss running; /health on http://localhost:${PORT}`,
  );
});

const channelInbound = startChannelInbound(ADMIN_ORG_ID);

const shutdown = async (signal: string) => {
  console.log(`[worker] received ${signal}; shutting down`);
  clearInterval(reconcileTimer);
  clearInterval(libraryCleanupTimer);
  clearInterval(libraryRecoveryTimer);
  clearInterval(packOAuthRefreshTimer);
  clearInterval(knowledgeRefreshTimer);
  workflowScheduler.stop();
  workflowApiDispatcher.stop();
  channelInbound.stop();
  unregisterRecordSchemaPreflight();
  unregisterRecordImportPreflight();
  unregisterRecordArtifactImportPreflight();
  unregisterRecordSalesforcePreflight();
  unregisterPackActionPreflight();
  unregisterPackConnectionPreflight();
  server.close();
  const cancelled = cancelAllAgents();
  if (cancelled > 0) {
    console.log(`[worker] cancelled ${cancelled} in-flight agent call(s)`);
  }
  try {
    await subscriptionManager.stop();
  } catch (e) {
    console.error("[worker] subscription manager stop error:", e);
  }
  try {
    setPluginRegistryInstance(null);
    if (pluginRegistry) {
      await pluginRegistry.stop();
    }
  } catch (e) {
    console.error("[worker] plugin shutdown error:", e);
  }
  try {
    await b.stop({ graceful: true });
  } catch (e) {
    console.error("[worker] pg-boss stop error:", e);
  }
  try {
    await recordsPool.end();
  } catch (e) {
    console.error("[worker] records pool shutdown error:", e);
  }
  try { await closeSandboxPools(); }
  catch (error) { console.error("[worker] sandbox shutdown error:", error); }
  await shutdownWorkerTelemetry();
  process.exit(0);
};
process.on("SIGINT", () => shutdown("SIGINT"));
process.on("SIGTERM", () => shutdown("SIGTERM"));
