import { randomBytes } from "node:crypto";
import { sql } from "drizzle-orm";
import { db } from "./index";
import { ADMINISTRATORS_GROUP_SLUG, EVERYONE_GROUP_SLUG } from "./schema";

type Db = ReturnType<typeof db>;
type Tx = Parameters<Parameters<Db["transaction"]>[0]>[0];
type Runner = Db | Tx;

export type GroupErrorCode = "not_found" | "builtin" | "implicit" | "lockout" | "conflict" | "invalid";

export class GroupError extends Error {
  constructor(public readonly code: GroupErrorCode, message: string) {
    super(message);
    this.name = "GroupError";
  }
}

export type UserGroupRow = {
  id: string;
  slug: string;
  name: string;
  description: string | null;
  kind: "builtin" | "custom";
  memberCount: number;
  ruleCount: number;
};

export type GroupMemberRow = {
  userId: string;
  email: string;
  name: string | null;
  disabled: boolean;
  sources: string[];
};

export type IdpGroupRow = {
  id: string;
  provider: string;
  tenantId: string;
  externalId: string;
  displayName: string | null;
  active: boolean;
  memberCount: number;
};

export type IdpGroupRuleRow = {
  id: string;
  ssoGroupId: string;
  idpGroupName: string;
  provider: string;
  userGroupId: string;
  userGroupName: string;
  createdAt: Date;
};

export type ResolvedUserGroups = {
  groupIds: string[];
  slugs: string[];
  ssoGroupIds: string[];
  administrator: boolean;
};

const RESERVED_SLUGS = new Set([ADMINISTRATORS_GROUP_SLUG, EVERYONE_GROUP_SLUG]);

function pgTextArray(values: Iterable<string>): string {
  return `{${[...values].map((v) => `"${v.replace(/(["\\])/g, "\\$1")}"`).join(",")}}`;
}

function rows<T>(result: unknown): T[] {
  return (result as { rows: T[] }).rows;
}

export function slugifyGroupName(name: string): string {
  const base = name
    .toLowerCase()
    .normalize("NFKD")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 48);
  return base || "group";
}

async function lockOrg(tx: Tx, orgId: string): Promise<void> {
  await tx.execute(sql`select pg_advisory_xact_lock(hashtext(${"openneko.app_user:" + orgId}))`);
}

async function requireGroup(runner: Runner, orgId: string, groupId: string) {
  const [group] = rows<{ id: string; slug: string; kind: string; name: string }>(
    await runner.execute(sql`select id, slug, kind, name from user_group where org_id = ${orgId} and id = ${groupId}`),
  );
  if (!group) throw new GroupError("not_found", "group not found");
  return group;
}

async function activeAdministratorCount(runner: Runner, orgId: string): Promise<number> {
  const [row] = rows<{ n: number }>(
    await runner.execute(sql`
      select count(distinct m.user_id)::int as n
      from user_group_membership m
      join user_group g on g.id = m.group_id and g.org_id = m.org_id and g.slug = ${ADMINISTRATORS_GROUP_SLUG}
      join app_user u on u.id = m.user_id and u.disabled_at is null
      where m.org_id = ${orgId}`),
  );
  return row?.n ?? 0;
}

/**
 * Fails when the operation leaves an organization that had an active
 * administrator without one. The caller runs this inside its transaction so
 * the whole change rolls back.
 */
export async function assertAdministratorsKept(tx: Tx, orgId: string, before: number): Promise<void> {
  if (before > 0 && (await activeAdministratorCount(tx, orgId)) === 0) {
    throw new GroupError("lockout", "refusing to remove the last active administrator");
  }
}

export async function withAdministratorGuard<T>(orgId: string, fn: (tx: Tx) => Promise<T>): Promise<T> {
  return db().transaction(async (tx) => {
    await lockOrg(tx, orgId);
    const before = await activeAdministratorCount(tx, orgId);
    const result = await fn(tx);
    await assertAdministratorsKept(tx, orgId, before);
    return result;
  });
}

