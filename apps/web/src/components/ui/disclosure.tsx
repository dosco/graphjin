import { ChevronDown } from "lucide-react";
import type { ComponentPropsWithoutRef, ReactNode } from "react";
import { cn } from "@/lib/cn";

type DisclosureProps = Omit<ComponentPropsWithoutRef<"details">, "title"> & {
  title: ReactNode;
  meta?: ReactNode;
};

export function Disclosure({
  title,
  meta,
  className,
  children,
  ...props
}: DisclosureProps) {
  return (
    <details
      className={cn(
        "group overflow-hidden rounded-inner border border-border bg-card",
        className,
      )}
      {...props}
    >
      <summary
        data-ui-disclosure=""
        className="flex min-h-10 cursor-pointer list-none items-center gap-3 px-3.5 py-2.5 font-body text-ui-body-sm font-bold text-text marker:hidden focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent [&::-webkit-details-marker]:hidden"
      >
        <span className="min-w-0 flex-1">{title}</span>
        {meta ? <span className="text-ui-caption font-semibold text-text3">{meta}</span> : null}
        <ChevronDown
          aria-hidden="true"
          className="size-4 shrink-0 text-text3 transition-transform group-open:rotate-180"
        />
      </summary>
      <div className="border-t border-border px-3.5 py-3">{children}</div>
    </details>
  );
}
