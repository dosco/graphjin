"use client";

import { ItemAccessPanel } from "@/components/admin/ItemAccessPanel";
import Link from "next/link";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { ArrowUpRight, Plus, Trash2, X } from "lucide-react";
import { cn } from "@/lib/cn";
import { confirmDialog } from "@/components/ConfirmModal";
import { describeSchedule } from "@/lib/cron-english";
import { formatSavedShort } from "@/lib/hours-saved";
import { Sparkline } from "@/components/Sparkline";
import PageHeading from "@/components/PageHeading";
import { Button, IconButton } from "@/components/ui/button";
import { ActionGroup } from "@/components/ui/action-group";
import { Checkbox } from "@/components/ui/checkbox";
import { EmptyState } from "@/components/ui/empty";
import { Badge, type BadgeVariant } from "@/components/ui/badge";
import { SearchInput } from "@/components/ui/search-input";
import { matchesListSearch } from "@/lib/list-search";
import { WorkflowApiAccessPanel } from "./WorkflowApiAccessPanel";

type WorkflowListItem = {
  id: string;
  name: string;
  description: string;
  goal: string;
  enabled: boolean;
  status: string;
  cron: string | null;
  cronTimezone: string;
  cronEnabled: boolean;
  steps: { id: string; description: string }[];
  createdAt: string;
  updatedAt: string;
};

type Subscription = {
  id: string;
  sourceKind: "workflow_output" | "source_change" | "external_event";
  filter: Record<string, unknown>;
  enabled: boolean;
  debounceMs: number;
};

type RecentRun = {
  id: string;
  status: string;
  triggerKind: string;
  executionMode: "single" | "batch" | null;
  queueAttempts: number;
  progress: Record<string, unknown>;
  telemetry: {
    durations?: { queueMs?: number; wallMs?: number };
    counts?: { inference?: number; tools?: number };
    usage?: {
      totalTokens?: number;
      billedCostUsd?: number;
      estimatedCostUsd?: number;
      coverage?: string;
    };
  } | null;
  summary: string | null;
  error: string | null;
  startedAt: string | null;
  finishedAt: string | null;
  durationMs: number | null;
  createdAt: string;
};

type RecentAction = {
  id: string;
  workflowRunId: string;
  kind: string;
  target: string | null;
  status: string;
  riskLevel: string | null;
  summary: string | null;
  approvedAt: string | null;
  createdAt: string;
};

type DrawerPayload = {
  workflow: WorkflowListItem & {
    systemPromptOverlay: string;
    dailyRunBudget: number | null;
    runsToday: number;
    minutesSaved30d: number;
    createdByThreadId: string | null;
    createdByRunId: string | null;
  };
  subscriptions: Subscription[];
  recentRuns: RecentRun[];
  recentActions: RecentAction[];
  activitySparkline: number[];
};

type PolicySummary = {
  id: string;
  name: string;
  mode: string;
  riskThresholdAutoApprove: string | null;
};

function formatDuration(ms: number | null): string {
  if (ms == null) return "—";
  if (ms < 1000) return `${ms}ms`;
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  return `${Math.floor(s / 60)}m ${s % 60}s`;
}

function formatRelative(iso: string | null): string {
  if (!iso) return "";
  const t = new Date(iso).getTime();
  const diff = Date.now() - t;
  if (diff < 60_000) return "just now";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
  const days = Math.floor(diff / 86_400_000);
  if (days < 14) return `${days}d ago`;
  return new Date(iso).toLocaleDateString("en-IN", {
    month: "short",
    day: "numeric",
  });
}

function formatCompactNumber(value: number): string {
  return new Intl.NumberFormat("en", {
    notation: value >= 1_000 ? "compact" : "standard",
    maximumFractionDigits: 1,
  }).format(value);
}

