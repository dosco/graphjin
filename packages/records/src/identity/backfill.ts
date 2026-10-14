import { createHash } from "node:crypto";
import type { Pool } from "pg";
import {
  claimRecordsActionExecution,
  renewRecordsActionExecutionLease,
  succeedRecordsActionExecution,
} from "../actions/execution";
import type { RecordsGraphjinTransport } from "../graphjin/client";
import { validateRecordIdentifier } from "../naming";
import { RECORD_DATA_COLUMN, RECORD_SYSTEM_COLUMNS } from "../policy/graphjin";
import { RecordRegistry } from "../registry";
import type { AppRegistrySnapshot, RecordField, RecordObject } from "../types";
import { listIdentityMappings, type IdentityMapping } from "./map";

const QUERY_PAGE_SIZE = 500;
const MUTATION_BATCH_SIZE = 50;

export type RecordOwnerBackfillRequest = {
  actionRequestId: string;
  orgId: string;
  appId: string;
  sourceInstanceId: string;
  actorUserId: string;
  sourceUserId?: string;
};

export type RecordOwnerBackfillReport = {
  actionRequestId: string;
  appId: string;
  sourceInstanceId: string;
  sourceUserId: string | null;
  mappings: number;
  objects: number;
  scanned: number;
  updated: number;
  unchanged: number;
  skippedObjects: Array<{ objectApiName: string; reason: string }>;
  replayed: boolean;
};

export class RecordOwnerBackfillTargetError extends Error {
  readonly code = "records_owner_backfill_target_invalid";

  constructor(message: string) {
    super(message);
    this.name = "RecordOwnerBackfillTargetError";
  }
}

export class RecordOwnerBackfillAuditError extends Error {
  readonly code = "records_owner_backfill_audit_missing";

  constructor() {
    super("owner backfill mutation returned without complete audit receipts");
    this.name = "RecordOwnerBackfillAuditError";
  }
}

type BackfillObject = {
  object: RecordObject;
  ownerField: RecordField;
  sourceInstanceField: RecordField | null;
};

type Candidate = {
  id: string;
  mutationId: string;
  sourceUserId: string;
  appUserId: string;
};

function required(value: string | undefined, label: string): string {
  if (typeof value !== "string" || !value.trim() || value.trim().length > 500) {
    throw new RecordOwnerBackfillTargetError(`${label}: non-empty string required`);
  }
  return value.trim();
}

function resultRows(data: Record<string, unknown>, key: string): Array<Record<string, unknown>> {
  const value = data[key];
  return Array.isArray(value)
    ? value.filter(
        (row): row is Record<string, unknown> =>
          typeof row === "object" && row !== null && !Array.isArray(row),
      )
    : [];
}

function mutationId(input: {
  request: RecordOwnerBackfillRequest;
  object: RecordObject;
  mapping: IdentityMapping;
  recordId: string;
}): string {
  return createHash("sha256")
    .update(
      [
        input.request.actionRequestId,
        input.object.apiName,
        input.mapping.sourceInstanceId,
        input.mapping.sourceUserId,
        input.mapping.appUserId,
        input.recordId,
      ].join("\u0000"),
      "utf8",
    )
    .digest("hex");
}

function backfillObjects(snapshot: AppRegistrySnapshot): BackfillObject[] {
  return snapshot.objects
    .filter((object) => object.archivedAt === null && object.visibility === "owner")
    .flatMap((object): BackfillObject[] => {
      const fields = snapshot.fields.filter(
        (field) => field.objectId === object.id && field.archivedAt === null,
      );
      const ownerField = fields.find(
        (field) =>
          field.sourceApiName?.toLocaleLowerCase("en-US") === "ownerid" ||
          field.apiName === "owner_id",
      );
      if (!ownerField) return [];
      const sourceInstanceField =
        fields.find(
          (field) =>
            field.apiName === "source_instance_id" ||
            field.sourceApiName?.toLocaleLowerCase("en-US") ===
              "opennekosourceinstanceid",
        ) ?? null;
      return [{ object, ownerField, sourceInstanceField }];
    });
}

async function auditedMutationIds(
  pool: Pool,
  input: { actionRequestId: string; mutationIds: string[] },
): Promise<Set<string>> {
  if (input.mutationIds.length === 0) return new Set();
  const result = await pool.query<{ mutation_id: string }>(
    `select mutation_id from engine.record_change_log
     where action_request_id = $1 and mutation_id = any($2::text[])`,
    [input.actionRequestId, input.mutationIds],
  );
  return new Set(result.rows.map((row) => row.mutation_id));
}

export class RecordOwnerBackfillExecutor {
  private readonly registry: RecordRegistry;

