"use client";

import { acknowledgeWorkStartup, markWorkStartup } from "@/lib/work-startup-timing";

import "@/a2ui/components";
import {
  ChevronsDownUp,
  ChevronsUpDown,
  History,
  Plus,
  Printer,
} from "lucide-react";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import {
  useParams,
  usePathname,
  useRouter,
  useSearchParams,
} from "next/navigation";
import BriefingCard from "@/components/BriefingCard";
import WorkHistoryDrawer from "@/components/WorkHistoryDrawer";
import BotGreeting from "@/components/chat/BotGreeting";
import { useThreadBot } from "@/components/chat/useThreadBot";
import { useBots } from "@/components/chat/useBots";
import { ThreadJump } from "@/components/chat/ThreadJump";
import { BotAvatar } from "@/components/chat/BotAvatar";
import { botStateForRun, useSetBotMood } from "@/components/chat/bot-mood";
import type { NekoState } from "@neko/avatars";
import { CHAT_PAGE_RUNS, cachedThread, fetchThreadPage, rememberThread } from "@/lib/thread-cache";
import { parseBriefingCardMessage } from "@/lib/briefing-card-context";
import {
  activeWorkMentions,
  appendWorkMentionBlock,
  stripWorkMentionBlock,
} from "@/lib/workflow-mention";
import { SkeletonList } from "@/components/ui/skeleton";
import { printThread } from "@/lib/print-thread";
import { Button } from "@/components/ui/button";
import { LocalDateTime } from "@/components/ui/local-date-time";
import { parseClarificationReply } from "@/a2ui/action";
import { useWorkShell } from "../work-shell-context";
import { joinMessageWithAttachments, uploadFiles } from "./work-attachments";
import { WorkComposer, useWorkMentions } from "./work-composer";
import { EmptyAsk, MessageBubble, PendingMemoryPanel } from "./work-messages";
import { WorkFailureNotice } from "./work-notices";
import { RunTimeline } from "./work-run-timeline";
import {
  isRunInFlight,
  latestInFlightRun,
  prependOlder,
  runWasCancelled,
  withAssistantTimelinePlaceholders,
} from "./work-thread";
import type {
  ActiveRunStream,
  MessageRecord,
  PendingMemory,
  RunRecord,
  StreamResult,
  ThreadBundle,
  UploadedWorkFile,
  WorkEvent,
} from "./work-types";

