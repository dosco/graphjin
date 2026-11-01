"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { adminApi } from "@/components/admin/admin-api";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Disclosure } from "@/components/ui/disclosure";
import { SearchInput } from "@/components/ui/search-input";
import { Spinner } from "@/components/ui/spinner";
import type { ItemTypeView } from "./GroupDetailClient";

type ItemOption = { id: string; label: string; detail?: string };
const MAX_SELECTION = 500;

function ItemTypeGrants({
  groupId,
  type,
  grants,
  onError,
}: {
  groupId: string;
  type: ItemTypeView;
  grants: string[];
  onError: (message: string | null) => void;
}) {
  const router = useRouter();
  const [options, setOptions] = useState<ItemOption[] | null>(null);
  const [selected, setSelected] = useState<string[]>([]);
  const [search, setSearch] = useState("");
  const [loadError, setLoadError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const all = grants.includes("*");
  const specific = grants.filter((id) => id !== "*");
  const labels = new Map((options ?? []).map((o) => [o.id, o]));
  const available = (options ?? []).filter((o) => !grants.includes(o.id));
  const matches = available.filter((option) => `${option.label} ${option.detail ?? ""}`.toLowerCase().includes(search.trim().toLowerCase()));
  const shown = matches.slice(0, 100);

  async function load() {
    if (options) return;
    const result = await adminApi<{ items: ItemOption[] }>(`/api/admin/items?type=${type.type}`);
    if (!result.ok) {
      setLoadError(result.error);
      return;
    }
    setLoadError(null);
    setOptions(result.body.items);
  }

  async function change(itemId: string, grant: boolean) {
    setBusy(true);
    onError(null);
    const result = await adminApi("/api/admin/item-grants", grant ? "POST" : "DELETE", { groupId, itemType: type.type, itemId });
    setBusy(false);
    if (!result.ok) return onError(result.error);
    router.refresh();
  }

  async function grantSelected() {
    const itemIds = selected.filter((id) => available.some((option) => option.id === id));
    if (itemIds.length === 0) return;
    setBusy(true);
    onError(null);
    const result = await adminApi("/api/admin/item-grants", "POST", {
      groupId, itemType: type.type, itemIds,
    });
    setBusy(false);
    if (!result.ok) return onError(result.error);
    setSelected([]);
    router.refresh();
  }

  return (
    <Disclosure
      title={type.plural}
      meta={all ? "All" : specific.length ? `${specific.length} granted` : "None"}
      onToggle={(event) => {
        if ((event.currentTarget as HTMLDetailsElement).open) void load();
      }}
    >
      <div className="flex flex-col gap-3 border-t border-border px-3.5 py-3 text-sm">
        <Checkbox
          label={`All current and future ${type.plural.toLowerCase()}`}
          checked={all}
          disabled={busy}
          onCheckedChange={(checked) => void change("*", checked === true)}
        />
        {specific.length > 0 && (
          <ul className="flex flex-col gap-2">
            {specific.map((id) => (
              <li key={id} className="flex flex-wrap items-center justify-between gap-2">
                <span>
                  <Link className="font-semibold text-text hover:text-accent" href={`/admin/access?type=${type.type}&id=${encodeURIComponent(id)}`}>
                    {labels.get(id)?.label ?? id}
                  </Link>
                  {labels.get(id)?.detail ? <span className="block text-xs text-text3">{labels.get(id)?.detail}</span> : null}
                </span>
                <Button size="sm" variant="ghost" disabled={busy} onClick={() => void change(id, false)}>Remove</Button>
              </li>
            ))}
          </ul>
        )}
        {all ? (
          <p className="text-text2">Every {type.label.toLowerCase()} is already available to this group. Turn off the all-items grant to choose specific items.</p>
        ) : loadError ? (
          <div className="flex flex-wrap items-center gap-3">
            <p role="alert" className="text-danger">Could not load {type.plural.toLowerCase()}: {loadError}</p>
            <Button size="sm" onClick={() => void load()}>Try again</Button>
          </div>
        ) : options === null ? (
          <Spinner />
        ) : available.length === 0 ? (
          <p className="text-text3">{options.length === 0 ? `No ${type.plural.toLowerCase()} exist yet.` : `Every ${type.label.toLowerCase()} is granted.`}</p>
        ) : (
          <div className="flex flex-col gap-3">
            <SearchInput
              label={`Search ${type.plural.toLowerCase()} to grant`}
              placeholder={`Search ${type.plural.toLowerCase()}`}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
            />
            <div className="flex flex-wrap items-center justify-between gap-2 text-ui-caption text-text3" aria-live="polite">
              <span>{matches.length} available{matches.length > shown.length ? ` · Showing first ${shown.length}` : ""} · {selected.length} selected{selected.length === MAX_SELECTION ? " (limit reached)" : ""}</span>
              <div className="flex gap-2">
                <Button size="sm" variant="ghost" disabled={busy || shown.length === 0 || selected.length === MAX_SELECTION} onClick={() => setSelected((current) => [...new Set([...current, ...shown.map((option) => option.id)])].slice(0, MAX_SELECTION))}>Select shown</Button>
                <Button size="sm" variant="ghost" disabled={busy || selected.length === 0} onClick={() => setSelected([])}>Clear</Button>
              </div>
            </div>
            {shown.length === 0 ? (
              <p className="text-text3">No matches. Try a different search.</p>
            ) : (
              <ul className="max-h-72 overflow-y-auto rounded-inner border border-border">
                {shown.map((option) => (
                  <li key={option.id} className="border-b border-border px-3 py-2.5 last:border-0">
                    <Checkbox
                      className="w-full"
                      checked={selected.includes(option.id)}
                      disabled={busy || (selected.length === MAX_SELECTION && !selected.includes(option.id))}
                      onCheckedChange={(checked) => setSelected((current) => checked === true
                        ? [...new Set([...current, option.id])].slice(0, MAX_SELECTION)
                        : current.filter((id) => id !== option.id))}
                      label={<span className="block min-w-0"><span className="block font-semibold text-text">{option.label}</span>{option.detail ? <span className="block text-ui-caption text-text3">{option.detail}</span> : null}</span>}
                    />
                  </li>
                ))}
              </ul>
            )}
            <Button variant="primary" disabled={busy || selected.length === 0} onClick={() => void grantSelected()}>{busy ? "Granting…" : `Grant ${selected.length} selected`}</Button>
          </div>
        )}
      </div>
    </Disclosure>
  );
}

export function ItemsSection({
  groupId,
  grants,
  itemTypes,
  onError,
}: {
  groupId: string;
  grants: Array<{ itemType: string; itemId: string }>;
  itemTypes: ItemTypeView[];
  onError: (message: string | null) => void;
}) {
  return (
    <section className="settings-card">
      <div className="settings-card-head">
        <div>
          <h2 className="settings-card-title">Items</h2>
          <p className="settings-card-copy">
            Members can see and use each item this group holds, and OpenNeko uses those items in their agent runs. A pack grant covers every item the pack installs.
          </p>
        </div>
      </div>
      <div className="flex flex-col gap-2">
        {itemTypes.map((type) => (
          <ItemTypeGrants
            key={type.type}
            groupId={groupId}
            type={type}
            grants={grants.filter((g) => g.itemType === type.type).map((g) => g.itemId)}
            onError={onError}
          />
        ))}
      </div>
    </section>
  );
}
