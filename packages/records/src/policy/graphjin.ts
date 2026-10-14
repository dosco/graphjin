import type { Pool } from "pg";
import { validateRecordIdentifier } from "../naming";
import { loadRecordPolicyGrants } from "./store";
import type {
  RecordAppStatus,
  RecordIdentifier,
  RecordVisibility,
} from "../types";

export const RECORD_GRAPHJIN_ROLES = [
  "anon",
  "user",
  "member",
  "admin",
  "service",
] as const;

export type RecordGraphjinRoleName = (typeof RECORD_GRAPHJIN_ROLES)[number];

export const RECORD_SYSTEM_COLUMNS = {
  orgId: "org_id",
  ownerUserId: "owner_user_id",
  createdAt: "nk_created_at",
  createdBy: "nk_created_by",
  updatedAt: "nk_updated_at",
  updatedBy: "nk_updated_by",
  actionRequestId: "nk_action_request_id",
  mutationId: "nk_mutation_id",
  deletedAt: "nk_deleted_at",
} as const;

/** The jsonb document that holds every custom field of a record. */
export const RECORD_DATA_COLUMN = "data";

const HUMAN_SYSTEM_READ_COLUMNS = new Set([
  "id",
  RECORD_SYSTEM_COLUMNS.ownerUserId,
  RECORD_SYSTEM_COLUMNS.createdAt,
  RECORD_SYSTEM_COLUMNS.createdBy,
  RECORD_SYSTEM_COLUMNS.updatedAt,
  RECORD_SYSTEM_COLUMNS.updatedBy,
]);

const SERVICE_IMMUTABLE_UPDATE_COLUMNS = new Set<string>([
  RECORD_SYSTEM_COLUMNS.createdAt,
  RECORD_SYSTEM_COLUMNS.createdBy,
]);

const HUMAN_QUERY_LIMIT = 100;
const SERVICE_QUERY_LIMIT = 500;

export const RECORD_RECYCLE_RELATION = {
  schema: "engine",
  table: "recycle_record",
} as const;

export const RECORD_RECYCLE_READ_COLUMNS = [
  "app_id",
  "object_api_name",
  "record_id",
  "record_name",
  "owner_user_id",
  "deleted_at",
  "deleted_by",
] as const;

export const RECORD_IDENTITY_RELATION = {
  schema: "engine",
  table: "identity_map",
} as const;

export const RECORD_IDENTITY_READ_COLUMNS = [
  "source_instance_id",
  "app_id",
  "source_user_id",
  "source_email",
  "source_name",
  "source_is_active",
  "app_user_id",
  "status",
  "linked_at",
  "updated_at",
] as const;

export const RECORD_CHANGE_LOG_RELATION = {
  schema: "engine",
  table: "record_change_log",
} as const;

export const RECORD_CHANGE_LOG_READ_COLUMNS = [
  "id",
  "app_id",
  "object_api_name",
  "record_id",
  "action",
  "actor_user_id",
  "action_request_id",
  "changes",
  "at",
] as const;

export const RECORD_SCHEMA_LOG_RELATION = {
  schema: "engine",
  table: "app_schema_log",
} as const;

export const RECORD_SCHEMA_LOG_READ_COLUMNS = [
  "id",
  "app_id",
  "action",
  "detail",
  "actor_user_id",
  "action_request_id",
  "at",
] as const;

export type RecordCatalogTable = {
  schema: RecordIdentifier;
  name: RecordIdentifier;
  columns: RecordIdentifier[];
};

export type RecordPolicyApp = {
  orgId: string;
  appId: string;
  status: RecordAppStatus;
};

export type RecordPolicyRegistryObject = {
  id: string;
  orgId: string;
  appId: string;
  apiName: RecordIdentifier;
  tableSchema: RecordIdentifier;
  tableName: RecordIdentifier;
  nameField: RecordIdentifier;
  visibility: RecordVisibility;
  archived: boolean;
};

export type RecordPolicyField = {
  objectId: string;
  columnName: RecordIdentifier;
  archived: boolean;
};

export type RecordPolicyPermission = {
  appId: string;
  role: string;
  objectApiName: RecordIdentifier;
  canRead: boolean;
  canCreate: boolean;
  canUpdate: boolean;
  canDelete: boolean;
};

