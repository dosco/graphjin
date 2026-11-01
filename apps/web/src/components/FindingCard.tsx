"use client";

// Lean briefing card tuned for findings (workflow_outputs) and approvals.
// Distinct from the existing BriefingCard which is heavy/KPI-shaped.

import { useState } from "react";
import { useRouter } from "next/navigation";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge, type BadgeVariant } from "@/components/ui/badge";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { cn } from "@/lib/cn";
import { summarizeFinding } from "@/lib/finding-summary";
import { workflowDisplayName } from "@/lib/workflow-label";

const MUTE_DURATIONS = ["1h", "24h", "7d"] as const;

export type FindingCardData = {
  id: string;
  kind: "approval" | "finding";
  workflowRunId: string | null;
  workflow: { id: string; name: string } | null;
  title: string;
  body?: string | null;
  scope?: string | null;
  target?: string | null;
  mood?: string | null;
  outputKind?: string | null;
  riskLevel?: string | null;
  /** OL8: occurrences within the dedupe window ("2× today" badge when > 1). */
  seenCount?: number;
  lastSeenAt?: string;
  createdAt: string;
  pinId?: string;
  pinnedAt?: string;
};

function formatRelative(iso: string): string {
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

function moodVariant(mood?: string | null): BadgeVariant {
  switch (mood) {
    case "good":
      return "success";
    case "watch":
      return "watch";
    case "act":
      return "danger";
    default:
      return "muted";
  }
}

const MOOD_LABEL: Record<string, string> = {
  good: "Good",
  watch: "Watch",
  act: "Needs action",
};

function riskVariant(risk?: string | null): BadgeVariant {
  switch (risk) {
    case "low":
      return "muted";
    case "medium":
      return "watch";
    case "high":
    case "critical":
      return "danger";
    default:
      return "muted";
  }
}

export default function FindingCard({
  data,
  index,
  onUnpin,
  onMuted,
}: {
  data: FindingCardData;
  index: number;
  onUnpin?: (pinId: string) => void;
  /** OL7: present on Briefing cards — right-click offers "mute scope". */
  onMuted?: () => void;
}) {
  const router = useRouter();
  const [expanded, setExpanded] = useState(false);
  const isApproval = data.kind === "approval";
  const summary = data.body ? summarizeFinding(data.body) : null;

  const muteScope = async (duration: (typeof MUTE_DURATIONS)[number]) => {
    if (!data.scope) return;
    try {
      await fetch("/api/briefing/mute", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ scope: data.scope, duration }),
      });
      onMuted?.();
    } catch {
      // best-effort
    }
  };
  const pillLabel = isApproval
    ? data.riskLevel
      ? `${data.riskLevel.charAt(0).toUpperCase()}${data.riskLevel.slice(1)} risk`
      : "Pending"
    : (MOOD_LABEL[data.mood ?? "watch"] ?? "Watch");
  const pillVariant = isApproval
    ? riskVariant(data.riskLevel)
    : moodVariant(data.mood);

  const onDrillIn = () => {
    if (isApproval) {
      router.push("/actions");
    } else if (data.workflowRunId) {
      router.push(`/runs/${data.workflowRunId}`);
    }
  };

  return (
    <ContextMenu>
      <ContextMenuTrigger asChild disabled={!onMuted || !data.scope}>
        <Card
          as="article"
          className={cn(
            "group px-5 py-4 cursor-pointer transition-[border-color,transform] duration-200",
            "hover:border-text3 hover:-translate-y-px",
            "focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2",
          )}
          style={{ animation: `fadeUp 0.4s ease ${index * 0.04}s both` }}
          onClick={onDrillIn}
          role="button"
          tabIndex={0}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              onDrillIn();
            }
          }}
        >
          <div className="flex items-start justify-between gap-3 mb-1.5">
            <h3 className="min-w-0 font-display text-ui-subsection font-bold tracking-[-0.01em] text-text leading-snug m-0 [overflow-wrap:anywhere]">
              {data.title}
            </h3>
            <Badge variant={pillVariant} className="flex-shrink-0">
              {pillLabel}
            </Badge>
          </div>

          {data.body && summary && (
            <div className="mb-3">
              <div className="work-markdown text-sm leading-[1.55] text-text2">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>
                  {expanded || !summary.truncated ? data.body : summary.excerpt}
                </ReactMarkdown>
              </div>
              {summary.truncated ? (
                <Button
                  variant="ghost"
                  size="sm"
                  type="button"
                  className="mt-1 -ml-2 text-accent"
                  aria-expanded={expanded}
                  onClick={(e) => {
                    e.stopPropagation();
                    setExpanded((open) => !open);
                  }}
                >
                  {expanded ? "Show less" : "Read the full report"}
                </Button>
              ) : null}
            </div>
          )}

          {isApproval && data.target && (
            <div className="mb-2">
              <span className="font-mono text-xs text-text2">
                {data.target}
              </span>
            </div>
          )}

          <div className="flex items-center gap-1.5 text-xs text-text3 flex-wrap">
            <span>
              From{" "}
              <span className="text-text2 font-medium">
                {workflowDisplayName(data.workflow)}
              </span>
            </span>
            <span className="opacity-50">·</span>
            <span className="text-xs tabular-nums text-text2">
              {formatRelative(data.createdAt)}
            </span>
            {(data.seenCount ?? 1) > 1 && (
              <>
                <span className="opacity-50">·</span>
                <span
                  className="text-xs tabular-nums text-text2"
                  title={
                    data.lastSeenAt
                      ? `last seen ${formatRelative(data.lastSeenAt)}`
                      : undefined
                  }
                >
                  {data.seenCount}× today
                </span>
              </>
            )}
            {data.pinId && onUnpin && (
              <Button
                variant="ghost"
                type="button"
                className="bg-transparent border-0 text-text3 font-[inherit] text-ui-caption p-0 cursor-pointer hover:text-danger hover:underline underline-offset-2"
                onClick={(e) => {
                  e.stopPropagation();
                  onUnpin(data.pinId as string);
                }}
                title="Unpin from briefing"
              >
                Unpin
              </Button>
            )}
            <span className="ml-auto text-xs text-accent group-hover:underline underline-offset-2">
              {isApproval ? "Open approvals" : "Open"}
            </span>
          </div>
        </Card>
      </ContextMenuTrigger>
      <ContextMenuContent className="min-w-[180px] rounded-inner border border-border bg-card p-1.5 shadow-lift">
        <ContextMenuLabel>
          Mute <span className="font-mono">{data.scope}</span>
        </ContextMenuLabel>
        {MUTE_DURATIONS.map((duration) => (
          <ContextMenuItem
            key={duration}
            onSelect={() => void muteScope(duration)}
          >
            for {duration}
          </ContextMenuItem>
        ))}
      </ContextMenuContent>
    </ContextMenu>
  );
}
