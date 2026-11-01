"use client";

import { useCallback, useEffect, useState } from "react";
import { SkeletonList } from "@/components/ui/skeleton";
import { useParams, useRouter } from "next/navigation";
import { Check, Download, MessageCircle, Pin, TriangleAlert } from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import AppHeader from "@/components/AppHeader";
import CreatorCredit from "@/components/CreatorCredit";
import PageHeading from "@/components/PageHeading";
import SectionNav from "@/components/SectionNav";
import { ActionGroup } from "@/components/ui/action-group";
import { Button, ButtonLink } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Disclosure } from "@/components/ui/disclosure";
import { Field, Textarea } from "@/components/ui/field";
import { Badge, type BadgeVariant } from "@/components/ui/badge";
import { cn } from "@/lib/cn";

type RunDetailPayload = {
  workflow: {
    id: string;
    name: string;
    description: string;
    goal: string;
  } | null;
  run: {
    id: string;
    workflowId: string;
    threadId: string;
    workRunId: string | null;
    triggerKind: string;
    triggerPayload: unknown;
    executionMode: "single" | "batch" | null;
    triggerInputPreview: Record<string, unknown> | null;
    chainDepth: number;
    status: string;
    telemetry: {
      durations?: {
        wallMs?: number;
        queueMs?: number;
        firstOutputMs?: number;
      };
      counts?: {
        inference?: number;
        tools?: number;
        delegations?: number;
        retries?: number;
        validations?: number;
      };
      batch?: {
        acceptedRows?: number;
        processedRows?: number;
        finalRows?: number;
        chunkCount?: number;
        artifactBytes?: number;
      };
      usage?: {
        inputTokens?: number;
        outputTokens?: number;
        totalTokens?: number;
        billedCostUsd?: number;
        estimatedCostUsd?: number;
        coverage?: "complete" | "partial" | "unavailable";
        missingReasons?: string[];
      };
      telemetryComplete?: boolean;
    } | null;
    terminalResult: Record<string, unknown> | null;
    progress: Record<string, unknown>;
    queueAttempts: number;
    admittedAt: string | null;
    resultExpiresAt: string | null;
    artifactUrl: string | null;
    summary: string | null;
    error: string | null;
    startedAt: string | null;
    finishedAt: string | null;
    createdAt: string;
    updatedAt: string;
  };
  outputs: Array<{
    id: string;
    kind: string;
    title: string;
    body: string | null;
    payload: unknown;
    scope: string | null;
    topic: string | null;
    mood: string | null;
    createdAt: string;
  }>;
  actions: Array<{
    id: string;
    kind: string;
    target: string | null;
    payload: unknown;
    scope: string;
    riskLevel: string | null;
    status: string;
    summary: string | null;
    approvedAt: string | null;
    rejectionReason: string | null;
    createdAt: string;
  }>;
  events: Array<{
    seq: number;
    type: string;
    event: Record<string, unknown> | null;
    createdAt: string;
  }>;
  lineage: {
    triggeredBySubscriptionId: string | null;
    triggeredByOutputId: string | null;
    triggeredByObservationId: string | null;
    upstream: null | {
      output: {
        id: string;
        title: string;
        scope: string | null;
        mood: string | null;
        createdAt: string;
      };
      workflow: { id: string; name: string } | null;
      workflowRunId: string | null;
    };
  };
};

function formatTime(iso: string | null): string {
  if (!iso) return "—";
  return new Date(iso).toLocaleTimeString("en-IN", {
    hour: "numeric",
    minute: "2-digit",
    hour12: true,
  });
}

function formatDuration(ms: number | null): string {
  if (ms == null) return "—";
  if (ms < 1000) return `${ms}ms`;
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  return `${Math.floor(s / 60)}m ${s % 60}s`;
}

function formatRunTimestamp(iso: string): string {
  return new Date(iso).toLocaleString("en-IN", {
    day: "numeric",
    month: "short",
    year: "numeric",
    hour: "numeric",
    minute: "2-digit",
    hour12: true,
  });
}

const ACTION_STATUS_LABEL: Record<string, string> = {
  pending_approval: "Waiting for you",
  approved: "Approved",
  rejected: "Rejected",
  executed: "Completed",
  failed: "Failed",
};