export type RecordsGraphjinPolicyModel = {
  orgId: string;
  catalog: RecordCatalogTable[];
  apps: RecordPolicyApp[];
  objects: RecordPolicyRegistryObject[];
  fields: RecordPolicyField[];
  permissions: RecordPolicyPermission[];
};

export type GraphjinRoleOperation = {
  block: boolean;
  filters?: string[];
  columns?: string[];
  presets?: Record<string, string>;
  limit?: number;
  disable_functions?: boolean;
};

export type GraphjinRoleTable = {
  name: string;
  schema: string;
  query: GraphjinRoleOperation;
  insert: GraphjinRoleOperation;
  update: GraphjinRoleOperation;
  upsert: GraphjinRoleOperation;
  delete: GraphjinRoleOperation;
};

export type GraphjinRole = {
  name: RecordGraphjinRoleName;
  match?: string;
  tables: GraphjinRoleTable[];
};

type CatalogRow = {
  table_schema: string;
  table_name: string;
  columns: string[];
};

type AppRow = {
  org_id: string;
  app_id: string;
  status: RecordAppStatus;
};

type ObjectRow = {
  id: string;
  org_id: string;
  app_id: string;
  api_name: string;
  table_schema: string;
  table_name: string;
  name_field: string;
  visibility: RecordVisibility;
  archived_at: Date | null;
};

type FieldRow = {
  object_id: string;
  column_name: string;
  archived_at: Date | null;
};

function relationKey(schema: string, table: string): string {
  return `${schema}\u0000${table}`;
}

function blockedOperation(): GraphjinRoleOperation {
  return { block: true };
}

function blockedTable(table: RecordCatalogTable): GraphjinRoleTable {
  return {
    name: table.name,
    schema: table.schema,
    query: blockedOperation(),
    insert: blockedOperation(),
    update: blockedOperation(),
    upsert: blockedOperation(),
    delete: blockedOperation(),
  };
}

function gqlString(value: string): string {
  return JSON.stringify(value);
}

function orgFilter(orgId: string): string {
  return `{ ${RECORD_SYSTEM_COLUMNS.orgId}: { eq: ${gqlString(orgId)} } }`;
}

function notDeletedFilter(): string {
  return `{ ${RECORD_SYSTEM_COLUMNS.deletedAt}: { is_null: true } }`;
}

function ownerFilter(): string {
  return `{ ${RECORD_SYSTEM_COLUMNS.ownerUserId}: { eq: $user_id } }`;
}

function uniqueSorted(values: Iterable<string>): string[] {
  return [...new Set(values)].sort((left, right) => left.localeCompare(right));
}

function hasColumns(table: RecordCatalogTable, names: Iterable<string>): boolean {
  const columns = new Set<string>(table.columns);
  for (const name of names) {
    if (!columns.has(name)) return false;
  }
  return true;
}

function readableColumns(input: {
  table: RecordCatalogTable;
  object: RecordPolicyRegistryObject;
  fields: RecordPolicyField[];
}): string[] {
  // Field-level access applies inside the document in the records read path.
  return uniqueSorted([
    RECORD_DATA_COLUMN,
    ...input.table.columns.filter((column) => HUMAN_SYSTEM_READ_COLUMNS.has(column)),
  ]).filter((column) => (input.table.columns as string[]).includes(column));
}

function serviceInsertColumns(table: RecordCatalogTable): string[] {
  return table.columns.filter(
    (column) =>
      column !== RECORD_SYSTEM_COLUMNS.orgId &&
      column !== RECORD_SYSTEM_COLUMNS.createdAt &&
      column !== RECORD_SYSTEM_COLUMNS.updatedAt &&
      column !== RECORD_SYSTEM_COLUMNS.deletedAt,
  );
}

function serviceUpdateColumns(table: RecordCatalogTable): string[] {
  return table.columns.filter(
    (column) =>
      !SERVICE_IMMUTABLE_UPDATE_COLUMNS.has(column) &&
      column !== RECORD_SYSTEM_COLUMNS.updatedAt,
  );
}