function recentRunTelemetry(run: RecentRun): string | null {
  const parts: string[] = [];
  const tokens = run.telemetry?.usage?.totalTokens;
  if (typeof tokens === "number")
    parts.push(`${formatCompactNumber(tokens)} tokens`);
  const cost =
    run.telemetry?.usage?.billedCostUsd ??
    run.telemetry?.usage?.estimatedCostUsd;
  if (typeof cost === "number")
    parts.push(`$${cost.toFixed(cost < 0.01 ? 4 : 2)}`);
  if (run.triggerKind === "api" && run.queueAttempts > 1) {
    parts.push(`${run.queueAttempts} queue attempts`);
  }
  return parts.length ? parts.join(" · ") : null;
}

function describeSubscription(s: Subscription): string {
  if (s.sourceKind === "workflow_output") {
    const f = s.filter as {
      scope?: string;
      mood?: string[] | string;
      topic?: string;
    };
    const parts: string[] = [];
    if (f.scope) parts.push(`scoped '${f.scope}'`);
    if (f.topic) parts.push(`topic '${f.topic}'`);
    if (f.mood) {
      const moods = Array.isArray(f.mood) ? f.mood : [f.mood];
      parts.push(`mood ${moods.join(" or ")}`);
    }
    const tail = parts.length ? ` (${parts.join(", ")})` : "";
    return `Outputs from any workflow${tail}.`;
  }
  if (s.sourceKind === "source_change") {
    const f = s.filter as { table?: string; operation?: string };
    const tbl = f.table ?? "any table";
    const op = f.operation ? ` ${f.operation}s` : " changes";
    return `The ${tbl} table — watches for${op}.`;
  }
  const f = s.filter as { provider?: string; topic?: string };
  return `External events${f.provider ? ` from ${f.provider}` : ""}${
    f.topic ? ` on '${f.topic}'` : ""
  }.`;
}

