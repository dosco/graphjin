import { sql } from "drizzle-orm";
import { groupHolds } from "./entitlements";
import { GroupError } from "./groups";
import { db } from "./index";
import { ADMINISTRATORS_GROUP_SLUG } from "./schema";

export type DataAccessRuleRow = {
  id: string;
  groupId: string;
  groupSlug: string;
  groupName: string;
  source: string;
  tableSchema: string;
  tableName: string;
  columns: string[];
  rowFilter: unknown;
  updatedAt: Date;
};

function rows<T>(result: unknown): T[] {
  return (result as { rows: T[] }).rows;
}

const RULE_COLUMNS = sql`
  r.id, r.group_id as "groupId", g.slug as "groupSlug", g.name as "groupName", r.source,
  r.table_schema as "tableSchema", r.table_name as "tableName", r.columns, r.row_filter as "rowFilter",
  r.updated_at as "updatedAt"`;

export async function listDataAccessRules(orgId: string, groupId?: string): Promise<DataAccessRuleRow[]> {
  return rows<DataAccessRuleRow>(
    await db().execute(sql`
      select ${RULE_COLUMNS}
      from data_access_rule r join user_group g on g.id = r.group_id
      where r.org_id = ${orgId} and (${groupId ?? null}::uuid is null or r.group_id = ${groupId ?? null}::uuid)
      order by lower(g.name), r.source, r.table_schema, r.table_name`),
  );
}

export async function upsertDataAccessRule(
  orgId: string,
  input: {
    groupId: string;
    source: string;
    tableSchema?: string;
    tableName: string;
    columns: string[];
    rowFilter: unknown;
    actorUserId?: string | null;
  },
): Promise<DataAccessRuleRow> {
  const [group] = rows<{ slug: string }>(
    await db().execute(sql`select slug from user_group where org_id = ${orgId} and id = ${input.groupId}`),
  );
  if (!group) throw new GroupError("not_found", "group not found");
  if (group.slug === ADMINISTRATORS_GROUP_SLUG) {
    throw new GroupError("builtin", "Administrators read every table; data access rules are not needed");
  }
  const columns = [...new Set(input.columns.map((c) => c.trim()).filter(Boolean))];
  if (columns.length === 0) throw new GroupError("invalid", "choose at least one column");
  if (!/^[A-Za-z_][A-Za-z0-9_$]*$/.test(input.tableName)) throw new GroupError("invalid", "invalid table name");
  const [row] = rows<{ id: string }>(await db().execute(sql`
    insert into data_access_rule (org_id, group_id, source, table_schema, table_name, columns, row_filter, created_by_user_id)
    values (${orgId}, ${input.groupId}, ${input.source}, ${input.tableSchema ?? ""}, ${input.tableName},
      ${`{${columns.map((c) => `"${c}"`).join(",")}}`}::text[], ${input.rowFilter == null ? null : JSON.stringify(input.rowFilter)}::jsonb,
      ${input.actorUserId ?? null})
    on conflict (org_id, group_id, source, table_schema, table_name)
    do update set columns = excluded.columns, row_filter = excluded.row_filter, updated_at = now()
    returning id`));
  return (await listDataAccessRules(orgId, input.groupId)).find((r) => r.id === row!.id)!;
}

export async function deleteDataAccessRule(orgId: string, ruleId: string): Promise<boolean> {
  const deleted = rows<{ id: string }>(
    await db().execute(sql`delete from data_access_rule where org_id = ${orgId} and id = ${ruleId} returning id`),
  );
  return deleted.length > 0;
}

export async function getGroupGrantsEnabled(orgId: string): Promise<boolean> {
  const [row] = rows<{ enabled: boolean }>(
    await db().execute(sql`select group_grants_enabled as enabled from data_access_settings where org_id = ${orgId}`),
  );
  return row?.enabled ?? false;
}

export async function setGroupGrantsEnabled(
  orgId: string,
  enabled: boolean,
  actorUserId: string | null,
  previousReadModes?: Record<string, string>,
): Promise<void> {
  await db().execute(sql`
    insert into data_access_settings (org_id, group_grants_enabled, enabled_by_user_id, previous_read_modes, updated_at)
    values (${orgId}, ${enabled}, ${actorUserId}, ${JSON.stringify(previousReadModes ?? {})}::jsonb, now())
    on conflict (org_id) do update set group_grants_enabled = excluded.group_grants_enabled,
      enabled_by_user_id = excluded.enabled_by_user_id,
      previous_read_modes = case when ${previousReadModes === undefined} then data_access_settings.previous_read_modes else excluded.previous_read_modes end,
      updated_at = now()`);
}

