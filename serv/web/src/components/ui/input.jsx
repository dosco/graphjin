import * as React from "react";

import { cn } from "@/lib/utils";

const Input = React.forwardRef(({ className, type, ...props }, ref) => (
  <input
    type={type}
    ref={ref}
    className={cn(
      "flex h-9 w-full min-w-0 rounded-xl border border-transparent bg-accent px-3 py-1 text-sm transition-colors outline-none placeholder:text-muted-foreground focus-visible:border-brand/60 focus-visible:bg-transparent focus-visible:ring-brand/15 focus-visible:ring-[3px] disabled:cursor-not-allowed disabled:opacity-50",
      className
    )}
    {...props}
  />
));
Input.displayName = "Input";

export { Input };
