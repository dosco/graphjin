"use client";

import { Loader2 } from "lucide-react";
import ReactMarkdown from "react-markdown";
import { useMemo } from "react";
import {
  WORKSPACE_MARKDOWN_COMPONENTS as MARKDOWN_COMPONENTS,
  linkifyWorkspacePaths,
} from "@/lib/linkify-workspace-paths";
import {
  ActionRequestCard,
  RuleSavedCard,
  WorkflowSavedCard,
  extractActionRequestEvents,
  extractRuleSaveEvent,
  extractWorkflowSaveEvent,
  stripNekoFences,
} from "@/components/RuleChatBubble";
import { presentWorkFailure } from "@/lib/work-failure";
import { useWorkShell } from "../work-shell-context";
import { ActionApprovalRow } from "./work-action-approval";
import { AnswerRunFooter } from "./work-answer-footer";
import { fallbackAnswerSurface, surfaceComponents } from "./work-answer-surface";
import { REMARK_PLUGINS } from "./work-markdown";
import {
  AnswerInProgress,
  CapabilityDeniedNotice,
  NeedsInputNotice,
  WorkFailureNotice,
} from "./work-notices";
import { ProviderProgress, WorkTrail, progressPreview, useElapsed } from "./work-progress";
import { SurfaceBlock } from "./work-surface";
import { buildRunTimeline } from "./work-timeline-model";
import { ToolGroup } from "./work-tool-rows";
import type { RunRecord, TimelineItem, WorkEvent } from "./work-types";

const TRAIL_KINDS = new Set<TimelineItem["kind"]>(["progress", "tools", "interim"]);

function FenceAwareBubble({
  keyPrefix,
  raw,
}: {
  keyPrefix: string;
  raw: string;
}) {
  const text = stripNekoFences(raw);
  const ruleEvent = extractRuleSaveEvent(raw);
  const workflowEvent = extractWorkflowSaveEvent(raw);
  const actionEvents = extractActionRequestEvents(raw);
  return (
    <>
      {text ? (
        <div key={`${keyPrefix}-text`} className="work-bubble-row">
          <div className="work-bubble">
            <div className="work-markdown">
              <ReactMarkdown remarkPlugins={REMARK_PLUGINS} components={MARKDOWN_COMPONENTS}>{linkifyWorkspacePaths(text)}</ReactMarkdown>
            </div>
          </div>
        </div>
      ) : null}
      {ruleEvent ? (
        <div key={`${keyPrefix}-rule`} className="flex justify-start mt-1.5 text-left">
          <RuleSavedCard payload={ruleEvent} href="/admin/rules" />
        </div>
      ) : null}
      {workflowEvent ? (
        <div key={`${keyPrefix}-workflow`} className="flex justify-start mt-1.5 text-left">
          <WorkflowSavedCard payload={workflowEvent} href="/workflows" />
        </div>
      ) : null}
      {actionEvents.map((a, i) => (
        <div key={`${keyPrefix}-action-${i}`} className="flex justify-start mt-1.5 text-left">
          <ActionRequestCard payload={a} href="/actions?filter=awaiting" />
        </div>
      ))}
    </>
  );
}

