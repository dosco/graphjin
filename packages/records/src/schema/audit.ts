import type { Pool, PoolClient } from "pg";
import { quoteRecordIdentifier, validateRecordIdentifier } from "../naming";
import type { RecordIdentifier } from "../types";

export const RECORDS_AUDIT_TRIGGER_NAME = "nk_record_change_audit";
export const RECORDS_AUDIT_OWNER_TRIGGER_NAME = "nk_record_change_owner";
export const RECORDS_AUDIT_TRIGGER_VERSION = 5;

const REQUIRED_AUDIT_COLUMNS = [
  "id",
  "data",
  "org_id",
  "nk_created_at",
  "nk_created_by",
  "nk_updated_at",
  "nk_updated_by",
  "nk_action_request_id",
  "nk_mutation_id",
  "nk_deleted_at",
] as const;

type Queryable = Pick<Pool | PoolClient, "query">;

function sqlLiteral(value: string): string {
  return `'${value.replaceAll("'", "''")}'`;
}

/**
 * Install the versioned engine audit trigger on one already-provisioned app
 * relation. This is the sole bounded app-table SQL surface: GraphJin still
 * owns table/column DDL and every business-data mutation.
 */
export async function ensureRecordsAuditTrigger(
  client: Queryable,
  input: {
    tableSchema: RecordIdentifier;
    tableName: RecordIdentifier;
    appId: string;
    objectApiName: RecordIdentifier;
  },
): Promise<"created" | "current"> {
  const tableSchema = validateRecordIdentifier(input.tableSchema);
  const tableName = validateRecordIdentifier(input.tableName);
  const objectApiName = validateRecordIdentifier(input.objectApiName);
  if (!input.appId.trim()) throw new Error("records audit trigger requires an app id");

  const substrate = await client.query<{ version: number }>(
    "select version from engine.substrate_version where name = 'record_audit_trigger'",
  );
  if (substrate.rows[0]?.version !== RECORDS_AUDIT_TRIGGER_VERSION) {
    throw new Error(
      `records audit substrate must be version ${RECORDS_AUDIT_TRIGGER_VERSION}`,
    );
  }

  const relation = await client.query<{ columns: string[] }>(
    `select array_agg(a.attname::text order by a.attnum) as columns
     from pg_catalog.pg_class c
     join pg_catalog.pg_namespace n on n.oid = c.relnamespace
     join pg_catalog.pg_attribute a on a.attrelid = c.oid
       and a.attnum > 0 and not a.attisdropped
     where n.nspname = $1 and c.relname = $2 and c.relkind in ('r', 'p')
     group by c.oid`,
    [tableSchema, tableName],
  );
  const columns = new Set(relation.rows[0]?.columns ?? []);
  if (columns.size === 0) {
    throw new Error(`records audit relation does not exist: ${tableSchema}.${tableName}`);
  }
  const missingColumns = REQUIRED_AUDIT_COLUMNS.filter((column) => !columns.has(column));
  if (missingColumns.length > 0) {
    throw new Error(
      `records audit relation ${tableSchema}.${tableName} is missing columns: ${missingColumns.join(", ")}`,
    );
  }

  const existing = await client.query<{ definition: string }>(
    `select pg_get_triggerdef(t.oid, true) as definition
     from pg_catalog.pg_trigger t
     join pg_catalog.pg_class c on c.oid = t.tgrelid
     join pg_catalog.pg_namespace n on n.oid = c.relnamespace
     where n.nspname = $1 and c.relname = $2 and t.tgname = $3
       and not t.tgisinternal`,
    [tableSchema, tableName, RECORDS_AUDIT_TRIGGER_NAME],
  );
  const definition = existing.rows[0]?.definition;
  let status: "created" | "current" = "current";
  if (definition) {
    if (
      !definition.includes("engine.capture_record_change(") ||
      !definition.includes(sqlLiteral(input.appId)) ||
      !definition.includes(sqlLiteral(objectApiName))
    ) {
      throw new Error(
        `records audit trigger drift detected on ${tableSchema}.${tableName}`,
      );
    }
  } else {
    await client.query(
      `create trigger ${quoteRecordIdentifier(validateRecordIdentifier(RECORDS_AUDIT_TRIGGER_NAME))}
       after insert or update on ${quoteRecordIdentifier(tableSchema)}.${quoteRecordIdentifier(tableName)}
       for each row execute function engine.capture_record_change(
         ${sqlLiteral(input.appId)}, ${sqlLiteral(objectApiName)}
       )`,
    );
    status = "created";
  }

  const ownerTrigger = await client.query<{ definition: string }>(
    `select pg_get_triggerdef(t.oid, true) as definition
     from pg_catalog.pg_trigger t
     join pg_catalog.pg_class c on c.oid = t.tgrelid
     join pg_catalog.pg_namespace n on n.oid = c.relnamespace
     where n.nspname = $1 and c.relname = $2 and t.tgname = $3
       and not t.tgisinternal`,
    [tableSchema, tableName, RECORDS_AUDIT_OWNER_TRIGGER_NAME],
  );
  const ownerDefinition = ownerTrigger.rows[0]?.definition;
  if (ownerDefinition) {
    if (!ownerDefinition.includes("engine.attach_record_change_owner()")) {
      throw new Error(
        `records owner audit trigger drift detected on ${tableSchema}.${tableName}`,
      );
    }
  } else {
    await client.query(
      `create trigger ${quoteRecordIdentifier(validateRecordIdentifier(RECORDS_AUDIT_OWNER_TRIGGER_NAME))}
       after insert or update on ${quoteRecordIdentifier(tableSchema)}.${quoteRecordIdentifier(tableName)}
       for each row execute function engine.attach_record_change_owner()`,
    );
    status = "created";
  }
  return status;
}
