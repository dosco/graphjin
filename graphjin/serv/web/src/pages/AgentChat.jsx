import React from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { AlertTriangle, Bot, CircleHelp, KeyRound, Send, Settings2, Timer, User } from "lucide-react";

import { DataErrorState, LoadingState, PageHeader, StatusPill } from "../components/ui";
import { agentStatus, askAgent } from "../services/agent";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Attachment } from "@/components/ui/attachment";
import { Badge } from "@/components/ui/badge";
import { Bubble } from "@/components/ui/bubble";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Marker } from "@/components/ui/marker";
import { Message, MessageAvatar, MessageContent } from "@/components/ui/message";
import { MessageScroller, MessageScrollerProvider } from "@/components/ui/message-scroller";
import { Select } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

const modes = [
  {
    value: "safe",
    label: "Safe queries",
    description: "Catalog, validation, help, and approved saved queries. Raw GraphQL stays unavailable.",
  },
  {
    value: "discovery_only",
    label: "Discovery only",
    description: "Catalog, validation, and help only. The agent will not execute data queries.",
  },
  {
    value: "raw_allowed",
    label: "Raw GraphQL",
    description: "Allows direct GraphQL execution when the server enables agent.allow_raw_graphql.",
  },
];

const initialMessages = [];

const AgentChat = () => {
  const statusQuery = useQuery({
    queryKey: ["agent-status"],
    queryFn: agentStatus,
    refetchInterval: 30000,
  });
  const status = statusQuery.data;
  const [messages, setMessages] = React.useState(initialMessages);
  const [instruction, setInstruction] = React.useState("");
  const [mode, setMode] = React.useState("safe");
  const [returnTrace, setReturnTrace] = React.useState(false);
  const [maxSteps, setMaxSteps] = React.useState(8);
  const [advancedOpen, setAdvancedOpen] = React.useState(false);

  React.useEffect(() => {
    if (status?.max_steps) {
      setMaxSteps(status.max_steps);
    }
  }, [status?.max_steps]);

  const mutation = useMutation({
    mutationFn: askAgent,
  });

  const ready = status?.ready === true;
  const rawGraphQLAllowed = status?.allow_raw_graphql === true;
  const inputDisabled = !ready || mutation.isPending;
  const canSubmit = ready && !mutation.isPending && instruction.trim().length > 0;

  React.useEffect(() => {
    if (mode === "raw_allowed" && status && !rawGraphQLAllowed) {
      setMode("safe");
    }
  }, [mode, status, rawGraphQLAllowed]);

  async function handleSubmit(event) {
    event.preventDefault();
    const text = instruction.trim();
    if (!text || !ready) {
      return;
    }

    setInstruction("");
    const userMessage = {
      id: messageID(),
      role: "user",
      content: text,
    };
    setMessages((current) => [...current, userMessage]);

    try {
      const showDebug = returnTrace;
      const response = await mutation.mutateAsync({
        instruction: text,
        mode,
        max_steps: Number(maxSteps) || undefined,
        return_trace: returnTrace,
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
    }
  }

  return (
    <div className="mx-auto grid max-w-5xl gap-5">
      <PageHeader
        title="Agent"
        description="Ask about catalog, runtime state, config, security, and saved queries."
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
        <Card className="overflow-hidden shadow-none">
          <CardContent className="grid h-[calc(100vh-15rem)] min-h-[560px] grid-rows-[auto_1fr_auto] p-0">
            <div className="flex items-center justify-between gap-3 border-b bg-muted/25 px-4 py-3">
              <div className="flex min-w-0 items-center gap-3">
                <div className="flex size-9 shrink-0 items-center justify-center rounded-md border bg-background text-muted-foreground">
                  <Bot className="size-4" aria-hidden="true" />
                </div>
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-foreground">GraphJin</p>
                  <p className="truncate text-xs text-muted-foreground">{ready ? "Ready" : status?.message || "Unavailable"}</p>
                </div>
              </div>
              <Badge variant="outline">{modeLabel(mode)}</Badge>
            </div>

            <MessageScrollerProvider>
              <MessageScroller className="min-h-0">
                {messages.length === 0 ? (
                  <EmptyAgentState ready={ready} />
                ) : (
                  messages.map((message) => (
                    <ChatMessage key={message.id} message={message} />
                  ))
                )}
                {mutation.isPending && (
                  <Message role="assistant">
                    <MessageAvatar><Bot className="size-4" /></MessageAvatar>
                    <MessageContent>
                      <div className="flex items-center gap-2">
                        <Marker status="loading" label="thinking" />
                      </div>
                      <Bubble role="assistant">GraphJin is working through the request.</Bubble>
                    </MessageContent>
                  </Message>
                )}
              </MessageScroller>
            </MessageScrollerProvider>

            <form className="grid gap-3 border-t bg-card p-4" onSubmit={handleSubmit}>
              {!ready && <AgentDisabledAlert status={status} />}
              <div className="overflow-hidden rounded-lg border bg-background shadow-xs focus-within:border-ring focus-within:ring-[3px] focus-within:ring-ring/50">
                <Textarea
                  value={instruction}
                  onChange={(event) => setInstruction(event.target.value)}
                  placeholder={ready ? "Ask the agent" : "Enable the GraphJin agent to chat"}
                  disabled={inputDisabled}
                  rows={3}
                  className="min-h-24 resize-none border-0 shadow-none focus-visible:ring-0"
                />
                <div className="flex items-center justify-between gap-3 border-t px-3 py-2">
                  <div className="flex min-w-0 items-center gap-2">
                    <Button type="button" variant="ghost" size="sm" onClick={() => setAdvancedOpen((open) => !open)} disabled={inputDisabled}>
                      <Settings2 className="size-4" aria-hidden="true" />
                      Options
                    </Button>
                    {returnTrace && <Badge variant="muted">Trace</Badge>}
                  </div>
                  <Button type="submit" size="sm" disabled={!canSubmit}>
                    <Send className="size-4" aria-hidden="true" />
                    Send
                  </Button>
                </div>
              </div>
              {advancedOpen && (
                <div className="grid gap-3 rounded-md border bg-muted/30 p-3 md:grid-cols-[minmax(10rem,1fr)_auto_auto] md:items-end">
                  <label className="grid gap-1.5 text-sm">
                    <span className="font-medium text-foreground">Mode</span>
                    <Select value={mode} onChange={(event) => setMode(event.target.value)} disabled={inputDisabled}>
                      {modes.map((item) => (
                        <option key={item.value} value={item.value} disabled={item.value === "raw_allowed" && !rawGraphQLAllowed}>
                          {item.label}
                        </option>
                      ))}
                    </Select>
                  </label>
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
                  <label className="flex h-9 items-center gap-2 text-sm font-medium text-foreground">
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

function EmptyAgentState({ ready }) {
  return (
    <div className="flex min-h-[22rem] items-center justify-center p-6">
      <div className="grid justify-items-center gap-3 text-center">
        <div className="flex size-11 items-center justify-center rounded-md border bg-muted text-muted-foreground">
          <Bot className="size-5" aria-hidden="true" />
        </div>
        <div className="grid gap-1">
          <p className="text-sm font-medium text-foreground">{ready ? "Ready" : "Agent unavailable"}</p>
          <p className="text-xs text-muted-foreground">{ready ? "Ask a question below." : "Check readiness details."}</p>
        </div>
      </div>
    </div>
  );
}

function modeLabel(value) {
  return modes.find((item) => item.value === value)?.label || "Safe queries";
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
        <SheetContent side="right" className="grid w-[min(92vw,26rem)] grid-rows-[auto_1fr] sm:w-[26rem]">
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
      <dl className="grid gap-3 text-sm">
        <StatusRow icon={Settings2} label="Provider" value={[status.provider, status.model].filter(Boolean).join(" / ") || "openai"} />
        <StatusRow icon={KeyRound} label="API key env" value={status.api_key_env || "OPENAI_API_KEY"} />
        <StatusRow icon={CircleHelp} label="REST endpoint" value={status.endpoint || "/api/v1/agent"} />
        <StatusRow icon={Bot} label="MCP tool" value={status.mcp_tool || "ask_graphjin_agent"} />
        <StatusRow icon={Timer} label="Timeout" value={`${status.timeout_seconds || 50}s`} />
      </dl>
      <div className="grid gap-2 text-sm text-muted-foreground">
        <div className="flex items-center justify-between gap-3">
          <span>Raw GraphQL</span>
          <Badge variant={status.allow_raw_graphql ? "success" : "muted"}>{status.allow_raw_graphql ? "allowed" : "off"}</Badge>
        </div>
        <div className="flex items-center justify-between gap-3">
          <span>Mutations</span>
          <Badge variant={status.allow_mutations ? "success" : "muted"}>{status.allow_mutations ? "allowed" : "off"}</Badge>
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
    <div className="grid grid-cols-[1rem_7rem_minmax(0,1fr)] items-center gap-2">
      <Icon className="size-4 text-muted-foreground" aria-hidden="true" />
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 truncate font-medium">{value}</dd>
    </div>
  );
}

function AgentDisabledAlert({ status }) {
  const missingKey = status.enabled && !status.api_key_configured;
  return (
    <Alert variant="warning">
      <AlertTriangle className="size-4" aria-hidden="true" />
      <AlertTitle>{missingKey ? "Agent key is missing" : "Agent is not enabled"}</AlertTitle>
      <AlertDescription>
        {missingKey ? (
          <>Set <code>{status.api_key_env || "OPENAI_API_KEY"}</code> in the GraphJin service environment.</>
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
      {!isUser && <MessageAvatar><Bot className="size-4" /></MessageAvatar>}
      <MessageContent className={cn(isUser && "items-end")}>
        {response && (
          <div className="flex flex-wrap items-center gap-2">
            <Marker status={response.status || message.status || "answered"} />
            {message.showDebug && response.trace_id && (
              <Badge variant="outline" className="max-w-48 truncate" title={response.trace_id}>
                trace {response.trace_id}
              </Badge>
            )}
          </div>
        )}
        <Bubble role={message.role} className="max-w-full overflow-hidden">
          <div className="whitespace-pre-wrap break-words">{message.content}</div>
        </Bubble>
        {response && <ResponseAttachments response={response} showDebug={message.showDebug} />}
      </MessageContent>
      {isUser && <MessageAvatar className="bg-primary text-primary-foreground"><User className="size-4" /></MessageAvatar>}
    </Message>
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
        <Attachment title="Errors" className="border-red-200 bg-red-50">
          <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-words text-xs leading-5 text-red-900">
            <code>{JSON.stringify(response.errors, null, 2)}</code>
          </pre>
        </Attachment>
      )}
    </div>
  );
}

function JSONAttachment({ title, value }) {
  if (!hasValue(value)) {
    return null;
  }
  return (
    <Attachment title={title}>
      <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-words text-xs leading-5">
        <code>{JSON.stringify(value, null, 2)}</code>
      </pre>
    </Attachment>
  );
}

function hasDebugPayload(response) {
  return hasValue(response.evidence) || hasValue(response.actions) || hasValue(response.next) || hasValue(response.usage) || hasValue(response.trace);
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

function messageID() {
  if (typeof crypto !== "undefined" && crypto.randomUUID) {
    return crypto.randomUUID();
  }
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

export default AgentChat;