export default function WorkflowsPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const selectedId = searchParams?.get("id") ?? null;

  const [workflows, setWorkflows] = useState<WorkflowListItem[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [sparklines, setSparklines] = useState<Record<string, number[]>>({});
  const [query, setQuery] = useState("");
  const inspectorRef = useRef<HTMLElement | null>(null);

  const fetchList = useCallback(async () => {
    try {
      const res = await fetch("/api/workflows", { cache: "no-store" });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      setError(null);
      setWorkflows(data.workflows ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load workflows");
    }
  }, []);

  useEffect(() => {
    const timer = window.setTimeout(() => void fetchList(), 0);
    return () => window.clearTimeout(timer);
  }, [fetchList]);

  const select = useCallback(
    (id: string | null) => {
      const params = new URLSearchParams(searchParams?.toString() ?? "");
      if (id) params.set("id", id);
      else params.delete("id");
      const qs = params.toString();
      router.replace(qs ? `/workflows?${qs}` : "/workflows", { scroll: false });
    },
    [router, searchParams],
  );

  const deleteWorkflow = useCallback(
    async (id: string, name: string) => {
      const ok = await confirmDialog({
        title: `Delete "${name}"?`,
        description:
          "This removes the workflow along with its triggers, run history, and proposed actions.",
        confirmLabel: "Delete",
        destructive: true,
      });
      if (!ok) return;
      const res = await fetch(`/api/workflows/${id}`, { method: "DELETE" });
      if (!res.ok) {
        setError(`Couldn't delete (HTTP ${res.status})`);
        return;
      }
      if (selectedId === id) select(null);
      setWorkflows((prev) => prev?.filter((w) => w.id !== id) ?? prev);
      void fetchList();
    },
    [selectedId, select, fetchList],
  );

  const grouped = useMemo(() => {
    const active: WorkflowListItem[] = [];
    const paused: WorkflowListItem[] = [];
    const broken: WorkflowListItem[] = [];
    for (const w of workflows ?? []) {
      if (
        !matchesListSearch(
          query,
          w.name,
          w.description,
          w.goal,
          w.status,
          w.steps.map((step) => step.description).join(" "),
        )
      ) {
        continue;
      }
      if (w.status === "broken") broken.push(w);
      else if (!w.enabled) paused.push(w);
      else active.push(w);
    }
    return { active, paused, broken };
  }, [query, workflows]);

  const recordSparkline = useCallback((id: string, values: number[]) => {
    setSparklines((prev) =>
      prev[id]?.length === values.length &&
      prev[id]?.every((v, i) => v === values[i])
        ? prev
        : { ...prev, [id]: values },
    );
  }, []);

  const totalCount =
    grouped.active.length + grouped.paused.length + grouped.broken.length;
  const visibleWorkflows = [
    ...grouped.active,
    ...grouped.paused,
    ...grouped.broken,
  ];
  const selectedWorkflow =
    visibleWorkflows.find((workflow) => workflow.id === selectedId) ?? null;

  useEffect(() => {
    if (!selectedId) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") select(null);
    };
    document.addEventListener("keydown", onKeyDown);

    const isSheet = window.matchMedia("(max-width: 900px)").matches;
    const previousOverflow = document.body.style.overflow;
    let focusFrame = 0;
    if (isSheet) {
      document.body.style.overflow = "hidden";
      focusFrame = requestAnimationFrame(() => inspectorRef.current?.focus());
    }

    return () => {
      document.removeEventListener("keydown", onKeyDown);
      if (isSheet) document.body.style.overflow = previousOverflow;
      if (focusFrame) cancelAnimationFrame(focusFrame);
    };
  }, [selectedId, select]);

  return (
    <>
      <PageHeading
        title="Workflows"
        description="Automations that monitor your systems, surface findings, and propose actions."
        actions={
          <div className="workflows-ops-status" aria-label="Workflow status">
            <div>
              <strong>{grouped.active.length}</strong>
              <span>active</span>
            </div>
            <div>
              <strong>{grouped.paused.length}</strong>
              <span>paused</span>
            </div>
            {grouped.broken.length > 0 ? (
              <div data-state="attention">
                <strong>{grouped.broken.length}</strong>
                <span>need attention</span>
              </div>
            ) : null}
            <Button
              variant="primary"
              onClick={() =>
                router.push(
                  `/work?seed=${encodeURIComponent("Set up a new workflow that ")}`,
                )
              }
            >
              <Plus aria-hidden="true" />
              New workflow
            </Button>
          </div>
        }
      />

      <div className="workflows-search">
        <SearchInput
          label="Search workflows"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Search workflows and steps"
        />
      </div>

      {error ? (
        <div className="workflow-page-state is-error" role="alert">
          <strong>Workflows could not be loaded</strong>
          <span>{error}</span>
          <Button size="sm" onClick={() => void fetchList()}>
            Retry
          </Button>
        </div>
      ) : workflows === null ? (
        <div className="workflow-page-state" role="status">
          <span className="workflow-loading-line is-wide" />
          <span className="workflow-loading-line" />
          <span className="workflow-loading-line is-short" />
        </div>
      ) : workflows.length === 0 || (query && totalCount === 0) ? (
        <EmptyState
          className="workflow-page-state is-empty"
          title={query ? "No matching workflows" : "No workflows"}
          body={
            query
              ? "Try another workflow name, status, goal, or step."
              : "Ask OpenNeko to create the first recurring task."
          }
          action={
            query ? null : (
              <Button
                variant="primary"
                onClick={() =>
                  router.push(
                    `/work?seed=${encodeURIComponent("Set up a new workflow that ")}`,
                  )
                }
              >
                Create a workflow
              </Button>
            )
          }
        />
      ) : (
        <div
          className={cn(
            "workflows-workspace",
            selectedWorkflow ? "has-selection" : "is-overview",
          )}
        >
          <div className="workflows-master">
            {grouped.active.length > 0 && (
              <WorkflowGroup
                title="Active"
                count={grouped.active.length}
                items={grouped.active}
                selectedId={selectedId}
                onSelect={select}
                onDelete={deleteWorkflow}
                sparklines={sparklines}
                onSparkline={recordSparkline}
              />
            )}
            {grouped.paused.length > 0 && (
              <WorkflowGroup
                title="Paused"
                count={grouped.paused.length}
                items={grouped.paused}
                selectedId={selectedId}
                onSelect={select}
                onDelete={deleteWorkflow}
                sparklines={sparklines}
                onSparkline={recordSparkline}
              />
            )}
            {grouped.broken.length > 0 && (
              <WorkflowGroup
                title="Needs attention"
                count={grouped.broken.length}
                items={grouped.broken}
                selectedId={selectedId}
                onSelect={select}
                onDelete={deleteWorkflow}
                sparklines={sparklines}
                onSparkline={recordSparkline}
              />
            )}
          </div>

          {selectedWorkflow ? (
            <>
              <button
                type="button"
                className="workflow-inspector-scrim"
                data-ui-bespoke-reason="Modal sheet scrim dismisses workflow details without presenting a visible control"
                aria-label="Close workflow details"
                onClick={() => select(null)}
              />
              <aside
                key={selectedWorkflow.id}
                ref={inspectorRef}
                id="workflow-inspector"
                className="workflow-inspector"
                aria-labelledby="workflow-inspector-title"
                tabIndex={-1}
                data-state={
                  selectedWorkflow.status === "broken"
                    ? "broken"
                    : selectedWorkflow.enabled
                      ? "active"
                      : "paused"
                }
              >
                <header className="workflow-inspector-head">
                  <div className="workflow-inspector-heading">
                    <span
                      className="workflow-inspector-state"
                      data-state={
                        selectedWorkflow.status === "broken"
                          ? "broken"
                          : selectedWorkflow.enabled
                            ? "active"
                            : "paused"
                      }
                    >
                      {selectedWorkflow.status === "broken"
                        ? "Needs attention"
                        : selectedWorkflow.enabled
                          ? "Active"
                          : "Paused"}
                    </span>
                    <h2 id="workflow-inspector-title">
                      {selectedWorkflow.name}
                    </h2>
                    <p>
                      {describeSchedule(
                        selectedWorkflow.cron,
                        selectedWorkflow.cronTimezone,
                        selectedWorkflow.cronEnabled,
                      )}
                    </p>
                  </div>
                  <IconButton
                    label="Close workflow controls"
                    size="icon"
                    className="self-start"
                    onClick={() => select(null)}
                  >
                    <X aria-hidden="true" />
                  </IconButton>
                </header>
                <WorkflowDetail
                  key={selectedWorkflow.id}
                  workflowId={selectedWorkflow.id}
                  onMutated={fetchList}
                />
              </aside>
            </>
          ) : null}
        </div>
      )}
    </>
  );
}

