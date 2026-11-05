import * as React from "react";
import { ChevronDown } from "lucide-react";

import { cn } from "@/lib/utils";

const Select = React.forwardRef(({ className, children, ...props }, ref) => (
  <div className={cn("relative", className)}>
    <select
      ref={ref}
      className="flex h-9 w-full appearance-none rounded-xl border border-transparent bg-accent [&>option]:bg-popover py-1 pl-3 pr-9 text-sm font-medium outline-none transition-colors focus-visible:border-brand focus-visible:ring-brand/20 focus-visible:ring-[3px] disabled:cursor-not-allowed disabled:opacity-50"
      {...props}
    >
      {children}
    </select>
    <ChevronDown className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
  </div>
));
Select.displayName = "Select";

export { Select };
