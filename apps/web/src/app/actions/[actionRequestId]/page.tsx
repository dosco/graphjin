"use client";

import { useCallback, useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import AppHeader from "@/components/AppHeader";
import { ActionChanges, hasActionChanges } from "@/components/ActionChanges";
import CreatorCredit from "@/components/CreatorCredit";
import PageHeading from "@/components/PageHeading";
import SectionNav from "@/components/SectionNav";
import {
  OUTCOME_LABEL,
  actionOutcome,
  describeOutcome,
  type ActionOutcome,
  systemForActionKind,
} from "@/lib/action-outcome";
import { cn } from "@/lib/cn";
import { Button } from "@/components/ui/button";
import { SkeletonList } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";

function actionStatusClasses(status: string): string {
  switch (status) {
    case "pending_approval":
      return "bg-watch-soft text-warn-ink";
    case "rejected":
    case "failed":
      return "bg-danger-soft text-danger";
    case "executed":
    case "succeeded":
      return "bg-success-soft text-success-mid";
    default:
      return "bg-accent-soft text-accent";
  }
}

type ActionDetailPayload = {
  actionRequest: {
    id: string;
    workflowRunId: string | null;
    triggeredByObservationId: string | null;
    policyId: string | null;
    scope: string;
    kind: string;
    target: string | null;
    payload: unknown;
    riskLevel: string | null;
    status: string;
    summary: string | null;
    approvedByUserId: string | null;
    approvedAt: string | null;
    rejectionReason: string | null;
    failureReason: string | null;
    createdAt: string;
    updatedAt: string;
  };
  executions: Array<{
    id: string;
    executor: string;
    commandOrOperation: string | null;
    payload: unknown;
    result: unknown;
    externalRef: string | null;
    status: string;
    error: string | null;
    startedAt: string | null;
    finishedAt: string | null;
    createdAt: string;
  }>;
  workflow: { id: string; name: string } | null;
  policy: { id: string; name: string; mode: string } | null;
  upstreamOutput: {
    id: string;
    title: string;
    workflowRunId: string | null;
  } | null;
  approverKind: "operator" | "policy" | "auto" | null;
  canRetry: boolean;
};

function backToActionsHref(outcome: ActionOutcome): string {
  if (outcome === "waiting") return "/actions?filter=awaiting";
  if (outcome === "rejected") return "/actions?filter=rejected";
  if (outcome === "failed" || outcome === "needs_check" || outcome === "partial")
    return "/actions?filter=failed";
  if (outcome === "completed" || outcome === "approved") return "/actions?filter=fired";
  return "/actions";
}

function backToActionsLabel(outcome: ActionOutcome): string {
  if (outcome === "waiting") return "Waiting for you";
  if (outcome === "rejected") return "Rejected";
  if (outcome === "failed" || outcome === "needs_check" || outcome === "partial") return "Failed";
  return "Completed";
}

function formatTime(iso: string | null): string {
  if (!iso) return "—";
  return new Date(iso).toLocaleString("en-IN", {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
    hour12: true,
  });
}

function formatRelative(iso: string | null): string {
  if (!iso) return "";
  const t = new Date(iso).getTime();
  const diff = Date.now() - t;
  if (diff < 60_000) return "just now";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
  return new Date(iso).toLocaleDateString("en-IN", {
    month: "short",
    day: "numeric",
  });
}

export default function ActionPage() {
  const params = useParams<{ actionRequestId: string }>();
  const actionRequestId = params?.actionRequestId;
  const router = useRouter();
  const [data, setData] = useState<ActionDetailPayload | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [rejecting, setRejecting] = useState(false);
  const [rejectReason, setRejectReason] = useState("");
  const [showPayload, setShowPayload] = useState(false);
  const [decisionError, setDecisionError] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!actionRequestId) return;
    try {
      const res = await fetch(`/api/action-requests/${actionRequestId}`, {
        cache: "no-store",
      });
      if (!res.ok) {
        setError(`Couldn't load action (HTTP ${res.status})`);
        return;
      }
      const json = (await res.json()) as ActionDetailPayload;
      setData(json);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load");
    }
  }, [actionRequestId]);

  useEffect(() => {
    const initialLoadId = window.setTimeout(() => {
      void load();
    }, 0);
    return () => window.clearTimeout(initialLoadId);
  }, [load]);

  // Poll while pending so an auto-approval or executor result lands without
  // reload. Stop once the action reaches a terminal state.
  useEffect(() => {
    const status = data?.actionRequest?.status;
    if (!status) return;
    if (status === "executed" || status === "rejected" || status === "failed") {
      return;
    }
    const id = setInterval(() => {
      void load();
    }, 3000);
    return () => clearInterval(id);
  }, [data?.actionRequest?.status, load]);

  const submitDecision = useCallback(
    async (decision: "approve" | "reject" | "retry", reason?: string) => {
      if (!actionRequestId) return;
      setBusy(true);
      setDecisionError(null);
      try {
        const response = await fetch(`/api/action-requests/${actionRequestId}`, {
          method: "PATCH",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ decision, reason }),
        });
        if (!response.ok) {
          const body = await response.json().catch(() => ({})) as { error?: string };
          setDecisionError(body.error ?? "Could not update this action. Refresh and try again.");
          return;
        }
        await load();
      } catch {
        setDecisionError("Could not update this action. Check your connection and try again.");
      } finally {
        setBusy(false);
      }
    },
    [actionRequestId, load],
  );

  const submitReject = useCallback(async () => {
    const reason = rejectReason.trim() || undefined;
    setRejecting(false);
    setRejectReason("");
    await submitDecision("reject", reason);
  }, [rejectReason, submitDecision]);

  const askFollowUp = useCallback(async () => {
    if (!actionRequestId) return;
    try {
      const res = await fetch("/api/work/threads", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ seedActionRequestId: actionRequestId }),
      });
      if (!res.ok) return;
      const json = (await res.json()) as { thread: { id: string } };
      if (json.thread?.id) router.push(`/work/${json.thread.id}`);
    } catch {
      // best-effort
    }
  }, [actionRequestId, router]);

  if (error) {
    return (
      <div className="root">
        <AppHeader>
          <SectionNav current="actions" />
        </AppHeader>
        <div className="py-[60px] text-center text-sm text-danger">{error}</div>
      </div>
    );
  }

  if (!data) {
    return (
      <div className="root">
        <AppHeader>
          <SectionNav current="actions" />
        </AppHeader>
        <SkeletonList rows={2} variant="row" label="Loading action" className="mt-10" />
      </div>
    );
  }

  const {
    actionRequest: ar,
    executions,
    workflow,
    policy,
    upstreamOutput,
    approverKind,
    canRetry,
  } = data;
  const isPending = ar.status === "pending_approval";
  const latestExecution = executions[0] ?? null;
  const resultStatus =
    latestExecution?.result && typeof latestExecution.result === "object"
      ? ((latestExecution.result as { status?: unknown }).status as string | undefined)
      : undefined;
  const outcome = actionOutcome(ar.status, resultStatus ?? latestExecution?.status);
  const outcomeSentence = describeOutcome(
    outcome,
    latestExecution?.error ?? ar.failureReason ?? ar.rejectionReason,
    systemForActionKind(ar.kind),
  );

  return (
    <>
      <div className="root run-root">
        <AppHeader
          back={{
            href: backToActionsHref(outcome),
            label: backToActionsLabel(outcome),
          }}
        >
          <SectionNav current="actions" />
        </AppHeader>

        <PageHeading
          title={ar.summary || ar.kind}
          meta={OUTCOME_LABEL[outcome]}
          description={[
            ar.kind,
            ar.target,
            ar.riskLevel ? `${ar.riskLevel.charAt(0).toUpperCase()}${ar.riskLevel.slice(1)} risk` : null,
            formatRelative(ar.createdAt),
          ]
            .filter(Boolean)
            .join(" · ")}
          actions={
            <Button
              size="sm"
              className="shrink-0"
              onClick={askFollowUp}
              title="Open an Ask thread pre-loaded with this action's context"
            >
              Ask a follow-up
            </Button>
          }
        />

        {decisionError && <p role="alert" className="mb-4 text-ui-body-sm text-danger">{decisionError}</p>}

        <Section title="Receipt">
          <dl className="grid gap-3.5 m-0">
            <Field label="Proposed">
              <span className="tabular-nums">{formatTime(ar.createdAt)}</span>
              {workflow && (
                <>
                  <span className="text-text3/70"> · </span>
                  <span>
                    by workflow{" "}
                    <Button
                      variant="ghost"
                      type="button"
                      className="bg-transparent border-0 cursor-pointer font-inherit p-0 font-semibold text-text underline underline-offset-2 hover:text-accent"
                      onClick={() =>
                        ar.workflowRunId &&
                        router.push(`/runs/${ar.workflowRunId}`)
                      }
                    >
                      {workflow.name}
                    </Button>
                  </span>
                </>
              )}
            </Field>

            <Field label="Approved">
              {ar.approvedAt ? (
                <>
                  <span className="tabular-nums">{formatTime(ar.approvedAt)}</span>
                  <span className="text-text3/70"> · </span>
                  {approverKind === "operator" && (
                    <span title={ar.approvedByUserId ?? undefined}>by an operator</span>
                  )}
                  {approverKind === "policy" && policy && (
                    <span>
                      by rule <strong>{policy.name}</strong> ({policy.mode})
                    </span>
                  )}
                  {approverKind === "auto" && <span>automatically</span>}
                </>
              ) : ar.status === "rejected" ? (
                <span className="text-text3 italic">
                  Rejected
                  {ar.rejectionReason ? `: ${ar.rejectionReason}` : ""}
                </span>
              ) : (
                <span className="text-text3 italic">awaiting decision</span>
              )}
            </Field>

            <Field label="Executor">
              {latestExecution ? (
                <>
                  <span className="font-mono">{latestExecution.executor}</span>
                  <span className="text-text3/70"> · </span>
                  <span
                    className={cn(
                      "inline-block px-2 py-0.5 rounded-full text-ui-caption font-semibold",
                      outcome === "needs_check" || outcome === "partial"
                        ? "bg-watch-soft text-warn-ink"
                        : actionStatusClasses(latestExecution.status),
                    )}
                  >
                    {outcome === "completed" || outcome === "approved"
                      ? "Succeeded"
                      : OUTCOME_LABEL[outcome]}
                  </span>
                  {latestExecution.finishedAt && (
                    <>
                      <span className="text-text3/70"> · </span>
                      <span className="font-mono">
                        {formatTime(latestExecution.finishedAt)}
                      </span>
                    </>
                  )}
                  {outcomeSentence && (
                    <div
                      className={cn(
                        "mt-2 rounded-lg px-3 py-2.5 text-ui-body-sm leading-[1.5]",
                        outcome === "failed"
                          ? "bg-danger-soft text-danger"
                          : "bg-watch-soft text-warn-ink",
                      )}
                    >
                      {outcomeSentence}
                      {latestExecution.error ? (
                        <span className="mt-1 block font-mono text-ui-caption opacity-75">
                          {latestExecution.error}
                        </span>
                      ) : null}
                    </div>
                  )}
                </>
              ) : (
                <span className="text-text3 italic">not yet executed</span>
              )}
            </Field>

            {hasActionChanges(ar.payload) ? (
              <Field label="Changes">
                <ActionChanges payload={ar.payload} className="mt-1" />
              </Field>
            ) : null}

            <Field label="Technical details">
              <Button
                variant="ghost"
                type="button"
                className="bg-transparent border-0 p-0 font-inherit text-accent underline underline-offset-2 cursor-pointer"
                onClick={() => setShowPayload((s) => !s)}
              >
                {showPayload ? "Hide" : "Show"} the raw payload
              </Button>
              {showPayload && (
                <pre className="mt-2 px-3.5 py-3 bg-card border border-border rounded-[10px] font-mono text-ui-body-sm text-text2 whitespace-pre-wrap break-words overflow-x-auto">
                  {JSON.stringify(ar.payload, null, 2)}
                </pre>
              )}
            </Field>
          </dl>
        </Section>

        {(workflow || upstreamOutput) && (
          <Section title="Lineage">
            {workflow && ar.workflowRunId && (
              <p className="m-0 mb-2 text-ui-body text-text2 leading-[1.55]">
                Proposed by workflow{" "}
                <Button
                  variant="ghost"
                  type="button"
                  className="bg-transparent border-0 cursor-pointer font-inherit p-0 font-semibold text-text underline underline-offset-2 hover:text-accent"
                  onClick={() => router.push(`/runs/${ar.workflowRunId}`)}
                >
                  {workflow.name}
                </Button>
                .
              </p>
            )}
            {upstreamOutput && (
              <p className="m-0 mb-2 text-ui-body text-text2 leading-[1.55]">
                Triggered by finding <em>{upstreamOutput.title}</em>
                {upstreamOutput.workflowRunId && (
                  <>
                    {" "}
                    in{" "}
                    <Button
                      variant="ghost"
                      type="button"
                      className="bg-transparent border-0 cursor-pointer font-inherit p-0 font-semibold text-text underline underline-offset-2 hover:text-accent"
                      onClick={() =>
                        router.push(`/runs/${upstreamOutput.workflowRunId}`)
                      }
                    >
                      its run
                    </Button>
                  </>
                )}
              </p>
            )}
          </Section>
        )}

        {isPending && (
          <Section title="Decide">
            {rejecting ? (
              <div className="pt-3 border-t border-border mt-2.5 flex flex-col gap-2">
                <label className="text-ui-caption font-semibold text-text3">
                  Why are you rejecting this? (optional)
                </label>
                <Textarea
                  className="border border-border rounded-[10px] px-3 py-2 font-body text-ui-body-sm text-text bg-card resize-y min-h-[50px] outline-none focus:border-accent"
                  value={rejectReason}
                  placeholder="e.g. wrong channel, retry tomorrow…"
                  onChange={(e) => setRejectReason(e.target.value)}
                  autoFocus
                  rows={2}
                />
                <div className="flex gap-2 mt-2.5">
                  <Button
                    variant="danger"
                    size="sm"
                    disabled={busy}
                    onClick={() => void submitReject()}
                  >
                    Confirm reject
                  </Button>
                  <Button
                    size="sm"
                    disabled={busy}
                    onClick={() => {
                      setRejecting(false);
                      setRejectReason("");
                    }}
                  >
                    Cancel
                  </Button>
                </div>
              </div>
            ) : (
              <div className="flex gap-2 mt-2.5">
                <Button
                  variant="primary"
                  size="sm"
                  disabled={busy}
                  onClick={() => void submitDecision("approve")}
                >
                  Approve
                </Button>
                <Button
                  variant="danger"
                  size="sm"
                  disabled={busy}
                  onClick={() => setRejecting(true)}
                >
                  Reject
                </Button>
              </div>
            )}
          </Section>
        )}

        {ar.status === "failed" && canRetry && (
          <Section title="Try again">
            <p className="mb-3 text-ui-body-sm text-text2">No change was sent. You can retry this approved action.</p>
            <Button variant="primary" size="sm" disabled={busy} onClick={() => void submitDecision("retry")}>{busy ? "Queuing retry…" : "Retry action"}</Button>
          </Section>
        )}
      </div>

      <CreatorCredit />
    </>
  );
}

function Section({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className="mb-7">
      <div className="text-ui-caption font-semibold text-text3 mb-2.5">
        {title}
      </div>
      {children}
    </div>
  );
}

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="grid grid-cols-[110px_1fr] gap-x-4 gap-y-2.5 items-baseline">
      <dt className="text-ui-caption font-semibold text-text3 m-0">
        {label}
      </dt>
      <dd className="m-0 text-ui-body text-text2 leading-[1.55]">{children}</dd>
    </div>
  );
}
