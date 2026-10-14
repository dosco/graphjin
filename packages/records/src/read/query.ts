import { validateRecordIdentifier } from "../naming";
import { RECORD_DATA_COLUMN, RECORD_SYSTEM_COLUMNS } from "../policy/graphjin";
import {
  evaluateRecordObjectPermission,
  selectRecordPolicyGrant,
} from "../policy/evaluate";
import type {
  AppRegistrySnapshot,
  JsonObject,
  RecordField,
  RecordFieldKind,
  RecordObject,
  RecordPermission,
} from "../types";

const DEFAULT_PAGE_SIZE = 50;
const MAX_PAGE_SIZE = 100;
const MAX_CURSOR_LENGTH = 4_096;
const MAX_SEARCH_LENGTH = 200;

export type RecordViewerRole = "admin" | "member";

export type RecordViewColumn = {
  apiName: string;
  columnName: string;
  label: string;
  kind: RecordFieldKind | "owner" | "system_datetime";
  required: boolean;
  readOnly: boolean;
  picklistValues: unknown[] | null;
  referenceTargets: string[] | null;
  scale: number | null;
};

export type RecordObjectView = {
  object: RecordObject;
  permission: RecordPermission;
  columns: RecordViewColumn[];
};

export type RecordFilterOperator =
  | "eq"
  | "neq"
  | "in"
  | "not_in"
  | "contains"
  | "starts_with"
  | "is_null"
  | "gt"
  | "gte"
  | "lt"
  | "lte"
  | "relative"
  | "is_open"
  | "is_closed";

export type RecordListFilter = {
  field: string;
  operator: RecordFilterOperator;
  value?: unknown;
};

export type RecordFilterExpression =
  | RecordListFilter
  | {
      op: "and" | "or";
      clauses: RecordFilterExpression[];
    };

export type RecordListQuery = {
  operationName: "RecordsList";
  query: string;
  variables: Record<string, unknown>;
  view: RecordObjectView;
  cursorField: string;
  resultField: string;
  totalField: string;
};

export type RecordDetailQuery = {
  operationName: "RecordsDetail";
  query: string;
  variables: { record_id: string };
  view: RecordObjectView;
  resultField: string;
};

export type RecordReferenceLabelQuery = {
  operationName: "RecordsReferenceLabels";
  query: string;
  variables: { record_ids: string[] };
  view: RecordObjectView;
  resultField: "rows";
};

export type RecordAggregateQuery = {
  operationName: "RecordsMetric";
  query: string;
  variables: Record<string, unknown>;
  view: RecordObjectView;
  resultField: "metric";
  valueField: string;
};

export type RecordRecycleListQuery = {
  operationName: "RecordsRecycleList";
  query: string;
  variables: Record<string, unknown>;
  view: RecordObjectView;
  cursorField: string;
  resultField: string;
  totalField: string;
};

export type RecordRecycleDetailQuery = {
  operationName: "RecordsRecycleDetail";
  query: string;
  variables: {
    app_id: string;
    object_api_name: string;
    record_id: string;
  };
  view: RecordObjectView;
  resultField: string;
};

export type RecordChangeLogQuery = {
  operationName: "RecordsChangeLog";
  query: string;
  variables: {
    app_id: string;
    object_api_name: string;
    record_id: string;
    first: number;
  };
  resultField: "history";
};

export type RecordSchemaLogQuery = {
  operationName: "RecordsSchemaLog";
  query: string;
  variables: { app_id: string; first: number };
  resultField: "history";
};

export class RecordReadTargetError extends Error {
  readonly code = "records_read_target_invalid";

  constructor(message: string) {
    super(message);
    this.name = "RecordReadTargetError";
  }
}

export class RecordReadPermissionError extends Error {
  readonly code = "records_read_permission_denied";

  constructor() {
    super("record object is not readable by this actor");
    this.name = "RecordReadPermissionError";
  }
}

function readIdentifier(value: string, label: string): ReturnType<typeof validateRecordIdentifier> {
  try {
    return validateRecordIdentifier(value);
  } catch {
    throw new RecordReadTargetError(`${label} is not a valid record identifier`);
  }
}

