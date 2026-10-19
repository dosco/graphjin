---
title: "How Configuration Works"
description: "The mental model behind GraphJin config — one file, two halves, layered overrides — and every way to change it: editor, CLI, the agent, MCP, and GraphQL."
nav_group: "configure"
doc_kind: "concept"
weight: 10
---

GraphJin is configured by a single YAML (or JSON) file. It has grown large because
it configures two things at once — the query **engine** and the **server** around
it — so this page is the map: what the file is, how values are resolved, and every
interface you can use to change it without memorizing the field list.

## The 60-second start

```bash
graphjin serve new blog     # scaffolds ./blog
```

That writes three config files and a schema:

| File | Used when | Notes |
| --- | --- | --- |
| `dev.yml` | local development | batteries-included runtime defaults |
| `prod.yml` | production | `inherits: dev`, overrides only what differs |
| `agentic.yml` | agent deployments | batteries-included, with locked-down access policy |
| `config.schema.json` | your editor | powers autocomplete (see below) |

Which file loads is decided by **`GO_ENV`** (`dev`, `prod`, or `agentic`; `dev`
when unset), so the same binary runs any environment. A file may pull values from
one sibling with `inherits:` — **one level only**; the inherited file cannot
itself inherit.

## The mental model: one file, two halves

The config file is a single flat namespace, but the keys belong to two worlds:

- **Engine (core)** — how GraphJin compiles and secures GraphQL: `sources`,
  `tables`, `roles`, `relationships`, `resolvers`, `blocklist`.
- **Server (serv)** — the service around the engine: `host_port`, `auth`,
  `rate_limiter`, `caching`, `redis`, `uploads`, CORS, logging, `web_ui`, and the
  built-in `agent`.

This split matters when you change a value at runtime: **engine changes reload in
place**, while most **server changes need a restart** to take effect. The
change-oriented interfaces below classify the affected `scope` (core, serv, or
mixed) and activation impact (`hot` or `restart`); core changes additionally
report whether their reload strategy is full or source-scoped.

### How a value is resolved

When two layers set the same key, the later one wins:

```
built-in literal defaults
  → inherited parent file (inherits:)
    → your config file
      → GJ_* / SJ_* environment variables
        → parsed mode defaults (dev and agentic)
```

`GJ_`-prefixed environment variables override any key (`GJ_LOG_LEVEL=warn`,
`GJ_DATABASE_PASSWORD=…`). Parsed `dev` and `agentic` configs automatically
enable managed artifacts, watches, the built-in agent, stateful MCP HTTP, and
the primitive MCP tools while preserving explicit file/environment opt-outs.
Development-only raw query, config-update, and schema-reload conveniences remain
limited to dev; prod defaults stay off. When you are unsure why a key has the
value it does, ask:

```bash
graphjin config explain log_level
```

```
key:      log_level
value:    warn
scope:    serv
reload:   restart
source:   environment variable GJ_LOG_LEVEL
docs:
  Log Level
  Logging level must be one of debug, error, warn, info
```

## Pick your interface

You never have to hand-edit YAML from memory. Choose the interface that fits.

### Editor autocomplete (the schema modeline)

Every scaffolded file starts with:

```yaml
# yaml-language-server: $schema=./config.schema.json
```

Any editor with the YAML language server (VS Code, JetBrains, neovim) then gives
you completion, hover docs, and inline validation for every key. Regenerate the
schema anytime:

```bash
graphjin config schema > config/config.schema.json
```

### The `graphjin config` CLI (offline)

git-config-style commands that read and edit the files on disk — no running
server, comments preserved:

```bash
graphjin config get rate_limiter.rate
graphjin config set log_level debug        # edits the file, keeps comments
graphjin config unset rate_limiter.bucket
graphjin config explain auth               # value + provenance + scope + docs
graphjin config validate                   # structural check, no DB needed
graphjin config docs prod                  # annotated example template
```

`set` parses the value as YAML (so `42` is a number, `[a, b]` a list) and reloads
the result through the real loader before writing — a change that would make the
file unloadable is refused.

