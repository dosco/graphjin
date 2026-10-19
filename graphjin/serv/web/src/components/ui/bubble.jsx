import * as React from "react";
import { cva } from "class-variance-authority";

import { cn } from "@/lib/utils";

const bubbleVariants = cva("group/bubble relative flex w-fit max-w-[82%] min-w-0 flex-col gap-1 data-[variant=ghost]:max-w-full", {
  variants: {
    variant: {
      default: "*:data-[slot=bubble-content]:bg-primary *:data-[slot=bubble-content]:text-primary-foreground",
      secondary: "*:data-[slot=bubble-content]:bg-secondary *:data-[slot=bubble-content]:text-secondary-foreground",
      muted: "*:data-[slot=bubble-content]:bg-muted",
      outline: "*:data-[slot=bubble-content]:border-border *:data-[slot=bubble-content]:bg-background",
      ghost: "*:data-[slot=bubble-content]:rounded-none *:data-[slot=bubble-content]:bg-transparent *:data-[slot=bubble-content]:p-0",
      destructive: "*:data-[slot=bubble-content]:bg-destructive/10 *:data-[slot=bubble-content]:text-destructive",
    },
  },
  defaultVariants: { variant: "default" },
});

function Bubble({ variant = "default", align = "start", className, ...props }) {
  return <div data-slot="bubble" data-variant={variant} data-align={align} className={cn(bubbleVariants({ variant }), align === "end" && "self-end", className)} {...props} />;
}

function BubbleContent({ className, ...props }) {
  return <div data-slot="bubble-content" className={cn("w-fit max-w-full min-w-0 overflow-hidden rounded-xl border border-transparent px-3.5 py-2.5 text-sm leading-relaxed break-words", className)} {...props} />;
}

export { Bubble, BubbleContent, bubbleVariants };
