import { sql } from "drizzle-orm";
import { GroupError, resolveUserGroups } from "./groups";
import { db } from "./index";
import { ADMINISTRATORS_GROUP_SLUG } from "./schema";

export const ITEM_TYPES = [
  "skill",
  "workflow",
  "library_collection",
  "library_concept",
  "metric",
  "dashboard",
  "watcher",
  "team_memory",
  "data_source",
  "saved_query",
  "api_operation",
  "action",
  "integration",
  "channel",
  "pack",
] as const;

export type ItemType = (typeof ITEM_TYPES)[number];
export type ItemRef = { type: ItemType; id: string };

export function isItemType(value: unknown): value is ItemType {
  return typeof value === "string" && (ITEM_TYPES as readonly string[]).includes(value);
}

/**
 * Who asks. A user resolves through their groups. A service actor with a
 * packId is limited to that pack's items; a service actor without one is a
 * system job and holds everything.
 */
export type EntitlementActor =
  | { orgId: string; kind: "user"; userId: string }
  | { orgId: string; kind: "service"; packId?: string | null }
  | { orgId: string; kind: "anonymous" };

export type HoldResult = { allowed: boolean; via: string[] };
export type HeldItems = "*" | Set<string>;

type GrantRow = { group_id: string; item_type: ItemType; item_id: string };
type GrantIndex = { revision: string; byGroup: Map<string, GrantRow[]> };

const PACK_ITEM_JOINS: Partial<Record<ItemType, { kind: string; idExpr: string; join: string }>> = {
  skill: { kind: "skill", idExpr: "a.target_ref", join: "" },
  saved_query: { kind: "saved_query", idExpr: "a.target_ref", join: "" },
  action: { kind: "action", idExpr: "a.target_ref", join: "" },
  data_source: { kind: "source", idExpr: "a.target_ref", join: "" },
  workflow: {
    kind: "workflow",
    idExpr: "w.id::text",
    join: "join workflow_definition w on w.org_id = a.org_id and w.name = a.target_ref",
  },
  watcher: {
    kind: "watcher",
    idExpr: "w.id::text",
    join: "join watcher w on w.org_id = a.org_id and w.name = a.target_ref",
  },
  metric: {
    kind: "metric",
    idExpr: "m.id::text",
    join: "join metric m on m.org_id = a.org_id and m.slug = a.target_ref",
  },
};

const indexCache = new Map<string, GrantIndex>();

function pgTextArray(values: Iterable<string>): string {
  return `{${[...values].map((v) => `"${v.replace(/(["\\])/g, "\\$1")}"`).join(",")}}`;
}

function rows<T>(result: unknown): T[] {
  return (result as { rows: T[] }).rows;
}

async function grantIndex(orgId: string): Promise<GrantIndex> {
  const [rev] = rows<{ revision: string }>(
    await db().execute(sql`select revision::text || '@' || updated_at::text as revision from item_grant_revision where org_id = ${orgId}`),
  );
  const revision = rev?.revision ?? "0";
  const cached = indexCache.get(orgId);
  if (cached && cached.revision === revision) return cached;
  const byGroup = new Map<string, GrantRow[]>();
  for (const row of rows<GrantRow>(
    await db().execute(sql`select group_id, item_type, item_id from item_grant where org_id = ${orgId}`),
  )) {
    const list = byGroup.get(row.group_id) ?? [];
    list.push(row);
    byGroup.set(row.group_id, list);
  }
  const index = { revision, byGroup };
  indexCache.set(orgId, index);
  return index;
}

export function _resetEntitlementCacheForTesting(): void {
  indexCache.clear();
}

export async function packsContaining(orgId: string, item: ItemRef): Promise<string[]> {
  const spec = PACK_ITEM_JOINS[item.type];
  if (!spec) return [];
  return rows<{ pack_id: string }>(
    await db().execute(sql`
      select distinct i.pack_id
      from pack_artifact a
      join pack_install i on i.id = a.pack_install_id and i.status <> 'removed'
      ${sql.raw(spec.join)}
      where a.org_id = ${orgId} and a.artifact_kind = ${spec.kind} and a.ownership <> 'detached'
        and ${sql.raw(spec.idExpr)} = ${item.id}`),
  ).map((r) => r.pack_id);
}