function WorkflowGroup({
  title,
  count,
  items,
  selectedId,
  onSelect,
  onDelete,
  sparklines,
  onSparkline,
}: {
  title: string;
  count: number;
  items: WorkflowListItem[];
  selectedId: string | null;
  onSelect: (id: string | null) => void;
  onDelete: (id: string, name: string) => void;
  sparklines: Record<string, number[]>;
  onSparkline: (id: string, values: number[]) => void;
}) {
  return (
    <section className="workflow-group">
      <div className="workflow-group-head">
        {title} <span>{count}</span>
      </div>
      <ul className="wf-grid">
        {items.map((w) => (
          <WorkflowRow
            key={w.id}
            w={w}
            active={selectedId === w.id}
            onSelect={() => onSelect(selectedId === w.id ? null : w.id)}
            onDelete={() => onDelete(w.id, w.name)}
            sparkline={sparklines[w.id]}
            onSparkline={(values) => onSparkline(w.id, values)}
          />
        ))}
      </ul>
    </section>
  );
}

function WorkflowRow({
  w,
  active,
  onSelect,
  onDelete,
  sparkline,
  onSparkline,
}: {
  w: WorkflowListItem;
  active: boolean;
  onSelect: () => void;
  onDelete: () => void;
  sparkline: number[] | undefined;
  onSparkline: (values: number[]) => void;
}) {
  // Lazy-load the per-workflow sparkline once when first rendered.
  useEffect(() => {
    if (sparkline) return;
    let cancelled = false;
    void fetch(`/api/workflows/${w.id}`, { cache: "no-store" })
      .then((r) => r.json())
      .then((data: DrawerPayload) => {
        if (cancelled) return;
        if (Array.isArray(data?.activitySparkline)) {
          onSparkline(data.activitySparkline);
        }
      })
      .catch(() => {
        if (cancelled) return;
        onSparkline([]);
      });
    return () => {
      cancelled = true;
    };
  }, [w.id, sparkline, onSparkline]);

  const hasActivity = sparkline?.some((v) => v > 0) ?? false;

  return (
    <li
      data-state={
        w.status === "broken" ? "broken" : w.enabled ? "active" : "paused"
      }
    >
      <div className={`workflows-row${active ? " is-active" : ""}`}>
        <span className="workflows-route-node" aria-hidden="true" />
        <button
          type="button"
          className="workflows-row-main"
          data-ui-bespoke-reason="Master-detail row selection needs aria-pressed and controls the adjacent workflow inspector"
          onClick={onSelect}
          aria-pressed={active}
          aria-controls={active ? "workflow-inspector" : undefined}
        >
          <div className="workflows-row-copy">
            <div className="workflows-row-title">
              <span>{w.name}</span>
              <ArrowUpRight
                className="workflows-row-arrow"
                aria-hidden="true"
              />
            </div>
            {w.description && (
              <div className="text-ui-body-sm text-text2 mt-1 leading-[1.45]">
                {w.description}
              </div>
            )}
            <div className="workflows-row-meta">
              <span>
                {describeSchedule(w.cron, w.cronTimezone, w.cronEnabled)}
              </span>
              {hasActivity && (
                <>
                  <span className="opacity-60">·</span>
                  <Sparkline values={sparkline ?? []} />
                </>
              )}
            </div>
          </div>
        </button>
        <IconButton
          label={`Delete ${w.name}`}
          size="icon-sm"
          variant="danger"
          className="workflows-row-delete"
          title="Delete workflow"
          onClick={(event) => {
            event.stopPropagation();
            onDelete();
          }}
        >
          <Trash2 aria-hidden="true" strokeWidth={2} />
        </IconButton>
      </div>
    </li>
  );
}

