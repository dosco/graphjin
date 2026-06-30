import React from "react";
import { AlertCircle, CheckCircle2, Loader2, ShieldAlert } from "lucide-react";

import { cn } from "@/lib/utils";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

export const cx = cn;

export function PageHeader({ eyebrow, title, description, actions }) {
  return (
    <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
      <div className="grid gap-2">
        {eyebrow && <p className="text-xs font-semibold uppercase text-muted-foreground">{eyebrow}</p>}
        <h1 className="text-3xl font-semibold leading-tight tracking-normal text-foreground md:text-4xl">{title}</h1>
        {description && <p className="max-w-3xl text-sm leading-6 text-muted-foreground md:text-base">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  );
}

export function Panel({ title, description, children, action, className }) {
  return (
    <Card className={className}>
      {(title || action) && (
        <CardHeader className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div>
            {title && <CardTitle>{title}</CardTitle>}
            {description && <CardDescription>{description}</CardDescription>}
          </div>
          {action}
        </CardHeader>
      )}
      <CardContent className={cx(!(title || action) && "pt-5")}>{children}</CardContent>
    </Card>
  );
}

export function Metric({ label, value, detail, tone = "neutral" }) {
  const toneClass =
    tone === "good"
      ? "border-emerald-200 bg-emerald-50"
      : tone === "warn"
        ? "border-amber-200 bg-amber-50"
        : "bg-background";
  return (
    <div className={cx("flex min-h-28 flex-col justify-between gap-3 rounded-md border p-4", toneClass)}>
      <span className="text-xs font-medium uppercase text-muted-foreground">{label}</span>
      <strong className="text-2xl font-semibold leading-none tracking-normal text-foreground">{value ?? 0}</strong>
      {detail && <small className="text-xs leading-5 text-muted-foreground">{detail}</small>}
    </div>
  );
}

export function StatusPill({ status, severity }) {
  const normalized = (status || severity || "unknown").toLowerCase();
  const tone =
    normalized === "ready" || normalized === "ok" || normalized === "info"
      ? "good"
      : normalized === "degraded" || normalized === "warn" || normalized === "warning"
        ? "warn"
      : normalized === "failed" || normalized === "error" || normalized === "critical" || normalized === "high"
        ? "bad"
        : "neutral";
  const variant = tone === "good" ? "success" : tone === "warn" ? "warning" : tone === "bad" ? "destructive" : "muted";
  return <Badge variant={variant}>{status || severity || "unknown"}</Badge>;
}

export function LoadingState({ label = "Loading" }) {
  return (
    <div className="flex min-h-32 items-center justify-center gap-2 rounded-lg border bg-card p-6 text-sm text-muted-foreground">
      <Loader2 aria-hidden="true" className="size-4 animate-spin" />
      <span>{label}</span>
    </div>
  );
}

export function EmptyState({ title = "No data", message = "Nothing matched this view." }) {
  return (
    <div className="flex min-h-32 items-start gap-3 rounded-lg border border-dashed bg-muted/30 p-5 text-sm">
      <CheckCircle2 aria-hidden="true" className="mt-0.5 size-4 text-muted-foreground" />
      <div>
        <strong className="font-medium text-foreground">{title}</strong>
        <p className="mt-1 leading-6 text-muted-foreground">{message}</p>
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
