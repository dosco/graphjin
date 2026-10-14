import type { Pool, PoolClient } from "pg";
import type { RecordsGraphjinTransport } from "../graphjin/client";
import { RECORD_DATA_COLUMN, RECORD_SYSTEM_COLUMNS } from "../policy/graphjin";
import { RecordRegistry } from "../registry";

export type ImportedSourceUser = {
  sourceUserId: string;
  email: string;
  name?: string | null;
  active?: boolean | null;
};

export type ImportedSourceUsersResult = {
  found: boolean;
  users: ImportedSourceUser[];
  invalidEmails: number;
  invalidEmailSourceUserIds: string[];
};

export type IdentityAppUser = {
  id: string;
  email: string;
  disabled?: boolean;
};

export type IdentityMappingStatus = "linked" | "unlinked" | "conflict" | "ignored";

export type IdentityMapping = {
  orgId: string;
  sourceInstanceId: string;
  appId: string;
  sourceUserId: string;
  sourceEmail: string;
  sourceName: string | null;
  sourceIsActive: boolean | null;
  appUserId: string | null;
  status: IdentityMappingStatus;
  linkedAt: Date | null;
  updatedAt: Date;
};

export type IdentityReconciliationResult = {
  mappings: IdentityMapping[];
  linked: number;
  unlinked: number;
  conflicts: number;
  ignored: number;
};

export type LinkedIdentityScope = {
  appId: string;
  sourceInstanceId: string;
  sourceUserId: string;
};

type RawIdentity = {
  org_id: string;
  source_instance_id: string;
  app_id: string;
  source_user_id: string;
  source_email: string;
  source_name: string | null;
  source_is_active: boolean | null;
  app_user_id: string | null;
  status: IdentityMappingStatus;
  linked_at: Date | null;
  updated_at: Date;
};

const IDENTITY_COLUMNS = `org_id, source_instance_id, app_id, source_user_id,
  source_email, source_name, source_is_active, app_user_id, status, linked_at,
  updated_at`;

export class IdentityMappingError extends Error {
  readonly code = "records_identity_mapping_invalid";

  constructor(message: string) {
    super(message);
    this.name = "IdentityMappingError";
  }
}

function required(value: string, label: string, max = 500): string {
  if (typeof value !== "string" || !value.trim() || value.trim().length > max) {
    throw new IdentityMappingError(`${label}: non-empty string required`);
  }
  return value.trim();
}

function normalizedEmail(value: string): string {
  const email = required(value, "source email", 320).toLocaleLowerCase("en-US");
  if (!/^[^\s@]+@[^\s@]+$/.test(email)) {
    throw new IdentityMappingError("source email: valid address required");
  }
  return email;
}

function sourceField(
  fields: Array<{ sourceApiName: string | null; apiName: string; columnName: string }>,
  name: string,
): string | null {
  const lower = name.toLocaleLowerCase("en-US");
  return (
    fields.find(
      (field) =>
        field.sourceApiName?.toLocaleLowerCase("en-US") === lower ||
        field.apiName === lower,
    )?.columnName ?? null
  );
}

