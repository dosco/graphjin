import * as React from "react";
import * as TabsPrimitive from "@radix-ui/react-tabs";

import { cn } from "@/lib/utils";

const Tabs = TabsPrimitive.Root;

const TabsList = React.forwardRef(({ className, ...props }, ref) => (
  <TabsPrimitive.List
    ref={ref}
    className={cn("inline-flex h-11 items-stretch justify-start gap-1 border-b text-muted-foreground", className)}
    {...props}
  />
));
TabsList.displayName = TabsPrimitive.List.displayName;

const TabsTrigger = React.forwardRef(({ className, ...props }, ref) => (
  <TabsPrimitive.Trigger
    ref={ref}
    className={cn(
      "relative inline-flex items-center justify-center gap-1.5 whitespace-nowrap px-3 text-sm font-medium transition-colors outline-none hover:bg-accent/60 hover:text-foreground focus-visible:ring-ring/40 focus-visible:ring-[3px] disabled:pointer-events-none disabled:opacity-50 data-[state=active]:font-bold data-[state=active]:text-foreground after:absolute after:inset-x-3 after:-bottom-px after:h-0.5 after:bg-transparent after:transition-colors data-[state=active]:after:bg-brand data-[state=active]:after:shadow-[0_0_12px_var(--glow)]",
      className
    )}
    {...props}
  />
));
TabsTrigger.displayName = TabsPrimitive.Trigger.displayName;

const TabsContent = React.forwardRef(({ className, ...props }, ref) => (
  <TabsPrimitive.Content ref={ref} className={cn("mt-2 outline-none", className)} {...props} />
));
TabsContent.displayName = TabsPrimitive.Content.displayName;

export { Tabs, TabsList, TabsTrigger, TabsContent };
