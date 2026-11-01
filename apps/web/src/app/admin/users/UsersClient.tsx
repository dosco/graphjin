"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { ActionGroup } from "@/components/ui/action-group";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Field, Input, NativeSelect } from "@/components/ui/field";
import { Badge } from "@/components/ui/badge";
import { SearchInput } from "@/components/ui/search-input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { matchesListSearch } from "@/lib/list-search";
import { EffectiveAccessSheet } from "./EffectiveAccessSheet";

export interface AdminUserRow {
  id: string;
  email: string;
  name: string | null;
  role: string;
  source: string;
  groups: Array<{ name: string; fromRule: boolean }>;
  disabled: boolean;
  hasSignedIn: boolean;
  lastLoginAt: string | null;
  createdAt: string | null;
}

/**
 * Interactive user administration: provision users ahead of first
 * sign-in (required for manual-provisioning auth like magic link),
 * change roles, and disable/enable accounts. The API enforces the
 * last-active-admin guard; errors from it surface inline.
 */
export function UsersClient({
  users,
  identitySetup = false,
  signInProvider = null,
  directoryCreateLabel = null,
}: {
  users: AdminUserRow[];
  identitySetup?: boolean;
  signInProvider?: string | null;
  directoryCreateLabel?: string | null;
}) {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [role, setRole] = useState<"member" | "admin">(identitySetup ? "admin" : "member");
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [addToDirectory, setAddToDirectory] = useState(true);
  const [query, setQuery] = useState("");
  const visibleUsers = users.filter((user) =>
    matchesListSearch(
      query,
      user.email,
      user.name,
      user.role,
      ...user.groups.map((g) => g.name),
      user.disabled ? "disabled" : "active",
    ),
  );

  async function callApi(path: string, init: RequestInit): Promise<Record<string, unknown> | null> {
    setError(null);
    try {
      const res = await fetch(path, {
        headers: { "Content-Type": "application/json" },
        ...init,
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as {
          error?: string;
        } | null;
        setError(body?.error ?? `request failed (${res.status})`);
        return null;
      }
      router.refresh();
      return ((await res.json().catch(() => null)) as Record<string, unknown> | null) ?? {};
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      return null;
    }
  }

  async function createUser(event: React.FormEvent) {
    event.preventDefault();
    setBusy("create");
    const created = await callApi("/api/admin/users", {
      method: "POST",
      body: JSON.stringify({
        email,
        name: name.trim().length > 0 ? name : undefined,
        role,
        ...(identitySetup ? { updateSoloAccount: true } : {}),
        ...(directoryCreateLabel && !identitySetup ? { addToDirectory } : {}),
      }),
    });
    if (typeof created?.directoryError === "string") {
      setError(`The user was added to OpenNeko. ${directoryCreateLabel} did not create the user: ${created.directoryError}`);
    }
    if (created) {
      setEmail("");
      setName("");
      setRole("member");
    }
    setBusy(null);
  }

  async function patchUser(
    id: string,
    patch: { role?: string; disabled?: boolean },
  ) {
    setBusy(id);
    await callApi(`/api/admin/users/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    });
    setBusy(null);
  }

  return (
    <>
      {error ? (
        <div
          className="mb-4 rounded-control bg-danger-soft px-3 py-2 text-sm text-danger"
          role="alert"
        >
          {error}
        </div>
      ) : null}

      {!identitySetup && <div className="mb-4 max-w-[520px]">
        <SearchInput
          label="Search users"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Search users by name, email, role, or status"
        />
      </div>}

      {identitySetup && <p className="mb-4 text-sm text-text2">Use the email you will sign in with if you enable SSO later.</p>}
      {/* A user row without a sign-in provider promises an account nobody
          can reach, and asks a real person for their address to do it. */}
      {!signInProvider && !identitySetup ? (
        <div className="mb-6 flex flex-wrap items-center justify-between gap-3 rounded-control border border-border bg-neutral-soft px-4 py-3">
          <p className="text-ui-body-sm text-text2">
            Set up single sign-on or email-link sign-in to add other people.
          </p>
          <Button asChild size="sm">
            <Link href="/admin/settings">Open Settings</Link>
          </Button>
        </div>
      ) : (
      <form
        onSubmit={createUser}
        className="mb-6 grid grid-cols-[minmax(220px,1.4fr)_minmax(180px,1fr)_minmax(130px,0.6fr)_auto] items-end gap-3 max-[820px]:grid-cols-2 max-[520px]:grid-cols-1"
      >
        <Field label="Email" htmlFor="new-user-email">
          <Input
            id="new-user-email"
            name="email"
            autoComplete="email"
            spellCheck={false}
            type="email"
            required
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            placeholder="person@company.com"
          />
        </Field>
        <Field label="Name (optional)" htmlFor="new-user-name">
          <Input
            id="new-user-name"
            name="name"
            autoComplete="name"
            type="text"
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="Display name"
          />
        </Field>
        <Field label="Role" htmlFor="new-user-role">
          <NativeSelect
            id="new-user-role"
            disabled={identitySetup}
            value={role}
            onChange={(event) =>
              setRole(event.target.value === "admin" ? "admin" : "member")
            }
          >
            <option value="member">Member</option>
            <option value="admin">Administrator</option>
          </NativeSelect>
        </Field>
        <Button type="submit" variant="primary" disabled={busy === "create"}>
          {busy === "create" ? "Saving…" : identitySetup ? "Save email" : "Add user"}
        </Button>
        {directoryCreateLabel && !identitySetup && (
          <Checkbox
            className="col-span-full"
            checked={addToDirectory}
            onCheckedChange={(value) => setAddToDirectory(value === true)}
            label={`Also create the user in ${directoryCreateLabel}`}
          />
        )}
      </form>
      )}

      {visibleUsers.length === 0 ? (
        <p className="text-sm text-text2">
          {query
            ? "No users match this search."
            : identitySetup ? "Your account already exists. Adding an email is optional until you enable SSO." : "No users yet. Add one above to allow them to sign in."}
        </p>
      ) : (
        <div className="overflow-x-auto">
          <Table className="w-full border-collapse text-left text-sm">
            <TableHeader>
              <TableRow>
                <TableHead className="border-b border-border px-3 py-2 font-bold">
                  User
                </TableHead>
                <TableHead className="border-b border-border px-3 py-2 font-bold">
                  Role
                </TableHead>
                <TableHead className="border-b border-border px-3 py-2 font-bold">
                  Groups
                </TableHead>
                <TableHead className="border-b border-border px-3 py-2 font-bold">
                  Status
                </TableHead>
                <TableHead className="border-b border-border px-3 py-2 font-bold">
                  Last login
                </TableHead>
                <TableHead className="border-b border-border px-3 py-2 font-bold">
                  Actions
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {visibleUsers.map((user) => (
                <TableRow
                  key={user.id}
                  className="border-b border-border last:border-0"
                >
                  <TableCell className="px-3 py-3">
                    <div className="font-semibold text-text">{user.email}</div>
                    <div className="text-xs text-text3">
                      {user.name ?? user.id}
                      {user.source !== "local" ? ` · ${user.source}` : null}
                      {user.hasSignedIn ? null : " · never signed in"}
                    </div>
                  </TableCell>
                  <TableCell className="px-3 py-3">
                    <RoleBadge role={user.role} />
                  </TableCell>
                  <TableCell className="px-3 py-3">
                    <div className="flex max-w-[260px] flex-wrap gap-1">
                      {user.groups.filter((g) => g.name !== "Administrators").map((g) => (
                        <Badge key={g.name} variant={g.fromRule ? "secondary" : "muted"} title={g.fromRule ? "From an IdP rule" : "Added in OpenNeko"}>
                          {g.name}{g.fromRule ? " · IdP" : ""}
                        </Badge>
                      ))}
                      <Badge variant="outline">Everyone</Badge>
                    </div>
                  </TableCell>
                  <TableCell className="px-3 py-3">
                    <StatusBadge disabled={user.disabled} />
                  </TableCell>
                  <TableCell className="px-3 py-3 text-text2">
                    <div className="whitespace-nowrap">{formatDate(user.lastLoginAt)}</div>
                    {user.createdAt ? <div className="whitespace-nowrap text-xs text-text3">Created {formatDay(user.createdAt)}</div> : null}
                  </TableCell>
                  <TableCell className="px-3 py-3">
                    <ActionGroup align="start" className="min-w-[220px]">
                      <EffectiveAccessSheet userId={user.id} email={user.email} />
                      <Button
                        size="sm"
                        disabled={busy === user.id}
                        onClick={() =>
                          patchUser(user.id, {
                            role: user.role === "admin" ? "member" : "admin",
                          })
                        }
                      >
                        {user.role === "admin" ? "Make member" : "Make admin"}
                      </Button>
                      <Button
                        variant={user.disabled ? "primary" : "danger"}
                        size="sm"
                        disabled={busy === user.id}
                        onClick={() =>
                          patchUser(user.id, { disabled: !user.disabled })
                        }
                      >
                        {user.disabled ? "Enable" : "Disable"}
                      </Button>
                    </ActionGroup>
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

function RoleBadge({ role }: { role: string }) {
  const isAdmin = role === "admin";
  return <Badge variant={isAdmin ? "secondary" : "muted"}>{isAdmin ? "Administrator" : "Member"}</Badge>;
}

function StatusBadge({ disabled }: { disabled: boolean }) {
  return (
    <Badge variant={disabled ? "danger" : "success"}>
      {disabled ? "Disabled" : "Active"}
    </Badge>
  );
}

function formatDay(value: string): string {
  return new Date(value).toLocaleDateString("en-US", { year: "numeric", month: "short", day: "2-digit" });
}

function formatDate(value: string | null): string {
  if (!value) return "Never";
  return new Date(value).toLocaleString("en-US", {
    year: "numeric",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}