export async function packItemIds(orgId: string, packIds: string[], type: ItemType): Promise<Set<string>> {
  const spec = PACK_ITEM_JOINS[type];
  if (!spec || packIds.length === 0) return new Set();
  const found = rows<{ id: string }>(
    await db().execute(sql`
      select distinct ${sql.raw(spec.idExpr)} as id
      from pack_artifact a
      join pack_install i on i.id = a.pack_install_id and i.status <> 'removed'
      ${sql.raw(spec.join)}
      where a.org_id = ${orgId} and a.artifact_kind = ${spec.kind} and a.ownership <> 'detached'
        and i.pack_id = any(${pgTextArray(packIds)}::text[])`),
  );
  return new Set(found.map((r) => r.id));
}

async function actorGrants(actor: Extract<EntitlementActor, { kind: "user" }>) {
  const groups = await resolveUserGroups(actor.orgId, actor.userId);
  const index = await grantIndex(actor.orgId);
  const grants = groups.groupIds.flatMap((id) => index.byGroup.get(id) ?? []);
  return { groups, grants };
}

/**
 * Whether the actor holds an item. `parents` lists items that contain this
 * one, such as the library collection of a concept.
 */
export async function holds(
  actor: EntitlementActor,
  type: ItemType,
  itemId: string,
  opts: { parents?: ItemRef[] } = {},
): Promise<HoldResult> {
  const refs: ItemRef[] = [{ type, id: itemId }, ...(opts.parents ?? [])];
  if (actor.kind === "anonymous") return { allowed: false, via: [] };
  if (actor.kind === "service") {
    if (!actor.packId) return { allowed: true, via: ["service"] };
    const packs = (await Promise.all(refs.map((ref) => packsContaining(actor.orgId, ref)))).flat();
    return { allowed: packs.includes(actor.packId), via: packs.includes(actor.packId) ? [`pack:${actor.packId}`] : [] };
  }
  const { groups, grants } = await actorGrants(actor);
  if (groups.administrator) return { allowed: true, via: [ADMINISTRATORS_GROUP_SLUG] };
  const via = new Set<string>();
  for (const grant of grants) {
    for (const ref of refs) {
      if (grant.item_type === ref.type && (grant.item_id === ref.id || grant.item_id === "*")) via.add(grant.group_id);
    }
  }
  const heldPacks = grants.filter((g) => g.item_type === "pack");
  if (via.size === 0 && heldPacks.length > 0) {
    const containing = new Set((await Promise.all(refs.map((ref) => packsContaining(actor.orgId, ref)))).flat());
    for (const grant of heldPacks) {
      if (grant.item_id === "*" ? containing.size > 0 : containing.has(grant.item_id)) via.add(grant.group_id);
    }
  }
  return { allowed: via.size > 0, via: [...via] };
}

/** "*" when the actor holds every item of the type, otherwise the held ids. */
export async function heldItems(actor: EntitlementActor, type: ItemType): Promise<HeldItems> {
  if (actor.kind === "anonymous") return new Set();
  if (actor.kind === "service") {
    if (!actor.packId) return "*";
    return packItemIds(actor.orgId, [actor.packId], type);
  }
  const { groups, grants } = await actorGrants(actor);
  if (groups.administrator) return "*";
  const ids = new Set<string>();
  const packs: string[] = [];
  for (const grant of grants) {
    if (grant.item_type === type) {
      if (grant.item_id === "*") return "*";
      ids.add(grant.item_id);
    }
    if (grant.item_type === "pack") packs.push(grant.item_id);
  }
  if (packs.length > 0) {
    const packIds = packs.includes("*")
      ? rows<{ pack_id: string }>(
          await db().execute(sql`select distinct pack_id from pack_install where org_id = ${actor.orgId} and status <> 'removed'`),
        ).map((r) => r.pack_id)
      : packs;
    for (const id of await packItemIds(actor.orgId, packIds, type)) ids.add(id);
  }
  return ids;
}

export function filterHeld<T>(held: HeldItems, items: T[], idOf: (item: T) => string): T[] {
  return held === "*" ? items : items.filter((item) => held.has(idOf(item)));
}

export type ItemHolder = { groupId: string; name: string; slug: string; via: "administrators" | "item" | "all" | `pack:${string}` };

