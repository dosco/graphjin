import type { ComponentPropsWithoutRef, ReactNode } from "react";
import { cn } from "@/lib/cn";

export function Empty({
  className,
  ...props
}: ComponentPropsWithoutRef<"section">) {
  return (
    <section
      data-slot="empty"
      data-ui-empty-state=""
      className={cn(
        "mx-auto grid max-w-[520px] justify-items-center gap-3 py-14 text-center",
        className,
      )}
      {...props}
    />
  );
}

export function EmptyMedia({
  className,
  ...props
}: ComponentPropsWithoutRef<"div">) {
  return (
    <div
      data-slot="empty-icon"
      data-ui-empty-state-icon=""
      className={cn(
        "grid size-11 place-items-center rounded-inner bg-neutral text-text2 [&_svg]:size-5",
        className,
      )}
      {...props}
    />
  );
}

export function EmptyHeader({
  className,
  ...props
}: ComponentPropsWithoutRef<"div">) {
  return (
    <div
      data-slot="empty-header"
      data-ui-empty-state-copy=""
      className={cn("grid gap-1.5", className)}
      {...props}
    />
  );
}

export function EmptyTitle({
  className,
  ...props
}: ComponentPropsWithoutRef<"h2">) {
  return (
    <h2
      data-slot="empty-title"
      data-ui-empty-state-title=""
      className={cn("text-ui-section", className)}
      {...props}
    />
  );
}

export function EmptyDescription({
  className,
  ...props
}: ComponentPropsWithoutRef<"div">) {
  return (
    <div
      data-slot="empty-description"
      data-ui-empty-state-body=""
      className={cn(
        "text-ui-body-sm leading-[var(--leading-body)] text-text2",
        className,
      )}
      {...props}
    />
  );
}

export function EmptyContent({
  className,
  ...props
}: ComponentPropsWithoutRef<"div">) {
  return (
    <div
      data-slot="empty-content"
      data-ui-empty-state-action=""
      className={cn("pt-1", className)}
      {...props}
    />
  );
}

type EmptyStateProps = {
  title: string;
  body?: ReactNode;
  action?: ReactNode;
  icon?: ReactNode;
  className?: string;
};

export function EmptyState({
  title,
  body,
  action,
  icon,
  className,
}: EmptyStateProps) {
  return (
    <Empty className={className}>
      {icon ? <EmptyMedia>{icon}</EmptyMedia> : null}
      <EmptyHeader>
        <EmptyTitle>{title}</EmptyTitle>
        {body ? <EmptyDescription>{body}</EmptyDescription> : null}
      </EmptyHeader>
      {action ? <EmptyContent>{action}</EmptyContent> : null}
    </Empty>
  );
}
