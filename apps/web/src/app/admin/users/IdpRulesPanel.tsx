"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import type { IdpGroupRow, UserGroupRow } from "@neko/db";
import { AdminError } from "@/components/admin/AdminError";
import { adminApi } from "@/components/admin/admin-api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty";
import { Field, NativeSelect } from "@/components/ui/field";
import { LocalDateTime } from "@/components/ui/local-date-time";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

export type IdpRuleView = {
  id: string;
  ssoGroupId: string;
  idpGroupName: string;
  provider: string;
  userGroupId: string;
  userGroupName: string;
  createdAt: string;
};

type DirectoryStatus = {
  provider: { providerLabel: string } | null;
  state: { status: string; finishedAt: string | null; lastError: string | null; stats: Record<string, number> };
};

export function IdpRulesPanel({
  groups,
  idpGroups,
  rules,
  directoryProvider,
}: {
  groups: UserGroupRow[];
  idpGroups: IdpGroupRow[];
  rules: IdpRuleView[];
  directoryProvider: string | null;
}) {
  const router = useRouter();
  const targets = groups.filter((g) => g.slug !== "everyone");
  const [ssoGroupId, setSsoGroupId] = useState(idpGroups.find((g) => g.active)?.id ?? "");
  const [userGroupId, setUserGroupId] = useState(targets.find((g) => g.kind === "custom")?.id ?? targets[0]?.id ?? "");
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [directory, setDirectory] = useState<DirectoryStatus | null>(null);

  useEffect(() => {
    if (!directoryProvider) return;
    void adminApi<DirectoryStatus>("/api/admin/directory").then((result) => {
      if (result.ok) setDirectory(result.body);
    });
  }, [directoryProvider]);

  async function run(key: string, call: () => Promise<{ ok: boolean; error?: string }>) {
    setBusy(key);
    setError(null);
    const result = await call();
    setBusy(null);
    if (!result.ok) setError(result.error ?? "Request failed");
    else router.refresh();
  }

  return (
    <>
      <div className="settings-card-head">
        <div>
          <h2 className="settings-card-title">IdP rules</h2>
          <p className="settings-card-copy">
            Put members of an identity provider group into an OpenNeko group. The provider decides these memberships; they change at sign-in and at each directory sync.
          </p>
        </div>
      </div>
      <AdminError message={error} />

      {directoryProvider && (
        <div className="mb-6 flex flex-wrap items-center justify-between gap-3 rounded-inner border border-border px-4 py-3">
          <div className="text-sm text-text2">
            <div className="font-semibold text-text">Directory sync · {directory?.provider?.providerLabel ?? directoryProvider}</div>
            {directory?.state.finishedAt ? (
              <div>
                Last sync {directory.state.status === "ok" ? "succeeded" : directory.state.status} <LocalDateTime value={directory.state.finishedAt} />
                {directory.state.lastError ? ` · ${directory.state.lastError}` : ""}
              </div>
            ) : (
              <div>Not synced yet. Sync runs every 6 hours.</div>
            )}
          </div>
          <Button
            size="sm"
            disabled={busy === "sync"}
            onClick={() =>
              run("sync", async () => {
                const result = await adminApi("/api/admin/directory", "POST");
                const status = await adminApi<DirectoryStatus>("/api/admin/directory");
                if (status.ok) setDirectory(status.body);
                return result;
              })
            }
          >
            {busy === "sync" ? "Syncing…" : "Sync now"}
          </Button>
        </div>
      )}

      {idpGroups.length === 0 ? (
        <EmptyState
          title="No IdP groups yet"
          body="IdP groups appear after a user signs in with SSO or after a directory sync."
        />
      ) : (
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void run("create", () => adminApi("/api/admin/idp-rules", "POST", { ssoGroupId, userGroupId }));
          }}
          className="mb-6 grid grid-cols-[minmax(220px,1fr)_minmax(220px,1fr)_auto] items-end gap-3 max-[820px]:grid-cols-1"
        >
          <Field label="IdP group" htmlFor="rule-idp-group">
            <NativeSelect id="rule-idp-group" value={ssoGroupId} onChange={(e) => setSsoGroupId(e.target.value)}>
              {idpGroups.map((g) => (
                <option key={g.id} value={g.id}>
                  {g.displayName ?? g.externalId} · {providerName(g.provider)}
                  {g.active ? "" : " (inactive)"}
                </option>
              ))}
            </NativeSelect>
          </Field>
          <Field label="OpenNeko group" htmlFor="rule-user-group">
            <NativeSelect id="rule-user-group" value={userGroupId} onChange={(e) => setUserGroupId(e.target.value)}>
              {targets.map((g) => (
                <option key={g.id} value={g.id}>{g.name}</option>
              ))}
            </NativeSelect>
          </Field>
          <Button type="submit" variant="primary" disabled={busy === "create" || !ssoGroupId || !userGroupId}>
            {busy === "create" ? "Adding…" : "Add rule"}
          </Button>
        </form>
      )}

      {rules.length > 0 && (
        <div className="overflow-x-auto">
          <Table className="w-full border-collapse text-left text-sm">
            <TableHeader>
              <TableRow>
                <TableHead className="border-b border-border px-3 py-2 font-bold">IdP group</TableHead>
                <TableHead className="border-b border-border px-3 py-2 font-bold">OpenNeko group</TableHead>
                <TableHead className="border-b border-border px-3 py-2 font-bold">Added</TableHead>
                <TableHead className="border-b border-border px-3 py-2 font-bold">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rules.map((rule) => (
                <TableRow key={rule.id} className="border-b border-border last:border-0">
                  <TableCell className="px-3 py-3">
                    <div className="font-semibold text-text">{rule.idpGroupName}</div>
                    <Badge variant="muted">{providerName(rule.provider)}</Badge>
                  </TableCell>
                  <TableCell className="px-3 py-3 text-text2">{rule.userGroupName}</TableCell>
                  <TableCell className="px-3 py-3 text-text2"><LocalDateTime value={rule.createdAt} /></TableCell>
                  <TableCell className="px-3 py-3">
                    <Button
                      size="sm"
                      variant="danger"
                      disabled={busy === rule.id}
                      onClick={() => run(rule.id, () => adminApi(`/api/admin/idp-rules/${rule.id}`, "DELETE"))}
                    >
                      Remove
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </>
  );
}

function providerName(provider: string): string {
  const name = provider.replace(/^@[^/]+\//, "");
  return name.charAt(0).toUpperCase() + name.slice(1);
}
