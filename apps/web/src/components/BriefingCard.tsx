"use client";

import { useState } from "react";
import { ChevronDown, Copy, RotateCw, Search, X } from "lucide-react";
import Chart from "./Chart";
import type { ChartDataPoint } from "./Chart";
import KpiHeadline from "./KpiHeadline";
import { Button, IconButton } from "@/components/ui/button";
import { MenuItem, OverflowMenu } from "@/components/ui/overflow-menu";
import { copyText } from "@/lib/copy-text";

async function copyCardToClipboard(ins: BriefingCardData): Promise<void> {
  const lines: string[] = [];
  if (ins.text) lines.push(ins.text);
  if (ins.metric || ins.label) {
    lines.push(`${ins.metric}${ins.label ? ` (${ins.label})` : ""}`.trim());
  }
  if (ins.detail) lines.push(ins.detail);
  const text = lines.join("\n");
  try {
    await copyText(text);
  } catch {}
}

const MOOD_LABELS: Record<string, string> = {
  good: "On track",
  watch: "Watch",
  act: "Act now",
  bad: "Act now",
};

const MOOD_CHART_ACCENT: Record<string, string> = {
  good: "var(--success-mid)",
  watch: "var(--watch)",
  act: "var(--danger)",
  bad: "var(--danger)",
};

export type BriefingCardState = "ok" | "pending" | "failed";

export interface BriefingCardData {
  id: string;
  metricId: string;
  source: string;
  state?: BriefingCardState;
  error?: string;
  mood: string;
  text: string;
  metric: string;
  label: string;
  detail: string;
  chart: string;
  chartData: ChartDataPoint[];
}

export default function BriefingCard({
  ins,
  index,
  onDismiss,
  onRetry,
  onDeepDive,
}: {
  ins: BriefingCardData;
  index: number;
  onDismiss?: () => void;
  onRetry?: (metricId: string) => void;
  onDeepDive?: (metricId: string) => void;
}) {
  // Desktop keeps the full analytical card. On phones, only the lead card
  // starts expanded; the rest stay concise until the operator asks for detail.
  const [mobileOpen, setMobileOpen] = useState(index === 0);
  const [retrying, setRetrying] = useState(false);
  const state: BriefingCardState = ins.state ?? "ok";
  const moodKey = MOOD_LABELS[ins.mood] ? ins.mood : "good";
  const moodLabel = MOOD_LABELS[moodKey];

  const refreshing = retrying || state === "pending";

  const handleRetry = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (!onRetry || !ins.metricId || refreshing) return;
    setRetrying(true);
    try {
      await onRetry(ins.metricId);
    } finally {
      setRetrying(false);
    }
  };

  return (
    <div
      className={`icard exp${state === "failed" ? " icard-failed" : ""}${state === "pending" ? " icard-pending" : ""}`}
      data-mood={moodKey}
      style={{ animation: `fadeUp 0.5s ease ${index * 0.07}s both` }}
    >
      <div className="itop">
        <div className="icontent">
          <div className="ieyebrow">
            <span className="ieyebrow-dot" aria-hidden="true" />
            <span>{moodLabel}</span>
          </div>
          <div className="itext">{ins.text}</div>
          {state === "pending" ? (
            <div
              className="iskel"
              aria-label="Refreshing metric"
              aria-busy="true"
            >
              <div className="skel skel-metric" />
              <div className="skel skel-label" />
            </div>
          ) : ins.metric ? (
            <KpiHeadline
              metric={ins.metric}
              label={ins.label}
              data={state === "ok" ? ins.chartData : undefined}
              size="card"
              mood={moodKey}
            />
          ) : null}
        </div>
      </div>
      <div className="iactions">
        {onDeepDive && ins.metricId && state === "ok" && (
          <IconButton
            label="Deep dive in Work"
            size="icon-sm"
            variant="ghost"
            onClick={(e) => {
              e.stopPropagation();
              onDeepDive(ins.metricId);
            }}
          >
            <Search aria-hidden="true" strokeWidth={2.25} />
          </IconButton>
        )}
        <OverflowMenu label="Card actions">
          {onRetry && ins.metricId ? (
            <MenuItem
              onClick={handleRetry}
              disabled={refreshing}
              aria-busy={refreshing}
            >
              <RotateCw
                aria-hidden="true"
                strokeWidth={2}
                className={refreshing ? "animate-spin" : undefined}
              />
              {refreshing ? "Re-running…" : "Re-run metric"}
            </MenuItem>
          ) : null}
          <MenuItem onClick={() => void copyCardToClipboard(ins)}>
            <Copy aria-hidden="true" strokeWidth={2} />
            Copy card text
          </MenuItem>
          {onDismiss ? (
            <MenuItem danger onClick={onDismiss}>
              <X aria-hidden="true" strokeWidth={2} />
              Dismiss card
            </MenuItem>
          ) : null}
        </OverflowMenu>
      </div>
      <Button
        variant="ghost"
        type="button"
        className="icard-toggle"
        data-ui-disclosure=""
        aria-expanded={mobileOpen}
        onClick={() => setMobileOpen((current) => !current)}
      >
        {mobileOpen ? "Hide detail" : "View detail"}
        <ChevronDown
          aria-hidden="true"
          className={mobileOpen ? "is-open" : ""}
        />
      </Button>
      <div className="idetail open" data-mobile-open={mobileOpen}>
        {state === "pending" ? (
          <div className="dskel" aria-hidden="true">
            <div className="skel skel-line" />
            <div className="skel skel-line skel-line-short" />
          </div>
        ) : (
          <div className="dtext">{ins.detail}</div>
        )}
        {state === "ok" && ins.chartData?.length > 1 && (
          <div className="dchart">
            <Chart
              type={ins.chart}
              data={ins.chartData}
              accent={MOOD_CHART_ACCENT[moodKey] ?? undefined}
              centerLabel={ins.metric}
              valueLabel={ins.label}
              baselineLabel="Prior Period"
            />
          </div>
        )}
      </div>
    </div>
  );
}
