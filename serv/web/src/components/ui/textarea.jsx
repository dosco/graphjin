import * as React from "react";

import { cn } from "@/lib/utils";

const Textarea = React.forwardRef(({ className, ...props }, ref) => (
  <textarea
    ref={ref}
    className={cn(
      "flex min-h-20 w-full min-w-0 rounded-xl border border-transparent bg-accent px-3 py-2 text-sm transition-colors outline-none placeholder:text-muted-foreground focus-visible:border-brand/60 focus-visible:bg-transparent focus-visible:ring-brand/15 focus-visible:ring-[3px] disabled:cursor-not-allowed disabled:opacity-50",
      className
    )}
    {...props}
  />
));
Textarea.displayName = "Textarea";

export { Textarea };