function objectFields(snapshot: AppRegistrySnapshot, object: RecordObject): RecordField[] {
  return snapshot.fields.filter(
    (field) => field.objectId === object.id && field.archivedAt === null,
  );
}

function permissionFor(
  snapshot: AppRegistrySnapshot,
  object: RecordObject,
  role: RecordViewerRole,
): RecordPermission {
  const permission = selectRecordPolicyGrant(snapshot.permissions, {
    appId: snapshot.app.appId,
    objectApiName: object.apiName,
    role,
  });
  if (
    !permission ||
    !evaluateRecordObjectPermission({
      actor: { role },
      object: {
        appId: object.appId,
        apiName: object.apiName,
        visibility: object.visibility,
        archived: object.archivedAt !== null,
      },
      grant: permission,
      operation: "read",
    })
  ) {
    throw new RecordReadPermissionError();
  }
  return permission;
}

function layoutFieldNames(definition: JsonObject, kind: "list" | "detail"): string[] {
  const result: string[] = [];
  const add = (value: unknown) => {
    if (typeof value === "string") result.push(value);
    else if (value && typeof value === "object" && !Array.isArray(value)) {
      const candidate = value as Record<string, unknown>;
      if (typeof candidate.field === "string") result.push(candidate.field);
      else if (typeof candidate.api_name === "string") result.push(candidate.api_name);
    }
  };
  const direct = definition.columns ?? definition.fields;
  if (Array.isArray(direct)) direct.forEach(add);
  if (kind === "detail" && Array.isArray(definition.sections)) {
    for (const section of definition.sections) {
      if (!section || typeof section !== "object" || Array.isArray(section)) continue;
      const fields = (section as Record<string, unknown>).fields;
      if (Array.isArray(fields)) fields.forEach(add);
    }
  }
  return result;
}

function fieldColumn(field: RecordField): RecordViewColumn {
  return {
    apiName: field.apiName,
    columnName: field.columnName,
    label: field.label,
    kind: field.kind,
    required: field.required,
    readOnly: field.readOnly,
    picklistValues: field.picklistValues,
    referenceTargets: field.referenceTargets,
    scale: field.scale,
  };
}

function uniqueColumns(columns: RecordViewColumn[]): RecordViewColumn[] {
  const seen = new Set<string>();
  return columns.filter((column) => {
    if (seen.has(column.columnName)) return false;
    seen.add(column.columnName);
    return true;
  });
}

function resolveView(input: {
  snapshot: AppRegistrySnapshot;
  objectApiName: string;
  role: RecordViewerRole;
  layoutKind: "list" | "detail";
  allFields?: boolean;
  columns?: string[];
}): RecordObjectView {
  if (input.snapshot.app.status !== "active") {
    throw new RecordReadTargetError("record app is not active");
  }
  const objectApiName = readIdentifier(input.objectApiName, "record object");
  const object = input.snapshot.objects.find(
    (candidate) => candidate.apiName === objectApiName && candidate.archivedAt === null,
  );
  if (!object) throw new RecordReadTargetError("record object is unknown or archived");
  const permission = permissionFor(input.snapshot, object, input.role);
  const fields = objectFields(input.snapshot, object);
  const byApiName = new Map(fields.map((field) => [field.apiName, field]));
  const layout = input.snapshot.layouts.find(
    (candidate) => candidate.objectId === object.id && candidate.kind === input.layoutKind,
  );
  const explicitColumns = input.columns !== undefined && !input.allFields;
  const requested = explicitColumns
    ? (input.columns ?? [])
    : layout && !input.allFields
      ? layoutFieldNames(layout.definition, input.layoutKind)
      : [];
  const selected = requested
    .map((apiName) => {
      try {
        return byApiName.get(validateRecordIdentifier(apiName));
      } catch {
        return undefined;
      }
    })
    .filter((field): field is RecordField => field !== undefined);
  const fallback = [
    byApiName.get(object.nameField),
    ...fields.filter((field) => field.apiName !== object.nameField),
  ].filter((field): field is RecordField => field !== undefined);
  // An explicit layout is an allowlist. If every entry is stale or invalid,
  // fail closed with no business columns instead of unexpectedly exposing all
  // live fields through the fallback view.
  const fieldColumns = (
    (explicitColumns || (layout && !input.allFields)) ? selected : fallback
  ).map(fieldColumn);
  const substrateColumns: RecordViewColumn[] = [];
  if (object.visibility === "owner") {
    substrateColumns.push({
      apiName: RECORD_SYSTEM_COLUMNS.ownerUserId,
      columnName: RECORD_SYSTEM_COLUMNS.ownerUserId,
      label: "Owner",
      kind: "owner",
      required: false,
      readOnly: true,
      picklistValues: null,
      referenceTargets: null,
      scale: null,
    });
  }
  if (input.layoutKind === "detail") {
    substrateColumns.push({
      apiName: RECORD_SYSTEM_COLUMNS.updatedAt,
      columnName: RECORD_SYSTEM_COLUMNS.updatedAt,
      label: "Last updated",
      kind: "system_datetime",
      required: false,
      readOnly: true,
      picklistValues: null,
      referenceTargets: null,
      scale: null,
    });
  }
  return {
    object,
    permission,
    columns: uniqueColumns([...fieldColumns, ...substrateColumns]),
  };
}

