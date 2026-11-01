"use client";

import type { ToolItem } from "./work-types";

export function ToolDetail({ tool }: { tool: ToolItem }) {
  return (
    <div className="work-tool-row-detail">
      {tool.input !== undefined ? (
        <>
          <div className="work-tool-row-section-label">Input</div>
          <pre className="work-tool-row-pre">{formatToolPayload(tool.input)}</pre>
        </>
      ) : null}
      {tool.deltas
        .map((delta) => describeToolDelta(delta))
        .filter(Boolean)
        .map((text, index) => (
          <div key={index} className="work-tool-delta">
            {text}
          </div>
        ))}
      {tool.end?.result ? (
        <>
          <div className="work-tool-row-section-label">Output</div>
          <pre className="work-tool-row-pre">{formatToolPayload(tool.end.result)}</pre>
        </>
      ) : null}
      {tool.end?.error ? (
        <>
          <div className="work-tool-row-section-label">Error</div>
          <pre className="work-tool-row-pre work-tool-row-pre-error">
            {tool.end.error}
          </pre>
        </>
      ) : null}
    </div>
  );
}

function describeToolDelta(delta: unknown): string {
  if (!delta || typeof delta !== "object") {
    return typeof delta === "string" ? delta : "";
  }
  const info = delta as {
    message?: unknown;
    summary?: unknown;
    elapsedSeconds?: unknown;
    durationMs?: unknown;
  };
  if (typeof info.summary === "string" && info.summary.trim()) {
    return info.summary.trim();
  }
  if (typeof info.message === "string" && info.message.trim()) {
    return info.message.trim();
  }
  if (typeof info.durationMs === "number" && Number.isFinite(info.durationMs)) {
    return `Took ${formatDuration(info.durationMs)}`;
  }
  if (typeof info.elapsedSeconds === "number" && Number.isFinite(info.elapsedSeconds)) {
    return `Running for ${info.elapsedSeconds.toFixed(1)}s`;
  }
  return "";
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

function formatToolPayload(value: unknown): string {
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value ?? {}, null, 2);
  } catch {
    return String(value);
  }
}