function WorkflowDetail({
  workflowId,
  onMutated,
}: {
  workflowId: string;
  onMutated: () => void;
}) {
  const router = useRouter();
  const [data, setData] = useState<DrawerPayload | null>(null);
  const [policies, setPolicies] = useState<PolicySummary[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await fetch(`/api/workflows/${workflowId}`, {
        cache: "no-store",
      });
      if (!res.ok) {
        setError(`Couldn't load (HTTP ${res.status})`);
        return;
      }
      const json = (await res.json()) as DrawerPayload;
      setError(null);
      setData(json);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load");
    }
  }, [workflowId]);

  useEffect(() => {
    const timer = window.setTimeout(() => void load(), 0);
    return () => window.clearTimeout(timer);
  }, [load]);

  useEffect(() => {
    // Policies are org-level today (no per-workflow override). Fetch the
    // active set so the drawer's Policy section reflects what's actually
    // gating actions for this org.
    void fetch("/api/policies", { cache: "no-store" })
      .then((r) => r.json())
      .then((d: { policies: PolicySummary[] }) =>
        setPolicies(
          (d.policies ?? []).filter(
            (p) => (p as unknown as { enabled?: boolean }).enabled !== false,
          ),
        ),
      )
      .catch(() => setPolicies([]));
  }, []);

  const togglePause = useCallback(async () => {
    if (!data) return;
    setBusy(true);
    try {
      await fetch(`/api/workflows/${workflowId}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ enabled: !data.workflow.enabled }),
      });
      await load();
      onMutated();
    } finally {
      setBusy(false);
    }
  }, [data, workflowId, load, onMutated]);

  // OL7: park until next UTC midnight; the worker's cron sweep re-enables.
  const pauseForToday = useCallback(async () => {
    if (!data) return;
    setBusy(true);
    try {
      await fetch(`/api/workflows/${workflowId}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ pauseForToday: true }),
      });
      await load();
      onMutated();
    } finally {
      setBusy(false);
    }
  }, [data, workflowId, load, onMutated]);

  const toggleCron = useCallback(async () => {
    if (!data) return;
    setBusy(true);
    try {
      await fetch(`/api/workflows/${workflowId}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ cronEnabled: !data.workflow.cronEnabled }),
      });
      await load();
      onMutated();
    } finally {
      setBusy(false);
    }
  }, [data, workflowId, load, onMutated]);

  const runNow = useCallback(async () => {
    setBusy(true);
    try {
      const res = await fetch(`/api/workflows/${workflowId}/runs`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({}),
      });
      if (!res.ok) {
        setError(`Couldn't run (HTTP ${res.status})`);
        return;
      }
      const json = await res.json();
      if (json.workflowRunId) {
        router.push(`/runs/${json.workflowRunId}`);
      } else if (json.threadId) {
        router.push(`/work/${json.threadId}`);
      } else {
        await load();
      }
    } finally {
      setBusy(false);
    }
  }, [workflowId, router, load]);

  if (error) {
    return (
      <div className="workflow-detail">
        <div className="workflow-detail-state is-error" role="alert">
          <strong>Details could not be loaded</strong>
          <span>{error}</span>
          <Button size="sm" onClick={() => void load()}>
            Retry
          </Button>
        </div>
      </div>
    );
  }

  if (!data) {
    return (
      <div className="workflow-detail">
        <div className="workflow-detail-state" role="status">
          <span className="workflow-loading-line is-wide" />
          <span className="workflow-loading-line" />
          <span className="workflow-loading-line is-short" />
        </div>
      </div>
    );
  }

  const { workflow, subscriptions, recentRuns, recentActions } = data;
  const budgetUsed = workflow.runsToday;
  const budgetCap = workflow.dailyRunBudget;
  const budgetPct = budgetCap ? Math.round((budgetUsed / budgetCap) * 100) : 0;

  return (
    <div className="workflow-detail">
      <ActionGroup className="px-[var(--panel-padding)] pb-5">
        <Button
          variant="primary"
          onClick={runNow}
          disabled={busy || !workflow.enabled}
          title={
            !workflow.enabled
              ? "Resume the workflow first"
              : "Manually run this workflow now"
          }
        >
          Run now
        </Button>
        <Button onClick={togglePause} disabled={busy}>
          {workflow.enabled ? "Pause" : "Resume"}
        </Button>
        {workflow.enabled && (
          <Button
            onClick={pauseForToday}
            disabled={busy}
            title="Pause until midnight UTC; resumes automatically"
          >
            Pause for today
          </Button>
        )}
      </ActionGroup>

      {workflow.minutesSaved30d > 0 && (
        <Section title="Hours saved (30d)">
          <p className="workflow-impact">
            <span className="workflow-impact-value">
              {formatSavedShort(workflow.minutesSaved30d)}
            </span>{" "}
            <span>
              of human time, estimated across this workflow&apos;s runs and
              actions.
            </span>
          </p>
        </Section>
      )}

      {workflow.description && (
        <Section title="Description">
          <p className="leading-[1.55]">{workflow.description}</p>
        </Section>
      )}

      {workflow.goal && (
        <Section title="Goal">
          <p className="leading-[1.55]">{workflow.goal}</p>
        </Section>
      )}

      {workflow.steps?.some((s) => s.description?.trim()) && (
        <Section title="Steps">
          <ol className="workflow-steps">
            {workflow.steps
              .filter((s) => s.description?.trim())
              .map((step, i) => (
                <li key={step.id ?? i}>{step.description}</li>
              ))}
          </ol>
        </Section>
      )}

      <Section title="Watches">
        {subscriptions.length === 0 ? (
          <p className="text-text3">
            No subscriptions yet. Add one by editing this workflow.
          </p>
        ) : (
          <ul className="list-none p-0 m-0 flex flex-col gap-1.5">
            {subscriptions.map((s) => (
              <li
                key={s.id}
                className="flex items-baseline gap-2 leading-[1.45]"
              >
                <span
                  className={cn(
                    "inline-block w-1.5 h-1.5 rounded-full flex-none -translate-y-px",
                    s.enabled ? "bg-success-mid" : "bg-text3",
                  )}
                  aria-hidden="true"
                />
                {describeSubscription(s)}
              </li>
            ))}
          </ul>
        )}
      </Section>

      <Section title="Schedule">
        <div className="flex items-center justify-between gap-3 text-ui-body-sm">
          <span>
            {describeSchedule(
              workflow.cron,
              workflow.cronTimezone,
              workflow.cronEnabled,
            )}
          </span>
          {workflow.cron && (
            <Checkbox
              checked={workflow.cronEnabled}
              onCheckedChange={() => void toggleCron()}
              disabled={busy}
              label={workflow.cronEnabled ? "Enabled" : "Disabled"}
              className="text-xs text-text3"
            />
          )}
        </div>
      </Section>

      <WorkflowApiAccessPanel workflowId={workflowId} />

      <Section title="Daily budget">
        <div className="workflow-budget">
          {budgetCap == null ? (
            <span className="text-text3">No cap set</span>
          ) : (
            <>
              <div className="workflow-budget-copy">
                <span>
                  {budgetUsed} / {budgetCap} runs used today
                </span>
                <strong data-state={budgetPct >= 80 ? "watch" : "normal"}>
                  {budgetPct}%
                </strong>
              </div>
              <div className="workflow-budget-track" aria-hidden="true">
                <span style={{ width: `${Math.min(100, budgetPct)}%` }} />
              </div>
            </>
          )}
        </div>
      </Section>

      <Section title="Rules">
        {policies === null ? (
          <p className="text-text3">Loading…</p>
        ) : policies.length === 0 ? (
          <p className="text-text3">
            No rules set. Actions will require approval by default.
          </p>
        ) : (
          <ul className="list-none p-0 mt-0 mb-1.5 flex flex-col gap-1.5">
            {policies.map((p) => (
              <li
                key={p.id}
                className="flex min-w-0 items-start gap-2 text-ui-body-sm"
              >
                <span className="min-w-0 flex-1 font-mono text-ui-caption text-text2 [overflow-wrap:anywhere]">
                  {p.name}
                </span>
                <Badge variant={policyModeVariant(p.mode)} className="ml-auto">
                  {describePolicyMode(p.mode)}
                </Badge>
              </li>
            ))}
          </ul>
        )}
        <Link
          className="inline-block mt-1 text-xs text-accent no-underline hover:underline hover:underline-offset-2"
          href="/admin/rules"
        >
          See all rules
        </Link>
      </Section>

      <Section title="Recent runs">
        {recentRuns.length === 0 ? (
          <p className="text-text3">No runs yet.</p>
        ) : (
          <ul className="list-none p-0 m-0 flex flex-col gap-1.5">
            {recentRuns.map((r) => (
              <li
                key={r.id}
                className="flex items-baseline gap-2 text-ui-body-sm"
              >
                <Button
                  variant="ghost"
                  size="sm"
                  className="workflow-drawer-run-link"
                  onClick={() => router.push(`/runs/${r.id}`)}
                >
                  <span
                    className={cn(
                      "inline-block w-3.5 text-center",
                      runStatusColor(r.status),
                    )}
                  >
                    {statusGlyph(r.status)}
                  </span>
                  <span className="workflow-drawer-run-meta text-xs tabular-nums text-text2">
                    {formatRelative(r.createdAt)} · {r.triggerKind}
                    {r.executionMode ? ` ${r.executionMode}` : ""} ·{" "}
                    {formatDuration(r.durationMs)}
                    {recentRunTelemetry(r) ? ` · ${recentRunTelemetry(r)}` : ""}
                  </span>
                  {r.executionMode ? (
                    <Badge
                      variant={
                        r.executionMode === "batch" ? "success" : "muted"
                      }
                    >
                      {r.executionMode}
                    </Badge>
                  ) : null}
                  <span
                    className="workflow-drawer-run-arrow ml-auto text-text3 text-ui-caption transition-[color,transform] duration-[0.18s]"
                    aria-hidden="true"
                  >
                    →
                  </span>
                </Button>
              </li>
            ))}
          </ul>
        )}
      </Section>

      <Section title="Recent actions">
        {recentActions.length === 0 ? (
          <p className="text-text3">No actions proposed yet.</p>
        ) : (
          <ul className="list-none p-0 m-0 flex flex-col gap-1.5">
            {recentActions.map((a) => (
              <li key={a.id} className="workflow-action-row">
                <div className="workflow-action-head">
                  <span className="workflow-action-kind">{a.kind}</span>
                  {a.target && (
                    <span className="workflow-action-target">{a.target}</span>
                  )}
                  <Badge
                    variant={actionPillVariant(a.status)}
                    className="ml-auto"
                  >
                    {actionStatusLabel(a.status)}
                  </Badge>
                </div>
                <div className="workflow-action-meta">
                  {formatRelative(a.createdAt)}
                  {a.summary ? ` · ${a.summary}` : ""}
                </div>
              </li>
            ))}
          </ul>
        )}
      </Section>

      <div className="workflow-detail-foot">
        {workflow.createdByThreadId ? (
          <Button
            size="sm"
            variant="ghost"
            className="bg-transparent border-0 text-text3 cursor-pointer text-xs font-semibold py-1 px-0 hover:text-accent hover:underline hover:underline-offset-[3px]"
            onClick={() => router.push(`/work/${workflow.createdByThreadId}`)}
          >
            View conversation
          </Button>
        ) : (
          <span />
        )}
        <Button
          size="sm"
          variant="ghost"
          className="bg-transparent border-0 text-accent cursor-pointer text-xs font-semibold py-1 px-0 hover:underline hover:underline-offset-[3px]"
          onClick={() =>
            router.push(
              `/work?seed=${encodeURIComponent(
                `Update the '${workflow.name}' workflow to `,
              )}`,
            )
          }
        >
          Edit with OpenNeko
        </Button>
      </div>
      <div className="px-[var(--panel-padding)] pb-5">
        <ItemAccessPanel itemType="workflow" itemId={workflow.id} label={workflow.name} />
      </div>
    </div>
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
    <section className="workflow-detail-section">
      <h3>{title}</h3>
      <div className="workflow-detail-section-body">{children}</div>
    </section>
  );
}

