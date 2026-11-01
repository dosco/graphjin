"use client";

import { ArrowUp, Paperclip, Square, X } from "lucide-react";
import { useMemo, useRef, useState, type Dispatch, type RefObject, type SetStateAction } from "react";
import {
  filterWorkMentions,
  inferTextEdit,
  updateDraftWorkMentions,
  workMentionKeyAction,
  type DraftWorkMention,
  type WorkMention,
} from "@/lib/workflow-mention";
import { Badge } from "@/components/ui/badge";
import { Button, IconButton } from "@/components/ui/button";
import { Input, Textarea } from "@/components/ui/field";
import {
  ACCEPTED_ATTACHMENT_EXTENSIONS,
  ACCEPTED_ATTACHMENT_SUFFIXES,
  MAX_ATTACHMENTS,
  MAX_ATTACHMENT_SIZE,
  fileExtension,
} from "./work-attachments";

export function useWorkMentions(
  draft: string,
  setDraft: (value: string) => void,
  textareaRef: RefObject<HTMLTextAreaElement | null>,
) {
  // Unified skill/workflow autocomplete. Draft selections retain their exact
  // ranges so colliding @names still serialize the selected kind and id.
  const [mentionOptions, setMentionOptions] = useState<WorkMention[]>([]);
  const [mentionSourceState, setMentionSourceState] = useState<{
    skills: "idle" | "loading" | "ready" | "error";
    workflows: "idle" | "loading" | "ready" | "error";
  }>({ skills: "idle", workflows: "idle" });
  const [mention, setMention] = useState<{
    query: string;
    anchor: number;
    activeIndex: number;
  } | null>(null);
  const [draftMentions, setDraftMentions] = useState<DraftWorkMention[]>([]);
  const mentionOptionsLoadedRef = useRef(false);

  const mentionMatches = useMemo(() => {
    if (!mention) return [] as WorkMention[];
    return filterWorkMentions(mentionOptions, mention.query);
  }, [mention, mentionOptions]);
  // Once the query holds a space, the menu stays only while something matches.
  const mentionOpen =
    mention !== null && (!/\s/.test(mention.query) || mentionMatches.length > 0 ||
      [mentionSourceState.skills, mentionSourceState.workflows].some((state) => state !== "ready" && state !== "error"));
  const mentionActiveIndex = mention
    ? Math.min(Math.max(mention.activeIndex, 0), mentionMatches.length - 1)
    : 0;
  const mentionActiveOption = mentionMatches[mentionActiveIndex] ?? null;
  const mentionLoading =
    mentionSourceState.skills === "loading" ||
    mentionSourceState.workflows === "loading";
  const mentionErrorCount = [
    mentionSourceState.skills,
    mentionSourceState.workflows,
  ].filter((state) => state === "error").length;
  const mentionSettled = [
    mentionSourceState.skills,
    mentionSourceState.workflows,
  ].every((state) => state === "ready" || state === "error");

  // Lazily fetch both catalogs. Promise.allSettled keeps one healthy source
  // visible when the other endpoint fails.
  async function loadMentionOptions() {
    if (mentionOptionsLoadedRef.current) return;
    mentionOptionsLoadedRef.current = true;
    setMentionSourceState({ skills: "loading", workflows: "loading" });

    const [workflowResult, skillResult] = await Promise.allSettled([
      fetch("/api/workflows").then(async (res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const data = (await res.json()) as {
          workflows?: Array<{
            id: string;
            name: string;
            description?: string | null;
            goal?: string | null;
          }>;
        };
        return (data.workflows ?? []).map(
          (workflow): WorkMention => ({
            kind: "workflow",
            id: workflow.id,
            name: workflow.name,
            description:
              workflow.description?.trim() ||
              workflow.goal?.trim() ||
              "Saved workflow",
          }),
        );
      }),
      fetch("/api/work/skills").then(async (res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const data = (await res.json()) as {
          skills?: Array<{ name: string; description?: string | null }>;
        };
        return (data.skills ?? []).map(
          (skill): WorkMention => ({
            kind: "skill",
            id: skill.name,
            name: skill.name,
            description:
              skill.description?.trim() || "Installed capability instructions",
          }),
        );
      }),
    ]);

    setMentionOptions([
      ...(workflowResult.status === "fulfilled" ? workflowResult.value : []),
      ...(skillResult.status === "fulfilled" ? skillResult.value : []),
    ]);
    setMentionSourceState({
      workflows: workflowResult.status === "fulfilled" ? "ready" : "error",
      skills: skillResult.status === "fulfilled" ? "ready" : "error",
    });
    if (
      workflowResult.status === "rejected" &&
      skillResult.status === "rejected"
    ) {
      mentionOptionsLoadedRef.current = false;
    }
  }

  // Detect an in-progress "@query" token ending at the caret: an "@" at the
  // start or just after whitespace, with no whitespace between it and the
  // caret. Returns null when the caret isn't inside a fresh mention.
  function detectMention(value: string, caret: number) {
    const upto = value.slice(0, caret);
    const at = upto.lastIndexOf("@");
    if (at === -1) return null;
    if (at > 0 && !/\s/.test(value[at - 1] ?? "")) return null;
    const query = upto.slice(at + 1);
    // Names hold spaces; a line break or a long run of text ends the search.
    if (/\n/.test(query) || query.length > 80) return null;
    return { query, anchor: at, activeIndex: 0 };
  }

  function handleDraftChange(value: string, caret: number) {
    const edit = inferTextEdit(draft, value);
    setDraft(value);
    setDraftMentions((previous) =>
      updateDraftWorkMentions(previous, edit),
    );
    const next = detectMention(value, caret);
    setMention(next);
    if (next) void loadMentionOptions();
  }

  // Replace the in-progress token and retain its exact typed identity. The
  // visible composer stays plain @name text; metadata is appended only at send.
  function insertMention(option: WorkMention) {
    const ta = textareaRef.current;
    const caret = ta ? ta.selectionStart : draft.length;
    const anchor = mention?.anchor ?? draft.slice(0, caret).lastIndexOf("@");
    if (anchor === -1) return;
    const before = draft.slice(0, anchor);
    const after = draft.slice(caret);
    const token = `@${option.name}`;
    const sep = after.startsWith(" ") ? "" : " ";
    const nextDraft = `${before}${token}${sep}${after}`;
    const nextCaret = before.length + token.length + sep.length;
    setDraft(nextDraft);
    setMention(null);
    setDraftMentions((previous) => [
      ...updateDraftWorkMentions(previous, {
        start: anchor,
        oldEnd: caret,
        newEnd: anchor + token.length,
      }),
      {
        kind: option.kind,
        id: option.id,
        name: option.name,
        start: anchor,
        end: anchor + token.length,
      },
    ]);
    requestAnimationFrame(() => {
      const el = textareaRef.current;
      if (el) {
        el.focus();
        el.setSelectionRange(nextCaret, nextCaret);
      }
    });
  }

  return {
    mention,
    setMention,
    draftMentions,
    setDraftMentions,
    mentionMatches,
    mentionOpen,
    mentionActiveIndex,
    mentionActiveOption,
    mentionLoading,
    mentionErrorCount,
    mentionSettled,
    handleDraftChange,
    insertMention,
  };
}

