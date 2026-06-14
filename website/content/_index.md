---
title: "GraphJin"
description: "GraphJin auto-learns your systems and compiles GraphQL into governed database, API, file, code, workflow, and agentic data access."
---

<section class="home-hero home-hero-centered"><div class="home-hero-copy"><p class="home-section-label">v3 · Apache 2.0</p><h1>Automatic GraphQL for AI agents.</h1><p class="home-lede">Point GraphJin at your databases, APIs, source code, files, and workflows. It auto-learns the shape, compiles queries into optimized database work, and exposes one governed GraphQL + MCP surface for agents.</p><p class="home-sublede"><strong>One config</strong> controls what agents can discover, query, execute, edit, and never touch.</p><div class="home-proof-grid" aria-label="GraphJin proof points"><article><span>01</span><strong>Auto-learn</strong><small>schema + sources</small></article><article><span>02</span><strong>Compile</strong><small>optimized DB work</small></article><article><span>03</span><strong>Govern</strong><small>policy boundaries</small></article><article><span>04</span><strong>Audit</strong><small>human/model review</small></article></div><div class="home-actions"><a class="button-primary" href="#quickstart">Get started</a><a class="button-secondary" href="#security-model">Security model</a></div></div></section>

<nav class="home-paths" aria-label="Choose your path"><a href="/start/install/"><strong>Install</strong><span>Run GraphJin locally and scaffold a project.</span></a><a href="/story/vision/"><strong>Vision</strong><span>Why GraphJin treats data, APIs, files, code, workflows, and policy as one operating graph.</span></a><a href="/core/query-language/"><strong>Query language</strong><span>Filters, geo filters, relationships, cursors, Expression aggregates.</span></a><a href="/agentic/mcp/"><strong>MCP for agents</strong><span>Catalog-first discovery, validation, execution, and policy checks.</span></a><a href="/configure/sources-mode/"><strong>Sources mode</strong><span>Databases, filesystems, code, remote APIs, and capabilities.</span></a><a href="/reference/test-backed-examples/"><strong>Examples</strong><span>Feature map tied back to the Go example tests.</span></a><a href="/reference/config-reference/"><strong>Config reference</strong><span>Production, auth, caching, uploads, federation, OpenAPI.</span></a></nav>

<section id="databases" class="home-section database-section"><div class="database-copy"><p class="home-section-label">Databases</p><h2>Works with all your databases.<br>And more.</h2><p>Point GraphJin at <strong>as many systems as you need</strong> - Postgres for users, MySQL for orders, Snowflake, Redshift, and BigQuery for analytics, Cassandra or Keyspaces for CQL workloads, MongoDB for events, HTTP APIs for remote services, object storage for files, and CodeSQL for source trees - and query them through a single GraphQL endpoint. Joins, remote joins, subscriptions, search, and mutations compose <strong>across systems in one request</strong>, so an AI assistant can reason across the data, APIs, files, and code without learning every backend.</p></div>

{{< database-logos >}}

</section>

<section id="how" class="home-section"><div class="home-section-heading"><p class="home-section-label">How it works</p><h2>One compiler. Any system. Any client.</h2><p>Point GraphJin at databases, object storage, source trees, and remote APIs. It learns the shape, compiles one GraphQL surface, enforces RBAC, and gives AI assistants, REST clients, and federated routers the same production-safe engine.</p></div>
{{< svg "compiler-flow" "GraphJin compiler flow" >}}
</section>

