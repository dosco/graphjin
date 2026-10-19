"use client";

import { useRouter } from "next/navigation";
import type { IdpGroupRow, UserGroupRow } from "@neko/db";
import { Tab, Tabs } from "@/components/ui/tabs";
import { GroupsPanel } from "./GroupsPanel";
import { IdpRulesPanel, type IdpRuleView } from "./IdpRulesPanel";
import { UsersClient, type AdminUserRow } from "./UsersClient";

export type UsersAdminTab = "users" | "groups" | "idp-rules";

export function UsersAdminTabs({
  tab,
  users,
  identitySetup,
  groups,
  idpGroups,
  rules,
  signInProvider,
  directoryProvider,
  directoryCreateLabel,
}: {
  tab: UsersAdminTab;
  users: AdminUserRow[];
  identitySetup: boolean;
  groups: UserGroupRow[];
  idpGroups: IdpGroupRow[];
  rules: IdpRuleView[];
  signInProvider: string | null;
  directoryProvider: string | null;
  directoryCreateLabel: string | null;
}) {
  const router = useRouter();
  const select = (next: UsersAdminTab) => router.replace(next === "users" ? "/admin/users" : `/admin/users?tab=${next}`, { scroll: false });

  return (
    <section className="settings-card">
      {!identitySetup && (
        <Tabs aria-label="User administration" className="mb-5">
          <Tab selected={tab === "users"} onClick={() => select("users")}>Users</Tab>
          <Tab selected={tab === "groups"} onClick={() => select("groups")}>Groups</Tab>
          <Tab selected={tab === "idp-rules"} onClick={() => select("idp-rules")}>IdP rules</Tab>
        </Tabs>
      )}
      {tab === "users" && (
        <>
          <div className="settings-card-head">
            <div>
              <h2 className="settings-card-title">{identitySetup ? "Your admin email" : "Users"}</h2>
              <p className="settings-card-copy">
                {identitySetup
                  ? "Your local account is ready to use. Add your email before you turn on SSO; your work stays with this account."
                  : signInProvider
                    ? "Add users before their first sign-in, choose administrators, and disable accounts. Group membership decides which items each user holds."
                    : "This installation runs as a single operator until you set up sign-in."}
              </p>
            </div>
            <div className="settings-source">
              <strong>{users.length} total</strong>
            </div>
          </div>
          <UsersClient
            users={users}
            identitySetup={identitySetup}
            signInProvider={signInProvider}
            directoryCreateLabel={directoryCreateLabel}
          />
        </>
      )}
      {tab === "groups" && <GroupsPanel groups={groups} />}
      {tab === "idp-rules" && (
        <IdpRulesPanel groups={groups} idpGroups={idpGroups} rules={rules} directoryProvider={directoryProvider} />
      )}
    </section>
  );
}
