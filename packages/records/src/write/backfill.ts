import { createHash } from "node:crypto";
import type { Pool } from "pg";
import {
  claimRecordsActionExecution,
  failRecordsActionExecution,
  renewRecordsActionExecutionLease,
  succeedRecordsActionExecution,
} from "../actions/execution";
import type { RecordsGraphjinTransport } from "../graphjin/client";
import { validateRecordIdentifier } from "../naming";
import {
  evaluateRecordObjectPermission,
  selectRecordPolicyGrant,
} from "../policy/evaluate";
import { RECORD_DATA_COLUMN, RECORD_SYSTEM_COLUMNS } from "../policy/graphjin";
import { RecordRegistry } from "../registry";
import type { RecordField, RecordObject } from "../types";
import { RecordValidationError, validateRecordFields } from "./validate";

const QUERY_PAGE_SIZE = 500;
const MUTATION_BATCH_SIZE = 50;
const LEASE_MS = 5 * 60_000;

export const RECORD_BACKFILL_TRANSFORMS = [
  "copy",
  "string",
  "integer",
  "decimal",
  "boolean",
  "date",
  "datetime",
] as const;

export type RecordBackfillTransform =
  (typeof RECORD_BACKFILL_TRANSFORMS)[number];

type RecordBackfillCommon = {
  actionRequestId: string;
  orgId: string;
  appId: string;
  objectApiName: string;
  targetFieldApiName: string;
  actorUserId: string;
};

export type RecordBackfillRequest = RecordBackfillCommon &
  (
    | {
        sourceFieldApiName: string;
        transform?: RecordBackfillTransform;
        value?: never;
      }
    | {
        value: unknown;
        sourceFieldApiName?: never;
        transform?: never;
      }
  );

export type RecordBackfillReport = {
  actionRequestId: string;
  appId: string;
  objectApiName: string;
  targetFieldApiName: string;
  sourceFieldApiName: string | null;
  transform: RecordBackfillTransform | null;
  scanned: number;
  updated: number;
  skippedNullSource: number;
  remainingNull: number;
  replayed: boolean;
};

export class RecordBackfillTargetError extends Error {
  readonly code = "records_backfill_target_invalid";

  constructor(message: string) {
    super(message);
    this.name = "RecordBackfillTargetError";
  }
}

export class RecordBackfillPermissionError extends Error {
  readonly code = "records_backfill_permission_denied";

  constructor() {
    super("record backfill requires update permission on the target object");
    this.name = "RecordBackfillPermissionError";
  }
}

export class RecordBackfillValueError extends Error {
  readonly code = "records_backfill_value_invalid";

  constructor(
    readonly recordId: string,
    readonly targetFieldApiName: string,
    message: string,
  ) {
    super(`${recordId}.${targetFieldApiName}: ${message}`);
    this.name = "RecordBackfillValueError";
  }
}

export class RecordBackfillAuditError extends Error {
  readonly code = "records_backfill_audit_missing";

  constructor() {
    super("record backfill mutation returned without complete audit receipts");
    this.name = "RecordBackfillAuditError";
  }
}

type ResolvedBackfill = {
  object: RecordObject;
  fields: RecordField[];
  target: RecordField;
  source: RecordField | null;
  transform: RecordBackfillTransform | null;
  constantValue?: unknown;
};

type BackfillCandidate = {
  id: string;
  value: unknown;
  mutationId: string;
};

function required(value: string, label: string): string {
  if (typeof value !== "string" || !value.trim() || value.trim().length > 500) {
    throw new RecordBackfillTargetError(`${label}: non-empty string required`);
  }
  return value.trim();
}

function isTransform(value: unknown): value is RecordBackfillTransform {
  return (
    typeof value === "string" &&
    RECORD_BACKFILL_TRANSFORMS.includes(value as RecordBackfillTransform)
  );
}

