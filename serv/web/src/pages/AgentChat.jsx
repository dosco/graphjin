import React from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import {
  AlertTriangle,
  BellRing,
  BookOpenCheck,
  Bot,
  CheckCircle2,
  CircleHelp,
  KeyRound,
  ListTodo,
  Pin,
  PanelRightClose,
  Send,
  Settings2,
  Sparkles,
  Timer,
  User,
  X,
} from "lucide-react";

import { DataErrorState, LoadingState, StatusPill } from "../components/ui";
import { agentStatus, askAgentStream } from "../services/agent";
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
import { Message, MessageAvatar, MessageContent } from "@/components/ui/message";
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
    label: "Runtime posture",
    text: "Summarize the current runtime posture and the next safest operator action.",
  },
  {
    label: "Security check",
    text: "Check security posture before I ask the agent to make any changes.",
  },
  {
    label: "Find saved queries",
    text: "Find saved queries that help inspect source health and recent runtime events.",
  },
];

const AgentChat = () => {
  const identity = useOperatorIdentity();
  const identityKey = operatorIdentityKey(identity);
  const [searchParams, setSearchParams] = useSearchParams();
  const linkedTaskID = searchParams.get("task_id") || "";
  const statusQuery = useQuery({
    queryKey: ["agent-status", identityKey],
    queryFn: agentStatus,
    refetchInterval: 30000,
  });
  const status = statusQuery.data;
  const [messages, setMessages] = React.useState(initialMessages);
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

  const mutation = useMutation({
    mutationFn: (vars) =>
      askAgentStream(vars, {
        onAction: (action) => setPendingActions((current) => [...current, action]),
      }),
  });

  const ready = status?.ready === true;
  const inputDisabled = !ready || mutation.isPending;
  const canSubmit = ready && !mutation.isPending && instruction.trim().length > 0;

  async function submitInstruction() {
    const text = instruction.trim();
    if (!text || !ready || mutation.isPending) {
      return;
    }

    setInstruction("");
    // Prior turns become the request history so follow-ups and clarification
    // answers keep their conversation context (the agent still re-discovers
    // its own evidence every run).
    const history = historyFromMessages(messages);
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
    <div className="grid h-full min-h-0 min-w-0 grid-cols-[minmax(0,1fr)] grid-rows-[minmax(0,1fr)_auto] bg-background">
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
                  <MessageScrollerItem key={message.id} scrollAnchor={index === messages.length - 1 && !mutation.isPending}>
                    <ChatMessage message={message} onPinTask={pinTask} onInspect={setInspection} />
                  </MessageScrollerItem>
                ))
              )}
              {mutation.isPending && (
                <MessageScrollerItem scrollAnchor>
                  <ThinkingMessage actions={pendingActions} />
                </MessageScrollerItem>
              )}
            </MessageScrollerContent>
          </MessageScrollerViewport>
          <MessageScrollerButton />
        </MessageScroller>
      </MessageScrollerProvider>

      <form className="min-w-0 border-t bg-background/96 px-3 py-3 backdrop-blur sm:px-6" onSubmit={handleSubmit}>
        <div className="mx-auto grid w-full min-w-0 max-w-3xl gap-2.5">
          {!ready && <AgentDisabledAlert status={status} />}
          {pinnedTaskID && (
            <PinnedTask taskID={pinnedTaskID} task={pinnedTaskQuery.data} isLoading={pinnedTaskQuery.isLoading} onClear={clearPinnedTask} />
          )}
          <div className="min-w-0 overflow-hidden rounded-2xl border bg-card shadow-[0_10px_35px_rgba(15,23,42,0.08)] transition focus-within:border-ring focus-within:ring-[3px] focus-within:ring-ring/20">
            <Textarea
              value={instruction}
              onChange={(event) => setInstruction(event.target.value)}
              onKeyDown={handleInstructionKeyDown}
              placeholder={ready ? "Ask GraphJin about your data, runtime, or next action" : "Enable the GraphJin agent to chat"}
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
              <Button type="submit" size="icon" className="rounded-xl" disabled={!canSubmit} aria-label="Send message">
                <Send aria-hidden="true" />
              </Button>
            </div>
          </div>
          {advancedOpen && (
            <div className="flex flex-wrap items-end justify-between gap-3 rounded-xl bg-muted/55 px-3 py-2.5">
              <label className="flex items-center gap-2 text-sm"><span className="font-medium">Steps</span><Input type="number" min="1" max="24" value={maxSteps} onChange={(event) => setMaxSteps(event.target.value)} className="h-8 w-20 bg-background" disabled={inputDisabled} /></label>
              <label className="flex h-8 items-center gap-2 text-sm font-medium"><Checkbox checked={returnTrace} onCheckedChange={(checked) => setReturnTrace(checked === true)} disabled={inputDisabled} />Return trace</label>
            </div>
          )}
        </div>
      </form>
    </div>
  );

  return (
    <div className="h-[calc(100dvh-6.25rem)] min-h-[38rem] overflow-hidden bg-background">
      {statusQuery.isLoading ? (
        <div className="p-6"><LoadingState label="Checking agent status" /></div>
      ) : statusQuery.error ? (
        <div className="p-6"><DataErrorState error={statusQuery.error} permissionMessage="Agent status is authenticated through the current GraphJin session. Open the console with an operator/admin identity." unavailableMessage="The agent status endpoint could not be reached from /api/v1/agent/status." /></div>
      ) : (
        <div className="grid h-full min-h-0 min-w-0 grid-cols-[minmax(0,1fr)] grid-rows-[auto_minmax(0,1fr)]">
          <div className="flex min-h-14 items-center justify-between gap-4 border-b px-4 py-2 sm:px-6">
            <div className="flex min-w-0 items-center gap-3">
              <span className={cn("flex size-8 shrink-0 items-center justify-center rounded-full bg-muted", ready ? "text-foreground" : "text-muted-foreground")}><Bot className="size-4" aria-hidden="true" /></span>
              <div className="min-w-0"><div className="flex items-center gap-2"><h1 className="truncate text-sm font-semibold">GraphJin Agent</h1><StatusPill status={ready ? "ready" : status?.status || "pending"} /></div><p className="hidden truncate text-xs text-muted-foreground sm:block">{ready ? "Governed by the current caller and catalog." : status?.message}</p></div>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              <Badge variant="muted" className="hidden max-w-56 truncate md:inline-flex" title={providerLabel}><Sparkles aria-hidden="true" />{providerLabel}</Badge>
              {inspection && <Button type="button" variant="ghost" size="sm" className="hidden lg:inline-flex" onClick={() => setInspection(null)}><PanelRightClose aria-hidden="true" />Close inspector</Button>}
              <AgentStatusAction status={status} />
            </div>
          </div>
          <div className="min-h-0 min-w-0 overflow-hidden">
            {inspection && desktop ? (
              <ResizablePanelGroup orientation="horizontal" className="h-full">
                <ResizablePanel defaultSize="65%" minSize="42%">{chatSurface}</ResizablePanel>
                <ResizableHandle withHandle />
                <ResizablePanel defaultSize="35%" minSize="24%" maxSize="52%"><AgentInspector inspection={inspection} onClose={() => setInspection(null)} /></ResizablePanel>
              </ResizablePanelGroup>
            ) : chatSurface}
            {!desktop && <Sheet open={Boolean(inspection)} onOpenChange={(open) => !open && setInspection(null)}><SheetContent side="right" className="w-[min(96vw,42rem)] p-0 sm:w-[42rem]">{inspection && <AgentInspector inspection={inspection} onClose={() => setInspection(null)} compact />}</SheetContent></Sheet>}
          </div>
        </div>
      )}
    </div>
  );
};

