"use client";

import { Suspense, useCallback, useEffect, useMemo, useState } from "react";
import { SkeletonList } from "@/components/ui/skeleton";
import { useRouter, useSearchParams } from "next/navigation";
import AppHeader from "@/components/AppHeader";
import CreatorCredit from "@/components/CreatorCredit";
import PageHeading from "@/components/PageHeading";
import SectionNav from "@/components/SectionNav";
import { Button } from "@/components/ui/button";
import { Badge, type BadgeVariant } from "@/components/ui/badge";
import { SearchInput } from "@/components/ui/search-input";
import { Segment, SegmentedControl } from "@/components/ui/tabs";
import { matchesListSearch } from "@/lib/list-search";

type StatusFilter = "active" | "completed" | "failed" | "all";

type WorkflowRunRow = {
  id: string;
  workflow: { id: string; name: string };
  triggerKind: string;
  executionMode: "single" | "batch" | null;
  chainDepth: number;
  status: string;
  summary: string | null;
  error: string | null;
  startedAt: string | null;
  finishedAt: string | null;
  durationMs: number | null;
  createdAt: string;
  updatedAt: string;
  outputCount: number;
  actionCount: number;
  pendingActionCount: number;
};

type RunsPayload = {
  runs: WorkflowRunRow[];
  status: StatusFilter;
};

const TABS: Array<{ key: StatusFilter; label: string }> = [
  { key: "all", label: "All" },
  { key: "active", label: "Active" },
  { key: "completed", label: "Completed" },
  { key: "failed", label: "Failed" },
];

function isStatusFilter(value: string | null): value is StatusFilter {
  return (
    value === "active" ||
    value === "completed" ||
    value === "failed" ||
    value === "all"
  );
}

