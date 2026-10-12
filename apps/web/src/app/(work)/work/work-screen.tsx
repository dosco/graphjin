"use client";

import { acknowledgeWorkStartup, markWorkStartup } from "@/lib/work-startup-timing";

import "@/a2ui/components";
import {
  ArrowUp,
  Check,
  Copy,
  ChevronsDownUp,
  ChevronsUpDown,
  History,
  Loader2,
  Paperclip,
  Pencil,
  Plus,
  Printer,
  RefreshCw,
  ShieldCheck,
  Square,
  X,
} from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import {
  useParams,
  usePathname,
  useRouter,
  useSearchParams,
} from "next/navigation";

// Matches paths to agent-generated/uploaded files inside the per-org workspace.
// Two shapes:
//   - absolute container paths, e.g.
//       ~/.config/openneko/agents/orgs/<orgId>/uploads/<threadId>/file.csv
//   - bare workspace-relative paths, e.g. `uploads/<threadId>/file.csv` — the
//     same form `joinMessageWithAttachments` emits into the user's outgoing
//     message and that the agent passes back when it cites files.
// Capture group 1 is the workspace-relative path the /api/work/files route serves.
const WORKSPACE_FILE_RE =
  /(?:\/agents\/orgs\/[A-Za-z0-9-]+\/|(?<![A-Za-z0-9._\-/]))((?:runs|uploads|skills|memory)\/[A-Za-z0-9._\-/]+\.[A-Za-z0-9]+)/g;

type MdNode = { type: string; value?: string; url?: string; children?: MdNode[] };

function autolinkWorkspaceFiles() {
  return (tree: unknown) => walkMdNode(tree, null);
}

function walkMdNode(node: unknown, parent: MdNode | null): void {
  if (!node || typeof node !== "object") return;
  const n = node as MdNode;
  if (n.type === "code" || n.type === "link") return;
  // inlineCode is the `path` case — assistant often wraps file paths in
  // backticks. Convert the whole inline-code node to a link if its value
  // matches a workspace file path.
  if (n.type === "inlineCode" && typeof n.value === "string" && parent?.children) {
    WORKSPACE_FILE_RE.lastIndex = 0;
    const match = WORKSPACE_FILE_RE.exec(n.value);
    if (match) {
      const idx = parent.children.indexOf(n);
      if (idx !== -1) {
        parent.children.splice(idx, 1, {
          type: "link",
          url: `/api/work/files/${match[1]}`,
          children: [{ type: "inlineCode", value: n.value.split("/").slice(-1)[0] }],
        });
      }
    }
    return;
  }
  if (n.type === "text" && typeof n.value === "string" && parent?.children) {
    const value = n.value;
    WORKSPACE_FILE_RE.lastIndex = 0;
    const matches = [...value.matchAll(WORKSPACE_FILE_RE)];
    if (matches.length === 0) return;
    const replacement: MdNode[] = [];
    let cursor = 0;
    for (const m of matches) {
      const start = m.index ?? 0;
      const end = start + m[0].length;
      if (start > cursor) {
        replacement.push({ type: "text", value: value.slice(cursor, start) });
      }
      replacement.push({
        type: "link",
        url: `/api/work/files/${m[1]}`,
        children: [{ type: "text", value: m[0].split("/").slice(-1)[0] }],
      });
      cursor = end;
    }
    if (cursor < value.length) {
      replacement.push({ type: "text", value: value.slice(cursor) });
    }
    const idx = parent.children.indexOf(n);
    if (idx !== -1) parent.children.splice(idx, 1, ...replacement);
    return;
  }
  if (Array.isArray(n.children)) {
    for (const child of n.children.slice()) walkMdNode(child, n);
  }
}

const REMARK_PLUGINS = [remarkGfm, autolinkWorkspaceFiles];

import {
  WORKSPACE_MARKDOWN_COMPONENTS as MARKDOWN_COMPONENTS,
  linkifyWorkspacePaths,
} from "@/lib/linkify-workspace-paths";
import BriefingCard from "@/components/BriefingCard";
import WorkHistoryDrawer from "@/components/WorkHistoryDrawer";
import BotGreeting from "@/components/chat/BotGreeting";
import { useThreadBot } from "@/components/chat/useThreadBot";
import { ThreadJump } from "@/components/chat/ThreadJump";
import {
  ActionRequestCard,
  RuleSavedCard,
  WorkflowSavedCard,
  extractActionRequestEvents,
  extractRuleSaveEvent,
  extractWorkflowSaveEvent,
  stripNekoFences,
} from "@/components/RuleChatBubble";
import { parseBriefingCardMessage } from "@/lib/briefing-card-context";
import {
  activeWorkMentions,
  appendWorkMentionBlock,
  filterWorkMentions,
  inferTextEdit,
  stripWorkMentionBlock,
  updateDraftWorkMentions,
  workMentionKeyAction,
  type DraftWorkMention,
  type WorkMention,
} from "@/lib/workflow-mention";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { printThread } from "@/lib/print-thread";
import { Button, IconButton } from "@/components/ui/button";
import { Input, Textarea } from "@/components/ui/field";
import { Disclosure } from "@/components/ui/disclosure";
import { LocalDateTime } from "@/components/ui/local-date-time";
import { renderComponent, renderChildren } from "@/a2ui/renderer";
import { applyMessage, getRootComponent, setDataModelValue } from "@/a2ui/surface";
import { buildActionFollowUp, parseClarificationReply } from "@/a2ui/action";
import type { SurfaceState, A2UIMessage } from "@/a2ui/types";
import { useWorkShell } from "../work-shell-context";
import { formatSavedShort } from "@/lib/hours-saved";
import { presentWorkFailure } from "@/lib/work-failure";

type AnswerVital = {
  label: string;
  value: string;
  sub?: string;
  basis?: "observed" | "calculated" | "estimated";
  asOf?: string;
  source?: string;
};

type MessageRecord = {
  id: string;
  runId: string | null;
  role: "user" | "assistant";
  content: string;
  createdAt: string;
};

type RunRecord = {
  id: string;
  backend: "hermes";
  status: string;
  error: string | null;
  createdAt: string;
  finishedAt: string | null;
  analysisMinutesSaved?: number | null;
  analysisMinutesBasis?: string | null;
  actorRole?: "admin" | "member" | "service" | null;
};

export type WorkEvent =
  | { type: "hello"; runId: string; threadId: string; backend?: RunRecord["backend"] }
  // `content` is a delta — concatenating all message events for a run
  // reconstructs the full assistant text. Mirrors `AgentEvent.message` in
  // packages/llm/src/agent-backend.ts.
  | { type: "message"; role: "user" | "assistant"; content: string }
  | { type: "interim"; id: string; content: string; source: "hermes_interim_assistant" }
  | { type: "tool_start"; id: string; name: string; input?: unknown }
  | { type: "tool_delta"; id: string; delta: unknown }
  | { type: "tool_end"; id: string; result?: unknown; error?: string }
  | { type: "surface"; messages: A2UIMessage[] }
  | { type: "artifact"; artifact: { path: string; label: string; mimeType?: string } }
  | { type: "status"; message: string }
  | {
      type: "progress";
      id: string;
      content: string;
      source: "provider_summary";
      provider: "google-gemini" | "anthropic";
    }
  | { type: "error"; message: string }
  | {
      type: "capability_denied";
      capability: "network_egress";
      reason: "policy_denied";
      host: string;
      port?: number;
      method?: string;
      path?: string;
    }
  | {
      type: "action_request_emit";
      action_request_id: string;
      kind: string;
      scope: "internal" | "external";
      risk_level?: string;
      intent?: string;
      summary?: string;
      decision: "auto_approved" | "pending_approval";
    }
  | {
      type: "action_request_result";
      action_request_id: string;
      kind: string;
      status: "succeeded" | "failed" | "rejected" | "partially_applied" | "reconcile_required";
      outcome?: {
        result?: Record<string, unknown> | null;
        externalRef?: string | null;
        commandOrOperation?: string | null;
      };
      error?: string;
      rejection_reason?: string;
    }
  | {
      type: "needs_input";
      question: string;
      options?: string[];
      questions?: Array<{
        id: string;
        header?: string;
        question: string;
        options?: Array<{ label: string; description?: string }>;
      }>;
      reason?: string;
      surfaceId?: string;
    }
  | { type: "followups"; items: string[] }
  | { type: "vitals"; items: AnswerVital[] }
  | { type: "done"; result?: unknown };

type ThreadBundle = {
  thread: {
    id: string;
    title: string;
    createdAt: string;
    updatedAt: string;
    lastMessageAt: string;
  };
  runs: RunRecord[];
  messages: MessageRecord[];
  eventsByRun: Record<string, WorkEvent[]>;
  hasOlder?: boolean;
};

const CHAT_PAGE_RUNS = 20;

function prependOlder(prev: ThreadBundle, older: ThreadBundle): ThreadBundle {
  const runIds = new Set(prev.runs.map((run) => run.id));
  const messageIds = new Set(prev.messages.map((message) => message.id));
  return {
    ...prev,
    runs: [...older.runs.filter((run) => !runIds.has(run.id)), ...prev.runs],
    messages: [...older.messages.filter((message) => !messageIds.has(message.id)), ...prev.messages],
    eventsByRun: { ...older.eventsByRun, ...prev.eventsByRun },
    hasOlder: older.hasOlder,
  };
}

type StreamResult = "done" | "closed";

type ActiveRunStream = {
  threadId: string;
  runId: string;
  close: () => void;
};

