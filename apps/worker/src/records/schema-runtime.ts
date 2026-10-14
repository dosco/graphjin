import { join } from "node:path";
import { networkInterfaces } from "node:os";
import type { PoolConfig } from "pg";
import type pg from "pg";
import {
  app_state,
  db,
  sql,
} from "@neko/db";
import {
  buildRecordsPoolConfig,
  runRecordsMigrations,
} from "@neko/db/records-migrate";
import {
  RECORDS_GRAPHJIN_CONFIG_FILENAME,
  RecordsAccessAdmin,
  RecordsGraphjinSchemaCli,
  RecordsSchemaPlanner,
  RecordsSchemaSaga,
  createRecordsGraphjinConfigValidator,
  createRecordsSchemaWorkspace,
  loadRecordsGraphjinPolicyModel,
  parseRecordAppDefinition,
  projectRecordAppDefinition,
  projectRecordsGraphjinRoles,
  recordIdentifier,
  recordsGraphjinCursorSecret,
  recordsGraphjinSigningSecret,
  recordsWatchGraphjinCursorSecret,
  recordsWatchGraphjinSigningSecret,
  recordsWatchWebhookSecret,
  writeRecordsGraphjinConfig,
  writeRecordsWatchGraphjinConfig,
  writeRecordsWatchWebhookSecret,
  type RecordAppDefinition,
  type RecordSchemaChange,
  type RecordsGraphjinConfigValidator,
  type WriteRecordsGraphjinConfigResult,
} from "@neko/records";
import { RecordsDataPlaneController } from "./data-plane-control.js";

export type RecordsMetadataAppStateInput = {
  orgId: string;
  definition: RecordAppDefinition;
  registryRevision: string;
  actorUserId: string | null;
};

export type RecordsMetadataAppStateProjector = (
  input: RecordsMetadataAppStateInput,
) => Promise<void>;

export type RecordsPolicyProjector = () => Promise<WriteRecordsGraphjinConfigResult>;

const RECORDS_WATCH_WEBHOOK_PATH = "/admin/events/records-watch";

export function resolveRecordsWatchWebhookUrl(
  env: NodeJS.ProcessEnv = process.env,
  interfaces: ReturnType<typeof networkInterfaces> = networkInterfaces(),
): string | null {
  const configured = env.OPENNEKO_RECORDS_WATCH_WEBHOOK_URL?.trim();
  if (configured) {
    let url: URL;
    try {
      url = new URL(configured);
    } catch {
      throw new Error("records watch webhook URL must be absolute");
    }
    if (url.protocol !== "http:" && url.protocol !== "https:") {
      throw new Error("records watch webhook URL must use HTTP or HTTPS");
    }
    if (
      url.username ||
      url.password ||
      url.search ||
      url.hash ||
      url.pathname !== RECORDS_WATCH_WEBHOOK_PATH
    ) {
      throw new Error(
        `records watch webhook URL must target exactly ${RECORDS_WATCH_WEBHOOK_PATH}`,
      );
    }
    return url.toString();
  }

  for (const name of Object.keys(interfaces).sort()) {
    for (const address of interfaces[name] ?? []) {
      if (address.family === "IPv4" && !address.internal) {
        // GraphJin permits a private target only when the allowlist names the
        // literal IP. Resolving the worker's current container address here
        // keeps the default Compose path exact and fail-closed across restarts.
        return `http://${address.address}:4100${RECORDS_WATCH_WEBHOOK_PATH}`;
      }
    }
  }
  return null;
}

function stringConfig(value: PoolConfig[keyof PoolConfig], label: string): string {
  if (typeof value !== "string" && typeof value !== "number") {
    throw new Error(`records Postgres ${label} must be a static value`);
  }
  const result = String(value);
  if (!result) throw new Error(`records Postgres ${label} is required`);
  return result;
}

/** Build the URL consumed by the separate GraphJin process without logging it. */
export function recordsPostgresConnectionString(
  env: NodeJS.ProcessEnv = process.env,
): string {
  const config = buildRecordsPoolConfig(env);
  const host = stringConfig(config.host, "host");
  const port = stringConfig(config.port, "port");
  const user = stringConfig(config.user, "user");
  const database = stringConfig(config.database, "database");
  const password =
    typeof config.password === "string" ? config.password : "";
  const authorityHost = host.includes(":") ? `[${host}]` : host;
  return `postgresql://${encodeURIComponent(user)}:${encodeURIComponent(
    password,
  )}@${authorityHost}:${port}/${encodeURIComponent(database)}`;
}

/**
 * Connection string written into records GraphJin configs. In development the
 * worker runs on the host while GraphJin runs in Docker, so
 * OPENNEKO_RECORDS_GRAPHJIN_DB_HOST and _PORT give GraphJin its own address.
 */