function actionStatusLabel(s: string): string {
  return ACTION_STATUS_LABEL[s] ?? s.replace(/_/g, " ");
}

function moodTone(mood: string): string {
  switch (mood) {
    case "good":
      return "is-good";
    case "watch":
      return "is-watch";
    case "act":
      return "is-act";
    default:
      return "is-neutral";
  }
}

function moodLabel(mood: string): string {
  switch (mood) {
    case "good":
      return "On track";
    case "watch":
      return "Needs attention";
    case "act":
      return "Action needed";
    default:
      return mood.replace(/_/g, " ");
  }
}

function formatTaxonomy(value: string): string {
  return value
    .replace(/[_-]+/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function actionPillVariant(status: string): BadgeVariant {
  switch (status) {
    case "pending_approval":
      return "watch";
    case "executed":
      return "success";
    case "rejected":
    case "failed":
      return "danger";
    case "approved":
      return "live";
    default:
      return "muted";
  }
}

function actionRiskClasses(risk: string): string {
  switch (risk) {
    case "low":
      return "text-text2";
    case "medium":
      return "text-warn-ink";
    case "high":
    case "critical":
      return "text-danger";
    default:
      return "text-text2";
  }
}

function formatTrigger(kind: string, mode?: string | null): string {
  switch (kind) {
    case "manual":
      return "Manual";
    case "cron":
      return "Scheduled";
    case "subscription":
      return "Triggered";
    case "api":
      return `API${mode ? ` · ${formatTaxonomy(mode)}` : ""}`;
    default:
      return formatTaxonomy(kind);
  }
}

function formatBytes(value: number | undefined): string {
  if (value === undefined) return "—";
  if (value < 1_024) return `${value} B`;
  if (value < 1_048_576) return `${(value / 1_024).toFixed(1)} KiB`;
  return `${(value / 1_048_576).toFixed(1)} MiB`;
}

function formatNumber(value: number | undefined): string {
  if (value === undefined) return "—";
  return new Intl.NumberFormat("en-IN").format(value);
}

function formatCost(value: number | undefined): string {
  if (value === undefined) return "—";
  return `$${value.toFixed(value < 0.01 ? 4 : 2)}`;
}

function apiRunStatusVariant(status: string): BadgeVariant {
  if (status === "completed") return "success";
  if (status === "failed" || status === "cancelled") return "danger";
  if (status === "running") return "live";
  return "watch";
}

export default function RunPage() {
  const params = useParams<{ workflowRunId: string }>();
  const workflowRunId = params?.workflowRunId;
  const router = useRouter();
  const [data, setData] = useState<RunDetailPayload | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [showEvents, setShowEvents] = useState(false);
  const [showLineage, setShowLineage] = useState(false);
  const [actionBusyId, setActionBusyId] = useState<string | null>(null);
  const [rejectingId, setRejectingId] = useState<string | null>(null);
  const [rejectReason, setRejectReason] = useState("");
  const [pinningOutputId, setPinningOutputId] = useState<string | null>(null);
  const [pinnedOutputIds, setPinnedOutputIds] = useState<Set<string>>(
    () => new Set(),
  );

  const load = useCallback(async () => {
    if (!workflowRunId) return;
    try {
      const res = await fetch(`/api/workflow-runs/${workflowRunId}`, {
        cache: "no-store",
      });
      if (!res.ok) {
        setError(`Couldn't load run (HTTP ${res.status})`);
        return;
      }
      const json = (await res.json()) as RunDetailPayload;
      setData(json);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load");
    }
  }, [workflowRunId]);

  useEffect(() => {
    // This client route intentionally hydrates from its no-store API.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load();
  }, [load]);

  // Poll while the run isn't terminal so users can watch live progress.
  useEffect(() => {
    const status = data?.run?.status;
    if (!status) return;
    if (status === "completed" || status === "failed" || status === "cancelled") {
      return;
    }
    const id = setInterval(() => {
      void load();
    }, 3000);
    return () => clearInterval(id);
  }, [data?.run?.status, load]);

  const actOnRequest = useCallback(
    async (id: string, decision: "approve" | "reject", reason?: string) => {
      setActionBusyId(id);
      try {
        await fetch(`/api/action-requests/${id}`, {
          method: "PATCH",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ decision, reason }),
        });
        await load();
      } finally {
        setActionBusyId(null);
      }
    },
    [load],
  );

  const submitReject = useCallback(async () => {
    if (!rejectingId) return;
    const id = rejectingId;
    const reason = rejectReason.trim() || undefined;
    setRejectingId(null);
    setRejectReason("");
    await actOnRequest(id, "reject", reason);
  }, [rejectingId, rejectReason, actOnRequest]);

  const cancelReject = useCallback(() => {
    setRejectingId(null);
    setRejectReason("");
  }, []);

  const pinOutput = useCallback(async (outputId: string) => {
    setPinningOutputId(outputId);
    try {
      const res = await fetch("/api/briefing/pins", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ outputId }),
      });
      if (res.ok) {
        setPinnedOutputIds((current) => {
          const next = new Set(current);
          next.add(outputId);
          return next;
        });
      }
    } catch {
      // Best-effort: leave the control available so the operator can retry.
    } finally {
      setPinningOutputId(null);
    }
  }, []);

  const askFollowUp = useCallback(async () => {
    if (!workflowRunId) return;
    try {
      const res = await fetch("/api/work/threads", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ seedWorkflowRunId: workflowRunId }),
      });
      if (!res.ok) return;
      const json = (await res.json()) as { thread: { id: string } };
      if (json.thread?.id) router.push(`/work/${json.thread.id}`);
    } catch {
      // best-effort
    }
  }, [workflowRunId, router]);

  if (error) {
    return (
      <div className="root">
        <AppHeader>
          <SectionNav current="workflows" />
        </AppHeader>
        <div className="py-[60px] text-center text-sm text-danger">{error}</div>
      </div>
    );
  }

  if (!data) {
    return (
      <div className="root">
        <AppHeader>
          <SectionNav current="workflows" />
        </AppHeader>
        <SkeletonList rows={3} variant="row" label="Loading run" className="mt-10" />
      </div>
    );
  }

  const { run, workflow, outputs, actions, lineage } = data;
  const durationMs =
    run.startedAt && run.finishedAt
      ? new Date(run.finishedAt).getTime() - new Date(run.startedAt).getTime()
      : null;

  return (
    <>
      <div className="root run-root">
        <AppHeader
          back={{
            href: workflow ? `/workflows?id=${encodeURIComponent(workflow.id)}` : "/workflows",
            label: workflow?.name ?? "Workflows",
          }}
        >
          <SectionNav current="workflows" />
        </AppHeader>

        <PageHeading
          title={workflow?.name ?? "Run"}
          meta={run.status.replace(/_/g, " ")}
          description={`${formatRunTimestamp(run.startedAt ?? run.createdAt)} · ${formatTrigger(run.triggerKind, run.executionMode)} · ${formatDuration(durationMs)}`}
          actions={
            <Button
              size="sm"
              className="run-followup-control"
              onClick={askFollowUp}
              title="Open an Ask thread pre-loaded with this run's context"
            >
              <MessageCircle aria-hidden="true" />
              <span>Ask about this run</span>
            </Button>
          }
        />

        {run.triggerKind === "api" ? (
          <ApiRunContext run={run} workflowId={workflow?.id ?? run.workflowId} />
        ) : null}

        {run.status === "completed" &&
          outputs.length === 0 &&
          actions.length === 0 && (
            <div className="run-empty-summary">
              {run.summary?.trim() || "Looked at the data; nothing to flag."}
            </div>
          )}

        <Section
          title="Findings"
          meta={`${outputs.length} ${outputs.length === 1 ? "result" : "results"}`}
        >
          {outputs.length === 0 ? (
            <p className="run-empty-state">
              This run has not produced any outputs yet.
            </p>
          ) : (
            <ul className="run-finding-list">
              {outputs.map((o) => (
                <li
                  key={o.id}
                  className={cn(
                    "run-finding-card run-output-card",
                    moodTone(o.mood ?? ""),
                  )}
                >
                  <div className="run-finding-head">
                    <div className="run-finding-context">
                      <span>{formatTaxonomy(o.kind)}</span>
                      <span aria-hidden="true">·</span>
                      <time dateTime={o.createdAt}>{formatTime(o.createdAt)}</time>
                    </div>
                    {o.mood && (
                      <span
                        className={cn(
                          "run-finding-status",
                          moodTone(o.mood),
                        )}
                      >
                        {o.mood === "act" && (
                          <TriangleAlert aria-hidden="true" />
                        )}
                        {moodLabel(o.mood)}
                      </span>
                    )}
                  </div>

                  <h2 className="run-finding-title">{o.title}</h2>

                  {o.body && (
                    <div className="run-evt-message run-finding-body">
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>{o.body}</ReactMarkdown>
                    </div>
                  )}

                  <footer className="run-finding-footer">
                    <div className="run-finding-tags" aria-label="Finding classification">
                      {o.scope && <span>{formatTaxonomy(o.scope)}</span>}
                      {o.topic && o.topic !== o.scope && (
                        <span>{formatTaxonomy(o.topic)}</span>
                      )}
                    </div>
                    <Button
                      variant="ghost"
                      size="sm"
                      className={cn(
                        "run-finding-pin-control",
                        pinnedOutputIds.has(o.id) &&
                          "disabled:text-success-mid disabled:opacity-100",
                      )}
                      disabled={
                        pinningOutputId === o.id || pinnedOutputIds.has(o.id)
                      }
                      onClick={() => void pinOutput(o.id)}
                      title="Pin this finding to the Briefing"
                    >
                      {pinnedOutputIds.has(o.id) ? (
                        <Check aria-hidden="true" />
                      ) : (
                        <Pin aria-hidden="true" />
                      )}
                      <span>
                        {pinnedOutputIds.has(o.id)
                          ? "Pinned"
                          : pinningOutputId === o.id
                            ? "Pinning…"
                            : "Pin to briefing"}
                      </span>
                    </Button>
                  </footer>
                </li>
              ))}
            </ul>
          )}
        </Section>

        {actions.length > 0 ? (
          <Section
            title="Actions"
            meta={`${actions.length} ${actions.length === 1 ? "proposal" : "proposals"}`}
          >
            <ul className="list-none p-0 m-0 flex flex-col gap-2.5">
              {actions.map((a) => (
                <li
                  key={a.id}
                  className="run-action-card bg-card border border-border rounded-2xl px-4 py-3.5"
                >
                  <div className="flex items-start justify-between gap-2 mb-1.5">
                    <Button
                      size="sm"
                      variant="ghost"
                      className="font-semibold text-sm text-text leading-[1.4] bg-transparent border-0 p-0 text-left cursor-pointer hover:underline hover:underline-offset-[3px]"
                      onClick={() => router.push(`/actions/${a.id}`)}
                      title="Open action receipt"
                    >
                      {a.summary || a.kind}
                    </Button>
                    <Badge variant={actionPillVariant(a.status)}>
                      {actionStatusLabel(a.status)}
                    </Badge>
                  </div>
                  <div className="flex flex-wrap gap-1.5 items-center text-xs text-text3">
                    <span className="font-mono text-text2">{a.kind}</span>
                    {a.target && (
                      <>
                        <span className="text-text3/70">·</span>
                        <span className="font-mono">{a.target}</span>
                      </>
                    )}
                    {a.riskLevel && (
                      <>
                        <span className="text-text3/70">·</span>
                        <span className={cn("font-semibold", actionRiskClasses(a.riskLevel))}>
                          {a.riskLevel}
                        </span>
                      </>
                    )}
                  </div>
                  {a.status === "pending_approval" && rejectingId === a.id ? (
                    <div className="pt-3 border-t border-border mt-2.5 flex flex-col gap-2">
                      <Field label="Why are you rejecting this?" hint="Optional">
                        <Textarea
                          className="min-h-[72px]"
                          value={rejectReason}
                          placeholder="e.g. wrong channel, retry tomorrow…"
                          onChange={(e) => setRejectReason(e.target.value)}
                          autoFocus
                          rows={2}
                        />
                      </Field>
                      <div className="flex gap-2 mt-2.5">
                        <Button
                          variant="danger"
                          size="sm"
                          disabled={actionBusyId === a.id}
                          onClick={() => void submitReject()}
                        >
                          Confirm reject
                        </Button>
                        <Button
                          size="sm"
                          disabled={actionBusyId === a.id}
                          onClick={cancelReject}
                        >
                          Cancel
                        </Button>
                      </div>
                    </div>
                  ) : a.status === "pending_approval" ? (
                    <div className="flex gap-2 mt-2.5">
                      <Button
                        variant="primary"
                        size="sm"
                        disabled={actionBusyId === a.id}
                        onClick={() => void actOnRequest(a.id, "approve")}
                      >
                        Approve
                      </Button>
                      <Button
                        variant="danger"
                        size="sm"
                        disabled={actionBusyId === a.id}
                        onClick={() => setRejectingId(a.id)}
                      >
                        Reject
                      </Button>
                    </div>
                  ) : null}
                  {a.status === "rejected" && a.rejectionReason && (
                    <p className="mt-2 text-ui-body-sm text-text2 italic">
                      Rejected: {a.rejectionReason}
                    </p>
                  )}
                </li>
              ))}
            </ul>
          </Section>
        ) : outputs.length > 0 ? (
          <p className="run-no-actions">
            <span aria-hidden="true" />
            Reporting only — this run proposed no follow-up action.
          </p>
        ) : null}

        <section className="run-trace" aria-labelledby="run-trace-title">
          <header className="run-trace-head">
            <div>
              <h2 id="run-trace-title">Run details</h2>
              <p>Execution trace and workflow origin.</p>
            </div>
          </header>

          <Disclosure
            className="run-expander"
            open={showEvents}
            onToggle={(e) => setShowEvents(e.currentTarget.open)}
            title="Execution trace"
            meta={`${data.events.length} events`}
          >
            <div className="run-expander-content">
              {data.events.length === 0 ? (
                <p className="run-empty-state">No events recorded.</p>
              ) : (
                <EventStream events={data.events} />
              )}
            </div>
          </Disclosure>

          <Disclosure
            className="run-expander"
            open={showLineage}
            onToggle={(e) => setShowLineage(e.currentTarget.open)}
            title="Origin"
            meta={formatTrigger(run.triggerKind, run.executionMode)}
          >
            <div className="run-expander-content">
              {!lineage.upstream && run.triggerKind === "manual" && (
                <p className="run-empty-state">
                  Started manually from the workflow drawer.
                </p>
              )}
              {!lineage.upstream && run.triggerKind === "cron" && (
                <p className="run-empty-state">
                  Scheduled by cron · no upstream output.
                </p>
              )}
              {lineage.upstream && (
                <div className="text-ui-body leading-[1.55] text-text">
                  <p>
                    Triggered by{" "}
                    {lineage.upstream.workflow ? (
                      <>
                        workflow{" "}
                        <strong>{lineage.upstream.workflow.name}</strong>
                      </>
                    ) : (
                      "an upstream workflow"
                    )}{" "}
                    on output{" "}
                    <span className="italic text-text2">
                      &ldquo;{lineage.upstream.output.title}&rdquo;
                    </span>
                  </p>
                  {lineage.upstream.workflowRunId && (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="mt-2"
                      onClick={() =>
                        router.push(`/runs/${lineage.upstream!.workflowRunId}`)
                      }
                    >
                      Open the run that started this
                    </Button>
                  )}
                </div>
              )}
            </div>
          </Disclosure>
        </section>
      </div>

      <CreatorCredit />
    </>
  );
}

