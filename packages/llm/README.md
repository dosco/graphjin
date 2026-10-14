# OpenNeko agent MCP runtime

This package owns the runtime contract between Hermes, OpenNeko's sandbox
bridge, the trusted worker control plane, GraphJin, and the A2UI renderer.

## GraphJin MCP

OpenNeko does not maintain a GraphJin tool allow-list or duplicate GraphJin's
tool schemas. At the start of each agent turn, the sandbox bridge asks the
trusted control plane for the configured GraphJin server's live `tools/list`
response. Every returned tool and its native JSON schema is exposed through the
higher-level `neko_graphjin` MCP. Calls route back as native `tools/call`
requests and their MCP results are returned unchanged.

GraphJin's current Streamable HTTP contract is stateless, so OpenNeko sends
`tools/list` and `tools/call` directly without `initialize`. Every request
carries GraphJin 3.20's `2026-07-28` protocol header, method header, and
per-request client metadata so it cannot fall into the legacy session path. The
client follows list cursors and accepts JSON or SSE responses.

The sandbox receives neither `data_source.mcp_url` nor a GraphJin credential.
The broker binds the organization and run, mints the actor or service JWT on the
trusted host, and treats GraphJin's caller-aware tool list and capability gates
as authoritative. The physical `neko` multiplexer changes only tool-name
prefixes; descriptions, annotations, and input/output schemas survive intact.

### Write policy

Database sources stay read-only by contract. GraphJin enforces `read_only: true`
per database source in core and pins that flag at startup, so a later config
patch cannot lift it. Every database source OpenNeko registers through chat
carries that flag. The trusted host adds a second gate in `callGraphjinTool`:
it forwards a GraphQL mutation through `execute_graphql` only when the org's
config at `OPENNEKO_GRAPHJIN_CONFIG` proves every database source is
`read_only: true`. A missing, unreadable, or mis-edited config fails closed.

API sources can take governed writes. The shipped config sets
`mcp.allow_mutations: true` and `system.capabilities.raw_graphql.mutate: true`
(in sources mode the capability overwrites the MCP flag at startup) and lists
the `member` and `service` roles the worker mints. GraphJin generates a
source's `access.write` block only for listed roles; an unlisted JWT role gets
no block. A write against an API source still needs, per source, `read_only`
unset, `capabilities.api.write: true`, `access.write` set to `authenticated` or
`admin`, and `expose_mutation: true` plus `allowed_roles` on the operation.

### Governed API writes through the config workflow

Nobody edits the GraphJin file by hand. The `graphjin-secret-init` one-shot
and the worker's boot pass run `reconcileGraphjinWritePolicy`, which brings an
existing sources-mode config up to the shipped policy (`mcp.allow_mutations`,
`raw_graphql.mutate`, the `member` and `service` roles) before GraphJin starts.

Per-source write enablement goes through `request_source_config_change`
proposals that an admin approves, then GraphJin's two-phase `gj_config`
preview and apply, then the durable file. `enable_api_writes { source, spec,
operation, allowedRoles, write?, exposeAs? }` carries every setting in one
update: `read_only: false`, `capabilities.api.write`, and the operation's
`expose_mutation` plus `allowed_roles` in `update_sources`, and `access.write`
in `source_patches`.
Verified live on 3.20.47: one preview, one apply, then
`mutation { shop_create_order(call: { body: { note: "x" } }) { ok status_code } }`
reached the upstream API for `admin` and was refused at `allowed_roles` for
`member`. `set_source_capabilities` and `expose_api_operation` adjust one
setting on a source that already has writes enabled.

`assertDatabaseSourcesStayReadOnly` runs at preview and again at approved
execution and rejects any write setting on a database source, or on a source
whose kind GraphJin does not report. Admin > Settings > GraphJin Config has an
"Enable API writes" launcher.

Known GraphJin 3.20.47 limit: while an `api` source is configured, the first
`gj_config` update after engine start completes, and the next one blocks
inside the config pipeline until GraphJin restarts (repro in the PR). One
combined proposal per API source stays inside that limit; a further change
needs a GraphJin restart first.

### Lazy catalogs

The sandbox bridge connects every logical server in memory at startup and
resolves tool catalogs on Hermes' first `tools/list`. A logical server whose
catalog fails (for example `neko_graphjin` for an org with no data source) logs
the failure and drops out of that listing; the other servers keep working, and
the next `tools/list` retries it.

### Live rig for the integration tests

`test/integration/graphjin-mcp-live.test.ts` and `agentic-knowledge-live.test.ts`
skip unless a sources-mode GraphJin answers at `OPENNEKO_TEST_GJ_SOURCES_URL`
(default `http://127.0.0.1:8090`). This rig loads the shipped customer config
with one `read_only` Postgres source, so the mutation case exercises the real
database block:

```sh
export XDG_CONFIG_HOME=/tmp/neko-live-xdg   # fresh install key for the tests
SECRET=$(apps/worker/node_modules/.bin/tsx -e 'import("./packages/llm/src/graphjin/token.ts").then(m => process.stdout.write(m.graphjinSigningSecretB64("org-gj4-live")))')
docker network create neko-live
docker run -d --name neko-live-pg --network neko-live -e POSTGRES_PASSWORD=pg -e POSTGRES_DB=app postgres:16-alpine
sleep 5 && docker exec neko-live-pg psql -U postgres -d app -c "create table orders(id serial primary key, note text); insert into orders(note) values ('seed');"
mkdir -p /tmp/neko-live-gj
sed -e "s|REPLACE_WITH_PER_ORG_SECRET_B64|$SECRET|" \
    -e "s|REPLACE_WITH_BASE64_32_BYTE_KEY|$(openssl rand -base64 32)|" \
    -e 's|^sources: \[\]|sources:\n  - name: app\n    kind: database\n    type: postgres\n    host: neko-live-pg\n    port: 5432\n    dbname: app\n    user: postgres\n    password: pg\n    read_only: true\n    access:\n      read: authenticated|' \
    db/graphjin/customer.sources.example.yml > /tmp/neko-live-gj/agentic.yml
docker run -d --name neko-gj-live --network neko-live -p 127.0.0.1:8090:8080 -e GO_ENV=agentic \
  -v /tmp/neko-live-gj:/config dosco/graphjin:3.21.13 serve --path /config
pnpm --filter @neko/llm exec vitest run test/integration/graphjin-mcp-live.test.ts
docker rm -f neko-gj-live neko-live-pg && docker network rm neko-live
```

A database source needs `access.read: authenticated` for the worker's roles to
read it; OpenNeko sets that on every source it registers. `query_catalog` is
absent from the tool list until a source is configured.

## A2UI rendering

A web turn mounts one logical server, `neko_ui`, with one tool,
`render_cards`. Its canonical Hermes ACP title is
`mcp_neko_ui_render_cards`. There is no separate Hermes render stub or live
fence path; the brokered server validates the call and solely emits the
surface.

[`src/work/a2ui-contract.ts`](src/work/a2ui-contract.ts) is the source of truth
for the names, the Zod validator, and the JSON Schema generated from it. Every
argument is a string, a list of strings, or an enum. The host builds the A2UI
v1.0 surface, including its version, catalog, surface ID, `Answer` root and
component IDs. Persisted A2UI v0.9 messages retain reader-only compatibility.

```json
{
  "title": "Revenue by quarter",
  "table": "Quarter | Revenue (USD)\n2026 Q2 | 13848055.85\n2026 Q3 | 13950000",
  "chart": "line",
  "chartColumn": "Revenue (USD)",
  "keyFigures": ["2026 Q3: $13.95M (up 1% on Q2)"],
  "followUps": ["Break 2026 Q3 down by region"]
}
```

`table` is a pipe table. The chart draws the `chartColumn` values from the
table, so a charted figure is written once. The host reads the text with plain
string splitting: rows on line breaks (or a literal `\n`), cells on `|`, and
numbers with `Number()` after removing thousands separators. `keyFigures`
entries are `Label: value` with an optional trailing `(note)`.

Flat text fields matter because Hermes defers MCP tools behind a `tool_call`
bridge whose `arguments` parameter is an open object. The model then sees the
schema only as text and gets no structural guidance, so small models fill flat
strings reliably and nested objects poorly. `normalizeRenderCardsInput`
recovers the bridge's call shapes: a second `{name, arguments}` wrapper, the
call sent as JSON text, and `null` / `false` / `""` placeholders.
`pnpm --filter @neko/worker eval:cards --bridge` reproduces that path.

The source configuration form is a separate tool, `present_source_form`, on the
`neko_source_config_manager` server; the host builds that form from the
agent's prefill values.

Every tool schema uses the portable JSON Schema subset in
[`src/tool-schema-portability.ts`](src/tool-schema-portability.ts): declared
object properties, typed array items and string enums. Providers drop or
reject open objects, unions and `const`, so a schema that relies on them loses
its structure on some models. `test/tool-schema-portability.test.ts` checks
every static tool.

Accepted calls remain visually quiet because the cards carry the evidence.
Rejected calls emit a correlated `tool_start` containing the exact `rawInput`,
canonical title, and validation issues, followed by `tool_end`. This preserves
diagnostic evidence without showing successful render-tool pills.

## Regression invariants

Tests cover future/unknown GraphJin tools across both broker hops, native schema
and result preservation, the no-`initialize` contract, one canonical render
server/tool, portable tool schemas, one surface emission for valid calls,
and exact rejected render input in telemetry.
