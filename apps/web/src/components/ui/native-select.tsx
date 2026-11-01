import { ChevronDown } from "lucide-react";
import { forwardRef, type ComponentPropsWithoutRef } from "react";
import { controlClassName } from "@/components/ui/input";
import { cn } from "@/lib/cn";

export const NativeSelect = forwardRef<
  HTMLSelectElement,
  ComponentPropsWithoutRef<"select">
>(function NativeSelect({ className, ...props }, ref) {
  return (
    <span className="relative block w-full">
      <select
        ref={ref}
        data-slot="native-select"
        data-ui-field-control=""
        className={cn(
          controlClassName,
          "cursor-pointer appearance-none pr-10",
          className,
        )}
        {...props}
      />
      <ChevronDown
        aria-hidden="true"
        className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 text-text3"
      />
    </span>
  );
});