function boundedPageSize(value: number | undefined): number {
  const pageSize = value ?? DEFAULT_PAGE_SIZE;
  if (!Number.isSafeInteger(pageSize) || pageSize < 1 || pageSize > MAX_PAGE_SIZE) {
    throw new RecordReadTargetError(`record page size must be between 1 and ${MAX_PAGE_SIZE}`);
  }
  return pageSize;
}

function cursorValue(value: string | null | undefined): string | null {
  if (value === undefined || value === null || value === "") return null;
  if (value !== value.trim() || value.length > MAX_CURSOR_LENGTH) {
    throw new RecordReadTargetError("record cursor is malformed");
  }
  return value;
}

function recordIdValue(value: string): string {
  const recordId = value.trim();
  if (!recordId || recordId.length > 512) {
    throw new RecordReadTargetError("record id must be a non-empty string");
  }
  return recordId;
}

function likeValue(value: string, mode: "contains" | "starts_with"): string {
  const escaped = value.replaceAll("\\", "\\\\").replaceAll("%", "\\%").replaceAll("_", "\\_");
  return mode === "contains" ? `%${escaped}%` : `${escaped}%`;
}

function dateBounds(
  value: unknown,
  now: Date,
): { start: string; end: string } {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new RecordReadTargetError("relative record filters require a date macro");
  }
  const macro = (value as Record<string, unknown>).macro;
  if (macro === "this_quarter") {
    const quarterMonth = Math.floor(now.getUTCMonth() / 3) * 3;
    const start = new Date(Date.UTC(now.getUTCFullYear(), quarterMonth, 1));
    const end = new Date(Date.UTC(now.getUTCFullYear(), quarterMonth + 3, 1));
    return { start: start.toISOString(), end: end.toISOString() };
  }
  if (macro === "last_n_days") {
    const days = (value as Record<string, unknown>).days;
    if (!Number.isSafeInteger(days) || Number(days) < 1 || Number(days) > 3650) {
      throw new RecordReadTargetError(
        "last_n_days record filters require between 1 and 3650 days",
      );
    }
    const end = new Date(now);
    const start = new Date(end.getTime() - Number(days) * 86_400_000);
    return { start: start.toISOString(), end: end.toISOString() };
  }
  throw new RecordReadTargetError("unknown relative record filter macro");
}

function semanticPicklistValues(
  field: RecordField,
  semantic: "open" | "closed",
): string[] {
  if (field.kind !== "picklist") {
    throw new RecordReadTargetError(
      `${semantic} record filters require a picklist field`,
    );
  }
  const values = (field.picklistValues ?? []).flatMap((entry): string[] => {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) return [];
    const option = entry as Record<string, unknown>;
    const semantics = Array.isArray(option.semantic)
      ? option.semantic
      : [option.semantic];
    return semantics.includes(semantic) && typeof option.value === "string"
      ? [option.value]
      : [];
  });
  if (values.length === 0) {
    throw new RecordReadTargetError(
      `picklist field has no ${semantic} semantic values`,
    );
  }
  return values;
}

