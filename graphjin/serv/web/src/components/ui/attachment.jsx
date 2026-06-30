import * as React from "react";

import { cn } from "@/lib/utils";

const Attachment = React.forwardRef(({ className, title, description, children, ...props }, ref) => (
  <div ref={ref} className={cn("rounded-lg border bg-muted/40 p-3 text-sm", className)} {...props}>
    {(title || description) && (
      <div className="mb-2 grid gap-1">
        {title && <span className="font-medium text-foreground">{title}</span>}
        {description && <span className="text-xs text-muted-foreground">{description}</span>}
      </div>
    )}
    {children}
  </div>
));
Attachment.displayName = "Attachment";

export { Attachment };
