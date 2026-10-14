import type { Pool, PoolClient } from "pg";
import { validateRecordIdentifier } from "../naming";
import { RECORD_DATA_COLUMN, RECORD_SYSTEM_COLUMNS } from "../policy/graphjin";
import { loadRecordsCatalogSnapshot } from "./catalog";
import type {
  RecordFieldKind,
  RecordIdentifier,
  RecordVisibility,
} from "../types";

export type RecordDdlFieldDefinition = {
  columnName: RecordIdentifier;
  kind: RecordFieldKind;
  required: boolean;
  archived?: boolean;
  length?: number | null;
  scale?: number | null;
};

export type RecordDdlObjectDefinition = {
  appId: string;
  apiName: RecordIdentifier;
  tableName: RecordIdentifier;
  nameField: RecordIdentifier;
  visibility: RecordVisibility;
  archived?: boolean;
  fields: RecordDdlFieldDefinition[];
};

type Queryable = Pick<Pool | PoolClient, "query">;

const RESERVED_COLUMNS = new Set<string>([
  "id",
  RECORD_DATA_COLUMN,
  RECORD_SYSTEM_COLUMNS.orgId,
  RECORD_SYSTEM_COLUMNS.ownerUserId,
  RECORD_SYSTEM_COLUMNS.createdAt,
  RECORD_SYSTEM_COLUMNS.createdBy,
  RECORD_SYSTEM_COLUMNS.updatedAt,
  RECORD_SYSTEM_COLUMNS.updatedBy,
  RECORD_SYSTEM_COLUMNS.actionRequestId,
  RECORD_SYSTEM_COLUMNS.mutationId,
  RECORD_SYSTEM_COLUMNS.deletedAt,
]);

function objectBlock(object: RecordDdlObjectDefinition): string {
  const apiName = validateRecordIdentifier(object.apiName);
  const tableName = validateRecordIdentifier(object.tableName);
  const nameField = validateRecordIdentifier(object.nameField);
  if (!object.appId.trim()) throw new Error(`records DDL object ${apiName} requires an app id`);

  const liveFields = object.fields.filter((field) => !field.archived);
  const seenColumns = new Set<string>();
  for (const field of liveFields) {
    const columnName = validateRecordIdentifier(field.columnName);
    if (field.kind === "id" && columnName !== "id") {
      throw new Error(`id field on ${tableName} must use the id column`);
    }
    if (field.kind !== "id" && RESERVED_COLUMNS.has(columnName)) {
      throw new Error(`field ${tableName}.${columnName} uses a records system column`);
    }
    if (seenColumns.has(columnName)) {
      throw new Error(`duplicate records DDL field: ${tableName}.${columnName}`);
    }
    seenColumns.add(columnName);
  }
  if (nameField !== "id" && !liveFields.some((field) => field.columnName === nameField)) {
    throw new Error(`records DDL name field is missing: ${tableName}.${nameField}`);
  }

  const ownerField =
    object.visibility === "owner"
      ? [`  ${RECORD_SYSTEM_COLUMNS.ownerUserId}: Text!`]
      : [];

  return [
    `type ${tableName} {`,
    "  id: Text! @id",
    `  ${RECORD_SYSTEM_COLUMNS.orgId}: Text!`,
    ...ownerField,
    `  ${RECORD_DATA_COLUMN}: Jsonb!`,
    `  ${RECORD_SYSTEM_COLUMNS.createdAt}: Timestamptz!`,
    `  ${RECORD_SYSTEM_COLUMNS.createdBy}: Text!`,
    `  ${RECORD_SYSTEM_COLUMNS.updatedAt}: Timestamptz!`,
    `  ${RECORD_SYSTEM_COLUMNS.updatedBy}: Text!`,
    `  ${RECORD_SYSTEM_COLUMNS.actionRequestId}: Text!`,
    `  ${RECORD_SYSTEM_COLUMNS.mutationId}: Text!`,
    `  ${RECORD_SYSTEM_COLUMNS.deletedAt}: Timestamptz`,
    "}",
  ].join("\n");
}

/**
 * Project the registry into GraphJin's additive DDL. Every object table has
 * the same shape: system columns plus one jsonb document that holds the
 * fields, so field changes never alter a table.
 */
