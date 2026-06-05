import React from "react";
import { AlertCircle, CheckCircle2, Loader2, ShieldAlert } from "lucide-react";

export function cx(...parts) {
  return parts.filter(Boolean).join(" ");
}

export function PageHeader({ eyebrow, title, description, actions }) {
  return (
    <div className="page-header">
      <div>
        {eyebrow && <p className="eyebrow">{eyebrow}</p>}
        <h1>{title}</h1>
        {description && <p>{description}</p>}
      </div>
      {actions && <div className="page-actions">{actions}</div>}
    </div>
  );
}

export function Panel({ title, description, children, action, className }) {
  return (
    <section className={cx("panel", className)}>
      {(title || action) && (
        <div className="panel-header">
          <div>
            {title && <h2>{title}</h2>}
            {description && <p>{description}</p>}
          </div>
          {action}
        </div>
      )}
      {children}
    </section>
  );
}

export function Metric({ label, value, detail, tone = "neutral" }) {
  return (
    <div className={cx("metric", `metric-${tone}`)}>
      <span>{label}</span>
      <strong>{value}</strong>
      {detail && <small>{detail}</small>}
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
  return <span className={cx("status-pill", `status-${tone}`)}>{status || severity || "unknown"}</span>;
}

export function LoadingState({ label = "Loading" }) {
  return (
    <div className="state state-loading">
      <Loader2 aria-hidden="true" className="spin" size={18} />
      <span>{label}</span>
    </div>
  );
}

export function EmptyState({ title = "No data", message = "Nothing matched this view." }) {
  return (
    <div className="state">
      <CheckCircle2 aria-hidden="true" size={18} />
      <div>
        <strong>{title}</strong>
        <p>{message}</p>
      </div>
    </div>
  );
}

export function ErrorState({ title = "Unavailable", message }) {
  return (
    <div className="state state-error">
      <AlertCircle aria-hidden="true" size={18} />
      <div>
        <strong>{title}</strong>
        <p>{message}</p>
      </div>
    </div>
  );
}

export function PermissionState({ title = "Operator access required", message }) {
  return (
    <div className="state state-permission">
      <ShieldAlert aria-hidden="true" size={18} />
      <div>
        <strong>{title}</strong>
        <p>{message}</p>
      </div>
    </div>
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
