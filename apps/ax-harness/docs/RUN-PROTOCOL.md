# Run protocol

Ax Harness runs one agent turn per process. The host writes one JSON run
specification to stdin. The harness writes one JSON event per line to stdout.
The host owns history, compaction across turns, approvals, the sandbox and
scheduling.

Build the binary with `go build -o bin/ax-harness ./cmd/ax-harness`.

## Input

Send exactly one object, at most 16 MiB. Unknown fields fail the run.

| Field | Type | Rule |
| --- | --- | --- |
| `version` | integer | Must be `1`. |
| `run_id` | string | Required. 128 characters or fewer. |
| `input_id` | string | Required. 128 characters or fewer. |
| `instructions` | string | Optional. The org prompt: the same text for every turn of the org. The model sees it first, as `Instructions:`. |
| `prompt` | string | Required. The rest of the turn, including history and the user message. The model sees it next, as `Question:`. Without `instructions`, it carries the whole turn. Only the model context window bounds the two together. |
| `skill_query` | string | Optional. The user message. The host skill index recalls up to 10 guides by meaning. When it recalls two or more, one model call on the context route keeps the two or fewer that fit the request. A `discover()` search that names a staged guide loads only that guide; a free-text search gets the same rerank. |
| `skill_rerank` | string | Optional. `off` keeps the index order and makes no rerank call. The harness also skips the call when `max_model_calls` is 1 to 3. |
| `stream_responses` | boolean | Optional. Sends live `answer.delta`, `thought.delta` and `actor.step` events. |
| `timeout_ms` | integer | Optional. Run wall clock, 1,000 to 1,800,000. Default 540,000 (9 minutes). |
| `max_actor_steps` | integer | Optional. Actor steps, 1 to 500. Default 25. |
| `max_child_steps` | integer | Optional. Child agent steps, 1 to 500. Default 50. |
| `max_operations` | integer | Optional. Tool calls per run, 0 to 4,000. 0 means no cap. |
| `max_model_calls` | integer | Optional. Model calls per run, 0 to 2,000. 0 means no cap. |
| `max_model_tokens` | integer | Optional. Token ceiling, up to 10,000,000. 0 means no ceiling. |
| `max_cost_micros` | integer | Optional. Cost ceiling in USD micros at Ax catalog rates. Every route needs a catalog price. |
| `reasoning_effort` | string | Optional. `low`, `medium` or `high`. |
| `context_window_tokens` | integer | Optional. The model context window. A prompt larger than this estimate fails before any model call. |
| `max_output_tokens` | integer | Optional. Maximum output tokens per model call. Must be below `context_window_tokens`. |

## Limits

The limits match Hermes, so the two backends behave the same. OpenNeko sets
them for both backends from one source, `packages/llm/src/agent-limits.ts`.
The defaults below apply only when the host does not set a value.

| Limit | Ax Harness | Hermes equivalent |
| --- | --- | --- |
| Prompt size | Model context window only; 16 MiB stdin sanity limit | No fixed cap; `model.context_length` |
| Context window, output tokens | `context_window_tokens`, `max_output_tokens` | `model.context_length`, `model.max_tokens` |
| Actor steps | Default 25, 1 to 500 | `agent.max_turns` 25; OpenNeko per-run override |
| Child agent steps | Default 50, 1 to 500 | `delegation.max_iterations` 50 |
| Tool calls, model calls | No cap unless set | No separate caps |
| Run wall clock | Default 9 minutes, maximum 30 minutes | OpenNeko turn timeout, 9 minutes |
| Actor code time | The run wall clock, including tool waits | No separate limit |
| Inline tool result | 100,000 characters | `DEFAULT_RESULT_SIZE_CHARS` |
| Saved result preview | 1,500 characters | `DEFAULT_PREVIEW_SIZE_CHARS` |
| Inline results per actor step | No limit: results go to actor code, and only printed output reaches the model | `DEFAULT_TURN_BUDGET_CHARS` 200,000 |
| Stored tool result | 16 MiB; MCP results 8 MiB, below the MCP SDK frame limit | Spilled to a file |
| Terminal timeout | Default 180 s, maximum 600 s | `TERMINAL_TIMEOUT` default 180, `FOREGROUND_MAX_TIMEOUT` 600 |
| Terminal output | 50,000 characters, head and tail | `DEFAULT_MAX_BYTES` 50,000 |
| File read | 100,000 characters, 2,000 lines, 2,000 characters per line | `_DEFAULT_MAX_READ_CHARS`, `DEFAULT_MAX_LINES`, `DEFAULT_MAX_LINE_LENGTH` |
| File write and edit | 1 MiB of content; readable files up to 10 MiB | No direct equivalent |
| Skills catalog | 256 skills; descriptions up to 1,024 characters; bodies up to 64 KiB | No cap |