export async function listUserGroups(orgId: string, runner: Runner = db()): Promise<UserGroupRow[]> {
  return rows<UserGroupRow>(
    await runner.execute(sql`
      select g.id, g.slug, g.name, g.description, g.kind,
        (select count(distinct m.user_id)::int from user_group_membership m where m.group_id = g.id) as "memberCount",
        (select count(*)::int from idp_group_rule r where r.user_group_id = g.id) as "ruleCount"
      from user_group g
      where g.org_id = ${orgId}
      order by case g.slug when ${ADMINISTRATORS_GROUP_SLUG} then 0 when ${EVERYONE_GROUP_SLUG} then 1 else 2 end, lower(g.name)`),
  );
}

export async function getUserGroup(orgId: string, groupId: string): Promise<UserGroupRow | null> {
  return (await listUserGroups(orgId)).find((g) => g.id === groupId) ?? null;
}

export async function createUserGroup(
  orgId: string,
  input: { name: string; description?: string | null },
): Promise<UserGroupRow> {
  const name = input.name.trim();
  if (!name || name.length > 120) throw new GroupError("invalid", "group name must be 1 to 120 characters");
  return db().transaction(async (tx) => {
    await lockOrg(tx, orgId);
    const taken = new Set(
      rows<{ slug: string; name: string }>(await tx.execute(sql`select slug, name from user_group where org_id = ${orgId}`))
        .flatMap((r) => [r.slug, `name:${r.name.toLowerCase()}`]),
    );
    if (taken.has(`name:${name.toLowerCase()}`)) throw new GroupError("conflict", `a group named "${name}" already exists`);
    const base = slugifyGroupName(name);
    if (RESERVED_SLUGS.has(base)) throw new GroupError("conflict", `"${name}" is reserved for a built-in group`);
    let slug = base;
    for (let i = 2; taken.has(slug) || RESERVED_SLUGS.has(slug); i++) slug = `${base}-${i}`;
    const [created] = rows<{ id: string }>(
      await tx.execute(sql`
        insert into user_group (org_id, slug, name, description, kind)
        values (${orgId}, ${slug}, ${name}, ${input.description?.trim() || null}, 'custom')
        returning id`),
    );
    return (await listUserGroups(orgId, tx)).find((g) => g.id === created!.id)!;
  });
}

export async function updateUserGroup(
  orgId: string,
  groupId: string,
  input: { name?: string; description?: string | null },
): Promise<UserGroupRow> {
  const group = await requireGroup(db(), orgId, groupId);
  const name = input.name?.trim();
  if (name !== undefined) {
    if (group.kind === "builtin" && name !== group.name) throw new GroupError("builtin", "built-in groups cannot be renamed");
    if (!name || name.length > 120) throw new GroupError("invalid", "group name must be 1 to 120 characters");
  }
  try {
    await db().execute(sql`
      update user_group set
        name = coalesce(${name ?? null}, name),
        description = ${input.description === undefined ? sql`description` : input.description?.trim() || null},
        updated_at = now()
      where org_id = ${orgId} and id = ${groupId}`);
  } catch (err) {
    throw new GroupError("conflict", err instanceof Error ? err.message : String(err));
  }
  return (await getUserGroup(orgId, groupId))!;
}

export async function deleteUserGroup(orgId: string, groupId: string): Promise<void> {
  await withAdministratorGuard(orgId, async (tx) => {
    const group = await requireGroup(tx, orgId, groupId);
    if (group.kind === "builtin") throw new GroupError("builtin", "built-in groups cannot be deleted");
    await tx.execute(sql`delete from user_group where org_id = ${orgId} and id = ${groupId}`);
  });
}