function transformSourceValue(
  value: unknown,
  transform: RecordBackfillTransform,
): unknown {
  if (value === null) return null;
  switch (transform) {
    case "copy":
      return value;
    case "string":
      if (
        typeof value !== "string" &&
        typeof value !== "number" &&
        typeof value !== "boolean"
      ) {
        throw new Error("cannot convert this source value to a string");
      }
      if (typeof value === "number" && !Number.isFinite(value)) {
        throw new Error("cannot convert a non-finite number to a string");
      }
      return String(value);
    case "integer": {
      if (
        (typeof value !== "string" && typeof value !== "number") ||
        !/^-?\d+$/.test(String(value))
      ) {
        throw new Error("cannot convert this source value to an integer");
      }
      const parsed = Number(value);
      if (!Number.isSafeInteger(parsed)) {
        throw new Error("converted integer is outside the safe range");
      }
      return parsed;
    }
    case "decimal":
      if (
        (typeof value !== "string" && typeof value !== "number") ||
        !/^-?\d+(?:\.\d+)?$/.test(String(value))
      ) {
        throw new Error("cannot convert this source value to a decimal");
      }
      return value;
    case "boolean":
      if (typeof value === "boolean") return value;
      if (value === "true") return true;
      if (value === "false") return false;
      throw new Error("boolean conversion accepts only true or false");
    case "date": {
      if (typeof value !== "string") {
        throw new Error("date conversion requires a string source value");
      }
      const parsed = new Date(value);
      if (Number.isNaN(parsed.getTime())) {
        throw new Error("cannot convert this source value to a date");
      }
      const converted = parsed.toISOString().slice(0, 10);
      if (/^\d{4}-\d{2}-\d{2}$/.test(value) && converted !== value) {
        throw new Error("cannot convert an invalid calendar date");
      }
      return converted;
    }
    case "datetime": {
      if (typeof value !== "string") {
        throw new Error("datetime conversion requires a string source value");
      }
      const parsed = new Date(value);
      if (Number.isNaN(parsed.getTime())) {
        throw new Error("cannot convert this source value to a datetime");
      }
      return parsed.toISOString();
    }
  }
}

function resultRows(
  data: Record<string, unknown>,
  key: string,
): Array<Record<string, unknown>> {
  const value = data[key];
  return Array.isArray(value)
    ? value.filter(
        (row): row is Record<string, unknown> =>
          typeof row === "object" && row !== null && !Array.isArray(row),
      )
    : [];
}

function mutationId(input: {
  request: RecordBackfillRequest;
  object: RecordObject;
  target: RecordField;
  recordId: string;
  value: unknown;
}): string {
  return createHash("sha256")
    .update(
      [
        input.request.actionRequestId,
        input.object.apiName,
        input.target.apiName,
        input.recordId,
        JSON.stringify(input.value),
      ].join("\u0000"),
      "utf8",
    )
    .digest("hex");
}

function terminalError(
  error: unknown,
): error is
  | RecordBackfillTargetError
  | RecordBackfillPermissionError
  | RecordBackfillValueError
  | RecordValidationError {
  return (
    error instanceof RecordBackfillTargetError ||
    error instanceof RecordBackfillPermissionError ||
    error instanceof RecordBackfillValueError ||
    error instanceof RecordValidationError
  );
}

export class RecordBackfillExecutor {
  private readonly registry: RecordRegistry;

  constructor(
    private readonly dependencies: {
      pool: Pool;
      graphjin: RecordsGraphjinTransport;
      serviceToken: (orgId: string) => Promise<string> | string;
      leaseOwner: string;
      recordSourceWrite: (input: {
        actionRequestId: string;
        orgId: string;
        tableName: string;
        recordId: string;
      }) => Promise<void>;
      assertWritesAllowed?: () => Promise<void>;
      registry?: RecordRegistry;
    },
  ) {
    this.registry = dependencies.registry ?? new RecordRegistry(dependencies.pool);
  }

  private validatedValue(
    resolved: ResolvedBackfill,
    rawValue: unknown,
    recordId: string,
  ): unknown {
    let value = rawValue;
    try {
      if (resolved.transform) {
        value = transformSourceValue(value, resolved.transform);
      }
      const validated = validateRecordFields({
        values: { [resolved.target.apiName]: value },
        registryFields: resolved.fields,
        operation: "update",
      });
      return validated.fields[resolved.target.columnName];
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "backfill value is invalid";
      throw new RecordBackfillValueError(
        recordId,
        resolved.target.apiName,
        message,
      );
    }
  }