function formatRelative(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime();
  const min = Math.max(0, Math.round(ms / 60000));
  if (min < 1) return "just now";
  if (min < 60) return `${min}m ago`;
  const hours = Math.round(min / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.round(hours / 24);
  return `${days}d ago`;
}

const RUNS_PAGE_SIZE = 25;

function formatDuration(ms: number | null): string {
  if (ms == null) return "running";
  if (ms < 1000) return "<1s";
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  return `${Math.floor(s / 60)}m ${s % 60}s`;
}

function statusLabel(status: string): string {
  return status.replace(/_/g, " ");
}

function statusVariant(status: string): BadgeVariant {
  switch (status) {
    case "completed":
      return "success";
    case "failed":
    case "cancelled":
      return "danger";
    case "queued":
    case "running":
    case "needs_input":
    case "waiting_approval":
      return "watch";
    default:
      return "muted";
  }
}

// Summaries are agent Markdown; the list card shows them as plain text.
function plainSummary(text: string): string {
  return text
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/`([^`]*)`/g, "$1")
    .replace(/(\*\*|__)(.*?)\1/g, "$2")
    .replace(/^\s{0,3}(#{1,6}|[-*+]|\d+\.)\s+/gm, "")
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/\s+/g, " ")
    .trim();
}

function describeRun(run: WorkflowRunRow): string {
  if (run.error) return run.error;
  if (run.summary?.trim()) return plainSummary(run.summary);
  if (run.outputCount > 0 || run.actionCount > 0) {
    const parts = [];
    if (run.outputCount > 0) {
      parts.push(
        `${run.outputCount} ${run.outputCount === 1 ? "finding" : "findings"}`,
      );
    }
    if (run.actionCount > 0) {
      parts.push(
        `${run.actionCount} ${run.actionCount === 1 ? "action" : "actions"}`,
      );
    }
    return parts.join(" · ");
  }
  if (run.status === "completed") return "Looked at the data; nothing to flag.";
  return "Run is still collecting context.";
}

export default function RunsPage() {
  return (
    <Suspense fallback={null}>
      <RunsPageInner />
    </Suspense>
  );
}

function RunsPageInner() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const initial = searchParams?.get("status");
  const [filter, setFilter] = useState<StatusFilter>(
    isStatusFilter(initial) ? initial : "all",
  );
  const [data, setData] = useState<RunsPayload | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [shown, setShown] = useState(RUNS_PAGE_SIZE);

  const load = useCallback(async () => {
    try {
      const res = await fetch(`/api/workflow-runs?status=${filter}`, {
        cache: "no-store",
      });
      if (!res.ok) {
        setError(`Couldn't load runs (HTTP ${res.status})`);
        return;
      }
      setData((await res.json()) as RunsPayload);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load runs");
    }
  }, [filter]);

  useEffect(() => {
    const initialLoadId = window.setTimeout(() => void load(), 0);
    return () => window.clearTimeout(initialLoadId);
  }, [load]);

  useEffect(() => {
    const active = data?.runs.some((r) =>
      ["queued", "running", "needs_input", "waiting_approval"].includes(
        r.status,
      ),
    );
    if (!active) return;
    const id = setInterval(() => void load(), 3000);
    return () => clearInterval(id);
  }, [data?.runs, load]);

  const switchFilter = useCallback((next: StatusFilter) => {
    setFilter(next);
    setShown(RUNS_PAGE_SIZE);
    const url = new URL(window.location.href);
    if (next === "all") url.searchParams.delete("status");
    else url.searchParams.set("status", next);
    window.history.replaceState({}, "", url.toString());
  }, []);
  const visibleRuns = useMemo(
    () =>
      (data?.runs ?? []).filter((run) =>
        matchesListSearch(
          query,
          run.workflow.name,
          run.triggerKind,
          run.executionMode,
          run.status,
          run.summary,
          run.error,
        ),
      ),
    [data?.runs, query],
  );

  return (
    <>
      <div className="root">
        <AppHeader back={{ href: "/workflows", label: "Workflows" }}>
          <SectionNav current="workflows" />
        </AppHeader>

        <PageHeading
          title="Run history"
          description="Inspect recent workflow runs, findings, and proposed actions."
          meta={data ? `${visibleRuns.length} shown` : undefined}
        />

        <div className="mb-5 max-w-[520px]">
          <SearchInput
            label="Search run history"
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setShown(RUNS_PAGE_SIZE);
            }}
            placeholder="Search workflows, triggers, and results"
          />
        </div>

        <SegmentedControl aria-label="Run status filter" className="mb-5">
          {TABS.map((tab) => (
            <Segment
              key={tab.key}
              selected={filter === tab.key}
              onClick={() => switchFilter(tab.key)}
            >
              {tab.label}
            </Segment>
          ))}
        </SegmentedControl>

        {error ? (
          <div className="py-[50px] text-center text-sm text-danger">
            {error}
          </div>
        ) : !data ? (
          <SkeletonList rows={5} variant="row" label="Loading runs" />
        ) : visibleRuns.length === 0 ? (
          <div className="py-[50px] text-center text-sm text-text3">
            {query ? "No runs match this search." : "No workflow runs yet."}
          </div>
        ) : (
          <ul className="list-none p-0 m-0 flex flex-col gap-2.5">
            {visibleRuns.slice(0, shown).map((run) => (
              <li key={run.id}>
                <Button
                  variant="secondary"
                  size="md"
                  className="!min-h-0 w-full !items-stretch !justify-start !whitespace-normal !rounded-2xl px-4 py-3.5 text-left hover:shadow-soft"
                  onClick={() => router.push(`/runs/${run.id}`)}
                >
                  <div className="flex w-full min-w-0 flex-1 flex-col gap-3">
                    <div className="flex items-start justify-between gap-3">
                      <div className="min-w-0">
                        <div className="flex items-center gap-2 flex-wrap">
                          <span className="font-semibold text-text">
                            {run.workflow.name}
                          </span>
                          <span className="font-mono text-ui-caption text-text3">
                            {run.triggerKind}
                            {run.chainDepth > 0
                              ? ` · chain ${run.chainDepth}`
                              : ""}
                          </span>
                          {run.triggerKind === "api" && run.executionMode ? (
                            <Badge
                              variant={
                                run.executionMode === "batch"
                                  ? "success"
                                  : "muted"
                              }
                            >
                              {run.executionMode}
                            </Badge>
                          ) : null}
                        </div>
                        <p className="mt-1.5 mb-0 text-ui-body-sm leading-[1.45] text-text2 line-clamp-2">
                          {describeRun(run)}
                        </p>
                      </div>
                      <Badge variant={statusVariant(run.status)}>
                        {statusLabel(run.status)}
                      </Badge>
                    </div>
                    <div className="flex flex-wrap items-center gap-1.5 text-ui-caption text-text3">
                      <span className="tabular-nums">
                        {formatRelative(run.createdAt)}
                      </span>
                      <span className="text-text3/70">·</span>
                      <span className="tabular-nums">
                        {formatDuration(run.durationMs)}
                      </span>
                      <span className="text-text3/70">·</span>
                      <span>
                        {run.outputCount}{" "}
                        {run.outputCount === 1 ? "finding" : "findings"}
                      </span>
                      <span className="text-text3/70">·</span>
                      <span>
                        {run.actionCount}{" "}
                        {run.actionCount === 1 ? "action" : "actions"}
                      </span>
                      {run.pendingActionCount > 0 && (
                        <>
                          <span className="text-text3/70">·</span>
                          <span className="text-warn-ink font-semibold">
                            {run.pendingActionCount} awaiting you
                          </span>
                        </>
                      )}
                      <span className="ml-auto text-ui-caption text-text3">
                        →
                      </span>
                    </div>
                  </div>
                </Button>
              </li>
            ))}
          </ul>
        )}
        {data && visibleRuns.length > shown ? (
          <div className="mt-4 flex justify-center">
            <Button
              size="sm"
              onClick={() => setShown((current) => current + RUNS_PAGE_SIZE)}
            >
              Show {Math.min(RUNS_PAGE_SIZE, visibleRuns.length - shown)} more runs
            </Button>
          </div>
        ) : null}
      </div>
      <CreatorCredit />
    </>
  );
}
