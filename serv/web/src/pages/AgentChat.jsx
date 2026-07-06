import React from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { AlertTriangle, Bot, ChevronDown, CircleHelp, Gauge, KeyRound, Send, Settings2, Sparkles, Timer, User } from "lucide-react";

import { DataErrorState, LoadingState, PageHeader, StatusPill } from "../components/ui";
import { agentStatus, askAgentStream } from "../services/agent";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Bubble } from "@/components/ui/bubble";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { CopyButton, MarkdownContent } from "@/components/ui/markdown-content";
import { Marker } from "@/components/ui/marker";
import { Message, MessageAvatar, MessageContent } from "@/components/ui/message";
import { MessageScroller, MessageScrollerProvider } from "@/components/ui/message-scroller";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
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
  const statusQuery = useQuery({
    queryKey: ["agent-status"],
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
  const statusDefaultsApplied = React.useRef(false);

  React.useEffect(() => {
    if (status && !statusDefaultsApplied.current) {
      statusDefaultsApplied.current = true;
      if (status.max_steps) {
        setMaxSteps(status.max_steps);
      }
      setReturnTrace(status.return_trace === true);
    }
  }, [status]);

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
      const showDebug = returnTrace;
      const response = await mutation.mutateAsync({
        instruction: text,
        max_steps: Number(maxSteps) || undefined,
        return_trace: returnTrace,
        history: history.length ? history : undefined,
      });
      setMessages((current) => [
        ...current,
        {
          id: messageID(),
          role: "assistant",
          content: displayAnswer(response),
          response,
          showDebug,
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

  const providerLabel = [status?.provider, status?.model].filter(Boolean).join(" / ") || "provider pending";

  return (
    <div className="mx-auto grid max-w-6xl gap-5">
      <PageHeader
        eyebrow="Agentic Control"
        title="Agent"
        description="Ask GraphJin through the same governed catalog, runtime, config, security, and saved-query surfaces exposed to agents."
        actions={status && <AgentStatusAction status={status} />}
      />

      {statusQuery.isLoading ? (
        <LoadingState label="Checking agent status" />
      ) : statusQuery.error ? (
        <DataErrorState
          error={statusQuery.error}
          permissionMessage="Agent status is authenticated through the current GraphJin session. Open the console with an operator/admin identity."
          unavailableMessage="The agent status endpoint could not be reached from /api/v1/agent/status."
        />
      ) : (
        <Card className="overflow-hidden border-border bg-card shadow-[0_18px_60px_rgba(28,35,48,0.08)]">
          <CardContent className="grid h-[calc(100vh-14rem)] min-h-[620px] grid-rows-[auto_minmax(0,1fr)_auto] p-0">
            <div className="grid gap-4 border-b bg-card px-4 py-4 md:px-5 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center">
              <div className="flex min-w-0 items-start gap-3">
                <div className={cn(
                  "flex size-11 shrink-0 items-center justify-center rounded-lg border shadow-[0_1px_2px_rgba(28,35,48,0.06)]",
                  ready ? "bg-muted text-foreground" : "bg-card text-muted-foreground"
                )}>
                  <Bot className="size-5" aria-hidden="true" />
                </div>
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <p className="truncate text-sm font-semibold text-foreground">GraphJin Agent</p>
                    <StatusPill status={ready ? "ready" : status?.status || "pending"} />
                  </div>
                  <p className="mt-1 max-w-2xl text-sm leading-6 text-muted-foreground">
                    {ready ? "Ready for catalog-first work with caller-scoped permissions." : status?.message || "Agent readiness is unavailable."}
                  </p>
                  <div className="mt-3 flex flex-wrap gap-2">
                    <Badge variant="outline" className="max-w-64 truncate" title={providerLabel}>
                      <Sparkles className="size-3" aria-hidden="true" />
                      {providerLabel}
                    </Badge>
                    <Badge variant="muted">
                      <Gauge className="size-3" aria-hidden="true" />
                      {maxSteps || status?.max_steps || 8} steps
                    </Badge>
                    <Badge variant={returnTrace ? "success" : "muted"}>trace {returnTrace ? "on" : "off"}</Badge>
                    <Badge variant="muted">
                      <Timer className="size-3" aria-hidden="true" />
                      {status?.timeout_seconds || 50}s timeout
                    </Badge>
                  </div>
                </div>
              </div>
              <AgentPostureSummary status={status} />
            </div>

            <MessageScrollerProvider>
              <MessageScroller className="min-h-0 bg-card" autoScroll={messages.length > 0 || mutation.isPending}>
                {messages.length === 0 ? (
                  <EmptyAgentState ready={ready} onSelectPrompt={setInstruction} />
                ) : (
                  messages.map((message) => (
                    <ChatMessage key={message.id} message={message} />
                  ))
                )}
                {mutation.isPending && <ThinkingMessage actions={pendingActions} />}
              </MessageScroller>
            </MessageScrollerProvider>

            <form className="grid gap-3 border-t bg-card p-4" onSubmit={handleSubmit}>
              {!ready && <AgentDisabledAlert status={status} />}
              <div className="overflow-hidden rounded-lg border bg-card shadow-[0_6px_24px_rgba(28,35,48,0.06)] transition focus-within:border-ring focus-within:ring-[3px] focus-within:ring-ring/20">
                <Textarea
                  value={instruction}
                  onChange={(event) => setInstruction(event.target.value)}
                  onKeyDown={handleInstructionKeyDown}
                  placeholder={ready ? "Ask the GraphJin agent" : "Enable the GraphJin agent to chat"}
                  disabled={inputDisabled}
                  rows={3}
                  className="min-h-24 resize-none border-0 bg-transparent shadow-none focus-visible:bg-transparent focus-visible:ring-0"
                />
                <div className="flex flex-wrap items-center justify-between gap-3 border-t bg-card px-3 py-2">
                  <div className="flex min-w-0 items-center gap-2">
                    <Button type="button" variant="ghost" size="sm" onClick={() => setAdvancedOpen((open) => !open)} disabled={inputDisabled}>
                      <Settings2 className="size-4" aria-hidden="true" />
                      Run settings
                    </Button>
                    {returnTrace && <Badge variant="muted">Trace</Badge>}
                    <Badge variant="outline" className="hidden sm:inline-flex">Cmd/Ctrl + Enter</Badge>
                  </div>
                  <Button type="submit" size="sm" disabled={!canSubmit}>
                    <Send className="size-4" aria-hidden="true" />
                    Send
                  </Button>
                </div>
              </div>
              {advancedOpen && (
                <div className="grid gap-3 rounded-lg border bg-card p-3 md:grid-cols-[minmax(10rem,1fr)_auto] md:items-end">
                  <label className="grid gap-1.5 text-sm">
                    <span className="font-medium text-foreground">Steps</span>
                    <Input
                      type="number"
                      min="1"
                      max="24"
                      value={maxSteps}
                      onChange={(event) => setMaxSteps(event.target.value)}
                      className="h-9 w-24"
                      disabled={inputDisabled}
                    />
                  </label>
                  <label className="flex h-9 items-center gap-2 text-sm font-medium text-foreground md:justify-self-end">
                    <Checkbox checked={returnTrace} onCheckedChange={(checked) => setReturnTrace(checked === true)} disabled={inputDisabled} />
                    Trace
                  </label>
                </div>
              )}
            </form>
          </CardContent>
        </Card>
      )}
    </div>
  );
};

function EmptyAgentState({ ready, onSelectPrompt }) {
  return (
    <div className="flex min-h-[18rem] items-center justify-center p-4 md:min-h-[26rem] md:p-6">
      <div className="grid max-w-2xl justify-items-center gap-4 text-center md:gap-5">
        <div className={cn(
          "flex size-12 items-center justify-center rounded-lg border shadow-[0_1px_2px_rgba(28,35,48,0.06)]",
          ready ? "bg-muted text-foreground" : "bg-card text-muted-foreground"
        )}>
          <Bot className="size-5" aria-hidden="true" />
        </div>
        <div className="grid gap-1">
          <p className="text-base font-semibold text-foreground">{ready ? "Ready for governed work" : "Agent unavailable"}</p>
          <p className="text-sm leading-6 text-muted-foreground">
            {ready ? "Catalog, runtime, and security context are available." : "Readiness details are available."}
          </p>
        </div>
        <div className="grid w-full gap-2 sm:grid-cols-3">
          {promptStarters.map((prompt) => (
            <button
              type="button"
              key={prompt.label}
              disabled={!ready}
              onClick={() => onSelectPrompt(prompt.text)}
              className="min-h-20 rounded-lg border bg-card p-3 text-left text-sm shadow-[0_1px_2px_rgba(28,35,48,0.05)] transition hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
            >
              <span className="block font-semibold text-foreground">{prompt.label}</span>
              <span className="mt-1 block text-xs leading-5 text-muted-foreground">{prompt.text}</span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

function AgentPostureSummary({ status }) {
  return (
    <div className="grid min-w-[min(100%,22rem)] gap-2 rounded-lg border bg-card p-3 text-xs">
      <div className="flex items-center justify-between gap-3">
        <span className="font-semibold text-foreground">Server posture</span>
        <Badge variant={status?.read_only ? "warning" : "success"}>{status?.read_only ? "read-only" : "role-governed"}</Badge>
      </div>
      <div className="flex flex-wrap gap-2">
        <Badge variant={status?.return_trace ? "success" : "muted"}>default trace {status?.return_trace ? "on" : "off"}</Badge>
        <Badge variant="outline">{status?.max_steps || 8} default steps</Badge>
        <Badge variant="outline">{status?.timeout_seconds || 50}s max run</Badge>
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
        <Bubble role="assistant" className="w-fit max-w-full">
          <span className="inline-flex items-center gap-1.5">
            GraphJin is working
            <span className="inline-flex gap-1" aria-hidden="true">
              <span className="size-1.5 animate-pulse rounded-full bg-muted-foreground/55" />
              <span className="size-1.5 animate-pulse rounded-full bg-muted-foreground/55 [animation-delay:120ms]" />
              <span className="size-1.5 animate-pulse rounded-full bg-muted-foreground/55 [animation-delay:240ms]" />
            </span>
          </span>
          {actions.length > 0 && (
            <ol className="mt-2 grid gap-1 border-t pt-2 font-mono text-xs text-muted-foreground">
              {actions.map((action) => (
                <li key={`${action.index}-${action.tool}`} className="flex items-center gap-2">
                  <span className={cn(
                    "size-1.5 shrink-0 rounded-full",
                    action.status === "error" ? "bg-red-500" : "bg-emerald-500"
                  )} aria-hidden="true" />
                  <span className="truncate">{actionLabel(action)}</span>
                </li>
              ))}
            </ol>
          )}
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

function ChatMessage({ message }) {
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
              <ResponseSummaryBadges response={response} />
            </div>
            {!isUser && <CopyButton text={message.content} label="Copy answer" copiedLabel="Copied answer" />}
          </div>
        )}
        <Bubble role={message.role} className="max-w-full overflow-hidden">
          {isUser ? (
            <div className="whitespace-pre-wrap break-words">{message.content}</div>
          ) : (
            <MarkdownContent value={message.content} />
          )}
        </Bubble>
        {response && <ResponseAttachments response={response} showDebug={message.showDebug} />}
      </MessageContent>
      {isUser && <MessageAvatar className="border-primary/35 bg-primary text-primary-foreground shadow-[0_6px_18px_rgba(28,35,48,0.12)]"><User className="size-4" /></MessageAvatar>}
    </Message>
  );
}

function ResponseSummaryBadges({ response }) {
  const actionCount = Array.isArray(response.actions) ? response.actions.length : 0;
  return (
    <>
      {hasValue(response.data) && <Badge variant="outline">data</Badge>}
      {hasValue(response.evidence) && <Badge variant="outline">evidence</Badge>}
      {actionCount > 0 && <Badge variant="outline">{actionCount} {actionCount === 1 ? "action" : "actions"}</Badge>}
      {response.trace_id && (
        <Badge variant="outline" className="max-w-48 truncate" title={response.trace_id}>
          trace {response.trace_id}
        </Badge>
      )}
    </>
  );
}

function ResponseAttachments({ response, showDebug }) {
  const hasErrors = hasValue(response.errors);
  const hasData = hasValue(response.data);
  const hasDebug = hasDebugPayload(response);
  if (!hasErrors && !hasData && !(showDebug && hasDebug)) {
    return null;
  }

  return (
    <div className="grid gap-2">
      <JSONAttachment title="Data" value={response.data} />
      {showDebug && (
        <>
          <JSONAttachment title="Evidence" value={response.evidence} />
          <JSONAttachment title="Actions" value={response.actions} />
          <JSONAttachment title="Next" value={response.next} />
          <JSONAttachment title="Usage" value={response.usage} />
          <JSONAttachment title="Trace" value={response.trace} />
        </>
      )}
      {hasErrors && (
        <JSONAttachment title="Errors" value={response.errors} defaultOpen className="border-red-300 bg-card text-red-900" codeClassName="text-red-900" />
      )}
    </div>
  );
}

function JSONAttachment({ title, value, defaultOpen = false, className, codeClassName }) {
  if (!hasValue(value)) {
    return null;
  }
  return (
    <details
      open={defaultOpen}
      className={cn(
        "group rounded-lg border bg-card text-sm shadow-[0_1px_2px_rgba(28,35,48,0.05)]",
        className
      )}
    >
      <summary className="flex cursor-pointer list-none items-center justify-between gap-3 px-3 py-2 text-xs font-semibold uppercase text-muted-foreground [&::-webkit-details-marker]:hidden">
        <span>{title}</span>
        <span className="flex items-center gap-2">
          <Badge variant="outline">{payloadSummary(value)}</Badge>
          <ChevronDown className="size-4 transition group-open:rotate-180" aria-hidden="true" />
        </span>
      </summary>
      <div className="px-3 pb-3">
        <pre className={cn("max-h-72 overflow-auto whitespace-pre-wrap break-words rounded-md border bg-card p-3 text-xs leading-5 text-foreground/85", codeClassName)}>
          <code>{JSON.stringify(value, null, 2)}</code>
        </pre>
      </div>
    </details>
  );
}

function hasDebugPayload(response) {
  return hasValue(response.evidence) || hasValue(response.actions) || hasValue(response.next) || hasValue(response.usage) || hasValue(response.trace);
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