  private async resolve(request: RecordBackfillRequest): Promise<ResolvedBackfill> {
    const snapshot = await this.registry.loadApp(request.orgId, request.appId);
    if (!snapshot || snapshot.app.status !== "active") {
      throw new RecordBackfillTargetError("record app is missing or not active");
    }
    const objectApiName = validateRecordIdentifier(request.objectApiName);
    const object = snapshot.objects.find(
      (candidate) =>
        candidate.apiName === objectApiName && candidate.archivedAt === null,
    );
    if (!object) {
      throw new RecordBackfillTargetError(
        "record object is unknown or archived",
      );
    }
    const fields = snapshot.fields.filter(
      (field) => field.objectId === object.id && field.archivedAt === null,
    );
    const targetApiName = validateRecordIdentifier(request.targetFieldApiName);
    const target = fields.find((field) => field.apiName === targetApiName);
    if (!target || target.kind === "id" || target.readOnly) {
      throw new RecordBackfillTargetError(
        "target field is unknown, archived, or read-only",
      );
    }
    const grant = selectRecordPolicyGrant(snapshot.permissions, {
      appId: snapshot.app.appId,
      role: "admin",
      objectApiName: object.apiName,
    });
    if (
      !evaluateRecordObjectPermission({
        actor: { role: "admin" },
        object: {
          appId: object.appId,
          apiName: object.apiName,
          visibility: object.visibility,
          archived: false,
        },
        grant,
        operation: "update",
      })
    ) {
      throw new RecordBackfillPermissionError();
    }

    if (typeof request.sourceFieldApiName === "string") {
      const sourceApiName = validateRecordIdentifier(request.sourceFieldApiName);
      const source = fields.find((field) => field.apiName === sourceApiName);
      if (!source || source.kind === "id") {
        throw new RecordBackfillTargetError(
          "source field is unknown or archived",
        );
      }
      if (source.id === target.id) {
        throw new RecordBackfillTargetError(
          "source and target fields must be different",
        );
      }
      const transform = request.transform ?? "copy";
      if (!isTransform(transform)) {
        throw new RecordBackfillTargetError("backfill transform is unsupported");
      }
      return { object, fields, target, source, transform };
    }

    if (request.value === null || request.value === undefined) {
      throw new RecordBackfillTargetError(
        "constant backfill value must be non-null",
      );
    }
    const resolved: ResolvedBackfill = {
      object,
      fields,
      target,
      source: null,
      transform: null,
    };
    return {
      ...resolved,
      constantValue: this.validatedValue(
        resolved,
        request.value,
        "constant",
      ),
    };
  }

  private async auditedMutationIds(input: {
    request: RecordBackfillRequest;
    resolved: ResolvedBackfill;
    mutationIds: string[];
  }): Promise<Set<string>> {
    if (input.mutationIds.length === 0) return new Set();
    const result = await this.dependencies.pool.query<{ mutation_id: string }>(
      `select mutation_id from engine.record_change_log
       where action_request_id = $1 and org_id = $2 and app_id = $3
         and object_api_name = $4 and mutation_id = any($5::text[])`,
      [
        input.request.actionRequestId,
        input.request.orgId,
        input.request.appId,
        input.resolved.object.apiName,
        input.mutationIds,
      ],
    );
    return new Set(result.rows.map((row) => row.mutation_id));
  }

  private async updateBatch(input: {
    request: RecordBackfillRequest;
    resolved: ResolvedBackfill;
    candidates: BackfillCandidate[];
    token: string;
  }): Promise<void> {
    if (input.candidates.length === 0) return;
    await renewRecordsActionExecutionLease(this.dependencies.pool, {
      actionRequestId: input.request.actionRequestId,
      leaseOwner: this.dependencies.leaseOwner,
      leaseMs: LEASE_MS,
    });
    const aliases: string[] = [];
    const variables: Record<string, unknown> = {};
    for (const [index, candidate] of input.candidates.entries()) {
      aliases.push(
        `row_${index}: ${input.resolved.object.tableName}(update: $data_${index}, where: { id: { eq: $id_${index} }, ${RECORD_DATA_COLUMN}: { ${input.resolved.target.columnName}: { is_null: true } }, ${RECORD_SYSTEM_COLUMNS.deletedAt}: { is_null: true } }) { id }`,
      );
      variables[`id_${index}`] = candidate.id;
      variables[`data_${index}`] = {
        [RECORD_DATA_COLUMN]: { [input.resolved.target.columnName]: candidate.value },
        [RECORD_SYSTEM_COLUMNS.updatedBy]: input.request.actorUserId,
        [RECORD_SYSTEM_COLUMNS.actionRequestId]:
          input.request.actionRequestId,
        [RECORD_SYSTEM_COLUMNS.mutationId]: candidate.mutationId,
      };
    }
    const operationName = "RecordsFieldBackfillBatch";
    await this.dependencies.graphjin.execute<Record<string, unknown>>({
      operationName,
      query: `mutation ${operationName} { ${aliases.join(" ")} }`,
      variables,
      token: input.token,
    });
    const audited = await this.auditedMutationIds({
      request: input.request,
      resolved: input.resolved,
      mutationIds: input.candidates.map((candidate) => candidate.mutationId),
    });
    if (audited.size !== input.candidates.length) {
      throw new RecordBackfillAuditError();
    }
  }

