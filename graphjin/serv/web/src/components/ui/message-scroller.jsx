import * as React from "react";
import {
  MessageScroller as MessageScrollerPrimitive,
  useMessageScroller,
  useMessageScrollerScrollable,
  useMessageScrollerVisibility,
} from "@shadcn/react/message-scroller";
import { ArrowDown } from "lucide-react";

import { cn } from "@/lib/utils";
import { Button } from "./button";

function MessageScrollerProvider(props) {
  return <MessageScrollerPrimitive.Provider {...props} />;
}

function MessageScroller({ className, ...props }) {
  return (
    <MessageScrollerPrimitive.Root
      data-slot="message-scroller"
      className={cn("group/message-scroller relative flex size-full min-h-0 flex-col overflow-hidden", className)}
      {...props}
    />
  );
}

function MessageScrollerViewport({ className, ...props }) {
  return (
    <MessageScrollerPrimitive.Viewport
      data-slot="message-scroller-viewport"
      className={cn("scroll-fade-b scrollbar-thin scrollbar-gutter-stable size-full min-h-0 min-w-0 overflow-y-auto overscroll-contain contain-content data-autoscrolling:scrollbar-none", className)}
      {...props}
    />
  );
}

function MessageScrollerContent({ className, ...props }) {
  return <MessageScrollerPrimitive.Content data-slot="message-scroller-content" className={cn("flex h-max min-h-full flex-col gap-7", className)} {...props} />;
}

function MessageScrollerItem({ className, scrollAnchor = false, ...props }) {
  return (
    <MessageScrollerPrimitive.Item
      data-slot="message-scroller-item"
      scrollAnchor={scrollAnchor}
      className={cn("min-w-0 shrink-0 [contain-intrinsic-size:auto_10rem] [content-visibility:auto]", className)}
      {...props}
    />
  );
}

function MessageScrollerButton({ direction = "end", className, children, ...props }) {
  return (
    <MessageScrollerPrimitive.Button
      data-slot="message-scroller-button"
      direction={direction}
      render={<Button variant="secondary" size="icon-sm" />}
      className={cn(
        "absolute inset-s-1/2 z-20 -translate-x-1/2 border bg-background text-foreground shadow-lg transition-[translate,scale,opacity] data-[active=false]:pointer-events-none data-[active=false]:scale-95 data-[active=false]:opacity-0 data-[active=true]:scale-100 data-[active=true]:opacity-100 data-[direction=end]:bottom-4 data-[direction=start]:top-4 rtl:translate-x-1/2",
        className
      )}
      {...props}
    >
      {children ?? <><ArrowDown /><span className="sr-only">{direction === "end" ? "Scroll to latest message" : "Scroll to first message"}</span></>}
    </MessageScrollerPrimitive.Button>
  );
}

export {
  MessageScrollerProvider,
  MessageScroller,
  MessageScrollerViewport,
  MessageScrollerContent,
  MessageScrollerItem,
  MessageScrollerButton,
  useMessageScroller,
  useMessageScrollerScrollable,
  useMessageScrollerVisibility,
};
