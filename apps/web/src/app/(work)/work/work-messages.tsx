"use client";

import { Check, Copy, Pencil, RefreshCw, X } from "lucide-react";
import ReactMarkdown from "react-markdown";
import { useState } from "react";
import {
  WORKSPACE_MARKDOWN_COMPONENTS as MARKDOWN_COMPONENTS,
  linkifyWorkspacePaths,
} from "@/lib/linkify-workspace-paths";
import { stripWorkMentionBlock } from "@/lib/workflow-mention";
import { parseClarificationReply } from "@/a2ui/action";
import { Button, IconButton } from "@/components/ui/button";
import { Textarea } from "@/components/ui/field";
import { LocalDateTime } from "@/components/ui/local-date-time";
import { REMARK_PLUGINS } from "./work-markdown";
import type { MessageRecord, PendingMemory } from "./work-types";

const EMPTY_PROMPTS: Array<{ label: string; text: string }> = [
  { label: "Top customers", text: "Who are our top 10 customers by revenue this year?" },
  { label: "Revenue trend", text: "How has revenue changed over the last 4 quarters?" },
  { label: "Inventory risk", text: "Which products are below their reorder threshold?" },
  { label: "Watch for a change", text: "Set up a workflow that " },
];

const ATTACHMENT_LINE = /^- (\S+)\s+\((.+), (\d+) KB\)$/;

