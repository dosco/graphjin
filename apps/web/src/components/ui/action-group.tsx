import type { ComponentPropsWithoutRef } from "react";
import { cn } from "@/lib/cn";

type ActionGroupProps = {
  align?: "start" | "end";
  mobile?: "wrap" | "stack";
} & ComponentPropsWithoutRef<"div">;

export function ActionGroup({
  align = "end",
  mobile = "wrap",
  className,
  ...props
}: ActionGroupProps) {
  return (
    <div
      data-ui-action-group=""
      data-mobile={mobile}
      className={cn(
        "flex min-w-0 flex-wrap items-center gap-2",
        align === "end" ? "justify-end" : "justify-start",
        className,
      )}
      {...props}
    />
  );
}
