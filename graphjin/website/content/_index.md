---
title: "GraphJin"
description: "GraphJin gives AI agents one governed graph to explore across your databases, files, APIs, and source code - within boundaries you control."
---

<section class="home-hero home-hero-centered"><div class="home-hero-copy"><p class="home-section-label">v3 · Apache 2.0</p><h1>The context layer for your AI agents.</h1><p class="home-lede">Most teams hand-wire a brittle API or tool for every question an agent might ask. GraphJin gives agents one governed graph to explore instead - every database, file, API, and your own source code, joined in a single GraphQL query that's simpler than SQL.</p><p class="home-sublede"><strong>Explore within bounds</strong> - one config controls what every agent can discover, query, run, change, and never touch. <strong>Or skip the wiring</strong> - a <a href="#agent">built-in agent</a> turns one instruction into an evidence-backed answer.</p><div class="home-proof-grid" aria-label="GraphJin proof points"><article><span>01</span><strong>Auto-learn</strong><small>schema + sources</small></article><article><span>02</span><strong>Compile</strong><small>optimized DB work</small></article><article><span>03</span><strong>Govern</strong><small>policy boundaries</small></article><article><span>04</span><strong>Audit</strong><small>human/model review</small></article></div><div class="home-actions"><a class="button-primary" href="#quickstart">Get started</a><a class="button-secondary" href="#how">How it works</a></div></div></section>

<section id="problem" class="home-section"><div class="home-section-heading"><p class="home-section-label">The problem</p><h2>Your agent is only as good as what it can see.</h2><p>A capable model still enters your stack blind. It doesn't know your schema, your permissions, your saved queries, or where a field is written in code - so it works from memory and guesses.</p></div>
{{< svg "problem-blind-agent" "An AI agent guessing at disconnected systems" >}}
<div class="home-card-grid"><article class="home-card"><div class="home-card-icon">01</div><h3>A tool for every question</h3><p>You hand-write a brittle API or MCP tool for each thing an agent might ask. The surface never keeps up, and every new question is new glue code.</p></article><article class="home-card"><div class="home-card-icon">02</div><h3>Guesses, not facts</h3><p>Without a map, agents invent joins, fake fields, and confuse API shape with database shape. Every translation is a chance to drift.</p></article><article class="home-card"><div class="home-card-icon">03</div><h3>Too risky for production</h3><p>Handing an agent raw credentials means hoping it guesses right - so it stays read-only, shallow, or boxed out of the systems that matter.</p></article></div><p class="home-section-note"><strong>The fix isn't a smarter prompt.</strong> It's giving the agent the map - and the guardrails.</p></section>

<section id="what" class="home-section"><div class="home-section-heading"><p class="home-section-label">What GraphJin is</p><h2>One graph your agent can explore - within bounds you set.</h2><p>Instead of brittle hand-written APIs, point GraphJin at the systems you already run. It auto-learns the shape and exposes one governed graph the agent can explore: a single GraphQL query - far simpler than SQL for most models - across every database, file, remote API, and your own source code, including the relationships across them.</p></div><div class="badge-row"><span>Not another MCP-to-database connector</span><span>Not a pile of hand-written wrappers</span><span>Not a prompt full of guesses</span></div>
{{< svg "what-is-graph" "One governed graph across data, files, APIs, and code" >}}
<div class="home-card-grid"><article class="home-card"><div class="home-card-icon">EX</div><h3>Explore, don't guess</h3><p>Discover schemas, relationships, and examples from the live system, not from memory.</p></article><article class="home-card"><div class="home-card-icon">1Q</div><h3>One query, every system</h3><p>Databases, files, APIs, and code join in a single request.</p></article><article class="home-card"><div class="home-card-icon">WB</div><h3>Within bounds</h3><p>One config decides what each agent can see, run, and change.</p></article></div><p class="home-section-note"><strong>There's nothing else that turns your data and your code into one governed surface an agent can safely explore.</strong></p></section>