/** A user message, with its attached-file lines shown as file links. */
function UserCopy({ content }: { content: string }) {
  const marker = content.search(/(^|\n\n)I've attached (a file|files):\n/);
  if (marker === -1) return <div className="work-markdown user-copy">{content}</div>;
  const text = content.slice(0, marker).trim();
  const files = content
    .slice(marker)
    .split("\n")
    .map((line) => ATTACHMENT_LINE.exec(line.trim()))
    .filter((match): match is RegExpExecArray => match !== null);
  if (files.length === 0) return <div className="work-markdown user-copy">{content}</div>;
  return (
    <div className="work-markdown user-copy">
      {text ? <div>{text}</div> : null}
      <ul className="work-user-attachments" aria-label="Attached files">
        {files.map(([, path, name, kb]) => (
          <li key={path}>
            <a href={`/api/work/files/${path}`} download={name}>{name}</a>
            <span>{kb} KB</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

export function EmptyAsk({ onPick }: { onPick: (text: string) => void }) {
  return (
    <div className="ask-start">
      <h2 className="ask-start-title">What should OpenNeko look into?</h2>
      <p className="ask-start-copy">
        Ask about your data, investigate a change, or set up a workflow.
        OpenNeko shows each step while it works.
      </p>
      <div className="ask-start-prompts" aria-label="Example questions">
        {EMPTY_PROMPTS.map((prompt) => (
          <Button
            key={prompt.label}
            variant="secondary"
            className="ask-start-prompt"
            onClick={() => onPick(prompt.text)}
          >
            <strong>{prompt.label}</strong>
            <span>{prompt.text.trim()}{prompt.text.endsWith(" ") ? "…" : ""}</span>
          </Button>
        ))}
      </div>
    </div>
  );
}

export function PendingMemoryPanel({
  pending,
  threadId,
  onDecide,
}: {
  pending: PendingMemory[];
  threadId: string | null;
  onDecide: (
    id: string,
    action: "accept" | "decline",
    overrides?: { scope?: string; scopeId?: string | null },
  ) => void;
}) {
  const item = pending[0];
  if (!item) return null;
  return (
    <div className="flex items-center justify-between gap-3 border border-border bg-white/80 rounded-2xl px-3 py-2.5 shadow-soft max-[560px]:items-stretch max-[560px]:flex-col">
      <div className="min-w-0 text-ui-body-sm leading-[1.45] text-text2">
        <div className="text-ui-caption font-semibold text-text3 mb-0.5">Memory suggestion</div>
        <div>{item.draftText}</div>
        {pending.length > 1 ? (
          <div className="mt-1 text-text3 text-ui-label">+{pending.length - 1} more</div>
        ) : null}
      </div>
      <div className="inline-flex min-w-0 gap-[7px] flex-wrap justify-end flex-shrink-0 max-[560px]:justify-start [&_button]:h-[30px] [&_button]:max-w-full [&_button]:rounded-[10px] [&_button]:border [&_button]:border-border [&_button]:bg-card [&_button]:text-text2 [&_button]:inline-flex [&_button]:items-center [&_button]:justify-center [&_button]:gap-1 [&_button]:px-2 [&_button]:text-ui-label [&_button]:cursor-pointer [&_button]:transition-[color,border-color,background-color] [&_button]:duration-200 [&_button:hover]:border-accent [&_button:hover]:text-accent">
        <IconButton
          label="Dismiss memory suggestion"
          size="icon-sm"
          variant="ghost"
          onClick={() => onDecide(item.id, "decline")}
          title="Dismiss"
        >
          <X size={14} />
        </IconButton>
        <Button
          size="sm"
          onClick={() => onDecide(item.id, "accept", { scope: "global" })}
          title="Save globally"
        >
          <Check size={14} />
          <span>Global</span>
        </Button>
        {threadId ? (
          <Button
            size="sm"
            onClick={() =>
              onDecide(item.id, "accept", { scope: "thread", scopeId: threadId })
            }
            title="Save for this thread only"
          >
            <Check size={14} />
            <span>Thread</span>
          </Button>
        ) : null}
      </div>
    </div>
  );
}

export function MessageBubble({
  message,
  onCopy,
  onRetry,
  onEdit,
}: {
  message: MessageRecord;
  onCopy?: () => void;
  onRetry?: () => void;
  onEdit?: (text: string) => void;
}) {
  // What the operator sees: the raw content minus the machine-readable
  // workflow-mention block (the agent still reads the full content).
  const display = stripWorkMentionBlock(message.content);
  const clarificationReply = parseClarificationReply(message.content);
  const [editing, setEditing] = useState(false);
  const [editText, setEditText] = useState(display);
  const [copied, setCopied] = useState(false);

  if (message.role !== "user") {
    return (
      <div className="work-bubble-row">
        <div className="work-bubble">
          <div className="work-markdown">
            <ReactMarkdown remarkPlugins={REMARK_PLUGINS} components={MARKDOWN_COMPONENTS}>{linkifyWorkspacePaths(message.content)}</ReactMarkdown>
          </div>
        </div>
      </div>
    );
  }

  if (editing && onEdit) {
    const trimmed = editText.trim();
    const dirty = trimmed.length > 0 && trimmed !== display.trim();
    const cancel = () => {
      setEditing(false);
      setEditText(display);
    };
    const save = () => {
      if (!dirty) return;
      setEditing(false);
      onEdit(trimmed);
    };
    return (
      <div className="work-bubble-row is-user">
        <div className="work-bubble is-user is-editing">
          <Textarea
            className="work-bubble-edit"
            value={editText}
            onChange={(e) => {
              setEditText(e.target.value);
              const el = e.currentTarget;
              el.style.height = "auto";
              el.style.height = `${Math.min(el.scrollHeight, 320)}px`;
            }}
            onKeyDown={(e) => {
              if (e.key === "Escape") {
                e.preventDefault();
                cancel();
              } else if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                save();
              }
            }}
            ref={(el) => {
              if (el) {
                el.style.height = "auto";
                el.style.height = `${Math.min(el.scrollHeight, 320)}px`;
              }
            }}
            rows={1}
            autoFocus
          />
        </div>
        <div className="work-bubble-edit-hint">
          <Button size="sm" variant="ghost" onClick={cancel}>
            Cancel
          </Button>
          <Button
            size="sm"
            variant="primary"
            className="is-primary"
            onClick={save}
            disabled={!dirty}
          >
            Send
          </Button>
        </div>
      </div>
    );
  }

  const showActions = onCopy || onRetry || onEdit || message.createdAt;
  return (
    <div className="work-bubble-row is-user has-actions">
      <div className="work-bubble is-user">
        {clarificationReply ? (
          <div className="work-markdown user-copy">
            <div className="text-ui-caption font-semibold opacity-60 mb-2">Your answers</div>
            <div className="space-y-3">
              {clarificationReply.map(({ question, answer }, index) => (
                <div key={index}>
                  <div className="opacity-70">{question}</div>
                  <div className="whitespace-pre-wrap font-semibold">{answer}</div>
                </div>
              ))}
            </div>
          </div>
        ) : (
          <UserCopy content={display} />
        )}
      </div>
      {showActions ? (
        <div className="work-bubble-actions">
          {message.createdAt ? <LocalDateTime value={message.createdAt} className="work-bubble-time" /> : null}
          {onCopy ? (
            <IconButton
              label={copied ? "Copied" : "Copy"}
              size="icon-sm"
              variant="ghost"
              title={copied ? "Copied" : "Copy"}
              onClick={() => {
                onCopy();
                setCopied(true);
                window.setTimeout(() => setCopied(false), 1200);
              }}
            >
              {copied ? <Check size={12} /> : <Copy size={12} />}
            </IconButton>
          ) : null}
          {onRetry ? (
            <IconButton
              label="Retry"
              size="icon-sm"
              variant="ghost"
              title="Retry"
              onClick={onRetry}
            >
              <RefreshCw size={12} />
            </IconButton>
          ) : null}
          {onEdit ? (
            <IconButton
              label="Edit"
              size="icon-sm"
              variant="ghost"
              title="Edit"
              onClick={() => {
                setEditText(display);
                setEditing(true);
              }}
            >
              <Pencil size={12} />
            </IconButton>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
