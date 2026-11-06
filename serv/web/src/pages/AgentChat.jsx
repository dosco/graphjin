import React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import {
  AlertTriangle,
  BellRing,
  Radar,
  BookOpenCheck,
  Bot,
  CheckCircle2,
  CircleHelp,
  KeyRound,
  ListTodo,
  Loader2,
  MessageSquarePlus,
  Pin,
  PanelRightClose,
  Plus,
  Send,
  Settings2,
  Sparkles,
  Square,
  Timer,
  X,
} from "lucide-react";

import { DataErrorState, LoadingState, StatusPill } from "../components/ui";
import {
  agentStatus,
  askAgentStream,
  cancelRun,
  createProfile,
  createThread,
  fetchThread,
  followRun,
  listProfiles,
  listThreads,
  markThreadSeen,
  profileHomeThread,
} from "../services/agent";
import { fetchMissionTask } from "../services/mission";
import { operatorIdentityKey, useOperatorIdentity } from "../services/identity";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Bubble, BubbleContent } from "@/components/ui/bubble";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { CopyButton, MarkdownContent } from "@/components/ui/markdown-content";
import { Marker } from "@/components/ui/marker";
import { Message, MessageContent } from "@/components/ui/message";
import {
  MessageScroller,
  MessageScrollerButton,
  MessageScrollerContent,
  MessageScrollerItem,
  MessageScrollerProvider,
  MessageScrollerViewport,
} from "@/components/ui/message-scroller";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

const initialMessages = [];

const promptStarters = [
  {
    label: "Server status",
    text: "How is the server doing?",
  },
  {
    label: "Security",
    text: "Any security issues?",
  },
  {
    label: "Saved queries",
    text: "What saved queries are there?",
  },
];