/** Stores read modes of sources seen for the first time. Stored modes stay. */
export async function recordPreviousReadModes(orgId: string, modes: Record<string, string>): Promise<void> {
  await db().execute(sql`
    update data_access_settings set previous_read_modes = ${JSON.stringify(modes)}::jsonb || previous_read_modes, updated_at = now()
    where org_id = ${orgId}`);
}

export async function getPreviousReadModes(orgId: string): Promise<Record<string, string>> {
  const [row] = rows<{ modes: Record<string, string> }>(
    await db().execute(sql`select previous_read_modes as modes from data_access_settings where org_id = ${orgId}`),
  );
  return row?.modes ?? {};
}

/**
 * Upgrade parity: when group grants turn on, Everyone reads every existing
 * table and column of each source, as members did before. Sources where
 * Everyone already has rules are left alone.
 */
export async function seedEveryoneDataAccess(
  orgId: string,
  catalog: Map<string, Array<{ schema: string; table: string; columns: string[] }>>,
): Promise<number> {
  const [everyone] = rows<{ id: string }>(
    await db().execute(sql`select id from user_group where org_id = ${orgId} and slug = 'everyone'`),
  );
  if (!everyone) return 0;
  const existing = new Set((await listDataAccessRules(orgId, everyone.id)).map((r) => r.source));
  let created = 0;
  for (const [source, tables] of catalog) {
    if (existing.has(source)) continue;
    for (const table of tables) {
      if (table.columns.length === 0 || !/^[A-Za-z_][A-Za-z0-9_$]*$/.test(table.table)) continue;
      await upsertDataAccessRule(orgId, {
        groupId: everyone.id,
        source,
        tableSchema: table.schema,
        tableName: table.table,
        columns: table.columns,
        rowFilter: null,
      });
      created++;
    }
  }
  return created;
}

/**
 * Inputs for the GraphJin group grants generator: rules only for groups that
 * hold the rule's data source, and the groups holding each API operation.
 */
export async function loadGroupGrantInputs(orgId: string, apiOperationIds: string[]) {
  const groups = rows<{ id: string; slug: string; name: string }>(
    await db().execute(sql`select id, slug, name from user_group where org_id = ${orgId} and slug <> ${ADMINISTRATORS_GROUP_SLUG} order by slug`),
  );
  const rules = [];
  for (const rule of await listDataAccessRules(orgId)) {
    if (await groupHolds(orgId, rule.groupId, "data_source", rule.source)) {
      rules.push({
        groupSlug: rule.groupSlug,
        groupName: rule.groupName,
        source: rule.source,
        tableSchema: rule.tableSchema,
        tableName: rule.tableName,
        columns: rule.columns,
        rowFilter: rule.rowFilter,
      });
    }
  }
  const apiOperationHolders = new Map<string, string[]>();
  for (const operation of apiOperationIds) {
    const holders: string[] = [];
    for (const group of groups) {
      if (await groupHolds(orgId, group.id, "api_operation", operation)) holders.push(group.slug);
    }
    if (holders.length) apiOperationHolders.set(operation, holders);
  }
  return { groups: groups.map((g) => ({ slug: g.slug, name: g.name })), rules, apiOperationHolders };
}

/** GraphJin token claims for a user: group roles with rules and every group slug. */
export async function graphjinGroupClaims(orgId: string, userId: string): Promise<{ roles: string[]; groups: string[] }> {
  const found = rows<{ slug: string; has_rules: boolean }>(
    await db().execute(sql`
      select distinct g.slug,
        exists (select 1 from data_access_rule r where r.group_id = g.id) or exists (
          select 1 from item_grant i where i.group_id = g.id and i.item_type = 'api_operation') as has_rules
      from user_group g
      join app_user u on u.id = ${userId} and u.org_id = g.org_id and u.disabled_at is null
      where g.org_id = ${orgId}
        and (g.slug = 'everyone' or exists (select 1 from user_group_membership m where m.group_id = g.id and m.user_id = u.id))
      order by g.slug`),
  );
  return {
    roles: found.filter((g) => g.has_rules && g.slug !== ADMINISTRATORS_GROUP_SLUG).map((g) => `og_${g.slug}`),
    groups: found.map((g) => g.slug),
  };
}