<section id="agentic" class="home-section"><div class="home-section-heading"><p class="home-section-label">Agentic GraphJin</p><h2>Auto-learn, compile, govern, audit.</h2><p>GraphJin is the operating graph agents use to understand a real organization. It auto-learns the live surface, compiles GraphQL into database and source-backed work, and keeps policy visible enough for both humans and models to inspect.</p></div><div class="agentic-grid"><div class="agentic-loop"><div class="agentic-loop-line">gj_catalog -> evidence -> gj_security -> validate/preview -> governed action -> observe/refresh</div><div class="agentic-steps"><article><span>01 - Discover</span><h3>gj_catalog</h3><p>Find schemas, relationships, syntax, workflows, capabilities, examples, and evidence before choosing a path.</p></article><article><span>02 - Check</span><h3>gj_security</h3><p>Read effective policy and high-risk findings before config, workflow, file, code, or mutation actions.</p></article><article><span>03 - Validate</span><h3>preview</h3><p>Validate filters, inspect generated work, run approved workflows, or preview CodeSQL changes before applying.</p></article><article><span>04 - Act</span><h3>governed surface</h3><p>Execute through GraphQL, MCP, saved queries, workflows, and guarded source operations instead of raw credentials.</p></article></div></div><aside class="agentic-panel"><span class="home-section-label">One operating graph</span><h3>Not a pile of wrappers. Not a prompt full of guesses.</h3><p>The agent learns from graph evidence: catalog rows, security rows, relationships, code references, workflow metadata, config facts, validation results, preview diffs, and execution results.</p><div class="badge-row"><span>Application data</span><span>Remote APIs</span><span>Files and objects</span><span>Source code</span><span>Workflows</span><span>Config</span><span>Security posture</span></div></aside></div></section>

<section id="why" class="home-section"><div class="home-section-heading"><p class="home-section-label">Why GraphJin</p><h2>Built for the AI era, hardened for production.</h2><p>A compiler, not a query parser and not a resolver framework. It learns the live shape of your systems, plans the work, and emits optimized database operations. The result is calmer code, fewer round-trips, and a governed integration point where agents can explore without guessing.</p></div>
{{< svg "why-grid" "Why GraphJin" >}}
</section>

<section id="mcp" class="home-section feature-split"><div class="home-section-heading"><p class="home-section-label">AI integration</p><h2>A native MCP server that starts with discovery.</h2><p>GraphJin ships a Model Context Protocol server with the tools an assistant actually needs: catalog-first discovery, saved queries, where-clause validation, query repair, gj_security guidance, query execution, audit logs, and health checks.</p></div><div class="feature-section-grid"><div class="feature-copy"><p>One command adds GraphJin to Claude Desktop, Codex, or any MCP host. Tools are discoverable, narrow, and audited: agents search <code>gj_catalog</code>, inspect evidence and examples, validate filters, check <code>gj_security</code>, then run approved queries or workflows.</p><p>For development, <code>graphjin mcp</code> can wire local clients. For team access, run it as a hosted HTTP endpoint, gated by MCP OAuth or the same JWT/OIDC identity context as the main API.</p>
{{< svg "mcp-flow" "MCP discovery to governed action" >}}
</div><aside class="code-stack">
{{< code-card filename="terminal" >}}
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

<section id="security-model" class="home-section"><div class="home-section-heading"><p class="home-section-label">Security model</p><h2>Safer agents, not smaller agents.</h2><p>GraphJin makes agents safer by giving them explicit boundaries, not by making them blind. Agents can explore more of the live organization because policy, evidence, and action paths are inspectable and enforced.</p></div><div class="security-layout"><div class="security-copy"><article class="security-thesis"><h3>One config defines the AI surface.</h3><p>Humans can review and diff the policy. Models can inspect the same posture through <code>gj_catalog</code> and <code>gj_security</code> before acting. GraphJin enforces that policy across GraphQL, MCP, workflows, code, files, APIs, and databases.</p></article><div class="security-controls"><article><h3>One auditable config</h3><p>Databases, sources, roles, MCP settings, saved queries, mutations, read-only boundaries, and workflow access live in one policy artifact.</p></article><article><h3>Same auth everywhere</h3><p>HTTP, WebSocket, SSE, CLI, workflows, and MCP land in the same request context before GraphJin compiles or executes work.</p></article><article><h3>RBAC and row filters</h3><p>Roles, table permissions, column blocks, automatic filters, and mutation limits are enforced inside the compiler.</p></article><article><h3>Saved queries and allow-lists</h3><p>Production agents can run named, reviewed query contracts instead of inventing arbitrary operations at runtime.</p></article><article><h3>Read-only source boundaries</h3><p>Filesystems, CodeSQL, databases, and control-plane tables can expose discovery without granting writes.</p></article><article><h3>Preview before change</h3><p>CodeSQL change sets require file hashes, exact ranges, old text, optional locks, and a preview/apply loop.</p></article></div></div><aside class="code-stack security-query">
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