type UploadedWorkFile = {
  name: string;
  size: number;
  relativePath: string;
  absolutePath: string;
};

const MAX_ATTACHMENTS = 5;
const MAX_ATTACHMENT_SIZE = 10 * 1024 * 1024;
const ACCEPTED_ATTACHMENT_EXTENSIONS = [
  ".csv",
  ".docx",
  ".html",
  ".json",
  ".md",
  ".pdf",
  ".pptx",
  ".tsv",
  ".txt",
  ".xlsx",
];
const ACCEPTED_ATTACHMENT_SUFFIXES = new Set(ACCEPTED_ATTACHMENT_EXTENSIONS);

function fileExtension(name: string): string {
  const idx = name.lastIndexOf(".");
  return idx > 0 ? name.slice(idx).toLowerCase() : "";
}

type PendingMemory = {
  id: string;
  draftText: string;
  draftKind: string;
  draftScope: string;
  confidence: number;
  reasoning: string | null;
  conflicts: Array<{ memoryId: string; text: string; similarity: number }>;
};

function isRunInFlight(run: RunRecord): boolean {
  return run.status === "queued" || run.status === "running";
}

function latestInFlightRun(runs: RunRecord[]): RunRecord | null {
  for (let i = runs.length - 1; i >= 0; i -= 1) {
    if (isRunInFlight(runs[i])) return runs[i];
  }
  return null;
}

function needsAssistantTimelinePlaceholder(
  run: RunRecord,
  events: WorkEvent[] | undefined,
): boolean {
  return isRunInFlight(run) || Boolean(run.error) || Boolean(events?.length);
}

function withAssistantTimelinePlaceholders(bundle: ThreadBundle): ThreadBundle {
  const placeholders: MessageRecord[] = [];
  const assistantRunIds = new Set(
    bundle.messages
      .filter((message) => message.role === "assistant" && message.runId)
      .map((message) => message.runId as string),
  );

  for (const run of bundle.runs) {
    if (assistantRunIds.has(run.id)) continue;
    if (!needsAssistantTimelinePlaceholder(run, bundle.eventsByRun[run.id])) {
      continue;
    }
    placeholders.push({
      id: `assistant-${run.id}`,
      runId: run.id,
      role: "assistant",
      content: "",
      createdAt: run.createdAt,
    });
  }

  if (placeholders.length === 0) return bundle;

  const messages = [...bundle.messages];
  for (const placeholder of placeholders) {
    let insertAt = messages.length;
    for (let i = messages.length - 1; i >= 0; i -= 1) {
      const message = messages[i];
      if (message.runId === placeholder.runId && message.role === "user") {
        insertAt = i + 1;
        break;
      }
    }
    messages.splice(insertAt, 0, placeholder);
  }

  return { ...bundle, messages };
}