export async function listGroupMembers(orgId: string, groupId: string): Promise<GroupMemberRow[]> {
  const group = await requireGroup(db(), orgId, groupId);
  if (group.slug === EVERYONE_GROUP_SLUG) {
    return rows<GroupMemberRow>(
      await db().execute(sql`
        select id as "userId", email, name, false as disabled, array['implicit']::text[] as sources
        from app_user where org_id = ${orgId} and disabled_at is null order by lower(email)`),
    );
  }
  return rows<GroupMemberRow>(
    await db().execute(sql`
      select u.id as "userId", u.email, u.name, (u.disabled_at is not null) as disabled,
        array_agg(m.source order by m.source) as sources
      from user_group_membership m
      join app_user u on u.id = m.user_id
      where m.org_id = ${orgId} and m.group_id = ${groupId}
      group by u.id, u.email, u.name, u.disabled_at
      order by lower(u.email)`),
  );
}

export async function addLocalGroupMember(orgId: string, groupId: string, userId: string): Promise<void> {
  await withAdministratorGuard(orgId, async (tx) => {
    const group = await requireGroup(tx, orgId, groupId);
    if (group.slug === EVERYONE_GROUP_SLUG) throw new GroupError("implicit", "every active user is already in Everyone");
    const [user] = rows<{ id: string }>(await tx.execute(sql`select id from app_user where org_id = ${orgId} and id = ${userId}`));
    if (!user) throw new GroupError("not_found", "user not found");
    await tx.execute(sql`
      insert into user_group_membership (org_id, group_id, user_id, source)
      values (${orgId}, ${groupId}, ${userId}, 'local') on conflict do nothing`);
  });
}

export async function removeLocalGroupMember(orgId: string, groupId: string, userId: string): Promise<{ stillMember: boolean }> {
  return withAdministratorGuard(orgId, async (tx) => {
    const group = await requireGroup(tx, orgId, groupId);
    if (group.slug === EVERYONE_GROUP_SLUG) throw new GroupError("implicit", "users cannot leave Everyone");
    await tx.execute(sql`
      delete from user_group_membership
      where org_id = ${orgId} and group_id = ${groupId} and user_id = ${userId} and source = 'local'`);
    const [left] = rows<{ n: number }>(await tx.execute(sql`
      select count(*)::int as n from user_group_membership
      where org_id = ${orgId} and group_id = ${groupId} and user_id = ${userId}`));
    return { stillMember: (left?.n ?? 0) > 0 };
  });
}

/** User ids that hold Administrators, by any membership source. */
export async function administratorUserIds(orgId: string, runner: Runner = db()): Promise<Set<string>> {
  const found = rows<{ user_id: string }>(
    await runner.execute(sql`
      select m.user_id from user_group_membership m
      join user_group g on g.id = m.group_id
      where m.org_id = ${orgId} and g.slug = ${ADMINISTRATORS_GROUP_SLUG}`),
  );
  return new Set(found.map((row) => row.user_id));
}

/** Active administrators, for the lockout guard and for worker notices. */
export async function activeAdministratorIds(orgId: string, runner: Runner = db()): Promise<string[]> {
  const found = rows<{ user_id: string }>(
    await runner.execute(sql`
      select m.user_id from user_group_membership m
      join user_group g on g.id = m.group_id
      join app_user u on u.id = m.user_id and u.disabled_at is null
      where m.org_id = ${orgId} and g.slug = ${ADMINISTRATORS_GROUP_SLUG}`),
  );
  return found.map((row) => row.user_id);
}

/** Adds or removes the local Administrators membership; rule memberships stay. */
export async function setLocalAdministrator(orgId: string, userId: string, administrator: boolean): Promise<void> {
  const groupId = await builtinGroupId(orgId, ADMINISTRATORS_GROUP_SLUG);
  if (administrator) await addLocalGroupMember(orgId, groupId, userId);
  else {
    const { stillMember } = await removeLocalGroupMember(orgId, groupId, userId);
    if (stillMember) throw new GroupError("conflict", "this user is an administrator through an IdP rule; change the rule instead");
  }
}