<section id="ai-queries" class="home-section ai-query-section"><div class="home-section-heading centered"><p class="home-section-label">AI-powered queries</p><h2>Ask in plain English. Get real data back.</h2><p>Claude Desktop, Codex, or any MCP client talks to GraphJin. GraphJin compiles the query, hits your database, and the assistant answers with rows it can reason over.</p></div><div class="ai-window"><div class="window-chrome"><span></span><span></span><span></span><i aria-hidden="true">✣</i><strong>Claude Desktop</strong></div><div class="ai-conversation"><div class="chat-user">who's the top customer?</div><div class="assistant-row"><div class="assistant-avatar" aria-hidden="true">✣</div><div class="chat-assistant"><div class="tool-call"><div class="tool-call-header"><span class="tool-caret" aria-hidden="true">⌄</span><span>execute_graphql</span></div><code>{ customers { id full_name email purchases { quantity product { price } } } }</code></div><p class="done-line"><span aria-hidden="true">✓</span> Done</p><p class="assistant-copy">Based on the purchase data, here are the top customers ranked by total spend:</p><div class="result-table" role="table" aria-label="Top customer results"><div role="row"><span>Rank</span><span>Customer</span><span>Email</span><span>Orders</span><span>Items</span><span>Total Spent</span></div><div role="row"><strong>01</strong><strong>Antwan Friesen</strong><span>francohirthe@medhurst.com</span><span>20</span><span>124</span><strong>$928.45</strong></div><div role="row"><strong>02</strong><span>Lon Cruickshank</span><span>margaretbailey@ruecker.info</span><span>20</span><span>94</span><span>$586.50</span></div><div role="row"><strong>03</strong><span>Susana Schaefer</span><span>jewelpowlowski@osinski.biz</span><span>20</span><span>91</span><span>$580.72</span></div></div><p class="assistant-summary">Antwan Friesen is the top customer with almost $1,000 in purchases, about 60% more than the runner-up.</p></div></div></div></div><div class="home-actions centered-actions"><a class="button-primary" href="#quickstart">Try it yourself</a></div></section>

<section id="codesql" class="home-section"><div class="home-section-heading"><p class="home-section-label">Code intelligence</p><h2>CodeSQL: query your code as well.</h2><p>GraphJin turns databases, HTTP APIs, discovered metadata, source code, and filesystems into one governed graph for AI agents. CodeSQL lets agents ask where a column exists, which code references it, which symbol owns that reference, and what guarded change set would update it.</p></div><div class="codesql-showcase" aria-label="CodeSQL turns source code into queryable tables"><div class="codesql-ask"><span>agent asks</span><strong>Where is users.email used?</strong><code>gj_code(where: { kind: { eq: "db_ref" }, name: { eq: "users.email" } }) { path symbol_name }</code></div><div class="codesql-map"><article class="codesql-source-card"><header><span>api/invoices.ts</span><em>source</em></header><pre><code>export async function createInvoiceHandler(req) {
  return workflows.run("sync-invoices")
}</code></pre></article><div class="codesql-lens"><span>CodeSQL</span><strong>tree-sitter + metadata</strong><em>read-only SQLite graph</em></div><div class="codesql-table-cloud"><span>gj_code</span><span>kind=file</span><span>kind=symbol</span><span>kind=db_ref</span><span>kind=doc</span></div><section class="codesql-results" aria-label="CodeSQL query results"><header>matched rows</header><div><span>symbol</span><strong>createInvoiceHandler</strong><em>api/invoices.ts</em></div><div><span>column ref</span><strong>users.email</strong><em>api/users.go</em></div><div><span>refresh</span><strong>dev live watch</strong><em>prod restart sync</em></div></section></div></div></section>