export async function whoHolds(orgId: string, type: ItemType, itemId: string): Promise<ItemHolder[]> {
  const packs = await packsContaining(orgId, { type, id: itemId });
  const found = rows<{ group_id: string; name: string; slug: string; item_type: string; item_id: string }>(
    await db().execute(sql`
      select g.id as group_id, g.name, g.slug, ig.item_type, ig.item_id
      from item_grant ig join user_group g on g.id = ig.group_id
      where ig.org_id = ${orgId}
        and ((ig.item_type = ${type} and ig.item_id in (${itemId}, '*'))
          or (ig.item_type = 'pack' and (ig.item_id = '*' and ${packs.length > 0} or ig.item_id = any(${pgTextArray(packs)}::text[]))))
      order by lower(g.name)`),
  );
  const holders: ItemHolder[] = [];
  const [admins] = rows<{ id: string; name: string }>(
    await db().execute(sql`select id, name from user_group where org_id = ${orgId} and slug = ${ADMINISTRATORS_GROUP_SLUG}`),
  );
  if (admins) holders.push({ groupId: admins.id, name: admins.name, slug: ADMINISTRATORS_GROUP_SLUG, via: "administrators" });
  for (const row of found) {
    holders.push({
      groupId: row.group_id,
      name: row.name,
      slug: row.slug,
      via: row.item_type === "pack" ? `pack:${row.item_id}` : row.item_id === "*" ? "all" : "item",
    });
  }
  return holders;
}

export type GroupItemGrant = { itemType: ItemType; itemId: string; createdAt: Date; createdByUserId: string | null };

export async function listGroupItemGrants(orgId: string, groupId: string): Promise<GroupItemGrant[]> {
  return rows<GroupItemGrant>(
    await db().execute(sql`
      select item_type as "itemType", item_id as "itemId", created_at as "createdAt", created_by_user_id as "createdByUserId"
      from item_grant where org_id = ${orgId} and group_id = ${groupId}
      order by item_type, item_id = '*' desc, item_id`),
  );
}

export type EffectiveItem = { itemType: ItemType; itemId: string; groups: Array<{ groupId: string; name: string }> };

export async function effectiveAccess(
  orgId: string,
  userId: string,
): Promise<{ administrator: boolean; items: EffectiveItem[] }> {
  const groups = await resolveUserGroups(orgId, userId);
  if (groups.groupIds.length === 0) return { administrator: false, items: [] };
  const found = rows<{ item_type: ItemType; item_id: string; group_id: string; name: string }>(
    await db().execute(sql`
      select ig.item_type, ig.item_id, g.id as group_id, g.name
      from item_grant ig join user_group g on g.id = ig.group_id
      where ig.org_id = ${orgId}
        and ig.group_id = any(${pgTextArray(groups.groupIds)}::uuid[])
      order by ig.item_type, ig.item_id = '*' desc, ig.item_id, lower(g.name)`),
  );
  const items = new Map<string, EffectiveItem>();
  for (const row of found) {
    const key = `${row.item_type}\u0000${row.item_id}`;
    const item = items.get(key) ?? { itemType: row.item_type, itemId: row.item_id, groups: [] };
    item.groups.push({ groupId: row.group_id, name: row.name });
    items.set(key, item);
  }
  return { administrator: groups.administrator, items: [...items.values()] };
}

async function requireGrantableGroup(orgId: string, groupId: string) {
  const [group] = rows<{ id: string; slug: string; name: string }>(
    await db().execute(sql`select id, slug, name from user_group where org_id = ${orgId} and id = ${groupId}`),
  );
  if (!group) throw new GroupError("not_found", "group not found");
  if (group.slug === ADMINISTRATORS_GROUP_SLUG) {
    throw new GroupError("builtin", "Administrators hold every item; grants are not needed");
  }
  return group;
}

export type ItemGrantInput = {
  groupId: string;
  itemType: ItemType;
  itemId: string;
  actorUserId?: string | null;
  actionRequestId?: string | null;
};

