import React from "react";
import { AlertCircle, CheckCircle2, Loader2, ShieldAlert } from "lucide-react";

import { cn } from "@/lib/utils";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";

export const cx = cn;

export function PageHeader({ eyebrow, title, description, actions }) {
  return (
    <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
      <div className="grid gap-2">
        {eyebrow && <p className="text-xs font-semibold uppercase tracking-normal text-muted-foreground">{eyebrow}</p>}
        <h1 className="text-3xl font-semibold leading-tight tracking-normal text-foreground md:text-[2.35rem]">{title}</h1>
        {description && <p className="max-w-3xl text-sm leading-6 text-muted-foreground md:text-[0.95rem]">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  );
}

export function Panel({ title, description, children, action, className, contentClassName }) {
  return (
    <section className={cx("min-w-0 border-y bg-background", className)}>
      {(title || action) && (
        <header className="flex flex-col gap-3 border-b bg-muted/25 px-4 py-4 sm:flex-row sm:items-start sm:justify-between sm:px-5">
          <div>
            {title && <h2 className="text-sm font-semibold text-foreground">{title}</h2>}
            {description && <p className="mt-1 text-sm leading-5 text-muted-foreground">{description}</p>}
          </div>
          {action}
        </header>
      )}
      <div className={cx("px-4 py-4 sm:px-5", !(title || action) && "py-5", contentClassName)}>{children}</div>
    </section>
  );
}

export function Metric({ label, value, detail, tone = "neutral", className }) {
  const toneClass =
    tone === "good"
      ? "bg-emerald-500/8"
      : tone === "warn"
        ? "bg-amber-500/10"
        : "bg-muted/45";
  return (
    <div className={cx("flex min-h-24 flex-col justify-between gap-2 rounded-lg p-4", toneClass, className)}>
      <span className="text-xs font-medium uppercase text-muted-foreground">{label}</span>
      <strong className="text-2xl font-semibold leading-none tracking-normal text-foreground">{value ?? 0}</strong>
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
    <div className="flex min-h-32 items-center justify-center gap-2 bg-muted/25 p-6 text-sm text-muted-foreground">
      <Loader2 aria-hidden="true" className="size-4 animate-spin" />
      <span>{label}</span>
    </div>
  );
}

export function EmptyState({ title = "No data", message = "Nothing matched this view.", children }) {
  return (
    <div className="flex min-h-32 items-start gap-3 rounded-lg border border-dashed bg-card p-5 text-sm">
      <CheckCircle2 aria-hidden="true" className="mt-0.5 size-4 text-muted-foreground" />
      <div className="min-w-0">
        <strong className="font-medium text-foreground">{title}</strong>
        <p className="mt-1 leading-6 text-muted-foreground">{message}</p>
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

export function PermissionState({ title = "Operator access required", message }) {
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
  permissionMessage = "This console uses the current GraphJin request context. Open it from local development or grant the operator/admin role access in agentic mode.",
  unavailableMessage = "GraphJin did not return this data. Check that the service is running and the console route is served by GraphJin.",
  queryMessage,
}) {
  if (error?.kind === "auth") {
    return <PermissionState message={permissionMessage} />;
  }
  if (error?.kind === "unavailable") {
    return <ErrorState title="Service unavailable" message={unavailableMessage} />;
  }
  return <ErrorState title={error?.kind === "graphql" ? "GraphQL query failed" : "Unavailable"} message={queryMessage || error?.message || unavailableMessage} />;
}