export function recordsGraphjinConnectionString(
  env: NodeJS.ProcessEnv = process.env,
): string {
  const url = new URL(recordsPostgresConnectionString(env));
  const host = env.OPENNEKO_RECORDS_GRAPHJIN_DB_HOST?.trim();
  const port = env.OPENNEKO_RECORDS_GRAPHJIN_DB_PORT?.trim();
  if (host) url.hostname = host;
  if (port) url.port = port;
  return url.toString();
}

function schemaChangeDefinition(change: RecordSchemaChange): RecordAppDefinition {
  const definition = parseRecordAppDefinition(change.artifact.detail.definition);
  if (definition.appId !== change.appId) {
    throw new Error("records schema artifact app does not match its journal row");
  }
  return definition;
}

/** Give groups that can open the object the same access to the added field. */
async function grantAddedField(pool: pg.Pool, change: RecordSchemaChange): Promise<void> {
  const payload = change.artifact.detail.payload as Record<string, unknown> | undefined;
  const field = payload?.field as Record<string, unknown> | undefined;
  const source = field?.api_name ?? field?.name ?? field?.label;
  if (typeof payload?.object !== "string" || typeof source !== "string") return;
  await new RecordsAccessAdmin(pool).grantFieldLikeObject({
    orgId: change.orgId,
    appId: change.appId,
    objectApiName: recordIdentifier(payload.object),
    fieldApiName: recordIdentifier(source),
    actor: {
      userId: change.artifact.actorUserId ?? "system",
      actionRequestId: change.actionRequestId,
    },
  });
}

export async function projectMetadataAppState(
  input: RecordsMetadataAppStateInput,
): Promise<void> {
  const status = input.definition.archived ? "archived" : "active";
  const config = JSON.stringify({
    registryRevision: input.registryRevision,
    label: input.definition.label,
    purpose: input.definition.purpose,
    createdByActor: input.actorUserId,
  });
  await db().execute(sql`
    insert into ${app_state}
      (org_id, app_id, status, created_by, config)
    values (
      ${input.orgId}, ${input.definition.appId}, ${status}, null,
      ${config}::jsonb
    )
    on conflict (org_id, app_id) do update
    set status = case
          when app_state.status = 'importing' and excluded.status = 'active'
            then 'importing'
          else excluded.status
        end,
        config = app_state.config || excluded.config
  `);
}

async function hasProjectionBlockers(pool: pg.Pool): Promise<boolean> {
  const result = await pool.query<{ blocked: boolean }>(`
    select exists (
      select 1 from engine.app_schema_change where phase in ('applying', 'applied')
      union all
      select 1 from engine.record_app where status = 'degraded'
    ) as blocked
  `);
  return result.rows[0]?.blocked ?? true;
}

export type RecordsSchemaRuntime = {
  planner: RecordsSchemaPlanner;
  saga: RecordsSchemaSaga;
  controller: RecordsDataPlaneController;
  watchController: RecordsDataPlaneController | null;
  projectPolicy: RecordsPolicyProjector;
  reconciliation: Awaited<ReturnType<RecordsSchemaSaga["reconcilePending"]>>;
};

/**
 * Migrate and reconcile the complete schema control plane before the worker
 * begins accepting action proposals. The returned planner and saga are then
 * registered by the worker's action layer.
 */