<section id="ai-queries" class="home-section ai-query-section"><div class="home-section-heading centered"><p class="home-section-label">See it work</p><h2>Ask in plain English. Get real data back.</h2><p>Claude Desktop, Codex, or any MCP client talks to GraphJin. The agent discovers the shape, validates its query, and GraphJin compiles it into one optimized database query - then answers with rows it can reason over.</p></div><div class="ai-stage"><div class="ai-window"><div class="window-chrome"><span></span><span></span><span></span><i aria-hidden="true">✣</i><strong>Claude Desktop</strong></div><div class="ai-conversation"><div class="chat-user">who's the top customer?</div><div class="assistant-row"><div class="assistant-avatar" aria-hidden="true">✣</div><div class="chat-assistant"><div class="tool-call"><div class="tool-call-header"><span class="tool-caret" aria-hidden="true">⌄</span><span>query_catalog</span></div><code>discover -> customers · purchases · products  (2 relationships)</code></div><div class="tool-call"><div class="tool-call-header"><span class="tool-caret" aria-hidden="true">⌄</span><span>validate_where_clause</span></div><code>validate -> filters ok on customers, purchases · order_by total_spent</code></div><div class="tool-call"><div class="tool-call-header"><span class="tool-caret" aria-hidden="true">⌄</span><span>execute_graphql</span></div><code>{ customers { id full_name email purchases { quantity product { price } } } }</code></div><details class="gen-sql"><summary><span aria-hidden="true">SQL</span> one optimized query, no N+1, no resolvers</summary>{{< hl sql >}}SELECT json_agg(__sj.json) AS customers
FROM customers AS c
LEFT JOIN LATERAL (
  SELECT sum(p.quantity * pr.price) AS total_spent
  FROM purchases p
  JOIN products pr ON pr.id = p.product_id
  WHERE p.customer_id = c.id
) __agg ON true
ORDER BY __agg.total_spent DESC NULLS LAST
LIMIT 5;{{< /hl >}}</details><p class="done-line"><span aria-hidden="true">✓</span> Done</p><p class="assistant-copy">Based on the purchase data, here are the top customers ranked by total spend:</p><div class="result-table" role="table" aria-label="Top customer results"><div role="row"><span>Rank</span><span>Customer</span><span>Email</span><span>Orders</span><span>Items</span><span>Total Spent</span></div><div role="row"><strong>01</strong><strong>Antwan Friesen</strong><span>francohirthe@medhurst.com</span><span>20</span><span>124</span><strong>$928.45</strong></div><div role="row"><strong>02</strong><span>Lon Cruickshank</span><span>margaretbailey@ruecker.info</span><span>20</span><span>94</span><span>$586.50</span></div><div role="row"><strong>03</strong><span>Susana Schaefer</span><span>jewelpowlowski@osinski.biz</span><span>20</span><span>91</span><span>$580.72</span></div></div><p class="assistant-summary">Antwan Friesen is the top customer with almost $1,000 in purchases, about 60% more than the runner-up.</p></div></div></div></div></div><div class="home-actions centered-actions"><a class="button-primary" href="#quickstart">Try it yourself</a></div></section>

<section id="agent" class="home-section"><div class="home-section-heading centered"><p class="home-section-label">Built-in agent</p><h2>One instruction in. One evidence-backed answer out.</h2><p>Above, an external client drives the loop, tool call by tool call. GraphJin can also run that loop itself. POST one instruction - the built-in agent discovers, validates, and executes inside GraphJin, as the caller, under the caller's permissions - and returns a typed answer your code can branch on. No orchestration framework, no tool wiring.</p></div><div class="agent-stage">
{{< code-card filename="terminal" language="bash" >}}
# enable once in agentic.yml:
#   agent: { enabled: true, provider: openai,
#            model: gpt-4.1-mini, api_key_env: OPENAI_API_KEY }