function servicePresets(table: RecordCatalogTable, orgId: string, operation: "insert" | "update") {
  const columns = new Set<string>(table.columns);
  const presets: Record<string, string> = {};
  if (columns.has(RECORD_SYSTEM_COLUMNS.orgId)) {
    presets[RECORD_SYSTEM_COLUMNS.orgId] = orgId;
  }
  if (operation === "insert" && columns.has(RECORD_SYSTEM_COLUMNS.createdAt)) {
    presets[RECORD_SYSTEM_COLUMNS.createdAt] = "now";
  }
  if (columns.has(RECORD_SYSTEM_COLUMNS.updatedAt)) {
    presets[RECORD_SYSTEM_COLUMNS.updatedAt] = "now";
  }
  return presets;
}

function humanTable(input: {
  role: "member" | "admin";
  table: RecordCatalogTable;
  object: RecordPolicyRegistryObject;
  fields: RecordPolicyField[];
  permission: RecordPolicyPermission | undefined;
  app: RecordPolicyApp | undefined;
  orgId: string;
}): GraphjinRoleTable {
  const { role, table, object, fields, permission, app, orgId } = input;
  if (
    object.archived ||
    app?.status !== "active" ||
    permission?.canRead !== true ||
    !hasColumns(table, [RECORD_SYSTEM_COLUMNS.orgId, RECORD_SYSTEM_COLUMNS.deletedAt]) ||
    (role === "member" &&
      object.visibility === "owner" &&
      !hasColumns(table, [RECORD_SYSTEM_COLUMNS.ownerUserId]))
  ) {
    return blockedTable(table);
  }

  const columns = readableColumns({ table, object, fields });
  // In GraphJin an empty columns list means unrestricted. A malformed or
  // entirely archived field projection therefore blocks instead of widening.
  if (columns.length === 0) return blockedTable(table);

  const filters = [orgFilter(orgId), notDeletedFilter()];
  if (role === "member" && object.visibility === "owner") {
    filters.push(ownerFilter());
  }

  return {
    ...blockedTable(table),
    query: {
      block: false,
      filters,
      columns,
      limit: HUMAN_QUERY_LIMIT,
      disable_functions: false,
    },
  };
}

function humanRecycleTable(input: {
  role: "member" | "admin";
  table: RecordCatalogTable;
  model: RecordsGraphjinPolicyModel;
}): GraphjinRoleTable {
  const { role, table, model } = input;
  if (
    !hasColumns(table, [
      RECORD_SYSTEM_COLUMNS.orgId,
      "scope_key",
      "visibility",
      ...RECORD_RECYCLE_READ_COLUMNS,
    ])
  ) {
    return blockedTable(table);
  }

  const activeApps = new Set(
    model.apps
      .filter((app) => app.orgId === model.orgId && app.status === "active")
      .map((app) => app.appId),
  );
  const readableObjects = model.objects
    .filter((object) => {
      if (object.archived || !activeApps.has(object.appId)) return false;
      return model.permissions.some(
        (permission) =>
          permission.role === role &&
          permission.appId === object.appId &&
          permission.objectApiName === object.apiName &&
          permission.canRead,
      );
    })
    .sort((left, right) =>
      `${left.appId}\u0000${left.apiName}`.localeCompare(
        `${right.appId}\u0000${right.apiName}`,
      ),
    );
  if (readableObjects.length === 0) return blockedTable(table);

  const scopeKeys = readableObjects.map((object) => `${object.appId}.${object.apiName}`);
  const filters = [
    orgFilter(model.orgId),
    `{ scope_key: { in: [${scopeKeys.map(gqlString).join(", ")}] } }`,
  ];
  if (role === "member") {
    filters.push(
      `{ or: { visibility: { eq: "org" }, owner_user_id: { eq: $user_id } } }`,
    );
  }

  return {
    ...blockedTable(table),
    query: {
      block: false,
      filters,
      columns: [...RECORD_RECYCLE_READ_COLUMNS],
      limit: HUMAN_QUERY_LIMIT,
      disable_functions: false,
    },
  };
}

