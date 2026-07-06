import * as React from "react";

import { cn } from "@/lib/utils";

const Message = React.forwardRef(({ className, role = "assistant", ...props }, ref) => (
  <article
    ref={ref}
    data-role={role}
    className={cn(
      "group flex w-full gap-3 data-[role=user]:justify-end data-[role=assistant]:justify-start data-[role=system]:justify-start",
      className
    )}
    {...props}
  />
));
Message.displayName = "Message";

const MessageAvatar = React.forwardRef(({ className, ...props }, ref) => (
  <div
    ref={ref}
    className={cn("mt-1 flex size-8 shrink-0 items-center justify-center rounded-lg border bg-card text-xs font-semibold text-muted-foreground shadow-[0_1px_2px_rgba(28,35,48,0.05)]", className)}
    {...props}
  />
));
MessageAvatar.displayName = "MessageAvatar";

const MessageContent = React.forwardRef(({ className, ...props }, ref) => (
  <div ref={ref} className={cn("grid max-w-[min(820px,86%)] gap-2", className)} {...props} />
));
MessageContent.displayName = "MessageContent";

export { Message, MessageAvatar, MessageContent };
