---
title: "No hand-built tools"
headline: "A map of everything, not a tool for each thing."
description: "Most company agents are wired up one hand-written tool at a time. DeepORG gives the agent a searchable map of everything you connect and lets it write its own queries, checked against your access rules and run as one database query."
lede: "Most company agents are wired up one tool at a time. DeepORG gives the agent a searchable map of everything you connect, and the agent writes its own queries against it. Connecting a new database or API is a few lines of config, not a new set of tools."
scene: "map"
scene_label: "Animated diagram: on the left a list of hand-written tools keeps growing and some go stale; on the right the agent finds its own path across one map of tables, files and APIs."
scene_caption: "a tool for each thing / a map of everything"
actions:
  - label: "Try it in 2 minutes"
    href: "/#quickstart"
  - label: "Catalog docs"
    href: "/agentic/catalog-graph/"
landing: true
toc: false
---

{{< band id="problem" label="The problem" title="Your agent is only as good as what it can see." >}}

A capable model still enters your stack blind. It does not know your schema, your permissions, your saved queries, or where a field is written in code, so it works from memory and guesses.

The usual fix is a tool for each question: a “get customer” tool here, a “list invoices” tool there, each written and maintained by hand. They break when a schema changes, and every new question or new system means more tools.

{{< grid >}}

- **A tool for every question.** Worldwide AI spend is forecast at $2.5 trillion for 2026,<sup><a href="#sources">1</a></sup> yet teams still hand-write a brittle API or MCP tool for each thing an agent might ask. The surface never keeps up.
- **Guesses, not facts.** 21% is the best model's success rate on real enterprise text-to-SQL, down from 91% on academic benchmarks.<sup><a href="#sources">2</a></sup> Without a map, agents invent joins and fields.
- **Too risky for production.** 88% of organizations saw an AI-agent security incident in the past year.<sup><a href="#sources">3</a></sup> Raw credentials mean hoping the agent guesses right, so it stays read-only, shallow, or boxed out.

{{< endgrid >}}

The fix is not a smarter prompt. It is giving the agent the map, and the guardrails. And it is not another raw connector: 43% of tested MCP servers shipped with command-injection flaws.<sup><a href="#sources">4</a></sup>

<p class="home-sources" id="sources">Sources: <a href="https://www.gartner.com/en/newsroom/press-releases/2026-1-15-gartner-says-worldwide-ai-spending-will-total-2-point-5-trillion-dollars-in-2026" rel="nofollow">1. Gartner, Jan 2026</a> · <a href="https://arxiv.org/abs/2411.07763" rel="nofollow">2. Spider 2.0, ICLR 2025</a> · <a href="https://www.gravitee.io/state-of-ai-agent-security" rel="nofollow">3. Gravitee State of AI Agent Security, 2026</a> · <a href="https://equixly.com/blog/2025/03/29/mcp-server-new-security-nightmare/" rel="nofollow">4. Equixly, 2025</a></p>

{{< endband >}}

{{< band label="The map" title="One catalog of everything you connect." >}}

DeepORG keeps a catalog of everything it can reach: databases and sources, tables and columns, relationships and the paths that cross from one database to another, functions, workflows, query patterns, and what each source is allowed to do.

The agent learns one habit: search by intent, then inspect the best match. Every run begins with a search for the person's own words. It pulls more only when a path it needs is missing, so the map can be very large while the model reads a small part of it. By default the first search returns up to 40 entries and later ones 20.

{{< grid >}}

- **Search by intent.** “Which accounts are behind on payment” finds accounts, invoices and the relationship between them, whatever the columns are called.
- **Inspect by id.** One entry at a time: the columns, their types, the values a status column can take, and how the table joins its neighbours.
- **Notes people leave.** Pin a note to anything on the map: what a column really means, which table is the source of truth. A note is approved before it is shared, and the agent treats it as data, never as an instruction.

{{< endgrid >}}

{{< endband >}}

{{< band label="The agent loop" title="Discover, check, validate, act." >}}

The model does not pick from a menu of tools. It writes a short program that calls a small, fixed set of functions: search the catalog, read the query syntax, validate a filter, run a query, and finish.

<div class="agentic-loop"><div class="agentic-steps"><article><span>01</span><h3>Discover</h3><code>gj_catalog</code></article><article><span>02</span><h3>Check</h3><code>gj_security</code></article><article><span>03</span><h3>Validate</h3><code>preview</code></article><article><span>04</span><h3>Act</h3><code>execute</code></article></div></div>

Some steps cannot be skipped. A write may only run after the target table's detail has been inspected, and a workflow is rejected until its detail was read. A model cannot talk its way past that: only real results count.

{{< endband >}}

{{< band label="A new system" title="A few lines of config, not a new set of tools." >}}

Connecting a database is naming it:

```yaml
sources:
  - name: app
    kind: database
    type: postgres
    default: true
    connection_string: ${DATABASE_URL}
    schema: public

  - name: warehouse
    kind: database
    type: snowflake
    connection_string: ${SNOWFLAKE_DSN}
```

Once a source is connected, its tables, columns and relationships are on the map. There is no tool to write for it and none to update when a column is renamed. [Every system you run](/why/every-system/) covers APIs, files and source code.

{{< endband >}}

{{< band label="What runs" title="The agent writes the query. The engine checks it and runs it." >}}

The agent's query is GraphQL. The engine compiles it into a single database query, applies your access rules while compiling, and returns the result. The model plans and the database computes: sums, counts and rankings are done where the data is.

One honest limit: a question that crosses two databases is not one statement. The engine fetches the parent rows, then runs the child query on the other database.

{{< endband >}}

{{< band label="In the docs" title="Go deeper." >}}

- [The catalog](/agentic/catalog-graph/): what is on the map and how the agent searches it.
- [Annotations](/agentic/annotations/): notes people pin to the map.
- [MCP](/agentic/mcp/): the fixed set of tools an outside agent sees.
- [Sources](/configure/sources-mode/): connecting databases, files, code and APIs.
- [The compiler](/core/compiler/): how one query becomes one statement.

{{< endband >}}