function adminIdentityTable(
  table: RecordCatalogTable,
  orgId: string,
): GraphjinRoleTable {
  if (
    !hasColumns(table, [
      RECORD_SYSTEM_COLUMNS.orgId,
      ...RECORD_IDENTITY_READ_COLUMNS,
    ])
  ) {
    return blockedTable(table);
  }

  return {
    ...blockedTable(table),
    query: {
      block: false,
      filters: [orgFilter(orgId)],
      columns: [...RECORD_IDENTITY_READ_COLUMNS],
      limit: HUMAN_QUERY_LIMIT,
      disable_functions: false,
    },
  };
}

function humanChangeLogTable(input: {
  role: "admin" | "member";
  table: RecordCatalogTable;
  model: RecordsGraphjinPolicyModel;
}): GraphjinRoleTable {
  const { role, table, model } = input;
  if (
    !hasColumns(table, [
      RECORD_SYSTEM_COLUMNS.orgId,
      RECORD_SYSTEM_COLUMNS.ownerUserId,
      ...RECORD_CHANGE_LOG_READ_COLUMNS,
    ])
  ) {
    return blockedTable(table);
  }
  const readable = model.objects.filter((object) => {
    const app = model.apps.find((candidate) => candidate.appId === object.appId);
    return (
      !object.archived &&
      app?.status === "active" &&
      model.permissions.some(
        (permission) =>
          permission.role === role &&
          permission.appId === object.appId &&
          permission.objectApiName === object.apiName &&
          permission.canRead,
      )
    );
  });
  if (readable.length === 0) return blockedTable(table);
  const scopes = readable.map((object) => {
    const clauses = [
      `{ app_id: { eq: ${gqlString(object.appId)} } }`,
      `{ object_api_name: { eq: ${gqlString(object.apiName)} } }`,
    ];
    if (role === "member" && object.visibility === "owner") {
      clauses.push(`{ owner_user_id: { eq: $user_id } }`);
    }
    return `{ and: [${clauses.join(", ")}] }`;
  });
  const scopeFilter =
    scopes.length === 1 ? scopes[0]! : `{ or: [${scopes.join(", ")}] }`;
  return {
    ...blockedTable(table),
    query: {
      block: false,
      filters: [
        orgFilter(model.orgId),
        scopeFilter,
      ],
      columns: [...RECORD_CHANGE_LOG_READ_COLUMNS],
      limit: HUMAN_QUERY_LIMIT,
      disable_functions: true,
    },
  };
}

function adminSchemaLogTable(
  table: RecordCatalogTable,
  orgId: string,
): GraphjinRoleTable {
  if (!hasColumns(table, [RECORD_SYSTEM_COLUMNS.orgId, ...RECORD_SCHEMA_LOG_READ_COLUMNS])) {
    return blockedTable(table);
  }
  return {
    ...blockedTable(table),
    query: {
      block: false,
      filters: [orgFilter(orgId)],
      columns: [...RECORD_SCHEMA_LOG_READ_COLUMNS],
      limit: HUMAN_QUERY_LIMIT,
      disable_functions: true,
    },
  };
}

function serviceTable(input: {
  table: RecordCatalogTable;
  object: RecordPolicyRegistryObject;
  app: RecordPolicyApp | undefined;
  orgId: string;
}): GraphjinRoleTable {
  const { table, object, app, orgId } = input;
  const serviceStatuses: ReadonlySet<RecordAppStatus> = new Set([
    "provisioning",
    "importing",
    "active",
    "degraded",
  ]);
  if (
    object.archived ||
    !app ||
    !serviceStatuses.has(app.status) ||
    !hasColumns(table, [RECORD_SYSTEM_COLUMNS.orgId, RECORD_SYSTEM_COLUMNS.deletedAt])
  ) {
    return blockedTable(table);
  }

  const queryColumns = uniqueSorted(table.columns);
  const insertColumns = uniqueSorted(serviceInsertColumns(table));
  const updateColumns = uniqueSorted(serviceUpdateColumns(table));
  if (queryColumns.length === 0 || insertColumns.length === 0 || updateColumns.length === 0) {
    return blockedTable(table);
  }

  return {
    name: table.name,
    schema: table.schema,
    query: {
      block: false,
      filters: [orgFilter(orgId), notDeletedFilter()],
      columns: queryColumns,
      limit: SERVICE_QUERY_LIMIT,
      disable_functions: false,
    },
    insert: {
      block: false,
      filters: [orgFilter(orgId), notDeletedFilter()],
      columns: insertColumns,
      presets: servicePresets(table, orgId, "insert"),
    },
    update: {
      block: false,
      // The executor always supplies the deleted-row predicate: ordinary
      // writes target live rows and record_restore deliberately targets the
      // recycle bin. The invariant GraphJin must enforce in both cases is org.
      filters: [orgFilter(orgId)],
      columns: updateColumns,
      presets: servicePresets(table, orgId, "update"),
    },
    // C4 has no upsert or hard-delete path. Keep both closed until a typed
    // action owns their semantics instead of exposing unused write surface.
    upsert: blockedOperation(),
    delete: blockedOperation(),
  };
}

