"use client";

import ReactMarkdown from "react-markdown";
import { useEffect, useState } from "react";
import {
  WORKSPACE_MARKDOWN_COMPONENTS as MARKDOWN_COMPONENTS,
  linkifyWorkspacePaths,
} from "@/lib/linkify-workspace-paths";
import { Disclosure } from "@/components/ui/disclosure";
import { REMARK_PLUGINS } from "./work-markdown";
import type { RunRecord, TimelineItem } from "./work-types";

export function ProviderProgress({
  content,
  live,
  collapsed = false,
}: {
  content: string;
  live: boolean;
  collapsed?: boolean;
}) {
  if (collapsed) {
    return (
      <Disclosure title={<span className="font-normal text-text2">{progressPreview(content)}</span>} meta="Progress" className="work-progress-disclosure">
        <div className="work-markdown px-3.5 pb-3">
          <ReactMarkdown remarkPlugins={REMARK_PLUGINS} components={MARKDOWN_COMPONENTS}>
            {linkifyWorkspacePaths(readableTrace(content))}
          </ReactMarkdown>
        </div>
      </Disclosure>
    );
  }
  // Every provider's reasoning renders as one note. Gemini thought summaries
  // carry bold headings; they stay inside the note as markdown.
  return (
    <div
      className="work-progress-summary"
      {...(live ? { role: "status", "aria-live": "polite" as const } : {})}
    >
      <span className="work-progress-summary-mark" aria-hidden="true" />
      <div className="work-progress-summary-copy">
        <span className="work-progress-summary-label">Progress</span>
        <div className="work-markdown">
          <ReactMarkdown remarkPlugins={REMARK_PLUGINS} components={MARKDOWN_COMPONENTS}>
            {linkifyWorkspacePaths(readableTrace(content))}
          </ReactMarkdown>
        </div>
      </div>
    </div>
  );
}

type ProgressSection = { heading: string; detail: string };

function markdownHeading(block: string[]): string | null {
  if (block.length !== 1) return null;
  const candidate = block[0].trim();
  if (candidate.startsWith("#")) {
    let markerLength = 0;
    while (markerLength < candidate.length && candidate[markerLength] === "#") {
      markerLength += 1;
    }
    if (markerLength <= 6 && candidate[markerLength] === " ") {
      return candidate.slice(markerLength + 1).trim() || null;
    }
  }
  for (const marker of ["**", "__"]) {
    if (
      candidate.length > marker.length * 2 &&
      candidate.startsWith(marker) &&
      candidate.endsWith(marker)
    ) {
      return candidate.slice(marker.length, -marker.length).trim() || null;
    }
  }
  return null;
}

function plainProgressHeading(block: string[], hasFollowingBlock: boolean): string | null {
  if (block.length !== 1 || !hasFollowingBlock) return null;
  const candidate = block[0].trim();
  if (!candidate || candidate.length > 80) return null;
  if ([".", "?", "!", ":", ";", ","].some((mark) => candidate.endsWith(mark))) {
    return null;
  }
  if (["{", "[", "`", "-", "*", ">"].some((mark) => candidate.startsWith(mark))) {
    return null;
  }
  const words = candidate.split(" ").filter(Boolean);
  return words.length >= 2 && words.length <= 10 ? candidate : null;
}

export function splitProgressSections(content: string): ProgressSection[] {
  const lines = content.split("\n").map((line) =>
    line.endsWith("\r") ? line.slice(0, -1) : line,
  );
  const blocks: string[][] = [];
  let pending: string[] = [];
  for (const line of lines) {
    if (line.trim()) {
      pending.push(line);
    } else if (pending.length) {
      blocks.push(pending);
      pending = [];
    }
  }
  if (pending.length) blocks.push(pending);
  if (!blocks.length) return [{ heading: "Details", detail: "" }];

  const sections: ProgressSection[] = [];
  let current: ProgressSection | null = null;
  for (let index = 0; index < blocks.length; index += 1) {
    const block = blocks[index];
    const heading = markdownHeading(block) ??
      plainProgressHeading(block, index + 1 < blocks.length);
    if (heading) {
      if (current) sections.push(current);
      current = { heading, detail: "" };
      continue;
    }
    const text = block.join("\n");
    if (!current) current = { heading: "Details", detail: text };
    else current.detail = current.detail ? `${current.detail}\n\n${text}` : text;
  }
  if (current) sections.push(current);

  return sections;
}

const INTERNAL_TOOL = /`?\b(?:mcp_{1,2})?neko_{1,2}([a-z]+)_([a-z_]+)\b`?/g;

/** Reasoning notes name tools by their internal ids; show readers plain names. */
export function readableTrace(content: string): string {
  return content
    .replace(/`?\b(?:mcp_{1,2}neko_{1,2}ui_)?render_cards\b`?/g, "the answer card")
    .replace(INTERNAL_TOOL, (_match, _server: string, tool: string) => `the ${tool.replaceAll("_", " ")} tool`)
    .replace(/`?"?\bminutes_saved\b"?`?/g, "the time-saved estimate");
}

/** The first line of a reasoning note, without markdown markers, for its collapsed row. */
export function progressPreview(content: string): string {
  const line = readableTrace(content)
    .split("\n")
    .map((text) => text.replace(/^[#>\-\s]+/, "").replace(/[*_`]+/g, "").trim())
    .find(Boolean) ?? "Progress";
  return line.length > 140 ? `${line.slice(0, 139)}…` : line;
}

/** Elapsed time such as "45 s", "3 min 12 s" or "1 h 5 min". */
export function formatElapsed(ms: number): string {
  const seconds = Math.max(0, Math.round(ms / 1000));
  if (seconds < 60) return `${seconds} s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return seconds % 60 ? `${minutes} min ${seconds % 60} s` : `${minutes} min`;
  return `${Math.floor(minutes / 60)} h ${minutes % 60} min`;
}

export function workDuration(run: RunRecord | null): string | null {
  if (!run?.finishedAt) return null;
  const ms = Date.parse(run.finishedAt) - Date.parse(run.createdAt);
  return Number.isFinite(ms) && ms >= 0 ? formatElapsed(ms) : null;
}

/** Elapsed time since `since`, refreshed every second while `active`. */
export function useElapsed(since: string | undefined, active: boolean): string | null {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [active]);
  if (!active || !since) return null;
  const ms = now - Date.parse(since);
  return Number.isFinite(ms) && ms >= 0 ? formatElapsed(ms) : null;
}

export function WorkTrail({ run, items, children }: { run: RunRecord | null; items: TimelineItem[]; children?: React.ReactNode }) {
  const notes = items.filter((item) => item.kind === "progress" || item.kind === "interim").length;
  const tools = items.reduce((count, item) => count + (item.kind === "tools" ? item.tools.length : 0), 0);
  const duration = workDuration(run);
  const meta = [
    notes ? `${notes} ${notes === 1 ? "note" : "notes"}` : "",
    tools ? `${tools} ${tools === 1 ? "tool call" : "tool calls"}` : "",
  ].filter(Boolean).join(", ");
  return (
    <Disclosure title={duration ? `Worked for ${duration}` : "How OpenNeko worked"} meta={meta} className="work-trail">
      <div className="flex flex-col gap-2.5 px-3 pb-3">{children}</div>
    </Disclosure>
  );
}
