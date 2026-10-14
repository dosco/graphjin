import { createHash } from "node:crypto";
import type { Pool } from "pg";
import {
  claimRecordsActionExecution,
  failRecordsActionExecution,
  renewRecordsActionExecutionLease,
  setRecordsActionExecutionContext,
  succeedRecordsActionExecution,
} from "../actions/execution";
import {
  RecordsGraphjinRequestError,
  type RecordsGraphjinTransport,
} from "../graphjin/client";
import { RECORD_DATA_COLUMN, RECORD_SYSTEM_COLUMNS } from "../policy/graphjin";
import { normalizeSalesforceId } from "../connect/salesforce/id";
import { RecordRegistry } from "../registry";
import type { RecordField, RecordObject } from "../types";
import {
  RecordValidationError,
  validateRecordFields,
} from "../write/validate";
import { parseRecordsCsv, type CsvRecord } from "./csv";
import {
  verifyRecordImportPlanHash,
  type RecordImportColumnPlan,
  type RecordImportPlan,
} from "./plan";
import {
  finishRecordImportRun,
  finishRecordImportCancellation,
  getRecordImportReceipt,
  getRecordImportReceiptAtBatch,
  getRecordImportRun,
  recordEmptyImportBatch,
  recordRecordImportRejects,
  startRecordImportRun,
  summarizeRecordImportRun,
  updateRecordImportProgress,
  type RecordImportRejectInput,
  type RecordImportRun,
} from "./store";

export type PreparedRecordImportRow = {
  rowNumber: number;
  rowHash: string;
  sourceRow: Record<string, string>;
  id: string;
  fields: Record<string, unknown>;
  duplicateValue: unknown;
};

export type PreparedRecordImportCandidate =
  | { kind: "row"; row: PreparedRecordImportRow }
  | { kind: "reject"; reject: RecordImportRejectInput };

export type RecordImportReport = {
  status: "succeeded" | "cancelled";
  importRunId: string;
  appId: string;
  objectApiName: string;
  sourceName: string;
  sourceRows: number;
  inserted: number;
  rejected: number;
  duplicates: number;
  batches: number;
  reconciled: boolean;
};

export class RecordImportTerminalError extends Error {
  readonly code = "records_import_terminal";

  constructor(message: string, options: { cause?: unknown } = {}) {
    super(message, options);
    this.name = "RecordImportTerminalError";
  }
}

class RecordImportCancelledError extends Error {
  constructor() {
    super("records import was cancelled");
    this.name = "RecordImportCancelledError";
  }
}

function sha256(value: string | Uint8Array): string {
  return createHash("sha256").update(value).digest("hex");
}