function assertPolicyModel(model: RecordsGraphjinPolicyModel): void {
  if (!model.orgId) throw new Error("records policy projection requires an org id");

  for (const app of model.apps) {
    if (app.orgId !== model.orgId) {
      throw new Error(`cross-org registry app in policy projection: ${app.appId}`);
    }
  }

  const catalogKeys = new Set<string>();
  for (const table of model.catalog) {
    const key = relationKey(table.schema, table.name);
    if (catalogKeys.has(key)) throw new Error(`duplicate live catalog relation: ${table.schema}.${table.name}`);
    catalogKeys.add(key);
    if (table.columns.length === 0) {
      throw new Error(`live catalog relation has no columns: ${table.schema}.${table.name}`);
    }
  }

  const registeredKeys = new Set<string>();
  for (const object of model.objects) {
    if (object.orgId !== model.orgId) {
      throw new Error(`cross-org registry object in policy projection: ${object.apiName}`);
    }
    const key = relationKey(object.tableSchema, object.tableName);
    if (registeredKeys.has(key)) {
      throw new Error(`duplicate registered relation: ${object.tableSchema}.${object.tableName}`);
    }
    registeredKeys.add(key);
  }
}

/**
 * Project live catalog + registry policy into GraphJin's explicit legacy role
 * table model. Every role receives every catalog relation and every operation,
 * because omission is unrestricted for authenticated roles in GraphJin.
 */
export function projectRecordsGraphjinRoles(model: RecordsGraphjinPolicyModel): GraphjinRole[] {
  assertPolicyModel(model);
  const objectByRelation = new Map(
    model.objects.map((object) => [
      relationKey(object.tableSchema, object.tableName),
      object,
    ]),
  );
  const appById = new Map(model.apps.map((app) => [app.appId, app]));
  const permissionByRoleObject = new Map(
    model.permissions.map((permission) => [
      `${permission.role}\u0000${permission.appId}\u0000${permission.objectApiName}`,
      permission,
    ]),
  );
  const catalog = [...model.catalog].sort((left, right) =>
    relationKey(left.schema, left.name).localeCompare(relationKey(right.schema, right.name)),
  );

  return RECORD_GRAPHJIN_ROLES.map((role): GraphjinRole => {
    const tables = catalog.map((table): GraphjinRoleTable => {
      if (
        table.schema === RECORD_RECYCLE_RELATION.schema &&
        table.name === RECORD_RECYCLE_RELATION.table
      ) {
        return role === "admin" || role === "member"
          ? humanRecycleTable({ role, table, model })
          : blockedTable(table);
      }
      if (
        table.schema === RECORD_IDENTITY_RELATION.schema &&
        table.name === RECORD_IDENTITY_RELATION.table
      ) {
        return role === "admin"
          ? adminIdentityTable(table, model.orgId)
          : blockedTable(table);
      }
      if (
        table.schema === RECORD_CHANGE_LOG_RELATION.schema &&
        table.name === RECORD_CHANGE_LOG_RELATION.table
      ) {
        return role === "admin" || role === "member"
          ? humanChangeLogTable({ role, table, model })
          : blockedTable(table);
      }
      if (
        table.schema === RECORD_SCHEMA_LOG_RELATION.schema &&
        table.name === RECORD_SCHEMA_LOG_RELATION.table
      ) {
        return role === "admin"
          ? adminSchemaLogTable(table, model.orgId)
          : blockedTable(table);
      }
      const object = objectByRelation.get(relationKey(table.schema, table.name));
      // Other engine, unknown/orphaned, and just-created relations have no
      // registry object and are blocked for all roles.
      if (!object) return blockedTable(table);

      const app = appById.get(object.appId);
      if (role === "admin" || role === "member") {
        return humanTable({
          role,
          table,
          object,
          fields: model.fields,
          permission: permissionByRoleObject.get(
            `${role}\u0000${object.appId}\u0000${object.apiName}`,
          ),
          app,
          orgId: model.orgId,
        });
      }
      if (role === "service") {
        return serviceTable({ table, object, app, orgId: model.orgId });
      }
      return blockedTable(table);
    });

    if (role === "member" || role === "admin" || role === "service") {
      return { name: role, match: `role = '${role}'`, tables };
    }
    return { name: role, tables };
  });
}

