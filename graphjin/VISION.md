# GraphJin Vision

## Organizations Are Made of Data and Code

The next great interface to an organization will be a living graph of how it
actually works.

Every organization is made from two operating systems: data and code.

Data is the memory of the organization. It holds customers, orders, accounts,
inventory, incidents, payments, permissions, events, metrics, logs, and the
trail of decisions that brought the business to its current state.

Code is the motion of the organization. It decides what gets created, billed,
validated, escalated, shown to customers, sent to partners, and trusted by the
workflows people rely on every day.

In reality, these systems are inseparable. A column exists because some code
reads or writes it. A workflow matters because it moves data from one state to
another. An API endpoint is useful because it exposes a piece of business truth.

But the tools usually split this world apart. Data lives in databases,
dashboards, replicas, and warehouses. Behavior lives in services, migrations,
SQL strings, GraphQL operations, REST clients, OpenAPI specs, config files, and
source repositories. The connections between them are spread across tribal
knowledge, scattered docs, and the one engineer who remembers why a field named
`status` means four different things.

That separation is expensive for humans. It is brutal for agents.

An LLM entering an organization does not just need a prompt. It needs a map:
what data exists, how it relates, what code reads or writes it, what APIs expose
it, what workflows depend on it, and which operations are safe. Without that
map, agents either stay shallow or improvise. They invent joins, guess fields,
confuse API shape with database shape, and grep without knowing what the code is
attached to.

## The Missing Graph

The missing abstraction is not another ORM, code search index, dashboard, or
agent framework. The missing abstraction is a common graph that connects the
organization's data and code through one governed interface.

GraphJin starts from a simple idea: GraphQL can be more than an application
API. It can be a schema-aware way to ask questions across the systems that make
an organization function.

GraphJin compiles GraphQL into optimized database queries, discovers schemas
and relationships, exposes metadata as queryable tables, joins across
databases and APIs, indexes source code through CodeSQL, and presents it all
through a single GraphQL and MCP interface.

In that graph, a table is not isolated from the code that writes it. A column is
not isolated from the handlers, workflows, SQL strings, GraphQL operations, or
config files that mention it. An OpenAPI operation, storage file, or database
function can become part of the same addressable surface.

The wow is practical: a question can start at a customer record, follow a
relationship into an invoice table, jump to the workflow that created it,
inspect the API that exposed it, find the code that validates it, and return
with a safe next action. The systems stay real. The interface becomes common.

That is the magic of GraphJin. It does not pretend complexity disappears. It
gives complexity a shape that humans and agents can share.

## One Interface for Databases, APIs, Files, and Code

For SQL databases, GraphJin compiles GraphQL into optimized SQL. For MongoDB,
it emits a JSON DSL that becomes aggregation pipelines. Object stores and
filesystem tables become queryable handles. OpenAPI operations become GraphQL
fields. Multiple databases can be traversed across boundaries. Metadata about
databases, tables, columns, relationships, functions, and indexes appears as
queryable `gj_*` rows.

Then CodeSQL brings source code into the same world.

This matters because the useful questions inside an organization rarely stay in
one system:

```graphql
query {
  gj_columns(where: { table_name: { eq: "invoices" } }) {
    column_name
    type
    code_db_refs {
      ref_kind
      file { path }
      symbol { name kind }
    }
  }
}
```

That query is small, but the idea behind it is large. A model can move from a
database column to code references without switching tools, inventing a code
search API, or asking a human where to look. It can see the schema and the
implementation together.

The same pattern applies across the rest of the system: ask what tables exist,
find a relationship path, validate a where clause, explain generated SQL, run a
saved query or workflow, preview a code edit, apply it, and re-read the graph.

## Why This Matters for Agents

LLM agents become powerful when they can iterate against reality. They become
dangerous or disappointing when they must act from memory.

A general model may know SQL, GraphQL, REST, and common framework patterns, but
it does not know a particular organization's schema, permissions, saved queries,
workflow scripts, API boundaries, or source layout until those facts are
exposed. The more fragmented the tools, the more the model must translate.
Every translation is a chance to drift.

GraphJin reduces that drift by giving agents a schema-aware loop:

1. Discover the live surface.
2. Ask for the exact syntax.
3. Validate the query or mutation.
4. Explain the generated database operation.
5. Execute only when the shape is understood.
6. Inspect the result and continue.

Through MCP, GraphJin can teach agents the local GraphJin DSL, list tables,
describe columns, find relationship paths, validate where clauses, run saved
queries, and orchestrate workflows. The agent can ask better questions because
it can see the shape of the organization.

