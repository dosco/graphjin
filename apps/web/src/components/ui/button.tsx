import { Slot } from "radix-ui";
import {
  forwardRef,
  type ComponentPropsWithoutRef,
  type ReactNode,
} from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/cn";

export const buttonVariants = cva(
  [
    "ui-button inline-flex items-center justify-center whitespace-nowrap rounded-control border-[1.5px] font-body font-semibold cursor-pointer",
    "transition-[color,background-color,border-color,transform,box-shadow,opacity] duration-150",
    "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg",
    "disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:translate-y-0",
    "aria-disabled:pointer-events-none aria-disabled:opacity-50",
    "[&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0",
  ],
  {
    variants: {
      variant: {
        primary:
          "border-text bg-text text-bg hover:border-accent hover:bg-accent hover:text-white",
        secondary:
          "border-border bg-card text-text2 hover:border-accent hover:bg-accent-soft hover:text-accent",
        ghost:
          "border-transparent bg-transparent text-text2 hover:bg-neutral-soft hover:text-text",
        danger:
          "border-danger/30 bg-danger-soft text-danger hover:border-danger hover:bg-danger hover:text-white",
      },
      size: {
        sm: "min-h-8 gap-1.5 px-3 py-1.5 text-ui-body-sm leading-none",
        md: "min-h-10 gap-2 px-4 py-2 text-ui-body leading-none",
        lg: "min-h-11 gap-2 px-5 py-2.5 text-ui-body-lg leading-none",
        "icon-sm": "size-8 shrink-0 p-0 [&_svg]:size-3.5",
        icon: "size-10 shrink-0 p-0 [&_svg]:size-4",
      },
    },
    defaultVariants: { variant: "secondary", size: "md" },
  },
);

export type ButtonVariant = NonNullable<
  VariantProps<typeof buttonVariants>["variant"]
>;
export type ButtonSize = NonNullable<
  VariantProps<typeof buttonVariants>["size"]
>;

export function buttonClassName({
  variant = "secondary",
  size = "md",
  className,
}: VariantProps<typeof buttonVariants> & { className?: string } = {}) {
  return cn(buttonVariants({ variant, size }), className);
}

export type ButtonProps = ComponentPropsWithoutRef<"button"> &
  VariantProps<typeof buttonVariants> & { asChild?: boolean };

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  function Button(
    {
      variant = "secondary",
      size = "md",
      asChild = false,
      type = "button",
      className,
      ...props
    },
    ref,
  ) {
    const Component = asChild ? Slot.Root : "button";
    return (
      <Component
        ref={ref}
        type={asChild ? undefined : type}
        data-slot="button"
        data-ui-button=""
        data-variant={variant}
        data-size={size}
        className={buttonClassName({ variant, size, className })}
        {...props}
      />
    );
  },
);

export type ButtonLinkProps = ComponentPropsWithoutRef<"a"> &
  VariantProps<typeof buttonVariants>;

export const ButtonLink = forwardRef<HTMLAnchorElement, ButtonLinkProps>(
  function ButtonLink(
    { variant = "secondary", size = "md", className, ...props },
    ref,
  ) {
    return (
      <a
        ref={ref}
        data-slot="button"
        data-ui-button=""
        data-variant={variant}
        data-size={size}
        className={buttonClassName({ variant, size, className })}
        {...props}
      />
    );
  },
);

type IconButtonProps = Omit<ButtonProps, "aria-label" | "children" | "size"> & {
  label: string;
  size?: Extract<ButtonSize, "icon-sm" | "icon">;
  children: ReactNode;
};

export function IconButton({
  label,
  size = "icon",
  title,
  children,
  ...props
}: IconButtonProps) {
  return (
    <Button aria-label={label} title={title ?? label} size={size} {...props}>
      {children}
    </Button>
  );
}