const NUMERIC_FIELD_KINDS = new Set<RecordFieldKind>(["integer", "decimal", "currency", "percent"]);

/** A filter on a key inside the record document. */
function documentClause(field: RecordField, body: string): string {
  return `${RECORD_DATA_COLUMN}: { ${field.columnName}: ${body} }`;
}

/**
 * GraphJin casts a JSON key only for a number or boolean literal; a variable
 * compares as text. Validated numbers and booleans are therefore inlined.
 */
function typedLiteral(field: RecordField, value: unknown): string | null {
  if (NUMERIC_FIELD_KINDS.has(field.kind)) {
    const number = typeof value === "number" ? value : Number(value);
    if (typeof value === "boolean" || value === "" || !Number.isFinite(number)) {
      throw new RecordReadTargetError(`${field.apiName} filters require a number`);
    }
    return String(number);
  }
  if (field.kind === "boolean") {
    if (typeof value !== "boolean") {
      throw new RecordReadTargetError(`${field.apiName} filters require true or false`);
    }
    return value ? "true" : "false";
  }
  return null;
}

function filterClause(
  filter: RecordListFilter,
  variable: string,
  fields: Map<string, RecordField>,
  now: Date,
): { clause: string; variables: Array<[string, unknown]> } {
  const apiName = readIdentifier(filter.field, "record filter field");
  const field = fields.get(apiName);
  if (!field) throw new RecordReadTargetError(`unknown record filter field: ${apiName}`);
  if (filter.operator === "is_null") {
    if (typeof filter.value !== "boolean") {
      throw new RecordReadTargetError("is_null record filters require a boolean value");
    }
    return {
      clause: documentClause(field, `{ is_null: ${filter.value ? "true" : "false"} }`),
      variables: [],
    };
  }
  if (filter.operator === "in" || filter.operator === "not_in") {
    if (!Array.isArray(filter.value) || filter.value.length < 1 || filter.value.length > 100) {
      throw new RecordReadTargetError(`${filter.operator} record filters require 1 to 100 values`);
    }
    if (NUMERIC_FIELD_KINDS.has(field.kind) || field.kind === "boolean") {
      const literals = filter.value.map((value) => typedLiteral(field, value));
      return {
        clause: documentClause(field, `{ ${filter.operator}: [${literals.join(", ")}] }`),
        variables: [],
      };
    }
    return {
      clause: documentClause(field, `{ ${filter.operator}: $${variable} }`),
      variables: [[variable, filter.value.map(String)]],
    };
  }
  if (filter.operator === "contains" || filter.operator === "starts_with") {
    if (typeof filter.value !== "string" || !filter.value.trim()) {
      throw new RecordReadTargetError(`${filter.operator} record filters require text`);
    }
    return {
      clause: documentClause(field, `{ ilike: $${variable} }`),
      variables: [
        [
          variable,
          likeValue(filter.value.trim().slice(0, MAX_SEARCH_LENGTH), filter.operator),
        ],
      ],
    };
  }
  if (filter.operator === "relative") {
    if (field.kind !== "date" && field.kind !== "datetime") {
      throw new RecordReadTargetError("relative record filters require a date field");
    }
    const bounds = dateBounds(filter.value, now);
    // Documents store dates as YYYY-MM-DD and datetimes as ISO strings, so
    // the bounds must use the same text form to compare correctly.
    const bound = (value: string) => (field.kind === "date" ? value.slice(0, 10) : value);
    return {
      clause:
        `and: [{ ${documentClause(field, `{ gte: $${variable}_start }`)} }, ` +
        `{ ${documentClause(field, `{ lt: $${variable}_end }`)} }]`,
      variables: [
        [`${variable}_start`, bound(bounds.start)],
        [`${variable}_end`, bound(bounds.end)],
      ],
    };
  }
  if (filter.operator === "is_open" || filter.operator === "is_closed") {
    const semantic = filter.operator === "is_open" ? "open" : "closed";
    return {
      clause: documentClause(field, `{ in: $${variable} }`),
      variables: [[variable, semanticPicklistValues(field, semantic)]],
    };
  }
  if (
    filter.operator !== "eq" &&
    filter.operator !== "neq" &&
    filter.operator !== "gt" &&
    filter.operator !== "gte" &&
    filter.operator !== "lt" &&
    filter.operator !== "lte"
  ) {
    throw new RecordReadTargetError("unknown record filter operator");
  }
  const literal = typedLiteral(field, filter.value);
  if (literal !== null) {
    return { clause: documentClause(field, `{ ${filter.operator}: ${literal} }`), variables: [] };
  }
  return {
    clause: documentClause(field, `{ ${filter.operator}: $${variable} }`),
    variables: [[variable, field.kind === "date" ? String(filter.value).slice(0, 10) : filter.value]],
  };
}

