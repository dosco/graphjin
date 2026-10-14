import { createHash } from "node:crypto";
import type { Pool } from "pg";
import {
  bumpRegistryRevision,
  prepareRecordImportSamples,
  RecordRegistry,
  type AppRegistrySnapshot,
  type PreparedRecordImportRow,
  type RecordField,
  type RecordImportPlan,
  type RecordObject,
  type RecordsGraphjinTransport,
} from "@neko/records";

const PAGE_SIZE = 500;
const DANGLING_SAMPLE_LIMIT = 20;

type JsonRecord = Record<string, unknown>;

type ImportRunProjection = {
  id: string;
  plan: RecordImportPlan;
  report: JsonRecord;
};

export type ArtifactImportRowValidation = {
  objectApiName: string;
  expected: number;
  inserted: number;
  rejected: number;
  duplicates: number;
  live: number;
  reconciled: boolean;
};

export type ArtifactImportChecksumValidation = {
  objectApiName: string;
  requested: number;
  rejected: number;
  verified: number;
  missing: number;
  mismatched: number;
  expectedSha256: string;
  actualSha256: string;
  matched: boolean;
};

export type ArtifactImportDanglingReferenceValidation = {
  objectApiName: string;
  fieldApiName: string;
  targetObjectApiNames: string[];
  populated: number;
  dangling: number;
  sampleValues: string[];
};

export type ArtifactImportValidationReport = {
  format: "openneko.records.artifact-import-validation.v1";
  integrity: "passed" | "failed";
  rows: ArtifactImportRowValidation[];
  sampledChecksums: ArtifactImportChecksumValidation[];
  danglingReferences: ArtifactImportDanglingReferenceValidation[];
  unmatchedUsers: number | null;
  permissionCollapse: {
    strategy: "admin_member";
    source: "salesforce_v1_mode_defaults";
    roles: Array<{
      role: string;
      objects: number;
      readable: number;
      creatable: number;
      updatable: number;
      deletable: number;
    }>;
    note: string;
  };
  recordCountsRefreshed: boolean;
};

type RegistryReader = {
  loadApp(orgId: string, appId: string): Promise<AppRegistrySnapshot | null>;
};

function records(value: unknown): JsonRecord[] {
  return Array.isArray(value)
    ? value.filter(
        (entry): entry is JsonRecord =>
          typeof entry === "object" && entry !== null && !Array.isArray(entry),
      )
    : [];
}

/** Fields live in each row's data document; read them as top-level keys. */
function flattenDocument(row: JsonRecord): JsonRecord {
  const document = row.data;
  return {
    ...(document && typeof document === "object" && !Array.isArray(document)
      ? (document as JsonRecord)
      : {}),
    id: row.id,
  };
}

function integer(value: unknown, label: string): number {
  const parsed = typeof value === "number" ? value : Number(value);
  if (!Number.isSafeInteger(parsed) || parsed < 0) {
    throw new Error(`artifact import validation received an invalid ${label}`);
  }
  return parsed;
}

