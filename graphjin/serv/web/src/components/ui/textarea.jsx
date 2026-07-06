import * as React from "react";

import { cn } from "@/lib/utils";

const Textarea = React.forwardRef(({ className, ...props }, ref) => (
  <textarea
    ref={ref}
    className={cn(
      "flex min-h-20 w-full rounded-lg border border-input bg-card px-3 py-2 text-sm shadow-[0_1px_2px_rgba(28,35,48,0.04)] transition-colors outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-ring/20 focus-visible:ring-[3px] disabled:cursor-not-allowed disabled:opacity-50",
      className
    )}
    {...props}
  />
));
Textarea.displayName = "Textarea";

export { Textarea };