curl -sS localhost:8080/api/v1/agent \
  -H 'content-type: application/json' \
  -d '{"instruction": "What production work should we prioritize next?"}'
{{< /code-card >}}
{{< code-card filename="response.json" language="json" >}}
{
  "status": "answered",
  "answer": "Three open production orders, in priority order: 420 bags of Northstar House Blend 340g, 80 bags of Harbor Espresso 1kg, and 160 bags of Dawn Patrol Single Origin 250g. Start the Northstar run - priority 1, largest committed volume.",
  "data": [
    { "priority": 1, "product_name": "Northstar House Blend 340g", "quantity_bags": 420 },
    { "priority": 2, "product_name": "Harbor Espresso 1kg", "quantity_bags": 80 },
    { "priority": 3, "product_name": "Dawn Patrol Single Origin 250g", "quantity_bags": 160 }
  ],
  "evidence": {
    "protocol": {
      "catalog_ids": ["table:ops:public.production_orders"],
      "executions": [{ "has_data": true }]
    }
  },
  "actions": [
    { "step": 1, "tool": "query_catalog", "status": "ok" },
    { "step": 2, "tool": "execute_graphql", "status": "ok" }
  ]
}
{{< /code-card >}}
</div><div class="badge-row"><span>3 lines of YAML to enable</span><span>MCP tool · REST · SSE streaming</span><span>Web-console chat built in</span><span>Kill-switch: agent.read_only</span></div><div class="home-card-grid"><article class="home-card"><div class="home-card-icon">ID</div><h3>Runs as the caller</h3><p>The agent inherits the request's identity. Core roles and row-level security decide what it can read or write - the same enforcement as any GraphQL request, with a capability profile that can't be spoofed from a request body.</p></article><article class="home-card"><div class="home-card-icon">EV</div><h3>Answers it can't fake</h3><p>Protocol guards in Go force discovery, validation, and evidence before execution. Skip a step and <code>answered</code> downgrades to <code>blocked</code> - with a machine-actionable refusal naming the exact unblock steps, not a lecture. <a href="#proof">Watch one get caught.</a></p></article><article class="home-card"><div class="home-card-icon">7B</div><h3>Any code-capable model</h3><p>The loop is generated JavaScript in a sandbox, not provider function-calling. Point <code>agent.base_url</code> at any OpenAI-compatible endpoint - vLLM, Ollama, OpenRouter, a local 7B - or set <code>agent.sampling: auto</code> and borrow the calling MCP client's model. No server-side key at all.</p></article></div><div class="home-actions centered-actions"><a class="button-primary" href="/agentic/server-agent/">Run the built-in agent</a><a class="button-secondary" href="/story/agentic/">Read the deep dive</a></div></section>

<section id="proof" class="home-section"><div class="home-section-heading"><p class="home-section-label">Answers it can't fake</p><h2>The model made up an answer. The ledger caught it.</h2><p>A real run against the coffee-roastery demo. Every real tool call goes through Go, which keeps its own ledger of what actually happened - what was discovered, what ran, what returned data. The model's words never write that ledger, and after the run GraphJin cross-checks every claim against it. Deterministically, in Go - where a clever prompt can't reach.</p></div><div class="agent-stage"><div class="proof-beats"><article><span>01 - The ask</span><h3>The query never runs</h3><p>"Which roast batches should be held for quality review?" The model reaches for the saved query but skips the required detail inspection, so the guard rejects the call. No rows ever come back.</p></article><article><span>02 - The bluff</span><h3>The model invents a table</h3><p>Instead of reporting the miss, it writes a confident quality-review table - batch codes, reasons, recommendations. Plausible, well-formatted, and completely made up.</p></article><article><span>03 - The catch</span><h3>Go checks the ledger</h3><p>No successful execution in the ledger means the claims have no evidence. GraphJin flips <code>answered</code> to <code>blocked</code> and returns the refusal on the right - the fake table never leaves the server.</p></article></div><div class="code-stack">
{{< code-card filename="response.json" language="json" >}}
{
  "status": "blocked",
  "refusal": {
    "code": "saved_query_detail_required",
    "blocked_action": "execute_saved_query",
    "because": [
      "protocol violation: inspect query_catalog(id: \"saved_query:batch_quality_snapshot\") before execute_saved_query"
    ],
    "unblock": [
      {
        "tool": "query_catalog",
        "args": { "id": "saved_query:batch_quality_snapshot" },
        "reason": "Inspect the saved query detail before executing it."
      }
    ],
    "lawful_alternative": "Inspect the saved query detail first, then execute the approved saved query.",
    "retryable": true
  }
}
{{< /code-card >}}
<p class="code-note"><strong>Captured live.</strong> The actual response from this run - trimmed, not retouched. The refusal is machine-actionable: it names the exact unblock step, so a calling agent corrects course and retries instead of guessing.</p></div></div><div class="badge-row"><span>Ledger written by Go, not the model</span><span>Model history is untrusted</span><span>Deterministic post-run check</span><span>Runs as the caller - RLS still applies</span></div><p class="home-section-note"><strong>The smoke suite jailbreak-tests this in every demo vertical.</strong> A model told to skip discovery and mutate anyway gets <code>status: "blocked"</code> and a machine-actionable refusal with unblock steps - asserted end-to-end on every smoke run. Watch the loop live: every demo streams each tool call - green dot or red - at <code>localhost:8080/agent</code>.</p><div class="home-actions"><a class="button-primary" href="/agentic/server-agent/">How the guards work</a><a class="button-secondary" href="#demos">Try it on a demo</a></div></section>

