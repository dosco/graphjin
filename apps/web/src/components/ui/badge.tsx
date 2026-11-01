import { Slot } from "radix-ui";
import type { ComponentPropsWithoutRef } from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/cn";

export const badgeVariants = cva(
  "inline-flex min-h-6 min-w-0 max-w-full shrink-0 items-center overflow-hidden text-ellipsis whitespace-nowrap rounded-full border px-2 py-1 font-body text-ui-caption font-semibold leading-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent",
  {
    variants: {
      variant: {
        live: "border-success/30 bg-success/15 text-success-ink",
        watch: "border-watch/30 bg-watch-soft text-warn-ink",
        danger: "border-danger/15 bg-danger-soft text-danger",
        muted: "border-border bg-neutral text-text2",
        success: "border-success-mid/15 bg-success-soft text-success-ink",
        default: "border-border bg-neutral text-text2",
        secondary: "border-border bg-card text-text2",
        destructive: "border-danger/15 bg-danger-soft text-danger",
        outline: "border-border bg-transparent text-text2",
      },
    },
    defaultVariants: { variant: "muted" },
  },
);

export type BadgeVariant = NonNullable<
  VariantProps<typeof badgeVariants>["variant"]
>;

type BadgeProps = ComponentPropsWithoutRef<"span"> &
  VariantProps<typeof badgeVariants> & { asChild?: boolean };

export function Badge({
  variant = "muted",
  asChild = false,
  className,
  children,
  title,
  ...props
}: BadgeProps) {
  const Component = asChild ? Slot.Root : "span";
  return (
    <Component
      data-slot="badge"
      data-variant={variant}
      className={cn(badgeVariants({ variant }), className)}
      title={title ?? (typeof children === "string" ? children : undefined)}
      {...props}
    >
      {children}
    </Component>
  );
}