<section id="fstable" class="home-section feature-split"><div class="home-section-heading"><p class="home-section-label">Storage</p><h2>Files as queryable tables. Local, S3, or GCS.</h2><p>GraphJin streams multipart uploads straight to local disk, S3, Cloudflare R2, or Google Cloud Storage. Each backend exposes a virtual table: list, stat, get, put, delete, presign, and join with the rest of your schema.</p></div><div class="feature-section-grid"><div class="feature-copy"><p>Uploads follow the graphql-multipart-request-spec: send a single request, GraphJin parses, validates, signs, and persists. Returned rows include the storage URL and metadata, ready for the next mutation or a presigned download.</p><p>Bring your own bucket: GCS uses Application Default Credentials, S3 respects the standard AWS chain, and local writes go to a configured volume.</p>
{{< svg "filesystem-pipeline" "Filesystem upload pipeline" >}}
</div><aside class="code-stack">
{{< code-card filename="config/prod.yml" language="yaml" >}}
sources:
  - name: "media"
    kind: file
    backend: s3
    bucket: "graphjin-media"
    region: "us-east-1"
    prefix: "uploads/"
    capabilities:
      files.list: true
      files.read: true
      files.write: true

uploads:
  enabled: true
  storage: "media"
  storage_key_prefix: "avatars/{date}/"
  allowed_mime: ["image/*", "application/pdf"]
{{< /code-card >}}
{{< code-card filename="upload.graphql" language="graphql" >}}
mutation ($file: Upload!) {
  avatars(insert: { file: $file, user_id: $auth.user_id }) {
    id
    file_url
    file_size
    content_type
  }
}
{{< /code-card >}}
</aside></div></section>

<section id="openapi" class="home-section feature-split"><div class="home-section-heading"><p class="home-section-label">Remote APIs</p><h2>OpenAPI specs become first-class fields in your graph.</h2><p>Drop a Stripe, GitHub, or internal-service OpenAPI 3 spec into the config directory. GraphJin parses it, classifies the operations, and exposes them alongside your tables, joinable on any column to parameter mapping.</p></div><div class="feature-section-grid"><div class="feature-copy"><p>Auth is configured once per spec: bearer, basic, API key, OAuth2 client-credentials, or token-exchange. Tokens are cached transparently and concurrency caps per spec keep upstream rate limits respected.</p><p>Joins are declarative: tell GraphJin which column feeds which parameter and the result is a nested field, RBAC-aware, with the same compiler that generates your SQL planning the calls.</p>
{{< svg "openapi-flow" "OpenAPI remote join flow" >}}
</div><aside class="code-stack">
{{< code-card filename="config/openapi/stripe.yml" language="yaml" >}}
base_url: "https://api.stripe.com"

auth:
  scheme: bearer
  token_url: "https://api.stripe.com/v1/oauth/token"
  cache_ttl: "55m"

joins:
  - table: customers
    operation: listInvoices
    params:
      - column: stripe_customer_id
        param: customer
{{< /code-card >}}
{{< code-card filename="customer_with_invoices.graphql" language="graphql" >}}
query ($id: ID!) {
  customers(id: $id) {
    full_name
    email
    invoices {
      id
      total
      status
    }
  }
}
{{< /code-card >}}
</aside></div></section>

<section id="cli" class="home-section feature-split"><div class="home-section-heading"><p class="home-section-label">Tooling</p><h2>A CLI that fits the developer loop.</h2><p>One binary covers everything: a dev server with auto schema discovery, a database toolchain, a remote client that authenticates over OIDC device-code, and MCP connection helpers. No tokens to copy, no frameworks to learn.</p></div><div class="feature-section-grid"><div class="feature-copy"><div class="cli-tree" aria-label="GraphJin CLI command tree"><span>graphjin</span><span>serve --demo</span><span>db setup / migrate / seed</span><span>cli query / workflow / audit</span><span>mcp add codex / claude</span></div><p><code>graphjin serve --demo</code> starts a working example in seconds. <code>graphjin cli setup</code> opens the device-code login URL in your browser and persists a refreshable JWT for every subsequent command.</p><p>Every subcommand respects the same config, RBAC, and allow-list. What runs in CI matches what runs in production.</p></div><aside class="code-stack">
{{< code-card filename="dev" >}}
graphjin serve --demo
graphjin db setup
graphjin db migrate
graphjin db seed
{{< /code-card >}}
{{< code-card filename="prod" >}}
graphjin cli setup https://api.example.com
graphjin mcp add codex http://localhost:8080
graphjin cli query top_customers --limit 5
graphjin cli workflow customer_report
graphjin cli audit --since 1h
{{< /code-card >}}
</aside></div></section>