/** Read the imported Salesforce User object through its validated registry projection. */
export async function readImportedSourceUsers(
  pool: Pool,
  input: {
    orgId: string;
    appId: string;
    graphjin: RecordsGraphjinTransport;
    token: string;
    sourceObject?: string;
  },
): Promise<ImportedSourceUsersResult> {
  const orgId = required(input.orgId, "organization id");
  const appId = required(input.appId, "app id");
  const sourceObject = input.sourceObject ?? "User";
  const snapshot = await new RecordRegistry(pool).loadApp(orgId, appId);
  if (!snapshot) throw new IdentityMappingError("imported identity app was not found");
  const object = snapshot.objects.find(
    (candidate) =>
      candidate.archivedAt === null &&
      candidate.sourceApiName?.toLocaleLowerCase("en-US") ===
        sourceObject.toLocaleLowerCase("en-US"),
  );
  if (!object) {
    return {
      found: false,
      users: [],
      invalidEmails: 0,
      invalidEmailSourceUserIds: [],
    };
  }
  const fields = snapshot.fields.filter(
    (field) => field.objectId === object.id && field.archivedAt === null,
  );
  const emailColumn = sourceField(fields, "Email");
  if (!emailColumn) {
    throw new IdentityMappingError("imported Salesforce User object has no Email field");
  }
  const nameColumn = sourceField(fields, "Name");
  const activeColumn = sourceField(fields, "IsActive");
  const rows: Array<Record<string, unknown>> = [];
  let after: string | null = null;
  do {
    const operationName = "RecordsImportedSourceUsers";
    const data: Record<string, unknown> = await input.graphjin.execute<
      Record<string, unknown>
    >({
      operationName,
      query: `query ${operationName} { rows: ${object.tableName}(where: { and: [{ ${RECORD_SYSTEM_COLUMNS.orgId}: { eq: $org_id } }, { ${RECORD_SYSTEM_COLUMNS.deletedAt}: { is_null: true } }${after ? ", { id: { gt: $after } }" : ""}] }, order_by: { id: asc }, limit: 500) { id ${RECORD_DATA_COLUMN} } }`,
      variables: { org_id: orgId, ...(after ? { after } : {}) },
      token: input.token,
    });
    const page: Array<Record<string, unknown>> = Array.isArray(data.rows)
      ? data.rows.filter(
          (row: unknown): row is Record<string, unknown> =>
            typeof row === "object" && row !== null && !Array.isArray(row),
        )
      : [];
    rows.push(
      ...page.map((row) => {
        const document = row[RECORD_DATA_COLUMN];
        return {
          ...(document && typeof document === "object" && !Array.isArray(document)
            ? (document as Record<string, unknown>)
            : {}),
          id: row.id,
        };
      }),
    );
    const lastId = page.at(-1)?.id;
    after = page.length === 500 && typeof lastId === "string" ? lastId : null;
  } while (after);
  const users: ImportedSourceUser[] = [];
  const invalidEmailSourceUserIds: string[] = [];
  let invalidEmails = 0;
  for (const row of rows) {
    const sourceUserId = typeof row.id === "string" ? row.id : "";
    if (!sourceUserId) {
      throw new IdentityMappingError("imported Salesforce User row has no id");
    }
    const emailValue = row[emailColumn];
    const email = typeof emailValue === "string" ? emailValue.trim() : "";
    if (!email || !/^[^\s@]+@[^\s@]+$/.test(email) || email.length > 320) {
      invalidEmails += 1;
      if (invalidEmailSourceUserIds.length < 100) {
        invalidEmailSourceUserIds.push(sourceUserId);
      }
      continue;
    }
    users.push({
      sourceUserId,
      email,
      name:
        nameColumn && typeof row[nameColumn] === "string"
          ? String(row[nameColumn])
          : null,
      active:
        activeColumn && typeof row[activeColumn] === "boolean"
          ? Boolean(row[activeColumn])
          : null,
    });
  }
  return {
    found: true,
    users,
    invalidEmails,
    invalidEmailSourceUserIds,
  };
}

function mapIdentity(row: RawIdentity): IdentityMapping {
  return {
    orgId: row.org_id,
    sourceInstanceId: row.source_instance_id,
    appId: row.app_id,
    sourceUserId: row.source_user_id,
    sourceEmail: row.source_email,
    sourceName: row.source_name,
    sourceIsActive: row.source_is_active,
    appUserId: row.app_user_id,
    status: row.status,
    linkedAt: row.linked_at,
    updatedAt: row.updated_at,
  };
}

