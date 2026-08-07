---
title: "Server-Side Agent"
description: "Let GraphJin run the catalog-first discovery loop for you and return a typed answer through one MCP tool and one REST endpoint."
nav_group: "agentic"
doc_kind: "guide"
weight: 5
---

GraphJin's built-in agent - the server-side agent - runs the catalog-first discovery loop **inside GraphJin** and exposes it as a single MCP tool, `ask_graphjin_agent`, and a single REST endpoint, `POST /api/v1/agent`. Instead of your client chaining `query_catalog` → `query_catalog(id)` → `validate_where_clause` → `execute_saved_query` itself, you send one instruction and GraphJin discovers, validates, executes, and returns a typed, evidence-backed answer.

It is enabled by default in `dev` and `agentic` modes. Use it when you want GraphJin to orchestrate discovery in one governed, caller-scoped, auditable call; use the [MCP tools](/agentic/mcp/) directly when you want to drive the loop step by step yourself. Production mode keeps its existing opt-in behavior.

## Try it in five minutes

The coffee-roastery demo ships with data, workflows, source code, and the agent wired up:

```bash
# with OPENAI_API_KEY, ANTHROPIC_API_KEY, or GOOGLE_API_KEY in ./.env the demo
# switches into agentic mode and enables the built-in agent automatically
graphjin serve --demo --path examples/coffee-roastery
```

Then hand it a question:

```bash
curl -sS localhost:8080/api/v1/agent \
  -H 'content-type: application/json' \
  -d '{"instruction": "What production work should we prioritize next?"}'
```

The answer comes back as typed JSON - `status`, `answer`, `data`, `evidence`, `actions`, `next` - grounded in the discovery the agent actually performed on this run. The same conversation is available as a chat page in the built-in web console at `localhost:8080/agent`, streaming one action event per tool call as the agent works.

Agent chat also renders response notices as actions. Open tasks can be pinned
above the composer, which sends their `task_id` on later turns; watch, failed
verification, and annotation-draft notices link directly into
[Mission Control](/agentic/mission-control/).

More verticals - a zero-Docker SaaS ops demo, a corrugated-box plant with JWT roles, a PCB fab spanning eight sources - are on the [demos page](/start/demos/).

Once the agent answers a useful question, use [GraphJin Eval](/agentic/evaluation/)
to turn that confidence into a repeatable release check. It verifies the answer,
the database-side method, the governed behavior, and safety across repeated runs.
The same contract is published across vendors in [DeepORG](/benchmarks/deeporg/).

## Configure it

`agentic.yml` loads when the environment is agentic. The agent itself needs no feature toggle:

```bash
GO_ENV=agentic graphjin serve --path ./config
```

```yaml
# agentic.yml (provider overrides are optional)
agent:
  provider: openai
  model: gpt-4.1-mini
  api_key_env: OPENAI_API_KEY
```

See the [Config Reference](/reference/config-reference/) for all options.

## How it works

The agent is an RLM (reasoning-with-code) loop, not a tool-calling loop. The model **writes JavaScript** that calls the discovery and execution tools as runtime globals inside a sandbox — `query_catalog({...})`, `graphql_help({...})`, `validate_where_clause({...})`, `execute_saved_query({...})`, `execute_graphql({...})`, and `final({...})`. GraphJin runs that code, feeds results back, and the typed result is parsed from the model's `key: value` output. The sandbox is goja, a JavaScript interpreter embedded in the GraphJin process - no external runtime is involved.