export async function createRecordsSchemaRuntime(options: {
  pool: pg.Pool;
  orgId: string;
  graphjinBaseUrl?: string;
  configDirectory?: string;
  graphjinBinary?: string;
  connectionString?: string;
  controller?: RecordsDataPlaneController;
  enableWatchPlane?: boolean;
  watchController?: RecordsDataPlaneController;
  watchGraphjinBaseUrl?: string;
  watchConfigDirectory?: string;
  watchWebhookAllow?: string[];
  cli?: RecordsGraphjinSchemaCli;
  validateConfig?: RecordsGraphjinConfigValidator;
  projectMetadata?: RecordsMetadataAppStateProjector;
}): Promise<RecordsSchemaRuntime> {
  await runRecordsMigrations({ pool: options.pool });

  const graphjinBaseUrl =
    options.graphjinBaseUrl ??
    process.env.OPENNEKO_RECORDS_GRAPHJIN_URL ??
    "http://127.0.0.1:8090";
  const configDirectory =
    options.configDirectory ??
    process.env.OPENNEKO_RECORDS_GRAPHJIN_CONFIG_DIR ??
    "/tmp/openneko-records-graphjin";
  const configFile = join(configDirectory, RECORDS_GRAPHJIN_CONFIG_FILENAME);
  const binary =
    options.graphjinBinary ??
    process.env.OPENNEKO_RECORDS_GRAPHJIN_BINARY ??
    "graphjin";
  const controller =
    options.controller ??
    new RecordsDataPlaneController({
      configDirectory,
      baseUrl: graphjinBaseUrl,
    });
  const watchConfigDirectory =
    options.watchConfigDirectory ?? join(configDirectory, "watch");
  const watchController = options.enableWatchPlane
    ? options.watchController ??
      new RecordsDataPlaneController({
        configDirectory: watchConfigDirectory,
        baseUrl:
          options.watchGraphjinBaseUrl ??
          process.env.OPENNEKO_RECORDS_WATCH_GRAPHJIN_URL ??
          "http://127.0.0.1:8091",
      })
    : null;
  const validateConfig =
    options.validateConfig ?? createRecordsGraphjinConfigValidator({ binary });
  const connectionString =
    options.connectionString ?? recordsGraphjinConnectionString();

  const projectPolicy: RecordsPolicyProjector = async () => {
    const model = await loadRecordsGraphjinPolicyModel(options.pool, options.orgId);
    const projected = await writeRecordsGraphjinConfig({
      configFile,
      config: {
        orgId: options.orgId,
        roles: projectRecordsGraphjinRoles(model),
        database: { connectionString },
        jwt: { secret: recordsGraphjinSigningSecret(options.orgId) },
        secretKey: recordsGraphjinCursorSecret(options.orgId),
      },
      validate: validateConfig,
      reloadRecordsGraphjin: () => controller.requestReload(),
    });
    if (watchController) {
      await writeRecordsWatchWebhookSecret(
        watchConfigDirectory,
        recordsWatchWebhookSecret(options.orgId),
      );
      await writeRecordsWatchGraphjinConfig({
        configFile: join(
          watchConfigDirectory,
          RECORDS_GRAPHJIN_CONFIG_FILENAME,
        ),
        config: {
          orgId: options.orgId,
          database: { connectionString },
          jwt: { secret: recordsWatchGraphjinSigningSecret(options.orgId) },
          secretKey: recordsWatchGraphjinCursorSecret(options.orgId),
          watchWebhookAllow: options.watchWebhookAllow,
        },
        validate: validateConfig,
        reloadRecordsGraphjin: () => watchController.requestReload(),
      });
    }
    return projected;
  };

  const setDataPlanesReady = async (ready: boolean, reason: string) => {
    if (!watchController) {
      await controller.setReady(ready, reason);
      return;
    }
    if (!ready) {
      await Promise.all([
        controller.setReady(false, reason),
        watchController.setReady(false, reason),
      ]);
      return;
    }
    await controller.setReady(true, reason);
    try {
      await watchController.setReady(true, reason);
    } catch (error) {
      await controller
        .setReady(false, "records watch data plane failed to become ready")
        .catch(() => undefined);
      throw error;
    }
  };

  const projectMetadata = options.projectMetadata ?? projectMetadataAppState;
  const cli = options.cli ?? new RecordsGraphjinSchemaCli({ binary });
  const saga = new RecordsSchemaSaga({
    pool: options.pool,
    cli,
    workspace: createRecordsSchemaWorkspace(configFile),
    setDataPlaneReady: setDataPlanesReady,
    project: async (change) => {
      const definition = schemaChangeDefinition(change);
      const registryRevision = await projectRecordAppDefinition(options.pool, {
        orgId: change.orgId,
        definition,
        desiredRevision: change.desiredRevision,
        ...(change.artifact.action === "app_create" && change.artifact.actorUserId
          ? {
              creatorGrant: {
                userId: change.artifact.actorUserId,
                actionRequestId: change.actionRequestId,
              },
            }
          : {}),
      });
      await projectPolicy();
      if (change.artifact.action === "app_field_add") {
        await grantAddedField(options.pool, change);
      }
      await projectMetadata({
        orgId: change.orgId,
        definition,
        registryRevision,
        actorUserId: change.artifact.actorUserId,
      });
    },
  });
  const planner = new RecordsSchemaPlanner({ pool: options.pool, saga });

  await setDataPlanesReady(false, "records worker boot reconciliation");
  await projectPolicy();
  const reconciliation = await saga.reconcilePending();
  if (reconciliation.failed.length > 0 || (await hasProjectionBlockers(options.pool))) {
    await setDataPlanesReady(
      false,
      "records schema reconciliation remains incomplete",
    );
  } else {
    await setDataPlanesReady(true, "records schema boot reconciliation complete");
  }

  return {
    planner,
    saga,
    controller,
    watchController,
    projectPolicy,
    reconciliation,
  };
}