function canonicalJson(value: unknown): string {
  if (value === null || typeof value !== "object") return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(canonicalJson).join(",")}]`;
  return `{${Object.entries(value as JsonRecord)
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([key, entry]) => `${JSON.stringify(key)}:${canonicalJson(entry)}`)
    .join(",")}}`;
}

function checksum(value: unknown): string {
  return createHash("sha256").update(canonicalJson(value), "utf8").digest("hex");
}

function canonicalDecimal(value: unknown): unknown {
  if (typeof value !== "number" && typeof value !== "string") return value;
  const source = String(value);
  if (!/^-?\d+(?:\.\d+)?$/.test(source)) return source;
  const negative = source.startsWith("-");
  const unsigned = negative ? source.slice(1) : source;
  const [wholeSource, fractionSource = ""] = unsigned.split(".");
  const whole = wholeSource!.replace(/^0+(?=\d)/, "");
  const fraction = fractionSource.replace(/0+$/, "");
  const normalized = fraction ? `${whole}.${fraction}` : whole;
  return negative && normalized !== "0" ? `-${normalized}` : normalized;
}

function canonicalValue(value: unknown, field: RecordField | undefined): unknown {
  if (value === undefined) return null;
  if (!field) return value;
  if (["decimal", "currency", "percent"].includes(field.kind)) {
    return canonicalDecimal(value);
  }
  if (field.kind === "integer") {
    const parsed = Number(value);
    return Number.isSafeInteger(parsed) ? parsed : value;
  }
  if (field.kind === "datetime" && typeof value === "string") {
    const parsed = new Date(value);
    return Number.isNaN(parsed.getTime()) ? value : parsed.toISOString();
  }
  return value;
}

function canonicalSample(
  row: JsonRecord,
  columns: string[],
  fieldsByColumn: ReadonlyMap<string, RecordField>,
): JsonRecord {
  return Object.fromEntries(
    ["id", ...columns].map((column) => [
      column,
      canonicalValue(row[column], fieldsByColumn.get(column)),
    ]),
  );
}

function countFrom(value: unknown): number {
  const first = records(value)[0];
  return integer(first?.count_id ?? first?.count ?? 0, "GraphJin row count");
}

function objectFields(snapshot: AppRegistrySnapshot, object: RecordObject): RecordField[] {
  return snapshot.fields.filter(
    (field) => field.objectId === object.id && field.archivedAt === null,
  );
}

async function liveCount(input: {
  graphjin: RecordsGraphjinTransport;
  token: string;
  object: RecordObject;
}): Promise<number> {
  const result = await input.graphjin.execute<JsonRecord>({
    operationName: "RecordsImportValidationCount",
    query: `query RecordsImportValidationCount { totals: ${input.object.tableName} { count_id } }`,
    token: input.token,
  });
  return countFrom(result.totals);
}

async function rejectedSampleRows(
  pool: Pool,
  runIds: string[],
): Promise<Set<string>> {
  const result = await pool.query<{ import_run_id: string; row_number: number }>(
    `select import_run_id::text, row_number
     from engine.import_reject
     where import_run_id = any($1::uuid[]) and row_number <= 5`,
    [runIds],
  );
  return new Set(result.rows.map((row) => `${row.import_run_id}\u0000${row.row_number}`));
}

async function validateSample(input: {
  graphjin: RecordsGraphjinTransport;
  token: string;
  run: ImportRunProjection;
  object: RecordObject;
  fields: RecordField[];
  rejectedRows: ReadonlySet<string>;
}): Promise<ArtifactImportChecksumValidation> {
  const candidates = prepareRecordImportSamples({
    plan: input.run.plan,
    fields: input.fields,
  });
  const expectedRows = candidates.flatMap((candidate): PreparedRecordImportRow[] => {
    if (candidate.kind !== "row") return [];
    return input.rejectedRows.has(`${input.run.id}\u0000${candidate.row.rowNumber}`)
      ? []
      : [candidate.row];
  });
  const rejected = candidates.length - expectedRows.length;
  const columns = [
    ...new Set(expectedRows.flatMap((row) => Object.keys(row.fields))),
  ].sort();
  let actualRows: JsonRecord[] = [];
  if (expectedRows.length > 0) {
    const result: JsonRecord = await input.graphjin.execute<JsonRecord>({
      operationName: "RecordsImportValidationSample",
      query: `query RecordsImportValidationSample { rows: ${input.object.tableName}(where: { id: { in: $ids } }, limit: ${expectedRows.length}) { id data } }`,
      variables: { ids: expectedRows.map((row) => row.id) },
      token: input.token,
    });
    actualRows = records(result.rows).map(flattenDocument);
  }
  const fieldsByColumn = new Map(input.fields.map((field) => [field.columnName, field]));
  const actualById = new Map(
    actualRows
      .filter((row): row is JsonRecord & { id: string } => typeof row.id === "string")
      .map((row) => [row.id, row]),
  );
  const expected = expectedRows
    .map((row) => canonicalSample({ id: row.id, ...row.fields }, columns, fieldsByColumn))
    .sort((left, right) => String(left.id).localeCompare(String(right.id)));
  const actual = expectedRows
    .map((row) => {
      const found = actualById.get(row.id);
      return found ? canonicalSample(found, columns, fieldsByColumn) : null;
    })
    .sort((left, right) =>
      String(left?.id ?? "").localeCompare(String(right?.id ?? "")),
    );
  let missing = 0;
  let mismatched = 0;
  for (const row of expected) {
    const found = actualById.get(String(row.id));
    if (!found) {
      missing += 1;
      continue;
    }
    if (checksum(row) !== checksum(canonicalSample(found, columns, fieldsByColumn))) {
      mismatched += 1;
    }
  }
  const expectedSha256 = checksum(expected);
  const actualSha256 = checksum(actual);
  return {
    objectApiName: input.object.apiName,
    requested: candidates.length,
    rejected,
    verified: expected.length - missing - mismatched,
    missing,
    mismatched,
    expectedSha256,
    actualSha256,
    matched: expectedSha256 === actualSha256 && missing === 0 && mismatched === 0,
  };
}

async function existingTargetIds(input: {
  graphjin: RecordsGraphjinTransport;
  token: string;
  object: RecordObject;
  values: string[];
}): Promise<Set<string>> {
  if (input.values.length === 0) return new Set();
  const result = await input.graphjin.execute<JsonRecord>({
    operationName: "RecordsImportReferenceTargets",
    query: `query RecordsImportReferenceTargets { rows: ${input.object.tableName}(where: { id: { in: $ids } }, limit: ${input.values.length}) { id } }`,
    variables: { ids: input.values },
    token: input.token,
  });
  return new Set(
    records(result.rows).flatMap((row) => (typeof row.id === "string" ? [row.id] : [])),
  );
}

async function validateObjectReferences(input: {
  graphjin: RecordsGraphjinTransport;
  token: string;
  snapshot: AppRegistrySnapshot;
  object: RecordObject;
  fields: RecordField[];
  expectedRows: number;
}): Promise<ArtifactImportDanglingReferenceValidation[]> {
  const referenceFields = input.fields.filter(
    (field) => field.kind === "reference" && (field.referenceTargets?.length ?? 0) > 0,
  );
  const summaries = new Map(
    referenceFields.map((field) => [
      field.id,
      {
        objectApiName: String(input.object.apiName),
        fieldApiName: String(field.apiName),
        targetObjectApiNames: [...(field.referenceTargets ?? [])].map(String),
        populated: 0,
        dangling: 0,
        sampleValues: [] as string[],
      },
    ]),
  );
  if (referenceFields.length === 0 || input.expectedRows === 0) {
    return [...summaries.values()];
  }
  let scanned = 0;
  let after: string | null = null;
  const cursors = new Set<string>();
  while (scanned < input.expectedRows) {
    const result = await input.graphjin.execute<JsonRecord>({
      operationName: "RecordsImportReferenceScan",
      query: `query RecordsImportReferenceScan($first: Int!${after ? ", $after: String!" : ""}) { rows: ${input.object.tableName}(first: $first${after ? ", after: $after" : ""}, order_by: { id: asc }) { id data } ${input.object.tableName}_cursor }`,
      variables: { first: PAGE_SIZE, ...(after ? { after } : {}) },
      token: input.token,
    });
    const page = records(result.rows).map(flattenDocument);
    if (page.length === 0) break;
    scanned += page.length;
    for (const field of referenceFields) {
      const values = page.flatMap((row) =>
        typeof row[field.columnName] === "string" && row[field.columnName]
          ? [String(row[field.columnName])]
          : [],
      );
      const uniqueValues = [...new Set(values)];
      const existing = new Set<string>();
      for (const targetName of field.referenceTargets ?? []) {
        const target = input.snapshot.objects.find(
          (candidate) => candidate.apiName === targetName && candidate.archivedAt === null,
        );
        if (!target) continue;
        for (const id of await existingTargetIds({
          graphjin: input.graphjin,
          token: input.token,
          object: target,
          values: uniqueValues,
        })) {
          existing.add(id);
        }
      }
      const summary = summaries.get(field.id)!;
      summary.populated += values.length;
      for (const value of values) {
        if (existing.has(value)) continue;
        summary.dangling += 1;
        if (
          summary.sampleValues.length < DANGLING_SAMPLE_LIMIT &&
          !summary.sampleValues.includes(value)
        ) {
          summary.sampleValues.push(value);
        }
      }
    }
    if (scanned >= input.expectedRows) break;
    const cursorValue: unknown =
      result.rows_cursor ?? result[`${input.object.tableName}_cursor`];
    if (typeof cursorValue !== "string" || !cursorValue || cursors.has(cursorValue)) {
      throw new Error(
        `artifact import reference validation stopped before reading ${input.object.apiName}`,
      );
    }
    cursors.add(cursorValue);
    after = cursorValue;
  }
  if (scanned !== input.expectedRows) {
    throw new Error(
      `artifact import reference validation read ${scanned} of ${input.expectedRows} ${input.object.apiName} rows`,
    );
  }
  return [...summaries.values()];
}

function permissionCollapse(snapshot: AppRegistrySnapshot) {
  const roles = [...new Set(snapshot.permissions.map((permission) => permission.role))].sort();
  return {
    strategy: "admin_member" as const,
    source: "salesforce_v1_mode_defaults" as const,
    roles: roles.map((role) => {
      const grants = snapshot.permissions.filter((permission) => permission.role === role);
      return {
        role,
        objects: grants.length,
        readable: grants.filter((grant) => grant.canRead).length,
        creatable: grants.filter((grant) => grant.canCreate).length,
        updatable: grants.filter((grant) => grant.canUpdate).length,
        deletable: grants.filter((grant) => grant.canDelete).length,
      };
    }),
    note:
      "Salesforce v1 artifacts do not retain named profiles; reviewed connector mode defaults are collapsed to the admin and member grants recorded here.",
  };
}

async function refreshRecordCounts(input: {
  pool: Pool;
  orgId: string;
  appId: string;
  rows: ArtifactImportRowValidation[];
}): Promise<void> {
  const client = await input.pool.connect();
  try {
    await client.query("begin");
    let changed = false;
    for (const row of input.rows) {
      const result = await client.query(
        `update engine.record_object
         set record_count = $4, updated_at = now()
         where org_id = $1 and app_id = $2 and api_name = $3
           and record_count is distinct from $4`,
        [input.orgId, input.appId, row.objectApiName, row.live],
      );
      changed = changed || result.rowCount === 1;
    }
    if (changed) await bumpRegistryRevision(client, input.orgId);
    await client.query("commit");
  } catch (error) {
    await client.query("rollback");
    throw error;
  } finally {
    client.release();
  }
}

/**
 * Execute the artifact import's Stage 6 checks through the service GraphJin
 * policy, then refresh only engine-owned registry metadata.
 */
export async function validateArtifactImport(input: {
  pool: Pool;
  orgId: string;
  appId: string;
  runIds: string[];
  graphjin: RecordsGraphjinTransport;
  serviceToken: string;
  registry?: RegistryReader;
}): Promise<ArtifactImportValidationReport> {
  const registry = input.registry ?? new RecordRegistry(input.pool);
  const snapshot = await registry.loadApp(input.orgId, input.appId);
  if (!snapshot) throw new Error("artifact import validation cannot load the app registry");
  const runResult = await input.pool.query<ImportRunProjection>(
    `select id::text, result->'plan' as plan, result->'report' as report
     from engine.import_run
     where org_id = $1 and id = any($2::uuid[]) and status = 'succeeded'
     order by id`,
    [input.orgId, input.runIds],
  );
  if (runResult.rows.length !== input.runIds.length) {
    throw new Error("artifact import validation is missing a successful object run");
  }
  const rejectedRows = await rejectedSampleRows(input.pool, input.runIds);
  const rowReports: ArtifactImportRowValidation[] = [];
  const sampledChecksums: ArtifactImportChecksumValidation[] = [];
  const danglingReferences: ArtifactImportDanglingReferenceValidation[] = [];
  for (const run of runResult.rows) {
    const object = snapshot.objects.find(
      (candidate) =>
        candidate.apiName === run.plan.objectApiName && candidate.archivedAt === null,
    );
    if (!object || object.tableName !== run.plan.tableName) {
      throw new Error(`artifact import validation cannot resolve ${run.plan.objectApiName}`);
    }
    const fields = objectFields(snapshot, object);
    const live = await liveCount({
      graphjin: input.graphjin,
      token: input.serviceToken,
      object,
    });
    const expected = integer(run.report.sourceRows, "source row count");
    const inserted = integer(run.report.inserted, "inserted row count");
    const rejected = integer(run.report.rejected, "rejected row count");
    const duplicates = integer(run.report.duplicates, "duplicate row count");
    rowReports.push({
      objectApiName: String(object.apiName),
      expected,
      inserted,
      rejected,
      duplicates,
      live,
      reconciled:
        run.report.reconciled === true &&
        expected === run.plan.rowCount &&
        inserted + rejected === expected &&
        live === inserted,
    });
    sampledChecksums.push(
      await validateSample({
        graphjin: input.graphjin,
        token: input.serviceToken,
        run,
        object,
        fields,
        rejectedRows,
      }),
    );
    danglingReferences.push(
      ...(await validateObjectReferences({
        graphjin: input.graphjin,
        token: input.serviceToken,
        snapshot,
        object,
        fields,
        expectedRows: live,
      })),
    );
  }
  rowReports.sort((left, right) => left.objectApiName.localeCompare(right.objectApiName));
  sampledChecksums.sort((left, right) =>
    left.objectApiName.localeCompare(right.objectApiName),
  );
  danglingReferences.sort((left, right) =>
    `${left.objectApiName}.${left.fieldApiName}`.localeCompare(
      `${right.objectApiName}.${right.fieldApiName}`,
    ),
  );
  await refreshRecordCounts({
    pool: input.pool,
    orgId: input.orgId,
    appId: input.appId,
    rows: rowReports,
  });
  const integrity =
    rowReports.every((row) => row.reconciled) &&
    sampledChecksums.every((sample) => sample.matched)
      ? "passed"
      : "failed";
  return {
    format: "openneko.records.artifact-import-validation.v1",
    integrity,
    rows: rowReports,
    sampledChecksums,
    danglingReferences,
    unmatchedUsers: null,
    permissionCollapse: permissionCollapse(snapshot),
    recordCountsRefreshed: true,
  };
}