export function RunTimeline({
  threadId,
  run,
  events,
  pending,
  fallbackContent,
  reasoningExpanded,
  avatar,
}: {
  threadId: string;
  run: RunRecord | null;
  events: WorkEvent[];
  pending: boolean;
  fallbackContent: string;
  /** Show every reasoning note and tool call in full, in every run. */
  reasoningExpanded: boolean;
  /** The bot's neko at the head of the turn, in place of the route dot. */
  avatar?: React.ReactNode;
}) {
  const { insertComposerRef } = useWorkShell();
  const presentation = useMemo(
    () => buildRunTimeline(events, run?.id ?? "pending"),
    [events, run?.id],
  );
  const hasSurface = presentation.items.some((item) => item.kind === "surface");
  const hasChoice = presentation.items.some((item) =>
    item.kind === "surface" && [...surfaceComponents(item.messages).values()].some((component) => component.component === "Choice"),
  );
  const hasText = presentation.items.some((item) => item.kind === "text");
  const hasError = presentation.items.some((item) => item.kind === "error");
  const fallbackFailure = presentWorkFailure(fallbackContent);
  const fallbackIsTechnicalFailure =
    !pending && !hasText && !run?.error && fallbackFailure.technical;
  const persistedText =
    !pending &&
    !hasText &&
    !fallbackIsTechnicalFailure &&
    fallbackContent.trim()
      ? fallbackContent
      : "";
  const persistedVitals = useMemo(
    () =>
      !pending && !hasSurface
        ? fallbackAnswerSurface(
            run?.id ?? "persisted",
            "",
            presentation.vitals,
            presentation.sources,
            presentation.items.some((item) => item.kind === "capability"),
          )
        : [],
    [
      hasSurface,
      pending,
      presentation.items,
      presentation.sources,
      presentation.vitals,
      run?.id,
    ],
  );
  const hasContent =
    presentation.items.length > 0 ||
    Boolean(persistedText) ||
    persistedVitals.length > 0;

  const lastProgress = presentation.items.map((item) => item.kind).lastIndexOf("progress");
  const elapsed = useElapsed(run?.createdAt, pending);
  const renderItem = (item: TimelineItem, index: number) => {
    if (item.kind === "text") {
      const failure = presentWorkFailure(item.content);
      if (failure.technical) {
        return (
          <WorkFailureNotice
            key={`text-error-${index}`}
            message={item.content}
          />
        );
      }
      return (
        <FenceAwareBubble
          key={`text-${index}`}
          keyPrefix={`text-${index}`}
          raw={item.content}
        />
      );
    }
    if (item.kind === "interim") {
      return (
        <FenceAwareBubble
          key={`interim-${item.id}-${index}`}
          keyPrefix={`interim-${item.id}-${index}`}
          raw={item.content}
        />
      );
    }
    if (item.kind === "progress") {
      return (
        <ProviderProgress
          key={`progress-${item.id}-${index}`}
          content={item.content}
          live={pending && index === lastProgress}
          collapsed={!reasoningExpanded && index !== lastProgress}
        />
      );
    }
    if (item.kind === "tools") {
      // Approvals render on their own below, never inside a collapsible group.
      const tools = item.tools.filter((tool) => !tool.approval);
      if (tools.length === 0) return null;
      return (
        <ToolGroup
          key={`tools-${index}`}
          tools={tools}
          threadId={threadId}
          runId={run?.id ?? ""}
        />
      );
    }
    if (item.kind === "surface") {
      return <SurfaceBlock key={`surface-${index}`} messages={item.messages} />;
    }
    if (item.kind === "capability") {
      return (
        <CapabilityDeniedNotice
          key={`capability-${item.denial.host}-${index}`}
          denial={item.denial}
          canAdminister={run?.actorRole === "admin"}
          onRequest={(prompt) => insertComposerRef.current?.(prompt)}
        />
      );
    }
    if (item.kind === "needs_input") {
      return (
        <NeedsInputNotice
          key={`needs-input-${index}`}
          request={item.request}
          onRespond={(text) => insertComposerRef.current?.(text)}
        />
      );
    }
    if (item.kind === "stopped") {
      return <p key={`stopped-${index}`} className="work-run-stopped">Stopped.</p>;
    }
    return <WorkFailureNotice key={`error-${index}`} message={item.message} />;
  };
  // A finished answer folds its whole working trail (reasoning, tool calls
  // and interim notes) into one row, so the answer leads.
  const trail = presentation.items
    .map((item, index) => ({ item, index }))
    .filter(({ item }) => TRAIL_KINDS.has(item.kind));
  const foldTrail = !reasoningExpanded && !pending && trail.length > 0 && (hasText || hasSurface || Boolean(persistedText));

  // An approval asks the operator to act, so it never folds into the trail.
  const approvals = presentation.items.flatMap((item) =>
    item.kind === "tools" ? item.tools.filter((tool) => tool.approval) : [],
  );

  return (
    <div className={`work-timeline flex flex-col gap-2.5 mt-1${avatar ? " has-avatar" : ""}`}>
      {avatar ? <span className="work-timeline-avatar">{avatar}</span> : null}
      {foldTrail ? (
        <WorkTrail run={run} items={trail.map(({ item }) => item)}>
          {trail.map(({ item, index }) => renderItem(item, index))}
        </WorkTrail>
      ) : null}
      {approvals.map((tool) => (
        <ActionApprovalRow key={`approval-${tool.id}`} threadId={threadId} runId={run?.id ?? ""} tool={tool} />
      ))}
      {presentation.items.map((item, index) =>
        (foldTrail && TRAIL_KINDS.has(item.kind)) || (!pending && item.kind === "surface") ? null : renderItem(item, index),
      )}

      {fallbackIsTechnicalFailure ? (
        <WorkFailureNotice message={fallbackContent} />
      ) : null}

      {persistedText ? (
        <FenceAwareBubble
          keyPrefix="persisted-answer"
          raw={persistedText}
        />
      ) : null}

      {/* A finished answer reads text first, then its cards, whatever order the events came in. */}
      {!pending ? presentation.items.map((item, index) => item.kind === "surface" ? renderItem(item, index) : null) : null}

      {persistedVitals.length > 0 ? (
        <SurfaceBlock messages={persistedVitals} />
      ) : null}

      {pending && !hasSurface ? (
        <AnswerInProgress
          since={run?.createdAt}
          step={lastProgress >= 0 ? progressPreview((presentation.items[lastProgress] as Extract<TimelineItem, { kind: "progress" }>).content) : null}
          sources={presentation.sources}
        />
      ) : null}
      {pending ? (
        <div className="work-status-row">
          <Loader2 className="work-status-spin" size={12} />
          <span>{presentation.lastStatus ?? "Running…"}</span>
          {elapsed ? <span className="work-status-elapsed">{elapsed}</span> : null}
        </div>
      ) : null}
      {!pending && run?.error && !hasError ? (
        <WorkFailureNotice message={run.error} />
      ) : null}
      {!pending && hasContent ? (
        <AnswerRunFooter
          run={run}
          sources={presentation.sources}
          artifacts={presentation.artifacts}
          followups={hasChoice ? [] : presentation.followups}
          onFollowup={(prompt) => insertComposerRef.current?.(prompt)}
        />
      ) : null}
    </div>
  );
}