export async function builtinGroupId(orgId: string, slug: string, runner: Runner = db()): Promise<string> {
  const [row] = rows<{ id: string }>(await runner.execute(sql`select id from user_group where org_id = ${orgId} and slug = ${slug}`));
  if (!row) throw new GroupError("not_found", `built-in group ${slug} is missing`);
  return row.id;
}

export async function listIdpGroups(orgId: string): Promise<IdpGroupRow[]> {
  return rows<IdpGroupRow>(
    await db().execute(sql`
      select g.id, g.provider, g.tenant_id as "tenantId", g.external_id as "externalId",
        g.display_name as "displayName", g.active,
        (select count(*)::int from sso_group_membership m where m.group_id = g.id) as "memberCount"
      from sso_group g where g.org_id = ${orgId}
      order by g.active desc, lower(coalesce(g.display_name, g.external_id))`),
  );
}

export async function listIdpGroupRules(orgId: string): Promise<IdpGroupRuleRow[]> {
  return rows<IdpGroupRuleRow>(
    await db().execute(sql`
      select r.id, r.sso_group_id as "ssoGroupId", coalesce(s.display_name, s.external_id) as "idpGroupName",
        s.provider, r.user_group_id as "userGroupId", g.name as "userGroupName", r.created_at as "createdAt"
      from idp_group_rule r
      join sso_group s on s.id = r.sso_group_id
      join user_group g on g.id = r.user_group_id
      where r.org_id = ${orgId}
      order by lower(g.name), lower(coalesce(s.display_name, s.external_id))`),
  );
}

export async function createIdpGroupRule(
  orgId: string,
  input: { ssoGroupId: string; userGroupId: string; createdByUserId?: string | null },
): Promise<{ id: string }> {
  return withAdministratorGuard(orgId, async (tx) => {
    const group = await requireGroup(tx, orgId, input.userGroupId);
    if (group.slug === EVERYONE_GROUP_SLUG) throw new GroupError("implicit", "every active user is already in Everyone");
    const [idp] = rows<{ id: string }>(await tx.execute(sql`select id from sso_group where org_id = ${orgId} and id = ${input.ssoGroupId}`));
    if (!idp) throw new GroupError("not_found", "IdP group not found");
    const [rule] = rows<{ id: string }>(await tx.execute(sql`
      insert into idp_group_rule (org_id, sso_group_id, user_group_id, created_by_user_id)
      values (${orgId}, ${input.ssoGroupId}, ${input.userGroupId}, ${input.createdByUserId ?? null})
      on conflict (org_id, sso_group_id, user_group_id) do update set created_at = idp_group_rule.created_at
      returning id`));
    await recomputeRuleMemberships(tx, orgId);
    return { id: rule!.id };
  });
}

export async function deleteIdpGroupRule(orgId: string, ruleId: string): Promise<void> {
  await withAdministratorGuard(orgId, async (tx) => {
    await tx.execute(sql`delete from idp_group_rule where org_id = ${orgId} and id = ${ruleId}`);
    await recomputeRuleMemberships(tx, orgId);
  });
}

/** Makes rule memberships equal to what the rules and IdP memberships say. */
export async function recomputeRuleMemberships(tx: Tx, orgId: string, userId: string | null = null): Promise<void> {
  const desired = sql`
    select distinct r.user_group_id as group_id, m.user_id, 'rule:' || r.id::text as source
    from idp_group_rule r
    join sso_group s on s.id = r.sso_group_id and s.org_id = r.org_id and s.active
    join sso_group_membership m on m.group_id = r.sso_group_id and m.org_id = r.org_id
    where r.org_id = ${orgId} and (${userId}::text is null or m.user_id = ${userId}::text)`;
  await tx.execute(sql`
    delete from user_group_membership um
    where um.org_id = ${orgId} and um.source like 'rule:%'
      and (${userId}::text is null or um.user_id = ${userId}::text)
      and not exists (
        select 1 from (${desired}) d
        where d.group_id = um.group_id and d.user_id = um.user_id and d.source = um.source)`);
  await tx.execute(sql`
    insert into user_group_membership (org_id, group_id, user_id, source)
    select ${orgId}, d.group_id, d.user_id, d.source from (${desired}) d
    on conflict do nothing`);
}