This means the only model requirement is that it is **good at code generation** and follows the `key: value` output format. It does **not** need provider function-calling or structured-output/JSON modes, so any OpenAI-compatible chat-completions endpoint works (see [OpenAI-compatible endpoints](#openai-compatible-endpoints)).

Every answer is grounded in observed evidence. Go-side protocol guards enforce the catalog-first contract — for example a saved query may only run after its `saved_query` catalog row has been inspected — and downgrade an `answered` result to `blocked` (with evidence) when a required step was skipped. Models cannot talk their way past the guards; only real tool results count.

### Semantic-aware catalog exploration

When [semantic catalog search](/configure/discovery-semantic-search/) initializes
successfully, GraphJin adds private search guidance to the RLM prompt. The
automatic first step stays exactly the same: the full user instruction is sent
to `query_catalog` with explanations before the model runs. The added guidance
tells the agent to formulate short business-intent phrases, treat similarity
matches as candidates, inspect card IDs, and use only catalog-returned
relationship paths as join evidence.

If that seed is missing an endpoint, verified path, or required column detail,
or is empty or materially ambiguous, the agent may use one private coverage
batch containing two or three diversified phrases. GraphJin embeds all cache
misses in one Ax request and ranks the phrases independently; it does not gain
breadth from concurrent tool calls. Exact identifiers stay pinned, one result
per phrase is reserved for coverage, and real caller-visible relationship paths
are added after rank fusion. Retrieval metadata tells the model when the index
is warming or the whole batch fell back to lexical search.

This private `searches` argument is not part of the public MCP
`query_catalog` schema. When semantic search is disabled or fails to initialize,
the agent keeps its existing prompt and tool contract.

{{< verified by="TestSemanticCatalogGuidanceAndToolSchemaAreConditional" file="agent/semantic_catalog_test.go" line="11" >}}
{{< verified by="TestSemanticCoverageProtocolValidationAndOneBatchLimit" file="agent/semantic_catalog_test.go" line="82" >}}
{{< verified by="TestSemanticAgentInspectsCoveragePathBeforeExecution" file="agent/semantic_catalog_test.go" line="167" >}}

## Access Controls

There are no per-request agent modes. The server-side agent always receives the same internal runtime tools, including `execute_graphql`, and Go protocol guards enforce the catalog-first contract before execution. Core roles and row-level security still decide what the caller can actually read or write.

Use `agent.read_only: true` as the operator kill-switch when the agent should never write. In read-only mode, the agent rejects mutations before execution, including saved-query mutations. With `agent.read_only: false`, mutations may still be blocked by the caller's role, source/table `read_only` policy, row-level security, or missing protocol evidence.

The public MCP `execute_graphql` tool is separate from the server-side agent's internal runtime global. It appears in `tools/list` only when `mcp.allow_raw_queries: true`; `ask_graphjin_agent` can remain the single MCP front door while it orchestrates its own guarded internal calls.

## Role-aware guidance

The agent is caller-aware. From the request's identity it derives a capability profile limited to fixed `gj_*` system roots—never application tables—and preloads every permitted Ax skill. The 15 flat guides cover data discovery/write, code read/write, workflow discovery/execution/authoring, watch inbox/lifecycle/flow enrichment/action delivery, declared-task read/write, and admin inspection/configuration.

There is no lexical router, embedding search, skill catalog, search callback, or skill-discovery turn. Global read-only posture removes every write guide. Workflow, watch, and admin guides are included only when their governed roots are visible; admin guides also require the admin role. Multi-domain requests can use more than one preloaded guide.

Skills only shape the guidance the model sees. **They never grant access.** GraphJin core roles and row-level security remain authoritative, and protocol preflight enforces mutation shape, saved-query detail, security/runtime evidence, and workflow detail before execution. Ax's `used(...)` primitive supplies self-reported usage telemetry; it cannot change authorization.

For standing requests, the watch skills apply the two-axis decision in [Choosing Watches, Flows, and Workflows](/agentic/watch-automation/): deterministic versus semantic/noisy, and notification versus an explicitly requested action. The agent may propose an action watch, but a protocol guard prevents it from creating/changing and approving that autonomous action in the same run.

{{< verified by="TestAllowedSkillsCapabilityMatrix" file="agent/skills_test.go" line="73" >}}
{{< verified by="TestRunPassesOnlyCapabilityFilteredConstructorSkills" file="agent/skills_test.go" line="194" >}}

## Request and response

Request fields: `instruction` (required), and optional `context`, `namespace`, `task_id`, `max_steps`, `return_trace`. A retained open or verifying [declared task](/agentic/tasks/) warm-starts the request and journals its result; it never grants access or satisfies evidence guards.

Response fields: `status` (`answered` | `needs_clarification` | `blocked` | `error`), `answer`, and optional `skills`, `skill`, `data`, `evidence`, `actions`, `next`, `refusal`, `notices`, `errors`, `usage`, `trace`, `trace_id`. `skills` contains the guides Ax reported through `used(...)`; deprecated `skill` is the first used ID through v3. `usage` is derived from Ax `GetUsage()` plus its merged stage chat logs and includes provider-neutral prompt, completion, total-token, and model-call counts when the provider reports them. GraphJin also writes those aggregate counts to the structured `GraphJin agent usage` log and agent trace attributes; prompts and answers are never added to that usage log.

### REST

```bash
curl -sS http://localhost:8080/api/v1/agent \
  -H 'content-type: application/json' \
  --data '{"instruction":"What production work should we prioritize next?"}'
```

### MCP

```json
{
  "name": "ask_graphjin_agent",
  "arguments": {
    "instruction": "What production work should we prioritize next?"
  }
}
```

{{< verified by="TestAskGraphJinAgentMCPSchema" file="serv/agent_handler_test.go" line="347" >}}

### Streaming and status

The REST endpoint streams progress when called with `Accept: text/event-stream`: `action` frames as the agent works, `result` frames as evidence lands, then a final `complete` frame carrying the full response. MCP callers that pass `_meta.progressToken` receive `notifications/progress` events per action. `GET /api/v1/agent/status` reports server-model readiness; the built-in web console uses that result to decide whether to offer the chat page.

## Structured refusals

A `blocked` response is machine-actionable, not prose. It carries a `refusal` object: a stable `code` (`access_unauthorized`, `capability_disabled`, `mutation_evidence_required`, ...), the `blocked_action`, evidence-backed `because` reasons, ordered `unblock` steps (each names a tool and args, filtered to the caller's visible capabilities so nothing hidden leaks), a `lawful_alternative` for when unblocking is impossible, and the `policy_final` / `retryable` pair. A calling agent should execute the unblock steps and retry only when `retryable` is true; `policy_final` means stop and escalate to an operator. The contract is discoverable at runtime with `query_catalog(id: "help:refusals")`.

## Task and watch notices

Declared tasks keep themselves visible through owner-scoped response notices.
`task_open_unlinked` lists active tasks when a run is not task-bound,
`task_context_loaded` confirms warm-started context, and `task_verify_failed`
reports a declared saved-query check that left its task open. Notice IDs are
correlation hints only; callers must still use the governed task roots, and a
task ID never grants access. See [Declared Tasks](/agentic/tasks/) for the full
lifecycle. `annotations_unshared` similarly lists bounded owner-only catalog
annotation drafts. The agent can draft one, but it cannot approve a new or
edited note in the same run; see [Catalog Annotations](/agentic/annotations/).
The built-in console renders these notices defensively, including unknown future
kinds; see [Mission Control](/agentic/mission-control/).

When the caller has unreviewed [watch events](/agentic/watches/), agent responses include a `notices` entry with kind `watch_events_unseen`, a count, and `watch_ids` scoped to the caller's owner/account identity. MCP clients can subscribe to `graphjin://watch-events/unseen/{watch_id}` for watch-specific push signals, while the aggregate `graphjin://watch-events/unseen` resource remains readable as the owner/account-wide recovery path. Resource notifications identify a changed resource but do not contain the full event payload.

## Server-owned model selection

GraphJin always uses the provider, model, and credential environment variable
configured under `agent`. If the configured credential is missing,
`ask_graphjin_agent` and watch enrichment fail closed with
`model_credentials_required`; REST reports the same missing-credentials state.
The removed `agent.sampling` setting is rejected with migration guidance.

## OpenAI-compatible endpoints

Point the agent at any OpenAI-compatible chat-completions endpoint (vLLM, Ollama's OpenAI endpoint, OpenRouter, Together, LM Studio, Groq, …) with `base_url`:

```yaml
agent:
  provider: openai            # or: openai-compatible
  model: <model-name>
  api_key_env: OPENAI_API_KEY # must hold a non-empty value (a dummy is fine for keyless local endpoints)
  base_url: https://your-endpoint/v1
```

Because the loop is driven by generated code rather than provider tool-calling, the practical requirement is a model that codegens well — not one with native function-calling support.

## When to use it

- **Use the server-side agent** to hand GraphJin a goal and get a typed answer in one call, with discovery, validation, and guardrails handled server-side and scoped to the caller.
- **Use [MCP tools](/agentic/mcp/) directly** when your own agent should own the loop and make each discovery/execution decision itself.

The agent can also manage configuration: it discovers config recipes and applies changes through the same preview → apply machinery as the engine settings. See [How Configuration Works](/configure/how-it-works/) for the full picture, including which server settings apply live versus needing a restart.