function isFilterGroup(
  value: RecordFilterExpression,
): value is Extract<RecordFilterExpression, { op: "and" | "or" }> {
  return "op" in value;
}

function expressionClause(
  expression: RecordFilterExpression,
  fields: Map<string, RecordField>,
  variables: Record<string, unknown>,
  now: Date,
  path = "0",
  budget = { count: 0 },
  depth = 0,
): string {
  budget.count += 1;
  if (budget.count > 50 || depth > 5) {
    throw new RecordReadTargetError("record filter expression is too complex");
  }
  if (!isFilterGroup(expression)) {
    const resolved = filterClause(expression, `filter_${path}`, fields, now);
    for (const [name, value] of resolved.variables) variables[name] = value;
    return resolved.clause;
  }
  if (
    (expression.op !== "and" && expression.op !== "or") ||
    !Array.isArray(expression.clauses) ||
    expression.clauses.length < 1 ||
    expression.clauses.length > 20
  ) {
    throw new RecordReadTargetError(
      "record filter groups require 1 to 20 clauses",
    );
  }
  const children = expression.clauses.map((child, index) =>
    expressionClause(child, fields, variables, now, `${path}_${index}`, budget, depth + 1),
  );
  return `${expression.op}: [${children.map((child) => `{ ${child} }`).join(", ")}]`;
}

function whereDocument(clauses: string[]): string | null {
  if (clauses.length === 0) return null;
  if (clauses.length === 1) return `{ ${clauses[0]} }`;
  return `{ and: [${clauses.map((clause) => `{ ${clause} }`).join(", ")}] }`;
}

function selectorArguments(args: string[]): string {
  return args.length === 0 ? "" : `(${args.join(", ")})`;
}

const SYSTEM_COLUMN_NAMES = new Set<string>(["id", ...Object.values(RECORD_SYSTEM_COLUMNS)]);

/** Fields live in the record document; system columns stay on the row. */
export function isRecordDocumentColumn(column: Pick<RecordViewColumn, "columnName">): boolean {
  return !SYSTEM_COLUMN_NAMES.has(column.columnName);
}

function querySelection(view: RecordObjectView): string {
  const systemColumns = view.columns
    .filter((column) => !isRecordDocumentColumn(column))
    .map((column) => column.columnName);
  const hasFields = view.columns.some(isRecordDocumentColumn);
  return ["id", ...systemColumns, ...(hasFields ? [RECORD_DATA_COLUMN] : [])]
    .filter((value, index, values) => values.indexOf(value) === index)
    .join(" ");
}

/**
 * Flatten a stored row into the view's columns. Only fields in the view
 * leave the document, so keys the viewer may not read are dropped here.
 */
export function projectRecordRow(
  view: Pick<RecordObjectView, "columns">,
  row: Record<string, unknown>,
): Record<string, unknown> {
  const raw = row[RECORD_DATA_COLUMN];
  const document =
    raw && typeof raw === "object" && !Array.isArray(raw) ? (raw as Record<string, unknown>) : {};
  const projected: Record<string, unknown> = { id: row.id };
  for (const column of view.columns) {
    projected[column.columnName] = isRecordDocumentColumn(column)
      ? (document[column.columnName] ?? null)
      : (row[column.columnName] ?? null);
  }
  return projected;
}

export function projectRecordRows(
  view: Pick<RecordObjectView, "columns">,
  rows: Array<Record<string, unknown>>,
): Array<Record<string, unknown>> {
  return rows.map((row) => projectRecordRow(view, row));
}

