import { connection } from "next/server";
import { notFound } from "next/navigation";
import {
  app_user,
  asc,
  db,
  eq,
  getGroupGrantsEnabled,
  getUserGroup,
  isNull,
  and,
  listDataAccessRules,
  listGroupItemGrants,
  listGroupMembers,
  listIdpGroupRules,
} from "@neko/db";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { ITEM_TYPE_LABELS } from "@/lib/groups-admin";
import { AdminDenied, AdminShell } from "../../../AdminShell";
import { GroupDetailClient } from "./GroupDetailClient";

export default async function GroupPage({ params }: { params: Promise<{ groupId: string }> }) {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;
  const { groupId } = await params;
  const orgId = await getOrgId();
  const group = await getUserGroup(orgId, groupId).catch(() => null);
  if (!group) notFound();

  const [members, grants, rules, idpRules, users, groupGrantsEnabled] = await Promise.all([
    listGroupMembers(orgId, groupId),
    listGroupItemGrants(orgId, groupId),
    listDataAccessRules(orgId, groupId),
    listIdpGroupRules(orgId),
    db()
      .select({ id: app_user.id, email: app_user.email, name: app_user.name })
      .from(app_user)
      .where(and(eq(app_user.org_id, orgId), isNull(app_user.disabled_at)))
      .orderBy(asc(app_user.email)),
    getGroupGrantsEnabled(orgId),
  ]);
  const ruleNames = new Map(idpRules.map((r) => [`rule:${r.id}`, r.idpGroupName]));

  return (
    <AdminShell
      title={group.name}
      subtitle={group.description ?? (group.kind === "builtin" ? "Built-in group" : "Custom group")}
      back={{ href: "/admin/users?tab=groups", label: "Groups" }}
    >
      <GroupDetailClient
        group={group}
        members={members.map((m) => ({
          ...m,
          sources: m.sources.map((s) => (s === "local" || s === "implicit" ? s : `IdP rule: ${ruleNames.get(s) ?? "removed"}`)),
        }))}
        grants={grants.map((g) => ({ itemType: g.itemType, itemId: g.itemId }))}
        rules={rules.map((r) => ({ ...r, updatedAt: new Date(r.updatedAt).toISOString() }))}
        users={users}
        itemTypes={Object.entries(ITEM_TYPE_LABELS).map(([type, labels]) => ({ type, ...labels }))}
        groupGrantsEnabled={groupGrantsEnabled}
      />
    </AdminShell>
  );
}
