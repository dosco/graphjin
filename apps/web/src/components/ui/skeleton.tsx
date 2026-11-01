import type { CSSProperties } from "react";
import { cn } from "@/lib/cn";

export function Skeleton({ className, style }: { className?: string; style?: CSSProperties }) {
  return <span aria-hidden="true" data-slot="skeleton" className={cn("block rounded-[6px]", className)} style={style} />;
}

// Placeholder rows that share the shape of the list they stand in for, so
// the page does not jump when the data arrives.
export function SkeletonList({
  rows = 3,
  label = "Loading",
  variant = "card",
  className,
}: {
  rows?: number;
  label?: string;
  variant?: "card" | "row";
  className?: string;
}) {
  return (
    <div role="status" aria-label={label} className={cn("grid gap-3", className)}>
      {Array.from({ length: rows }, (_, index) =>
        variant === "card" ? (
          <div key={index} className="grid gap-3 rounded-2xl border border-border bg-card px-6 py-5">
            <Skeleton className="h-5 w-24 rounded-full" />
            <Skeleton className="h-4" style={{ width: `${82 - index * 9}%` }} />
            <Skeleton className="h-3 w-32" />
          </div>
        ) : (
          <div key={index} className="grid gap-2 border-b border-border py-4 last:border-b-0">
            <Skeleton className="h-4" style={{ width: `${64 - index * 7}%` }} />
            <Skeleton className="h-3 w-40" />
          </div>
        ),
      )}
      <span className="sr-only">{label}</span>
    </div>
  );
}
