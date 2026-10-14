"use client";

import { Check, Loader2, X } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { ActionApprovalRow } from "./work-action-approval";
import { ToolDetail } from "./work-tool-detail";
import type { ToolItem } from "./work-types";

export function ToolGroup({
  tools,
  threadId,
  runId,
}: {
  tools: ToolItem[];
  threadId: string;
  runId: string;
}) {
  const inflight = tools.filter((t) => !t.end).length;
  const failed = tools.filter((t) => t.end?.error).length;
  const showHeader = tools.length > 1;
  // Collapsed by default and left alone — the running/failed badges on the
  // header carry live progress, so the body never expands on its own. An
  // auto-open while in flight grew the transcript on every tool call and
  // yanked the viewport down; the operator opens it only if they want detail.
  const [open, setOpen] = useState(false);

  if (!showHeader) {
    return (
      <div className="work-tool-group work-tool-group-single">
        <ToolRow tool={tools[0]} threadId={threadId} runId={runId} />
      </div>
    );
  }

  return (
    <div className="work-tool-group">
      <Button
        size="sm"
        variant="ghost"
        className="work-tool-group-head"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
      >
        <span className="work-tool-group-toggle">{open ? "▾" : "▸"}</span>
        <span className="work-tool-group-count">
          {tools.length} tool {tools.length === 1 ? "call" : "calls"}
        </span>
        {inflight > 0 ? (
          <span className="work-tool-group-badge running">
            <Loader2 className="work-status-spin" size={11} /> running
          </span>
        ) : null}
        {failed > 0 ? (
          <span className="work-tool-group-badge failed">
            {failed} failed
          </span>
        ) : null}
      </Button>
      {open ? (
        <div className="work-tool-group-body">
          {tools.map((tool) => (
            <ToolRow
              key={tool.id}
              tool={tool}
              threadId={threadId}
              runId={runId}
            />
          ))}
        </div>
      ) : null}
    </div>
  );
}

function ToolRow({
  tool,
  threadId,
  runId,
}: {
  tool: ToolItem;
  threadId: string;
  runId: string;
}) {
  if (tool.approval) {
    return (
      <ActionApprovalRow threadId={threadId} runId={runId} tool={tool} />
    );
  }
  return <RegularToolRow tool={tool} />;
}

function RegularToolRow({ tool }: { tool: ToolItem }) {
  const [open, setOpen] = useState(false);
  const status: "running" | "done" | "failed" = tool.end?.error
    ? "failed"
    : tool.end
      ? "done"
      : "running";
  const subtitle = toolSubtitle(tool);
  const hasDetail =
    tool.input !== undefined ||
    tool.deltas.length > 0 ||
    tool.end?.result !== undefined ||
    tool.end?.error !== undefined;

  return (
    <div className={`work-tool-row work-tool-row-${status}`}>
      <Button
        size="sm"
        variant="ghost"
        className="work-tool-row-head"
        onClick={() => hasDetail && setOpen((v) => !v)}
        disabled={!hasDetail}
        aria-expanded={open}
      >
        <span className={`work-tool-row-icon work-tool-row-icon-${status}`}>
          {status === "running" ? (
            <Loader2 className="work-status-spin" size={12} />
          ) : status === "failed" ? (
            <X size={12} />
          ) : (
            <Check size={12} />
          )}
        </span>
        <span className="work-tool-row-name">{tool.name}</span>
        {subtitle ? <span className="work-tool-row-subtitle">{subtitle}</span> : null}
      </Button>
      {open ? <ToolDetail tool={tool} /> : null}
    </div>
  );
}

function toolSubtitle(tool: ToolItem): string {
  for (const delta of tool.deltas) {
    if (delta && typeof delta === "object" && "summary" in delta) {
      const s = (delta as { summary?: unknown }).summary;
      if (typeof s === "string" && s.trim()) return s.trim();
    }
  }
  if (tool.input && typeof tool.input === "object") {
    const obj = tool.input as Record<string, unknown>;
    if (typeof obj.title === "string" && obj.title.trim()) return obj.title.trim();
    if (typeof obj.command === "string" && obj.command.trim()) return obj.command.trim();
    if (typeof obj.description === "string" && obj.description.trim()) {
      return obj.description.trim();
    }
  }
  return "";
}