<section id="demos" class="home-section"><div class="home-section-heading"><p class="home-section-label">Demo verticals</p><h2>Five demos. Real domains. One command each.</h2><p>Not toy schemas - each demo boots a full vertical: schema, seeded operational data, saved queries, workflows, and the agent wired in. Seeds anchor to today, so the data never goes stale. Delete the <code>demo/</code> folder to reset. Pick a domain and start interrogating it.</p></div><div class="badge-row"><span>5 verticals</span><span>6 database engines</span><span>CodeSQL + files + OpenAPI sources</span><span>12 workflows · 12 saved queries</span></div><div class="demo-grid"><article class="home-card demo-card"><div class="demo-meta"><span class="demo-port">:8080</span><span class="demo-stack">Postgres · BigQuery-emu · CodeSQL</span></div><h3>Coffee roastery</h3><p>Roast schedules, cupping scores, sensor telemetry, production orders - the flagship agentic demo.</p><p class="demo-question">"Which roast batch should be held for quality - and why?"</p><code class="demo-cmd">graphjin serve --demo --path examples/coffee-roastery</code></article><article class="home-card demo-card"><div class="demo-meta"><span class="demo-port">:8083</span><span class="demo-stack">SQLite - zero Docker</span></div><h3>Clinic scheduler</h3><p>Appointments, waitlists, no-shows - built into the binary; boots in seconds, no clone, no containers.</p><p class="demo-question">"Who should get the next open cardiology slot?"</p><code class="demo-cmd">graphjin serve --demo</code></article><article class="home-card demo-card"><div class="demo-meta"><span class="demo-port">:8081</span><span class="demo-stack">MySQL · BigQuery-emu · JWT roles</span></div><h3>Corrugated plant</h3><p>Work orders, corrugator runs, downtime, quality holds - behind real role-gated auth.</p><p class="demo-question">"Which work orders should the corrugator run first?"</p><code class="demo-cmd">graphjin serve --demo --path examples/corrugated-plant</code></article><article class="home-card demo-card"><div class="demo-meta"><span class="demo-port">:8082</span><span class="demo-stack">8 sources - Mongo · files · OpenAPI</span></div><h3>PCB fab</h3><p>Fab orders, yield analytics, test measurements, Gerber files, a live supplier API - one graph.</p><p class="demo-question">"Which fab order should we release next - and what's the evidence?"</p><code class="demo-cmd">graphjin serve --demo --path examples/pcb-fab</code></article></div><p class="demo-classic">Just want the classic GraphQL starter? <strong>webshop</strong> - single Postgres, RBAC, a Stripe-style remote join, recursive comments. <code>graphjin serve --demo --path examples/webshop</code></p><div class="home-actions"><a class="button-primary" href="/start/demos/">Explore all five demos</a><a class="button-secondary" href="/start/demos/#clinic-scheduler">Zero Docker? Start with the clinic</a></div></section>

<section id="databases" class="home-section database-section"><div class="database-copy"><p class="home-section-label">Databases</p><h2>Works with all your databases.<br>And more.</h2><p>Point GraphJin at <strong>as many systems as you need</strong> - Postgres for users, MySQL for orders, Snowflake, Redshift, and BigQuery for analytics, Cassandra or Keyspaces for CQL workloads, MongoDB for events, HTTP APIs for remote services, object storage for files, and CodeSQL for source trees - and query them through a single GraphQL endpoint. Joins, remote joins, subscriptions, search, and mutations compose <strong>across systems in one request</strong>, so an AI assistant can reason across the data, APIs, files, and code without learning every backend.</p></div>

{{< database-logos >}}

</section>

