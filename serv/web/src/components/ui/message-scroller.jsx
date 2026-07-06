import * as React from "react";

import { cn } from "@/lib/utils";
import { ScrollArea } from "./scroll-area";

const MessageScrollerContext = React.createContext(null);

function MessageScrollerProvider({ children }) {
  const viewportRef = React.useRef(null);
  const scrollToBottom = React.useCallback(() => {
    const node = viewportRef.current?.querySelector("[data-radix-scroll-area-viewport]");
    if (node) {
      node.scrollTop = node.scrollHeight;
    }
  }, []);
  const value = React.useMemo(() => ({ viewportRef, scrollToBottom }), [scrollToBottom]);
  return <MessageScrollerContext.Provider value={value}>{children}</MessageScrollerContext.Provider>;
}

function useMessageScroller() {
  return React.useContext(MessageScrollerContext);
}

const MessageScroller = React.forwardRef(({ className, children, autoScroll = true, ...props }, ref) => {
  const context = useMessageScroller();
  React.useEffect(() => {
    if (autoScroll) {
      context?.scrollToBottom();
    }
  }, [autoScroll, children, context]);

  return (
    <ScrollArea ref={context?.viewportRef || ref} className={cn("h-full", className)} {...props}>
      <div className="grid gap-4 p-4 md:p-5">{children}</div>
    </ScrollArea>
  );
});
MessageScroller.displayName = "MessageScroller";

export { MessageScrollerProvider, MessageScroller, useMessageScroller };
