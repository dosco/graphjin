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
  answered: "border-emerald-300 bg-card text-emerald-700",
  ready: "border-emerald-300 bg-card text-emerald-700",
  blocked: "border-red-300 bg-card text-red-700",
  error: "border-red-300 bg-card text-red-700",
  loading: "border-border bg-card text-muted-foreground",
  needs_clarification: "border-amber-300 bg-card text-amber-700",
  pending: "border-border bg-card text-muted-foreground",
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