<section id="how" class="home-section"><div class="home-section-heading"><p class="home-section-label">How it works</p><h2>One compiler. Any system. Any client.</h2><p>Point GraphJin at databases, object storage, source trees, and remote APIs. It's a compiler, not a resolver framework: it learns the live shape, plans the work, and emits one optimized database operation - then enforces RBAC and serves AI assistants, REST clients, and federated routers from the same engine. No N+1, no resolver code, test-backed against real compiler paths.</p></div>
{{< svg "compiler-flow" "GraphJin compiler flow" >}}
</section>

<section id="agentic" class="home-section"><div class="home-section-heading"><p class="home-section-label">The agent loop</p><h2>Discover, check, validate, act.</h2><p>GraphJin is the operating graph agents use to understand a real organization. It auto-learns the live surface, compiles GraphQL into database and source-backed work, and keeps policy visible enough for both humans and models to inspect.</p></div><div class="agentic-grid"><div class="agentic-loop"><div class="agentic-loop-line">gj_catalog -> evidence -> gj_security -> validate/preview -> governed action -> observe/refresh</div><div class="agentic-steps"><article><span>01 - Discover</span><h3>gj_catalog</h3><p>Find schemas, relationships, syntax, workflows, capabilities, examples, and evidence before choosing a path.</p></article><article><span>02 - Check</span><h3>gj_security</h3><p>Read effective policy and high-risk findings before config, workflow, file, code, or mutation actions.</p></article><article><span>03 - Validate</span><h3>preview</h3><p>Validate filters, inspect generated work, run approved workflows, or preview CodeSQL changes before applying.</p></article><article><span>04 - Act</span><h3>governed surface</h3><p>Execute through GraphQL, MCP, saved queries, workflows, and guarded source operations instead of raw credentials.</p></article></div></div><aside class="agentic-panel"><span class="home-section-label">One operating graph</span><h3>The model reads evidence, not guesses.</h3><p>The agent learns from graph evidence: catalog rows, security rows, relationships, code references, workflow metadata, config facts, validation results, preview diffs, and execution results.</p><div class="badge-row"><span>Application data</span><span>Remote APIs</span><span>Source code</span><span>Workflows</span><span>Watches</span><span>Security posture</span></div></aside></div></section>

<section id="security-model" class="home-section"><div class="home-section-heading"><p class="home-section-label">Security model</p><h2>Safer agents, not smaller agents.</h2><p>GraphJin makes agents safer by giving them explicit boundaries, not by making them blind. Agents can explore more of the live organization because policy, evidence, and action paths are inspectable and enforced.</p></div><div class="security-layout"><div class="security-copy"><article class="security-thesis"><h3>One config defines the AI surface.</h3><p>Humans can review and diff the policy. Models can inspect the same posture through <code>gj_catalog</code> and <code>gj_security</code> before acting. GraphJin enforces that policy across GraphQL, MCP, workflows, code, files, APIs, and databases.</p></article><div class="security-controls"><article><h3>RBAC and row filters</h3><p>Roles, table permissions, column blocks, automatic filters, and mutation limits are enforced inside the compiler.</p></article><article><h3>Saved queries and allow-lists</h3><p>Production agents can run named, reviewed query contracts instead of inventing arbitrary operations at runtime.</p></article><article><h3>Read-only source boundaries</h3><p>Filesystems, CodeSQL, databases, and control-plane tables can expose discovery without granting writes.</p></article><article><h3>Preview before change</h3><p>CodeSQL change sets require file hashes, exact ranges, old text, optional locks, and a preview/apply loop.</p></article></div></div><aside class="code-stack security-query">
{{< code-card filename="policy-before-action.graphql" language="graphql" >}}
query {
  summary: gj_security(id: "summary") {
    mode
    summary
    summary_json
  }

  findings: gj_security(
    where: {
      kind: { eq: "finding" }
      severity: { in: ["high", "critical"] }
    }
    order_by: { severity_rank: desc }
  ) {
    severity
    title
    recommendation
    evidence_json
  }
}
{{< /code-card >}}
<p class="code-note"><strong>Policy before action:</strong> agents check high-risk findings, effective permissions, and read-only state before requesting a write, workflow execution, config update, file operation, or code edit.</p></aside></div></section>