export function WorkComposer({
  textareaRef,
  draft,
  files,
  setFiles,
  sending,
  mentions,
  placeholder,
  assistantName,
  onSend,
  onStop,
  onAttachError,
}: {
  textareaRef: RefObject<HTMLTextAreaElement | null>;
  draft: string;
  files: File[];
  setFiles: Dispatch<SetStateAction<File[]>>;
  sending: boolean;
  mentions: ReturnType<typeof useWorkMentions>;
  placeholder: string;
  assistantName: string;
  onSend: () => void;
  onStop: () => void;
  onAttachError: (message: string | null) => void;
}) {
  const {
    mention,
    setMention,
    mentionMatches,
    mentionOpen,
    mentionActiveIndex,
    mentionActiveOption,
    mentionLoading,
    mentionErrorCount,
    mentionSettled,
    handleDraftChange,
    insertMention,
  } = mentions;
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  return (
    <>
      <div
        className={`work-composer-shell relative${sending ? " is-working" : ""}`}
      >
        {mentionOpen ? (
          <div
            className="work-mention-picker"
            aria-label="Skills and workflows"
          >
            <div className="work-mention-heading" aria-hidden="true">
              <span>Skills and workflows</span>
              {mentionMatches.length > 0 ? (
                <span className="tabular-nums">{mentionMatches.length}</span>
              ) : null}
            </div>
            <div className="work-mention-layout">
              <div
                id="work-capability-listbox"
                className="work-mention-list"
                role="listbox"
                aria-label="Skills and workflows"
                aria-busy={mentionLoading}
              >
                {mentionMatches.map((option, index) => (
                  <button
                    id={`work-capability-option-${index}`}
                    key={`${option.kind}:${option.id}`}
                    type="button"
                    role="option"
                    aria-selected={index === mentionActiveIndex}
                    data-ui-bespoke-reason="Combobox option must keep focus in the Work composer while exposing active-descendant selection"
                    className="work-mention-option"
                    onMouseEnter={() =>
                      setMention((current) =>
                        current
                          ? { ...current, activeIndex: index }
                          : current,
                      )
                    }
                    onPointerDown={(event) => {
                      event.preventDefault();
                      insertMention(option);
                    }}
                  >
                    <span className="work-mention-option-head">
                      <Badge
                        className="work-mention-kind"
                        variant={option.kind === "skill" ? "success" : "muted"}
                      >
                        {option.kind === "skill" ? "Skill" : "Workflow"}
                      </Badge>
                      <span className="work-mention-name">@{option.name}</span>
                    </span>
                    <span className="work-mention-description">
                      {option.description}
                    </span>
                  </button>
                ))}
                {mentionLoading && mentionMatches.length === 0 ? (
                  <div className="work-mention-state" role="status">
                    Loading skills and workflows…
                  </div>
                ) : null}
                {mentionSettled && mentionMatches.length === 0 ? (
                  <div className="work-mention-state" role="status">
                    {mentionErrorCount === 2
                      ? "Skills and workflows could not be loaded. Close and reopen the picker to try again."
                      : mention?.query
                        ? `No capability matches @${mention.query}.`
                        : "No skills or workflows are available yet."}
                  </div>
                ) : null}
              </div>
              {mentionActiveOption ? (
                <aside
                  id="work-capability-detail"
                  className="work-mention-detail"
                  aria-live="polite"
                >
                  <Badge
                    className="work-mention-kind"
                    variant={
                      mentionActiveOption.kind === "skill"
                        ? "success"
                        : "muted"
                    }
                  >
                    {mentionActiveOption.kind === "skill"
                      ? "Skill"
                      : "Workflow"}
                  </Badge>
                  <strong>@{mentionActiveOption.name}</strong>
                  <p>{mentionActiveOption.description}</p>
                </aside>
              ) : null}
            </div>
            {mentionErrorCount === 1 ? (
              <p className="work-mention-notice" role="status">
                One capability source is unavailable. Available results are
                still shown.
              </p>
            ) : null}
          </div>
        ) : null}
        {files.length > 0 ? (
          <div className="flex flex-wrap gap-1.5 px-2 pt-2 pb-1">
            {files.map((file, index) => (
              <div key={`${file.name}-${index}`} className="work-file-chip">
                <Paperclip size={11} strokeWidth={2} aria-hidden />
                <span className="max-w-[140px] overflow-hidden text-ellipsis whitespace-nowrap text-text">
                  {file.name}
                </span>
                <span className="text-text3 tabular-nums text-ui-label tracking-wide">
                  {Math.max(1, Math.round(file.size / 1024))} KB
                </span>
                <IconButton
                  label={`Remove ${file.name}`}
                  size="icon-sm"
                  variant="ghost"
                  onClick={() =>
                    setFiles((prev) => prev.filter((_, fileIndex) => fileIndex !== index))
                  }
                >
                  <X size={11} strokeWidth={2.25} />
                </IconButton>
              </div>
            ))}
          </div>
        ) : null}
        <Textarea
          ref={textareaRef}
          className="work-input !min-h-0 !resize-none !border-0 !rounded-none !bg-transparent !shadow-none"
          role="combobox"
          aria-autocomplete="list"
          aria-haspopup="listbox"
          aria-expanded={mentionOpen}
          aria-controls={mentionOpen ? "work-capability-listbox" : undefined}
          aria-activedescendant={
            mentionOpen && mentionActiveOption
              ? `work-capability-option-${mentionActiveIndex}`
              : undefined
          }
          aria-describedby={
            mentionOpen && mentionActiveOption
              ? "work-capability-detail"
              : undefined
          }
          placeholder={placeholder}
          value={draft}
          onChange={(event) =>
            handleDraftChange(
              event.target.value,
              event.target.selectionStart ?? event.target.value.length,
            )
          }
          onKeyDown={(event) => {
            if (mentionOpen) {
              const action = workMentionKeyAction(
                event.key,
                mentionActiveIndex,
                mentionMatches.length,
              );
              if (action) {
                event.preventDefault();
                if (action.type === "close") setMention(null);
                if (action.type === "move") {
                  setMention((current) =>
                    current
                      ? { ...current, activeIndex: action.index }
                      : current,
                  );
                }
                if (action.type === "select") {
                  insertMention(mentionMatches[action.index]);
                }
                return;
              }
              if (event.key === "Enter" && mentionLoading) {
                event.preventDefault();
                return;
              }
            }
            if (event.key === "Enter" && !event.shiftKey && !sending) {
              event.preventDefault();
              onSend();
            }
          }}
          onBlur={() => {
            // Delay so a click on a dropdown option still registers.
            window.setTimeout(() => setMention(null), 120);
          }}
          disabled={sending}
          rows={1}
          autoComplete="off"
          autoCorrect="on"
          spellCheck
          enterKeyHint="send"
        />
        <div className="flex items-center justify-between gap-2.5 px-1.5 py-1 max-[720px]:px-1">
          <div className="inline-flex items-center gap-2 min-w-0">
            <IconButton
              label="Attach a file"
              size="icon-sm"
              variant="ghost"
              className="work-icon-btn"
              onClick={() => fileInputRef.current?.click()}
              title="Attach a file"
              disabled={sending || files.length >= MAX_ATTACHMENTS}
            >
              <Paperclip size={15} strokeWidth={2} />
            </IconButton>
            <span className="work-composer-hint" aria-live="polite">
              {sending ? (
                <span className="work-composer-pulse">OpenNeko is working</span>
              ) : files.length > 0 ? (
                <>{files.length} of {MAX_ATTACHMENTS} attached</>
              ) : (
                <span className="work-composer-keys">Enter to send · Shift + Enter for a new line</span>
              )}
            </span>
          </div>
          {sending ? (
            <Button
              size="sm"
              variant="danger"
              className="work-send-btn is-stop"
              onClick={onStop}
              aria-label="Stop"
            >
              <Square size={13} fill="currentColor" strokeWidth={0} aria-hidden />
              <span>Stop</span>
            </Button>
          ) : (
            <Button
              size="sm"
              variant="primary"
              className="work-send-btn"
              onClick={onSend}
              disabled={!draft.trim() && files.length === 0}
              aria-label={`Send to ${assistantName}`}
            >
              <span>Send</span>
              <ArrowUp size={14} strokeWidth={2.5} aria-hidden />
            </Button>
          )}
        </div>
      </div>

      <Input
        ref={fileInputRef}
        type="file"
        multiple
        hidden
        accept={ACCEPTED_ATTACHMENT_EXTENSIONS.join(",")}
        onChange={(event) => {
          const picked = Array.from(event.target.files ?? []);
          event.target.value = "";
          if (picked.length === 0) return;
          const accepted: File[] = [];
          const rejections: string[] = [];
          const startingCount = files.length;
          for (const file of picked) {
            if (startingCount + accepted.length >= MAX_ATTACHMENTS) {
              rejections.push(`Max ${MAX_ATTACHMENTS} files per message.`);
              break;
            }
            if (file.type.startsWith("image/")) {
              rejections.push(`"${file.name}": image uploads aren't supported yet.`);
              continue;
            }
            if (file.size > MAX_ATTACHMENT_SIZE) {
              rejections.push(`"${file.name}": over ${Math.round(MAX_ATTACHMENT_SIZE / (1024 * 1024))} MB.`);
              continue;
            }
            if (!ACCEPTED_ATTACHMENT_SUFFIXES.has(fileExtension(file.name))) {
              rejections.push(`"${file.name}": unsupported file type.`);
              continue;
            }
            accepted.push(file);
          }
          if (accepted.length > 0) {
            setFiles((prev) => [...prev, ...accepted].slice(0, MAX_ATTACHMENTS));
          }
          onAttachError(rejections.length > 0 ? rejections.join(" · ") : null);
        }}
      />
    </>
  );
}
