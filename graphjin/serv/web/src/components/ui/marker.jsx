import * as React from "react";
import { CircleCheck, CircleDashed, CircleHelp, CircleX, Loader2 } from "lucide-react";

import { cn } from "@/lib/utils";

const statusIcons = {
  answered: CircleCheck,
  ready: CircleCheck,
  blocked: CircleX,
  error: CircleX,
  loading: Loader2,
  needs_clarification: CircleHelp,
  pending: CircleDashed,
};

function Marker({ className, variant = "default", status, label, children, ...props }) {
  const normalized = String(status || "").toLowerCase();
  const Icon = statusIcons[normalized];
  return (
    <div
      data-slot="marker"
      data-variant={variant}
      role={status ? "status" : undefined}
      className={cn(
        "group/marker relative flex min-h-4 w-full items-center gap-2 text-left text-sm text-muted-foreground",
        variant === "separator" && "before:mr-1 before:h-px before:min-w-0 before:flex-1 before:bg-border after:ml-1 after:h-px after:min-w-0 after:flex-1 after:bg-border",
        variant === "border" && "border-b pb-2",
        status && "w-fit text-xs",
        (normalized === "ready" || normalized === "answered") && "text-emerald-700 dark:text-emerald-400",
        (normalized === "error" || normalized === "blocked") && "text-destructive",
        normalized === "needs_clarification" && "text-amber-700 dark:text-amber-400",
        className
      )}
      {...props}
    >
      {Icon && <span data-slot="marker-icon" aria-hidden="true" className="size-4 shrink-0"><Icon className={cn("size-4", normalized === "loading" && "animate-spin")} /></span>}
      <span data-slot="marker-content" className="min-w-0 break-words">{children || label || status}</span>
    </div>
  );
}

export { Marker };