export function buildRecordsGraphjinDdl(objects: RecordDdlObjectDefinition[]): string {
  const liveObjects = objects
    .filter((object) => !object.archived)
    .sort((left, right) => left.tableName.localeCompare(right.tableName));
  const seenTables = new Set<string>();
  for (const object of liveObjects) {
    const tableName = validateRecordIdentifier(object.tableName);
    if (seenTables.has(tableName)) {
      throw new Error(`duplicate records DDL table: ${tableName}`);
    }
    seenTables.add(tableName);
  }
  if (liveObjects.length === 0) {
    return "# No live records objects. Archived tables remain physically retained.\n";
  }
  return `${liveObjects.map(objectBlock).join("\n\n")}\n`;
}

export type RecordsDdlVerification = {
  applied: boolean;
  problems: string[];
};

/** Prove the additive desired shape exists after sync or during recovery. */
export async function verifyRecordsGraphjinDdlApplied(
  client: Queryable,
  objects: RecordDdlObjectDefinition[],
): Promise<RecordsDdlVerification> {
  const catalog = await loadRecordsCatalogSnapshot(client);
  const problems: string[] = [];
  const columnByRelation = new Map<string, Map<string, { dataType: string; notNull: boolean }>>();
  for (const column of catalog.columns) {
    const key = `${column.schema}\u0000${column.table}`;
    const relation = columnByRelation.get(key) ?? new Map();
    relation.set(column.column, { dataType: column.dataType, notNull: column.notNull });
    columnByRelation.set(key, relation);
  }

  for (const object of objects.filter((candidate) => !candidate.archived)) {
    const tableName = validateRecordIdentifier(object.tableName);
    const relationKey = `public\u0000${tableName}`;
    const columns = columnByRelation.get(relationKey);
    if (!columns) {
      problems.push(`missing relation public.${tableName}`);
      continue;
    }

    const expectedColumns: Array<{ name: string; dataType: string; notNull: boolean }> = [
      { name: "id", dataType: "text", notNull: true },
      { name: RECORD_SYSTEM_COLUMNS.orgId, dataType: "text", notNull: true },
      ...(object.visibility === "owner"
        ? [{ name: RECORD_SYSTEM_COLUMNS.ownerUserId, dataType: "text", notNull: true }]
        : []),
      { name: RECORD_DATA_COLUMN, dataType: "jsonb", notNull: true },
      { name: RECORD_SYSTEM_COLUMNS.createdAt, dataType: "timestamp with time zone", notNull: true },
      { name: RECORD_SYSTEM_COLUMNS.createdBy, dataType: "text", notNull: true },
      { name: RECORD_SYSTEM_COLUMNS.updatedAt, dataType: "timestamp with time zone", notNull: true },
      { name: RECORD_SYSTEM_COLUMNS.updatedBy, dataType: "text", notNull: true },
      { name: RECORD_SYSTEM_COLUMNS.actionRequestId, dataType: "text", notNull: true },
      { name: RECORD_SYSTEM_COLUMNS.mutationId, dataType: "text", notNull: true },
      { name: RECORD_SYSTEM_COLUMNS.deletedAt, dataType: "timestamp with time zone", notNull: false },
    ];
    for (const expected of expectedColumns) {
      const actual = columns.get(expected.name);
      if (!actual) {
        problems.push(`missing column public.${tableName}.${expected.name}`);
        continue;
      }
      if (actual.dataType !== expected.dataType) {
        problems.push(
          `type drift public.${tableName}.${expected.name}: expected ${expected.dataType}, found ${actual.dataType}`,
        );
      }
      if (expected.notNull && !actual.notNull) {
        problems.push(`nullable required column public.${tableName}.${expected.name}`);
      }
    }

    const primaryKey = catalog.constraints.some(
      (constraint) =>
        constraint.schema === "public" &&
        constraint.table === tableName &&
        /^PRIMARY KEY \("?id"?\)$/i.test(constraint.definition),
    );
    if (!primaryKey) problems.push(`missing id primary key public.${tableName}`);
  }
  return { applied: problems.length === 0, problems };
}