  constructor(
    private readonly dependencies: {
      pool: Pool;
      graphjin: RecordsGraphjinTransport;
      serviceToken: (orgId: string) => Promise<string> | string;
      leaseOwner: string;
      registry?: RecordRegistry;
    },
  ) {
    this.registry = dependencies.registry ?? new RecordRegistry(dependencies.pool);
  }

  private async updateBatch(input: {
    request: RecordOwnerBackfillRequest;
    target: BackfillObject;
    candidates: Candidate[];
    token: string;
  }): Promise<number> {
    if (input.candidates.length === 0) return 0;
    await renewRecordsActionExecutionLease(this.dependencies.pool, {
      actionRequestId: input.request.actionRequestId,
      leaseOwner: this.dependencies.leaseOwner,
      leaseMs: 5 * 60_000,
    });
    const aliases: string[] = [];
    const variables: Record<string, unknown> = {};
    for (const [index, candidate] of input.candidates.entries()) {
      const where: string[] = [
        `id: { eq: $id_${index} }`,
        `${RECORD_DATA_COLUMN}: { ${input.target.ownerField.columnName}: { eq: $source_user_${index} } }`,
        `${RECORD_SYSTEM_COLUMNS.deletedAt}: { is_null: true }`,
      ];
      if (input.target.sourceInstanceField) {
        where.push(
          `${RECORD_DATA_COLUMN}: { ${input.target.sourceInstanceField.columnName}: { eq: $source_instance_${index} } }`,
        );
        variables[`source_instance_${index}`] = input.request.sourceInstanceId;
      }
      aliases.push(
        `row_${index}: ${input.target.object.tableName}(update: $data_${index}, where: { ${where.join(", ")} }) { id }`,
      );
      variables[`id_${index}`] = candidate.id;
      variables[`source_user_${index}`] = candidate.sourceUserId;
      variables[`data_${index}`] = {
        [RECORD_SYSTEM_COLUMNS.ownerUserId]: candidate.appUserId,
        [RECORD_SYSTEM_COLUMNS.updatedBy]: input.request.actorUserId,
        [RECORD_SYSTEM_COLUMNS.actionRequestId]: input.request.actionRequestId,
        [RECORD_SYSTEM_COLUMNS.mutationId]: candidate.mutationId,
      };
    }
    const operationName = "RecordsOwnerBackfillBatch";
    await this.dependencies.graphjin.execute<Record<string, unknown>>({
      operationName,
      query: `mutation ${operationName} { ${aliases.join(" ")} }`,
      variables,
      token: input.token,
    });
    const audited = await auditedMutationIds(this.dependencies.pool, {
      actionRequestId: input.request.actionRequestId,
      mutationIds: input.candidates.map((candidate) => candidate.mutationId),
    });
    if (audited.size !== input.candidates.length) {
      throw new RecordOwnerBackfillAuditError();
    }
    return audited.size;
  }

  private async backfillMapping(input: {
    request: RecordOwnerBackfillRequest;
    target: BackfillObject;
    mapping: IdentityMapping;
    token: string;
  }): Promise<{ scanned: number; updated: number; unchanged: number }> {
    if (!input.mapping.appUserId) return { scanned: 0, updated: 0, unchanged: 0 };
    let after: string | null = null;
    let scanned = 0;
    let updated = 0;
    let unchanged = 0;
    do {
      const where = [
        `${RECORD_DATA_COLUMN}: { ${input.target.ownerField.columnName}: { eq: $source_user_id } }`,
        ...(after ? ["id: { gt: $after }"] : []),
        ...(input.target.sourceInstanceField
          ? [
              `${RECORD_DATA_COLUMN}: { ${input.target.sourceInstanceField.columnName}: { eq: $source_instance_id } }`,
            ]
          : []),
      ];
      const operationName = "RecordsOwnerBackfillPage";
      const data: Record<string, unknown> = await this.dependencies.graphjin.execute<
        Record<string, unknown>
      >({
        operationName,
        query: `query ${operationName} { rows: ${input.target.object.tableName}(where: { ${where.join(", ")} }, order_by: { id: asc }, limit: ${QUERY_PAGE_SIZE}) { id owner_user_id } }`,
        variables: {
          source_user_id: input.mapping.sourceUserId,
          ...(after ? { after } : {}),
          ...(input.target.sourceInstanceField
            ? { source_instance_id: input.request.sourceInstanceId }
            : {}),
        },
        token: input.token,
      });
      const rows: Array<Record<string, unknown> & { id: string }> = resultRows(
        data,
        "rows",
      ).filter(
        (row): row is Record<string, unknown> & { id: string } =>
          typeof row.id === "string" && row.id.length > 0,
      );
      scanned += rows.length;
      const candidates = rows
        .filter((row) => row.owner_user_id !== input.mapping.appUserId)
        .map(
          (row): Candidate => ({
            id: row.id,
            sourceUserId: input.mapping.sourceUserId,
            appUserId: input.mapping.appUserId!,
            mutationId: mutationId({
              request: input.request,
              object: input.target.object,
              mapping: input.mapping,
              recordId: row.id,
            }),
          }),
        );
      unchanged += rows.length - candidates.length;
      for (let index = 0; index < candidates.length; index += MUTATION_BATCH_SIZE) {
        updated += await this.updateBatch({
          request: input.request,
          target: input.target,
          candidates: candidates.slice(index, index + MUTATION_BATCH_SIZE),
          token: input.token,
        });
      }
      after = rows.length === QUERY_PAGE_SIZE ? rows.at(-1)!.id : null;
    } while (after);
    return { scanned, updated, unchanged };
  }