<section id="mcp" class="home-section feature-split"><div class="home-section-heading"><p class="home-section-label">AI integration</p><h2>Connect any agent over MCP, in one command.</h2><p>GraphJin ships a built-in Model Context Protocol (MCP) server - so Claude, Codex, or any MCP client can explore and query your data through the same governed surface.</p></div><div class="feature-section-grid"><div class="feature-copy"><p>One command wires GraphJin into Claude Desktop, Codex, or any MCP host. From there the agent discovers what exists, validates its query, checks policy, and runs only what you've approved - no hand-written tool for every question.</p><p>Run it locally for development, or as a hosted HTTP endpoint for your team - gated by MCP OAuth or the same JWT/OIDC identity as the main API.</p>
{{< svg "mcp-flow" "MCP discovery to governed action" >}}
</div><aside class="code-stack">
{{< code-card filename="terminal" language="bash" >}}
# local/dev helper
graphjin mcp add codex
graphjin mcp add claude

# direct client URL
codex mcp add graphjin --url http://localhost:8080/api/v1/mcp
claude mcp add --transport http graphjin http://localhost:8080/api/v1/mcp

# hosted GraphJin with OAuth
codex mcp add graphjin --url https://graphjin.example.com/api/v1/mcp
claude mcp add --transport http graphjin https://graphjin.example.com/api/v1/mcp
{{< /code-card >}}
</aside></div></section>

<section id="codesql" class="home-section"><div class="home-section-heading"><p class="home-section-label">Code intelligence</p><h2>CodeSQL: query your code as well.</h2><p>CodeSQL indexes your source tree into a read-only SQLite graph. Agents can ask where a column is used, which code references it, which symbol owns that reference, and what guarded change set would update it - all through the same GraphQL interface.</p></div><div class="codesql" aria-label="Querying source code with CodeSQL"><div class="codesql-head"><span class="codesql-kicker">agent asks</span><strong>Where is <code>users.email</code> used?</strong></div><div class="codesql-grid"><figure class="codesql-pane codesql-pane-query"><figcaption>GraphQL · gj_code</figcaption>{{< hl graphql >}}query {
  gj_code(where: {
    name: { eq: "users.email" }
    kind: { eq: "db_ref" }
  }) {
    path
    symbol_name
  }
}{{< /hl >}}</figure><div class="codesql-pane codesql-pane-rows"><div class="codesql-rows-head"><span>matched rows</span><em>3</em></div><div class="codesql-rowset"><div class="codesql-row"><span class="codesql-path">api/users.go</span><span class="codesql-sym">getUser</span></div><div class="codesql-row"><span class="codesql-path">api/invoices.ts</span><span class="codesql-sym">createInvoiceHandler</span></div><div class="codesql-row"><span class="codesql-path">graph/schema.go</span><span class="codesql-sym">userType</span></div></div></div></div><div class="codesql-foot"><span>tree-sitter → read-only SQLite</span><div class="codesql-kinds"><code>kind=file</code><code>kind=symbol</code><code>kind=db_ref</code><code>kind=doc</code></div></div></div></section>

<section id="capabilities" class="home-section"><div class="home-section-heading"><p class="home-section-label">In the box</p><h2>A full backend, not just an agent gateway.</h2><p>The same binary and config that govern the AI surface also run your realtime, files, remote APIs, auth, and federation - no extra services to operate.</p></div><div class="stats-grid"><article><strong>1</strong><span>Auditable config for agent access across the AI surface.</span></article><article><strong>12+</strong><span>Database and warehouse engines through one GraphQL surface.</span></article><article><strong>0</strong><span>Lines of resolver code. The compiler does the work.</span></article></div><div class="home-card-grid feature-grid-large"><article class="home-card"><div class="home-card-icon">FS</div><h3>Files as tables</h3><p>Uploads stream to local disk, S3, R2, or GCS; each bucket is a queryable table you can join with the rest of your schema.</p></article><article class="home-card"><div class="home-card-icon">API</div><h3>Remote APIs</h3><p>Drop in an OpenAPI 3 spec and its operations become joinable, RBAC-aware fields with per-spec auth caching.</p></article><article class="home-card"><div class="home-card-icon">RT</div><h3>Realtime</h3><p>Subscribe with the same GraphQL; cursor-based SSE and WebSocket streams resume after a drop, with polls batched into one statement.</p></article><article class="home-card"><div class="home-card-icon">JWT</div><h3>Authentication</h3><p>JWT and OIDC from Auth0, Firebase, Okta, or any JWKS - one auth pipeline across HTTP, WebSocket, SSE, and MCP.</p></article><article class="home-card"><div class="home-card-icon">WF</div><h3>Workflows</h3><p>Discover approved workflows and run them through GraphQL, REST, MCP, or the CLI.</p></article><article class="home-card"><div class="home-card-icon">RDS</div><h3>Caching</h3><p>Response caching on Redis with an in-memory fallback and stale-while-revalidate.</p></article><article class="home-card"><div class="home-card-icon">CLI</div><h3>One binary CLI</h3><p>Dev server, database toolchain, device-code login, and MCP wiring. What runs in CI matches production.</p></article><article class="home-card"><div class="home-card-icon">FED</div><h3>Federation</h3><p>Advanced: flip a flag and every keyed table becomes an Apollo Federation v2 subgraph.</p></article></div></section>

