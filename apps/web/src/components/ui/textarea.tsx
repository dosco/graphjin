import { forwardRef, type ComponentPropsWithoutRef } from "react";
import { controlClassName } from "@/components/ui/input";
import { cn } from "@/lib/cn";

export const Textarea = forwardRef<
  HTMLTextAreaElement,
  ComponentPropsWithoutRef<"textarea">
>(function Textarea({ className, ...props }, ref) {
  return (
    <textarea
      ref={ref}
      data-slot="textarea"
      data-ui-field-control=""
      className={cn(controlClassName, "min-h-24 resize-y", className)}
      {...props}
    />
  );
});
