import * as React from "react";
import { Loader2 } from "lucide-react";

import { cn } from "@/lib/utils";


function Marker({ className, variant = "default", status, label, children, ...props }) {
  const normalized = String(status || "").toLowerCase();
  return (
    <div
      data-slot="marker"
      data-variant={variant}
      role={status ? "status" : undefined}
      className={cn(
        "group/marker relative flex min-h-4 w-full items-center gap-2 text-left text-sm text-muted-foreground",
        variant === "separator" && "before:mr-1 before:h-px before:min-w-0 before:flex-1 before:bg-border after:ml-1 after:h-px after:min-w-0 after:flex-1 after:bg-border",
        variant === "border" && "border-b pb-2",
        status && "w-fit font-mono text-[0.6875rem]",
        (normalized === "ready" || normalized === "answered") && "text-success",
        (normalized === "error" || normalized === "blocked") && "text-destructive",
        normalized === "needs_clarification" && "text-warning",
        className
      )}
      {...props}
    >
      {normalized === "loading" && <Loader2 data-slot="marker-icon" aria-hidden="true" className="size-3.5 shrink-0 animate-spin text-brand" />}
      <span data-slot="marker-content" className="min-w-0 break-words">{children || label || status}</span>
    </div>
  );
}

export { Marker };
