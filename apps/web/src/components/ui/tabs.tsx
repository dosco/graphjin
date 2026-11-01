"use client";

import {
  Children,
  cloneElement,
  isValidElement,
  type AriaRole,
  type ComponentPropsWithoutRef,
  type CSSProperties,
  type ReactElement,
  type ReactNode,
} from "react";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { cn } from "@/lib/cn";

type ItemProps = Omit<
  ComponentPropsWithoutRef<typeof ToggleGroupItem>,
  "value"
> & {
  selected?: boolean;
  value?: string;
};

type GroupProps = {
  children: ReactNode;
  className?: string;
  id?: string;
  role?: AriaRole;
  style?: CSSProperties;
  "aria-label"?: string;
};

function SelectionGroup({ children, className, ...props }: GroupProps) {
  const items = Children.toArray(children).filter(
    isValidElement,
  ) as ReactElement<ItemProps>[];
  const selectedIndex = items.findIndex((item) => item.props.selected);
  return (
    <ToggleGroup
      type="single"
      value={selectedIndex >= 0 ? String(selectedIndex) : ""}
      spacing={0}
      className={className}
      {...props}
    >
      {items.map((item, index) =>
        cloneElement(item, { value: item.props.value ?? String(index) }),
      )}
    </ToggleGroup>
  );
}

export function Tabs({ className, ...props }: GroupProps) {
  return (
    <SelectionGroup
      className={cn(
        "inline-flex max-w-full items-center gap-1 overflow-x-auto rounded-control border border-border bg-neutral-soft p-1",
        className,
      )}
      {...props}
    />
  );
}

export function Tab({
  selected = false,
  value = "",
  className,
  ...props
}: ItemProps) {
  return (
    <ToggleGroupItem
      data-ui-tab=""
      data-selected={selected || undefined}
      value={value}
      aria-label={
        typeof props.children === "string" ? props.children : undefined
      }
      className={cn(
        "inline-flex min-h-[30px] shrink-0 items-center justify-center rounded-[8px] border border-transparent bg-transparent px-3 py-1.5 font-body text-ui-body-sm font-semibold text-text2 hover:bg-transparent hover:text-text focus-visible:ring-2 focus-visible:ring-accent data-[state=on]:border-border data-[state=on]:bg-card data-[state=on]:text-text data-[state=on]:shadow-soft",
        className,
      )}
      {...props}
    />
  );
}

export function SegmentedControl({ className, ...props }: GroupProps) {
  return (
    <SelectionGroup
      className={cn(
        "inline-flex max-w-full items-center gap-0.5 overflow-x-auto rounded-full border border-border bg-neutral p-1",
        className,
      )}
      {...props}
    />
  );
}

export function Segment({
  selected = false,
  value = "",
  className,
  ...props
}: ItemProps) {
  return (
    <ToggleGroupItem
      data-ui-segment=""
      data-selected={selected || undefined}
      value={value}
      className={cn(
        "inline-flex min-h-[30px] shrink-0 items-center justify-center rounded-full border-0 bg-transparent px-3 py-1.5 font-body text-ui-body-sm font-semibold text-text2 hover:bg-transparent hover:text-text focus-visible:ring-2 focus-visible:ring-accent data-[state=on]:bg-card data-[state=on]:text-text data-[state=on]:shadow-soft",
        className,
      )}
      {...props}
    />
  );
}