<nav class="home-paths" aria-label="Choose your path"><a href="/start/install/"><strong>Install</strong><span>Run GraphJin locally and scaffold a project.</span></a><a href="/start/demos/"><strong>Demos</strong><span>Five runnable verticals - coffee, clinic, corrugated, PCB fab, webshop - one command each.</span></a><a href="/story/vision/"><strong>Vision</strong><span>Why GraphJin treats data, APIs, files, code, workflows, and policy as one operating graph.</span></a><a href="/core/query-language/"><strong>Query language</strong><span>Filters, geo filters, relationships, cursors, Expression aggregates.</span></a><a href="/agentic/mcp/"><strong>MCP for agents</strong><span>Catalog-first discovery, validation, execution, and policy checks.</span></a><a href="/agentic/server-agent/"><strong>Built-in agent</strong><span>One instruction in, a typed evidence-backed answer out.</span></a><a href="/configure/sources-mode/"><strong>Sources mode</strong><span>Databases, filesystems, code, remote APIs, and capabilities.</span></a><a href="/reference/test-backed-examples/"><strong>Examples</strong><span>Feature map tied back to the Go example tests.</span></a><a href="/reference/config-reference/"><strong>Config reference</strong><span>Production, auth, caching, uploads, federation, OpenAPI.</span></a></nav>

<section id="quickstart" class="home-section"><div class="home-section-heading"><p class="home-section-label">Get started</p><h2>Run it in two minutes.</h2><p>Three commands: install, boot a real demo, connect your agent.</p></div><div class="start-steps"><div class="start-step"><div class="start-num">1</div><div class="start-step-body"><h3>Install the binary</h3>{{< code-card filename="terminal" language="bash" >}}
curl -fsSL https://graphjin.com/install.sh | bash
{{< /code-card >}}<p class="start-note">Or <code>brew install dosco/graphjin/graphjin</code></p></div></div><div class="start-step"><div class="start-num">2</div><div class="start-step-body"><h3>Boot the built-in demo</h3>{{< code-card filename="terminal" language="bash" >}}
graphjin serve --demo
{{< /code-card >}}<p class="start-note">The binary ships with the clinic-scheduler demo - SQLite, no Docker, no clone; it extracts to <code>./graphjin-demo</code> with seeded data, saved queries, and workflows. Drop a model key in <code>./.env</code> (<code>OPENAI_API_KEY</code>, <code>ANTHROPIC_API_KEY</code>, or <code>GOOGLE_APIKEY</code>) and the built-in agent switches on automatically. Want the flagship vertical? Clone the repo and run <code>graphjin serve --demo --path examples/coffee-roastery</code>.</p></div></div><div class="start-step"><div class="start-num">3</div><div class="start-step-body"><h3>Connect your AI client</h3>{{< code-card filename="terminal" language="bash" >}}
graphjin mcp add claude
{{< /code-card >}}<p class="start-note">Using Codex? <code>graphjin mcp add codex</code></p></div></div></div><p class="start-done">That's it. Open the web console at <code>localhost:8083</code> - chat with the built-in agent at <code>localhost:8083/agent</code>, or ask from your MCP client. Real schema, real rows, evidence-backed answers.</p><div class="home-actions"><a class="button-primary" href="/start/quick-start/">Read the quick start</a><a class="button-secondary" href="/start/demos/">Browse the demos</a></div></section>