const AgentChat = () => {
  const identity = useOperatorIdentity();
  const identityKey = operatorIdentityKey(identity);
  const [searchParams, setSearchParams] = useSearchParams();
  const linkedTaskID = searchParams.get("task_id") || "";
  const threadID = searchParams.get("thread") || "";
  const queryClient = useQueryClient();
  const statusQuery = useQuery({
    queryKey: ["agent-status", identityKey],
    queryFn: agentStatus,
    refetchInterval: 30000,
  });
  const status = statusQuery.data;
  const threadsEnabled = status?.threads_enabled === true;
  const threadsQuery = useQuery({
    queryKey: ["agent-threads", identityKey],
    queryFn: listThreads,
    enabled: threadsEnabled,
  });
  const profilesQuery = useQuery({
    queryKey: ["agent-profiles", identityKey],
    queryFn: listProfiles,
    enabled: threadsEnabled,
  });
  const profiles = React.useMemo(() => profilesQuery.data || [], [profilesQuery.data]);
  const threads = React.useMemo(() => threadsQuery.data || [], [threadsQuery.data]);
  const activeProfile = profileForThread(threads.find((thread) => thread.id === threadID), profiles);
  const [newAgentOpen, setNewAgentOpen] = React.useState(false);
  const [messages, setMessages] = React.useState(initialMessages);
  const [activeRunID, setActiveRunID] = React.useState("");
  const [resuming, setResuming] = React.useState(false);
  const [instruction, setInstruction] = React.useState("");
  const [returnTrace, setReturnTrace] = React.useState(false);
  const [maxSteps, setMaxSteps] = React.useState(8);
  const [advancedOpen, setAdvancedOpen] = React.useState(false);
  const [pendingActions, setPendingActions] = React.useState([]);
  const [inspection, setInspection] = React.useState(null);
  const [pinnedTaskID, setPinnedTaskID] = React.useState(linkedTaskID);
  const statusDefaultsApplied = React.useRef(false);
  const pinnedTaskQuery = useQuery({
    queryKey: ["mission", "task", identityKey, pinnedTaskID],
    queryFn: () => fetchMissionTask(pinnedTaskID),
    enabled: Boolean(pinnedTaskID),
  });

  React.useEffect(() => {
    if (status && !statusDefaultsApplied.current) {
      statusDefaultsApplied.current = true;
      if (status.max_steps) {
        setMaxSteps(status.max_steps);
      }
      setReturnTrace(status.return_trace === true);
    }
  }, [status]);

  React.useEffect(() => {
    setPinnedTaskID(linkedTaskID);
  }, [linkedTaskID]);

  // The open thread is the one being read, so its answers never count as
  // unread; the sidebar refreshes to clear its dot.
  const markSeen = React.useCallback(async (id) => {
    if (!id) {
      return;
    }
    try {
      await markThreadSeen(id);
    } catch {
      // Unread state is advisory; a failed mark only leaves the dot on.
    }
    void queryClient.invalidateQueries({ queryKey: ["agent-threads"] });
  }, [queryClient]);

  // A thread in the URL is loaded from the store; a run still active on it
  // (a reload mid-answer, or a run started elsewhere) is followed to its end.
  const skipNextThreadLoad = React.useRef(false);
  React.useEffect(() => {
    if (!threadsEnabled || !threadID) {
      return undefined;
    }
    if (skipNextThreadLoad.current) {
      skipNextThreadLoad.current = false;
      return undefined;
    }
    let cancelled = false;
    async function load() {
      const thread = await fetchThread(threadID);
      if (cancelled || !thread) {
        return;
      }
      setMessages(messagesFromThread(thread));
      if (thread.unread) {
        void markSeen(threadID);
      }
      if (thread.active_run_id) {
        setActiveRunID(thread.active_run_id);
        setResuming(true);
        setPendingActions([]);
        try {
          await followRun(thread.active_run_id, {
            onAction: (action) => !cancelled && setPendingActions((current) => [...current, action]),
          });
        } catch {
          // A lost run ends without a result; the reload below shows why.
        }
        if (cancelled) {
          return;
        }
        setResuming(false);
        setActiveRunID("");
        setPendingActions([]);
        const finished = await fetchThread(threadID);
        if (!cancelled && finished) {
          setMessages(messagesFromThread(finished));
        }
        await markSeen(threadID);
      }
    }
    void load().catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [threadID, threadsEnabled, queryClient, markSeen]);

  const mutation = useMutation({
    mutationFn: (vars) =>
      askAgentStream(vars, {
        onAction: (action) => setPendingActions((current) => [...current, action]),
        onRun: (run) => setActiveRunID(run?.run_id || ""),
      }),
  });

  const ready = status?.ready === true;
  const pending = mutation.isPending || resuming;
  const inputDisabled = !ready || pending;
  const canSubmit = ready && !pending && instruction.trim().length > 0;

  function selectThread(nextID) {
    const next = new URLSearchParams(searchParams);
    if (nextID) {
      next.set("thread", nextID);
    } else {
      next.delete("thread");
    }
    setSearchParams(next, { replace: false });
    if (!nextID) {
      setMessages(initialMessages);
    }
  }

  // Opening an agent opens its home thread, created on first use.
  async function selectProfile(profile) {
    try {
      const thread = await profileHomeThread(profile.id);
      if (thread?.id) {
        selectThread(thread.id);
        void queryClient.invalidateQueries({ queryKey: ["agent-threads"] });
      }
    } catch {
      // The thread list stays as it was; the agent can be opened again.
    }
  }

  async function addAgent(input) {
    const profile = await createProfile(input);
    await queryClient.invalidateQueries({ queryKey: ["agent-profiles"] });
    setNewAgentOpen(false);
    if (profile?.id) {
      await selectProfile(profile);
    }
  }

  async function stopRun() {
    if (!activeRunID) {
      return;
    }
    try {
      await cancelRun(activeRunID);
    } catch {
      // The run may have just finished; its result arrives on the stream.
    }
  }

  async function submitInstruction() {
    const text = instruction.trim();
    if (!text || !ready || mutation.isPending) {
      return;
    }

    setInstruction("");
    // Prior turns become the request history so follow-ups and clarification
    // answers keep their conversation context (the agent still re-discovers
    // its own evidence every run). A thread carries its own stored history.
    const history = threadsEnabled ? [] : historyFromMessages(messages);
    let currentThreadID = threadID;
    if (threadsEnabled && !currentThreadID) {
      try {
        const thread = await createThread();
        currentThreadID = thread?.id || "";
        if (currentThreadID) {
          // The new thread's messages are already on screen.
          skipNextThreadLoad.current = true;
          selectThread(currentThreadID);
        }
      } catch {
        currentThreadID = "";
      }
    }
    const userMessage = {
      id: messageID(),
      role: "user",
      content: text,
    };
    setMessages((current) => [...current, userMessage]);
    setPendingActions([]);

    try {
      const response = await mutation.mutateAsync({
        instruction: text,
        max_steps: Number(maxSteps) || undefined,
        return_trace: returnTrace,
        history: history.length ? history : undefined,
        task_id: pinnedTaskID || undefined,
        thread_id: currentThreadID || undefined,
      });
      setMessages((current) => [
        ...current,
        {
          id: messageID(),
          role: "assistant",
          content: displayAnswer(response),
          response,
          status: response.status || "answered",
        },
      ]);
    } catch (error) {
      setMessages((current) => [
        ...current,
        {
          id: messageID(),
          role: "assistant",
          status: "error",
          content: error.message || "The GraphJin agent request failed.",
          response: {
            status: "error",
            errors: error.errors?.length ? error.errors : [{ message: error.message }],
          },
        },
      ]);
    } finally {
      setPendingActions([]);
      setActiveRunID("");
      if (threadsEnabled) {
        // The answer arrived on screen, so the thread is already read.
        await markSeen(currentThreadID);
      }
    }
  }

  function handleSubmit(event) {
    event.preventDefault();
    void submitInstruction();
  }

  function handleInstructionKeyDown(event) {
    if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
      event.preventDefault();
      void submitInstruction();
    }
  }

  function pinTask(taskID) {
    if (!taskID) {
      return;
    }
    setPinnedTaskID(taskID);
    const next = new URLSearchParams(searchParams);
    next.set("task_id", taskID);
    setSearchParams(next, { replace: true });
  }

  function clearPinnedTask() {
    setPinnedTaskID("");
    const next = new URLSearchParams(searchParams);
    next.delete("task_id");
    setSearchParams(next, { replace: true });
  }

  const providerLabel = [status?.provider, status?.model].filter(Boolean).join(" / ") || "provider pending";
  const desktop = useDesktopLayout();

  const chatSurface = (
    <div className="grid h-full min-h-0 min-w-0 grid-cols-[minmax(0,1fr)] grid-rows-[minmax(0,1fr)_auto]">
      <MessageScrollerProvider>
        <MessageScroller className="min-h-0">
          <MessageScrollerViewport>
            <MessageScrollerContent className="mx-auto w-full max-w-3xl px-4 py-8 sm:px-6">
              {messages.length === 0 ? (
                <MessageScrollerItem scrollAnchor>
                  <EmptyAgentState ready={ready} onSelectPrompt={setInstruction} />
                </MessageScrollerItem>
              ) : (
                messages.map((message, index) => (
                  <MessageScrollerItem key={message.id} scrollAnchor={index === messages.length - 1 && !pending}>
                    <ChatMessage message={message} onPinTask={pinTask} onInspect={setInspection} />
                  </MessageScrollerItem>
                ))
              )}
              {pending && (
                <MessageScrollerItem scrollAnchor>
                  <ThinkingMessage actions={pendingActions} />
                </MessageScrollerItem>
              )}
            </MessageScrollerContent>
          </MessageScrollerViewport>
          <MessageScrollerButton />
        </MessageScroller>
      </MessageScrollerProvider>

      <form className="min-w-0 px-3 pb-4 pt-2 sm:px-6 sm:pb-6" onSubmit={handleSubmit}>
        <div className="mx-auto grid w-full min-w-0 max-w-3xl gap-2.5">
          {!ready && <AgentDisabledAlert status={status} />}
          {pinnedTaskID && (
            <PinnedTask taskID={pinnedTaskID} task={pinnedTaskQuery.data} isLoading={pinnedTaskQuery.isLoading} onClear={clearPinnedTask} />
          )}
          <div className="min-w-0 overflow-hidden rounded-lg bg-card/85 shadow-[0_0_0_1px_var(--border),0_24px_60px_-24px_var(--glow)] backdrop-blur-xl transition-shadow focus-within:shadow-[0_0_0_1px_var(--glow-edge),0_0_32px_-6px_var(--glow)]">
            <Textarea
              value={instruction}
              onChange={(event) => setInstruction(event.target.value)}
              onKeyDown={handleInstructionKeyDown}
              placeholder={ready ? "Ask a question" : "The agent is off"}
              disabled={inputDisabled}
              rows={2}
              className="min-h-20 resize-none border-0 bg-transparent px-4 py-3 shadow-none focus-visible:bg-transparent focus-visible:ring-0"
            />
            <div className="flex flex-wrap items-center justify-between gap-3 px-2.5 pb-2.5">
              <div className="flex min-w-0 items-center gap-1">
                <Button type="button" variant="ghost" size="sm" onClick={() => setAdvancedOpen((open) => !open)} disabled={inputDisabled}>
                  <Settings2 aria-hidden="true" />Run settings
                </Button>
                {returnTrace && <Badge variant="muted">Trace</Badge>}
              </div>
              {pending && activeRunID ? (
                <Button type="button" size="icon" variant="outline" onClick={() => void stopRun()} aria-label="Stop the run">
                  <Square aria-hidden="true" />
                </Button>
              ) : (
                <Button type="submit" size="icon" disabled={!canSubmit} aria-label="Send message">
                  <Send aria-hidden="true" />
                </Button>
              )}
            </div>
          </div>
          {advancedOpen && (
            <div className="flex flex-wrap items-end justify-between gap-3 rounded-md bg-accent px-3 py-2.5">
              <label className="flex items-center gap-2 text-sm"><span className="font-medium">Steps</span><Input type="number" min="1" max="24" value={maxSteps} onChange={(event) => setMaxSteps(event.target.value)} className="h-8 w-20 bg-background" disabled={inputDisabled} /></label>
              <label className="flex h-8 items-center gap-2 text-sm font-medium"><Checkbox checked={returnTrace} onCheckedChange={(checked) => setReturnTrace(checked === true)} disabled={inputDisabled} />Return trace</label>
            </div>
          )}
        </div>
      </form>
    </div>
  );

  return (
    <div className="h-[calc(100dvh-var(--chrome-top)-var(--chrome-bottom))] min-h-[38rem] overflow-hidden">
      {statusQuery.isLoading ? (
        <div className="p-6"><LoadingState /></div>
      ) : statusQuery.error ? (
        <div className="p-6"><DataErrorState error={statusQuery.error} permissionMessage="Sign in as an operator or admin to use the agent." unavailableMessage="The agent status endpoint could not be reached from /api/v1/agent/status." /></div>
      ) : (
        <div className="grid h-full min-h-0 min-w-0 grid-cols-[minmax(0,1fr)] grid-rows-[auto_minmax(0,1fr)]">
          <div className="flex min-h-16 items-center justify-between gap-4 px-4 py-2 sm:px-6">
            <div className="flex min-w-0 items-center gap-3">
              <div className="min-w-0 overflow-hidden"><div className="flex items-center gap-2"><h1 className="truncate text-[0.9375rem] font-extrabold tracking-tight">{activeProfile?.name || "Agent"}</h1><StatusPill status={ready ? "ready" : status?.status || "pending"} /></div></div>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              {threadsEnabled && (
                <Button type="button" variant="ghost" size="sm" onClick={() => selectThread("")} disabled={pending}>
                  <MessageSquarePlus aria-hidden="true" /><span className="sr-only sm:not-sr-only">New chat</span>
                </Button>
              )}
              <Badge variant="muted" className="hidden max-w-56 truncate md:inline-flex" title={providerLabel}><Sparkles aria-hidden="true" />{providerLabel}</Badge>
              {inspection && <Button type="button" variant="ghost" size="sm" className="hidden lg:inline-flex" onClick={() => setInspection(null)}><PanelRightClose aria-hidden="true" />Close inspector</Button>}
              <AgentStatusAction status={status} />
            </div>
          </div>
          <div className={cn("min-h-0 min-w-0 overflow-hidden", threadsEnabled && desktop && "grid grid-cols-[15rem_minmax(0,1fr)]")}>
            {threadsEnabled && desktop && (
              <ThreadList
                threads={threads}
                profiles={profiles}
                activeID={threadID}
                onSelect={selectThread}
                onSelectProfile={(profile) => void selectProfile(profile)}
                onNewAgent={() => setNewAgentOpen(true)}
                disabled={pending}
              />
            )}
            {inspection && desktop ? (
              <ResizablePanelGroup orientation="horizontal" className="h-full">
                <ResizablePanel defaultSize="65%" minSize="42%">{chatSurface}</ResizablePanel>
                <ResizableHandle withHandle />
                <ResizablePanel defaultSize="35%" minSize="24%" maxSize="52%"><AgentInspector inspection={inspection} onClose={() => setInspection(null)} /></ResizablePanel>
              </ResizablePanelGroup>
            ) : chatSurface}
            {threadsEnabled && <NewAgentDialog open={newAgentOpen} onOpenChange={setNewAgentOpen} onCreate={addAgent} />}
            {!desktop && <Sheet open={Boolean(inspection)} onOpenChange={(open) => !open && setInspection(null)}><SheetContent side="right" className="w-[min(96vw,42rem)] p-0 sm:w-[42rem]">{inspection && <AgentInspector inspection={inspection} onClose={() => setInspection(null)} compact />}</SheetContent></Sheet>}
          </div>
        </div>
      )}
    </div>
  );
};

