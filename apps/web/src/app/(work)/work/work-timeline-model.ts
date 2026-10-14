import {
  canonicalAnswerCreateId,
  fallbackAnswerSurface,
  ownVitalsBySurface,
  surfaceIdOf,
} from "./work-answer-surface";
import { runWasCancelled } from "./work-thread";
import type {
  AnswerVital,
  ApprovalItem,
  RunArtifact,
  TimelineItem,
  ToolItem,
  WorkEvent,
} from "./work-types";

function policyDenialFromToolEnd(
  event: Extract<WorkEvent, { type: "tool_end" }>,
): Extract<WorkEvent, { type: "capability_denied" }> | null {
  const raw = [
    event.error ?? "",
    typeof event.result === "string"
      ? event.result
      : JSON.stringify(event.result ?? ""),
  ].join("\n");
  if (!/policy_denied|not permitted by policy/i.test(raw)) return null;
  const match = raw.match(
    /\b(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+([a-z0-9.-]+)(?::(\d+))?(\/[^\s"'\\]*)?\s+not permitted by policy/i,
  );
  if (!match) return null;
  return {
    type: "capability_denied",
    capability: "network_egress",
    reason: "policy_denied",
    host: match[2].toLowerCase(),
    ...(match[3] ? { port: Number(match[3]) } : {}),
    method: match[1].toUpperCase(),
    ...(match[4] ? { path: match[4] } : {}),
  };
}

function parseNestedToolResult(value: unknown): Record<string, unknown> | null {
  let current = value;
  for (let depth = 0; depth < 4; depth += 1) {
    if (typeof current === "string") {
      try {
        current = JSON.parse(current);
        continue;
      } catch {
        return null;
      }
    }
    if (!current || typeof current !== "object" || Array.isArray(current)) {
      return null;
    }
    const record = current as Record<string, unknown>;
    if (
      typeof record.result === "string" &&
      !("action_request_id" in record)
    ) {
      current = record.result;
      continue;
    }
    return record;
  }
  return null;
}

/**
 * Older sandbox brokers persisted the action request but dropped the
 * action_request_emit event. Recover the approval from the successful MCP
 * tool result so historical pending requests remain actionable.
 */
function approvalFromToolResult(
  tool: ToolItem | undefined,
  event: Extract<WorkEvent, { type: "tool_end" }>,
): ApprovalItem | null {
  if (!tool) return null;
  const input =
    tool.input && typeof tool.input === "object"
      ? (tool.input as Record<string, unknown>)
      : {};
  const title = `${tool.name} ${String(input.title ?? "")}`.toLowerCase();
  const isInstall = title.includes("plugin_manager_request_plugin_install");
  const isUninstall = title.includes("plugin_manager_request_plugin_uninstall");
  if (!isInstall && !isUninstall) return null;

  const result = parseNestedToolResult(event.result);
  const actionRequestId =
    typeof result?.action_request_id === "string"
      ? result.action_request_id
      : null;
  if (!actionRequestId || result?.ok !== true) return null;
  const rawInput =
    input.rawInput && typeof input.rawInput === "object"
      ? (input.rawInput as Record<string, unknown>)
      : {};
  const intent =
    typeof rawInput.intent === "string" ? rawInput.intent : null;
  return {
    actionRequestId,
    actionKind: isInstall ? "plugin_install" : "plugin_uninstall",
    intent,
    summary: intent,
    decision:
      result.decision === "approved"
        ? "auto_approved"
        : "pending_approval",
    result: null,
  };
}

function toolSources(event: Extract<WorkEvent, { type: "tool_start" }>): string[] {
  const raw =
    typeof (event.input as { title?: unknown })?.title === "string"
      ? String((event.input as { title: string }).title)
      : JSON.stringify(event.input ?? "");
  const found: string[] = [];
  for (const match of raw.matchAll(
    /"(?:from_table|to_table|table)"\s*:\s*"([a-z0-9_]+)"/gi,
  )) {
    found.push(match[1].toLowerCase());
  }
  const rootField = raw.match(/"query"\s*:\s*"\{\s*([a-z_][a-z0-9_]*)/i);
  if (rootField) found.push(rootField[1].toLowerCase());
  return found;
}

// Walks a run's event stream chronologically and produces an interleaved
// timeline: text segments split at tool boundaries, with each tool placed
// inline where it ran. Backends emit `message` events as deltas (new text
// since the previous event), so segments build by appending — no string
// archaeology needed.
export function buildRunTimeline(events: WorkEvent[], runId: string): {
  items: TimelineItem[];
  lastStatus: string | null;
  isDone: boolean;
  vitals: AnswerVital[];
  followups: string[];
  sources: string[];
  artifacts: RunArtifact[];
} {
  const items: TimelineItem[] = [];
  const stopped = events.some((event) => runWasCancelled(event));
  const toolsById = new Map<string, ToolItem>();
  const progressById = new Map<
    string,
    Extract<TimelineItem, { kind: "progress" }>
  >();
  const approvalToolByRequest = new Map<string, ToolItem>();
  let surfaceItem: Extract<TimelineItem, { kind: "surface" }> | null = null;
  let canonicalAnswerSurfaceId: string | null = null;
  const supersededAnswerSurfaceIds = new Set<string>();
  const denialKeys = new Set<string>();
  const sources = new Set<string>();
  const artifacts: RunArtifact[] = [];
  let vitals: AnswerVital[] = [];
  let followups: string[] = [];
  let pendingText = "";
  let lastStatus: string | null = null;
  let isDone = false;

  const flushTextSegment = () => {
    if (pendingText.trim()) {
      items.push({ kind: "text", content: pendingText });
    }
    pendingText = "";
  };

  for (const event of events) {
    switch (event.type) {
      case "message": {
        if (event.role !== "assistant") break;
        pendingText += event.content;
        break;
      }
      case "interim": {
        flushTextSegment();
        items.push({ kind: "interim", id: event.id, content: event.content });
        break;
      }
      case "progress": {
        flushTextSegment();
        const existing = progressById.get(event.id);
        if (existing) {
          existing.content += event.content;
          break;
        }
        const item: Extract<TimelineItem, { kind: "progress" }> = {
          kind: "progress",
          id: event.id,
          content: event.content,
        };
        progressById.set(event.id, item);
        items.push(item);
        break;
      }
      case "tool_start": {
        // A tool_start can arrive twice (e.g. a replayed in-flight run);
        // update the existing row in place rather than emitting a duplicate.
        const existing = toolsById.get(event.id);
        if (existing) {
          existing.name = event.name;
          existing.input = event.input;
          break;
        }
        flushTextSegment();
        const item: ToolItem = {
          id: event.id,
          name: event.name,
          input: event.input,
          deltas: [],
        };
        toolsById.set(event.id, item);
        for (const source of toolSources(event)) sources.add(source);
        // Cluster consecutive tool calls (no text/error between them) into a
        // single collapsible group — keeps long tool runs from dominating the
        // transcript while preserving the start of a new group when the model
        // narrates between calls.
        const last = items[items.length - 1];
        if (last && last.kind === "tools") {
          last.tools.push(item);
        } else {
          items.push({ kind: "tools", tools: [item] });
        }
        break;
      }
      case "tool_delta": {
        const item = toolsById.get(event.id);
        if (item) item.deltas.push(event.delta);
        break;
      }
      case "tool_end": {
        const item = toolsById.get(event.id);
        if (item) item.end = event;
        const recoveredApproval = approvalFromToolResult(item, event);
        if (
          recoveredApproval &&
          item &&
          !approvalToolByRequest.has(recoveredApproval.actionRequestId)
        ) {
          item.approval = recoveredApproval;
          approvalToolByRequest.set(
            recoveredApproval.actionRequestId,
            item,
          );
        }
        const denial = policyDenialFromToolEnd(event);
        if (denial) {
          const key = `${denial.host}:${denial.port ?? 443}`;
          if (!denialKeys.has(key)) {
            denialKeys.add(key);
            flushTextSegment();
            items.push({ kind: "capability", denial });
          }
        }
        break;
      }
      case "status": {
        lastStatus = event.message;
        break;
      }
      case "error": {
        flushTextSegment();
        // An operator's stop ends the run without a failure notice.
        items.push(stopped ? { kind: "stopped" } : { kind: "error", message: event.message });
        break;
      }
      case "surface": {
        flushTextSegment();
        if (!surfaceItem) {
          surfaceItem = { kind: "surface", messages: [] };
          items.push(surfaceItem);
        }
        for (const message of event.messages) {
          const nextAnswerSurfaceId = canonicalAnswerCreateId(message);
          if (
            nextAnswerSurfaceId &&
            canonicalAnswerSurfaceId &&
            nextAnswerSurfaceId !== canonicalAnswerSurfaceId
          ) {
            // A Work run owns one canonical answer. If the agent retries that
            // answer with a fresh surface id, treat the later create as a
            // revision instead of replaying both reports. Other A2UI surfaces
            // (forms, confirmations, and protocol updates) remain independent.
            supersededAnswerSurfaceIds.add(canonicalAnswerSurfaceId);
            surfaceItem.messages = surfaceItem.messages.filter(
              (existing) =>
                surfaceIdOf([existing]) !== canonicalAnswerSurfaceId,
            );
          }
          if (nextAnswerSurfaceId) {
            canonicalAnswerSurfaceId = nextAnswerSurfaceId;
            supersededAnswerSurfaceIds.delete(nextAnswerSurfaceId);
          }

          const targetSurfaceId = surfaceIdOf([message]);
          if (
            targetSurfaceId &&
            supersededAnswerSurfaceIds.has(targetSurfaceId)
          ) {
            continue;
          }
          surfaceItem.messages.push(message);

          if (
            "deleteSurface" in message &&
            message.deleteSurface.surfaceId === canonicalAnswerSurfaceId
          ) {
            canonicalAnswerSurfaceId = null;
          }
        }
        break;
      }
      case "capability_denied": {
        const key = `${event.host}:${event.port ?? 443}`;
        if (!denialKeys.has(key)) {
          denialKeys.add(key);
          flushTextSegment();
          items.push({ kind: "capability", denial: event });
        }
        break;
      }
      case "artifact": {
        artifacts.push(event.artifact);
        break;
      }
      case "needs_input": {
        // The AskUserQuestion tool normally emits a deterministic A2UI form
        // immediately before this event. Thin/historical channels have no
        // surface, so retain a modality-free fallback in the timeline.
        if (!event.surfaceId) {
          flushTextSegment();
          items.push({ kind: "needs_input", request: event });
        }
        break;
      }
      case "vitals": {
        vitals = event.items;
        break;
      }
      case "followups": {
        followups = event.items;
        break;
      }
      case "action_request_emit": {
        // Auto-approved actions are ordinary tool execution. Their tool row
        // already carries running/completed/failed state, so a second
        // approval-shaped entry would only duplicate the call.
        if (event.decision === "auto_approved") break;

        const approval: ApprovalItem = {
          actionRequestId: event.action_request_id,
          actionKind: event.kind,
          intent: event.intent ?? null,
          summary: event.summary ?? null,
          decision: event.decision,
          result: null,
        };
        const existingTool = approvalToolByRequest.get(
          event.action_request_id,
        );
        if (existingTool) {
          existingTool.approval = {
            ...existingTool.approval,
            ...approval,
            result: existingTool.approval?.result ?? null,
          };
          break;
        }

        // The broker emits the request while the corresponding MCP tool is
        // still open. Attach the decision state to that row. Fence-based and
        // historical events may have no tool_start, so synthesize the same row
        // shape instead of falling back to a different card component.
        const activeTool = [...toolsById.values()]
          .reverse()
          .find((tool) => !tool.end);
        const targetTool: ToolItem =
          activeTool ?? {
            id: `approval-${event.action_request_id}`,
            name: event.kind,
            deltas: [],
          };
        targetTool.approval = approval;
        approvalToolByRequest.set(event.action_request_id, targetTool);
        if (!activeTool) {
          flushTextSegment();
          toolsById.set(targetTool.id, targetTool);
          items.push({ kind: "tools", tools: [targetTool] });
        }
        break;
      }
      case "action_request_result": {
        const targetTool = approvalToolByRequest.get(event.action_request_id);
        if (!targetTool?.approval) break;
        targetTool.approval = { ...targetTool.approval, result: event };
        break;
      }
      case "done": {
        isDone = true;
        break;
      }
      default:
        break;
    }
  }
  flushTextSegment();
  const sourceList = [...sources];
  const deniedNetwork = denialKeys.size > 0;
  if (surfaceItem) {
    surfaceItem.messages = ownVitalsBySurface(
      surfaceItem.messages,
      vitals,
      sourceList,
      deniedNetwork,
    );
  } else if (isDone) {
    // Plain assistant prose remains conversation. Vitals are additive evidence,
    // never a replacement surface that turns every answer into a report.
    const fallback = fallbackAnswerSurface(
      runId,
      "",
      vitals,
      sourceList,
      deniedNetwork,
    );
    if (fallback.length > 0) {
      items.push({
        kind: "surface",
        messages: fallback,
      });
    }
  }
  // Synthesize an end for any tool that never got a tool_end before the run
  // terminated. Without this, ACP runs that miss the final tool_call_update
  // notification (Hermes occasionally drops it for read tools) leave the
  // cluster stuck on "running" forever.
  if (isDone) {
    for (const tool of toolsById.values()) {
      if (!tool.end) {
        tool.end = {
          type: "tool_end",
          id: tool.id,
          result: undefined,
        };
      }
    }
  }
  return {
    items,
    lastStatus,
    isDone,
    vitals,
    followups,
    sources: sourceList,
    artifacts,
  };
}