A result above the inline limit, or above the step budget, returns a run-local
reference with a preview. Actor code reads the full result with
`harnessSavedOperation(id)`.

## Output events

Every event has `version`, `run_id`, `input_id`, `sequence` and `type`.
`sequence` counts up from 1. Model events carry no prompt text. No event carries
credentials. `tool.finished` carries the tool result in `data`.

| Type | Meaning |
| --- | --- |
| `run.started` | The run started. |
| `child.admitted` | The run has a read-only child agent. |
| `model.request.started` | A model call passed admission. `name` is the configured model, `origin` the route, `provider` the provider type, `call_id` the count. |
| `model.request.finished` | A model call ended. `usage` holds the tokens and the call's cost, `observed_model` the model the provider reported, and `error` is set on failure. |
| `model.route.fallback` | A transient provider error moved the call from `name` to `origin`. |
| `executor.step.failed` | Actor code failed. Later executor calls can use the escalation route. |
| `skill.used` | The agent declared that the staged skill `name` influenced the turn. |
| `tool.input.rejected` | Tool input failed its schema. The tool did not run. |
| `tool.started`, `tool.finished` | One tool call, with `operation_id`, `effect` and, when finished, `data` or `error`. |
| `observation.retrieved` | Actor code read a saved tool result by `operation_id`. |
| `span.started`, `span.finished` | Ax span lifecycle. |
| `child.started`, `child.finished` | Child agent lifecycle. |
| `run.finished` | The last event. `result` holds the outcome. |

Live events have `sequence` 0 and appear only with `stream_responses`. Treat
them as provisional; only `run.finished` holds the answer.

| Type | `data` |
| --- | --- |
| `answer.delta` | `{version, index, text}` from the responder. A new `version` replaces the earlier text. |
| `thought.delta` | `{version, index, text}`. Provider thought summaries, from Anthropic and Gemini routes with `reasoning_effort` set. |
| `actor.step` | `{text}`. The short request the actor writes when it hands work to the executor. |

## Result

