"use client";

import {
  extractActionRequestFences,
  extractRuleSaveFence,
  extractWorkflowSaveFence,
} from "@neko/llm/workflows/fences";
import {
  ACTION_REQUEST_SCHEMA,
  POLICY_SAVE_SCHEMA,
  WORKFLOW_SAVE_SCHEMA,
} from "@neko/llm/workflows/fence-schemas";
import type {
  ActionRequestPayload,
  PolicySavePayload,
  WorkflowSavePayload,
} from "@neko/llm/workflows/fence-schemas";

const NEKO_FENCE_STRIP_RE = /```neko_[a-z_]+\s*[\s\S]*?(?:```|$)/gi;
const RULE_TOLERANT_RE = /```neko_rule_save\s*([\s\S]*?)(?:```|$)/i;
const WORKFLOW_TOLERANT_RE = /```neko_workflow_save\s*([\s\S]*?)(?:```|$)/i;
const ACTION_TOLERANT_RE = /```neko_action_request\s*([\s\S]*?)(?:```|$)/gi;

export function stripNekoFences(raw: string): string {
  return raw.replace(NEKO_FENCE_STRIP_RE, "").replace(/\n{3,}/g, "\n\n").trim();
}

function tolerantParse<T>(
  raw: string,
  re: RegExp,
  schema: { safeParse: (v: unknown) => { success: boolean; data?: T } },
): T | null {
  const m = raw.match(re);
  if (!m) return null;
  try {
    const result = schema.safeParse(JSON.parse(m[1].trim()));
    return result.success ? (result.data as T) : null;
  } catch {
    return null;
  }
}

export function extractRuleSaveEvent(raw: string): PolicySavePayload | null {
  return (
    extractRuleSaveFence(raw).payload ??
    tolerantParse<PolicySavePayload>(raw, RULE_TOLERANT_RE, POLICY_SAVE_SCHEMA)
  );
}

export function extractWorkflowSaveEvent(raw: string): WorkflowSavePayload | null {
  return (
    extractWorkflowSaveFence(raw).payload ??
    tolerantParse<WorkflowSavePayload>(raw, WORKFLOW_TOLERANT_RE, WORKFLOW_SAVE_SCHEMA)
  );
}

export function extractActionRequestEvents(raw: string): ActionRequestPayload[] {
  const strict = extractActionRequestFences(raw).payloads;
  if (strict.length) return strict;
  const out: ActionRequestPayload[] = [];
  for (const m of raw.matchAll(ACTION_TOLERANT_RE)) {
    try {
      const result = ACTION_REQUEST_SCHEMA.safeParse(JSON.parse(m[1].trim()));
      if (result.success) out.push(result.data);
    } catch {
      // skip malformed
    }
  }
  return out;
}

function modeLabel(mode: PolicySavePayload["mode"]): string {
  if (mode === "auto_approve") return "auto-approves";
  if (mode === "approval_required") return "needs approval";
  if (mode === "observe_only") return "observe only";
  if (mode === "draft_only") return "draft only";
  if (mode === "never") return "never fires";
  return mode;
}

function EventShell({
  href,
  className,
  children,
}: {
  href?: string;
  className: string;
  children: React.ReactNode;
}) {
  if (href) {
    return (
      <a className={`${className} rule-event-link`} href={href}>
        {children}
      </a>
    );
  }
  return <article className={className}>{children}</article>;
}

export function RuleSavedCard({
  payload,
  href,
}: {
  payload: PolicySavePayload;
  href?: string;
}) {
  const limits =
    payload.limits && Object.keys(payload.limits).length > 0
      ? Object.entries(payload.limits)
          .map(([k, v]) => `${k.replace(/_/g, " ")} ${v}`)
          .join(", ")
      : null;

  return (
    <EventShell href={href} className="rule-event">
      <span className="rule-event-tick" aria-hidden="true">✓</span>
      <div className="rule-event-body">
        <div className="rule-event-eyebrow">Rule saved</div>
        <div className="rule-event-name">{payload.name}</div>
        <div className="rule-event-meta">
          <span className="rule-event-mode">{modeLabel(payload.mode)}</span>
          {limits && (
            <>
              <span className="rule-event-sep">·</span>
              <span className="rule-event-limits">{limits}</span>
            </>
          )}
        </div>
      </div>
      {href && <span className="rule-event-arrow" aria-hidden="true">→</span>}
    </EventShell>
  );
}

export function WorkflowSavedCard({
  payload,
  href,
}: {
  payload: WorkflowSavePayload;
  href?: string;
}) {
  const stepCount = payload.steps.length;
  const cron = payload.triggers?.cron;
  return (
    <EventShell href={href} className="rule-event workflow-event">
      <span className="rule-event-tick workflow-event-tick" aria-hidden="true">⚙</span>
      <div className="rule-event-body">
        <div className="rule-event-eyebrow">Workflow saved</div>
        <div className="rule-event-name">{payload.name}</div>
        <div className="rule-event-meta">
          <span>{stepCount} {stepCount === 1 ? "step" : "steps"}</span>
          {cron && (
            <>
              <span className="rule-event-sep">·</span>
              <span className="rule-event-limits">runs <code>{cron}</code></span>
            </>
          )}
        </div>
      </div>
      {href && <span className="rule-event-arrow" aria-hidden="true">→</span>}
    </EventShell>
  );
}

export function ActionRequestCard({
  payload,
  href,
}: {
  payload: ActionRequestPayload;
  href?: string;
}) {
  return (
    <EventShell href={href} className="rule-event action-event">
      <span className="rule-event-tick action-event-tick" aria-hidden="true">→</span>
      <div className="rule-event-body">
        <div className="rule-event-eyebrow">Action proposed</div>
        <div className="rule-event-name">{payload.summary}</div>
        <div className="rule-event-meta">
          <span>{payload.kind}</span>
          {payload.target && (
            <>
              <span className="rule-event-sep">·</span>
              <span className="rule-event-limits">{payload.target}</span>
            </>
          )}
          {payload.risk_level && (
            <>
              <span className="rule-event-sep">·</span>
              <span className={`action-event-risk action-event-risk-${payload.risk_level}`}>
                {payload.risk_level}
              </span>
            </>
          )}
        </div>
      </div>
      {href && <span className="rule-event-arrow" aria-hidden="true">→</span>}
    </EventShell>
  );
}
