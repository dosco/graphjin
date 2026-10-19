import * as React from "react";

import { cn } from "@/lib/utils";

const Attachment = React.forwardRef(({ className, title, description, children, ...props }, ref) => (
  <div ref={ref} className={cn("rounded-lg border bg-card p-3 text-sm shadow-[0_1px_2px_rgba(28,35,48,0.05)]", className)} {...props}>
    {(title || description) && (
      <div className="mb-2 grid gap-1">
        {title && <span className="text-xs font-semibold uppercase text-muted-foreground">{title}</span>}
        {description && <span className="text-xs text-muted-foreground">{description}</span>}
      </div>
    )}
    {children}
  </div>
));
Attachment.displayName = "Attachment";

export { Attachment };