  private async durableRecordIds(input: {
    request: RecordBackfillRequest;
    resolved: ResolvedBackfill;
  }): Promise<string[]> {
    const result = await this.dependencies.pool.query<{ record_id: string }>(
      `select distinct record_id from engine.record_change_log
       where action_request_id = $1 and org_id = $2 and app_id = $3
         and object_api_name = $4
       order by record_id`,
      [
        input.request.actionRequestId,
        input.request.orgId,
        input.request.appId,
        input.resolved.object.apiName,
      ],
    );
    return result.rows.map((row) => row.record_id);
  }

  private async countRemainingNull(input: {
    request: RecordBackfillRequest;
    resolved: ResolvedBackfill;
    token: string;
  }): Promise<number> {
    let after: string | null = null;
    let count = 0;
    do {
      await renewRecordsActionExecutionLease(this.dependencies.pool, {
        actionRequestId: input.request.actionRequestId,
        leaseOwner: this.dependencies.leaseOwner,
        leaseMs: LEASE_MS,
      });
      const where: string[] = [
        `${RECORD_DATA_COLUMN}: { ${input.resolved.target.columnName}: { is_null: true } }`,
        `${RECORD_SYSTEM_COLUMNS.deletedAt}: { is_null: true }`,
        ...(after ? ["id: { gt: $after }"] : []),
      ];
      const operationName = "RecordsFieldBackfillRemainingPage";
      const data: Record<string, unknown> =
        await this.dependencies.graphjin.execute<Record<string, unknown>>({
          operationName,
          query: `query ${operationName} { rows: ${input.resolved.object.tableName}(where: { ${where.join(", ")} }, order_by: { id: asc }, limit: ${QUERY_PAGE_SIZE}) { id } }`,
          variables: after ? { after } : {},
          token: input.token,
        });
      const rows = resultRows(data, "rows").filter(
        (row): row is Record<string, unknown> & { id: string } =>
          typeof row.id === "string" && row.id.length > 0,
      );
      count += rows.length;
      after = rows.length === QUERY_PAGE_SIZE ? rows.at(-1)!.id : null;
    } while (after);
    return count;
  }

