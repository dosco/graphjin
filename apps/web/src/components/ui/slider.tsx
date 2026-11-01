"use client";

import { Slider as SliderPrimitive } from "radix-ui";
import { forwardRef, type ComponentPropsWithoutRef } from "react";
import { cn } from "@/lib/cn";

export const Slider = forwardRef<
  HTMLSpanElement,
  ComponentPropsWithoutRef<typeof SliderPrimitive.Root> & { thumbLabel?: string }
>(function Slider({ className, thumbLabel, ...props }, ref) {
  return (
    <SliderPrimitive.Root
      ref={ref}
      data-slot="slider"
      data-ui-slider=""
      className={cn(
        "relative flex h-11 w-full touch-none select-none items-center data-[disabled]:opacity-50 md:h-6",
        className,
      )}
      {...props}
    >
      <SliderPrimitive.Track className="relative h-1 w-full grow overflow-hidden rounded-full bg-border">
        <SliderPrimitive.Range className="absolute h-full bg-accent" />
      </SliderPrimitive.Track>
      <SliderPrimitive.Thumb
        aria-label={thumbLabel}
        className={cn(
          "block size-[18px] rounded-full border-2 border-accent bg-card shadow-sm outline-none transition-transform duration-150",
          "hover:scale-110 focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-card",
          "motion-reduce:transition-none",
        )}
      />
    </SliderPrimitive.Root>
  );
});
