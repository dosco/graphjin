---
title: "Server-Side Agent"
description: "Let GraphJin run the catalog-first discovery loop for you and return a typed answer through one MCP tool and one REST endpoint."
nav_group: "agentic"
doc_kind: "guide"
weight: 15
---

The server-side agent runs GraphJin's catalog-first discovery loop **inside GraphJin** and exposes it as a single MCP tool, `ask_graphjin_agent`, and a single REST endpoint, `POST /api/v1/agent`. Instead of your client chaining `query_catalog` → `query_catalog(id)` → `validate_where_clause` → `execute_saved_query` itself, you send one instruction and GraphJin discovers, validates, executes, and returns a typed, evidence-backed answer.

It is optional and off by default. Use it when you want GraphJin to orchestrate discovery in one governed, caller-scoped, auditable call; use the [MCP tools](/agentic/mcp/) directly when you want to drive the loop step by step yourself.

## Enable it

The agent block lives in `agentic.yml`, which loads when the environment is agentic:

```bash
GO_ENV=agentic graphjin serve --path ./config
```

```yaml
# agentic.yml
agent:
  enabled: true
  provider: openai
  model: gpt-4.1-mini
  api_key_env: OPENAI_API_KEY
```

See the [Config Reference](/reference/config-reference/) for all options.

## How it works

The agent is an RLM (reasoning-with-code) loop, not a tool-calling loop. The model **writes JavaScript** that calls the discovery and execution tools as runtime globals inside a sandbox — `query_catalog({...})`, `graphql_help({...})`, `validate_where_clause({...})`, `execute_saved_query({...})`, and `final({...})`. GraphJin runs that code, feeds results back, and the typed result is parsed from the model's `key: value` output.

This means the only model requirement is that it is **good at code generation** and follows the `key: value` output format. It does **not** need provider function-calling or structured-output/JSON modes, so any OpenAI-compatible chat-completions endpoint works (see [OpenAI-compatible endpoints](#openai-compatible-endpoints)).

Every answer is grounded in observed evidence. Go-side protocol guards enforce the catalog-first contract — for example a saved query may only run after its `saved_query` catalog row has been inspected — and downgrade an `answered` result to `blocked` (with evidence) when a required step was skipped. Models cannot talk their way past the guards; only real tool results count.

## Modes

| Mode | What the agent may do |
| :--- | :--- |
| `safe` (default) | Discover, validate, and run **approved saved queries/mutations**. No raw GraphQL. |
| `discovery_only` | Read-only: catalog/help/validation only. No execution. |
| `raw_allowed` | Adds `execute_graphql` for composed queries — only when `agent.allow_raw_graphql` and `mcp.allow_raw_queries` are both set. |

## Role-aware guidance

The agent is caller-aware. From the request's identity it derives a capability profile (limited to the fixed `gj_*` system roots — never application tables) and picks a focused **guidance skill** for the task: per domain (data, code, workflow, watch, admin) and per operation (read vs write, chosen from the instruction). An admin asking to change config is guided toward the `gj_config` recipe flow; a normal user is guided through plain data discovery.

Skills only shape the guidance the model sees. **They never grant access.** What any caller can actually read or write is enforced by GraphJin core (roles + row-level security), exactly as for a direct GraphQL request — the agent always runs as the caller.

{{< verified by="TestSelectSkill" file="agent/skills_test.go" line="88" >}}
{{< verified by="TestHasWriteIntent" file="agent/skills_test.go" line="65" >}}

## Request and response

Request fields: `instruction` (required), and optional `context`, `namespace`, `mode`, `max_steps`, `return_trace`.

Response fields: `status` (`answered` | `needs_clarification` | `blocked` | `error`), `answer`, and optional `data`, `evidence`, `actions`, `next`, `refusal`, `notices`, `errors`, `usage`, `trace`, `trace_id`.

### REST

```bash
curl -sS http://localhost:8080/api/v1/agent \
  -H 'content-type: application/json' \
  --data '{"instruction":"What production work should we prioritize next?","mode":"safe"}'
```

### MCP

```json
{
  "name": "ask_graphjin_agent",
  "arguments": {
    "instruction": "What production work should we prioritize next?",
    "mode": "safe"
  }
}
```

{{< verified by="TestAskGraphJinAgentMCPSchema" file="serv/agent_handler_test.go" line="347" >}}

## Structured refusals

A `blocked` response is machine-actionable, not prose. It carries a `refusal` object: a stable `code` (`access_unauthorized`, `capability_disabled`, `mutation_evidence_required`, ...), the `blocked_action`, evidence-backed `because` reasons, ordered `unblock` steps (each names a tool and args, filtered to the caller's visible capabilities so nothing hidden leaks), a `lawful_alternative` for when unblocking is impossible, and the `policy_final` / `retryable` pair. A calling agent should execute the unblock steps and retry only when `retryable` is true; `policy_final` means stop and escalate to an operator. The contract is discoverable at runtime with `query_catalog(id: "help:refusals")`.

## Watch notices

When the caller has unreviewed [watch events](/agentic/watches/), agent responses include a `notices` entry with kind `watch_events_unseen` and a count — the cue to query `gj_watch_event` and mark reviewed events seen.

## Borrow the client's model (MCP sampling)

```yaml
agent:
  sampling: auto # off (default) | auto | require
```

With `auto`, `ask_graphjin_agent` runs on the calling MCP client's model via `sampling/createMessage` whenever the client advertises the sampling capability, and falls back to the server-configured model otherwise; `require` fails closed instead of falling back. This removes the need for a server-side model key — the caller's client provides and pays for the reasoning. Only the model changes: caller identity, roles, row-level security, evidence gates, and refusals apply exactly as without sampling, and because the guards live in Go, a hostile sampling response cannot talk the agent past them. Works over stdio and, with `mcp.http_stateful: true`, over stateful HTTP sessions.

## OpenAI-compatible endpoints

Point the agent at any OpenAI-compatible chat-completions endpoint (vLLM, Ollama's OpenAI endpoint, OpenRouter, Together, LM Studio, Groq, …) with `base_url`:

```yaml
agent:
  enabled: true
  provider: openai            # or: openai-compatible
  model: <model-name>
  api_key_env: OPENAI_API_KEY # must hold a non-empty value (a dummy is fine for keyless local endpoints)
  base_url: https://your-endpoint/v1
```

Because the loop is driven by generated code rather than provider tool-calling, the practical requirement is a model that codegens well — not one with native function-calling support.

## When to use it

- **Use the server-side agent** to hand GraphJin a goal and get a typed answer in one call, with discovery, validation, and guardrails handled server-side and scoped to the caller.
- **Use [MCP tools](/agentic/mcp/) directly** when your own agent should own the loop and make each discovery/execution decision itself.