### Ask the built-in agent

With the agent enabled, describe the change in natural language:

> "enable rate limiting at 100 requests per second"

The agent finds the matching **config recipe** in its catalog, which walks it
through preflight → apply → verify. Writable server settings (rate limiting, agent
tuning) are applied through the same machinery as engine settings; secret-bearing
ones (auth, redis, uploads) route you to the exact `graphjin config set` command
instead of guessing. See [Server-Side Agent](/agentic/server-agent/).

### Config tools from an AI IDE (dev, over MCP)

In **dev mode**, GraphJin exposes config tools over [MCP](/agentic/mcp/) — even
when the agent is the front door — so a connected AI IDE keeps first-class access:

- `get_current_config` — read the running config (redacted).
- `validate_config` — dry-run any change: full validation and reload-impact
  classification, with no side effects.
- `update_current_config` — apply a change (when `mcp.allow_config_updates` is on).

These are dev-only; agentic and production deployments never expose them.

### The `gj_config` control plane (GraphQL)

Programmatic callers read and write config through the `gj_config` system root.
It reflects the full config — both engine and server halves, secrets redacted:

```graphql
query { gj_config(id: "current") { sources roles serv catalog_revision } }
```

Writes use a preview → apply handshake guarded by `catalog_revision`. Preview
responses classify the affected half of the config separately from activation
impact and the core reload strategy:

```graphql
mutation {
  gj_config(id: "current", update: {
    mode: "preview"
    expected_catalog_revision: "<catalog_revision>"
    serv: { rate_limiter: { rate: 100 } }
  }) {
    valid
    scope             # core, serv, or mixed
    reload_mode       # hot or restart
    reload_strategy   # full or source_scoped for core changes; otherwise null
    preview_id
    expires_at
    change_summary_json
    findings_json
    errors_json
  }
}
```

These impact fields describe a proposed or applied mutation; an ordinary
`gj_config(id: "current")` read leaves them null. See [Source Mode](/agentic/source-mode/).

### Environment variables only (containers)

For twelve-factor deployments, skip file edits entirely and set `GJ_*` variables.
They sit near the top of the precedence ladder and are ideal for secrets:
`GJ_DATABASE_PASSWORD`, `GJ_AUTH_JWT_SECRET`, `GJ_REDIS_URL`.

## The safety model

GraphJin's `mode` decides how open the surface is:

| Mode | Config writes | Agentic surface (MCP, agent, `gj_*`) |
| --- | --- | --- |
| `dev` | on by default; config MCP tools exposed | on |
| `agentic` | only if explicitly enabled | on, but locked down (system roots admin-only) |
| `prod` | off | off — fails closed |

Additional guardrails apply everywhere: config values are **redacted** in every
read surface, runtime writes accept **secret references, not literals**, source-mode
writes require a **preview with a revision guard**, and each change reports whether
it is **hot** (live) or needs a **restart**.

## Common tasks

| I want to… | Ask the agent | Or run |
| --- | --- | --- |
| Enable rate limiting | "enable rate limiting" | `graphjin config set rate_limiter.rate 100` |
| Tune the agent | "use gpt-4o with 12 steps" | `graphjin config set agent.model gpt-4o` |
| Enable JWT auth | "set up JWT auth" | edit `auth.jwt.*`, keep the secret in `GJ_AUTH_JWT_SECRET` |
| Add Redis | "use redis for caching" | `graphjin config set redis.url '${REDIS_URL}'` |
| Harden for prod | "production checklist" | `graphjin config --file prod.yml validate` |

## Where to go next

- Field-by-field reference: `CONFIG.md` in the repo, or `graphjin config docs`.
- Deep dives: [Sources Mode](/configure/sources-mode/),
  [Auth And RBAC](/configure/auth-rbac/),
  [Environment And Production](/configure/environment-production/).
- Agent and MCP: [Server-Side Agent](/agentic/server-agent/), [MCP](/agentic/mcp/).
