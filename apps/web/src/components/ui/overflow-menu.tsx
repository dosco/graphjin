"use client";

import { MoreHorizontal } from "lucide-react";
import type { ComponentPropsWithoutRef, ReactNode } from "react";
import { IconButton } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

type OverflowMenuProps = {
  label?: string;
  size?: "icon-sm" | "icon";
  align?: "start" | "end";
  children: ReactNode;
  className?: string;
};

export function OverflowMenu({
  label = "More actions",
  size = "icon-sm",
  align = "end",
  children,
  className,
}: OverflowMenuProps) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <IconButton
          label={label}
          size={size}
          variant="ghost"
          className={className}
          data-ui-menu-trigger=""
        >
          <MoreHorizontal aria-hidden="true" />
        </IconButton>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align={align}
        className="min-w-44 border border-border bg-card p-1.5 shadow-lift"
      >
        {children}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

type MenuItemProps = {
  danger?: boolean;
} & ComponentPropsWithoutRef<typeof DropdownMenuItem>;

export function MenuItem({
  danger = false,
  className,
  ...props
}: MenuItemProps) {
  return (
    <DropdownMenuItem
      data-ui-menu-item=""
      variant={danger ? "destructive" : "default"}
      className={className}
      {...props}
    />
  );
}
