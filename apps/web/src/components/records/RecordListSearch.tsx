"use client";

import { useRouter } from "next/navigation";
import { useEffect, useRef, useState, useTransition } from "react";
import { SearchInput } from "@/components/ui/search-input";
import { hrefWithQuery, PAGING_RESET } from "@/lib/records-filter";

export function RecordListSearch({
  base,
  query,
  pluralLabel,
  nameLabel,
}: {
  base: string;
  query: Record<string, string | undefined>;
  pluralLabel: string;
  nameLabel: string;
}) {
  const router = useRouter();
  const [value, setValue] = useState(query.q ?? "");
  const [pending, startTransition] = useTransition();
  const applied = useRef(query.q ?? "");
  const latestQuery = useRef(query);
  useEffect(() => {
    latestQuery.current = query;
  });

  useEffect(() => {
    const next = value.trim();
    if (next === applied.current.trim()) return;
    const timer = window.setTimeout(() => {
      applied.current = next;
      startTransition(() => {
        router.replace(hrefWithQuery(base, latestQuery.current, { ...PAGING_RESET, q: next || "" }), { scroll: false });
      });
    }, 300);
    return () => window.clearTimeout(timer);
  }, [base, router, value]);

  return (
    <div className="records-search" data-pending={pending || undefined}>
      <SearchInput
        name="q"
        value={value}
        onChange={(event) => setValue(event.target.value)}
        maxLength={200}
        placeholder={`Search by ${nameLabel.toLowerCase()}`}
        label={`Search ${pluralLabel} by ${nameLabel.toLowerCase()}`}
        aria-busy={pending || undefined}
      />
    </div>
  );
}
