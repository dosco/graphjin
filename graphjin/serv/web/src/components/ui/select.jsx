import * as React from "react";
import { ChevronDown } from "lucide-react";

import { cn } from "@/lib/utils";

const Select = React.forwardRef(({ className, children, ...props }, ref) => (
  <div className={cn("relative", className)}>
    <select
      ref={ref}
      className="flex h-9 w-full appearance-none rounded-lg border border-input bg-card py-1 pl-3 pr-9 text-sm font-medium shadow-[0_1px_2px_rgba(28,35,48,0.04)] outline-none transition-colors focus-visible:border-ring focus-visible:ring-ring/20 focus-visible:ring-[3px] disabled:cursor-not-allowed disabled:opacity-50"
      {...props}
    >
      {children}
    </select>
    <ChevronDown className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
  </div>
));
Select.displayName = "Select";

export { Select };