export function buildRecordListQuery(input: {
  snapshot: AppRegistrySnapshot;
  objectApiName: string;
  role: RecordViewerRole;
  userId: string;
  first?: number;
  after?: string | null;
  sort?: { field: string; direction: "asc" | "desc" };
  search?: string | null;
  filters?: RecordListFilter[];
  filter?: RecordFilterExpression | null;
  myRecords?: boolean;
  now?: Date;
  columns?: string[];
}): RecordListQuery {
  const view = resolveView({ ...input, layoutKind: "list" });
  const fields = new Map(
    objectFields(input.snapshot, view.object).map((field) => [field.apiName, field]),
  );
  const variables: Record<string, unknown> = { first: boundedPageSize(input.first) };
  const clauses: string[] = [];
  const after = cursorValue(input.after);
  if (after !== null) variables.rows_cursor = after;

  const search = input.search?.trim() ?? "";
  if (search) {
    if (search.length > MAX_SEARCH_LENGTH) {
      throw new RecordReadTargetError(`record search cannot exceed ${MAX_SEARCH_LENGTH} characters`);
    }
    clauses.push(
      view.object.nameField === "id"
        ? "id: { ilike: $search }"
        : `${RECORD_DATA_COLUMN}: { ${view.object.nameField}: { ilike: $search } }`,
    );
    variables.search = likeValue(search, "contains");
  }
  if (input.myRecords) {
    if (view.object.visibility !== "owner" || !input.userId.trim()) {
      throw new RecordReadTargetError("my records scope requires an owner-visible object and actor");
    }
    clauses.push(`${RECORD_SYSTEM_COLUMNS.ownerUserId}: { eq: $viewer_id }`);
    variables.viewer_id = input.userId;
  }
  const now = input.now ?? new Date();
  for (const [index, filter] of (input.filters ?? []).entries()) {
    const resolved = filterClause(filter, `filter_${index}`, fields, now);
    clauses.push(resolved.clause);
    for (const [name, value] of resolved.variables) variables[name] = value;
  }
  if (input.filter) {
    clauses.push(expressionClause(input.filter, fields, variables, now));
  }

  const rawSortField = input.sort?.field ?? view.object.nameField;
  const sortField =
    rawSortField === "id"
      ? readIdentifier("id", "record sort field")
      : fields.get(readIdentifier(rawSortField, "record sort field"))?.columnName;
  if (!sortField) throw new RecordReadTargetError("unknown record sort field");
  const direction = input.sort?.direction ?? "asc";
  const order =
    sortField === "id"
      ? `{ id: ${direction} }`
      : `{ ${RECORD_DATA_COLUMN}: { ${sortField}: ${direction} }, id: ${direction} }`;
  const where = whereDocument(clauses);
  const pageArguments = [
    "first: $first",
    ...(after === null ? [] : ["after: $rows_cursor"]),
    `order_by: ${order}`,
    ...(where ? [`where: ${where}`] : []),
  ];
  const totalArguments = where ? [`where: ${where}`] : [];
  const tableName = view.object.tableName;
  // GraphJin names the returned cursor after the selector alias (`rows`),
  // even though the document must request the physical table cursor field.
  const cursorSelection = `${tableName}_cursor`;
  const cursorField = "rows_cursor";
  const query = `query RecordsList { rows: ${tableName}${selectorArguments(pageArguments)} { ${querySelection(view)} } ${cursorSelection} totals: ${tableName}${selectorArguments(totalArguments)} { count_id } }`;
  return {
    operationName: "RecordsList",
    query,
    variables,
    view,
    cursorField,
    resultField: "rows",
    totalField: "totals",
  };
}

