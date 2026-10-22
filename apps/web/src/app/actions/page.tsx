"use client";

import { Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import AppHeader from "@/components/AppHeader";
import CreatorCredit from "@/components/CreatorCredit";
import PageHeading from "@/components/PageHeading";
import SectionNav from "@/components/SectionNav";
import ActCard, {
  type ActCardData,
  type ActRowData,
  type ActRowTone,
} from "@/components/ActCard";
import { ActionChanges } from "@/components/ActionChanges";
import { Disclosure } from "@/components/ui/disclosure";
import { useApprovalDecisions } from "@/hooks/useApprovalDecisions";
import {
  actionOutcome,
  describeOutcome,
  systemForActionKind,
  type ActionOutcome,
} from "@/lib/action-outcome";
import { cn } from "@/lib/cn";
import { formatSavedShort } from "@/lib/hours-saved";
import { workflowDisplayName } from "@/lib/workflow-label";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty";
import { SearchInput } from "@/components/ui/search-input";
import { SkeletonList } from "@/components/ui/skeleton";
import { Tab, Tabs } from "@/components/ui/tabs";
import { matchesListSearch } from "@/lib/list-search";

type Filter = "awaiting" | "fired" | "failed" | "rejected" | "all";

type ActionRow = {
  id: string;
  workflowRunId: string | null;
  workflow: { id: string; name: string } | null;
  triggeredByObservation: { title: string } | null;
  kind: string;
  target: string | null;
  payload: unknown;
  riskLevel: string | null;
  summary: string | null;
  scope: string;
  status: string;
  minutesSaved: number | null;
  approvedAt: string | null;
  approverKind: "operator" | "policy" | "auto" | null;
  approverLabel: string | null;
  rejectionReason: string | null;
  failureReason: string | null;
  outcome: ActionOutcome;
  executionError: string | null;
  runAt: string;
  createdAt: string;
};

type ActionsPayload = {
  actions: ActionRow[];
  count: number;
  filter: Filter;
};

const TABS: Array<{ key: Filter; label: string }> = [
  { key: "awaiting", label: "Waiting for you" },
  { key: "fired", label: "Completed" },
  { key: "failed", label: "Failed" },
  { key: "rejected", label: "Rejected" },
  { key: "all", label: "All" },
];

function approverPhrase(
  kind: ActionRow["approverKind"],
  label: string | null,
): string | null {
  if (kind === "operator") return label ? `you · ${label}` : "you";
  if (kind === "policy") return label ? `rule "${label}"` : "a rule";
  if (kind === "auto") return "auto";
  return null;
}

function outcomeOf(row: ActionRow): ActionOutcome {
  return row.outcome ?? actionOutcome(row.status);
}

function rowToneFor(row: ActionRow): ActRowTone {
  const outcome = outcomeOf(row);
  if (outcome === "rejected" || outcome === "failed") return "action";
  if (outcome === "needs_check" || outcome === "partial") return "watch";
  if (row.status === "pending_approval") {
    if (row.riskLevel === "high" || row.riskLevel === "critical") return "action";
    return "watch";
  }
  return "good";
}

function stateFor(row: ActionRow): ActCardData["state"] {
  if (row.status === "pending_approval") return "awaiting";
  const outcome = outcomeOf(row);
  if (outcome === "rejected") return "rejected";
  if (outcome === "failed") return "failed";
  if (outcome === "needs_check" || outcome === "partial") return "needs_check";
  return "live";
}

function isFilter(value: string | null): value is Filter {
  return (
    value === "awaiting" ||
    value === "fired" ||
    value === "failed" ||
    value === "rejected" ||
    value === "all"
  );
}

type Group = {
  key: string;
  runId: string | null;
  runAt: string;
  trigger: string | null;
  workflowName: string;
  state: ActCardData["state"];
  rows: ActionRow[];
};

function groupActions(actions: ActionRow[]): Group[] {
  // Key groups by (runId, state) so a mixed-status run produces separate cards
  // with consistent badges. Within a group, rows stay time-ordered (API order).
  const map = new Map<string, Group>();
  for (const row of actions) {
    const state = stateFor(row);
    // Chat-proposed admin actions have no workflow run — group each alone.
    const key = `${row.workflowRunId ?? row.id}:${state}`;
    const existing = map.get(key);
    if (existing) {
      existing.rows.push(row);
    } else {
      map.set(key, {
        key,
        runId: row.workflowRunId,
        runAt: row.runAt,
        trigger: row.triggeredByObservation?.title ?? null,
        workflowName: workflowDisplayName(row.workflow),
        state,
        rows: [row],
      });
    }
  }
  return Array.from(map.values());
}

export default function ActionsPage() {
  return (
    <Suspense fallback={null}>
      <ActionsPageInner />
    </Suspense>
  );
}

function ActionsPageInner() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const initial = searchParams?.get("filter");
  const [filter, setFilter] = useState<Filter>(isFilter(initial) ? initial : "awaiting");
  const [data, setData] = useState<ActionsPayload | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [focusedId, setFocusedId] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const rowRefs = useRef<Record<string, HTMLLIElement | null>>({});

  const load = useCallback(async () => {
    try {
      const res = await fetch(`/api/approvals?filter=${filter}`, {
        cache: "no-store",
      });
      if (!res.ok) {
        setError(`Couldn't load (HTTP ${res.status})`);
        return;
      }
      const json = (await res.json()) as ActionsPayload;
      setData(json);
      if (filter === "awaiting" && !focusedId && json.actions[0]) {
        setFocusedId(json.actions[0].id);
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load");
    }
  }, [filter, focusedId]);

  const {
    hiddenIds,
    rejectingId,
    rejectReason,
    setRejectReason,
    approve,
    beginReject,
    cancelReject,
    submitReject,
  } = useApprovalDecisions(load);
  useEffect(() => {
    const initialLoadId = window.setTimeout(() => {
      void load();
    }, 0);
    return () => window.clearTimeout(initialLoadId);
  }, [load]);

  const switchFilter = useCallback((next: Filter) => {
    setFilter(next);
    setFocusedId(null);
    cancelReject();
    const url = new URL(window.location.href);
    if (next === "awaiting") url.searchParams.delete("filter");
    else url.searchParams.set("filter", next);
    window.history.replaceState({}, "", url.toString());
  }, [cancelReject]);

  const labelFor = useCallback(
    (id: string) => data?.actions.find((a) => a.id === id)?.summary ?? "",
    [data?.actions],
  );

  useEffect(() => {
    if (!focusedId) return;
    rowRefs.current[focusedId]?.scrollIntoView({
      behavior: "smooth",
      block: "nearest",
    });
  }, [focusedId]);

  const visibleActions = useMemo(
    () =>
      (data?.actions ?? []).filter((action) =>
        !hiddenIds.has(action.id) &&
        matchesListSearch(
          query,
          action.summary,
          action.kind,
          action.target,
          action.scope,
          action.status,
          action.workflow?.name,
          action.triggeredByObservation?.title,
        ),
      ),
    [data?.actions, hiddenIds, query],
  );
  const groups = useMemo(() => groupActions(visibleActions), [visibleActions]);
  const visibleFocusedId = visibleActions.some(
    (action) => action.id === focusedId,
  )
    ? focusedId
    : (visibleActions[0]?.id ?? null);

  return (
    <>
      <div className="root approvals-root">
        <AppHeader>
          <SectionNav current="actions" />
        </AppHeader>

        <PageHeading
          title="Approvals"
          description="Changes OpenNeko wants to make in your systems. Nothing runs until you approve it."
          meta={
            data && filter === "awaiting"
              ? `${data.count} pending`
              : undefined
          }
        />

        <div className="mb-5 max-w-[520px]">
          <SearchInput
            label="Search actions"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search actions, workflows, and targets"
          />
        </div>

        <Tabs aria-label="Approvals filter" className="mb-[18px]">
          {TABS.map((t) => {
            const active = filter === t.key;
            return (
              <Tab
                key={t.key}
                selected={active}
                onClick={() => switchFilter(t.key)}
              >
                {t.label}
              </Tab>
            );
          })}
        </Tabs>

        {error ? (
          <div className="py-[60px] text-center text-danger text-ui-body">{error}</div>
        ) : data === null ? (
          <SkeletonList rows={3} label="Loading approvals" className="max-w-[620px]" />
        ) : data.actions.length === 0 ? (
          <ActionsEmptyState filter={filter} onBack={() => router.push("/")} />
        ) : visibleActions.length === 0 ? (
          <EmptyState
            title="No matching actions"
            body="Try another action, workflow, target, or status."
          />
        ) : (
          <div className="triage-layout">
            <div className="act-list triage-queue">
            {groups.map((group, i) => {
              const cardData: ActCardData = {
                runId: group.runId,
                runAt: group.runAt,
                trigger: group.trigger,
                state: group.state,
                workflowName: group.workflowName,
                rows: group.rows.map<ActRowData>((r) => ({
                  id: r.id,
                  tone: rowToneFor(r),
                  headline: r.summary || r.kind,
                  detail: describeOutcome(outcomeOf(r), r.executionError ?? r.failureReason, systemForActionKind(r.kind)),
                  target: r.target,
                  kind: r.kind,
                  payload: r.payload,
                  rejectionReason:
                    r.status === "rejected" ? r.rejectionReason : null,
                  approverPhrase: approverPhrase(r.approverKind, r.approverLabel),
                  status: r.status,
                  minutesSaved: r.minutesSaved,
                })),
              };

              return (
                <ActCard
                  key={group.key}
                  data={cardData}
                  index={i}
                  focusedRowId={visibleFocusedId}
                  rejectingRowId={rejectingId}
                  rejectReason={rejectReason}
                  onRejectReasonChange={setRejectReason}
                  onCancelReject={cancelReject}
                  onSubmitReject={() => submitReject(rejectingId ? labelFor(rejectingId) : "")}
                  onFocusRow={setFocusedId}
                  onApproveRow={(id) => approve(id, labelFor(id))}
                  onBeginRejectRow={beginReject}
                  rowRef={(id, el) => {
                    rowRefs.current[id] = el;
                  }}
                />
              );
            })}
            </div>
            {filter === "awaiting" && (
              <ActionReadingPane
                action={
                  visibleActions.find((a) => a.id === visibleFocusedId) ?? null
                }
                busy={false}
                onApprove={() => {
                  if (visibleFocusedId) approve(visibleFocusedId, labelFor(visibleFocusedId));
                }}
                onReject={() => {
                  if (visibleFocusedId) beginReject(visibleFocusedId);
                }}
              />
            )}
          </div>
        )}
      </div>

      <CreatorCredit />
    </>
  );
}

const RISK_PILL: Record<string, string> = {
  critical: "bg-danger text-white",
  high: "bg-danger-soft text-danger border border-danger/30",
  medium: "bg-watch-soft text-warn-ink border border-watch/30",
  low: "bg-success-soft text-success-ink border border-success-mid/30",
};

// Reading pane for the triage queue (Compact). Shows the focused action's
// full context — why, target, payload, value — beside the queue list.
function ActionReadingPane({
  action,
  busy,
  onApprove,
  onReject,
}: {
  action: ActionRow | null;
  busy: boolean;
  onApprove: () => void;
  onReject: () => void;
}) {
  if (!action) {
    return (
      <aside className="triage-pane">
        <div className="bg-card border border-border rounded-2xl px-5 py-10 text-center text-ui-body-sm text-text3 shadow-soft">
          Select an action to review.
        </div>
      </aside>
    );
  }
  const risk = action.riskLevel ?? "low";
  return (
    <aside className="triage-pane">
      <div className="bg-card border border-border rounded-2xl px-5 py-[18px] shadow-soft">
        <div className="flex items-center gap-2.5 mb-2.5">
          <span className={cn("font-display text-ui-caption font-semibold px-2 py-0.5 rounded-full", RISK_PILL[risk] ?? RISK_PILL.low)}>
            {risk.charAt(0).toUpperCase() + risk.slice(1)} risk
          </span>
        </div>
        <h2 className="font-display text-ui-section font-extrabold tracking-[-0.02em] leading-[1.2] text-text">
          {action.summary || action.kind}
        </h2>
        <div className="text-ui-body-sm text-text2 mt-2 leading-[1.5]">
          Proposed by <span className="text-text font-semibold">{workflowDisplayName(action.workflow)}</span>
          {action.triggeredByObservation ? <> · triggered by “{action.triggeredByObservation.title}”</> : null}
        </div>

        {action.target && (
          <div className="mt-4">
            <div className="text-ui-caption font-semibold text-text3 mb-1.5">Target</div>
            <code className="font-mono text-ui-caption text-text2 break-all">{action.target}</code>
          </div>
        )}

        <ActionChanges payload={action.payload} className="mt-4" />

        <Disclosure title="Technical details" className="mt-4">
          <div className="grid gap-2 text-ui-caption text-text2">
            <div>
              Action <code className="font-mono text-text">{action.kind}</code>
            </div>
            <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-words rounded-[8px] bg-bg px-3 py-2.5 font-mono text-ui-caption text-text2">
              {JSON.stringify(action.payload, null, 2)}
            </pre>
          </div>
        </Disclosure>

        {(action.minutesSaved ?? 0) > 0 && (
          <div className="mt-4 text-ui-body-sm text-text2">
            Saves <span className="font-semibold text-success-ink">{formatSavedShort(action.minutesSaved as number)}</span> of manual effort.
          </div>
        )}

        <div className="flex items-center gap-2.5 mt-[18px] pt-4 border-t border-border">
          <Button
            variant="primary"
            disabled={busy}
            onClick={onApprove}
          >
            Approve
          </Button>
          <Button
            variant="danger"
            disabled={busy}
            onClick={onReject}
          >
            Reject
          </Button>
        </div>
      </div>
    </aside>
  );
}

function ActionsEmptyState({ filter, onBack }: { filter: Filter; onBack: () => void }) {
  const copy =
    filter === "awaiting"
      ? {
          line: "Nothing's waiting.",
          sub: "Anything that needs your decision will appear here.",
        }
      : filter === "fired"
        ? {
            line: "No completed actions yet.",
            sub: "Changes that ran and were confirmed in your systems appear here.",
          }
        : filter === "failed"
          ? {
              line: "Nothing failed.",
              sub: "Approved changes that did not run, or that OpenNeko could not confirm, appear here.",
            }
        : filter === "rejected"
          ? {
              line: "Nothing rejected.",
              sub: "Changes that someone declined appear here with the reason.",
            }
          : {
              line: "No actions yet.",
              sub: "Workflows haven't proposed anything yet. Once they do, the receipts live here.",
            };
  return (
    <EmptyState
      title={copy.line}
      body={copy.sub}
      className="py-20"
      action={
        <Button variant="ghost" size="sm" onClick={onBack}>
          Back to Briefing
        </Button>
      }
    />
  );
}