function policyModeVariant(mode: string): BadgeVariant {
  switch (mode) {
    case "auto_approve":
      return "success";
    case "approval_required":
      return "watch";
    case "never":
      return "danger";
    default:
      return "muted";
  }
}

function runStatusColor(status: string): string {
  switch (status) {
    case "completed":
      return "text-success-mid";
    case "failed":
      return "text-danger";
    case "needs_input":
    case "waiting_approval":
      return "text-watch";
    default:
      return "text-text3";
  }
}

function actionPillVariant(status: string): BadgeVariant {
  switch (status) {
    case "executed":
      return "success";
    case "pending_approval":
      return "watch";
    case "rejected":
    case "failed":
      return "danger";
    default:
      return "muted";
  }
}

function statusGlyph(status: string): string {
  switch (status) {
    case "completed":
      return "✓";
    case "running":
    case "queued":
      return "…";
    case "failed":
      return "✗";
    case "cancelled":
      return "—";
    case "needs_input":
    case "waiting_approval":
      return "?";
    default:
      return "·";
  }
}

function describePolicyMode(mode: string): string {
  switch (mode) {
    case "auto_approve":
      return "auto-approves";
    case "approval_required":
      return "requires approval";
    case "observe_only":
      return "observes only";
    case "draft_only":
      return "drafts only";
    case "never":
      return "never executes";
    default:
      return mode.replace(/_/g, " ");
  }
}

function actionStatusLabel(status: string): string {
  switch (status) {
    case "pending_approval":
      return "pending";
    case "executed":
      return "executed";
    case "rejected":
      return "rejected";
    case "failed":
      return "failed";
    case "approved":
      return "approved";
    case "expired":
      return "expired";
    default:
      return status;
  }
}
