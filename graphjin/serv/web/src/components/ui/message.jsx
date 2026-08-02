import * as React from "react";

import { cn } from "@/lib/utils";

function MessageGroup({ className, ...props }) {
  return <div data-slot="message-group" className={cn("flex min-w-0 flex-col gap-2", className)} {...props} />;
}

function Message({ className, align, role = "assistant", ...props }) {
  const resolvedAlign = align || (role === "user" ? "end" : "start");
  return (
    <article
      data-slot="message"
      data-align={resolvedAlign}
      data-role={role}
      className={cn("group/message relative flex w-full min-w-0 gap-3 text-sm data-[align=end]:flex-row-reverse", className)}
      {...props}
    />
  );
}

function MessageAvatar({ className, ...props }) {
  return <div data-slot="message-avatar" className={cn("flex size-8 shrink-0 items-center justify-center self-end overflow-hidden rounded-full bg-muted text-xs font-semibold text-muted-foreground", className)} {...props} />;
}

function MessageContent({ className, ...props }) {
  return <div data-slot="message-content" className={cn("flex w-full min-w-0 max-w-[52rem] flex-col gap-2.5 break-words group-data-[align=end]/message:items-end", className)} {...props} />;
}

function MessageHeader({ className, ...props }) {
  return <div data-slot="message-header" className={cn("flex max-w-full min-w-0 items-center gap-2 text-xs font-medium text-muted-foreground", className)} {...props} />;
}

function MessageFooter({ className, ...props }) {
  return <div data-slot="message-footer" className={cn("flex max-w-full min-w-0 items-center gap-2 text-xs text-muted-foreground group-data-[align=end]/message:justify-end", className)} {...props} />;
}

export { MessageGroup, Message, MessageAvatar, MessageContent, MessageHeader, MessageFooter };
