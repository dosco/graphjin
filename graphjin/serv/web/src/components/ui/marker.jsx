import * as React from "react";
import { CircleCheck, CircleDashed, CircleHelp, CircleX, Loader2 } from "lucide-react";

import { cn } from "@/lib/utils";

const icons = {
  answered: CircleCheck,
  ready: CircleCheck,
  blocked: CircleX,
  error: CircleX,
  loading: Loader2,
  needs_clarification: CircleHelp,
  pending: CircleDashed,
};

const variants = {
  answered: "border-emerald-200 bg-emerald-50 text-emerald-700",
  ready: "border-emerald-200 bg-emerald-50 text-emerald-700",
  blocked: "border-red-200 bg-red-50 text-red-700",
  error: "border-red-200 bg-red-50 text-red-700",
  loading: "border-sky-200 bg-sky-50 text-sky-700",
  needs_clarification: "border-amber-200 bg-amber-50 text-amber-700",
  pending: "border-border bg-muted text-muted-foreground",
};

function Marker({ status = "pending", label, className }) {
  const normalized = String(status || "pending").toLowerCase();
  const Icon = icons[normalized] || CircleDashed;
  return (
    <span className={cn("inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs font-medium", variants[normalized] || variants.pending, className)}>
      <Icon className={cn("size-3.5", normalized === "loading" && "animate-spin")} aria-hidden="true" />
      {label || status}
    </span>
  );
}

export { Marker };