  async execute(rawRequest: RecordOwnerBackfillRequest): Promise<RecordOwnerBackfillReport> {
    const request: RecordOwnerBackfillRequest = {
      actionRequestId: required(rawRequest.actionRequestId, "action request id"),
      orgId: required(rawRequest.orgId, "organization id"),
      appId: validateRecordIdentifier(required(rawRequest.appId, "app id")),
      sourceInstanceId: required(rawRequest.sourceInstanceId, "source instance id"),
      actorUserId: required(rawRequest.actorUserId, "actor user id"),
      ...(rawRequest.sourceUserId
        ? { sourceUserId: required(rawRequest.sourceUserId, "source user id") }
        : {}),
    };
    const snapshot = await this.registry.loadApp(request.orgId, request.appId);
    if (!snapshot || snapshot.app.status !== "active") {
      throw new RecordOwnerBackfillTargetError("record app is missing or not active");
    }
    const allMappings = await listIdentityMappings(this.dependencies.pool, {
      orgId: request.orgId,
      appId: request.appId,
    });
    const mappings = allMappings.filter(
      (mapping) =>
        mapping.sourceInstanceId === request.sourceInstanceId &&
        mapping.status === "linked" &&
        mapping.appUserId !== null &&
        (!request.sourceUserId || mapping.sourceUserId === request.sourceUserId),
    );
    const distinctSources = new Set(allMappings.map((mapping) => mapping.sourceInstanceId));
    const targets = backfillObjects(snapshot);
    const skippedObjects = snapshot.objects
      .filter((object) => object.archivedAt === null && object.visibility === "owner")
      .filter((object) => !targets.some((target) => target.object.id === object.id))
      .map((object) => ({
        objectApiName: object.apiName,
        reason: "No imported source owner field is registered.",
      }));
    const safeTargets = targets.filter((target) => {
      if (distinctSources.size <= 1 || target.sourceInstanceField) return true;
      skippedObjects.push({
        objectApiName: target.object.apiName,
        reason:
          "Multiple source instances share this app, but the object has no source-instance provenance field.",
      });
      return false;
    });
    const claim = await claimRecordsActionExecution(this.dependencies.pool, {
      actionRequestId: request.actionRequestId,
      orgId: request.orgId,
      appId: request.appId,
      actionKind: "record_sync",
      leaseOwner: this.dependencies.leaseOwner,
      leaseMs: 5 * 60_000,
    });
    if (claim.kind === "replay") {
      return {
        ...(claim.result as Omit<RecordOwnerBackfillReport, "replayed">),
        replayed: true,
      };
    }

    const report: RecordOwnerBackfillReport = {
      actionRequestId: request.actionRequestId,
      appId: request.appId,
      sourceInstanceId: request.sourceInstanceId,
      sourceUserId: request.sourceUserId ?? null,
      mappings: mappings.length,
      objects: safeTargets.length,
      scanned: 0,
      updated: 0,
      unchanged: 0,
      skippedObjects,
      replayed: false,
    };
    const token = await this.dependencies.serviceToken(request.orgId);
    for (const target of safeTargets) {
      for (const mapping of mappings) {
        const result = await this.backfillMapping({ request, target, mapping, token });
        report.scanned += result.scanned;
        report.updated += result.updated;
        report.unchanged += result.unchanged;
      }
    }
    const durableUpdates = await this.dependencies.pool.query<{ updated: string }>(
      `select count(*)::text as updated from engine.record_change_log
       where action_request_id = $1 and org_id = $2 and app_id = $3`,
      [request.actionRequestId, request.orgId, request.appId],
    );
    report.updated = Number.parseInt(durableUpdates.rows[0]?.updated ?? "0", 10);
    report.unchanged = Math.max(0, report.scanned - report.updated);
    await succeedRecordsActionExecution(this.dependencies.pool, {
      actionRequestId: request.actionRequestId,
      leaseOwner: this.dependencies.leaseOwner,
      result: report,
    });
    return report;
  }
}