// groupThreadsByAgent files threads under the agent profile they belong to.
// Plain chats (and threads whose profile the caller can no longer see) come
// first; every agent is listed even before its home thread exists.
export function groupThreadsByAgent(threads = [], profiles = []) {
  const known = new Set(profiles.map((profile) => profile.id));
  const groups = [{ profile: null, threads: [] }, ...profiles.map((profile) => ({ profile, threads: [] }))];
  const byProfile = new Map(groups.slice(1).map((group) => [group.profile.id, group]));
  for (const thread of threads) {
    const group = known.has(thread.profile) ? byProfile.get(thread.profile) : groups[0];
    group.threads.push(thread);
  }
  return groups.map((group) => ({ ...group, unread: group.threads.some((thread) => thread.unread) }));
}

function profileForThread(thread, profiles) {
  if (!thread?.profile) {
    return null;
  }
  return profiles.find((profile) => profile.id === thread.profile) || null;
}

export function ThreadList({ threads, profiles = [], activeID, onSelect, onSelectProfile, onNewAgent, disabled }) {
  const groups = groupThreadsByAgent(threads, profiles);
  const [chats, ...agents] = groups;
  return (
    <nav aria-label="Conversations" className="grid min-h-0 grid-cols-[minmax(0,1fr)] content-start gap-5 overflow-y-auto overflow-x-hidden p-3">
      <section aria-label="Agents" className="grid grid-cols-[minmax(0,1fr)] gap-0.5">
        <div className="flex items-center justify-between gap-2 px-2 pt-1">
          <h2 className="hud-label">Agents</h2>
          {onNewAgent && (
            <Button type="button" variant="ghost" size="icon-sm" onClick={onNewAgent} disabled={disabled} aria-label="New agent">
              <Plus aria-hidden="true" />
            </Button>
          )}
        </div>
        {agents.length === 0 && <p className="px-2 py-1 text-xs text-muted-foreground">None yet</p>}
        {agents.map((group) => (
          <div key={group.profile.id} className="grid gap-0.5">
            <button
              type="button"
              disabled={disabled}
              onClick={() => onSelectProfile?.(group.profile)}
              aria-current={group.threads.some((thread) => thread.id === activeID) ? "true" : undefined}
              className={cn(
                "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm font-medium transition-colors hover:bg-accent disabled:opacity-60",
                group.threads.length === 1 && group.threads[0].id === activeID && "font-bold",
              )}
            >
                            <span className="min-w-0 flex-1 truncate">{group.profile.name}</span>
              {!group.profile.mine && <span className="shrink-0 text-xs font-normal text-muted-foreground">team</span>}
              {group.unread && <UnreadDot />}
            </button>
            {group.threads.length > 1 && (
              <ThreadItems threads={group.threads} activeID={activeID} onSelect={onSelect} disabled={disabled} nested />
            )}
          </div>
        ))}
      </section>
      <section aria-label="Chats" className="grid grid-cols-[minmax(0,1fr)] gap-0.5">
        <h2 className="hud-label px-2 pt-1">Chats</h2>
        {chats.threads.length === 0 ? (
          <p className="px-2 py-1 text-xs text-muted-foreground">None yet</p>
        ) : (
          <ThreadItems threads={chats.threads} activeID={activeID} onSelect={onSelect} disabled={disabled} />
        )}
      </section>
    </nav>
  );
}