export function buildRecordAggregateQuery(input: {
  snapshot: AppRegistrySnapshot;
  objectApiName: string;
  role: RecordViewerRole;
  aggregate: "count" | "sum";
  field?: string | null;
  filter?: RecordFilterExpression | null;
  now?: Date;
}): RecordAggregateQuery {
  const view = resolveView({ ...input, layoutKind: "list" });
  const fields = new Map(
    objectFields(input.snapshot, view.object).map((field) => [field.apiName, field]),
  );
  let valueField = "count_id";
  if (input.aggregate === "sum") {
    if (!input.field) {
      throw new RecordReadTargetError("sum record metrics require a field");
    }
    const apiName = readIdentifier(input.field, "record metric field");
    const field = fields.get(apiName);
    if (
      !field ||
      !["integer", "decimal", "currency", "percent"].includes(field.kind)
    ) {
      throw new RecordReadTargetError("sum record metrics require a numeric field");
    }
    valueField = `total: sum(expr: { cast: { expr: { json: { col: ${RECORD_DATA_COLUMN}, path: [${field.columnName}] } }, as: "numeric" } })`;
  } else if (input.field) {
    throw new RecordReadTargetError("count record metrics do not accept a field");
  }
  const variables: Record<string, unknown> = {};
  const clauses = input.filter
    ? [
        expressionClause(
          input.filter,
          fields,
          variables,
          input.now ?? new Date(),
        ),
      ]
    : [];
  const where = whereDocument(clauses);
  const tableName = view.object.tableName;
  const query =
    `query RecordsMetric { metric: ${tableName}` +
    `${selectorArguments(where ? [`where: ${where}`] : [])} { ${valueField} } }`;
  return {
    operationName: "RecordsMetric",
    query,
    variables,
    view,
    resultField: "metric",
    valueField: input.aggregate === "sum" ? "total" : valueField,
  };
}

export function buildRecordDetailQuery(input: {
  snapshot: AppRegistrySnapshot;
  objectApiName: string;
  role: RecordViewerRole;
  recordId: string;
  allFields?: boolean;
}): RecordDetailQuery {
  const view = resolveView({ ...input, layoutKind: "detail" });
  const recordId = recordIdValue(input.recordId);
  const query = `query RecordsDetail { rows: ${view.object.tableName}(where: { id: { eq: $record_id } }, limit: 1) { ${querySelection(view)} } }`;
  return {
    operationName: "RecordsDetail",
    query,
    variables: { record_id: recordId },
    view,
    resultField: "rows",
  };
}

/**
 * Resolve reference display labels without widening the caller's access.
 * The target object passes through the same active-object and role checks as
 * every generated list/detail query, and GraphJin still enforces org,
 * soft-delete, and row policies on the viewer token.
 */
export function buildRecordReferenceLabelQuery(input: {
  snapshot: AppRegistrySnapshot;
  objectApiName: string;
  role: RecordViewerRole;
  recordIds: string[];
}): RecordReferenceLabelQuery {
  const target = input.snapshot.objects.find(
    (candidate) =>
      candidate.apiName === readIdentifier(input.objectApiName, "reference target") &&
      candidate.archivedAt === null,
  );
  if (!target) {
    throw new RecordReadTargetError("reference target is unknown or archived");
  }
  const recordIds = [...new Set(input.recordIds.map(recordIdValue))];
  if (recordIds.length < 1 || recordIds.length > 100) {
    throw new RecordReadTargetError("reference label queries require 1 to 100 record ids");
  }
  const view = resolveView({
    snapshot: input.snapshot,
    objectApiName: target.apiName,
    role: input.role,
    layoutKind: "list",
    columns: [target.nameField],
  });
  const query =
    `query RecordsReferenceLabels { rows: ${view.object.tableName}` +
    `(where: { id: { in: $record_ids } }, limit: ${recordIds.length}) { ${querySelection(view)} } }`;
  return {
    operationName: "RecordsReferenceLabels",
    query,
    variables: { record_ids: recordIds },
    view,
    resultField: "rows",
  };
}

const RECYCLE_SELECTION = [
  "app_id",
  "object_api_name",
  "record_id",
  "record_name",
  "owner_user_id",
  "deleted_at",
  "deleted_by",
].join(" ");

function recycleTargetWhere(search: boolean): string {
  const clauses = [
    "{ app_id: { eq: $app_id } }",
    "{ object_api_name: { eq: $object_api_name } }",
    ...(search ? ["{ record_name: { ilike: $search } }"] : []),
  ];
  return `{ and: [${clauses.join(", ")}] }`;
}