export default function WorkScreen() {
  const router = useRouter();
  const params = useParams();
  const searchParams = useSearchParams();
  const threadBase = usePathname()?.startsWith("/chat/") ? "/chat/t" : "/work";
  const inBotChat = threadBase !== "/work";
  const routeThreadId =
    typeof params?.threadId === "string" ? params.threadId : null;
  const { bot: threadBot } = useThreadBot(routeThreadId, inBotChat);
  const assistantName = threadBot?.name ?? "OpenNeko";
  const {
    setActiveRunId,
    insertComposerRef,
    submitFollowUpRef,
  } = useWorkShell();
  const [gateChecked, setGateChecked] = useState(false);
  const [gateError, setGateError] = useState<string | null>(null);
  const [activeThreadId, setActiveThreadId] = useState<string | null>(null);
  const [bundle, setBundle] = useState<ThreadBundle | null>(null);
  const [pendingMemories, setPendingMemories] = useState<PendingMemory[]>([]);
  const [draft, setDraft] = useState(() => searchParams?.get("seed") ?? "");
  const [files, setFiles] = useState<File[]>([]);
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
  const [loadingThread, setLoadingThread] = useState(false);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const olderSentinelRef = useRef<HTMLDivElement | null>(null);
  const keepOffsetRef = useRef<number | null>(null);
  const loadOlderRef = useRef<() => Promise<boolean>>(async () => false);
  const [sending, setSending] = useState(false);
  const [streamError, setStreamError] = useState<string | null>(null);
  const [activeRunId, setActiveRunIdState] = useState<string | null>(null);
  const [historyOpen, setHistoryOpen] = useState(false);
  const endRef = useRef<HTMLDivElement | null>(null);
  const transcriptRef = useRef<HTMLDivElement | null>(null);
  const [reasoningExpanded, setReasoningExpanded] = useState(false);
  useEffect(() => {
    // Read after hydration so the server and first client render agree.
    const id = window.setTimeout(() => {
      try {
        setReasoningExpanded(window.localStorage.getItem(REASONING_STORAGE_KEY) === "expanded");
      } catch {
        // Storage can be unavailable; reasoning then starts collapsed.
      }
    }, 0);
    return () => window.clearTimeout(id);
  }, []);
  const toggleReasoning = () => {
    setReasoningExpanded((expanded) => {
      try {
        window.localStorage.setItem(REASONING_STORAGE_KEY, expanded ? "collapsed" : "expanded");
      } catch {
        // The choice still applies to this page.
      }
      return !expanded;
    });
  };
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const activeRunStreamRef = useRef<ActiveRunStream | null>(null);
  const activeThreadIdRef = useRef<string | null>(null);
  const mountedRef = useRef(true);

  const updateActiveRunId = (next: string | null) => {
    setActiveRunIdState(next);
    setActiveRunId(next);
  };

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      activeRunStreamRef.current?.close();
      activeRunStreamRef.current = null;
      setActiveRunId(null);
    };
    // setActiveRunId comes from a stable provider; intentional empty deps.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    activeThreadIdRef.current = activeThreadId;
  }, [activeThreadId]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await fetch("/api/onboarding/status");
        const status = await res.json().catch(() => ({ state: "db_error", message: "Could not reach server" }));
        if (cancelled) return;
        if (status.state === "db_error") {
          setGateError(status.message ?? "Database unavailable");
          setGateChecked(true);
          return;
        }
        if (
          status.state === "needs_wizard" ||
          status.state === "needs_persona"
        ) {
          router.replace("/onboarding");
          return;
        }
        if (status.state === "failed") {
          router.replace("/onboarding?failed=1");
          return;
        }
        if (status.state === "processing") {
          router.replace("/business-profile?from=setup");
          return;
        }
        setGateChecked(true);
      } catch (err) {
        if (cancelled) return;
        setGateError(err instanceof Error ? err.message : "Could not reach server");
        setGateChecked(true);
      }
    })();
    return () => { cancelled = true; };
  }, [router]);

  // React to URL thread changes. The /work page (no threadId) is the new-thread
  // state: clear the screen and let the composer create a thread on first send.
  useEffect(() => {
    if (!gateChecked || gateError) return;
    if (routeThreadId) {
      if (routeThreadId !== activeThreadIdRef.current) {
        void loadThread(routeThreadId);
      }
      return;
    }
    void resolveLandingThread();
    // loadThread/resolveLandingThread are stable closures over component state.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [gateChecked, gateError, routeThreadId]);

  // Follow new output only while the reader is at the bottom of a running
  // thread. Opening a thread, scrolling up, or clicking into the transcript
  // (a tool call, reasoning or text) leaves the page where the reader put it.
  const followRef = useRef(false);
  const ignoreScrollUntilRef = useRef(0);
  useEffect(() => {
    const nearBottom = () =>
      window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 120;
    const onScroll = () => {
      if (Date.now() < ignoreScrollUntilRef.current) return;
      followRef.current = nearBottom();
    };
    const stop = () => {
      followRef.current = false;
      ignoreScrollUntilRef.current = 0;
    };
    const onWheel = (event: WheelEvent) => {
      if (event.deltaY < 0) stop();
    };
    const onPointerDown = (event: PointerEvent) => {
      if (transcriptRef.current?.contains(event.target as Node)) stop();
    };
    window.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("wheel", onWheel, { passive: true });
    window.addEventListener("touchmove", stop, { passive: true });
    document.addEventListener("pointerdown", onPointerDown, true);
    return () => {
      window.removeEventListener("scroll", onScroll);
      window.removeEventListener("wheel", onWheel);
      window.removeEventListener("touchmove", stop);
      document.removeEventListener("pointerdown", onPointerDown, true);
    };
  }, []);

  // An Ask thread opens at the top. A bot chat opens at its latest message.
  // Neither is followed until the reader sends.
  useEffect(() => {
    followRef.current = false;
    window.scrollTo({ top: 0 });
  }, [routeThreadId]);

  useEffect(() => {
    const transcript = transcriptRef.current;
    if (!inBotChat || loadingThread || !transcript || !bundle?.messages.length) return;
    const toEnd = () =>
      window.scrollTo({ top: document.documentElement.scrollHeight, behavior: "instant" });
    const resize = new ResizeObserver(toEnd);
    const release = () => resize.disconnect();
    toEnd();
    resize.observe(transcript);
    const timer = window.setTimeout(release, 2000);
    window.addEventListener("wheel", release, { once: true, passive: true });
    window.addEventListener("touchstart", release, { once: true, passive: true });
    return () => {
      window.clearTimeout(timer);
      release();
      window.removeEventListener("wheel", release);
      window.removeEventListener("touchstart", release);
    };
    // Only the end of a thread load moves the reader.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [inBotChat, loadingThread, activeThreadId]);

  useLayoutEffect(() => {
    if (keepOffsetRef.current === null) return;
    window.scrollTo({ top: document.documentElement.scrollHeight - keepOffsetRef.current, behavior: "instant" });
    keepOffsetRef.current = null;
  }, [bundle]);

  useEffect(() => {
    loadOlderRef.current = loadOlder;
  });

  useEffect(() => {
    let lastY = window.scrollY;
    const onScroll = () => {
      const up = window.scrollY < lastY;
      lastY = window.scrollY;
      const top = olderSentinelRef.current?.getBoundingClientRect().bottom;
      if (up && top !== undefined && top > -800) void loadOlderRef.current();
    };
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  // Sending shows the question and the running indicator, then follows.
  useEffect(() => {
    if (!sending) return;
    followRef.current = true;
    ignoreScrollUntilRef.current = Date.now() + 800;
    endRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, [sending]);

  useEffect(() => {
    if ((sending || activeRunId) && followRef.current) endRef.current?.scrollIntoView({ block: "end" });
  }, [bundle, sending, activeRunId]);

  // Register the handle the rail's "Ask next" chips call to drop a question
  // into the composer (focused, caret at end) so the operator can edit it
  // before sending — it never fires on its own. setDraft runs from the click,
  // not synchronously inside this effect.
  useEffect(() => {
    insertComposerRef.current = (q: string) => {
      setDraft(q);
      setDraftMentions([]);
      setMention(null);
      requestAnimationFrame(() => {
        const ta = textareaRef.current;
        if (ta) {
          ta.focus();
          ta.setSelectionRange(ta.value.length, ta.value.length);
        }
      });
    };
    return () => {
      insertComposerRef.current = null;
    };
  }, [insertComposerRef]);

  // Register the handle an interactive A2UI Choice calls to submit a follow-up
  // turn. Re-registered each render so the closure sees the latest thread/state.
  useEffect(() => {
    submitFollowUpRef.current = (prompt: string) => {
      void sendText(prompt);
    };
    return () => {
      submitFollowUpRef.current = null;
    };
  });

  // Auto-grow the textarea up to its max-height (~9 lines); past that the
  // textarea scrolls internally. CSS alone can't do this — `rows={1}` is
  // the floor and there's no `content-size` for textareas. Keep the cap in
  // sync with .work-input { max-height } in globals.css.
  useEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 222)}px`;
  }, [draft]);

  function resolveLandingThread() {
    activeThreadIdRef.current = null;
    setActiveThreadId(null);
    activeRunStreamRef.current?.close();
    activeRunStreamRef.current = null;
    setSending(false);
    updateActiveRunId(null);
    setBundle(null);
    setPendingMemories([]);
  }

  async function loadThread(threadId: string) {
    setLoadingThread(true);
    try {
      const res = await fetch(
        `/api/work/threads/${threadId}${inBotChat ? `?limit=${CHAT_PAGE_RUNS}` : ""}`,
      );
      if (!res.ok) {
        activeRunStreamRef.current?.close();
        activeRunStreamRef.current = null;
        setSending(false);
        updateActiveRunId(null);
        setBundle(null);
        return;
      }
      const data = (await res.json()) as ThreadBundle;
      const nextBundle = withAssistantTimelinePlaceholders(data);
      setBundle(nextBundle);
      activeThreadIdRef.current = threadId;
      setActiveThreadId(threadId);
      resumeLatestInFlightRun(threadId, nextBundle);
      await loadPendingMemories(threadId);
    } finally {
      setLoadingThread(false);
    }
  }

  async function loadOlder(): Promise<boolean> {
    const threadId = activeThreadIdRef.current;
    const before = bundle?.runs[0]?.id;
    if (!threadId || !before || !bundle?.hasOlder || loadingOlder) return false;
    setLoadingOlder(true);
    try {
      const res = await fetch(
        `/api/work/threads/${threadId}?limit=${CHAT_PAGE_RUNS}&before=${encodeURIComponent(before)}`,
      );
      if (!res.ok || activeThreadIdRef.current !== threadId) return false;
      const older = withAssistantTimelinePlaceholders((await res.json()) as ThreadBundle);
      keepOffsetRef.current = document.documentElement.scrollHeight - window.scrollY;
      setBundle((prev) => (prev ? prependOlder(prev, older) : prev));
      return Boolean(older.hasOlder);
    } finally {
      setLoadingOlder(false);
    }
  }

  async function loadAllOlder() {
    const threadId = activeThreadIdRef.current;
    if (!threadId || !bundle?.hasOlder) return;
    const res = await fetch(`/api/work/threads/${threadId}`);
    if (!res.ok || activeThreadIdRef.current !== threadId) return;
    const full = withAssistantTimelinePlaceholders((await res.json()) as ThreadBundle);
    setBundle((prev) => (prev ? prependOlder(prev, { ...full, hasOlder: false }) : prev));
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  }

  async function loadPendingMemories(threadId: string) {
    const res = await fetch(`/api/work/memories/pending?threadId=${encodeURIComponent(threadId)}`);
    if (!res.ok) return;
    const data = (await res.json()) as { pending: PendingMemory[] };
    setPendingMemories(data.pending ?? []);
  }

  async function createThread(): Promise<string> {
    const res = await fetch("/api/work/threads", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({}),
    });
    const data = (await res.json()) as {
      thread: {
        id: string;
        title: string;
        createdAt: string;
        updatedAt: string;
        lastMessageAt: string;
      };
    };
    const nextId = data.thread.id;
    activeThreadIdRef.current = nextId;
    setActiveThreadId(nextId);
    setBundle({
      thread: data.thread,
      runs: [],
      messages: [],
      eventsByRun: {},
    });
    setPendingMemories([]);
    router.replace(`${threadBase}/${nextId}`);
    return nextId;
  }

  async function uploadFiles(
    threadId: string,
    picked: File[],
  ): Promise<{ uploaded: UploadedWorkFile[]; errors: string[] }> {
    const uploaded: UploadedWorkFile[] = [];
    const errors: string[] = [];
    for (const file of picked) {
      const body = new FormData();
      body.append("threadId", threadId);
      body.append("file", file);
      let res: Response;
      try {
        res = await fetch("/api/work/upload", { method: "POST", body });
      } catch (err) {
        errors.push(`"${file.name}": ${err instanceof Error ? err.message : "network error"}`);
        continue;
      }
      if (!res.ok) {
        const payload = (await res.json().catch(() => null)) as { error?: string } | null;
        errors.push(`"${file.name}": ${payload?.error ?? `upload failed (HTTP ${res.status})`}`);
        continue;
      }
      const data = (await res.json()) as { file: UploadedWorkFile };
      uploaded.push(data.file);
    }
    return { uploaded, errors };
  }

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

  async function sendMessage() {
    const submittedAt = performance.now();
    const trimmed = draft.trim();
    if (!trimmed && files.length === 0) return;

    setSending(true);
    setStreamError(null);

    let threadId = activeThreadId;
    if (!threadId) {
      threadId = await createThread();
    }

    let uploads: UploadedWorkFile[] = [];
    if (threadId && files.length > 0) {
      const result = await uploadFiles(threadId, files);
      uploads = result.uploaded;
      if (result.errors.length > 0) {
        setStreamError(result.errors.join(" · "));
      }
      if (uploads.length === 0 && result.errors.length > 0) {
        setSending(false);
        return;
      }
    }
    const selectedMentions = activeWorkMentions(draft, draftMentions);
    const message = appendWorkMentionBlock(
      joinMessageWithAttachments(trimmed, uploads),
      selectedMentions,
    );
    const tempMessage: MessageRecord = {
      id: `temp-${Date.now()}`,
      runId: null,
      role: "user",
      content: message,
      createdAt: new Date().toISOString(),
    };
    setBundle((prev) =>
      prev
        ? { ...prev, messages: [...prev.messages, tempMessage] }
        : prev,
    );
    setDraft("");
    setFiles([]);
    setDraftMentions([]);
    setMention(null);

    await postAndStreamRun(threadId, message, submittedAt);
  }

  // Submit a follow-up turn directly (no composer round-trip) — the landing
  // for an interactive A2UI Choice click. Mirrors sendMessage's tail.
  async function sendText(text: string) {
    const trimmed = text.trim();
    if (!trimmed || sending) return;
    setSending(true);
    setStreamError(null);
    let threadId = activeThreadId;
    if (!threadId) threadId = await createThread();
    if (!threadId) {
      setSending(false);
      return;
    }
    const tempMessage: MessageRecord = {
      id: `temp-${Date.now()}`,
      runId: null,
      role: "user",
      content: trimmed,
      createdAt: new Date().toISOString(),
    };
    setBundle((prev) =>
      prev ? { ...prev, messages: [...prev.messages, tempMessage] } : prev,
    );
    await postAndStreamRun(threadId, trimmed);
  }

  async function copyUserMessage(text: string) {
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      // clipboard permission denied or insecure context — silently no-op
    }
  }

  async function retryOrEditUserMessage(
    messageId: string,
    text: string,
  ): Promise<void> {
    if (sending) return;
    if (!activeThreadId || !text.trim()) return;
    const threadId = activeThreadId;
    setSending(true);
    setStreamError(null);
    if (activeRunId) {
      await fetch(`/api/work/runs/${activeRunId}/cancel`, { method: "POST" }).catch(
        () => {},
      );
    }
    const res = await fetch(`/api/work/threads/${threadId}/truncate`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ messageId }),
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({}));
      setSending(false);
      setStreamError(err.error ?? `Could not truncate (HTTP ${res.status})`);
      return;
    }
    await loadThread(threadId);
    setBundle((prev) =>
      prev
        ? {
            ...prev,
            messages: [
              ...prev.messages,
              {
                id: `temp-${Date.now()}`,
                runId: null,
                role: "user",
                content: text,
                createdAt: new Date().toISOString(),
              },
            ],
          }
        : prev,
    );
    await postAndStreamRun(threadId, text);
  }

  async function postAndStreamRun(threadId: string, message: string, submittedAt = performance.now()) {
    // Every entry point must expose the live-run controls. Retry/edit reloads
    // the truncated thread first, and that reload clears `sending` when no
    // earlier run remains in flight.
    setSending(true);
    const body = JSON.stringify({ message });
    const res = await fetch(`/api/work/threads/${threadId}/runs`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      ...(body.length < 60_000 ? { keepalive: true } : {}),
      body,
    });
    if (!res.ok) {
      const errBody = await res.json().catch(() => ({}));
      setSending(false);
      setStreamError(errBody.error ?? `HTTP ${res.status}`);
      await loadThread(threadId);
      return;
    }
    const { runId, actorRole } = (await res.json()) as {
      runId: string;
      actorRole?: RunRecord["actorRole"];
    };
    acknowledgeWorkStartup(threadId, runId, submittedAt);
    if (!mountedRef.current) return;

    updateActiveRunId(runId);
    setBundle((prev) =>
      prev
        ? {
            ...prev,
            messages: [
              ...prev.messages.map((message, index) =>
                index === prev.messages.length - 1 &&
                message.role === "user" &&
                message.runId === null
                  ? { ...message, runId }
                  : message,
              ),
              {
                id: `assistant-${runId}`,
                runId,
                role: "assistant" as const,
                content: "",
                createdAt: new Date().toISOString(),
              },
            ],
            runs: [
              ...prev.runs,
              {
                id: runId,
                backend: "hermes",
                status: "running",
                error: null,
                createdAt: new Date().toISOString(),
                finishedAt: null,
                analysisMinutesSaved: null,
                analysisMinutesBasis: null,
                actorRole: actorRole ?? null,
              },
            ],
          }
        : prev,
    );

    await streamAndRefreshRun(threadId, runId, 0);
  }

  function resumeLatestInFlightRun(threadId: string, nextBundle: ThreadBundle) {
    const run = latestInFlightRun(nextBundle.runs);
    const current = activeRunStreamRef.current;
    if (current && (!run || current.threadId !== threadId || current.runId !== run.id)) {
      current.close();
    }

    if (!run) {
      if (activeThreadIdRef.current === threadId) {
        setSending(false);
        updateActiveRunId(null);
      }
      return;
    }

    setStreamError(null);
    setSending(true);
    updateActiveRunId(run.id);
    // The loaded bundle already holds this in-flight run's persisted events.
    // Resuming replays them from the start (afterId 0), so drop the local copy
    // first — otherwise replayed events append as duplicates (doubled tool rows
    // and assistant text).
    setBundle((prev) =>
      prev
        ? { ...prev, eventsByRun: { ...prev.eventsByRun, [run.id]: [] } }
        : prev,
    );
    void streamAndRefreshRun(threadId, run.id, 0);
  }

  async function streamAndRefreshRun(
    threadId: string,
    runId: string,
    afterId: number,
  ) {
    const result = await followRunEvents(threadId, runId, afterId);
    if (result !== "done" || !mountedRef.current) return;
    if (activeThreadIdRef.current !== threadId) return;

    setSending(false);
    setStreamError(null);
    updateActiveRunId(null);
    await loadThread(threadId);
    window.setTimeout(() => {
      if (!mountedRef.current || activeThreadIdRef.current !== threadId) return;
      void loadPendingMemories(threadId);
    }, 1500);
  }

  async function followRunEvents(
    threadId: string,
    runId: string,
    afterId: number,
  ): Promise<StreamResult> {
    const current = activeRunStreamRef.current;
    if (current?.runId === runId && current.threadId === threadId) {
      return "closed";
    }

    current?.close();

    const params = afterId > 0 ? `?afterId=${afterId}` : "";
    return new Promise<StreamResult>((resolve) => {
      let settled = false;
      const es = new EventSource(
        `/api/work/threads/${threadId}/runs/${runId}/events${params}`,
      );
      const settle = (result: StreamResult) => {
        if (settled) return;
        settled = true;
        es.close();
        const active = activeRunStreamRef.current;
        if (active?.runId === runId && active.threadId === threadId) {
          activeRunStreamRef.current = null;
        }
        resolve(result);
      };

      activeRunStreamRef.current = {
        threadId,
        runId,
        close: () => settle("closed"),
      };

      es.onopen = () => markWorkStartup(runId, "streamOpenMs");
      es.onmessage = (msgEvent) => {
        let event: WorkEvent;
        try {
          event = JSON.parse(msgEvent.data) as WorkEvent;
        } catch {
          return;
        }
        if (event.type === "hello") return;
        applyIncomingEvent(runId, event);
        markWorkStartup(runId, "firstEventMs");
        if ((event.type === "message" && event.role === "assistant" && event.content) || event.type === "surface") markWorkStartup(runId, "firstOutputMs");
        if (event.type === "done") markWorkStartup(runId, "doneMs");
        if (event.type === "done") settle("done");
      };
      es.onerror = () => {
        // EventSource will reconnect automatically, carrying Last-Event-ID
        // when the server closes a long poll before the worker is finished.
      };
    });
  }

  function applyIncomingEvent(runId: string, event: WorkEvent) {
    if (event.type === "message" && event.role === "assistant") {
      // `content` is a delta — append to the assistant message's running text
      // (the placeholder created in sendMessage already exists with content "").
      setBundle((prev) => {
        if (!prev) return prev;
        return {
          ...prev,
          messages: prev.messages.map((message) =>
            message.runId === runId && message.role === "assistant"
              ? { ...message, content: message.content + event.content }
              : message,
          ),
        };
      });
    }

    setBundle((prev) => {
      if (!prev) return prev;
      const currentEvents = prev.eventsByRun[runId] ?? [];
      const nextEvents =
        event.type === "done"
          ? [...currentEvents, event]
          : [...currentEvents, event];
      const nextRuns = prev.runs.map((run) =>
        run.id === runId
          ? {
              ...run,
              status:
                event.type === "done"
                  ? String((event.result as { status?: string } | undefined)?.status ?? run.status)
                  : event.type === "error"
                  ? "failed"
                  : run.status,
              error: event.type === "error"
                ? event.message
                : runWasCancelled(event) ? null : run.error,
            }
          : run,
      );
      return {
        ...prev,
        runs: nextRuns,
        eventsByRun: {
          ...prev.eventsByRun,
          [runId]: nextEvents,
        },
      };
    });

    if (event.type === "error") {
      setStreamError(event.message);
    }
    // A stop the operator asked for is not a failure.
    if (runWasCancelled(event)) setStreamError(null);
  }

  async function cancelRun() {
    if (!activeRunId) return;
    await fetch(`/api/work/runs/${activeRunId}/cancel`, { method: "POST" });
  }

  async function decidePendingMemory(
    id: string,
    action: "accept" | "decline",
    overrides: { scope?: string; scopeId?: string | null } = {},
  ) {
    const res = await fetch("/api/work/memories/decide", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id, action, ...overrides }),
    });
    if (!res.ok) return;
    if (activeThreadId) await loadPendingMemories(activeThreadId);
  }

  const runLookup = useMemo(() => {
    const map = new Map<string, RunRecord>();
    for (const run of bundle?.runs ?? []) map.set(run.id, run);
    return map;
  }, [bundle?.runs]);
  const workPhase =
    sending || activeRunId
      ? "running"
      : bundle?.messages.length
        ? "result"
        : "prompt";
  const workStateLabel =
    workPhase === "running"
      ? "OpenNeko is working"
      : workPhase === "result"
        ? "Answer available"
        : "Ready";
  const threadTitle =
    !bundle?.messages.length ||
    !bundle.thread.title.trim() ||
    /^untitled thread$/i.test(bundle.thread.title.trim())
      ? "Ask"
      : bundle.thread.title;

  if (!gateChecked) {
    return (
      <div className="work-gate-state" role="status">
        <span className="work-gate-mark" aria-hidden="true" />
        <strong>Loading work</strong>
        <p>Checking your workspace and agent context.</p>
      </div>
    );
  }

  if (gateError) {
    return (
      <div className="work-gate-state is-error" role="alert">
        <span className="work-gate-mark" aria-hidden="true" />
        <strong>Workspace unavailable</strong>
        <p>OpenNeko cannot reach the database. No work has been started.</p>
        <Button
          size="sm"
          onClick={() => { setGateError(null); setGateChecked(false); window.location.reload(); }}
        >
          Retry
        </Button>
      </div>
    );
  }

  return (
    <div className="work-command-surface">
      {threadBase === "/work" || bundle?.messages.length ? (
      <header className="work-command-head">
        <div className="work-command-copy">
          {inBotChat ? null : workPhase !== "prompt" ? (
            <div className="work-command-eyebrow">
              <span>Work</span>
              <span className="work-command-slash" aria-hidden="true">/</span>
              <span data-phase={workPhase}>{workStateLabel}</span>
            </div>
          ) : null}
          {inBotChat ? null : <h1 title={threadTitle}>{threadTitle}</h1>}
        </div>
        <div className="work-command-actions">
          {bundle?.messages.length ? (
            <Button
              variant="secondary"
              className="work-command-action"
              onClick={toggleReasoning}
              aria-pressed={reasoningExpanded}
            >
              {reasoningExpanded ? <ChevronsDownUp aria-hidden="true" strokeWidth={1.9} /> : <ChevronsUpDown aria-hidden="true" strokeWidth={1.9} />}
              <span>{reasoningExpanded ? "Collapse reasoning" : "Expand reasoning"}</span>
            </Button>
          ) : null}
          {bundle?.messages.length ? (
            <Button
              variant="secondary"
              className="work-command-action"
              onClick={() => void loadAllOlder().then(() => printThread())}
              title="Save this thread as a PDF"
            >
              <Printer aria-hidden="true" strokeWidth={1.9} />
              <span>Save as PDF</span>
            </Button>
          ) : null}
          {threadBase === "/work" ? (
          <Button
            variant="secondary"
            className="work-command-action"
            onClick={() => setHistoryOpen(true)}
            aria-expanded={historyOpen}
            aria-haspopup="dialog"
          >
            <History aria-hidden="true" strokeWidth={1.9} />
            <span>History</span>
          </Button>
          ) : null}
          {threadBase === "/work" && (activeThreadId || bundle?.messages.length) ? (
            <Button
              variant="primary"
              className="work-command-action is-primary"
              onClick={() => router.push(threadBase)}
            >
              <Plus aria-hidden="true" strokeWidth={2} />
              <span>New question</span>
            </Button>
          ) : null}
        </div>
      </header>
      ) : null}

      <WorkHistoryDrawer
        open={historyOpen}
        onClose={() => setHistoryOpen(false)}
      />

      <div className="work-transcript" ref={transcriptRef}>
        {bundle?.messages.length ? (
          <header className="work-print-head" aria-hidden="true">
            <span>OpenNeko · Ask</span>
            <h1>{threadTitle}</h1>
            {bundle.thread?.lastMessageAt ?? bundle.thread?.updatedAt ? (
              <LocalDateTime value={(bundle.thread.lastMessageAt ?? bundle.thread.updatedAt)!} />
            ) : null}
          </header>
        ) : null}
        {loadingThread ? (
          <div className="work-loading-state" role="status">
            <span />
            Loading the work trace
          </div>
        ) : !bundle?.messages.length && threadBase !== "/work" ? (
          <BotGreeting
            threadId={routeThreadId}
            onPick={(text) => {
              setDraft(text);
              requestAnimationFrame(() => textareaRef.current?.focus());
            }}
          />
        ) : !bundle?.messages.length ? (
          <EmptyAsk
            onPick={(text) => {
              setDraft(text);
              requestAnimationFrame(() => textareaRef.current?.focus());
            }}
          />
        ) : null}
        {bundle?.hasOlder && !loadingThread ? (
          <div ref={olderSentinelRef} className="thread-older" role="status">
            {loadingOlder ? "Loading earlier messages" : ""}
          </div>
        ) : null}
        {bundle?.messages.length ? (
          bundle.messages.flatMap((message, index, arr) => {
            if (message.role === "assistant" && message.runId) {
              // Assistant turns are reconstructed chronologically from the
              // run event stream (text segments interleaved with tool calls
              // and the final surface artifact). The persisted message
              // content is the fallback for runs missing event history.
              const events = bundle.eventsByRun[message.runId] ?? [];
              const run = runLookup.get(message.runId) ?? null;
              const isPending =
                (run ? isRunInFlight(run) : false) ||
                (sending && activeRunId === message.runId);
              return (
                <div key={`${message.id}-${index}`} className="flex flex-col gap-2.5">
                  <RunTimeline
                    threadId={activeThreadId ?? ""}
                    run={run}
                    events={events}
                    pending={isPending}
                    fallbackContent={message.content}
                    reasoningExpanded={reasoningExpanded}
                  />
                </div>
              );
            }
            const isPersistedUser =
              message.role === "user" &&
              !!message.runId &&
              !message.id.startsWith("temp-");
            // Briefing-card context messages: when the user opens a
            // deep-dive from the dashboard, the seed message stored at
            // thread-creation time encodes the full card payload after
            // the BRIEFING_CARD_SENTINEL marker. Render it as a real
            // BriefingCard so the user sees the same chrome they
            // clicked, not the raw JSON.
            const briefingCardCtx =
              message.role === "user"
                ? parseBriefingCardMessage(message.content)
                : null;
            const clarificationReply =
              message.role === "user"
                ? parseClarificationReply(message.content)
                : null;
            // If this user message's run terminated (cancelled/failed)
            // and the next message is NOT the assistant reply for it,
            // render a status badge so the cancelled run is visible.
            const orphanRun =
              message.role === "user" && message.runId
                ? runLookup.get(message.runId)
                : null;
            const nextIsAssistantForRun =
              arr[index + 1]?.role === "assistant" &&
              arr[index + 1]?.runId === message.runId;
            const showRunBadge =
              orphanRun &&
              !nextIsAssistantForRun &&
              (orphanRun.status === "cancelled" ||
                orphanRun.status === "failed");
            return [
              <div key={`${message.id}-${index}`} className="flex flex-col gap-2.5">
                {briefingCardCtx ? (
                  <div className="flex flex-col gap-2 mb-1">
                    <div className="inline-flex items-center gap-2.5 font-display text-ui-caption font-semibold text-text3">
                      <span aria-hidden="true" className="w-6 h-px bg-border" />
                      From your briefing
                    </div>
                    <BriefingCard ins={briefingCardCtx} index={0} />
                  </div>
                ) : (
                  <MessageBubble
                    message={message}
                    onCopy={
                      isPersistedUser
                        ? () =>
                            void copyUserMessage(
                              clarificationReply
                                ? clarificationReply.map(({ question, answer }) => `${question}\n${answer}`).join("\n\n")
                                : stripWorkMentionBlock(message.content),
                            )
                        : undefined
                    }
                    onRetry={
                      isPersistedUser && !sending
                        ? () =>
                            void retryOrEditUserMessage(
                              message.id,
                              message.content,
                            )
                        : undefined
                    }
                    onEdit={
                      isPersistedUser && !sending && !clarificationReply
                        ? (text) =>
                            void retryOrEditUserMessage(message.id, text)
                        : undefined
                    }
                  />
                )}
              </div>,
              ...(showRunBadge && orphanRun
                ? [
                    <div
                      key={`run-status-${orphanRun.id}`}
                      className={`rounded-xl px-3 py-1.5 text-xs mx-auto mb-1 w-fit tracking-[0.01em] ${
                        orphanRun.status === "cancelled"
                          ? "bg-neutral-soft text-text2"
                          : "bg-warn-soft text-warn-ink"
                      }`}
                    >
                      {orphanRun.status === "cancelled"
                        ? "Cancelled by user"
                        : `Run failed${orphanRun.error ? `: ${orphanRun.error}` : ""}`}
                    </div>,
                  ]
                : []),
            ];
          })
        ) : null}

        {streamError ? <WorkFailureNotice message={streamError} /> : null}
        <div ref={endRef} />
      </div>

      <div className="work-composer">
        {inBotChat && bundle?.messages.length ? <ThreadJump onTop={loadAllOlder} /> : null}
        {pendingMemories.length > 0 ? (
          <PendingMemoryPanel
            pending={pendingMemories}
            threadId={activeThreadId}
            onDecide={(id, action, overrides) =>
              void decidePendingMemory(id, action, overrides)
            }
          />
        ) : null}

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
            placeholder={
              sending
                ? `${assistantName} is working…`
                : threadBot
                  ? `Ask ${threadBot.name}…`
                  : "Ask a question or describe a task…"
            }
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
                void sendMessage();
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
                onClick={() => void cancelRun()}
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
                onClick={() => void sendMessage()}
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
            setStreamError(rejections.length > 0 ? rejections.join(" · ") : null);
          }}
        />
      </div>
    </div>
  );
}

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

function runWasCancelled(event: { type: string; result?: unknown }): boolean {
  return event.type === "done" && (event.result as { status?: string } | undefined)?.status === "cancelled";
}

function EmptyAsk({ onPick }: { onPick: (text: string) => void }) {
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

function PendingMemoryPanel({
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

function MessageBubble({
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

type ToolItem = {
  id: string;
  name: string;
  input?: unknown;
  deltas: unknown[];
  end?: Extract<WorkEvent, { type: "tool_end" }>;
  approval?: ApprovalItem;
};

type ApprovalItem = {
  actionRequestId: string;
  actionKind: string;
  intent: string | null;
  summary: string | null;
  decision: "auto_approved" | "pending_approval";
  result:
    | Extract<WorkEvent, { type: "action_request_result" }>
    | null;
};

type TimelineItem =
  | { kind: "text"; content: string }
  | { kind: "interim"; id: string; content: string }
  | { kind: "progress"; id: string; content: string }
  | { kind: "tools"; tools: ToolItem[] }
  | { kind: "surface"; messages: A2UIMessage[] }
  | {
      kind: "capability";
      denial: Extract<WorkEvent, { type: "capability_denied" }>;
    }
  | {
      kind: "needs_input";
      request: Extract<WorkEvent, { type: "needs_input" }>;
    }
  | { kind: "error"; message: string }
  | { kind: "stopped" };

type RunArtifact = Extract<WorkEvent, { type: "artifact" }>["artifact"];

function surfaceComponents(messages: A2UIMessage[]) {
  const components: Array<Record<string, unknown>> = [];
  for (const message of messages) {
    if ("createSurface" in message && Array.isArray(message.createSurface.components)) {
      components.push(
        ...(message.createSurface.components as Array<Record<string, unknown>>),
      );
    }
    if ("updateComponents" in message) {
      components.push(
        ...(message.updateComponents.components as Array<Record<string, unknown>>),
      );
    }
  }
  const latest = new Map<string, Record<string, unknown>>();
  for (const component of components) {
    if (typeof component.id === "string") latest.set(component.id, component);
  }
  return latest;
}

function surfaceIdOf(messages: A2UIMessage[]): string | null {
  for (const message of messages) {
    if ("createSurface" in message) return message.createSurface.surfaceId;
    if ("updateComponents" in message) return message.updateComponents.surfaceId;
    if ("updateDataModel" in message) return message.updateDataModel.surfaceId;
    if ("deleteSurface" in message) return message.deleteSurface.surfaceId;
  }
  return null;
}

function canonicalAnswerCreateId(message: A2UIMessage): string | null {
  if (!("createSurface" in message)) return null;
  return isCanonicalAnswerSurface([message])
    ? message.createSurface.surfaceId
    : null;
}

function isCanonicalAnswerSurface(messages: A2UIMessage[]): boolean {
  const components = surfaceComponents(messages);
  const hasAnswer = [...components.values()].some(
    (component) => component.component === "Answer",
  );
  const carriesAnswer = [...components.values()].some((component) =>
    ["Markdown", "Table", "Chart", "KeyFigures", "MetricCard", "Callout"].includes(
      String(component.component),
    ),
  );
  return hasAnswer && carriesAnswer;
}

function withVitalProvenance(
  items: AnswerVital[],
  sources: string[],
  deniedNetwork: boolean,
): AnswerVital[] {
  return items.map((item) => ({
    ...item,
    basis:
      item.basis ??
      (deniedNetwork && sources.length === 0 ? "estimated" : "calculated"),
    source: item.source ?? sources[0],
  }));
}

/**
 * Existing answers used a Row of MetricCards. Replay them as the new ruled
 * KeyFigures component, and add missing per-run vitals to otherwise complete
 * Answer surfaces. Persisted protocol messages stay immutable.
 */
function ownVitalsBySurface(
  messages: A2UIMessage[],
  vitals: AnswerVital[],
  sources: string[],
  deniedNetwork: boolean,
): A2UIMessage[] {
  if (!isCanonicalAnswerSurface(messages)) return messages;
  const components = surfaceComponents(messages);
  const figures = withVitalProvenance(vitals, sources, deniedNetwork);
  const metricCards = [...components.values()].filter(
    (component) => component.component === "MetricCard",
  );
  const fallbackFigures: AnswerVital[] = metricCards.map((card) => ({
    label: typeof card.label === "string" ? card.label : "Figure",
    value: typeof card.metric === "string" ? card.metric : "—",
    sub: typeof card.text === "string" ? card.text : undefined,
    basis: deniedNetwork ? "estimated" : "calculated",
    source: sources[0],
  }));
  const ownedFigures = figures.length > 0 ? figures : fallbackFigures;
  if (ownedFigures.length === 0) return messages;

  const existing = [...components.values()].find(
    (component) => component.component === "KeyFigures",
  );
  const metricRow = [...components.values()].find((component) => {
    if (component.component !== "Row" || !Array.isArray(component.children)) {
      return false;
    }
    return (
      component.children.length > 0 &&
      component.children.every(
        (id) => components.get(String(id))?.component === "MetricCard",
      )
    );
  });
  const answer = [...components.values()].find(
    (component) => component.component === "Answer",
  );
  const surfaceId = surfaceIdOf(messages);
  if (!surfaceId || !answer) return messages;

  if (existing || metricRow) {
    const target = existing ?? metricRow!;
    return [
      ...messages,
      {
        version: "v1.0",
        updateComponents: {
          surfaceId,
          components: [
            {
              id: String(target.id),
              component: "KeyFigures",
              items: ownedFigures,
            },
          ],
        },
      },
    ];
  }

  const children = Array.isArray(answer.children)
    ? answer.children.map(String)
    : [];
  return [
    ...messages,
    {
      version: "v1.0",
      updateComponents: {
        surfaceId,
          components: [
            {
              ...answer,
              id: String(answer.id),
              component: "Answer",
              children: [...children, "__run_key_figures"],
            },
          {
            id: "__run_key_figures",
            component: "KeyFigures",
            items: ownedFigures,
          },
        ],
      },
    },
  ];
}

function fallbackAnswerSurface(
  runId: string,
  raw: string,
  vitals: AnswerVital[],
  sources: string[],
  deniedNetwork: boolean,
): A2UIMessage[] {
  const text = stripNekoFences(raw).trim();
  const figures = withVitalProvenance(vitals, sources, deniedNetwork);
  const children = [
    ...(text ? ["__fallback_markdown"] : []),
    ...(figures.length > 0 ? ["__fallback_figures"] : []),
  ];
  if (children.length === 0) return [];
  return [
    {
      version: "v1.0",
      createSurface: {
        surfaceId: `answer-${runId}`,
        catalogId: "urn:openneko:catalog:work:v2",
        components: [
          {
            id: "root",
            component: "Answer",
            title: "",
            children,
          },
          ...(text
            ? [
                {
                  id: "__fallback_markdown",
                  component: "Markdown",
                  text,
                },
              ]
            : []),
          ...(figures.length > 0
            ? [
                {
                  id: "__fallback_figures",
                  component: "KeyFigures",
                  items: figures,
                },
              ]
            : []),
        ],
        dataModel: {},
      },
    },
  ];
}

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

const TRAIL_KINDS = new Set<TimelineItem["kind"]>(["progress", "tools", "interim"]);
const REASONING_STORAGE_KEY = "openneko.work.reasoning";

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

function workDuration(run: RunRecord | null): string | null {
  if (!run?.finishedAt) return null;
  const ms = Date.parse(run.finishedAt) - Date.parse(run.createdAt);
  return Number.isFinite(ms) && ms >= 0 ? formatElapsed(ms) : null;
}

/** Elapsed time since `since`, refreshed every second while `active`. */
function useElapsed(since: string | undefined, active: boolean): string | null {
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

function RunTimeline({
  threadId,
  run,
  events,
  pending,
  fallbackContent,
  reasoningExpanded,
}: {
  threadId: string;
  run: RunRecord | null;
  events: WorkEvent[];
  pending: boolean;
  fallbackContent: string;
  /** Show every reasoning note and tool call in full, in every run. */
  reasoningExpanded: boolean;
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
    <div className="work-timeline flex flex-col gap-2.5 mt-1">
      {foldTrail ? (
        <WorkTrail run={run} items={trail.map(({ item }) => item)}>
          {trail.map(({ item, index }) => renderItem(item, index))}
        </WorkTrail>
      ) : null}
      {approvals.map((tool) => (
        <ActionApprovalRow key={`approval-${tool.id}`} threadId={threadId} runId={run?.id ?? ""} tool={tool} />
      ))}
      {presentation.items.map((item, index) =>
        foldTrail && TRAIL_KINDS.has(item.kind) ? null : renderItem(item, index),
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

const ANSWER_CARD_PLACEHOLDER_MS = 60_000;

/**
 * A long run shows where its answer will appear, with the step it is on and
 * the data it has read, until the agent sends its card.
 */
function AnswerInProgress({ since, step, sources }: { since?: string; step: string | null; sources: string[] }) {
  const [shown, setShown] = useState(false);
  useEffect(() => {
    const started = since ? Date.parse(since) : Date.now();
    const wait = Math.max(0, ANSWER_CARD_PLACEHOLDER_MS - (Date.now() - started));
    const id = window.setTimeout(() => setShown(true), wait);
    return () => window.clearTimeout(id);
  }, [since]);
  if (!shown) return null;
  return (
    <div className="work-surface-frame answer-in-progress" role="status" aria-live="polite">
      <div className="work-surface">
        <div className="answer-draft-note">
          <span className="answer-draft-pulse" aria-hidden="true" />
          Preparing the answer
        </div>
        {step ? <div className="answer-in-progress-step">{step}</div> : null}
        {sources.length > 0 ? (
          <div className="answer-in-progress-sources">
            <span>Read so far</span>
            <strong>{sources.slice(0, 6).join(" · ")}{sources.length > 6 ? ` · ${sources.length - 6} more` : ""}</strong>
          </div>
        ) : null}
        <div className="answer-in-progress-figures" aria-hidden="true">
          <Skeleton className="h-16" />
          <Skeleton className="h-16" />
          <Skeleton className="h-16" />
        </div>
        <Skeleton className="mt-3 h-28" />
      </div>
    </div>
  );
}

function WorkFailureNotice({ message }: { message: string }) {
  const failure = presentWorkFailure(message);
  return (
    <div
      className="rounded-2xl border border-warn/40 bg-warn-soft px-3 py-2.5 text-ui-body-sm text-warn-ink"
      role="alert"
    >
      <p className="font-semibold">{failure.summary}</p>
      <Disclosure
        title="Technical details"
        className="mt-2 border-warn/40 bg-card/70"
      >
        <p className="break-words font-mono text-ui-caption text-text2">
          {failure.detail}
        </p>
      </Disclosure>
    </div>
  );
}

function NeedsInputNotice({
  request,
  onRespond,
}: {
  request: Extract<WorkEvent, { type: "needs_input" }>;
  onRespond: (text: string) => void;
}) {
  const questions: NonNullable<typeof request.questions> = request.questions?.length
    ? request.questions
    : [{
        id: "q1",
        header: undefined,
        question: request.question,
        options: request.options?.map((label) => ({ label })),
      }];
  return (
    <section className="work-surface-frame" aria-label="Clarification needed">
      <div className="work-surface">
        <div className="work-surface-eyebrow">
          <span className="work-surface-eyebrow-rule" aria-hidden="true" />
          Clarification
        </div>
        <div className="work-surface-title">A detail before I continue</div>
        {request.reason ? <div className="work-surface-sub">{request.reason}</div> : null}
        <div className="work-a2ui-layout is-column">
          {questions.map((question) => (
            <div key={question.id} className="work-a2ui-field">
              <span className="work-a2ui-label">
                {question.header ? `${question.header}: ` : ""}{question.question}
              </span>
              {question.options?.length ? (
                <div className="work-a2ui-chips">
                  {question.options.map((option) => (
                    <Button
                      key={option.label}
                      size="sm"
                      variant="secondary"
                      className="work-choice-btn"
                      onClick={() => onRespond(`${question.question}\n\n${option.label}`)}
                    >
                      <span>{option.label}</span>
                      <span aria-hidden="true">→</span>
                    </Button>
                  ))}
                </div>
              ) : (
                <Button
                  size="sm"
                  className="work-a2ui-button"
                  onClick={() => onRespond(`${question.question}\n\n`)}
                >
                  Answer in composer
                </Button>
              )}
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

function CapabilityDeniedNotice({
  denial,
  canAdminister,
  onRequest,
}: {
  denial: Extract<WorkEvent, { type: "capability_denied" }>;
  canAdminister: boolean;
  onRequest: (prompt: string) => void;
}) {
  const endpoint = `${denial.host}${denial.port ? `:${denial.port}` : ""}`;
  const requestPrompt = [
    `Find an appropriate approved integration that can securely access ${endpoint}`,
    "for the request I just made.",
    "Show me the exact integration and file an approval-gated install request.",
    "Do not enable blanket network access.",
  ].join(" ");
  return (
    <section className="work-capability-denied" role="status">
      <div className="work-capability-kicker">Network access blocked</div>
      <div className="work-capability-grid">
        <div>
          <h3>{endpoint} is outside this workspace</h3>
          <p>
            OpenNeko did not receive live data. The sandbox stayed
            default-deny, so this answer must not present fallback estimates as
            current facts.
          </p>
          <code>
            {denial.method ?? "REQUEST"} {denial.path ?? "/"}
          </code>
        </div>
        <div className="work-capability-recovery">
          <span>Safe recovery</span>
          {canAdminister ? (
            <>
              <p>
                Ask OpenNeko to find the right integration. Installation will
                still require your explicit approval.
              </p>
              <Button
                size="sm"
                variant="primary"
                onClick={() => onRequest(requestPrompt)}
              >
                Request secure integration
              </Button>
            </>
          ) : (
            <p>
              Contact an OpenNeko administrator to install an integration
              approved for this host.
            </p>
          )}
          <Link href={canAdminister ? "/admin/plugins" : "/integrations"}>
            {canAdminister ? "Review plugins" : "View integrations"}
          </Link>
        </div>
      </div>
    </section>
  );
}

function artifactHref(path: string): string {
  return `/api/work/files/${path.replace(
    /^.*\/(runs|uploads|skills|memory)\//,
    "$1/",
  )}`;
}

function AnswerRunFooter({
  run,
  sources,
  artifacts,
  followups,
  onFollowup,
}: {
  run: RunRecord | null;
  sources: string[];
  artifacts: RunArtifact[];
  followups: string[];
  onFollowup: (prompt: string) => void;
}) {
  const saved =
    typeof run?.analysisMinutesSaved === "number" &&
    run.analysisMinutesSaved > 0
      ? formatSavedShort(run.analysisMinutesSaved)
      : null;
  const answeredIn = workDuration(run);
  const hasEvidence = sources.length > 0 || artifacts.length > 0 || saved || answeredIn;
  if (!hasEvidence && followups.length === 0) return null;
  return (
    <footer className="work-answer-footer">
      {hasEvidence ? (
        <div className="work-answer-evidence">
          {sources.length > 0 ? (
            <div>
              <span>Evidence</span>
              <strong>{sources.join(" · ")}</strong>
            </div>
          ) : null}
          {artifacts.map((artifact) => (
            <a
              key={artifact.path}
              href={artifactHref(artifact.path)}
              title={artifact.path}
              download={artifact.label}
            >
              <span>Artifact</span>
              <strong>{artifact.label}</strong>
            </a>
          ))}
          {answeredIn && run?.finishedAt ? (
            <div>
              <span>Answered in</span>
              <strong>
                {answeredIn}
                <LocalDateTime value={run.finishedAt} className="work-answer-time" />
              </strong>
            </div>
          ) : null}
          {saved ? (
            <div title={run?.analysisMinutesBasis ?? undefined}>
              <span>Analysis avoided</span>
              <strong>{saved}</strong>
            </div>
          ) : null}
        </div>
      ) : null}
      {followups.length > 0 ? (
        <div className="work-answer-followups">
          <span>Continue</span>
          <div>
            {followups.map((prompt) => (
              <Button
                key={prompt}
                size="sm"
                variant="ghost"
                onClick={() => onFollowup(prompt)}
              >
                {prompt}
                <span aria-hidden="true">↗</span>
              </Button>
            ))}
          </div>
        </div>
      ) : null}
    </footer>
  );
}

type ActionRequestSnapshot = {
  status: string;
  result: ApprovalItem["result"];
};

async function fetchActionRequestSnapshot(
  actionRequestId: string,
  actionKind: string,
): Promise<ActionRequestSnapshot | null> {
  const res = await fetch(`/api/action-requests/${actionRequestId}`, {
    cache: "no-store",
  });
  if (!res.ok) return null;
  const body = (await res.json()) as {
    actionRequest?: {
      status?: string;
      rejectionReason?: string | null;
      failureReason?: string | null;
    };
    executions?: Array<{
      status?: string;
      result?: Record<string, unknown> | null;
      externalRef?: string | null;
      commandOrOperation?: string | null;
      error?: string | null;
    }>;
  };
  const status = body.actionRequest?.status ?? "pending_approval";
  const execution = body.executions?.[0];
  let result: ApprovalItem["result"] = null;
  if (status === "executed") {
    result = {
      type: "action_request_result",
      action_request_id: actionRequestId,
      kind: actionKind,
      status: execution?.status === "partially_applied" || execution?.status === "reconcile_required"
        ? execution.status
        : "succeeded",
      outcome: {
        result: execution?.result ?? null,
        externalRef: execution?.externalRef ?? null,
        commandOrOperation: execution?.commandOrOperation ?? null,
      },
    };
  } else if (status === "failed") {
    result = {
      type: "action_request_result",
      action_request_id: actionRequestId,
      kind: actionKind,
      status: "failed",
      error: body.actionRequest?.failureReason ?? execution?.error ?? "Action execution failed.",
    };
  } else if (status === "rejected") {
    result = {
      type: "action_request_result",
      action_request_id: actionRequestId,
      kind: actionKind,
      status: "rejected",
      ...(body.actionRequest?.rejectionReason
        ? { rejection_reason: body.actionRequest.rejectionReason }
        : {}),
    };
  }
  return { status, result };
}

function ActionApprovalRow({
  threadId,
  runId,
  tool,
}: {
  threadId: string;
  runId: string;
  tool: ToolItem;
}) {
  const approval = tool.approval!;
  const [busy, setBusy] = useState(false);
  const [open, setOpen] = useState(false);
  const [rejectMode, setRejectMode] = useState(false);
  const [rejectReason, setRejectReason] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const [localDecision, setLocalDecision] = useState<
    "approve" | "reject" | null
  >(null);
  const [localStatus, setLocalStatus] = useState<string | null>(null);
  const [localResult, setLocalResult] =
    useState<ApprovalItem["result"]>(null);

  useEffect(() => {
    if (approval.result) return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;
    const sync = async () => {
      const snapshot = await fetchActionRequestSnapshot(
        approval.actionRequestId,
        approval.actionKind,
      );
      if (cancelled || !snapshot) return;
      setLocalStatus(snapshot.status);
      setLocalResult(snapshot.result);
      if (snapshot.status === "approved") {
        timer = setTimeout(() => void sync(), 1_000);
      }
    };
    void sync();
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [
    approval.actionKind,
    approval.actionRequestId,
    approval.result,
    localDecision,
  ]);

  const headline =
    approval.intent ??
    approval.summary ??
    `Agent wants to run "${approval.actionKind}".`;
  const effectiveResult = approval.result ?? localResult;
  const pending =
    approval.decision === "pending_approval" &&
    !effectiveResult &&
    !localDecision &&
    (localStatus === null || localStatus === "pending_approval");
  const processing =
    !effectiveResult &&
    (approval.decision === "auto_approved" ||
      localDecision === "approve" ||
      localStatus === "approved");
  const failed = effectiveResult?.status === "failed";
  const partlyApplied = effectiveResult?.status === "partially_applied";
  const needsReconciliation = effectiveResult?.status === "reconcile_required";
  const rejected = effectiveResult?.status === "rejected";
  const succeeded = effectiveResult?.status === "succeeded";
  const visualStatus = failed || partlyApplied || needsReconciliation
    ? "failed"
    : rejected
      ? "rejected"
      : succeeded
        ? "done"
        : processing
          ? "running"
          : "approval";
  const stateLabel = partlyApplied
    ? "Partly applied"
    : needsReconciliation
      ? "Needs checking"
      : failed
        ? "Failed"
        : rejected
          ? "Rejected"
          : succeeded
            ? "Done"
            : processing
              ? "Running"
              : "Approval required";
  const summary = partlyApplied
    ? "Some changes were applied. Review the action details before retrying."
    : needsReconciliation
      ? "The outcome is uncertain. Review the action details before retrying."
      : failed
        ? (effectiveResult?.error ?? "The action failed.")
        : rejected
          ? (effectiveResult?.rejection_reason ?? "The request was rejected.")
          : headline;
  const hasDetail =
    tool.input !== undefined ||
    tool.deltas.length > 0 ||
    tool.end?.result !== undefined ||
    tool.end?.error !== undefined;

  async function decide(decision: "approve" | "reject") {
    if (!runId) {
      setLocalError("Run not ready yet — try again in a moment.");
      return;
    }
    setBusy(true);
    setLocalError(null);
    try {
      const res = await fetch(
        `/api/work/threads/${threadId}/runs/${runId}/approve-action`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            actionRequestId: approval.actionRequestId,
            decision,
            ...(decision === "reject" && rejectReason
              ? { rejectionReason: rejectReason }
              : {}),
          }),
        },
      );
      if (!res.ok) {
        if (res.status === 409) {
          const snapshot = await fetchActionRequestSnapshot(
            approval.actionRequestId,
            approval.actionKind,
          );
          if (snapshot) {
            setLocalStatus(snapshot.status);
            setLocalResult(snapshot.result);
            setLocalDecision(decision);
            return;
          }
        }
        const body = (await res.json().catch(() => ({}))) as { error?: string };
        setLocalError(
          body.error ?? `Request failed (${res.status} ${res.statusText})`,
        );
        return;
      }
      const body = (await res.json().catch(() => ({}))) as {
        status?: string;
        decision?: string;
      };
      setLocalStatus(
        body.status ??
          (decision === "approve" ? "approved" : "rejected"),
      );
      setLocalDecision(decision);
      setRejectMode(false);
    } catch (e) {
      setLocalError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div
      className={`work-tool-row work-action-row work-action-row-${visualStatus}`}
    >
      <div className="work-tool-row-head work-action-row-head">
        <span
          className={`work-tool-row-icon work-tool-row-icon-${visualStatus}`}
          aria-hidden="true"
        >
          {processing ? (
            <Loader2 className="work-status-spin" size={12} />
          ) : failed || partlyApplied || needsReconciliation || rejected ? (
            <X size={12} />
          ) : succeeded ? (
            <Check size={12} />
          ) : (
            <ShieldCheck size={12} />
          )}
        </span>
        <span className="work-action-row-copy">
          <span className="work-tool-row-name">
            {approvalActionLabel(approval.actionKind)}
          </span>
          <span className="work-tool-row-subtitle">{summary}</span>
        </span>
        <span className={`work-action-row-state is-${visualStatus}`}>
          {stateLabel}
        </span>
        {pending && !rejectMode ? (
          <span className="work-action-row-actions">
            <Button
              size="sm"
              variant="primary"
              disabled={busy}
              onClick={() => decide("approve")}
              className="work-action-button is-primary"
            >
              {busy ? "Approving…" : "Approve"}
            </Button>
            <Button
              size="sm"
              disabled={busy}
              onClick={() => setRejectMode(true)}
              className="work-action-button"
            >
              Reject
            </Button>
          </span>
        ) : null}
        {hasDetail ? (
          <Button
            size="sm"
            variant="ghost"
            className="work-action-row-detail-toggle"
            onClick={() => setOpen((value) => !value)}
            aria-expanded={open}
          >
            {open ? "Hide details" : "Details"}
          </Button>
        ) : null}
      </div>

      {pending && rejectMode ? (
        <div className="work-action-row-decision">
          <Input
            type="text"
            placeholder="Reason (optional, shown to the agent)"
            value={rejectReason}
            onChange={(event) => setRejectReason(event.target.value)}
            disabled={busy}
          />
          <div className="work-action-row-actions">
            <Button
              size="sm"
              variant="danger"
              disabled={busy}
              onClick={() => decide("reject")}
              className="work-action-button is-danger"
            >
              {busy ? "Rejecting…" : "Confirm reject"}
            </Button>
            <Button
              size="sm"
              disabled={busy}
              onClick={() => {
                setRejectMode(false);
                setRejectReason("");
              }}
              className="work-action-button"
            >
              Cancel
            </Button>
          </div>
        </div>
      ) : null}

      {localError ? (
        <div className="work-action-row-error">{localError}</div>
      ) : null}

      {open ? <ToolDetail tool={tool} /> : null}
    </div>
  );
}

function approvalActionLabel(kind: string): string {
  const known: Record<string, string> = {
    plugin_install: "Install integration",
    plugin_uninstall: "Remove integration",
    web_fetch: "Fetch from web",
    web_search: "Search the web",
    send_slack_message: "Send Slack message",
  };
  if (known[kind]) return known[kind];
  const words = kind
    .replace(/^mcp[_:.-]*/i, "")
    .split(/[_:.-]+/)
    .filter(Boolean);
  if (words.length === 0) return "Run action";
  return words
    .map((word, index) =>
      index === 0
        ? `${word.charAt(0).toUpperCase()}${word.slice(1).toLowerCase()}`
        : word.toLowerCase(),
    )
    .join(" ");
}

function ToolGroup({
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

function ToolDetail({ tool }: { tool: ToolItem }) {
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

// Back-compat: confirmation cards (workflow/trigger/rule saves) used to be
// emitted as the dashboard `Briefing` root, which renders the 52px display
// greeting — jarring inside the chat timeline. They now emit a `Confirmation`
// component. Run events persisted before that change are frozen as `Briefing`,
// so normalize them here at render time. A real briefing (BriefingCard
// children) is left untouched.
function remapLegacyConfirmations(messages: A2UIMessage[]): A2UIMessage[] {
  return messages.map((message) => {
    if (!("updateComponents" in message)) return message;
    const comps = message.updateComponents.components;
    const isConfirmation =
      comps.some((c) => c.component === "Briefing") &&
      !comps.some((c) => c.component === "BriefingCard");
    if (!isConfirmation) return message;
    return {
      ...message,
      updateComponents: {
        ...message.updateComponents,
        components: comps.map((c) => {
          if (c.component !== "Briefing") return c;
          const { greeting, subtitle, role, isExample, ...rest } = c;
          void role;
          void isExample;
          return {
            ...rest,
            component: "Confirmation",
            label: typeof greeting === "string" ? greeting : "",
            title: typeof subtitle === "string" ? subtitle : "",
          };
        }),
      },
    };
  });
}

function SurfaceBlock({ messages }: { messages: A2UIMessage[] }) {
  const surfaces = useMemo(() => {
    let next = new Map<string, SurfaceState>();
    for (const message of remapLegacyConfirmations(messages)) {
      next = applyMessage(next, message);
    }
    return next;
  }, [messages]);

  const { submitFollowUp } = useWorkShell();
  const nodes: React.ReactNode[] = [];
  for (const [, surface] of surfaces) {
    if (surface.components.size === 0) continue;
    nodes.push(<InteractiveSurface key={surface.surfaceId} surface={surface} submitFollowUp={submitFollowUp} />);
  }

  if (nodes.length === 0) return null;
  return <div className="flex flex-col gap-2.5 mt-1">{nodes}</div>;
}

function InteractiveSurface({
  surface,
  submitFollowUp,
}: {
  surface: SurfaceState;
  submitFollowUp: (prompt: string) => void;
}) {
  const [dataModel, setDataModel] = useState(surface.dataModel);
  useEffect(() => {
    const timer = window.setTimeout(
      () => setDataModel(surface.dataModel),
      0,
    );
    return () => window.clearTimeout(timer);
  }, [surface.dataModel]);

  const liveSurface = { ...surface, dataModel };
  const ctx = {
    surface: liveSurface,
    onDataChange: (path: string, value: unknown) => {
      setDataModel((current) => setDataModelValue(current, path, value));
    },
    onAction: (
      _componentId: string,
      _eventName: string,
      context?: Record<string, unknown>,
    ) => {
      const followUp = buildActionFollowUp(context);
      if (followUp) submitFollowUp(followUp);
    },
  };
  const root = getRootComponent(liveSurface);
  const ids = Array.from(liveSurface.components.keys());
  return (
    <div className="work-surface-frame">
      {root ? renderComponent(root, ctx) : renderChildren(ids, ctx)}
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

function joinMessageWithAttachments(
  text: string,
  files: UploadedWorkFile[],
): string {
  const prefix = text.trim();
  if (files.length === 0) return prefix;
  const lines = files.map((file) => {
    const kb = Math.max(1, Math.round(file.size / 1024));
    return `- ${file.relativePath}  (${file.name}, ${kb} KB)`;
  });
  const header = `I've attached ${files.length === 1 ? "a file" : "files"}:`;
  return prefix ? `${prefix}\n\n${header}\n${lines.join("\n")}` : `${header}\n${lines.join("\n")}`;
}