| Field | Values |
| --- | --- |
| `status` | `completed`, `failed`, `cancelled` |
| `kind` | `answer`, `summary`, `clarification`, `failure` |
| `answer` | The final text, unchanged, with any fences. |
| `code` | Set when the run did not complete, or names the budget for a `summary`. |
| `usage` | Requests, reported calls, tokens, `coverage` (`complete`, `partial` or `unavailable`) and the fields in [Cost](#cost). |
| `cost` | Present when `max_cost_micros` is set: `catalog_version`, `charged_micros` (reservations for calls without usage included) and `budget_micros`. |

A tool error is a normal result. The model sees it and can adapt; the run does
not fail because of it.

When the run stops at `max_operations`, `max_actor_steps` or `max_model_calls`,
the harness makes one more model call with no tools. That call answers from the
tool results. The result is `completed` with kind `summary` and code
`operations_exhausted`, `actor_steps_exhausted` or `model_calls_exhausted`.
When `max_model_calls` is set, the agent stops one call early to keep that call
inside the cap. A token, cost, deadline or cancel stop never makes this call.

Failure codes: `model_failed`, `model_http_<status>`, `model_context_overflow`,
`model_output_truncated`, `actor_steps_exhausted`, `operations_exhausted`,
`model_calls_exhausted`, `invalid_output`, `model_budget_exceeded`,
`model_token_budget_exceeded`, `cost_budget_exceeded`, `deadline_exceeded`,
`cancelled`.

A tool with effect `pause` ends the turn as `clarification` with the answer
"Awaiting operator input."

## Cost

The harness prices each model call from the model catalog of the pinned Ax Go
module, for the route that served the call. A fallback call is priced at the
fallback route's rates. Cache reads and cache writes use their catalog rates.
A call whose input, cache reads and cache writes exceed the model's
`longContextThreshold` uses the long-context rates. Tokens in the total beyond
input, output and cache are reasoning tokens, billed at the output rate. An
`openai-compatible` or `azure-openai` route uses OpenAI catalog prices, and a
`vertex-ai` route uses Gemini or Anthropic prices. With `max_cost_micros`,
each call reserves its token reservation at the model's highest rate, and the
catalog cost replaces the reservation when the provider reports usage.

| Usage field | Meaning |
| --- | --- |
| `cost_micros` | USD micros for the reported calls, each rounded up. |
| `priced_by` | `catalog` when every reported call had a catalog price, otherwise `unpriced`. |
| `catalog_version` | The pinned Ax module version. |

`go run ./cmd/ax-harness catalog` writes the same prices as JSON. OpenNeko
checks the output in as `packages/llm/src/spend/ax-model-catalog.json`, and a
test fails when that file differs from the pinned module.

## Exit codes and signals

| Code | Meaning |
| --- | --- |
| 0 | The run completed. |
| 1 | The run did not complete, or event delivery failed. |
| 2 | The input or the configuration is invalid. Nothing ran. |

SIGINT and SIGTERM cancel the run. The run then finishes with status
`cancelled`.

## Model configuration

The host sets the model. Run input cannot change it.

- One model: `HARNESS_MODEL`, `HARNESS_MODEL_API_KEY`, and optionally
  `HARNESS_MODEL_PROVIDER`, `HARNESS_MODEL_URL` and `HARNESS_MODEL_OPTIONS`.
  `HARNESS_MODEL_URL` is required for the default provider, `openai-compatible`.
- Several routes: `HARNESS_MODEL_ROUTES`, a JSON object:

```json
{"context":"cheap","executor":"work","responder":"work",
 "fallbacks":[{"from":"work","to":"spare"}],
 "routes":[
  {"key":"cheap","model":"model-a","url":"https://provider.example/v1","api_key_env":"HARNESS_CHEAP_KEY"},
  {"key":"work","provider":"anthropic","model":"model-b","api_key_env":"HARNESS_WORK_KEY"},
  {"key":"spare","provider":"google-gemini","model":"model-c","api_key_env":"HARNESS_SPARE_KEY"}]}
```

`provider` is `openai-compatible` (the default, which needs `url`) or any Ax
provider profile, for example `anthropic`, `google-gemini`, `openai`,
`azure-openai`, `mistral`, `groq`, `deepseek` or `openrouter`. A profile keeps
its provider's auth header, request shape and model rules. A native provider
uses its default URL when `url` is absent. An unknown provider is a
configuration error.

`HARNESS_MODEL_OPTIONS`, or `options` on a route, is a JSON object of string
provider settings. Azure needs `resource_name` and `deployment_name`, and
takes `api_version`; Ax builds the endpoint from them. Options cannot set the
key, the URL, the model or the transport.

`executor_escalation` and `executor_after_errors` (1 to 8) name a stronger
executor route after actor-code errors. Each route names the environment
variable that holds its key. Ax client retries are off; the harness owns fallback.

`reasoning_effort` sets `reasoning_effort` on OpenAI routes and Ax's thinking
budget on other providers.

Each stage's system prompt is identical for every turn, and `instructions`
starts the first user message, so every provider sees a stable prefix. On
`anthropic`, `openai` and `openai-responses` routes the harness turns on Ax's
context cache and sends `instructions` as a user message of its own with a
cache mark. Anthropic then puts a `cache_control` breakpoint on it, and OpenAI
GPT-5.6 and GPT-6 models put an explicit cache breakpoint on it, so a new turn
of the same org reuses the cached org prompt. Other routes, Gemini included,
use the provider's implicit prefix cache. The harness does not create Gemini
cache objects: one process runs one turn, so a cache object would need a
registry across processes, and Ax creates it outside the metered model calls.

## OpenNeko entry

`cmd/ax-harness` reads these environment variables:

| Variable | Use |
| --- | --- |
| `OPENNEKO_MCP_ORG_ID`, `OPENNEKO_MCP_THREAD_ID` | Required run scope. |
| `OPENNEKO_MCP_BRIDGE` | Path of the OpenNeko MCP bridge. Starts `node` with it. |
| `OPENNEKO_MCP_SERVERS` | Comma list of bridge servers to start. |
| `OPENNEKO_BROKER_URL`, `OPENNEKO_BROKER_TOKEN` | Passed to the bridge only, then removed from the harness process. |
| `OPENNEKO_HARNESS_WORKSPACE_DIR` | Adds `file_read`, `file_edit`, `file_write`, `file_search`. |
| `OPENNEKO_HARNESS_SHELL` | `1` adds `terminal`. Needs the workspace. |
| `OPENNEKO_HARNESS_TERMINAL_TIMEOUT_SECONDS` | Default terminal timeout, 1 to 600. Default 180. |
| `OPENNEKO_HARNESS_TERMINAL_MAX_OUTPUT` | Terminal output cap in characters, 1,000 to 1,000,000. Default 50,000. |
| `OPENNEKO_HARNESS_WEB_HOSTS` | Comma list of hosts for `web_fetch`. `*.example.com` matches subdomains; `*` matches any public host. Unset means no `web_fetch`. |
| `OPENNEKO_HARNESS_UPLOADS_DIR` | Adds read-only upload tools. |
| `OPENNEKO_HARNESS_SKILLS_READ`, `OPENNEKO_MCP_SKILLS_ROOT` | Adds the staged skills to the Ax skills catalog, and `skill_read` and `skill_search` for supporting files. |
| `OPENNEKO_HARNESS_CHILD_TOOLS` | `*` or a comma list of tools for the child agent. Pause tools stay with the parent. |

`terminal` runs `/bin/sh -c` in the workspace in its own process group. A
timeout or a cancel kills the whole group. Its environment has no broker
binding and no model credential: the entry removes `OPENNEKO_BROKER_URL`,
`OPENNEKO_BROKER_TOKEN`, `HARNESS_MODEL_API_KEY`, `HARNESS_MODEL_ROUTES` and
every route's key variable. A non-zero exit is a normal result with
`exit_code` and `output`.

`file_edit` replaces `old_string` with `new_string`. The file must be read in
this run and unchanged since the last read or edit. `old_string` must match
once unless `replace_all` is true.

`file_search` takes an RE2 `pattern`, a `glob`, or both. A pattern returns
`{path, line, text}` matches; a glob alone returns paths. The default limit is
50 and the maximum is 200.

`web_fetch` sends a GET with no cookies or credentials. Each redirect must stay
on the allowlist. Without a proxy, connections reach public addresses only.
HTML returns as plain text. The cap is 10 MiB of body and 2,000,000 characters,
as in Hermes `web_extract`.

Every tool is a JavaScript function in the Goja actor. One actor step can call
many tools, loop over results and filter them, so extra tool calls add no model
calls.