export function buildRecordChangeLogQuery(input: {
  snapshot: AppRegistrySnapshot;
  objectApiName: string;
  role: RecordViewerRole;
  recordId: string;
  first?: number;
}): RecordChangeLogQuery {
  const view = resolveView({ ...input, layoutKind: "detail" });
  return {
    operationName: "RecordsChangeLog",
    query:
      "query RecordsChangeLog { history: record_change_log(" +
      "first: $first, order_by: { at: desc, id: desc }, where: { " +
      "app_id: { eq: $app_id }, object_api_name: { eq: $object_api_name }, " +
      "record_id: { eq: $record_id } }) { id action actor_user_id " +
      "action_request_id changes at } }",
    variables: {
      app_id: input.snapshot.app.appId,
      object_api_name: view.object.apiName,
      record_id: recordIdValue(input.recordId),
      first: boundedPageSize(input.first ?? 25),
    },
    resultField: "history",
  };
}

export function buildRecordSchemaLogQuery(input: {
  snapshot: AppRegistrySnapshot;
  role: RecordViewerRole;
  first?: number;
}): RecordSchemaLogQuery {
  if (input.role !== "admin") throw new RecordReadPermissionError();
  if (input.snapshot.app.status !== "active") {
    throw new RecordReadTargetError("record app is not active");
  }
  return {
    operationName: "RecordsSchemaLog",
    query:
      "query RecordsSchemaLog { history: app_schema_log(" +
      "first: $first, order_by: { at: desc, id: desc }, " +
      "where: { app_id: { eq: $app_id } }) { id action detail " +
      "actor_user_id action_request_id at } }",
    variables: {
      app_id: input.snapshot.app.appId,
      first: boundedPageSize(input.first ?? 50),
    },
    resultField: "history",
  };
}

export function buildRecordRecycleListQuery(input: {
  snapshot: AppRegistrySnapshot;
  objectApiName: string;
  role: RecordViewerRole;
  first?: number;
  after?: string | null;
  search?: string | null;
}): RecordRecycleListQuery {
  const view = resolveView({ ...input, layoutKind: "list" });
  const variables: Record<string, unknown> = {
    app_id: input.snapshot.app.appId,
    object_api_name: view.object.apiName,
    first: boundedPageSize(input.first),
  };
  const after = cursorValue(input.after);
  if (after !== null) variables.rows_cursor = after;
  const search = input.search?.trim() ?? "";
  if (search.length > MAX_SEARCH_LENGTH) {
    throw new RecordReadTargetError(
      `record search cannot exceed ${MAX_SEARCH_LENGTH} characters`,
    );
  }
  if (search) variables.search = likeValue(search, "contains");
  const where = recycleTargetWhere(Boolean(search));
  const argumentsList = [
    "first: $first",
    ...(after === null ? [] : ["after: $rows_cursor"]),
    "order_by: { deleted_at: desc, record_id: asc }",
    `where: ${where}`,
  ];
  const query = `query RecordsRecycleList { rows: recycle_record${selectorArguments(argumentsList)} { ${RECYCLE_SELECTION} } recycle_record_cursor totals: recycle_record(where: ${where}) { count: count_record_id } }`;
  return {
    operationName: "RecordsRecycleList",
    query,
    variables,
    view,
    cursorField: "rows_cursor",
    resultField: "rows",
    totalField: "totals",
  };
}

export function buildRecordRecycleDetailQuery(input: {
  snapshot: AppRegistrySnapshot;
  objectApiName: string;
  role: RecordViewerRole;
  recordId: string;
}): RecordRecycleDetailQuery {
  const view = resolveView({ ...input, layoutKind: "detail" });
  const variables = {
    app_id: input.snapshot.app.appId,
    object_api_name: view.object.apiName,
    record_id: recordIdValue(input.recordId),
  };
  const where = `{ and: [{ app_id: { eq: $app_id } }, { object_api_name: { eq: $object_api_name } }, { record_id: { eq: $record_id } }] }`;
  return {
    operationName: "RecordsRecycleDetail",
    query: `query RecordsRecycleDetail { rows: recycle_record(where: ${where}, limit: 1) { ${RECYCLE_SELECTION} } }`,
    variables,
    view,
    resultField: "rows",
  };
}
