import { connection } from "next/server";
import {
  administratorUserIds,
  app_user,
  asc,
  db,
  eq,
  isUnclaimedSoloEmail,
  listIdpGroupRules,
  listIdpGroups,
  listUserGroups,
  user_group,
  user_group_membership,
} from "@neko/db";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { getAuthGateStatus } from "@/lib/auth";
import { requestWorker } from "@/lib/groups-admin";
import { AdminDenied, AdminShell } from "../AdminShell";
import { UsersAdminTabs, type UsersAdminTab } from "./UsersAdminTabs";
import type { AdminUserRow } from "./UsersClient";

const TABS: UsersAdminTab[] = ["users", "groups", "idp-rules"];

export default async function AdminUsersPage({ searchParams }: { searchParams: Promise<{ tab?: string }> }) {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  const orgId = await getOrgId();
  const [users, memberships, administrators, groups, idpGroups, rules, directoryStatus, authGate, query] = await Promise.all([
    db()
      .select({
        id: app_user.id,
        sub: app_user.sub,
        email: app_user.email,
        name: app_user.name,
        source: app_user.source,
        disabledAt: app_user.disabled_at,
        createdAt: app_user.created_at,
        lastLoginAt: app_user.last_login_at,
      })
      .from(app_user)
      .where(eq(app_user.org_id, orgId))
      .orderBy(asc(app_user.email)),
    db()
      .select({ userId: user_group_membership.user_id, name: user_group.name, source: user_group_membership.source })
      .from(user_group_membership)
      .innerJoin(user_group, eq(user_group.id, user_group_membership.group_id))
      .where(eq(user_group_membership.org_id, orgId)),
    administratorUserIds(orgId),
    listUserGroups(orgId),
    listIdpGroups(orgId),
    listIdpGroupRules(orgId),
    requestWorker("/admin/directory/status").catch(() => null),
    getAuthGateStatus(),
    searchParams,
  ]);

  const groupsByUser = new Map<string, Array<{ name: string; fromRule: boolean }>>();
  for (const m of memberships) {
    const list = groupsByUser.get(m.userId) ?? [];
    const existing = list.find((g) => g.name === m.name);
    if (existing) existing.fromRule = existing.fromRule && m.source !== "local";
    else list.push({ name: m.name, fromRule: m.source !== "local" });
    groupsByUser.set(m.userId, list);
  }

  const directory = directoryStatus?.body as
    | { provider?: { pluginName: string; providerLabel: string; canCreateUsers: boolean } | null }
    | undefined;
  const directoryCreateLabel = directory?.provider?.canCreateUsers ? directory.provider.providerLabel : null;
  const signInLabel = authGate.provider?.providerLabel ?? null;

  // Claiming the operator's address only matters once someone can sign
  // in, so the prompt waits for a sign-in provider. Without one it would ask
  // whoever opens the page, which on a public installation is a stranger.
  const identitySetup =
    Boolean(authGate.provider ?? authGate.pending) &&
    users.some((user) => user.id === actor.userId && isUnclaimedSoloEmail(user.email));
  const rows: AdminUserRow[] = users.map((user) => ({
    id: user.id,
    email: isUnclaimedSoloEmail(user.email) ? "Email not set" : user.email,
    name: user.name,
    role: administrators.has(user.id) ? "admin" : "member",
    source: user.source,
    groups: groupsByUser.get(user.id) ?? [],
    disabled: Boolean(user.disabledAt),
    hasSignedIn: Boolean(user.sub ?? user.lastLoginAt),
    lastLoginAt: user.lastLoginAt?.toISOString() ?? null,
    createdAt: user.createdAt?.toISOString() ?? null,
  }));
  const tab = TABS.includes(query.tab as UsersAdminTab) ? (query.tab as UsersAdminTab) : "users";

  return (
    <AdminShell
      title="User administration"
      subtitle={
        signInLabel
          ? `Multi-user mode via ${signInLabel}.`
          : "Solo mode uses your local admin account."
      }
      back={{ href: "/admin", label: "Admin" }}
    >
      <UsersAdminTabs
        tab={identitySetup ? "users" : tab}
        users={rows}
        identitySetup={identitySetup}
        groups={groups}
        idpGroups={idpGroups.map((g) => ({ ...g }))}
        rules={rules.map((r) => ({ ...r, createdAt: new Date(r.createdAt).toISOString() }))}
        signInProvider={signInLabel ?? authGate.pending?.providerLabel ?? null}
        directoryProvider={directory?.provider?.pluginName ?? null}
        directoryCreateLabel={directoryCreateLabel}
      />
    </AdminShell>
  );
}