function deterministicRecordId(plan: RecordImportPlan, rowNumber: number): string {
  const hex = sha256(
    `${plan.source.sha256}\u0000${plan.appId}\u0000${plan.objectApiName}\u0000${rowNumber}`,
  ).slice(0, 32);
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-4${hex.slice(13, 16)}-a${hex.slice(
    17,
    20,
  )}-${hex.slice(20)}`;
}

function rowHash(plan: RecordImportPlan, row: CsvRecord): string {
  return sha256(
    JSON.stringify({
      format: "openneko.records.csv-row.v1",
      plan: plan.planHash,
      row: row.rowNumber,
      values: row.values,
    }),
  );
}

function mutationId(actionRequestId: string, row: PreparedRecordImportRow): string {
  return sha256(`${actionRequestId}\u0000${row.rowNumber}\u0000${row.rowHash}`);
}

function batchHash(
  plan: RecordImportPlan,
  candidates: PreparedRecordImportCandidate[],
): string {
  return sha256(
    JSON.stringify({
      format: "openneko.records.csv-batch.v1",
      plan: plan.planHash,
      rows: candidates.map((candidate) =>
        candidate.kind === "row" ? candidate.row.rowHash : candidate.reject.rowHash,
      ),
    }),
  );
}

function sourceRow(
  plan: RecordImportPlan,
  headers: string[],
  row: CsvRecord,
): Record<string, string> {
  return Object.fromEntries(headers.map((header, index) => [header, row.values[index] ?? ""]));
}

function booleanValue(value: string): boolean {
  if (/^(?:true|yes|1)$/i.test(value)) return true;
  if (/^(?:false|no|0)$/i.test(value)) return false;
  throw new Error("must be true, false, yes, no, 1, or 0");
}

function mappedValue(raw: string, column: RecordImportColumnPlan): unknown {
  if (raw === "") {
    return column.emptyText ? "" : null;
  }
  if (column.sourceId === true) return normalizeSalesforceId(raw);
  switch (column.targetKind) {
    case "boolean":
      return booleanValue(raw.trim());
    case "integer": {
      const value = Number(raw.trim());
      if (!Number.isSafeInteger(value)) throw new Error("must be a safe integer");
      return value;
    }
    case "multipicklist": {
      const trimmed = raw.trim();
      if (trimmed.startsWith("[")) {
        const parsed = JSON.parse(trimmed) as unknown;
        if (!Array.isArray(parsed)) throw new Error("must be a JSON array or semicolon list");
        return parsed;
      }
      return trimmed.split(";").map((entry) => entry.trim()).filter(Boolean);
    }
    default:
      return raw;
  }
}

function rejectFor(
  objectApiName: string,
  rowNumber: number,
  hash: string,
  source: Record<string, string>,
  reasonCode: string,
  reason: string,
): RecordImportRejectInput {
  return {
    objectApiName,
    rowNumber,
    rowHash: hash,
    reasonCode,
    reason: reason.slice(0, 2_000),
    sourceRow: source,
  };
}

function prepareCandidate(input: {
  plan: RecordImportPlan;
  headers: string[];
  row: CsvRecord;
  fields: RecordField[];
}): PreparedRecordImportCandidate {
  const source = sourceRow(input.plan, input.headers, input.row);
  const hash = rowHash(input.plan, input.row);
  try {
    const mapped: Record<string, unknown> = {};
    for (const [index, column] of input.plan.columns.entries()) {
      if (!column.targetField) continue;
      mapped[column.targetField] = mappedValue(input.row.values[index] ?? "", column);
    }
    const explicitId = Object.prototype.hasOwnProperty.call(mapped, "id");
    const idValue = explicitId ? mapped.id : deterministicRecordId(input.plan, input.row.rowNumber);
    if (typeof idValue !== "string" || !idValue.trim() || idValue.length > 512) {
      throw new Error("id must be a non-empty string no longer than 512 characters");
    }
    const id = idValue.trim();
    delete mapped.id;
    const validated = validateRecordFields({
      values: mapped,
      registryFields: input.fields,
      operation: "create",
      allowReadOnly: input.plan.allowReadOnly === true,
    });
    const duplicateValue =
      input.plan.duplicateKey === "id"
        ? id
        : validated.fields[input.plan.duplicateKey];
    if (duplicateValue === null || duplicateValue === undefined || duplicateValue === "") {
      throw new Error(`duplicate key ${input.plan.duplicateKey} cannot be empty`);
    }
    return {
      kind: "row",
      row: {
        rowNumber: input.row.rowNumber,
        rowHash: hash,
        sourceRow: source,
        id,
        fields: validated.fields,
        duplicateValue,
      },
    };
  } catch (error) {
    const reason =
      error instanceof RecordValidationError
        ? error.issues.map((issue) => `${issue.field}: ${issue.message}`).join("; ")
        : error instanceof Error
          ? error.message
          : String(error);
    return {
      kind: "reject",
      reject: rejectFor(
        input.plan.objectApiName,
        input.row.rowNumber,
        hash,
        source,
        "validation",
        reason,
      ),
    };
  }
}

/**
 * Rebuild the exact deterministic row/reject candidates bound by an approved
 * plan. The executor and migration fixtures share this function so their
 * reconciliation evidence cannot drift from the write path.
 */
export function prepareRecordImportSource(input: {
  plan: RecordImportPlan;
  source: Uint8Array;
  fields: RecordField[];
}): {
  headers: string[];
  candidates: PreparedRecordImportCandidate[];
} {
  verifyRecordImportPlanHash(input.plan);
  if (
    input.source.byteLength !== input.plan.source.bytes ||
    sha256(input.source) !== input.plan.source.sha256
  ) {
    throw new RecordImportTerminalError("import source changed after approval");
  }
  let parsed;
  try {
    parsed = parseRecordsCsv(
      new TextDecoder("utf-8", { fatal: true }).decode(input.source),
    );
  } catch (error) {
    throw new RecordImportTerminalError("approved CSV can no longer be parsed", {
      cause: error,
    });
  }
  if (parsed.rows.length !== input.plan.rowCount) {
    throw new RecordImportTerminalError("import row count changed after approval");
  }
  if (
    parsed.headers.length !== input.plan.columns.length ||
    parsed.headers.some(
      (header, index) => header !== input.plan.columns[index]?.sourceColumn,
    )
  ) {
    throw new RecordImportTerminalError("import headers changed after approval");
  }
  return {
    headers: parsed.headers,
    candidates: parsed.rows.map((row) =>
      prepareCandidate({
        plan: input.plan,
        headers: parsed.headers,
        row,
        fields: input.fields,
      }),
    ),
  };
}

/**
 * Rebuild the human-reviewed preview rows without reopening the staged CSV.
 * `sampleRows` and every mapping are covered by the approved plan hash, so the
 * final import validator can compare a deterministic target sample after the
 * worker has finished loading the object.
 */
export function prepareRecordImportSamples(input: {
  plan: RecordImportPlan;
  fields: RecordField[];
}): PreparedRecordImportCandidate[] {
  verifyRecordImportPlanHash(input.plan);
  const headers = input.plan.columns.map((column) => column.sourceColumn);
  return input.plan.sampleRows.map((sample, index) =>
    prepareCandidate({
      plan: input.plan,
      headers,
      row: {
        rowNumber: index + 1,
        values: headers.map((header) => sample[header] ?? ""),
      },
      fields: input.fields,
    }),
  );
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

function comparable(value: unknown): string {
  return `${typeof value}:${JSON.stringify(value)}`;
}

function retryableGraphjinError(error: unknown): boolean {
  if (!(error instanceof RecordsGraphjinRequestError)) return false;
  return (
    error.status === null ||
    error.status === 408 ||
    error.status === 429 ||
    (error.status !== null && error.status >= 500) ||
    (error.status === 200 && error.graphjinErrors.length === 0)
  );
}

export class RecordImportExecutor {
  private readonly registry: RecordRegistry;

  constructor(
    private readonly dependencies: {
      pool: Pool;
      graphjin: RecordsGraphjinTransport;
      serviceToken: (orgId: string) => Promise<string> | string;
      leaseOwner: string;
      readSource: (orgId: string, path: string) => Promise<Uint8Array>;
      registry?: RecordRegistry;
    },
  ) {
    this.registry = dependencies.registry ?? new RecordRegistry(dependencies.pool);
  }

  private async assertNotCancelled(run: RecordImportRun): Promise<void> {
    const current = await getRecordImportRun(this.dependencies.pool, {
      orgId: run.orgId,
      id: run.id,
    });
    if (!current || current.status === "cancelled" || current.cancelRequestedAt) {
      throw new RecordImportCancelledError();
    }
  }

  private async existingDuplicateValues(input: {
    plan: RecordImportPlan;
    object: RecordObject;
    rows: PreparedRecordImportRow[];
    token: string;
  }): Promise<Set<string>> {
    if (input.rows.length === 0) return new Set();
    const operationName = "RecordsImportDuplicateCheck";
    const field = input.plan.duplicateKey;
    const inDocument = field !== "id";
    // Keys inside the document compare as text, so the candidates do too.
    const data = await this.dependencies.graphjin.execute<Record<string, unknown>>({
      operationName,
      query: inDocument
        ? `query ${operationName} { rows: ${input.object.tableName}(where: { ${RECORD_DATA_COLUMN}: { ${field}: { in: $keys } } }, limit: ${input.rows.length}) { id ${RECORD_DATA_COLUMN} } }`
        : `query ${operationName} { rows: ${input.object.tableName}(where: { id: { in: $keys } }, limit: ${input.rows.length}) { id } }`,
      variables: { keys: input.rows.map((row) => (inDocument ? String(row.duplicateValue) : row.duplicateValue)) },
      token: input.token,
    });
    return new Set(
      resultRows(data, "rows").map((row) => {
        const document = row[RECORD_DATA_COLUMN];
        return comparable(
          inDocument && document && typeof document === "object"
            ? (document as Record<string, unknown>)[field]
            : row[field],
        );
      }),
    );
  }

  private async processChunk(input: {
    run: RecordImportRun;
    object: RecordObject;
    candidates: PreparedRecordImportCandidate[];
    actorUserId: string;
    token: string;
    leaseOwner: string;
  }): Promise<void> {
    if (input.candidates.length === 0) return;
    await this.assertNotCancelled(input.run);
    await renewRecordsActionExecutionLease(this.dependencies.pool, {
      actionRequestId: input.run.actionRequestId,
      leaseOwner: input.leaseOwner,
      leaseMs: 5 * 60_000,
    });
    const hash = batchHash(input.run.plan, input.candidates);
    const batchNo =
      (input.candidates[0]!.kind === "row"
        ? input.candidates[0]!.row.rowNumber
        : input.candidates[0]!.reject.rowNumber) - 1;
    const exactReceipt = await getRecordImportReceipt(this.dependencies.pool, {
      runId: input.run.id,
      objectApiName: input.run.plan.objectApiName,
      batchHash: hash,
    });
    if (exactReceipt) return;

    const occupied = await getRecordImportReceiptAtBatch(this.dependencies.pool, {
      runId: input.run.id,
      objectApiName: input.run.plan.objectApiName,
      batchNo,
    });
    if (occupied) {
      if (input.candidates.length === 1) {
        throw new Error("records import resume found a conflicting batch identity");
      }
      const midpoint = Math.ceil(input.candidates.length / 2);
      await this.processChunk({ ...input, candidates: input.candidates.slice(0, midpoint) });
      await this.processChunk({ ...input, candidates: input.candidates.slice(midpoint) });
      return;
    }

    const staticRejects = input.candidates
      .filter((candidate): candidate is Extract<PreparedRecordImportCandidate, { kind: "reject" }> =>
        candidate.kind === "reject",
      )
      .map((candidate) => candidate.reject);
    const validRows = input.candidates
      .filter((candidate): candidate is Extract<PreparedRecordImportCandidate, { kind: "row" }> =>
        candidate.kind === "row",
      )
      .map((candidate) => candidate.row);
    await recordRecordImportRejects(this.dependencies.pool, {
      runId: input.run.id,
      rejects: staticRejects,
    });

    let existing: Set<string>;
    try {
      existing = await this.existingDuplicateValues({
        plan: input.run.plan,
        object: input.object,
        rows: validRows,
        token: input.token,
      });
    } catch (error) {
      if (retryableGraphjinError(error)) throw error;
      throw new RecordImportTerminalError("duplicate detection query failed", { cause: error });
    }
    const seen = new Set<string>();
    const duplicateRejects: RecordImportRejectInput[] = [];
    const insertable = validRows.filter((row) => {
      const key = comparable(row.duplicateValue);
      if (!existing.has(key) && !seen.has(key)) {
        seen.add(key);
        return true;
      }
      duplicateRejects.push(
        rejectFor(
          input.run.plan.objectApiName,
          row.rowNumber,
          row.rowHash,
          row.sourceRow,
          "duplicate",
          `${input.run.plan.duplicateKey} already exists`,
        ),
      );
      return false;
    });
    const allRejects = [...staticRejects, ...duplicateRejects];
    if (insertable.length === 0) {
      await recordEmptyImportBatch(this.dependencies.pool, {
        runId: input.run.id,
        objectApiName: input.run.plan.objectApiName,
        batchNo,
        batchHash: hash,
        rejects: allRejects,
      });
      return;
    }
    await recordRecordImportRejects(this.dependencies.pool, {
      runId: input.run.id,
      rejects: duplicateRejects,
    });

    await setRecordsActionExecutionContext(this.dependencies.pool, {
      actionRequestId: input.run.actionRequestId,
      leaseOwner: input.leaseOwner,
      leaseMs: 5 * 60_000,
      context: {
        import_batch: {
          run_id: input.run.id,
          object_api_name: input.run.plan.objectApiName,
          batch_no: batchNo,
          batch_hash: hash,
        },
      },
    });
    const rows = insertable.map((row) => ({
      id: row.id,
      [RECORD_DATA_COLUMN]: row.fields,
      ...(input.object.visibility === "owner"
        ? {
            [RECORD_SYSTEM_COLUMNS.ownerUserId]:
              input.run.plan.ownerUserId ?? input.actorUserId,
          }
        : {}),
      [RECORD_SYSTEM_COLUMNS.createdBy]: input.actorUserId,
      [RECORD_SYSTEM_COLUMNS.updatedBy]: input.actorUserId,
      [RECORD_SYSTEM_COLUMNS.actionRequestId]: input.run.actionRequestId,
      [RECORD_SYSTEM_COLUMNS.mutationId]: mutationId(input.run.actionRequestId, row),
    }));
    try {
      const operationName = "RecordsImportBatch";
      await this.dependencies.graphjin.execute<Record<string, unknown>>({
        operationName,
        query: `mutation ${operationName} { inserted: ${input.object.tableName}(insert: $rows) { id } }`,
        variables: { rows },
        token: input.token,
      });
      const receipt = await getRecordImportReceipt(this.dependencies.pool, {
        runId: input.run.id,
        objectApiName: input.run.plan.objectApiName,
        batchHash: hash,
      });
      if (!receipt || receipt.rowCount !== insertable.length) {
        throw new Error("records import mutation returned without its complete batch receipt");
      }
    } catch (error) {
      const committed = await getRecordImportReceipt(this.dependencies.pool, {
        runId: input.run.id,
        objectApiName: input.run.plan.objectApiName,
        batchHash: hash,
      });
      if (committed?.rowCount === insertable.length) return;
      if (retryableGraphjinError(error)) throw error;
      if (insertable.length === 1) {
        await recordEmptyImportBatch(this.dependencies.pool, {
          runId: input.run.id,
          objectApiName: input.run.plan.objectApiName,
          batchNo,
          batchHash: hash,
          rejects: [
            rejectFor(
              input.run.plan.objectApiName,
              insertable[0]!.rowNumber,
              insertable[0]!.rowHash,
              insertable[0]!.sourceRow,
              "database",
              error instanceof Error ? error.message : String(error),
            ),
          ],
        });
        return;
      }
      const midpoint = Math.ceil(input.candidates.length / 2);
      await this.processChunk({ ...input, candidates: input.candidates.slice(0, midpoint) });
      await this.processChunk({ ...input, candidates: input.candidates.slice(midpoint) });
    }
  }

  async execute(input: {
    orgId: string;
    importRunId: string;
    actorUserId: string;
    /** Stable pg-boss job identity so the same job can resume after restart. */
    leaseOwner?: string;
  }): Promise<RecordImportReport> {
    const leaseOwner = input.leaseOwner ?? this.dependencies.leaseOwner;
    let run = await startRecordImportRun(this.dependencies.pool, {
      orgId: input.orgId,
      id: input.importRunId,
    });
    if (run.status === "cancelled") {
      const report: RecordImportReport = {
        status: "cancelled",
        importRunId: run.id,
        appId: run.appId,
        objectApiName: run.plan.objectApiName,
        sourceName: run.plan.source.name,
        sourceRows: run.plan.rowCount,
        inserted: 0,
        rejected: 0,
        duplicates: 0,
        batches: 0,
        reconciled: false,
      };
      const claim = await claimRecordsActionExecution(this.dependencies.pool, {
        actionRequestId: run.actionRequestId,
        orgId: run.orgId,
        appId: run.appId,
        actionKind: "records_import_start",
        leaseOwner,
        leaseMs: 5 * 60_000,
      });
      if (claim.kind === "replay") return claim.result as RecordImportReport;
      run = await finishRecordImportCancellation(this.dependencies.pool, {
        orgId: run.orgId,
        id: run.id,
        report,
      });
      await succeedRecordsActionExecution(this.dependencies.pool, {
        actionRequestId: run.actionRequestId,
        leaseOwner,
        result: report,
      });
      return report;
    }
    verifyRecordImportPlanHash(run.plan);
    const snapshot = await this.registry.loadApp(run.orgId, run.appId);
    if (!snapshot || snapshot.app.status !== "active") {
      throw new RecordImportTerminalError("import app is missing or not active");
    }
    const object = snapshot.objects.find(
      (candidate) =>
        candidate.apiName === run.plan.objectApiName && candidate.archivedAt === null,
    );
    if (!object || object.tableName !== run.plan.tableName) {
      throw new RecordImportTerminalError("import object changed after approval");
    }
    const fields = snapshot.fields.filter(
      (field) => field.objectId === object.id && field.archivedAt === null,
    );
    let source: Uint8Array;
    try {
      source = await this.dependencies.readSource(run.orgId, run.plan.source.path);
    } catch (error) {
      throw new RecordImportTerminalError("approved import source is unavailable", {
        cause: error,
      });
    }
    const prepared = prepareRecordImportSource({
      plan: run.plan,
      source,
      fields,
    });

    const claim = await claimRecordsActionExecution(this.dependencies.pool, {
      actionRequestId: run.actionRequestId,
      orgId: run.orgId,
      appId: run.appId,
      actionKind: "records_import_start",
      leaseOwner,
      leaseMs: 5 * 60_000,
    });
    if (claim.kind === "replay") return claim.result as RecordImportReport;
    const token = await this.dependencies.serviceToken(run.orgId);
    try {
      const candidates = prepared.candidates;
      for (let index = 0; index < candidates.length; index += run.plan.batchSize) {
        const chunk = candidates.slice(index, index + run.plan.batchSize);
        await this.processChunk({
          run,
          object,
          candidates: chunk,
          actorUserId: input.actorUserId,
          token,
          leaseOwner,
        });
        const summary = await summarizeRecordImportRun(this.dependencies.pool, run.id);
        await updateRecordImportProgress(this.dependencies.pool, {
          orgId: run.orgId,
          id: run.id,
          stage: "load",
          progress: {
            processed: Math.min(index + chunk.length, candidates.length),
            total: candidates.length,
            ...summary,
          },
        });
      }
      const summary = await summarizeRecordImportRun(this.dependencies.pool, run.id);
      const report: RecordImportReport = {
        status: "succeeded",
        importRunId: run.id,
        appId: run.appId,
        objectApiName: run.plan.objectApiName,
        sourceName: run.plan.source.name,
        sourceRows: run.plan.rowCount,
        ...summary,
        reconciled: summary.inserted + summary.rejected === run.plan.rowCount,
      };
      if (!report.reconciled) {
        throw new Error("records import row reconciliation failed");
      }
      run = await finishRecordImportRun(this.dependencies.pool, {
        orgId: run.orgId,
        id: run.id,
        report,
      });
      await succeedRecordsActionExecution(this.dependencies.pool, {
        actionRequestId: run.actionRequestId,
        leaseOwner,
        result: report,
      });
      return report;
    } catch (error) {
      if (error instanceof RecordImportCancelledError) {
        const summary = await summarizeRecordImportRun(this.dependencies.pool, run.id);
        const report: RecordImportReport = {
          status: "cancelled",
          importRunId: run.id,
          appId: run.appId,
          objectApiName: run.plan.objectApiName,
          sourceName: run.plan.source.name,
          sourceRows: run.plan.rowCount,
          ...summary,
          reconciled: false,
        };
        run = await finishRecordImportCancellation(this.dependencies.pool, {
          orgId: run.orgId,
          id: run.id,
          report,
        });
        await succeedRecordsActionExecution(this.dependencies.pool, {
          actionRequestId: run.actionRequestId,
          leaseOwner,
          result: report,
        });
        return report;
      }
      if (error instanceof RecordImportTerminalError) {
        await failRecordsActionExecution(this.dependencies.pool, {
          actionRequestId: run.actionRequestId,
          leaseOwner,
          error: { code: error.code, message: error.message },
        });
      }
      throw error;
    }
  }
}
