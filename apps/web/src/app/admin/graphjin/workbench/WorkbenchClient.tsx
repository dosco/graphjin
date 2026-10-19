"use client";

import { useId, useState, type FormEvent } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Disclosure } from "@/components/ui/disclosure";
import { Field, Textarea } from "@/components/ui/field";

const DEFAULT_QUERY = `query SourceHealth {
  gj_runtime(where: { kind: { eq: "source" } }, order_by: { source: asc }) {
    source
    source_kind
    status
    severity
    summary
    next_action
  }
}
`;

type AgentTurn = {
  instruction: string;
  status: string;
  answer: string;
  notices: Array<{ kind: string; message: string }>;
  detail: Record<string, unknown>;
};

async function postJson(url: string, body: unknown): Promise<Record<string, unknown>> {
  const res = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const payload = (await res.json().catch(() => null)) as Record<string, unknown> | null;
  if (!res.ok) {
    throw new Error(typeof payload?.error === "string" ? payload.error : `HTTP ${res.status}`);
  }
  return payload ?? {};
}

export default function WorkbenchClient({
  agent,
}: {
  agent: { ready: boolean; detail: string };
}) {
  return (
    <div className="grid gap-4 xl:grid-cols-2">
      <QueryPanel />
      <AgentPanel agent={agent} />
    </div>
  );
}

function QueryPanel() {
  const queryId = useId();
  const variablesId = useId();
  const [query, setQuery] = useState(DEFAULT_QUERY);
  const [variables, setVariables] = useState("");
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<unknown>(null);

  async function run(event: FormEvent) {
    event.preventDefault();
    setError(null);
    let parsed: Record<string, unknown> | undefined;
    if (variables.trim()) {
      try {
        parsed = JSON.parse(variables) as Record<string, unknown>;
      } catch {
        setError("Variables must be a JSON object.");
        return;
      }
    }
    setRunning(true);
    try {
      setResult(await postJson("/api/admin/graphjin/query", { query, variables: parsed }));
    } catch (e) {
      setResult(null);
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setRunning(false);
    }
  }

  return (
    <section className="settings-card">
      <div className="settings-card-head">
        <div>
          <h2 className="settings-card-title">GraphQL</h2>
          <p className="settings-card-copy">
            Application roots and GraphJin system roots, run with an admin token.
          </p>
        </div>
      </div>
      <form className="grid gap-3" onSubmit={(event) => void run(event)}>
        <Field label="Query" htmlFor={queryId}>
          <Textarea
            id={queryId}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            className="min-h-72 font-mono text-xs"
            spellCheck={false}
          />
        </Field>
        <Field label="Variables" hint="Optional JSON object." htmlFor={variablesId}>
          <Textarea
            id={variablesId}
            value={variables}
            onChange={(event) => setVariables(event.target.value)}
            className="font-mono text-xs"
            placeholder="{}"
            spellCheck={false}
          />
        </Field>
        <div>
          <Button type="submit" variant="primary" disabled={running || !query.trim()}>
            {running ? "Running" : "Run query"}
          </Button>
        </div>
      </form>
      {error ? <p className="mt-3 text-ui-body-sm text-danger">{error}</p> : null}
      {result ? (
        <pre className="mt-3 max-h-[520px] overflow-auto whitespace-pre-wrap break-words rounded-inner bg-neutral-soft p-3 font-mono text-xs leading-5 text-text">
          <code>{JSON.stringify(result, null, 2)}</code>
        </pre>
      ) : null}
    </section>
  );
}

function AgentPanel({ agent }: { agent: { ready: boolean; detail: string } }) {
  const instructionId = useId();
  const [instruction, setInstruction] = useState("");
  const [turns, setTurns] = useState<AgentTurn[]>([]);
  const [asking, setAsking] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function ask(event: FormEvent) {
    event.preventDefault();
    const text = instruction.trim();
    if (!text) return;
    setError(null);
    setAsking(true);
    try {
      const history = turns.slice(-5).flatMap((turn) => [
        { role: "user", content: turn.instruction },
        { role: "assistant", content: turn.answer, status: turn.status },
      ]);
      const response = await postJson("/api/admin/graphjin/agent", { instruction: text, history });
      const { status, answer, notices, ...detail } = response;
      setTurns((current) => [
        ...current,
        {
          instruction: text,
          status: typeof status === "string" ? status : "unknown",
          answer: typeof answer === "string" ? answer : "",
          notices: Array.isArray(notices) ? (notices as AgentTurn["notices"]) : [],
          detail,
        },
      ]);
      setInstruction("");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setAsking(false);
    }
  }

  return (
    <section className="settings-card">
      <div className="settings-card-head">
        <div>
          <h2 className="settings-card-title">Agent</h2>
          <p className="settings-card-copy">{agent.detail}</p>
        </div>
        <Badge variant={agent.ready ? "success" : "watch"}>{agent.ready ? "Ready" : "Not ready"}</Badge>
      </div>

      {turns.length > 0 ? (
        <ol className="mb-4 grid gap-4">
          {turns.map((turn, index) => (
            <li key={index} className="grid gap-2 border-b border-border pb-4 last:border-0">
              <div className="text-ui-body-sm font-semibold text-text">{turn.instruction}</div>
              <div>
                <Badge variant={turn.status === "ok" || turn.status === "answered" ? "success" : "muted"}>
                  {turn.status}
                </Badge>
              </div>
              {turn.answer ? (
                <div className="work-markdown text-sm leading-[1.55] text-text2">
                  <ReactMarkdown remarkPlugins={[remarkGfm]}>{turn.answer}</ReactMarkdown>
                </div>
              ) : null}
              {turn.notices.map((notice, noticeIndex) => (
                <p key={noticeIndex} className="text-ui-body-sm text-text2">
                  <Badge variant="watch" className="mr-2">{notice.kind}</Badge>
                  {notice.message}
                </p>
              ))}
              {Object.keys(turn.detail).length > 0 ? (
                <Disclosure title="Data and evidence">
                  <pre className="max-h-[420px] overflow-auto whitespace-pre-wrap break-words font-mono text-xs leading-5 text-text">
                    <code>{JSON.stringify(turn.detail, null, 2)}</code>
                  </pre>
                </Disclosure>
              ) : null}
            </li>
          ))}
        </ol>
      ) : null}

      <form className="grid gap-3" onSubmit={(event) => void ask(event)}>
        <Field label="Ask GraphJin" htmlFor={instructionId}>
          <Textarea
            id={instructionId}
            value={instruction}
            onChange={(event) => setInstruction(event.target.value)}
            placeholder="Which sources are degraded, and why?"
            disabled={asking}
          />
        </Field>
        <div>
          <Button type="submit" variant="primary" disabled={asking || !instruction.trim()}>
            {asking ? "Asking" : "Ask"}
          </Button>
        </div>
      </form>
      {error ? <p className="mt-3 text-ui-body-sm text-danger">{error}</p> : null}
    </section>
  );
}
