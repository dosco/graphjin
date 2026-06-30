import * as React from "react";

import { cn } from "@/lib/utils";

const Bubble = React.forwardRef(({ className, role = "assistant", ...props }, ref) => (
  <div
    ref={ref}
    data-role={role}
    className={cn(
      "rounded-lg border px-4 py-3 text-sm leading-6 shadow-xs data-[role=assistant]:bg-card data-[role=assistant]:text-card-foreground data-[role=system]:bg-muted data-[role=user]:border-primary data-[role=user]:bg-primary data-[role=user]:text-primary-foreground",
      className
    )}
    {...props}
  />
));
Bubble.displayName = "Bubble";

export { Bubble };
