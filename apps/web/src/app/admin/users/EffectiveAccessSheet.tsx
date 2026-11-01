"use client";

import { useEffect, useState } from "react";
import { adminApi } from "@/components/admin/admin-api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { Spinner } from "@/components/ui/spinner";

type EffectiveItem = { itemType: string; itemId: string; groups: Array<{ groupId: string; name: string }> };
type Access = { administrator: boolean; items: EffectiveItem[]; groupSlugs: string[] };

const TYPE_LABELS: Record<string, string> = {
  skill: "Skills",
  workflow: "Workflows",
  metric: "Metrics",
  dashboard: "Dashboards",
  watcher: "Watchers",
  team_memory: "Team memory",
  data_source: "Data sources",
  saved_query: "Saved queries",
  api_operation: "API operations",
  action: "Actions",
  integration: "Integrations",
  channel: "Channels",
  pack: "Packs",
};

export function EffectiveAccessSheet({ userId, email }: { userId: string; email: string }) {
  const [open, setOpen] = useState(false);
  const [access, setAccess] = useState<Access | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    const timer = window.setTimeout(() => {
      void adminApi<Access>(`/api/admin/users/${encodeURIComponent(userId)}/access`).then((result) =>
        result.ok ? setAccess(result.body) : setError(result.error),
      );
    }, 0);
    return () => window.clearTimeout(timer);
  }, [open, userId]);

  const byType = new Map<string, EffectiveItem[]>();
  for (const item of access?.items ?? []) byType.set(item.itemType, [...(byType.get(item.itemType) ?? []), item]);

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (next) {
          setAccess(null);
          setError(null);
        }
        setOpen(next);
      }}
    >
      <SheetTrigger asChild>
        <Button size="sm">Access</Button>
      </SheetTrigger>
      <SheetContent className="w-full overflow-y-auto sm:max-w-md">
        <SheetHeader>
          <SheetTitle>Effective access</SheetTitle>
          <SheetDescription>{email}</SheetDescription>
        </SheetHeader>
        <div className="flex flex-col gap-4 px-4 pb-6 text-sm">
          {error ? <p className="text-danger" role="alert">{error}</p> : null}
          {!access && !error ? <Spinner /> : null}
          {access?.administrator ? (
            <p className="text-text2">This user is in Administrators and holds every item.</p>
          ) : null}
          {access && !access.administrator && access.items.length === 0 ? (
            <p className="text-text2">This user holds no items. Add them to a group that holds items.</p>
          ) : null}
          {access && !access.administrator
            ? [...byType].map(([type, items]) => (
                <div key={type}>
                  <div className="mb-2 font-semibold text-text">{TYPE_LABELS[type] ?? type}</div>
                  <ul className="flex flex-col gap-2">
                    {items.map((item) => (
                      <li key={item.itemId} className="flex flex-wrap items-center justify-between gap-2">
                        <span className="text-text2">{item.itemId === "*" ? "All current and future" : item.itemId}</span>
                        <span className="flex flex-wrap gap-1">
                          {item.groups.map((g) => <Badge key={g.groupId} variant="muted">{g.name}</Badge>)}
                        </span>
                      </li>
                    ))}
                  </ul>
                </div>
              ))
            : null}
        </div>
      </SheetContent>
    </Sheet>
  );
}
