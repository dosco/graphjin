"use client";

import { Check } from "lucide-react";
import { Checkbox as CheckboxPrimitive } from "radix-ui";
import {
  forwardRef,
  useId,
  type ComponentPropsWithoutRef,
  type ReactNode,
} from "react";
import { cn } from "@/lib/cn";

export const CheckboxControl = forwardRef<
  HTMLButtonElement,
  ComponentPropsWithoutRef<typeof CheckboxPrimitive.Root>
>(function CheckboxControl({ className, ...props }, ref) {
  return (
    <CheckboxPrimitive.Root
      ref={ref}
      data-slot="checkbox"
      data-ui-checkbox-control=""
      className={cn(
        "mt-0.5 grid size-4 shrink-0 place-items-center rounded-[4px] border-[1.5px] border-text3 bg-card text-white outline-none hover:border-accent",
        "data-[state=checked]:border-accent data-[state=checked]:bg-accent",
        "focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-card",
        "disabled:cursor-not-allowed",
        className,
      )}
      {...props}
    >
      <CheckboxPrimitive.Indicator data-slot="checkbox-indicator">
        <Check aria-hidden="true" className="size-3" strokeWidth={3} />
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  );
});

export type CheckboxProps = Omit<
  ComponentPropsWithoutRef<typeof CheckboxPrimitive.Root>,
  "className"
> & {
  label: ReactNode;
  className?: string;
  inputClassName?: string;
  labelClassName?: string;
};

export const Checkbox = forwardRef<HTMLButtonElement, CheckboxProps>(
  function Checkbox(
    {
      label,
      className,
      inputClassName,
      labelClassName,
      disabled,
      id,
      ...props
    },
    ref,
  ) {
    const generatedId = useId();
    const controlId = id ?? generatedId;
    return (
      <span
        data-slot="checkbox-field"
        data-ui-checkbox=""
        className={cn(
          "inline-flex min-w-0 items-start gap-2 font-body text-ui-body-sm leading-[var(--leading-compact)] text-text2",
          disabled ? "opacity-60" : null,
          className,
        )}
      >
        <CheckboxControl
          ref={ref}
          id={controlId}
          disabled={disabled}
          className={inputClassName}
          {...props}
        />
        <label
          htmlFor={controlId}
          className={cn(
            "min-w-0",
            disabled ? "cursor-not-allowed" : "cursor-pointer",
            labelClassName,
          )}
        >
          {label}
        </label>
      </span>
    );
  },
);