const REASONING_STORAGE_KEY = "openneko.work.reasoning";

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
  const [loadingThread, setLoadingThread] = useState(false);
  const routeThreadIdRef = useRef(routeThreadId);
  routeThreadIdRef.current = routeThreadId;
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
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const mentions = useWorkMentions(draft, setDraft, textareaRef);
  const { setMention, draftMentions, setDraftMentions } = mentions;
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

  // A bot chat in front of the reader is a read chat: clear its marker in the chat list.
  const { markSeen } = useBots();
  useEffect(() => {
    if (!inBotChat || !activeThreadId || !bundle) return;
    const threadId = activeThreadId;
    const seen = () => {
      if (document.visibilityState === "visible" && document.hasFocus()) markSeen(threadId);
    };
    seen();
    window.addEventListener("focus", seen);
    return () => window.removeEventListener("focus", seen);
  }, [inBotChat, activeThreadId, bundle, markSeen]);

  // A routine can reply in a bot chat while it is open. Check for a new or
  // finished run when the reader returns to the tab and once a minute.
  const latestRunRef = useRef<{ id: string; status: string } | null>(null);
  useEffect(() => {
    const last = bundle?.runs[bundle.runs.length - 1];
    latestRunRef.current = last ? { id: last.id, status: last.status } : null;
  }, [bundle]);
  useEffect(() => {
    if (!inBotChat || !routeThreadId) return;
    const threadId = routeThreadId;
    let busy = false;
    const refresh = async () => {
      if (busy || document.visibilityState !== "visible" || activeRunStreamRef.current) return;
      busy = true;
      try {
        const data = await fetchThreadPage<ThreadBundle>(threadId);
        if (!data || routeThreadIdRef.current !== threadId || activeRunStreamRef.current) return;
        const latest = data.runs[data.runs.length - 1];
        const known = latestRunRef.current;
        if (!latest || (known?.id === latest.id && known.status === latest.status)) return;
        const nextBundle = withAssistantTimelinePlaceholders(data);
        setBundle(nextBundle);
        resumeLatestInFlightRun(threadId, nextBundle);
      } finally {
        busy = false;
      }
    };
    const onVisible = () => void refresh();
    document.addEventListener("visibilitychange", onVisible);
    const timer = window.setInterval(onVisible, 60_000);
    return () => {
      document.removeEventListener("visibilitychange", onVisible);
      window.clearInterval(timer);
    };
    // resumeLatestInFlightRun is a stable closure over component state.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [inBotChat, routeThreadId]);

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

  useEffect(() => {
    if (inBotChat && activeThreadId && bundle && !loadingThread) rememberThread(activeThreadId, bundle);
  }, [inBotChat, activeThreadId, bundle, loadingThread]);

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
  }, [insertComposerRef, setDraftMentions, setMention]);

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
    // A bot chat opened before shows at once and refreshes in the background.
    const cached = inBotChat ? cachedThread<ThreadBundle>(threadId) : null;
    if (cached) {
      setBundle(withAssistantTimelinePlaceholders(cached));
      activeThreadIdRef.current = threadId;
      setActiveThreadId(threadId);
    } else {
      setLoadingThread(true);
    }
    try {
      const data = inBotChat
        ? await fetchThreadPage<ThreadBundle>(threadId)
        : await fetch(`/api/work/threads/${threadId}`).then((res) => (res.ok ? (res.json() as Promise<ThreadBundle>) : null));
      if (routeThreadIdRef.current !== threadId) return;
      if (!data) {
        if (cached) return;
        activeRunStreamRef.current?.close();
        activeRunStreamRef.current = null;
        setSending(false);
        updateActiveRunId(null);
        setBundle(null);
        return;
      }
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


  async function sendMessage() {
    const submittedAt = performance.now();
    const trimmed = draft.trim();
    if (!trimmed && files.length === 0) return;

    setSending(true);
    setStreamError(null);

    let threadId = activeThreadId ?? routeThreadId;
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
    let threadId = activeThreadId ?? routeThreadId;
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

  // The header neko acts out the turn: listening, thinking, searching,
  // working, speaking, then a short happy or error face when it ends.
  const setBotMood = useSetBotMood();
  const running = Boolean(sending || activeRunId);
  const latestRun = bundle?.runs[bundle.runs.length - 1] ?? null;
  const liveState: NekoState | null = running
    ? botStateForRun(bundle?.eventsByRun[activeRunId ?? latestRun?.id ?? ""] ?? [], true, false)
    : null;
  const [afterRun, setAfterRun] = useState<NekoState | null>(null);
  const wasRunningRef = useRef(false);
  useEffect(() => {
    const ended = wasRunningRef.current && !running;
    wasRunningRef.current = running;
    if (!ended) return;
    const face: NekoState = latestRun?.status === "failed" ? "error" : "happy";
    const show = window.setTimeout(() => setAfterRun(face), 0);
    const hide = window.setTimeout(() => setAfterRun(null), face === "error" ? 6000 : 3000);
    return () => {
      window.clearTimeout(show);
      window.clearTimeout(hide);
    };
  }, [running, latestRun?.status]);
  const botMood: NekoState = liveState ?? afterRun ?? (draft.trim() ? "listening" : "idle");
  useEffect(() => {
    if (inBotChat) setBotMood({ threadId: routeThreadId, state: botMood });
  }, [inBotChat, routeThreadId, botMood, setBotMood]);
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
        {loadingThread && inBotChat ? (
          <SkeletonList rows={3} variant="row" label="Opening the chat" />
        ) : loadingThread ? (
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
                  {run?.routineName ? (
                    <p className="chat-routine-label">Routine · {run.routineName}</p>
                  ) : null}
                  <RunTimeline
                    threadId={activeThreadId ?? ""}
                    run={run}
                    events={events}
                    pending={isPending}
                    fallbackContent={message.content}
                    reasoningExpanded={reasoningExpanded}
                    avatar={
                      threadBot ? (
                        <BotAvatar
                          coat={threadBot.coat}
                          pattern={threadBot.pattern}
                          size={26}
                          state={botStateForRun(events, isPending, run?.status === "failed")}
                          animated={isPending}
                          interactive={false}
                          aria-hidden
                        />
                      ) : undefined
                    }
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

        <WorkComposer
          textareaRef={textareaRef}
          draft={draft}
          files={files}
          setFiles={setFiles}
          sending={sending}
          mentions={mentions}
          placeholder={
            sending
              ? `${assistantName} is working…`
              : threadBot
                ? `Ask ${threadBot.name}…`
                : "Ask a question or describe a task…"
          }
          assistantName={assistantName}
          onSend={() => void sendMessage()}
          onStop={() => void cancelRun()}
          onAttachError={setStreamError}
        />
      </div>
    </div>
  );
}