function AgentInspector({ inspection, onClose, compact = false }) {
  const response = inspection?.response || {};
  const payloads = React.useMemo(() => [
    { id: "data", label: "Data", value: response.data },
    { id: "evidence", label: "Evidence", value: response.evidence },
    { id: "actions", label: "Actions", value: response.actions },
    { id: "next", label: "Next", value: response.next },
    { id: "usage", label: "Usage", value: response.usage },
    { id: "trace", label: "Trace", value: response.trace },
    { id: "errors", label: "Errors", value: response.errors },
  ].filter((item) => hasValue(item.value)), [response]);
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
          <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Inspector</p>
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
              {selected.value.map((action, index) => (
                <li key={`${action?.index ?? index}-${action?.tool || "action"}`} className="grid gap-1 py-3 first:pt-0">
                  <div className="flex items-center justify-between gap-3"><span className="font-mono text-sm font-medium">{action?.tool || `Action ${index + 1}`}</span><StatusPill status={action?.status || "completed"} /></div>
                  {action?.args && <pre className="overflow-auto whitespace-pre-wrap break-words rounded-lg bg-muted/55 p-3 text-xs leading-5"><code>{JSON.stringify(action.args, null, 2)}</code></pre>}
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
        <div className={cn(
          "flex size-12 items-center justify-center rounded-full bg-muted",
          ready ? "text-foreground" : "text-muted-foreground"
        )}>
          <Bot className="size-5" aria-hidden="true" />
        </div>
        <div className="grid gap-1">
          <p className="text-base font-semibold text-foreground">{ready ? "Ready for governed work" : "Agent unavailable"}</p>
          <p className="text-sm leading-6 text-muted-foreground">
            {ready ? "Catalog, runtime, and security context are available." : "Readiness details are available."}
          </p>
        </div>
        <div className="w-full divide-y border-y text-left">
          {promptStarters.map((prompt) => (
            <button
              type="button"
              key={prompt.label}
              disabled={!ready}
              onClick={() => onSelectPrompt(prompt.text)}
              className="group grid w-full gap-0.5 px-2 py-3 text-left text-sm transition hover:bg-muted/55 disabled:cursor-not-allowed disabled:opacity-50 sm:grid-cols-[9rem_minmax(0,1fr)] sm:items-baseline sm:gap-4"
            >
              <span className="font-semibold text-foreground">{prompt.label}</span>
              <span className="text-xs leading-5 text-muted-foreground group-hover:text-foreground/75">{prompt.text}</span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

function ThinkingMessage({ actions = [] }) {
  return (
    <Message role="assistant">
      <MessageAvatar className="text-primary"><Bot className="size-4" /></MessageAvatar>
      <MessageContent>
        <div className="flex items-center gap-2">
          <Marker status="loading" label="working" />
          <span className="text-xs text-muted-foreground">catalog-first loop active</span>
        </div>
        <Bubble variant="muted" className="w-fit max-w-full">
          <BubbleContent>
          <span>GraphJin is working</span>
          {actions.length > 0 && (
            <ol className="mt-2 grid gap-1 border-t pt-2 font-mono text-xs text-muted-foreground">
              {actions.map((action) => (
                <li key={`${action.index}-${action.tool}`} className="flex items-center gap-2">
                  {action.status === "error" ? (
                    <AlertTriangle className="size-3.5 shrink-0 text-destructive" aria-hidden="true" />
                  ) : (
                    <CheckCircle2 className="size-3.5 shrink-0 text-emerald-600" aria-hidden="true" />
                  )}
                  <span className="truncate">{actionLabel(action)}</span>
                </li>
              ))}
            </ol>
          )}
          </BubbleContent>
        </Bubble>
      </MessageContent>
    </Message>
  );
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
      <StatusPill status={status.status} />
      <Sheet>
        <SheetTrigger asChild>
          <Button type="button" variant="outline" size="sm">
            <CircleHelp className="size-4" aria-hidden="true" />
            Details
          </Button>
        </SheetTrigger>
        <SheetContent side="right" className="grid w-[min(92vw,28rem)] grid-rows-[auto_1fr] border-white/70 bg-background/88 sm:w-[28rem]">
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
      <div className="grid gap-2 rounded-lg border bg-card p-3 text-sm text-muted-foreground">
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
    <div className="grid grid-cols-[2rem_7rem_minmax(0,1fr)] items-center gap-2 rounded-lg border bg-card p-2">
      <span className="flex size-8 items-center justify-center rounded-md bg-muted text-foreground">
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
      {!isUser && <MessageAvatar className="text-primary"><Bot className="size-4" /></MessageAvatar>}
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
      {isUser && <MessageAvatar className="border-primary/35 bg-primary text-primary-foreground shadow-[0_6px_18px_rgba(28,35,48,0.12)]"><User className="size-4" /></MessageAvatar>}
    </Message>
  );
}

function ResponseSummaryBadges({ response, onInspect }) {
  const actionCount = Array.isArray(response.actions) ? response.actions.length : 0;
  const noticeCount = Array.isArray(response.notices) ? response.notices.length : 0;
  return (
    <>
      {hasValue(response.data) && <Button type="button" variant="ghost" size="sm" onClick={() => onInspect({ response, tab: "data", title: "Agent result" })}>Data</Button>}
      {hasValue(response.evidence) && <Button type="button" variant="ghost" size="sm" onClick={() => onInspect({ response, tab: "evidence", title: "Evidence" })}>Evidence</Button>}
      {actionCount > 0 && <Button type="button" variant="ghost" size="sm" onClick={() => onInspect({ response, tab: "actions", title: "Actions" })}>{actionCount} {actionCount === 1 ? "action" : "actions"}</Button>}
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
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-amber-300 bg-amber-50/30 px-3 py-2">
      <div className="flex min-w-0 items-center gap-2">
        <Pin className="size-4 shrink-0 text-amber-800" aria-hidden="true" />
        <div className="min-w-0">
          <p className="text-xs font-semibold uppercase text-amber-900">Pinned task</p>
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
      "grid gap-3 rounded-lg border bg-card p-3 text-sm",
      variant === "destructive" && "border-red-300",
      variant === "success" && "border-emerald-300"
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