export async function resolveUserGroups(orgId: string, userId: string): Promise<ResolvedUserGroups> {
  const found = rows<{ id: string; slug: string }>(
    await db().execute(sql`
      select distinct g.id, g.slug from user_group g
      join app_user u on u.id = ${userId} and u.org_id = g.org_id and u.disabled_at is null
      where g.org_id = ${orgId}
        and (g.slug = ${EVERYONE_GROUP_SLUG}
          or exists (select 1 from user_group_membership m where m.group_id = g.id and m.user_id = u.id))
      order by g.slug`),
  );
  const sso = rows<{ id: string }>(
    await db().execute(sql`
      select s.id from sso_group_membership m
      join sso_group s on s.id = m.group_id and s.org_id = m.org_id and s.active
      where m.org_id = ${orgId} and m.user_id = ${userId}`),
  );
  return {
    groupIds: found.map((g) => g.id),
    slugs: found.map((g) => g.slug),
    ssoGroupIds: sso.map((s) => s.id),
    administrator: found.some((g) => g.slug === ADMINISTRATORS_GROUP_SLUG),
  };
}

export type IdpGroupInput = { externalId: string; displayName: string | null };

async function upsertIdpGroups(
  tx: Tx,
  orgId: string,
  provider: string,
  tenantId: string,
  groups: IdpGroupInput[],
): Promise<Map<string, string>> {
  const ids = new Map<string, string>();
  for (const group of groups) {
    const [row] = rows<{ id: string }>(await tx.execute(sql`
      insert into sso_group (org_id, provider, tenant_id, external_id, display_name, active, updated_at)
      values (${orgId}, ${provider}, ${tenantId}, ${group.externalId}, ${group.displayName}, true, now())
      on conflict (org_id, provider, tenant_id, external_id)
      do update set display_name = excluded.display_name, active = true, updated_at = now()
      returning id`));
    ids.set(group.externalId, row!.id);
  }
  return ids;
}

/** Sign-in path: replaces one user's IdP groups and recomputes their rule memberships. */
export async function reconcileSignInGroups(input: {
  orgId: string;
  userId: string;
  provider: string;
  tenantId: string;
  groups: IdpGroupInput[];
}): Promise<void> {
  await withAdministratorGuard(input.orgId, async (tx) => {
    const ids = await upsertIdpGroups(tx, input.orgId, input.provider, input.tenantId, input.groups);
    await tx.execute(sql`delete from sso_group_membership where org_id = ${input.orgId} and user_id = ${input.userId}`);
    for (const groupId of new Set(ids.values())) {
      await tx.execute(sql`
        insert into sso_group_membership (org_id, group_id, user_id, synced_at)
        values (${input.orgId}, ${groupId}, ${input.userId}, now()) on conflict do nothing`);
    }
    await tx.execute(sql`
      insert into sso_group_sync_audit (org_id, user_id, provider, tenant_id, external_group_ids)
      values (${input.orgId}, ${input.userId}, ${input.provider}, ${input.tenantId},
        ${pgTextArray(input.groups.map((g) => g.externalId))}::text[])`);
    await recomputeRuleMemberships(tx, input.orgId, input.userId);
  });
}

export type DirectorySnapshot = {
  orgId: string;
  provider: string;
  tenantId: string;
  createUsers: boolean;
  users: Array<{ externalId: string; sub?: string | null; email: string; name?: string | null; active: boolean }>;
  groups: IdpGroupInput[];
  memberships: Array<{ userExternalId: string; groupExternalId: string }>;
};

