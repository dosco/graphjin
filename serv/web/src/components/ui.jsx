import React from "react";
import { AlertCircle, Loader2, ShieldAlert } from "lucide-react";

import { cn } from "@/lib/utils";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";

export const cx = cn;

export function PageHeader({ eyebrow, title, description, actions }) {
  return (
    <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
      <div className="grid gap-1.5">
        {eyebrow && (
          <p className="hud-label">{eyebrow}</p>
        )}
        <h1 className="text-[1.75rem] font-extrabold leading-[1.1] tracking-[-0.025em] text-foreground md:text-[2.125rem]">{title}</h1>
        {description && <p className="max-w-3xl text-[0.9375rem] leading-6 text-muted-foreground">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  );
}

export function Panel({ title, description, children, action, className, contentClassName }) {
  return (
    <section className={cx("min-w-0", className)}>
      {(title || action) && (
        <header className="flex flex-col gap-3 px-1 pt-2 pb-1 sm:flex-row sm:items-start sm:justify-between">
          <div>
            {title && <h2 className="text-[1.1875rem] font-extrabold tracking-tight text-foreground">{title}</h2>}
            {description && <p className="mt-0.5 font-mono text-xs leading-5 text-muted-foreground">{description}</p>}
          </div>
          {action}
        </header>
      )}
      <div className={cx("px-1 py-3", !(title || action) && "py-4", contentClassName)}>{children}</div>
    </section>
  );
}

export function Metric({ label, value, detail, tone = "neutral", className }) {
  const toneClass = tone === "good" ? "text-success" : tone === "warn" ? "text-warning" : "text-muted-foreground";
  return (
    <div className={cx("flex min-h-24 flex-col justify-between gap-2 py-3", className)}>
      <span className={cx("hud-label", toneClass)}>{label}</span>
      <strong className="font-mono text-[1.625rem] font-semibold leading-none tracking-tight text-foreground tabular-nums">{value ?? 0}</strong>
      {detail && <small className="text-xs leading-5 text-muted-foreground">{detail}</small>}
    </div>
  );
}

export function StatusPill({ status, severity }) {
  const normalized = (status || severity || "unknown").toLowerCase();
  const tone =
    normalized === "ready" || normalized === "ok" || normalized === "info" ||
    normalized === "verified" || normalized === "passed" || normalized === "approved" ||
    normalized === "closed" || normalized === "active"
      ? "good"
      : normalized === "degraded" || normalized === "warn" || normalized === "warning" ||
        normalized === "verifying" || normalized === "pending" || normalized === "paused" ||
        normalized === "stopped" || normalized === "needs improvement"
        ? "warn"
      : normalized === "failed" || normalized === "error" || normalized === "critical" || normalized === "high"
        ? "bad"
        : "neutral";
  const variant = tone === "good" ? "success" : tone === "warn" ? "warning" : tone === "bad" ? "destructive" : "muted";
  return <Badge variant={variant}>{status || severity || "unknown"}</Badge>;
}

export function LoadingState({ label = "Loading" }) {
  return (
    <div className="flex min-h-32 items-center justify-center gap-2.5 p-6 text-sm text-muted-foreground">
      <Loader2 aria-hidden="true" className="size-4 animate-spin text-brand" />
      <span className="font-mono text-xs uppercase tracking-[0.08em]">{label}</span>
    </div>
  );
}

export function EmptyState({ title = "Nothing here", message, children }) {
  return (
    <div className="py-6 text-sm">
      <div className="min-w-0 max-w-xl">
        <strong className="font-bold text-foreground">{title}</strong>
        {message && <p className="mt-1 leading-6 text-muted-foreground">{message}</p>}
        {children ? <div className="mt-3">{children}</div> : null}
      </div>
    </div>
  );
}

export function ErrorState({ title = "Unavailable", message }) {
  return (
    <Alert variant="destructive">
      <AlertCircle aria-hidden="true" className="size-4" />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{message}</AlertDescription>
    </Alert>
  );
}

export function PermissionState({ title = "No access", message }) {
  return (
    <Alert variant="warning">
      <ShieldAlert aria-hidden="true" className="size-4" />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{message}</AlertDescription>
    </Alert>
  );
}

export function DataErrorState({
  error,
  permissionMessage = "Your role can't see this.",
  unavailableMessage = "Couldn't reach the server.",
  queryMessage,
}) {
  if (error?.kind === "auth") {
    return <PermissionState message={permissionMessage} />;
  }
  if (error?.kind === "unavailable") {
    return <ErrorState title="Unavailable" message={unavailableMessage} />;
  }
  return <ErrorState title={error?.kind === "graphql" ? "Query failed" : "Unavailable"} message={queryMessage || error?.message || unavailableMessage} />;
}