type RunRecord = RunDetailPayload["run"];

function recordNumber(
  record: Record<string, unknown> | null | undefined,
  key: string,
): number | undefined {
  const value = record?.[key];
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function recordString(
  record: Record<string, unknown> | null | undefined,
  key: string,
): string | undefined {
  const value = record?.[key];
  return typeof value === "string" ? value : undefined;
}

function ApiRunContext({
  run,
  workflowId,
}: {
  run: RunRecord;
  workflowId: string;
}) {
  const usage = run.telemetry?.usage;
  const counts = run.telemetry?.counts;
  const batch = run.telemetry?.batch;
  const acceptedRows =
    recordNumber(run.progress, "acceptedRows") ?? batch?.acceptedRows;
  const processedRows =
    recordNumber(run.progress, "processedRows") ?? batch?.processedRows;
  const finalRows = recordNumber(run.progress, "finalRows") ?? batch?.finalRows;
  const chunkCount =
    recordNumber(run.progress, "chunkCount") ?? batch?.chunkCount;
  const plannedChunks = recordNumber(run.progress, "plannedChunks");
  const artifactBytes =
    recordNumber(run.progress, "artifactBytes") ?? batch?.artifactBytes;
  const stage = recordString(run.progress, "stage") ?? run.status;
  const queueMs =
    run.telemetry?.durations?.queueMs ??
    (run.admittedAt && run.startedAt
      ? Math.max(
          0,
          new Date(run.startedAt).getTime() - new Date(run.admittedAt).getTime(),
        )
      : undefined);
  const cost = usage?.billedCostUsd ?? usage?.estimatedCostUsd;
  const progressPercent =
    acceptedRows && processedRows !== undefined
      ? Math.min(100, Math.round((processedRows / acceptedRows) * 100))
      : null;

  return (
    <Section
      title="API execution"
      meta={run.executionMode ? `${formatTaxonomy(run.executionMode)} mode` : "API"}
    >
      <Card className="run-api-card">
        <div className="run-api-card-head">
          <div>
            <Badge variant={apiRunStatusVariant(run.status)}>
              {formatTaxonomy(run.status)}
            </Badge>
            <strong>{formatTaxonomy(stage)}</strong>
          </div>
          <ActionGroup>
            <ButtonLink
              variant="ghost"
              size="sm"
              href={`/workflows?id=${encodeURIComponent(workflowId)}`}
            >
              View workflow
            </ButtonLink>
            {run.artifactUrl ? (
              <ButtonLink size="sm" href={run.artifactUrl} download>
                <Download aria-hidden="true" />
                Download CSV
              </ButtonLink>
            ) : null}
          </ActionGroup>
        </div>

        <dl className="run-api-metrics">
          <div>
            <dt>Queue time</dt>
            <dd>{formatDuration(queueMs ?? null)}</dd>
          </div>
          <div>
            <dt>Queue attempts</dt>
            <dd>{formatNumber(run.queueAttempts)}</dd>
          </div>
          <div>
            <dt>Model calls</dt>
            <dd>{formatNumber(counts?.inference)}</dd>
          </div>
          <div>
            <dt>Tool calls</dt>
            <dd>{formatNumber(counts?.tools)}</dd>
          </div>
          <div>
            <dt>Total tokens</dt>
            <dd>{formatNumber(usage?.totalTokens)}</dd>
          </div>
          <div>
            <dt>Provider spend</dt>
            <dd>{formatCost(cost)}</dd>
          </div>
        </dl>

        {run.error ? (
          <div className="run-api-error" role="alert">
            <Badge variant="danger">Execution error</Badge>
            <p>{run.error}</p>
          </div>
        ) : null}

        {run.executionMode === "batch" ? (
          <div className="run-api-progress" aria-label="Batch progress">
            <div>
              <span>
                {formatNumber(processedRows)} of {formatNumber(acceptedRows)} records
              </span>
              <strong>{progressPercent === null ? "—" : `${progressPercent}%`}</strong>
            </div>
            <div className="run-api-progress-track" aria-hidden="true">
              <span style={{ width: `${progressPercent ?? 0}%` }} />
            </div>
            <p>
              {formatNumber(finalRows)} final rows · {formatNumber(chunkCount)}
              {plannedChunks === undefined ? "" : ` of ${formatNumber(plannedChunks)}`} chunks · {formatBytes(artifactBytes)} artifact
            </p>
          </div>
        ) : null}

        <div className="run-api-json-grid">
          <JsonPanel
            title="Input preview"
            value={run.triggerInputPreview}
            empty="No retained preview."
          />
          {run.terminalResult ? (
            <JsonPanel
              title="Terminal result"
              value={run.terminalResult}
              empty="No inline result."
            />
          ) : null}
        </div>

        <div className="run-api-retention">
          <span>
            Telemetry {usage?.coverage ?? "unavailable"}
            {run.telemetry?.telemetryComplete ? " · complete" : ""}
          </span>
          {run.resultExpiresAt ? (
            <span>Result retained until {formatRunTimestamp(run.resultExpiresAt)}</span>
          ) : null}
        </div>
      </Card>
    </Section>
  );
}

function JsonPanel({
  title,
  value,
  empty,
}: {
  title: string;
  value: Record<string, unknown> | null;
  empty: string;
}) {
  return (
    <div className="run-api-json">
      <h3>{title}</h3>
      {value ? <pre><code>{JSON.stringify(value, null, 2)}</code></pre> : <p>{empty}</p>}
    </div>
  );
}

type EventRow = RunDetailPayload["events"][number];

type RenderedItem =
  | { type: "message-block"; seq: number; content: string }
  | { type: "single"; seq: number; ev: EventRow };

function coalesceEvents(events: EventRow[]): RenderedItem[] {
  // Streaming chunks each become their own `message` event row. Rendering each
  // independently through ReactMarkdown breaks any inline syntax that spans a
  // chunk boundary (e.g. `**Daily Revenue Health Check**` split between two
  // events leaves both halves un-bolded). Coalesce consecutive message events
  // into a single block so the markdown parser sees a complete string.
  const items: RenderedItem[] = [];
  let current: { seq: number; content: string } | null = null;
  for (const ev of events) {
    if (ev.type === "message") {
      const content = (ev.event as { content?: string } | null)?.content ?? "";
      if (current) {
        current.content += content;
      } else {
        current = { seq: ev.seq, content };
      }
      continue;
    }
    if (current) {
      items.push({ type: "message-block", seq: current.seq, content: current.content });
      current = null;
    }
    items.push({ type: "single", seq: ev.seq, ev });
  }
  if (current) {
    items.push({ type: "message-block", seq: current.seq, content: current.content });
  }
  return items;
}

function EventStream({ events }: { events: EventRow[] }) {
  const items = coalesceEvents(events);
  return (
    <ul className="list-none p-0 m-0 flex flex-col gap-1.5 text-ui-body border-l-2 border-border pl-3">
      {items.map((item) => {
        if (item.type === "message-block") {
          return (
            <li key={item.seq} className="run-evt-message text-text leading-[1.55]">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>
                {item.content}
              </ReactMarkdown>
            </li>
          );
        }
        const ev = item.ev;
        if (ev.type === "status") {
          const message = (ev.event as { message?: string } | null)?.message;
          return (
            <li key={ev.seq} className="text-text3 text-ui-body-sm italic">
              · {message}
            </li>
          );
        }
        if (ev.type === "output_emit") {
          const e = ev.event as { kind?: string } | null;
          return (
            <li key={ev.seq} className="text-text2 text-ui-body-sm">
              emitted output ({e?.kind ?? "unknown kind"})
            </li>
          );
        }
        if (ev.type === "action_request_emit") {
          const e = ev.event as
            | { kind?: string; risk_level?: string }
            | null;
          return (
            <li key={ev.seq} className="text-text2 text-ui-body-sm">
              proposed action: {e?.kind ?? "unknown"}
              {e?.risk_level ? ` (risk ${e.risk_level})` : ""}
            </li>
          );
        }
        if (ev.type === "needs_input") {
          const e = ev.event as { question?: string } | null;
          return (
            <li key={ev.seq} className="text-warn-ink text-ui-body-sm">
              paused for input: {e?.question}
            </li>
          );
        }
        if (ev.type === "error") {
          const e = ev.event as { message?: string } | null;
          return (
            <li key={ev.seq} className="text-danger text-ui-body-sm">
              error: {e?.message}
            </li>
          );
        }
        if (ev.type === "done") {
          return (
            <li key={ev.seq} className="text-text3 text-ui-caption font-mono">
              — done
            </li>
          );
        }
        return null;
      })}
    </ul>
  );
}

function Section({
  title,
  meta,
  children,
}: {
  title: string;
  meta?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section className="run-section">
      <header className="run-section-head">
        <h2>{title}</h2>
        {meta ? <span>{meta}</span> : null}
      </header>
      <div className="run-section-body">{children}</div>
    </section>
  );
}