export type DirectorySyncStats = {
  usersCreated: number;
  usersDisabled: number;
  usersEnabled: number;
  usersMatched: number;
  groups: number;
  groupsDeactivated: number;
  memberships: number;
};

/**
 * Full directory sync from a plugin. Only users that this provider supplied
 * are disabled or enabled; local users, including a solo owner, are matched
 * but never disabled.
 */
export async function reconcileDirectorySnapshot(snapshot: DirectorySnapshot): Promise<DirectorySyncStats> {
  const { orgId, provider, tenantId } = snapshot;
  return withAdministratorGuard(orgId, async (tx) => {
    const stats: DirectorySyncStats = {
      usersCreated: 0, usersDisabled: 0, usersEnabled: 0, usersMatched: 0,
      groups: snapshot.groups.length, groupsDeactivated: 0, memberships: 0,
    };
    const groupIds = await upsertIdpGroups(tx, orgId, provider, tenantId, snapshot.groups);
    const keep = [...groupIds.values()];
    const deactivated = rows<{ id: string }>(await tx.execute(sql`
      update sso_group set active = false, updated_at = now()
      where org_id = ${orgId} and provider = ${provider} and tenant_id = ${tenantId} and active
        and not (id = any(${pgTextArray(keep)}::uuid[]))
      returning id`));
    stats.groupsDeactivated = deactivated.length;

    const userIds = new Map<string, string>();
    const seen = new Set<string>();
    for (const user of snapshot.users) {
      const email = user.email.trim().toLowerCase();
      const [match] = rows<{ id: string; source: string; disabled: boolean }>(await tx.execute(sql`
        select id, source, disabled_at is not null as disabled from app_user
        where org_id = ${orgId}
          and ((${user.sub ?? null}::text is not null and sub = ${user.sub ?? null}::text) or lower(email) = ${email})
        order by (sub = ${user.sub ?? null}::text) desc nulls last
        limit 1`));
      if (match) {
        stats.usersMatched++;
        seen.add(match.id);
        userIds.set(user.externalId, match.id);
        if (match.source === provider && match.disabled === user.active) {
          await tx.execute(sql`
            update app_user set disabled_at = ${user.active ? null : sql`now()`}, updated_at = now()
            where id = ${match.id}`);
          if (user.active) stats.usersEnabled++;
          else stats.usersDisabled++;
        }
        continue;
      }
      if (!user.active || !snapshot.createUsers) continue;
      const id = `usr_${randomBytes(9).toString("base64url")}`;
      await tx.execute(sql`
        insert into app_user (id, org_id, email, name, sub, source)
        values (${id}, ${orgId}, ${email}, ${user.name ?? null}, ${user.sub ?? null}, ${provider})`);
      stats.usersCreated++;
      seen.add(id);
      userIds.set(user.externalId, id);
    }
    const gone = rows<{ id: string }>(await tx.execute(sql`
      update app_user set disabled_at = now(), updated_at = now()
      where org_id = ${orgId} and source = ${provider} and disabled_at is null
        and not (id = any(${pgTextArray(seen)}::text[]))
      returning id`));
    stats.usersDisabled += gone.length;

    if (keep.length > 0) {
      await tx.execute(sql`
        delete from sso_group_membership
        where org_id = ${orgId} and group_id = any(${pgTextArray(keep)}::uuid[])`);
    }
    for (const m of snapshot.memberships) {
      const userId = userIds.get(m.userExternalId);
      const groupId = groupIds.get(m.groupExternalId);
      if (!userId || !groupId) continue;
      await tx.execute(sql`
        insert into sso_group_membership (org_id, group_id, user_id, synced_at)
        values (${orgId}, ${groupId}, ${userId}, now()) on conflict do nothing`);
      stats.memberships++;
    }
    await recomputeRuleMemberships(tx, orgId);
    return stats;
  });
}
