"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import type { UserGroupRow } from "@neko/db";
import { AdminError } from "@/components/admin/AdminError";
import { adminApi } from "@/components/admin/admin-api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Field, Input } from "@/components/ui/field";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

export function GroupsPanel({ groups }: { groups: UserGroupRow[] }) {
  const router = useRouter();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function create(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    const result = await adminApi<{ group: { id: string } }>("/api/admin/groups", "POST", { name, description });
    setBusy(false);
    if (!result.ok) return setError(result.error);
    router.push(`/admin/users/groups/${result.body.group.id}`);
  }

  return (
    <>
      <div className="settings-card-head">
        <div>
          <h2 className="settings-card-title">Groups</h2>
          <p className="settings-card-copy">
            Grant skills, workflows, library collections, metrics and other items to groups. A user holds every item that any of their groups holds.
          </p>
        </div>
        <div className="settings-source">
          <strong className="is-ok">{groups.length} {groups.length === 1 ? "group" : "groups"}</strong>
        </div>
      </div>
      <AdminError message={error} />
      <form
        onSubmit={create}
        className="mb-6 grid grid-cols-[minmax(200px,1fr)_minmax(240px,1.4fr)_auto] items-end gap-3 max-[820px]:grid-cols-1"
      >
        <Field label="Group name" htmlFor="new-group-name">
          <Input id="new-group-name" required maxLength={120} value={name} onChange={(e) => setName(e.target.value)} placeholder="Finance" />
        </Field>
        <Field label="Description (optional)" htmlFor="new-group-description">
          <Input id="new-group-description" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Month-end close and revenue reporting" />
        </Field>
        <Button type="submit" variant="primary" disabled={busy || !name.trim()}>
          {busy ? "Creating…" : "Create group"}
        </Button>
      </form>
      <div className="overflow-x-auto">
        <Table className="w-full border-collapse text-left text-sm">
          <TableHeader>
            <TableRow>
              <TableHead className="border-b border-border px-3 py-2 font-bold">Group</TableHead>
              <TableHead className="border-b border-border px-3 py-2 font-bold">Members</TableHead>
              <TableHead className="border-b border-border px-3 py-2 font-bold">IdP rules</TableHead>
              <TableHead className="border-b border-border px-3 py-2 font-bold">Type</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {groups.map((group) => (
              <TableRow key={group.id} className="border-b border-border last:border-0">
                <TableCell className="px-3 py-3">
                  <Link className="font-semibold text-text hover:text-accent" href={`/admin/users/groups/${group.id}`}>
                    {group.name}
                  </Link>
                  {group.description ? <div className="text-xs text-text3">{group.description}</div> : null}
                </TableCell>
                <TableCell className="px-3 py-3 text-text2">
                  {group.slug === "everyone" ? "All active users" : group.memberCount}
                </TableCell>
                <TableCell className="px-3 py-3 text-text2">{group.ruleCount}</TableCell>
                <TableCell className="px-3 py-3">
                  <Badge variant={group.kind === "builtin" ? "secondary" : "muted"}>{group.kind === "builtin" ? "Built-in" : "Custom"}</Badge>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </>
  );
}
