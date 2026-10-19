import * as React from "react";
import { cva } from "class-variance-authority";

import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex w-fit shrink-0 items-center justify-center gap-1 overflow-hidden rounded-md border px-2 py-0.5 text-xs font-medium whitespace-nowrap transition-colors shadow-[inset_0_1px_0_rgba(255,255,255,0.55)] [&_svg]:size-3",
  {
    variants: {
      variant: {
        default: "border-transparent bg-primary text-primary-foreground",
        secondary: "border-border bg-card text-secondary-foreground",
        destructive: "border-transparent bg-destructive text-white",
        outline: "border-border bg-card text-foreground",
        success: "border-emerald-300 bg-card text-emerald-700",
        warning: "border-amber-300 bg-card text-amber-800",
        muted: "border-border bg-card text-muted-foreground",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
);

function Badge({ className, variant, ...props }) {
  return <span className={cn(badgeVariants({ variant }), className)} {...props} />;
}

export { Badge, badgeVariants };
