"use client";

import { useRouter } from "next/navigation";
import { NativeSelect } from "@/components/ui/field";
import { hrefWithQuery, PAGING_RESET, RECORD_PAGE_SIZES } from "@/lib/records-filter";

export function RecordPageSize({
  base,
  query,
  size,
}: {
  base: string;
  query: Record<string, string | undefined>;
  size: number;
}) {
  const router = useRouter();
  return (
    <label className="records-page-size">
      <span>Rows per page</span>
      <NativeSelect
        value={String(size)}
        onChange={(event) =>
          router.push(hrefWithQuery(base, query, { ...PAGING_RESET, size: event.target.value === "50" ? null : event.target.value }))
        }
      >
        {RECORD_PAGE_SIZES.map((option) => (
          <option key={option} value={option}>
            {option}
          </option>
        ))}
      </NativeSelect>
    </label>
  );
}