/**
 * Take a repeatable-read snapshot of the live GraphJin-visible catalog and
 * the registry rows that may narrow it. The catalog—not the registry—defines
 * the universe, so orphan and engine relations cannot disappear by omission.
 */
export async function loadRecordsGraphjinPolicyModel(
  pool: Pool,
  orgId: string,
): Promise<RecordsGraphjinPolicyModel> {
  if (!orgId) throw new Error("records policy projection requires an org id");
  const client = await pool.connect();
  try {
    await client.query("begin isolation level repeatable read read only");
    const catalogResult = await client.query<CatalogRow>(`
      select n.nspname as table_schema,
             c.relname as table_name,
             array_agg(a.attname::text order by a.attnum) as columns
      from pg_catalog.pg_class c
      join pg_catalog.pg_namespace n on n.oid = c.relnamespace
      join pg_catalog.pg_attribute a on a.attrelid = c.oid
        and a.attnum > 0 and not a.attisdropped
      where c.relkind in ('r', 'p', 'v', 'm', 'f')
        and n.nspname <> 'information_schema'
        and n.nspname !~ '^pg_'
      group by n.nspname, c.relname
      order by n.nspname, c.relname
    `);
    const appResult = await client.query<AppRow>(
      `select org_id, app_id, status
       from engine.record_app where org_id = $1 order by app_id`,
      [orgId],
    );
    const objectResult = await client.query<ObjectRow>(
      `select id, org_id, app_id, api_name, table_schema, table_name,
              name_field, visibility, archived_at
       from engine.record_object where org_id = $1 order by app_id, api_name`,
      [orgId],
    );
    const fieldResult = await client.query<FieldRow>(
      `select f.object_id, f.column_name, f.archived_at
       from engine.record_field f
       join engine.record_object o on o.id = f.object_id and o.org_id = f.org_id
       where o.org_id = $1 order by f.object_id, f.column_name`,
      [orgId],
    );
    const permissions = await loadRecordPolicyGrants(client, { orgId });

    const model: RecordsGraphjinPolicyModel = {
      orgId,
      catalog: catalogResult.rows.map((row) => ({
        schema: validateRecordIdentifier(row.table_schema),
        name: validateRecordIdentifier(row.table_name),
        columns: row.columns.map(validateRecordIdentifier),
      })),
      apps: appResult.rows.map((row) => ({
        orgId: row.org_id,
        appId: row.app_id,
        status: row.status,
      })),
      objects: objectResult.rows.map((row) => ({
        id: row.id,
        orgId: row.org_id,
        appId: row.app_id,
        apiName: validateRecordIdentifier(row.api_name),
        tableSchema: validateRecordIdentifier(row.table_schema),
        tableName: validateRecordIdentifier(row.table_name),
        nameField: validateRecordIdentifier(row.name_field),
        visibility: row.visibility,
        archived: row.archived_at !== null,
      })),
      fields: fieldResult.rows.map((row) => ({
        objectId: row.object_id,
        columnName: validateRecordIdentifier(row.column_name),
        archived: row.archived_at !== null,
      })),
      permissions,
    };
    await client.query("commit");
    assertPolicyModel(model);
    return model;
  } catch (error) {
    await client.query("rollback");
    throw error;
  } finally {
    client.release();
  }
}