function ThreadItems({ threads, activeID, onSelect, disabled, nested = false }) {
  return (
    <ul className={cn("grid grid-cols-[minmax(0,1fr)] gap-0.5", nested && "ml-4 border-l pl-1")}>
      {threads.map((thread) => (
        <li key={thread.id}>
          <button
            type="button"
            disabled={disabled}
            onClick={() => onSelect(thread.id)}
            aria-current={thread.id === activeID ? "page" : undefined}
            className={cn(
              "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground disabled:opacity-60",
              thread.id === activeID && "font-bold text-foreground",
            )}
          >
            <span className="min-w-0 flex-1 truncate">{thread.title || "New conversation"}</span>
            {thread.active_run_id && <Timer className="size-3.5 shrink-0 text-muted-foreground" aria-label="Running" />}
            {thread.unread && <UnreadDot />}
          </button>
        </li>
      ))}
    </ul>
  );
}

function UnreadDot() {
  return <span role="status" aria-label="Unread" className="size-1.5 shrink-0 bg-brand" />;
}

function NewAgentDialog({ open, onOpenChange, onCreate }) {
  const [name, setName] = React.useState("");
  const [instructions, setInstructions] = React.useState("");
  const [error, setError] = React.useState("");
  const [saving, setSaving] = React.useState(false);

  React.useEffect(() => {
    if (open) {
      setName("");
      setInstructions("");
      setError("");
    }
  }, [open]);

  async function handleSubmit(event) {
    event.preventDefault();
    if (!name.trim() || saving) {
      return;
    }
    setSaving(true);
    setError("");
    try {
      await onCreate({ name: name.trim(), instructions: instructions.trim() });
    } catch (err) {
      setError(err?.message || "The agent could not be created.");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="grid w-[min(92vw,28rem)] content-start gap-5 sm:w-[28rem]">
        <SheetHeader className="pr-8">
          <SheetTitle className="flex items-center gap-2"><Bot className="size-4" aria-hidden="true" />New agent</SheetTitle>
          <SheetDescription>An agent keeps its own conversation and follows your instructions on every answer. It only sees what you can see.</SheetDescription>
        </SheetHeader>
        <form className="grid gap-4" onSubmit={handleSubmit}>
          <label className="grid gap-1.5 text-sm">
            <span className="font-medium">Name</span>
            <Input value={name} onChange={(event) => setName(event.target.value)} maxLength={80} placeholder="Finance" required />
          </label>
          <label className="grid gap-1.5 text-sm">
            <span className="font-medium">Instructions</span>
            <Textarea
              value={instructions}
              onChange={(event) => setInstructions(event.target.value)}
              maxLength={2048}
              rows={6}
              placeholder="Focus on revenue and margin. Report amounts in EUR and compare against the prior month."
            />
            <span className="text-xs text-muted-foreground">{instructions.length} / 2048</span>
          </label>
          {error && <p className="text-sm text-destructive">{error}</p>}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>Cancel</Button>
            <Button type="submit" disabled={!name.trim() || saving}>Create agent</Button>
          </div>
        </form>
      </SheetContent>
    </Sheet>
  );
}

// messagesFromThread maps stored thread messages onto chat messages.
function messagesFromThread(thread) {
  return (thread?.messages || []).map((message) => ({
    id: message.id,
    role: message.role,
    content: message.role === "assistant" && message.response ? displayAnswer(message.response) || message.content : message.content,
    status: message.status || undefined,
    response: message.response,
  }));
}

export function AgentInspector({ inspection, onClose, compact = false }) {
  const response = inspection?.response || {};
  const delegations = responseDelegations(response);
  const payloads = React.useMemo(() => [
    { id: "data", label: "Data", value: response.data },
    { id: "questions", label: "Questions", value: delegations },
    // The caller's saved skills and memories that shaped this answer.
    { id: "skills", label: "Skills", value: response.skills },
    { id: "memories", label: "Memories", value: response.memories },
    { id: "evidence", label: "Evidence", value: response.evidence },
    { id: "actions", label: "Actions", value: response.actions },
    { id: "next", label: "Next", value: response.next },
    { id: "usage", label: "Usage", value: response.usage },
    { id: "trace", label: "Trace", value: response.trace },
    { id: "errors", label: "Errors", value: response.errors },
  ].filter((item) => hasValue(item.value)), [response, delegations]);
  const preferred = payloads.some((item) => item.id === inspection?.tab) ? inspection.tab : payloads[0]?.id;
  const [tab, setTab] = React.useState(preferred || "data");

  React.useEffect(() => {
    setTab(preferred || "data");
  }, [inspection, preferred]);

  const selected = payloads.find((item) => item.id === tab) || payloads[0];
  return (
    <aside className="grid h-full min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] bg-background" aria-label="Agent result inspector">
      <div className={cn("flex items-start justify-between gap-4 border-b px-4 py-4", compact && "pr-12")}>
        <div className="min-w-0">
          <p className="hud-label">Inspector</p>
          <h2 className="mt-1 truncate text-base font-semibold">{inspection?.title || "Run details"}</h2>
          {response.trace_id && <p className="mt-1 truncate font-mono text-xs text-muted-foreground">{response.trace_id}</p>}
        </div>
        {!compact && <Button type="button" variant="ghost" size="icon-sm" onClick={onClose} aria-label="Close inspector"><X aria-hidden="true" /></Button>}
      </div>
      {payloads.length > 0 && (
        <Tabs value={tab} onValueChange={setTab} className="border-b px-3 py-2">
          <TabsList className="scrollbar-none h-auto max-w-full justify-start overflow-x-auto border-0 bg-transparent p-0 shadow-none">
            {payloads.map((item) => <TabsTrigger key={item.id} value={item.id} className="shrink-0">{item.label}<Badge variant="muted">{payloadSummary(item.value)}</Badge></TabsTrigger>)}
          </TabsList>
        </Tabs>
      )}
      <div className="min-h-0 overflow-auto p-4">
        {selected ? (
          selected.id === "actions" && Array.isArray(selected.value) ? (
            <ol className="divide-y">
              {groupActionsByDelegate(selected.value, delegations).map((entry, index) => entry.kind === "question" ? (
                <li key={`question-${entry.delegate}`} className="grid gap-2 py-3 first:pt-0">
                  <div className="flex items-start justify-between gap-3"><span className="min-w-0 break-words text-sm font-medium">{entry.question || `Question ${entry.delegate}`}</span><StatusPill status={entry.status} /></div>
                  {entry.actions.length > 0 && (
                    <ol className="grid gap-3 border-l pl-3">
                      {entry.actions.map((action, actionIndex) => <InspectorAction key={`${entry.delegate}-${action?.index ?? actionIndex}-${action?.tool || "action"}`} action={action} index={actionIndex} />)}
                    </ol>
                  )}
                </li>
              ) : (
                <InspectorAction key={`${entry.action?.index ?? index}-${entry.action?.tool || "action"}`} action={entry.action} index={index} className="py-3 first:pt-0" />
              ))}
            </ol>
          ) : selected.id === "questions" ? (
            <ol className="divide-y">
              {selected.value.map((delegation, index) => (
                <li key={`delegation-${index}`} className="grid gap-2 py-3 first:pt-0">
                  <div className="flex items-start justify-between gap-3"><span className="min-w-0 break-words text-sm font-medium">{delegation?.question || `Question ${index + 1}`}</span><StatusPill status={delegation?.status || "unknown"} /></div>
                  {hasValue(delegation?.answer) && <MarkdownContent value={String(delegation.answer)} className="text-sm" />}
                  {hasValue(delegation?.data) && <pre className="overflow-auto whitespace-pre-wrap break-words rounded-lg bg-muted/55 p-3 text-xs leading-5"><code>{JSON.stringify(delegation.data, null, 2)}</code></pre>}
                </li>
              ))}
            </ol>
          ) : (
            <pre className="min-h-full overflow-auto whitespace-pre-wrap break-words rounded-lg bg-muted/55 p-4 text-xs leading-5 text-foreground"><code>{JSON.stringify(selected.value, null, 2)}</code></pre>
          )
        ) : <p className="text-sm text-muted-foreground">No structured payload is available for this response.</p>}
      </div>
    </aside>
  );
}

function InspectorAction({ action, index, className }) {
  return (
    <li className={cn("grid gap-1", className)}>
      <div className="flex items-center justify-between gap-3"><span className="font-mono text-sm font-medium">{action?.tool || `Action ${index + 1}`}</span><StatusPill status={action?.status || "completed"} /></div>
      {action?.args && <pre className="overflow-auto whitespace-pre-wrap break-words rounded-lg bg-muted/55 p-3 text-xs leading-5"><code>{JSON.stringify(action.args, null, 2)}</code></pre>}
    </li>
  );
}

function useDesktopLayout() {
  const [desktop, setDesktop] = React.useState(() => window.matchMedia("(min-width: 1024px)").matches);
  React.useEffect(() => {
    const media = window.matchMedia("(min-width: 1024px)");
    const onChange = (event) => setDesktop(event.matches);
    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, []);
  return desktop;
}

function EmptyAgentState({ ready, onSelectPrompt }) {
  return (
    <div className="flex min-h-[18rem] items-center justify-center p-4 md:min-h-[26rem] md:p-6">
      <div className="grid max-w-2xl justify-items-center gap-4 text-center md:gap-5">
        <p className="text-[2rem] font-extrabold leading-tight tracking-[-0.03em] text-foreground">{ready ? "Ask about your data" : "The agent is off"}</p>
        <div className="grid w-full gap-1 text-left">
          {promptStarters.map((prompt) => (
            <button
              type="button"
              key={prompt.label}
              disabled={!ready}
              onClick={() => onSelectPrompt(prompt.text)}
              className="w-full rounded-md px-3 py-2.5 text-left text-[0.9375rem] text-muted-foreground transition hover:bg-accent hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
            >
              {prompt.text}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

export function ThinkingMessage({ actions = [] }) {
  const entries = groupActionsByDelegate(actions);
  return (
    <Message role="assistant">
      <MessageContent>
        <div className="flex items-center gap-2">
          <Marker status="loading" label="working" />
          <span className="text-xs text-muted-foreground">catalog-first loop active</span>
        </div>
        <Bubble variant="muted" className="w-fit max-w-full">
          <BubbleContent>
          <span>GraphJin is working</span>
          {entries.length > 0 && (
            <ol className="mt-2 grid gap-1 border-t pt-2 font-mono text-xs text-muted-foreground">
              {entries.map((entry) => entry.kind === "question" ? (
                <li key={`question-${entry.delegate}`} className="grid gap-1">
                  <div className="flex items-start gap-2 font-sans text-foreground">
                    <QuestionStatusIcon status={entry.status} />
                    <span className="min-w-0 break-words">{entry.question || `Question ${entry.delegate}`}</span>
                  </div>
                  {entry.status === "error" && entry.error && <p className="ml-5 font-sans text-destructive">{entry.error}</p>}
                  {entry.actions.length > 0 && (
                    <ol className="ml-[0.4rem] grid gap-1 border-l pl-3">
                      {entry.actions.map((action) => <TrailAction key={`${entry.delegate}-${action.index}-${action.tool}`} action={action} />)}
                    </ol>
                  )}
                </li>
              ) : (
                <TrailAction key={`${entry.action.index}-${entry.action.tool}`} action={entry.action} />
              ))}
            </ol>
          )}
          </BubbleContent>
        </Bubble>
      </MessageContent>
    </Message>
  );
}

function TrailAction({ action }) {
  return (
    <li className="flex items-center gap-2">
      {action.status === "error" ? (
        <AlertTriangle className="size-3.5 shrink-0 text-destructive" aria-hidden="true" />
      ) : (
        <CheckCircle2 className="size-3.5 shrink-0 text-success" aria-hidden="true" />
      )}
      <span className="truncate">{actionLabel(action)}</span>
    </li>
  );
}

function QuestionStatusIcon({ status }) {
  if (questionRunning(status)) {
    return <Loader2 className="mt-0.5 size-3.5 shrink-0 animate-spin text-muted-foreground" aria-label="Running" role="img" />;
  }
  if (questionFailed(status)) {
    return <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-destructive" aria-label="Failed" role="img" />;
  }
  return <CheckCircle2 className="mt-0.5 size-3.5 shrink-0 text-success" aria-label="Done" role="img" />;
}

function questionRunning(status) {
  return !status || status === "running" || status === "pending";
}

function questionFailed(status) {
  return status === "error" || status === "failed" || status === "blocked";
}

// isDelegationAction reports the lead's own record of a delegated question: one
// ask_graphjin action per question (running, then done or error). The worker's
// actions for that question carry the same delegate index.
function isDelegationAction(action) {
  return action?.delegate != null && action.source === "lead" && action.tool === "ask_graphjin";
}

// groupActionsByDelegate folds actions carrying a delegate index under the
// question they answer. Questions run in parallel, so their events interleave;
// the questions sit together where the first delegated action arrived, ordered
// by delegate index. Actions without a delegate pass through unchanged, so a
// plain run yields one entry per action. A response's data.delegations fill in
// the question and final status.
export function groupActionsByDelegate(actions = [], delegations = []) {
  const entries = [];
  const groups = new Map();
  let slot = -1;
  for (const action of actions || []) {
    const delegate = action?.delegate;
    if (delegate == null) {
      entries.push({ kind: "action", action });
      continue;
    }
    if (slot < 0) {
      slot = entries.length;
      entries.push(null);
    }
    let group = groups.get(delegate);
    if (!group) {
      group = { kind: "question", delegate, question: "", status: "", error: "", actions: [] };
      groups.set(delegate, group);
    }
    if (action.question) {
      group.question = action.question;
    }
    if (isDelegationAction(action)) {
      group.status = action.status || group.status;
      group.error = action.error || "";
    } else {
      group.actions.push(action);
    }
  }
  if (slot < 0) {
    return entries;
  }
  const questions = [...groups.values()]
    .sort((a, b) => a.delegate - b.delegate)
    .map((group) => {
      const delegation = Array.isArray(delegations) ? delegations[group.delegate - 1] : null;
      return {
        ...group,
        question: group.question || delegation?.question || "",
        status: delegation?.status || group.status || "running",
      };
    });
  entries.splice(slot, 1, ...questions);
  return entries;
}

function responseDelegations(response) {
  const delegations = response?.data?.delegations;
  return Array.isArray(delegations) ? delegations : null;
}

// actionLabel renders one streamed ActionEvent as a compact progress line,
// e.g. `query_catalog(id=table:db.products)`.
function actionLabel(action) {
  let arg = "";
  for (const key of ["id", "ids", "search", "name", "table", "kind", "for"]) {
    const value = action?.args?.[key];
    if (value != null) {
      arg = `${key}=${typeof value === "string" ? value : JSON.stringify(value)}`;
      break;
    }
  }
  const label = `${action.tool}${arg ? `(${arg})` : ""}`;
  return label.length > 96 ? `${label.slice(0, 96)}…` : label;
}

function AgentStatusAction({ status }) {
  return (
    <>
      <Sheet>
        <SheetTrigger asChild>
          <Button type="button" variant="ghost" size="sm">
            Details
          </Button>
        </SheetTrigger>
        <SheetContent side="right" className="grid w-[min(92vw,28rem)] grid-rows-[auto_1fr] sm:w-[28rem]">
          <SheetHeader className="pr-8">
            <SheetTitle className="flex items-center gap-2">
              <Bot className="size-4" aria-hidden="true" />
              Agent Readiness
            </SheetTitle>
            <SheetDescription>{status.message}</SheetDescription>
          </SheetHeader>
          <AgentStatusDetails status={status} />
        </SheetContent>
      </Sheet>
    </>
  );
}

function AgentStatusDetails({ status }) {
  return (
    <div className="grid content-start gap-5 overflow-y-auto py-5">
      <div className="flex flex-wrap gap-2">
        <StatusPill status={status.status} />
        <Badge variant={status.enabled ? "success" : "warning"}>{status.enabled ? "enabled" : "disabled"}</Badge>
        <Badge variant={status.api_key_configured ? "success" : "warning"}>{status.api_key_configured ? "key set" : "key missing"}</Badge>
      </div>
      <dl className="grid gap-2 text-sm">
        <StatusRow icon={Settings2} label="Provider" value={[status.provider, status.model].filter(Boolean).join(" / ") || "openai"} />
        <StatusRow icon={KeyRound} label="API key env" value={status.api_key_env || "OPENAI_API_KEY"} />
        <StatusRow icon={CircleHelp} label="REST endpoint" value={status.endpoint || "/api/v1/agent"} />
        <StatusRow icon={Bot} label="MCP tool" value={status.mcp_tool || "ask_graphjin_agent"} />
        <StatusRow icon={Timer} label="Timeout" value={`${status.timeout_seconds || 50}s`} />
      </dl>
      <div className="grid gap-2 rounded-lg bg-card p-3 text-sm text-muted-foreground">
        <div className="flex items-center justify-between gap-3">
          <span>Read-only override</span>
          <Badge variant={status.read_only ? "warning" : "muted"}>{status.read_only ? "on" : "off"}</Badge>
        </div>
        <div className="flex items-center justify-between gap-3">
          <span>Default trace</span>
          <Badge variant={status.return_trace ? "success" : "muted"}>{status.return_trace ? "on" : "off"}</Badge>
        </div>
        <div className="flex items-center justify-between gap-3">
          <span>Default max steps</span>
          <Badge variant="outline">{status.max_steps}</Badge>
        </div>
      </div>
    </div>
  );
}

function StatusRow({ icon: Icon, label, value }) {
  return (
    <div className="grid grid-cols-[2rem_7rem_minmax(0,1fr)] items-center gap-2 rounded-lg bg-card p-2">
      <span className="flex size-8 items-center justify-center text-foreground">
        <Icon className="size-4" aria-hidden="true" />
      </span>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 truncate font-medium">{value}</dd>
    </div>
  );
}

function AgentDisabledAlert({ status }) {
  const missingKey = status?.enabled && !status?.api_key_configured;
  return (
    <Alert variant="warning">
      <AlertTriangle className="size-4" aria-hidden="true" />
      <AlertTitle>{missingKey ? "Agent key is missing" : "Agent is not enabled"}</AlertTitle>
      <AlertDescription>
        {missingKey ? (
          <>Set <code>{status?.api_key_env || "OPENAI_API_KEY"}</code> in the GraphJin service environment.</>
        ) : (
          <>Enable <code>agent.enabled: true</code> and configure <code>agent.api_key_env</code> for GraphJin.</>
        )}
      </AlertDescription>
    </Alert>
  );
}

function ChatMessage({ message, onPinTask, onInspect }) {
  const isUser = message.role === "user";
  const response = message.response;
  return (
    <Message role={message.role}>
      <MessageContent className={cn(isUser && "items-end")}>
        {response && (
          <div className={cn("flex flex-wrap items-center justify-between gap-2", isUser && "justify-end")}>
            <div className="flex flex-wrap items-center gap-2">
              <Marker status={response.status || message.status || "answered"} />
              <ResponseSummaryBadges response={response} onInspect={onInspect} />
            </div>
            {!isUser && <CopyButton text={message.content} label="Copy answer" copiedLabel="Copied answer" />}
          </div>
        )}
        <Bubble variant={isUser ? "default" : "ghost"} align={isUser ? "end" : "start"} className="max-w-full overflow-hidden">
          <BubbleContent>
            {isUser ? <div className="whitespace-pre-wrap break-words">{message.content}</div> : <MarkdownContent value={message.content} />}
          </BubbleContent>
        </Bubble>
        {response && <NoticeList notices={response.notices} onPinTask={onPinTask} />}
        {response?.errors?.length > 0 && <NoticeFrame icon={AlertTriangle} title="Agent errors" message={response.errors.map((error) => error.message).filter(Boolean).join(" · ")} variant="destructive" />}
      </MessageContent>
    </Message>
  );
}

export function ResponseSummaryBadges({ response, onInspect }) {
  const delegations = responseDelegations(response);
  // A lead's ask_graphjin actions are the questions themselves, so they are
  // counted as questions rather than actions.
  const actionCount = Array.isArray(response.actions) ? response.actions.filter((action) => !isDelegationAction(action)).length : 0;
  const noticeCount = Array.isArray(response.notices) ? response.notices.length : 0;
  const actionText = `${actionCount} ${actionCount === 1 ? "action" : "actions"}`;
  return (
    <>
      {hasValue(response.data) && <Button type="button" variant="ghost" size="sm" onClick={() => onInspect({ response, tab: "data", title: "Agent result" })}>Data</Button>}
      {hasValue(response.evidence) && <Button type="button" variant="ghost" size="sm" onClick={() => onInspect({ response, tab: "evidence", title: "Evidence" })}>Evidence</Button>}
      {delegations ? (
        <Button type="button" variant="ghost" size="sm" onClick={() => onInspect({ response, tab: delegations.length > 0 ? "questions" : "actions", title: "Questions" })}>
          {delegations.length} {delegations.length === 1 ? "question" : "questions"} · {actionText}
        </Button>
      ) : actionCount > 0 && <Button type="button" variant="ghost" size="sm" onClick={() => onInspect({ response, tab: "actions", title: "Actions" })}>{actionText}</Button>}
      {noticeCount > 0 && <Badge variant="warning">{noticeCount} {noticeCount === 1 ? "notice" : "notices"}</Badge>}
      {response.trace_id && (
        <button type="button" className="inline-flex max-w-48 items-center truncate rounded-md px-2 py-1 text-xs text-muted-foreground hover:bg-muted hover:text-foreground" title={response.trace_id} onClick={() => onInspect({ response, tab: hasValue(response.trace) ? "trace" : "usage", title: "Run details" })}>
          trace {response.trace_id}
        </button>
      )}
    </>
  );
}

function PinnedTask({ taskID, task, isLoading, onClear }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg bg-warning/10 px-3 py-2">
      <div className="flex min-w-0 items-center gap-2">
        <Pin className="size-4 shrink-0 text-warning" aria-hidden="true" />
        <div className="min-w-0">
          <p className="text-xs font-semibold uppercase text-warning">Pinned task</p>
          <p className="truncate text-sm text-foreground">
            {isLoading ? "Loading declared goal…" : task?.goal || taskID}
          </p>
        </div>
      </div>
      <div className="flex items-center gap-1">
        <Button asChild type="button" variant="ghost" size="sm">
          <Link to={`/user/tasks?task_id=${encodeURIComponent(taskID)}`}>Open trail</Link>
        </Button>
        <Button type="button" variant="ghost" size="icon" className="size-8" onClick={onClear} aria-label="Unpin task">
          <X aria-hidden="true" />
        </Button>
      </div>
    </div>
  );
}

function NoticeList({ notices, onPinTask }) {
  if (!Array.isArray(notices) || notices.length === 0) {
    return null;
  }
  return (
    <div className="grid w-full gap-2" aria-label={`${notices.length} agent notices`}>
      {notices.map((notice, index) => (
        <AgentNotice key={`${notice?.kind || "notice"}-${index}`} notice={notice || {}} onPinTask={onPinTask} />
      ))}
    </div>
  );
}

function AgentNotice({ notice, onPinTask }) {
  const taskIDs = Array.isArray(notice.task_ids) ? notice.task_ids.filter(Boolean) : [];
  const kind = notice.kind || "notice";

  if (kind === "watch_created") {
    return (
      <NoticeFrame icon={Radar} title="Watch created" message={notice.message}>
        <Button asChild variant="outline" size="sm">
          <Link to="/user/watches">Open watches</Link>
        </Button>
      </NoticeFrame>
    );
  }
  if (kind === "watch_events_unseen") {
    return (
      <NoticeFrame icon={BellRing} title={`${notice.count || "New"} unseen watch ${notice.count === 1 ? "event" : "events"}`} message={notice.message}>
        <Button asChild variant="outline" size="sm">
          <Link to="/user/watches">Open watch inbox</Link>
        </Button>
      </NoticeFrame>
    );
  }
  if (kind === "task_open_unlinked") {
    return (
      <NoticeFrame icon={ListTodo} title="Open tasks are not linked to this run" message={notice.message}>
        <div className="flex flex-wrap gap-2">
          {taskIDs.map((taskID) => (
            <Button key={taskID} type="button" variant="outline" size="sm" onClick={() => onPinTask(taskID)}>
              <Pin aria-hidden="true" />
              Continue {shortID(taskID)}
            </Button>
          ))}
        </div>
      </NoticeFrame>
    );
  }
  if (kind === "task_context_loaded") {
    return (
      <NoticeFrame icon={CheckCircle2} title="Declared task context loaded" message={notice.message} variant="success">
        {taskIDs[0] && (
          <Button asChild variant="ghost" size="sm">
            <Link to={`/user/tasks?task_id=${encodeURIComponent(taskIDs[0])}`}>View trail</Link>
          </Button>
        )}
      </NoticeFrame>
    );
  }
  if (kind === "task_verify_failed") {
    return (
      <NoticeFrame icon={AlertTriangle} title="Task verification failed" message={notice.message} variant="destructive">
        <div className="flex flex-wrap gap-2">
          {taskIDs.map((taskID) => (
            <Button key={taskID} asChild variant="outline" size="sm">
              <Link to={`/user/tasks?task_id=${encodeURIComponent(taskID)}`}>Inspect {shortID(taskID)}</Link>
            </Button>
          ))}
        </div>
      </NoticeFrame>
    );
  }
  if (kind === "annotations_unshared") {
    return (
      <NoticeFrame icon={BookOpenCheck} title={`${notice.count || "Annotation"} ${notice.count === 1 ? "draft" : "drafts"} awaiting review`} message={notice.message}>
        <Button asChild variant="outline" size="sm">
          <Link to="/user/artifacts">Open review queue</Link>
        </Button>
      </NoticeFrame>
    );
  }
  return <NoticeFrame icon={CircleHelp} title={kind.replaceAll("_", " ")} message={notice.message || "GraphJin returned an agent notice."} />;
}

function NoticeFrame({ icon: Icon, title, message, children, variant = "default" }) {
  return (
    <div className={cn(
      "grid gap-3 rounded-lg bg-card p-3 text-sm",
      variant === "destructive" && "border-destructive/45",
      variant === "success" && "border-success/40"
    )}>
      <div className="flex items-start gap-2">
        <Icon className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
        <div className="min-w-0">
          <p className="font-semibold capitalize text-foreground">{title}</p>
          {message && <p className="mt-1 text-xs leading-5 text-muted-foreground">{message}</p>}
        </div>
      </div>
      {children}
    </div>
  );
}

function shortID(value) {
  if (typeof value !== "string" || value.length <= 18) {
    return value;
  }
  return `${value.slice(0, 8)}…${value.slice(-6)}`;
}

function payloadSummary(value) {
  if (Array.isArray(value)) {
    return `${value.length} ${value.length === 1 ? "item" : "items"}`;
  }
  if (value && typeof value === "object") {
    const count = Object.keys(value).length;
    return `${count} ${count === 1 ? "key" : "keys"}`;
  }
  if (typeof value === "string") {
    return `${value.length} chars`;
  }
  return typeof value;
}

function hasValue(value) {
  if (value == null) {
    return false;
  }
  if (typeof value === "string") {
    return value.trim() !== "";
  }
  if (Array.isArray(value)) {
    return value.length > 0;
  }
  if (typeof value === "object") {
    return Object.keys(value).length > 0;
  }
  return true;
}

function fallbackAnswer(response) {
  if (response?.status === "needs_clarification") {
    return "I need a little more detail before I can answer safely.";
  }
  if (response?.errors?.length) {
    return response.errors.map((error) => error.message).filter(Boolean).join("\n") || "The agent returned errors.";
  }
  return "The agent returned a response without an answer.";
}

function displayAnswer(response) {
  return extractClarificationQuestion(response?.answer) || response?.answer || fallbackAnswer(response);
}

function extractClarificationQuestion(answer) {
  if (typeof answer !== "string" || !answer.trim().startsWith("{")) {
    return "";
  }
  try {
    return findQuestion(JSON.parse(answer));
  } catch {
    return "";
  }
}

function findQuestion(value) {
  if (Array.isArray(value)) {
    for (const item of value) {
      const question = findQuestion(item);
      if (question) {
        return question;
      }
    }
    return "";
  }
  if (value && typeof value === "object") {
    if (typeof value.question === "string" && value.question.trim()) {
      return value.question.trim();
    }
    if (value.args) {
      return findQuestion(value.args);
    }
  }
  return "";
}

// historyFromMessages maps the visible chat into agent request history turns
// (most recent 12), carrying each assistant turn's status and the catalog ids
// it inspected as warm-start hints for the next run.
function historyFromMessages(messages) {
  return messages
    .filter((message) => (message.role === "user" || message.role === "assistant") && typeof message.content === "string" && message.content.trim() !== "")
    .slice(-12)
    .map((message) => {
      const turn = { role: message.role, content: message.content };
      if (message.role === "assistant") {
        if (message.status) {
          turn.status = message.status;
        }
        const catalogIDs = catalogIDsFromResponse(message.response);
        if (catalogIDs.length > 0) {
          turn.catalog_ids = catalogIDs;
        }
      }
      return turn;
    });
}

function catalogIDsFromResponse(response) {
  const evidence = response?.evidence;
  const protocol = evidence?.protocol || evidence;
  const ids = protocol?.catalog_detail_ids;
  if (!Array.isArray(ids)) {
    return [];
  }
  return ids.filter((id) => typeof id === "string" && id !== "").slice(0, 16);
}

function messageID() {
  if (typeof crypto !== "undefined" && crypto.randomUUID) {
    return crypto.randomUUID();
  }
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

export default AgentChat;