export async function grantItem(orgId: string, input: ItemGrantInput): Promise<{ created: boolean }> {
  if (!isItemType(input.itemType)) throw new GroupError("invalid", `unknown item type ${String(input.itemType)}`);
  const itemId = input.itemId.trim();
  if (!itemId || itemId.length > 500) throw new GroupError("invalid", "item id must be 1 to 500 characters");
  const group = await requireGrantableGroup(orgId, input.groupId);
  return db().transaction(async (tx) => {
    const inserted = rows<{ item_id: string }>(await tx.execute(sql`
      insert into item_grant (org_id, group_id, item_type, item_id, created_by_user_id, action_request_id)
      values (${orgId}, ${group.id}, ${input.itemType}, ${itemId}, ${input.actorUserId ?? null}, ${input.actionRequestId ?? null})
      on conflict do nothing returning item_id`));
    if (inserted.length > 0) {
      await tx.execute(sql`
        insert into item_grant_audit (org_id, actor_user_id, action, group_id, group_name, item_type, item_id, action_request_id)
        values (${orgId}, ${input.actorUserId ?? null}, 'grant', ${group.id}, ${group.name}, ${input.itemType}, ${itemId}, ${input.actionRequestId ?? null})`);
    }
    return { created: inserted.length > 0 };
  });
}

/** Grant a selection in one atomic write, with one audit row per new grant. */
export async function grantItems(
  orgId: string,
  input: Omit<ItemGrantInput, "itemId"> & { itemIds: string[] },
): Promise<{ created: number }> {
  if (!isItemType(input.itemType)) throw new GroupError("invalid", `unknown item type ${String(input.itemType)}`);
  if (!Array.isArray(input.itemIds) || input.itemIds.length < 1 || input.itemIds.length > 500) {
    throw new GroupError("invalid", "select 1 to 500 items to grant");
  }
  const ids = [...new Set(input.itemIds.map((id) => typeof id === "string" ? id.trim() : ""))];
  if (ids.some((id) => !id || id.length > 500)) throw new GroupError("invalid", "item id must be 1 to 500 characters");
  const group = await requireGrantableGroup(orgId, input.groupId);
  const inserted = rows<{ item_id: string }>(await db().execute(sql`
    WITH created AS (
      INSERT INTO item_grant (org_id, group_id, item_type, item_id, created_by_user_id, action_request_id)
      SELECT ${orgId}, ${group.id}, ${input.itemType}, item_id, ${input.actorUserId ?? null}, ${input.actionRequestId ?? null}
      FROM unnest(${pgTextArray(ids)}::text[]) AS selected(item_id)
      ON CONFLICT DO NOTHING
      RETURNING item_id
    )
    INSERT INTO item_grant_audit (org_id, actor_user_id, action, group_id, group_name, item_type, item_id, action_request_id)
    SELECT ${orgId}, ${input.actorUserId ?? null}, 'grant', ${group.id}, ${group.name}, ${input.itemType}, item_id, ${input.actionRequestId ?? null}
    FROM created RETURNING item_id`));
  return { created: inserted.length };
}

export async function revokeItem(orgId: string, input: ItemGrantInput): Promise<{ removed: boolean }> {
  const group = await requireGrantableGroup(orgId, input.groupId);
  return db().transaction(async (tx) => {
    const removed = rows<{ item_id: string }>(await tx.execute(sql`
      delete from item_grant
      where org_id = ${orgId} and group_id = ${group.id} and item_type = ${input.itemType} and item_id = ${input.itemId}
      returning item_id`));
    if (removed.length > 0) {
      await tx.execute(sql`
        insert into item_grant_audit (org_id, actor_user_id, action, group_id, group_name, item_type, item_id, action_request_id)
        values (${orgId}, ${input.actorUserId ?? null}, 'revoke', ${group.id}, ${group.name}, ${input.itemType}, ${input.itemId}, ${input.actionRequestId ?? null})`);
    }
    return { removed: removed.length > 0 };
  });
}

/** Whether one group holds an item, including '*' and pack grants. */
export async function groupHolds(orgId: string, groupId: string, type: ItemType, itemId: string): Promise<boolean> {
  const [group] = rows<{ slug: string }>(
    await db().execute(sql`select slug from user_group where org_id = ${orgId} and id = ${groupId}`),
  );
  if (!group) return false;
  if (group.slug === ADMINISTRATORS_GROUP_SLUG) return true;
  const grants = (await grantIndex(orgId)).byGroup.get(groupId) ?? [];
  if (grants.some((g) => g.item_type === type && (g.item_id === itemId || g.item_id === "*"))) return true;
  const packGrants = grants.filter((g) => g.item_type === "pack");
  if (packGrants.length === 0) return false;
  const containing = await packsContaining(orgId, { type, id: itemId });
  return packGrants.some((g) => (g.item_id === "*" ? containing.length > 0 : containing.includes(g.item_id)));
}
