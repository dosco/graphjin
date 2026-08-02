import { GripVertical } from "lucide-react";
import * as ResizablePrimitive from "react-resizable-panels";

import { cn } from "@/lib/utils";

function ResizablePanelGroup({ className, ...props }) {
  return <ResizablePrimitive.Group className={cn("flex h-full w-full aria-[orientation=vertical]:flex-col", className)} {...props} />;
}

function ResizablePanel(props) {
  return <ResizablePrimitive.Panel {...props} />;
}

function ResizableHandle({ withHandle, className, ...props }) {
  return (
    <ResizablePrimitive.Separator
      className={cn("relative flex w-px items-center justify-center bg-border outline-none after:absolute after:inset-y-0 after:left-1/2 after:w-2 after:-translate-x-1/2 focus-visible:ring-2 focus-visible:ring-ring", className)}
      {...props}
    >
      {withHandle && <span className="z-10 flex h-7 w-4 items-center justify-center rounded-md border bg-background"><GripVertical className="size-3 text-muted-foreground" /></span>}
    </ResizablePrimitive.Separator>
  );
}

export { ResizablePanelGroup, ResizablePanel, ResizableHandle };
