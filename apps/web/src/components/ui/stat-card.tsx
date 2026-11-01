import type { ReactNode } from "react";
import { Card } from "@/components/ui/card";
import { cn } from "@/lib/cn";

export type StatTone = "neutral" | "ok" | "warn";

export function StatGrid({
  label,
  className,
  children,
}: {
  label: string;
  className?: string;
  children: ReactNode;
}) {
  return (
    <section
      aria-label={label}
      data-slot="stat-grid"
      className={cn("grid gap-3 sm:grid-cols-2 lg:grid-cols-4", className)}
    >
      {children}
    </section>
  );
}

export function StatCard({
  label,
  value,
  detail,
  tone = "neutral",
}: {
  label: string;
  value: ReactNode;
  detail?: ReactNode;
  tone?: StatTone;
}) {
  return (
    <Card data-slot="stat-card" className="p-4">
      <div className="text-ui-caption font-semibold text-text2">{label}</div>
      <div
        className={cn(
          "mt-2 font-display text-2xl font-bold leading-tight tabular-nums",
          tone === "ok" ? "text-success-ink" : tone === "warn" ? "text-danger" : "text-text",
        )}
      >
        {value}
      </div>
      {detail ? (
        <div className="mt-1 text-ui-caption text-text2">{detail}</div>
      ) : null}
    </Card>
  );
}
