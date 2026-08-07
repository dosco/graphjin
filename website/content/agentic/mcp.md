---
title: "MCP"
description: "Connect AI clients to GraphJin through catalog-first Model Context Protocol tools."
nav_group: "agentic"
doc_kind: "guide"
weight: 10
---

## Try it with no setup at all

Nothing to start, no Docker, no config file, no model key:

```bash
claude mcp add graphjin -- graphjin mcp --demo
codex mcp add graphjin -- graphjin mcp --demo
```

`graphjin mcp --demo` extracts the built-in SaaS ops demo to `./graphjin-demo` — SQLite, in-process, no containers — and serves MCP over stdio. Your IDE's own model does the reasoning, so no provider key is needed. Delete `./graphjin-demo` to reset. Point at another [demo vertical](/start/demos/) from a repo clone with `--path examples/<name>`.

The catalog-first, governed read-only path is measured across models in [The Organizational Agent Benchmark](/benchmarks/organizational-agent/).

The demo deliberately keeps its own configuration out of reach. It sets `mcp.allow_config_updates: false` and puts the `gj_config` root behind `admin`, so a stdio caller gets `validate_config` — enough to dry-run a change — but neither `get_current_config` nor `update_current_config`. To configure GraphJin from your IDE, scaffold your own project with `graphjin serve new my-api`; the generated `dev.yml` enables all three.

## Connect to a running server

```bash
graphjin mcp add codex
graphjin mcp add claude
```

Defaults are client `codex`, server `http://localhost:8080`, and project scope; the URL is normalized to `/api/v1/mcp`. Use `--global` to make the connection available outside the current project.

This path is HTTP-only and **requires GraphJin to already be running** — the command sends a real MCP `initialize` probe and stops if nothing answers. Start the server first:

```bash
graphjin serve --demo     # terminal 1
graphjin mcp add codex    # terminal 2
```

For hosted GraphJin:

```bash
codex mcp add graphjin --url https://graphjin.example.com/api/v1/mcp
claude mcp add --transport http graphjin https://graphjin.example.com/api/v1/mcp
```

{{< svg "mcp-flow" "MCP discovery to governed action flow" >}}

## Tool philosophy

GraphJin's MCP surface starts with discovery:

- Search catalog rows before writing a query.
- Ask for query syntax and examples before choosing operators.
- Inspect `gj_security` before writes, workflows, config changes, file access, or code changes.
- Execute through saved queries or validated GraphQL.

`query_catalog` and `execute_saved_query` use the same artifact overlay as HTTP GraphQL. With a configured artifact store and `user_id`, the caller sees global config files plus their own `gj_artifacts` rows.

Exact `query_catalog(id: ...)` and `query_catalog(ids: ...)` lookups also merge
visible approved [catalog annotations](/agentic/annotations/) as explicitly
untrusted organizational context. Raw `gj_catalog` and broad catalog searches
do not contain annotation text.

Declared-task lifecycle and journal writes stay on GraphQL roots `gj_task` and
`gj_task_entry`; GraphJin adds no task-specific MCP tools or resources. The one
MCP addition is optional `task_id` on `ask_graphjin_agent`, which loads the
same-owner open or verifying task as untrusted warm-start context and journals
the run. Task closes can declare a saved-query check in `verify_json`; GraphJin
runs it as the stored owner and returns `verified`, `failed`, or `pending`
state through the same GraphQL row. Agent responses also surface active and
failed task notices. See [Declared Tasks](/agentic/tasks/).

{{< verified by="TestMCPCLIParity" file="cmd/mcp_parity_test.go" line="18" >}}
{{< verified by="TestProcessCursorsForMCP" file="serv/mcp_cursor_test.go" line="20" >}}
{{< verified by="TestUserArtifactSavedQueryOverridesGlobalOnlyForOwner" file="serv/artifact_overlay_test.go" line="94" >}}

## Cursor IDs

MCP responses replace opaque GraphJin cursor strings with short cursor IDs when cursor caching is available. Clients pass those IDs back to `execute_graphql` or `execute_saved_query`; GraphJin expands them to the original encrypted cursor before execution.

Do not hardcode `gj-`, `__gj-enc:`, or any cursor prefix in an MCP client. GraphJin uses a dynamic security prefix, and prefix guessing can make encrypted cursor recognition fail.

```json
{
  "name": "ProductsPage",
  "variables": {
    "cursor": "cursor_01H..."
  }
}
```

{{< verified by="TestMCP_CursorRoundtripIntegration" file="serv/mcp_test.go" line="282" >}}
{{< verified by="TestMCP_AlreadyEncryptedCursorUnchanged" file="serv/mcp_test.go" line="669" >}}

## Production identity

HTTP MCP endpoints can be protected by OAuth or the same JWT/OIDC context as the main API. Stdio mode is useful for local development.

## Capability-aware tools

The MCP tool list is catalog-first in both sources and non-sources configs. In
`dev` and `agentic`, `graphql_help`, `query_catalog`, `execute_saved_query`,
`validate_where_clause`, and the [server-side agent](/agentic/server-agent/)
are available without feature toggles, with the remaining policy-allowed
primitive tools alongside them. Set `mcp.include_tools_with_agent: false` only
when the agent should be the single front door. Raw GraphQL remains separately
gated by `mcp.allow_raw_queries`.

The same Streamable HTTP endpoint serves modern stateless MCP `2026-07-28`
requests and legacy stateful clients automatically. The built-in agent always
uses GraphJin-owned provider credentials and fails closed when they are absent.

For local development, named query auto-save and workflow saves fall back to config files only when there is no `user_id` or no artifact store.