async function transaction<T>(client: PoolClient, operation: () => Promise<T>): Promise<T> {
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

function summarize(mappings: IdentityMapping[]): IdentityReconciliationResult {
  return {
    mappings,
    linked: mappings.filter((mapping) => mapping.status === "linked").length,
    unlinked: mappings.filter((mapping) => mapping.status === "unlinked").length,
    conflicts: mappings.filter((mapping) => mapping.status === "conflict").length,
    ignored: mappings.filter((mapping) => mapping.status === "ignored").length,
  };
}

/** Reconcile one imported source-user set against active OpenNeko users. */
export async function reconcileImportedIdentities(
  pool: Pool,
  input: {
    orgId: string;
    sourceInstanceId: string;
    appId: string;
    sourceUsers: ImportedSourceUser[];
    appUsers: IdentityAppUser[];
  },
): Promise<IdentityReconciliationResult> {
  const orgId = required(input.orgId, "organization id");
  const sourceInstanceId = required(input.sourceInstanceId, "source instance id");
  const appId = required(input.appId, "app id");
  const sourceIds = input.sourceUsers.map((user) => required(user.sourceUserId, "source user id"));
  if (new Set(sourceIds).size !== sourceIds.length) {
    throw new IdentityMappingError("source user ids must be unique within an import");
  }
  const activeUsersByEmail = new Map<string, IdentityAppUser[]>();
  const activeUsersById = new Map<string, IdentityAppUser>();
  for (const user of input.appUsers) {
    if (user.disabled) continue;
    const id = required(user.id, "app user id");
    const email = normalizedEmail(user.email);
    activeUsersById.set(id, { ...user, id, email });
    activeUsersByEmail.set(email, [
      ...(activeUsersByEmail.get(email) ?? []),
      { ...user, id, email },
    ]);
  }

  const client = await pool.connect();
  try {
    return await transaction(client, async () => {
      const existing = await client.query<RawIdentity>(
        `select ${IDENTITY_COLUMNS}
         from engine.identity_map
         where org_id = $1 and source_instance_id = $2 and app_id = $3
         for update`,
        [orgId, sourceInstanceId, appId],
      );
      const existingBySource = new Map(
        existing.rows.map((row) => [row.source_user_id, mapIdentity(row)]),
      );
      const proposed = input.sourceUsers.map((sourceUser, index) => {
        const sourceUserId = sourceIds[index]!;
        const email = normalizedEmail(sourceUser.email);
        const previous = existingBySource.get(sourceUserId);
        if (previous?.status === "ignored") {
          return { sourceUser, sourceUserId, email, status: "ignored" as const, appUserId: null };
        }
        const stable =
          previous?.status === "linked" && previous.appUserId
            ? activeUsersById.get(previous.appUserId)
            : undefined;
        const matches = stable ? [stable] : activeUsersByEmail.get(email) ?? [];
        return {
          sourceUser,
          sourceUserId,
          email,
          status:
            matches.length === 1
              ? ("linked" as const)
              : matches.length > 1
                ? ("conflict" as const)
                : ("unlinked" as const),
          appUserId: matches.length === 1 ? matches[0]!.id : null,
        };
      });

      const claims = new Map<string, number>();
      const importedSourceIds = new Set(sourceIds);
      for (const mapping of existingBySource.values()) {
        if (
          !importedSourceIds.has(mapping.sourceUserId) &&
          mapping.status === "linked" &&
          mapping.appUserId
        ) {
          claims.set(mapping.appUserId, (claims.get(mapping.appUserId) ?? 0) + 1);
        }
      }
      for (const candidate of proposed) {
        if (candidate.status === "linked" && candidate.appUserId) {
          claims.set(candidate.appUserId, (claims.get(candidate.appUserId) ?? 0) + 1);
        }
      }
      for (const candidate of proposed) {
        if (
          candidate.status === "linked" &&
          candidate.appUserId &&
          (claims.get(candidate.appUserId) ?? 0) > 1
        ) {
          candidate.status = "conflict";
          candidate.appUserId = null;
        }
      }

      const mappings: IdentityMapping[] = [];
      for (const candidate of proposed) {
        const result = await client.query<RawIdentity>(
          `insert into engine.identity_map
             (org_id, source_instance_id, app_id, source_user_id,
              source_email, source_name, source_is_active, app_user_id,
              status, linked_at, updated_at)
           values ($1, $2, $3, $4, $5, $6, $7, $8, $9,
                   case when $9 = 'linked' then now() else null end, now())
           on conflict (org_id, source_instance_id, app_id, source_user_id)
           do update set
             source_email = excluded.source_email,
             source_name = excluded.source_name,
             source_is_active = excluded.source_is_active,
             app_user_id = excluded.app_user_id,
             status = excluded.status,
             linked_at = case
               when excluded.status = 'linked'
                 then coalesce(engine.identity_map.linked_at, now())
               else null
             end,
             updated_at = now()
           returning ${IDENTITY_COLUMNS}`,
          [
            orgId,
            sourceInstanceId,
            appId,
            candidate.sourceUserId,
            candidate.email,
            candidate.sourceUser.name?.trim() || null,
            candidate.sourceUser.active ?? null,
            candidate.appUserId,
            candidate.status,
          ],
        );
        mappings.push(mapIdentity(result.rows[0]!));
      }
      return summarize(mappings);
    });
  } finally {
    client.release();
  }
}

export async function listIdentityMappings(
  pool: Pool,
  input: {
    orgId: string;
    appId: string;
    sourceInstanceId?: string;
    status?: IdentityMappingStatus;
  },
): Promise<IdentityMapping[]> {
  const orgId = required(input.orgId, "organization id");
  const appId = required(input.appId, "app id");
  const values: unknown[] = [orgId, appId];
  const clauses = ["org_id = $1", "app_id = $2"];
  if (input.sourceInstanceId) {
    values.push(required(input.sourceInstanceId, "source instance id"));
    clauses.push(`source_instance_id = $${values.length}`);
  }
  if (input.status) {
    values.push(input.status);
    clauses.push(`status = $${values.length}`);
  }
  const result = await pool.query<RawIdentity>(
    `select ${IDENTITY_COLUMNS} from engine.identity_map
     where ${clauses.join(" and ")}
     order by source_email_normalized, source_instance_id, source_user_id`,
    values,
  );
  return result.rows.map(mapIdentity);
}

/** Apply an explicit admin link/ignore decision to one exact scoped identity. */
export async function decideIdentityMapping(
  pool: Pool,
  input: {
    orgId: string;
    sourceInstanceId: string;
    appId: string;
    sourceUserId: string;
    decision: "link" | "ignore";
    appUserId?: string;
  },
): Promise<IdentityMapping> {
  if (input.decision !== "link" && input.decision !== "ignore") {
    throw new IdentityMappingError("decision must be link or ignore");
  }
  const orgId = required(input.orgId, "organization id");
  const sourceInstanceId = required(input.sourceInstanceId, "source instance id");
  const appId = required(input.appId, "app id");
  const sourceUserId = required(input.sourceUserId, "source user id");
  const appUserId =
    input.decision === "link" ? required(input.appUserId ?? "", "app user id") : null;
  const client = await pool.connect();
  try {
    return await transaction(client, async () => {
      if (appUserId) {
        const occupied = await client.query(
          `select 1 from engine.identity_map
           where org_id = $1 and source_instance_id = $2 and app_id = $3
             and app_user_id = $4 and status = 'linked' and source_user_id <> $5
           limit 1`,
          [
            orgId,
            sourceInstanceId,
            appId,
            appUserId,
            sourceUserId,
          ],
        );
        if (occupied.rowCount) {
          throw new IdentityMappingError(
            "app user is already linked to another user in this source instance",
          );
        }
      }
      const result = await client.query<RawIdentity>(
        `update engine.identity_map
         set app_user_id = $5,
             status = $6,
             linked_at = case when $6 = 'linked' then now() else null end,
             updated_at = now()
         where org_id = $1 and source_instance_id = $2 and app_id = $3
           and source_user_id = $4
         returning ${IDENTITY_COLUMNS}`,
        [
          orgId,
          sourceInstanceId,
          appId,
          sourceUserId,
          appUserId,
          input.decision === "link" ? "linked" : "ignored",
        ],
      );
      if (!result.rows[0]) throw new IdentityMappingError("source identity was not found");
      return mapIdentity(result.rows[0]);
    });
  } finally {
    client.release();
  }
}

/** Exact app/source identities currently attached to one OpenNeko user. */
export async function listLinkedIdentityScopesForUser(
  pool: Pool,
  input: { orgId: string; appUserId: string },
): Promise<LinkedIdentityScope[]> {
  const orgId = required(input.orgId, "organization id");
  const appUserId = required(input.appUserId, "app user id");
  const result = await pool.query<{
    app_id: string;
    source_instance_id: string;
    source_user_id: string;
  }>(
    `select app_id, source_instance_id, source_user_id
     from engine.identity_map
     where org_id = $1 and app_user_id = $2 and status = 'linked'
     order by app_id, source_instance_id, source_user_id`,
    [orgId, appUserId],
  );
  return result.rows.map((row) => ({
    appId: row.app_id,
    sourceInstanceId: row.source_instance_id,
    sourceUserId: row.source_user_id,
  }));
}

/** Lazy SSO hook: link only unambiguous matches, flag shared mailboxes. */
export async function linkIdentityMappingsForUser(
  pool: Pool,
  input: { orgId: string; appUserId: string; email: string },
): Promise<{ linked: number; conflicts: number }> {
  const email = normalizedEmail(input.email);
  const client = await pool.connect();
  try {
    return await transaction(client, async () => {
      const candidates = await client.query<RawIdentity>(
        `select ${IDENTITY_COLUMNS}
         from engine.identity_map
         where org_id = $1 and source_email_normalized = $2 and status <> 'ignored'
         order by source_instance_id, app_id, source_user_id
         for update`,
        [input.orgId, email],
      );
      const groups = new Map<string, RawIdentity[]>();
      for (const row of candidates.rows) {
        const key = `${row.source_instance_id}\u0000${row.app_id}`;
        groups.set(key, [...(groups.get(key) ?? []), row]);
      }
      let linked = 0;
      let conflicts = 0;
      for (const rows of groups.values()) {
        if (rows.length !== 1) {
          const result = await client.query(
            `update engine.identity_map set app_user_id = null, status = 'conflict',
                    linked_at = null, updated_at = now()
             where org_id = $1 and source_instance_id = $2 and app_id = $3
               and source_user_id = any($4::text[])`,
            [
              input.orgId,
              rows[0]!.source_instance_id,
              rows[0]!.app_id,
              rows.map((row) => row.source_user_id),
            ],
          );
          conflicts += result.rowCount ?? 0;
          continue;
        }
        const row = rows[0]!;
        const occupied = await client.query(
          `select 1 from engine.identity_map
           where org_id = $1 and source_instance_id = $2 and app_id = $3
             and app_user_id = $4 and status = 'linked' and source_user_id <> $5
           limit 1`,
          [input.orgId, row.source_instance_id, row.app_id, input.appUserId, row.source_user_id],
        );
        if (occupied.rowCount) {
          await client.query(
            `update engine.identity_map set app_user_id = null, status = 'conflict',
                    linked_at = null, updated_at = now()
             where org_id = $1 and source_instance_id = $2 and app_id = $3
               and source_user_id = $4`,
            [input.orgId, row.source_instance_id, row.app_id, row.source_user_id],
          );
          conflicts += 1;
          continue;
        }
        await client.query(
          `update engine.identity_map set app_user_id = $5, status = 'linked',
                  linked_at = coalesce(linked_at, now()), updated_at = now()
           where org_id = $1 and source_instance_id = $2 and app_id = $3
             and source_user_id = $4`,
          [input.orgId, row.source_instance_id, row.app_id, row.source_user_id, input.appUserId],
        );
        linked += 1;
      }
      return { linked, conflicts };
    });
  } finally {
    client.release();
  }
}