<section id="auth" class="home-section feature-split"><div class="home-section-heading"><p class="home-section-label">Security</p><h2>OAuth, JWT, OIDC, and row-level rules.</h2><p>JWT from Auth0, Firebase, Okta, or any JWKS endpoint. Header- or cookie-based sessions for legacy stacks. OIDC device-code login for the CLI, plus MCP OAuth for hosted AI clients.</p></div><div class="feature-section-grid"><div class="feature-copy"><p>Configure once. Every transport - HTTP, WebSocket, SSE, MCP - runs the same auth pipeline. Roles and row-level filters are authored in YAML and enforced inside the compiler.</p>
{{< svg "auth-modes" "GraphJin authentication modes" >}}
</div><aside class="code-stack">
{{< code-card filename="config/prod.yml" language="yaml" >}}
auth:
  type: jwt
  jwt:
    provider: "auth0"
    audience: "https://api.example.com"
    jwks_url: "https://example.auth0.com/.well-known/jwks.json"

auth_login:
  enabled: true
  provider: "https://login.example.com"
  client_id: "graphjin-cli"

mcp:
  oauth:
    enabled: true
    mode: builtin
{{< /code-card >}}
{{< code-card filename="config/roles.yml" language="yaml" >}}
roles:
  - name: user
    tables:
      orders:
        query:
          filters: ["{ user_id: { eq: $user_id } }"]
        insert:
          columns: [product_id, quantity]
{{< /code-card >}}
</aside></div></section>

<section id="subscriptions" class="home-section feature-split"><div class="home-section-heading"><p class="home-section-label">Realtime</p><h2>Live queries with cursors that survive reconnects.</h2><p>Subscribe with the same GraphQL you would use for a query. GraphJin streams deltas over SSE or WebSockets, batches database polls into one statement, and emits cursors so clients can resume after a network hiccup.</p></div><div class="feature-section-grid"><div class="feature-copy"><p>The subscription API is just queries with a cursor: no new schema, no resolver tree, no pub/sub bus to operate. Cursor-based pagination keeps feeds and chat-style UIs deterministic.</p>
{{< svg "subscriptions-transport" "Subscriptions transport" >}}
</div><aside class="code-stack">
{{< code-card filename="live_orders.graphql" language="graphql" >}}
subscription LiveOrders($since: Cursor) {
  orders(
    where: { status: { eq: "open" } }
    after: $since
    first: 50
    order_by: { id: asc }
  ) {
    id
    total
    customer { id full_name }
    cursor
  }
}
{{< /code-card >}}
{{< code-card filename="config/prod.yml" language="yaml" >}}
subs_poll_duration: "2s"
subs_max_clients: 10000

http:
  sse: true
  websocket: true
{{< /code-card >}}
</aside></div></section>