The practical effect is fewer hallucinated joins, fewer fake fields, fewer
invented endpoints, fewer risky mutations, and fewer "please paste me the
schema" moments. The model can enter the organization through a front door
built for it: constrained, inspectable, auditable, and expressive.

## CodeSQL: Source Code as a Database

CodeSQL is the bridge from the data graph to the code graph.

Configure a source tree, and GraphJin creates a managed SQLite cache under
`config/codesql/`. It indexes source files with tree-sitter and records files,
versions, parse errors, nodes, symbols, scopes, references, imports, docs, and
best-effort database references. SQL strings, GraphQL operations, config and
allowlist files, migrations, and common model tags can all become searchable.

This does not claim omniscience. CodeSQL is structured source intelligence, not
a full semantic compiler for every language and framework. It does not promise
perfect type resolution or LSP-grade go-to-definition. Its value is practical:
it makes a source tree queryable enough for agents to find real anchors before
they reason or edit.

An agent can ask which handlers mention a table, where a column appears in SQL,
which workflows import a module, what code sits near a database reference, and
what changed after a source edit was applied and reindexed.

This is where the graph starts to feel alive. The model can follow a field in
production data to the migration that introduced it, the handler that writes it,
the GraphQL query that reads it, and the edit that would safely change it.

## Collaborative Coding with LLMs

Once code is part of the graph, editing can also become a governed workflow.

GraphJin's CodeSQL mutation path is designed for collaborative coding agents.
The model does not need raw shell writes to be useful. It can read live source,
request `code_files { path hash }`, prepare a guarded change set, preview the
diff, and apply only after the preview looks correct.

Change sets can replace byte ranges, create files, delete files, and rename
files. Replacements require exact ranges and `old_text`. Replace, delete, and
rename operations require the current file hash. Creates fail if the target
already exists. Longer sessions can use `code_locks` with leases and whole-file
path reservations. After apply, GraphJin reconciles the CodeSQL index so the
next read sees the new source truth.

That loop matters. The model can inspect, propose, preview, apply, and
re-inspect. Humans can see the same diff and audit trail. Other agents can
observe the updated graph. Conflicts become structured errors rather than
silent overwrites.

## Governance, Safety, and Trust

Connecting data, APIs, files, and code into one interface only works if the
interface is governable.

GraphJin makes the policy surface visible instead of scattering it through
prompts, proxy code, custom middleware, and human reminders. A GraphJin app can
be governed from a single config file: databases, metadata exposure, table and
role permissions, blocklists, read-only boundaries, automatic filters, mutation
blocks, MCP settings, and raw-query policy. The gateway becomes a file humans
can review, diff, and audit. An LLM can audit it too, asking what an agent can
read, what it can change, and where the dangerous paths are closed.

In production, GraphJin can require saved queries instead of arbitrary client
GraphQL. Those saved queries are contracts: named, pre-reviewed,
variable-shaped, and auditable. Role filters can be applied automatically,
columns or tables can be blocked, mutations can be disabled, and read-only
databases keep DDL and writes out of reach.

This is the security story that makes the graph feel almost unreasonable: one
governed gateway to the organization's data systems, APIs, metadata, files, and
code. Not a hundred fragile wrappers. One place to see access and tighten it,
without handing agents the keys to everything.

The goal is not to make agents harmless by making them powerless. The goal is
to make them useful inside boundaries the organization can understand. When an
agent asks to edit source, GraphJin does not trust a plausible patch. It checks
hashes, ranges, old text, locks, and paths.

## The Future: Agents That Understand the Organization

Organizations do not need agents that merely chat about the business. They
need agents that can enter it, understand its operating systems, and help
improve them.

That requires more than model intelligence. It requires substrate: a way to see
the relationships humans rely on but rarely write down. Which service owns this
table? Which workflow depends on this endpoint? Which saved query is safe to
run? Which code edit would update the behavior, and which data proves it
worked?

GraphJin's vision is to become that substrate, and then to become something
larger: the shared operating surface where people and agents work as one team.

In that future, GraphJin is not just a tool an agent calls. It is the graph the
team stands on. Humans bring intent, judgment, taste, accountability, and
business context. Agents bring tireless inspection, synthesis, execution, and
verification. GraphJin gives both sides the same map, controls, audit trail,
and way to change what is true.

That is when the organization starts to feel different. Agents can trace a
customer problem through data, APIs, and code; propose the workflow change;
preview the source edit; run the saved query that proves the fix; and hand the
result back to the humans who own the decision. They are inside the governed
graph, collaborating.

This is the future GraphJin is reaching for: organizations whose data and code
are no longer separate territories, but one governable surface where human
intent and agent capability become a team capable of driving organizations
forward.
