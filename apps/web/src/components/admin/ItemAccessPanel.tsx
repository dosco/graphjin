"use client";

import { useCallback, useEffect, useState } from "react";
import { adminApi } from "@/components/admin/admin-api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { NativeSelect } from "@/components/ui/field";

type Holder = { groupId: string; name: string; slug: string; via: string };
type Group = { id: string; name: string; slug: string };

function viaLabel(via: string): string {
  if (via === "administrators") return "Holds every item";
  if (via === "all") return "All current and future";
  if (via.startsWith("pack:")) return `Through pack ${via.slice(5)}`;
  return "Granted";
}

/**
 * Lists the groups that hold an item and lets administrators grant or revoke
 * it. Renders nothing for users who are not administrators.
 */
export function ItemAccessPanel({ itemType, itemId, label }: { itemType: string; itemId: string; label?: string }) {
  const [holders, setHolders] = useState<Holder[] | null>(null);
  const [groups, setGroups] = useState<Group[]>([]);
  const [selected, setSelected] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    const [holding, allGroups] = await Promise.all([
      adminApi<{ holders: Holder[] }>(`/api/admin/item-grants?itemType=${itemType}&itemId=${encodeURIComponent(itemId)}`),
      adminApi<{ groups: Group[] }>("/api/admin/groups"),
    ]);
    if (!holding.ok || !allGroups.ok) return setHolders(null);
    setHolders(holding.body.holders);
    const candidates = allGroups.body.groups.filter(
      (g) => g.slug !== "administrators" && !holding.body.holders.some((h) => h.groupId === g.id && (h.via === "item" || h.via === "all")),
    );
    setGroups(candidates);
    setSelected((current) => (candidates.some((g) => g.id === current) ? current : (candidates[0]?.id ?? "")));
  }, [itemType, itemId]);

  useEffect(() => {
    const timer = window.setTimeout(() => void load(), 0);
    return () => window.clearTimeout(timer);
  }, [load]);

  async function change(groupId: string, grant: boolean) {
    setBusy(true);
    setError(null);
    const result = await adminApi("/api/admin/item-grants", grant ? "POST" : "DELETE", { groupId, itemType, itemId });
    setBusy(false);
    if (!result.ok) return setError(result.error);
    await load();
  }

  if (!holders) return null;
  return (
    <section className="settings-card" data-item-access-panel="">
      <div className="settings-card-head">
        <div>
          <h2 className="settings-card-title">Access</h2>
          <p className="settings-card-copy">Groups that hold {label ?? "this item"}. Members of these groups can see and use it.</p>
        </div>
      </div>
      {error ? <p className="mb-3 text-sm text-danger" role="alert">{error}</p> : null}
      <ul className="mb-4 flex flex-col gap-2 text-sm">
        {holders.map((holder) => (
          <li key={`${holder.groupId}-${holder.via}`} className="flex flex-wrap items-center justify-between gap-2">
            <span className="flex items-center gap-2">
              <span className="font-semibold text-text">{holder.name}</span>
              <Badge variant="muted">{viaLabel(holder.via)}</Badge>
            </span>
            {holder.via === "item" ? (
              <Button size="sm" variant="ghost" disabled={busy} onClick={() => void change(holder.groupId, false)}>Remove</Button>
            ) : null}
          </li>
        ))}
      </ul>
      {groups.length > 0 && (
        <div className="grid grid-cols-[minmax(200px,1fr)_auto] items-end gap-3 max-[520px]:grid-cols-1">
          <NativeSelect aria-label="Group" value={selected} onChange={(e) => setSelected(e.target.value)}>
            {groups.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
          </NativeSelect>
          <Button disabled={busy || !selected} onClick={() => void change(selected, true)}>Grant to group</Button>
        </div>
      )}
    </section>
  );
}