<section id="features" class="home-section"><div class="home-section-heading"><p class="home-section-label">Features</p><h2>Everything a governed AI surface needs.</h2><p>One binary, one config file: compiler, catalog, MCP, auth, workflows, CodeSQL, subscriptions, and a CLI. The agent sees a map; the organization keeps the controls.</p></div><div class="stats-grid"><article><strong>1</strong><span>Auditable config for agent access across the AI surface.</span></article><article><strong>12+</strong><span>Database and warehouse engines through one GraphQL surface.</span></article><article><strong>0</strong><span>Lines of resolver code. The compiler does the work.</span></article></div><div class="home-card-grid feature-grid-large"><article class="home-card"><div class="home-card-icon">C</div><h3>Catalog discovery spine</h3><p>Agents discover tables, columns, relationships, syntax, workflows, and safety notes through gj_catalog.</p></article><article class="home-card"><div class="home-card-icon">IR</div><h3>Compiler engine</h3><p>GraphQL compiles into optimized database work, with cross-database composition when sources allow it.</p></article><article class="home-card"><div class="home-card-icon">S</div><h3>Security posture graph</h3><p>gj_security exposes policy rows and findings so agents can check risk before write-capable actions.</p></article><article class="home-card"><div class="home-card-icon">RT</div><h3>Live subscriptions</h3><p>SSE and WebSocket transports with cursor-based resume.</p></article><article class="home-card"><div class="home-card-icon">WF</div><h3>Governed workflows</h3><p>Discover approved workflows, inspect variable contracts, and execute through GraphQL, REST, MCP, or CLI.</p></article><article class="home-card"><div class="home-card-icon">RO</div><h3>Read-only replicas</h3><p>Lock a database, filesystem, CodeSQL source, or control surface to query-only with config.</p></article><article class="home-card"><div class="home-card-icon">API</div><h3>Remote API joins</h3><p>Stitch in REST and GraphQL endpoints alongside your tables.</p></article><article class="home-card"><div class="home-card-icon">CS</div><h3>CodeSQL preview/apply</h3><p>Source edits use hashes, exact ranges, old text, optional locks, and preview diffs before apply.</p></article><article class="home-card"><div class="home-card-icon">YML</div><h3>Auditable config</h3><p>One YAML surface defines roles, sources, saved queries, MCP permissions, and read-only boundaries.</p></article></div></section>

<section id="quickstart" class="home-section"><div class="home-section-heading"><p class="home-section-label">Quickstart</p><h2>Run it in under a minute.</h2><p>Pick your platform, copy the command, and you are querying. The demo flag ships a real schema and example queries so there is something to point an AI client at on the very first run.</p></div><div class="quickstart-grid">
{{< code-card filename="npx" >}}
npx graphjin serve --demo
{{< /code-card >}}
{{< code-card filename="macOS" >}}
brew install dosco/graphjin/graphjin
{{< /code-card >}}
{{< code-card filename="curl" >}}
curl -fsSL https://graphjin.com/install.sh | bash
{{< /code-card >}}
</div><div class="mcp-command-panel"><h3>Wire it into your AI client</h3><div class="quickstart-grid">
{{< code-card filename="OpenAI Codex" >}}
graphjin mcp add codex
{{< /code-card >}}
{{< code-card filename="Claude Code" >}}
graphjin mcp add claude
{{< /code-card >}}
</div></div></section>

<section id="get-started" class="home-section"><div class="home-section-heading"><p class="home-section-label">Get started</p><h2>Two paths. Both end with queries running.</h2></div><div class="home-card-grid"><article class="home-card"><div class="home-card-icon">DB</div><h3>Existing database</h3><p>Configure the connection, let GraphJin introspect tables, columns, and relationships on boot, then start querying joins, mutations, subscriptions, federation, and MCP.</p></article><article class="home-card"><div class="home-card-icon">DDL</div><h3>Start fresh</h3><p>Describe the schema you need, preview and apply GraphJin DDL changes, then query immediately as the schema reloads.</p></article><article class="home-card"><div class="home-card-icon">DOC</div><h3>Go deep</h3><p>Move from install to saved production operations, role policy, MCP, multi-source configuration, and test-backed references.</p></article></div><div class="home-actions"><a class="button-primary" href="/start/quick-start/">Read the quick start</a><a class="button-secondary" href="/reference/test-backed-examples/">Test-backed examples</a></div></section>

<section id="federation" class="home-section feature-split"><div class="home-section-heading"><p class="home-section-label">Advanced - Supergraph</p><h2>Drop GraphJin into a federated supergraph.</h2><p>Already running Apollo Router, Cosmo, or Hive? Flip one config flag and every primary-keyed table becomes a federation v2 subgraph: SDL with @key, @shareable, and @inaccessible directives, plus a working _service entry point.</p></div><div class="feature-section-grid">
{{< code-card filename="config/prod.yml" language="yaml" >}}
federation:
  enabled: true
  version: v2.5
  keys:
    users: "id"
    products: "sku"
{{< /code-card >}}
<ul class="federation-list"><li>Generated SDL refreshes on schema change.</li><li>Per-table key overrides with field-level @shareable and @inaccessible.</li><li>Multiple GraphJin processes compose into one supergraph.</li><li>Same RBAC and allow-lists apply to entity references.</li></ul></div></section>
