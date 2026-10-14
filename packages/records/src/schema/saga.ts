import { chmod, copyFile, mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { basename, join } from "node:path";
import type { Pool, PoolClient } from "pg";
import { RECORDS_GRAPHJIN_CONFIG_FILENAME } from "../graphjin/config";
import {
  RecordsGraphjinSchemaCli,
  recordsSchemaApprovalHash,
  writeRecordsGraphjinDdl,
} from "../graphjin/schema";
import { validateRecordIdentifier } from "../naming";
import { ensureRecordsAuditTrigger } from "./audit";
import { loadRecordsCatalogSnapshot } from "./catalog";
import {
  buildRecordsGraphjinDdl,
  verifyRecordsGraphjinDdlApplied,
  type RecordDdlObjectDefinition,
} from "./ddl";

const RECORDS_SCHEMA_LOCK = "openneko-records-schema-v1";
const ARTIFACT_FORMAT = "openneko.records.schema-change.v1";

export const RECORD_SCHEMA_ACTIONS = [
  "app_create",
  "app_object_create",
  "app_field_add",
  "app_field_modify",
  "app_object_archive",
  "app_field_archive",
  "app_permission_set",
  "app_layout_update",
] as const;

export const RECORD_SCHEMA_PHASES = [
  "planned",
  "approved",
  "applying",
  "applied",
  "projected",
  "failed",
] as const;

export type RecordSchemaAction = (typeof RECORD_SCHEMA_ACTIONS)[number];
export type RecordSchemaPhase = (typeof RECORD_SCHEMA_PHASES)[number];

export type RecordSchemaChangeArtifact = {
  format: typeof ARTIFACT_FORMAT;
  action: RecordSchemaAction;
  actorUserId: string | null;
  detail: Record<string, unknown>;
  document: string;
  objects: RecordDdlObjectDefinition[];
};

export type RecordSchemaChange = {
  id: string;
  orgId: string;
  appId: string;
  actionRequestId: string;
  desiredRevision: string;
  catalogRevision: string;
  artifact: RecordSchemaChangeArtifact;
  previewSql: string;
  previewHash: string;
  phase: RecordSchemaPhase;
  attempts: number;
  lastError: Record<string, unknown> | null;
};

type RawSchemaChange = {
  id: string;
  org_id: string;
  app_id: string;
  action_request_id: string;
  desired_revision: string;
  catalog_revision: string;
  graphjin_ddl: unknown;
  preview_sql: string;
  preview_hash: string;
  phase: RecordSchemaPhase;
  attempts: number;
  last_error: Record<string, unknown> | null;
};

export type RecordsSchemaWorkspace = <T>(
  input: { actionRequestId: string; ddl: string },
  operation: (configDirectory: string) => Promise<T>,
) => Promise<T>;

export class RecordsSchemaApprovalStaleError extends Error {
  readonly code = "records_schema_approval_stale";

  constructor(message: string) {
    super(message);
    this.name = "RecordsSchemaApprovalStaleError";
  }
}

export class RecordsSchemaPhaseError extends Error {
  readonly code = "records_schema_phase_invalid";

  constructor(message: string) {
    super(message);
    this.name = "RecordsSchemaPhaseError";
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function parseArtifact(value: unknown): RecordSchemaChangeArtifact {
  if (!isRecord(value) || value.format !== ARTIFACT_FORMAT) {
    throw new Error("invalid records schema change artifact format");
  }
  if (!RECORD_SCHEMA_ACTIONS.includes(value.action as RecordSchemaAction)) {
    throw new Error("invalid records schema change action");
  }
  if (typeof value.document !== "string" || !value.document.trim()) {
    throw new Error("invalid records schema change DDL document");
  }
  if (!Array.isArray(value.objects) || !isRecord(value.detail)) {
    throw new Error("invalid records schema change desired model");
  }
  const artifact = value as RecordSchemaChangeArtifact;
  const projected = buildRecordsGraphjinDdl(artifact.objects);
  if (projected !== artifact.document) {
    throw new Error("records schema change DDL does not match its desired model");
  }
  return artifact;
}

function mapChange(row: RawSchemaChange): RecordSchemaChange {
  return {
    id: row.id,
    orgId: row.org_id,
    appId: row.app_id,
    actionRequestId: row.action_request_id,
    desiredRevision: row.desired_revision,
    catalogRevision: row.catalog_revision,
    artifact: parseArtifact(row.graphjin_ddl),
    previewSql: row.preview_sql,
    previewHash: row.preview_hash,
    phase: row.phase,
    attempts: row.attempts,
    lastError: row.last_error,
  };
}

function durableError(error: unknown): Record<string, unknown> {
  const code =
    error instanceof RecordsSchemaApprovalStaleError ||
    error instanceof RecordsSchemaPhaseError
      ? error.code
      : "records_schema_execution_failed";
  return {
    code,
    message: error instanceof Error ? error.message : String(error),
    at: new Date().toISOString(),
  };
}

async function inTransaction<T>(client: PoolClient, operation: () => Promise<T>): Promise<T> {
  await client.query("begin");
  try {
    const result = await operation();
    await client.query("commit");
    return result;
  } catch (error) {
    await client.query("rollback");
    throw error;
  }
}

async function appendSchemaLog(
  client: PoolClient,
  change: RecordSchemaChange,
  phase: RecordSchemaPhase,
  extra: Record<string, unknown> = {},
): Promise<void> {
  await client.query(
    `insert into engine.app_schema_log
       (org_id, app_id, action, detail, ddl, actor_user_id, action_request_id)
     values ($1, $2, $3, $4::jsonb, $5::jsonb, $6, $7)`,
    [
      change.orgId,
      change.appId,
      change.artifact.action,
      JSON.stringify({ ...change.artifact.detail, sagaPhase: phase, ...extra }),
      JSON.stringify({
        graphjinDdl: change.artifact.document,
        previewSql: change.previewSql,
        previewHash: change.previewHash,
        catalogRevision: change.catalogRevision,
        desiredRevision: change.desiredRevision,
      }),
      change.artifact.actorUserId,
      change.actionRequestId,
    ],
  );
}

async function loadChange(
  client: Pick<Pool | PoolClient, "query">,
  actionRequestId: string,
  forUpdate = false,
): Promise<RecordSchemaChange | null> {
  const result = await client.query<RawSchemaChange>(
    `select id, org_id, app_id, action_request_id,
            desired_revision::text as desired_revision,
            catalog_revision, graphjin_ddl, preview_sql, preview_hash,
            phase, attempts, last_error
     from engine.app_schema_change
     where action_request_id = $1${forUpdate ? " for update" : ""}`,
    [actionRequestId],
  );
  return result.rows[0] ? mapChange(result.rows[0]) : null;
}

export function createRecordsSchemaWorkspace(configFile: string): RecordsSchemaWorkspace {
  if (basename(configFile) !== RECORDS_GRAPHJIN_CONFIG_FILENAME) {
    throw new Error(`records schema config must be named ${RECORDS_GRAPHJIN_CONFIG_FILENAME}`);
  }
  return async (input, operation) => {
    const directory = await mkdtemp(join(tmpdir(), "openneko-records-schema-"));
    try {
      const stagedConfig = join(directory, RECORDS_GRAPHJIN_CONFIG_FILENAME);
      await copyFile(configFile, stagedConfig);
      await chmod(stagedConfig, 0o600);
      await writeRecordsGraphjinDdl(directory, input.ddl);
      return await operation(directory);
    } finally {
      await rm(directory, { recursive: true, force: true });
    }
  };
}

const TABLE_ACTIONS = new Set<RecordSchemaAction>([
  "app_create",
  "app_object_create",
  "app_object_archive",
]);

export class RecordsSchemaSaga {
  constructor(
    private readonly dependencies: {
      pool: Pool;
      cli: RecordsGraphjinSchemaCli;
      workspace: RecordsSchemaWorkspace;
      setDataPlaneReady: (ready: boolean, reason: string) => Promise<void>;
      project: (change: RecordSchemaChange) => Promise<void>;
    },
  ) {}

  async get(actionRequestId: string): Promise<RecordSchemaChange | null> {
    return loadChange(this.dependencies.pool, actionRequestId);
  }

  async plan(input: {
    orgId: string;
    appId: string;
    actionRequestId: string;
    desiredRevision: string;
    action: RecordSchemaAction;
    actorUserId: string | null;
    detail: Record<string, unknown>;
    objects: RecordDdlObjectDefinition[];
  }): Promise<RecordSchemaChange> {
    if (!input.orgId.trim() || !input.appId.trim() || !input.actionRequestId.trim()) {
      throw new Error("records schema proposal requires org, app, and action request ids");
    }
    if (!/^\d+$/.test(input.desiredRevision) || input.desiredRevision === "0") {
      throw new Error("records schema desired revision must be a positive integer");
    }
    const document = buildRecordsGraphjinDdl(input.objects);
    const artifact: RecordSchemaChangeArtifact = {
      format: ARTIFACT_FORMAT,
      action: input.action,
      actorUserId: input.actorUserId,
      detail: input.detail,
      document,
      objects: input.objects,
    };
    const catalog = await loadRecordsCatalogSnapshot(this.dependencies.pool);
    const previewSql = await this.dependencies.workspace(
      { actionRequestId: input.actionRequestId, ddl: document },
      (directory) => this.dependencies.cli.diff(directory),
    );
    const previewHash = recordsSchemaApprovalHash({
      desiredRevision: input.desiredRevision,
      catalogRevision: catalog.revision,
      graphjinDdl: document,
      previewSql,
    });

    const client = await this.dependencies.pool.connect();
    try {
      return await inTransaction(client, async () => {
        const existing = await loadChange(client, input.actionRequestId, true);
        if (existing) {
          if (existing.previewHash !== previewHash) {
            throw new Error("records schema action request was reused with different bytes");
          }
          return existing;
        }
        const inserted = await client.query<RawSchemaChange>(
          `insert into engine.app_schema_change
             (org_id, app_id, action_request_id, desired_revision,
              catalog_revision, graphjin_ddl, preview_sql, preview_hash, phase)
           values ($1, $2, $3, $4::bigint, $5, $6::jsonb, $7, $8, 'planned')
           returning id, org_id, app_id, action_request_id,
                     desired_revision::text as desired_revision,
                     catalog_revision, graphjin_ddl, preview_sql, preview_hash,
                     phase, attempts, last_error`,
          [
            input.orgId,
            input.appId,
            input.actionRequestId,
            input.desiredRevision,
            catalog.revision,
            JSON.stringify(artifact),
            previewSql,
            previewHash,
          ],
        );
        const change = mapChange(inserted.rows[0]!);
        await appendSchemaLog(client, change, "planned");
        return change;
      });
    } finally {
      client.release();
    }
  }

  async approve(actionRequestId: string, previewHash: string): Promise<RecordSchemaChange> {
    const client = await this.dependencies.pool.connect();
    try {
      return await inTransaction(client, async () => {
        const change = await loadChange(client, actionRequestId, true);
        if (!change) throw new Error(`unknown records schema action request: ${actionRequestId}`);
        if (change.previewHash !== previewHash) {
          throw new RecordsSchemaApprovalStaleError(
            "records schema approval hash does not match the planned bytes",
          );
        }
        if (change.phase === "approved") return change;
        if (change.phase !== "planned") {
          throw new RecordsSchemaPhaseError(
            `records schema change cannot be approved from ${change.phase}`,
          );
        }
        await client.query(
          `update engine.app_schema_change
           set phase = 'approved', updated_at = now(), last_error = null
           where id = $1`,
          [change.id],
        );
        const approved = { ...change, phase: "approved" as const, lastError: null };
        await appendSchemaLog(client, approved, "approved");
        return approved;
      });
    } finally {
      client.release();
    }
  }

  private async markPhase(
    client: PoolClient,
    change: RecordSchemaChange,
    phase: RecordSchemaPhase,
    options: { incrementAttempts?: boolean; extra?: Record<string, unknown> } = {},
  ): Promise<RecordSchemaChange> {
    return inTransaction(client, async () => {
      await client.query(
        `update engine.app_schema_change
         set phase = $2,
             attempts = attempts + $3,
             last_error = null,
             updated_at = now()
         where id = $1`,
        [change.id, phase, options.incrementAttempts ? 1 : 0],
      );
      const updated = {
        ...change,
        phase,
        attempts: change.attempts + (options.incrementAttempts ? 1 : 0),
        lastError: null,
      };
      await appendSchemaLog(client, updated, phase, options.extra);
      return updated;
    });
  }

  private async failChange(
    client: PoolClient,
    change: RecordSchemaChange,
    error: unknown,
  ): Promise<void> {
    const failure = durableError(error);
    const terminal = error instanceof RecordsSchemaApprovalStaleError;
    await inTransaction(client, async () => {
      await client.query(
        `update engine.app_schema_change
         set phase = case when $3 then 'failed' else phase end,
             last_error = $2::jsonb,
             updated_at = now()
         where id = $1`,
        [change.id, JSON.stringify(failure), terminal],
      );
      await appendSchemaLog(
        client,
        change,
        terminal ? "failed" : change.phase,
        { error: failure },
      );
      await client.query(
        `update engine.record_app set status = 'degraded', updated_at = now()
         where org_id = $1 and app_id = $2 and status <> 'archived'`,
        [change.orgId, change.appId],
      );
    });
  }

  private async verifyFinalPreview(change: RecordSchemaChange): Promise<void> {
    const catalog = await loadRecordsCatalogSnapshot(this.dependencies.pool);
    if (catalog.revision !== change.catalogRevision) {
      throw new RecordsSchemaApprovalStaleError(
        "records catalog changed after schema preview; a fresh approval is required",
      );
    }
    const previewSql = await this.dependencies.workspace(
      { actionRequestId: change.actionRequestId, ddl: change.artifact.document },
      (directory) => this.dependencies.cli.diff(directory),
    );
    const previewHash = recordsSchemaApprovalHash({
      desiredRevision: change.desiredRevision,
      catalogRevision: catalog.revision,
      graphjinDdl: change.artifact.document,
      previewSql,
    });
    if (previewSql !== change.previewSql || previewHash !== change.previewHash) {
      throw new RecordsSchemaApprovalStaleError(
        "records GraphJin diff changed after approval; a fresh approval is required",
      );
    }
  }

  private async restoreReadiness(reason: string): Promise<void> {
    const blockers = await this.dependencies.pool.query<{ blocked: boolean }>(`
      select exists (
        select 1 from engine.app_schema_change where phase in ('applying', 'applied')
        union all
        select 1 from engine.record_app where status = 'degraded'
      ) as blocked
    `);
    const blocked = blockers.rows[0]?.blocked ?? true;
    await this.dependencies.setDataPlaneReady(
      !blocked,
      blocked ? "records schema reconciliation remains incomplete" : reason,
    );
  }

  async execute(actionRequestId: string): Promise<RecordSchemaChange> {
    const client = await this.dependencies.pool.connect();
    let change: RecordSchemaChange | null = null;
    try {
      await client.query("select pg_advisory_lock(hashtextextended($1, 0))", [
        RECORDS_SCHEMA_LOCK,
      ]);
      change = await loadChange(client, actionRequestId);
      if (!change) throw new Error(`unknown records schema action request: ${actionRequestId}`);
      // Fields live in each record's document, so only creating or archiving
      // a table changes the physical schema. Everything else is a registry
      // and policy update that the data plane serves without stopping.
      const physical =
        TABLE_ACTIONS.has(change.artifact.action) ||
        !(await verifyRecordsGraphjinDdlApplied(this.dependencies.pool, change.artifact.objects))
          .applied;
      if (physical) {
        await this.dependencies.setDataPlaneReady(
          false,
          `records schema projection ${actionRequestId}`,
        );
      }
      if (change.phase === "projected") {
        await this.restoreReadiness("records schema already projected");
        return change;
      }
      if (!["approved", "applying", "applied"].includes(change.phase)) {
        throw new RecordsSchemaPhaseError(
          `records schema change cannot execute from ${change.phase}`,
        );
      }
      const storedHash = recordsSchemaApprovalHash({
        desiredRevision: change.desiredRevision,
        catalogRevision: change.catalogRevision,
        graphjinDdl: change.artifact.document,
        previewSql: change.previewSql,
      });
      if (storedHash !== change.previewHash) {
        throw new RecordsSchemaApprovalStaleError(
          "records schema journal bytes do not match their approval hash",
        );
      }

      if (change.phase === "approved") {
        await this.verifyFinalPreview(change);
        change = await this.markPhase(client, change, "applying", {
          incrementAttempts: true,
        });
      }

      if (change.phase === "applying") {
        let verification = await verifyRecordsGraphjinDdlApplied(
          this.dependencies.pool,
          change.artifact.objects,
        );
        if (!verification.applied) {
          await this.verifyFinalPreview(change);
          await this.dependencies.workspace(
            { actionRequestId: change.actionRequestId, ddl: change.artifact.document },
            (directory) => this.dependencies.cli.sync(directory),
          );
          verification = await verifyRecordsGraphjinDdlApplied(
            this.dependencies.pool,
            change.artifact.objects,
          );
          if (!verification.applied) {
            throw new Error(
              `records GraphJin sync did not produce the desired catalog: ${verification.problems.join("; ")}`,
            );
          }
        }
        for (const object of change.artifact.objects.filter((candidate) => !candidate.archived)) {
          await ensureRecordsAuditTrigger(this.dependencies.pool, {
            tableSchema: validateRecordIdentifier("public"),
            tableName: object.tableName,
            appId: object.appId,
            objectApiName: object.apiName,
          });
        }
        change = await this.markPhase(client, change, "applied");
      }

      if (change.phase === "applied") {
        const verification = await verifyRecordsGraphjinDdlApplied(
          this.dependencies.pool,
          change.artifact.objects,
        );
        if (!verification.applied) {
          throw new Error(
            `records applied schema drifted before projection: ${verification.problems.join("; ")}`,
          );
        }
        for (const object of change.artifact.objects.filter((candidate) => !candidate.archived)) {
          await ensureRecordsAuditTrigger(this.dependencies.pool, {
            tableSchema: validateRecordIdentifier("public"),
            tableName: object.tableName,
            appId: object.appId,
            objectApiName: object.apiName,
          });
        }
        await this.dependencies.project(change);
        change = await this.markPhase(client, change, "projected");
      }
      await this.restoreReadiness("records schema projected");
      return change;
    } catch (error) {
      if (change) await this.failChange(client, change, error).catch(() => undefined);
      throw error;
    } finally {
      await client
        .query("select pg_advisory_unlock(hashtextextended($1, 0))", [RECORDS_SCHEMA_LOCK])
        .catch(() => undefined);
      client.release();
    }
  }

  async reconcilePending(limit = 25): Promise<{
    projected: string[];
    failed: Array<{ actionRequestId: string; error: string }>;
  }> {
    if (!Number.isSafeInteger(limit) || limit <= 0 || limit > 100) {
      throw new Error("records schema reconciliation limit must be between 1 and 100");
    }
    const pending = await this.dependencies.pool.query<{ action_request_id: string }>(
      `select action_request_id
       from engine.app_schema_change
       where phase in ('approved', 'applying', 'applied')
       order by updated_at, created_at
       limit $1`,
      [limit],
    );
    const projected: string[] = [];
    const failed: Array<{ actionRequestId: string; error: string }> = [];
    for (const row of pending.rows) {
      try {
        await this.execute(row.action_request_id);
        projected.push(row.action_request_id);
      } catch (error) {
        failed.push({
          actionRequestId: row.action_request_id,
          error: error instanceof Error ? error.message : String(error),
        });
      }
    }
    return { projected, failed };
  }
}