{{< verified by="TestRegisterTools_SourcesUsedRawGraphQLCapabilityControlsTool" file="serv/mcp_registration_test.go" line="316" >}}
{{< verified by="TestMCPCallerCapabilityProfileReflectsSourceRootAccess" file="serv/mcp_registration_test.go" line="551" >}}

## Configure GraphJin from your AI IDE

While you are building, you can stop editing config files by hand. Ask your IDE for a database connection or a role that only sees its own rows; GraphJin checks the change against your real databases, then applies it and writes it back to `dev.yml`.

Three **dev-mode** tools do this. They are registered even when the built-in agent is the MCP front door, so an AI IDE keeps first-class config access:

| Tool | What it does |
| --- | --- |
| `get_current_config` | Reads the running config with secrets redacted. Optional `section`: `sources`, `system`, `workflows`, `databases`, `relationships`, `tables`, `roles`, `blocklist`, `functions`, `resolvers`, `mcp`, or `all`. |
| `validate_config` | A real dry run, not a lint. Runs the entire update pipeline — databases actually connected, schema actually discovered, reload impact classified — then discards the staged runtime. Returns `valid`, errors, a change summary, `scope`, `reload_mode`, and `reload_strategy`. Nothing is written, not even a preview. |
| `update_current_config` | Applies the change and reloads. Additionally requires `mcp.allow_config_updates`. |

### What an agent can change

Databases, roles and RBAC in full (per-role, per-table `query`/`insert`/`update`/`upsert`/`delete` with `limit`, `filters`, `columns`, `presets`, and `block`), source access policy, tables, blocklist, functions, resolvers, relationships, workflows, and an allowlisted slice of `serv` — the agent's `model`, `max_steps`, `timeout_seconds`, `read_only`, `return_trace`, plus `log_level`, `log_format`, `web_ui`, `http_compress`, `server_timing`, and `rate_limiter`.

Database changes are all-or-nothing: every new or changed connection is tested live, and if any one fails, no database change is applied.

### What it can never change

| Off-limits | Why |
| --- | --- |
| `auth`, `redis`, `uploads` | Secret-bearing server settings. Read-only on `gj_config` by design — edit the file and restart. |
| `agent.enabled`, `provider`, `api_key_env`, `base_url` | Gate startup wiring or name secrets. Only agent tuning fields are writable. |
| `read_only: true` databases | Snapshotted at startup. A runtime patch flipping one to `false` is forced back to `true` and logged. |
| System database names | `postgres`, `mysql`, `information_schema`, `master` and friends are rejected unless explicitly allowed. |
| Plaintext secrets | Rejected unless a local keystore key is configured. |

Beyond dev, the doors close. In **agentic** mode these tools are not registered at all; config writes move to the `gj_config` GraphQL root, which is admin-only and still needs `mcp.allow_config_updates` — the shipped `agentic.yml` sets it to `false`. In **production** the surface is off and fails closed.

Note that the source-mode `preview` → `apply` handshake is a consistency guard, not a human approval step: `apply` must carry the `preview_id` and the exact same payload, matched by catalog revision and payload hash.

### Recipes an agent can follow

The catalog ships 15 `config_recipe` rows, each with preflight checks, the apply mutation, verification, and stop conditions. An agent finds them through `query_catalog` — adding a role, setting source access defaults, classifying tables, enabling artifacts, tasks or watches, rate limiting, agent tuning, JWT auth, Redis caching, uploads, and production hardening.

See [How Configuration Works](/configure/how-it-works/) for every config interface, including the `graphjin config` CLI and `GJ_*` environment variables.

## Tool inventory

GraphJin deliberately exposes a small MCP surface. A scaffolded dev project has nine tools:

`graphql_help`, `query_catalog`, `validate_where_clause`, `execute_saved_query`, `execute_graphql`, `get_current_config`, `validate_config`, `update_current_config`, and `ask_graphjin_agent`.

The shipped demos have seven: they set `mcp.allow_config_updates: false`, which drops `update_current_config`, and they put the `gj_config` root behind `admin`, which hides `get_current_config` from a non-admin caller. Agentic and production deployments drop the config tools entirely, and `execute_graphql` stays gated behind `mcp.allow_raw_queries`.

The tool list is filtered per caller, so what you see depends on your role as well as the mode. Tools that need a GraphJin system root — `query_catalog` and `ask_graphjin_agent` need `gj_catalog`, the config tools need `gj_config` — disappear for callers who cannot reach that root.

## Watch event resource

When watches are enabled, MCP exposes an aggregate caller-scoped resource and a per-watch template:

```text
graphjin://watch-events/unseen
graphjin://watch-events/unseen/{watch_id}
```

Clients should retain the ID returned by `gj_watch`, RFC 6570-expand the template so reserved characters in the ID are percent-encoded, and subscribe to that concrete per-watch URI. Exact subscriptions receive `notifications/resources/updated` only for that watch; the URI identifies which watch changed, and a read returns compact metadata only. The aggregate resource retains owner/account-wide compatibility for hosts without per-URI subscription support; those hosts must filter to the conversation's watch IDs before reading full events or marking them seen. Full event payloads remain behind `gj_watch_event` or the REST/GraphQL watch APIs.

Unsubscribing from this MCP resource only removes the in-memory resource subscription. It never pauses, expires, deletes, or cleans up watch definitions.

Creation, flow preview/approval, autonomous-action approval, pause/resume, and updates all use `gj_watch`. See [Choosing Watches, Flows, and Workflows](/agentic/watch-automation/) for the decision matrix and review examples.

{{< verified by="TestWatchMCPPerWatchRoutingSameOwnerSessions" file="serv/watches_test.go" line="1571" >}}