  async execute(rawRequest: RecordBackfillRequest): Promise<RecordBackfillReport> {
    await this.dependencies.assertWritesAllowed?.();
    const common: RecordBackfillCommon = {
      actionRequestId: required(
        rawRequest.actionRequestId,
        "action request id",
      ),
      orgId: required(rawRequest.orgId, "organization id"),
      appId: validateRecordIdentifier(required(rawRequest.appId, "app id")),
      objectApiName: validateRecordIdentifier(
        required(rawRequest.objectApiName, "object api name"),
      ),
      targetFieldApiName: validateRecordIdentifier(
        required(rawRequest.targetFieldApiName, "target field api name"),
      ),
      actorUserId: required(rawRequest.actorUserId, "actor user id"),
    };
    const request: RecordBackfillRequest =
      typeof rawRequest.sourceFieldApiName === "string"
        ? {
            ...common,
            sourceFieldApiName: validateRecordIdentifier(
              required(rawRequest.sourceFieldApiName, "source field api name"),
            ),
            ...(rawRequest.transform
              ? { transform: rawRequest.transform }
              : {}),
          }
        : { ...common, value: rawRequest.value };
    const resolved = await this.resolve(request);
    const claim = await claimRecordsActionExecution(this.dependencies.pool, {
      actionRequestId: request.actionRequestId,
      orgId: request.orgId,
      appId: request.appId,
      actionKind: "record_backfill",
      leaseOwner: this.dependencies.leaseOwner,
      leaseMs: LEASE_MS,
    });
    if (claim.kind === "replay") {
      return {
        ...(claim.result as Omit<RecordBackfillReport, "replayed">),
        replayed: true,
      };
    }

    const report: RecordBackfillReport = {
      actionRequestId: request.actionRequestId,
      appId: request.appId,
      objectApiName: resolved.object.apiName,
      targetFieldApiName: resolved.target.apiName,
      sourceFieldApiName: resolved.source?.apiName ?? null,
      transform: resolved.transform,
      scanned: 0,
      updated: 0,
      skippedNullSource: 0,
      remainingNull: 0,
      replayed: false,
    };
    try {
      const token = await this.dependencies.serviceToken(request.orgId);
      let after: string | null = null;
      do {
        await renewRecordsActionExecutionLease(this.dependencies.pool, {
          actionRequestId: request.actionRequestId,
          leaseOwner: this.dependencies.leaseOwner,
          leaseMs: LEASE_MS,
        });
        const where: string[] = [
          `${RECORD_DATA_COLUMN}: { ${resolved.target.columnName}: { is_null: true } }`,
          `${RECORD_SYSTEM_COLUMNS.deletedAt}: { is_null: true }`,
          ...(after ? ["id: { gt: $after }"] : []),
        ];
        const operationName = "RecordsFieldBackfillPage";
        const data: Record<string, unknown> =
          await this.dependencies.graphjin.execute<Record<string, unknown>>({
            operationName,
            query: `query ${operationName} { rows: ${resolved.object.tableName}(where: { ${where.join(", ")} }, order_by: { id: asc }, limit: ${QUERY_PAGE_SIZE}) { id${resolved.source ? ` ${RECORD_DATA_COLUMN}` : ""} } }`,
            variables: after ? { after } : {},
            token,
          });
        const rows: Array<Record<string, unknown> & { id: string }> =
          resultRows(data, "rows").filter(
            (row): row is Record<string, unknown> & { id: string } =>
              typeof row.id === "string" && row.id.length > 0,
          );
        report.scanned += rows.length;
        const candidates: BackfillCandidate[] = [];
        for (const row of rows) {
          const document = row[RECORD_DATA_COLUMN];
          const rawValue = resolved.source
            ? document && typeof document === "object" && !Array.isArray(document)
              ? (document as Record<string, unknown>)[resolved.source.columnName]
              : undefined
            : resolved.constantValue;
          if (rawValue === null || rawValue === undefined) {
            report.skippedNullSource += 1;
            continue;
          }
          const value = this.validatedValue(resolved, rawValue, row.id);
          candidates.push({
            id: row.id,
            value,
            mutationId: mutationId({
              request,
              object: resolved.object,
              target: resolved.target,
              recordId: row.id,
              value,
            }),
          });
        }
        for (
          let index = 0;
          index < candidates.length;
          index += MUTATION_BATCH_SIZE
        ) {
          await this.updateBatch({
            request,
            resolved,
            candidates: candidates.slice(index, index + MUTATION_BATCH_SIZE),
            token,
          });
        }
        after = rows.length === QUERY_PAGE_SIZE ? rows.at(-1)!.id : null;
      } while (after);

      const recordIds = await this.durableRecordIds({ request, resolved });
      for (const recordId of recordIds) {
        await this.dependencies.recordSourceWrite({
          actionRequestId: request.actionRequestId,
          orgId: request.orgId,
          tableName: resolved.object.tableName,
          recordId,
        });
      }
      report.updated = recordIds.length;
      report.remainingNull = await this.countRemainingNull({
        request,
        resolved,
        token,
      });
      await succeedRecordsActionExecution(this.dependencies.pool, {
        actionRequestId: request.actionRequestId,
        leaseOwner: this.dependencies.leaseOwner,
        result: report,
      });
      return report;
    } catch (error) {
      if (terminalError(error)) {
        await failRecordsActionExecution(this.dependencies.pool, {
          actionRequestId: request.actionRequestId,
          leaseOwner: this.dependencies.leaseOwner,
          error: {
            code: error.code,
            message: error.message,
            at: new Date().toISOString(),
          },
        });
      }
      throw error;
    }
  }
}
