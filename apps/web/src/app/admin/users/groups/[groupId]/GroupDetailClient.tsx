"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import type { GroupMemberRow, UserGroupRow } from "@neko/db";
import { AdminError } from "@/components/admin/AdminError";
import { adminApi } from "@/components/admin/admin-api";
import { confirmDialog } from "@/components/ConfirmModal";
import { ActionGroup } from "@/components/ui/action-group";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Field, Input, NativeSelect } from "@/components/ui/field";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { DataAccessSection, type DataAccessRuleView } from "./DataAccessSection";
import { ItemsSection } from "./ItemsSection";

export type ItemTypeView = { type: string; label: string; plural: string };

export function GroupDetailClient({
  group,
  members,
  grants,
  rules,
  users,
  itemTypes,
  groupGrantsEnabled,
}: {
  group: UserGroupRow;
  members: GroupMemberRow[];
  grants: Array<{ itemType: string; itemId: string }>;
  rules: DataAccessRuleView[];
  users: Array<{ id: string; email: string; name: string | null }>;
  itemTypes: ItemTypeView[];
  groupGrantsEnabled: boolean;
}) {
  const router = useRouter();
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [name, setName] = useState(group.name);
  const [description, setDescription] = useState(group.description ?? "");
  const everyone = group.slug === "everyone";
  const administrators = group.slug === "administrators";
  const memberIds = new Set(members.map((m) => m.userId));
  const candidates = users.filter((u) => !memberIds.has(u.id));
  const [newMember, setNewMember] = useState(candidates[0]?.id ?? "");

  async function run(key: string, call: () => Promise<{ ok: boolean; error?: string }>, after?: () => void) {
    setBusy(key);
    setError(null);
    const result = await call();
    setBusy(null);
    if (!result.ok) return setError(result.error ?? "Request failed");
    after?.();
    router.refresh();
  }

  return (
    <>
      <AdminError message={error} />

      {group.kind === "custom" && (
        <section className="settings-card">
          <div className="settings-card-head">
            <div>
              <h2 className="settings-card-title">Details</h2>
              <p className="settings-card-copy">The GraphJin role for this group is og_{group.slug}. The role name does not change when you rename the group.</p>
            </div>
          </div>
          <form
            className="grid grid-cols-[minmax(200px,1fr)_minmax(240px,1.4fr)_auto] items-end gap-3 max-[820px]:grid-cols-1"
            onSubmit={(event) => {
              event.preventDefault();
              void run("details", () => adminApi(`/api/admin/groups/${group.id}`, "PATCH", { name, description: description || null }));
            }}
          >
            <Field label="Name" htmlFor="group-name">
              <Input id="group-name" required maxLength={120} value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
            <Field label="Description" htmlFor="group-description">
              <Input id="group-description" value={description} onChange={(e) => setDescription(e.target.value)} />
            </Field>
            <ActionGroup align="start">
              <Button type="submit" disabled={busy === "details"}>Save</Button>
              <Button
                type="button"
                variant="danger"
                disabled={busy === "delete"}
                onClick={async () => {
                  const ok = await confirmDialog({
                    title: `Delete ${group.name}?`,
                    description: "Members lose the items this group holds. IdP rules for this group are removed.",
                    confirmLabel: "Delete group",
                    destructive: true,
                  });
                  if (ok) void run("delete", () => adminApi(`/api/admin/groups/${group.id}`, "DELETE"), () => router.push("/admin/users?tab=groups"));
                }}
              >
                Delete group
              </Button>
            </ActionGroup>
          </form>
        </section>
      )}

      <section className="settings-card">
        <div className="settings-card-head">
          <div>
            <h2 className="settings-card-title">Members</h2>
            <p className="settings-card-copy">
              {everyone
                ? "Every active user belongs to Everyone. You cannot add or remove members."
                : "Add members here, or add an IdP rule. Members from an IdP rule change only in the identity provider."}
            </p>
          </div>
          <div className="settings-source">
            <strong className="is-ok">{members.length} {members.length === 1 ? "member" : "members"}</strong>
          </div>
        </div>
        {!everyone && candidates.length > 0 && (
          <form
            className="mb-5 grid grid-cols-[minmax(240px,1fr)_auto] items-end gap-3 max-[520px]:grid-cols-1"
            onSubmit={(event) => {
              event.preventDefault();
              void run("add-member", () => adminApi(`/api/admin/groups/${group.id}/members`, "POST", { userId: newMember }));
            }}
          >
            <Field label="Add member" htmlFor="add-member">
              <NativeSelect id="add-member" value={newMember} onChange={(e) => setNewMember(e.target.value)}>
                {candidates.map((u) => (
                  <option key={u.id} value={u.id}>{u.name ? `${u.name} · ${u.email}` : u.email}</option>
                ))}
              </NativeSelect>
            </Field>
            <Button type="submit" variant="primary" disabled={busy === "add-member" || !newMember}>Add member</Button>
          </form>
        )}
        {members.length === 0 ? (
          <p className="text-sm text-text2">No members yet. Add a user above, or add an IdP rule for this group.</p>
        ) : (
        <div className="overflow-x-auto">
          <Table className="w-full border-collapse text-left text-sm">
            <TableHeader>
              <TableRow>
                <TableHead className="border-b border-border px-3 py-2 font-bold">User</TableHead>
                <TableHead className="border-b border-border px-3 py-2 font-bold">Membership</TableHead>
                <TableHead className="border-b border-border px-3 py-2 font-bold">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {members.map((member) => (
                <TableRow key={member.userId} className="border-b border-border last:border-0">
                  <TableCell className="px-3 py-3">
                    <div className="font-semibold text-text">{member.email}</div>
                    <div className="text-xs text-text3">{member.name ?? member.userId}{member.disabled ? " · disabled" : ""}</div>
                  </TableCell>
                  <TableCell className="px-3 py-3">
                    <div className="flex flex-wrap gap-1">
                      {member.sources.map((source) => (
                        <Badge key={source} variant={source === "local" ? "muted" : "secondary"}>
                          {source === "local" ? "Added in OpenNeko" : source === "implicit" ? "Every active user" : source}
                        </Badge>
                      ))}
                    </div>
                  </TableCell>
                  <TableCell className="px-3 py-3">
                    {member.sources.includes("local") && !everyone ? (
                      <Button
                        size="sm"
                        variant="danger"
                        disabled={busy === member.userId}
                        onClick={() => run(member.userId, () => adminApi(`/api/admin/groups/${group.id}/members`, "DELETE", { userId: member.userId }))}
                      >
                        Remove
                      </Button>
                    ) : everyone ? null : (
                      <span className="text-xs text-text3">Managed by the identity provider</span>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        )}
      </section>

      {administrators ? (
        <section className="settings-card">
          <div className="settings-card-head">
            <div>
              <h2 className="settings-card-title">Items and data access</h2>
              <p className="settings-card-copy">Administrators hold every item and read every table. Administration pages are open only to this group.</p>
            </div>
          </div>
        </section>
      ) : (
        <>
          <ItemsSection groupId={group.id} grants={grants} itemTypes={itemTypes} onError={setError} />
          <DataAccessSection group={group} rules={rules} enabled={groupGrantsEnabled} onError={setError} />
        </>
      )}
    </>
  );
}
