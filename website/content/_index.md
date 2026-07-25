---
title: "GraphJin"
description: "One highly reactive AI agent across your databases, APIs, files, and code. Ask questions—or leave governed questions running to notice changes, silence, and risk."
---

<section class="home-hero"><div class="home-hero-copy"><p class="home-section-label">v3 · Apache 2.0</p><h1>One AI agent. All your databases and code. <span class="h1-accent">Ask anything. Instant answers. No hallucinations.</span></h1><p class="home-lede">Ask questions across your whole company. GraphJin finds the right data, runs the query, and shows the evidence.</p><div class="home-actions"><a class="button-primary" href="#quickstart">Run the 2-minute demo</a></div></div><div class="home-hero-demo"><div class="ai-window ai-window-hero" tabindex="0" aria-label="GraphJin agent example. Focus to pause rotation."><div class="window-chrome"><span></span><span></span><span></span><i aria-hidden="true">✣</i><strong>GraphJin Agent · localhost:8083/agent</strong></div><div class="ai-conversation"><div class="chat-user">Which account is most at risk of churn—and why?</div><div class="assistant-row"><div class="assistant-avatar" aria-hidden="true">✣</div><div class="chat-assistant hero-trace"><div class="tool-call"><div class="tool-call-header"><span class="tool-caret" aria-hidden="true">⌄</span><span>query_catalog</span></div><code>3 relevant sources discovered</code><div class="hero-source-list" aria-label="Sources discovered"><span><strong>Postgres</strong> accounts + invoices</span><span><strong>Snowflake</strong> product usage</span><span><strong>CRM API</strong> opportunities</span></div></div><div class="tool-call"><div class="tool-call-header"><span class="tool-caret" aria-hidden="true">⌄</span><span>validate_where_clause</span></div><code>relationships + churn filters valid</code></div><div class="tool-call"><div class="tool-call-header"><span class="tool-caret" aria-hidden="true">⌄</span><span>execute_graphql</span></div><code>3 optimized, policy-checked operations</code></div><p class="done-line"><span aria-hidden="true">✓</span><span class="done-text"> Evidence checked · 3 systems · 3 operations</span></p><p class="assistant-copy">Meridian Robotics — renewal in 9 days, usage down 38%, two failed payments, and an unresolved escalation.</p></div></div></div></div><script type="application/json" id="hero-rotate-data">[{"user":"Which account is most at risk of churn—and why?","sources":[{"name":"Postgres","detail":"accounts + invoices"},{"name":"Snowflake","detail":"product usage"},{"name":"CRM API","detail":"opportunities"}],"tools":[{"name":"query_catalog","line":"3 relevant sources discovered"},{"name":"validate_where_clause","line":"relationships + churn filters valid"},{"name":"execute_graphql","line":"3 optimized, policy-checked operations"}],"done":"Evidence checked · 3 systems · 3 operations","answer":"Meridian Robotics — renewal in 9 days, usage down 38%, two failed payments, and an unresolved escalation."},{"user":"Which support issues are about to breach SLA?","sources":[{"name":"Postgres","detail":"accounts + owners"},{"name":"Ticket API","detail":"open escalations"},{"name":"Filesystem","detail":"SLA policy"}],"tools":[{"name":"query_catalog","line":"3 relevant sources discovered"},{"name":"validate_where_clause","line":"priority + deadline filters valid"},{"name":"execute_graphql","line":"3 optimized, policy-checked operations"}],"done":"Evidence checked · 3 systems · 3 operations","answer":"Meridian Robotics has one urgent escalation past SLA and another due by 17:00 today."},{"user":"Where is revenue concentrated—and what is at risk?","sources":[{"name":"Postgres","detail":"subscriptions"},{"name":"MySQL","detail":"invoices + renewals"},{"name":"Snowflake","detail":"product usage"}],"tools":[{"name":"query_catalog","line":"3 relevant sources discovered"},{"name":"validate_where_clause","line":"revenue + renewal filters valid"},{"name":"execute_graphql","line":"3 optimized, policy-checked operations"}],"done":"Evidence checked · 3 systems · 3 operations","answer":"81% of MRR is enterprise, with $5,200 exposed across two past-due accounts whose usage is declining."}]</script></div></section>

<section id="deployment-proof" class="deployment-proof" aria-labelledby="deployment-proof-title"><div class="deployment-proof-inner"><h2 id="deployment-proof-title" class="home-section-label">Deployed at scale</h2><figure><blockquote><p>“I deployed GraphJin at a large Silicon Valley company, where it works across Snowflake, Postgres, and other databases spanning <strong>5,000+ tables</strong> and <strong>100 billion rows</strong>. It also connects to the company’s sales, marketing, and other SaaS APIs.”</p></blockquote><figcaption><img src="/assets/amit-deshmukh.webp" width="160" height="160" alt="" loading="lazy" decoding="async"><div><strong>Amit Deshmukh</strong><span>Forward Deployed Engineer, <a href="https://openneko.app/" rel="external" target="_blank">OpenNeko</a></span></div></figcaption></figure></div></section>

<section id="ai-queries" class="home-section ai-query-section"><div class="home-section-heading centered"><p class="home-section-label">See it work</p><h2>Ask in plain English. Get real data back.</h2><p>Ask a question. Watch GraphJin discover the relevant shape, validate the request, compile the operation, and return the rows behind the answer.</p></div><div class="ai-stage"><div class="ai-window"><div class="window-chrome"><span></span><span></span><span></span><i aria-hidden="true">✣</i><strong>AI Assistant</strong></div><div class="ai-conversation"><div class="chat-user">who's the top customer?</div><div class="assistant-row"><div class="assistant-avatar" aria-hidden="true">✣</div><div class="chat-assistant"><div class="tool-call"><div class="tool-call-header"><span class="tool-caret" aria-hidden="true">⌄</span><span>query_catalog</span></div><code>discover -> customers · purchases · products  (2 relationships)</code></div><div class="tool-call"><div class="tool-call-header"><span class="tool-caret" aria-hidden="true">⌄</span><span>validate_where_clause</span></div><code>validate -> filters ok on customers, purchases · order_by total_spent</code></div><div class="tool-call"><div class="tool-call-header"><span class="tool-caret" aria-hidden="true">⌄</span><span>execute_graphql</span></div><code>{ customers { id full_name email purchases { quantity product { price } } } }</code></div><details class="gen-sql"><summary><span aria-hidden="true">SQL</span> one optimized query, no N+1, no resolvers</summary>{{< hl sql >}}SELECT json_agg(__sj.json) AS customers
FROM customers AS c
LEFT JOIN LATERAL (
  SELECT sum(p.quantity * pr.price) AS total_spent
  FROM purchases p
  JOIN products pr ON pr.id = p.product_id
  WHERE p.customer_id = c.id
) __agg ON true
ORDER BY __agg.total_spent DESC NULLS LAST
LIMIT 5;{{< /hl >}}</details><p class="done-line"><span aria-hidden="true">✓</span> Done</p><p class="assistant-copy">Based on the purchase data, here are the top customers ranked by total spend:</p><div class="result-table" role="table" aria-label="Top customer results"><div role="row"><span>Rank</span><span>Customer</span><span>Email</span><span>Orders</span><span>Items</span><span>Total Spent</span></div><div role="row"><strong>01</strong><strong>Antwan Friesen</strong><span>francohirthe@medhurst.com</span><span>20</span><span>124</span><strong>$928.45</strong></div><div role="row"><strong>02</strong><span>Lon Cruickshank</span><span>margaretbailey@ruecker.info</span><span>20</span><span>94</span><span>$586.50</span></div><div role="row"><strong>03</strong><span>Susana Schaefer</span><span>jewelpowlowski@osinski.biz</span><span>20</span><span>91</span><span>$580.72</span></div></div><p class="assistant-summary">Antwan Friesen is the top customer with almost $1,000 in purchases, about 60% more than the runner-up.</p></div></div></div></div></div><div class="home-actions centered-actions"><a class="button-primary" href="#quickstart">Try it yourself</a></div></section>

<section id="why-it-works" class="home-section why-it-works-section"><div class="home-section-heading"><p class="home-section-label">How it works</p><h2>Why this still works at 5,000 tables.</h2><p>GraphJin’s reasoning-with-code agent discovers only the schemas and relationships it needs, then writes compact, model-friendly dynamic GraphQL. The compiler turns that into optimized, policy-checked operations—keeping context small enough for smaller models, permissions enforced, and every answer backed by evidence from what actually ran.</p><p>Bring Claude or Codex over MCP, or use the <a href="#agent">built-in agent</a>.</p></div></section>

<section id="problem" class="home-section"><div class="home-section-heading"><p class="home-section-label">The problem</p><h2>Your agent is only as good as what it can see.</h2><p>$2.5 trillion goes into AI this year — and a capable model still enters your stack blind. It doesn't know your schema, your permissions, your saved queries, or where a field is written in code — so it works from memory and guesses.</p></div>
{{< svg "problem-blind-agent" "An AI agent guessing at disconnected systems" >}}
<div class="home-card-grid"><article class="home-card"><div class="stat-figure">$2.5T<sup><a href="#sources">1</a></sup></div><h3>A tool for every question</h3><p>Worldwide AI spend in 2026 — yet teams still hand-write a brittle API or MCP tool for each thing an agent might ask. The surface never keeps up, and every new question is new glue code.</p></article><article class="home-card"><div class="stat-figure">21%<sup><a href="#sources">2</a></sup></div><h3>Guesses, not facts</h3><p>The best model's success rate on real enterprise text-to-SQL — down from 91% on academic benchmarks. Without a map, agents invent joins, fake fields, and confuse API shape with database shape.</p></article><article class="home-card"><div class="stat-figure">88%<sup><a href="#sources">3</a></sup></div><h3>Too risky for production</h3><p>Organizations that saw an AI-agent security incident in the past year. Handing an agent raw credentials means hoping it guesses right — so it stays read-only, shallow, or boxed out.</p></article></div><p class="home-section-note"><strong>The fix isn't a smarter prompt.</strong> It's giving the agent the map — and the guardrails. And it isn't another raw connector: 43% of tested MCP servers shipped with command-injection flaws.<sup><a href="#sources">4</a></sup></p><p class="home-sources" id="sources">Sources: <a href="https://www.gartner.com/en/newsroom/press-releases/2026-1-15-gartner-says-worldwide-ai-spending-will-total-2-point-5-trillion-dollars-in-2026" rel="nofollow">1. Gartner, Jan 2026</a> · <a href="https://arxiv.org/abs/2411.07763" rel="nofollow">2. Spider 2.0, ICLR 2025</a> · <a href="https://www.gravitee.io/state-of-ai-agent-security" rel="nofollow">3. Gravitee State of AI Agent Security, 2026</a> · <a href="https://equixly.com/blog/2025/03/29/mcp-server-new-security-nightmare/" rel="nofollow">4. Equixly, 2025</a></p></section>

<section id="what" class="home-section only-graphjin-section"><div class="home-section-heading"><p class="home-section-label">Only GraphJin</p><h2>The only system that lets AI agents understand your whole organization.</h2></div>

<figure class="company-system-map" aria-labelledby="company-system-question"><figcaption id="company-system-question"><span>One question</span><strong>“Which customers are at renewal risk — and why?”</strong></figcaption><div class="company-system-layers"><section class="company-system-layer organization-layer" aria-labelledby="organization-layer-title"><div class="company-layer-heading"><span>01</span><h3 id="organization-layer-title">Your systems</h3></div><div class="company-scale-row" aria-label="Organization scale"><div><strong>12+</strong><span>engines</span></div><div><strong>1,000s</strong><span>of tables</span></div><div><strong>100s</strong><span>of columns</span></div></div><p class="company-source-line">Postgres · Snowflake · MySQL · APIs · Files · Code</p></section><div class="company-map-connector" aria-hidden="true"><span>→</span></div><section class="company-system-layer graphjin-system-layer" aria-labelledby="graphjin-layer-title"><div class="company-layer-heading"><span>02</span><h3 id="graphjin-layer-title">GraphJin</h3></div><ol class="graphjin-system-steps"><li><strong>Discover</strong></li><li><strong>Connect</strong></li><li><strong>Run</strong></li><li><strong>Check</strong></li></ol></section><div class="company-map-connector" aria-hidden="true"><span>→</span></div><section class="company-system-layer outcome-layer" aria-labelledby="outcome-layer-title"><div class="company-layer-heading"><span>03</span><h3 id="outcome-layer-title">Agent answer</h3></div><ul class="company-outcome-list"><li>Who is at risk</li><li>Why</li><li>What can happen next</li></ul><p class="company-outcome-result">Answer · data · evidence · actions</p></section></div></figure>
</section>

<section id="agent" class="home-section"><div class="home-section-heading"><p class="home-section-label">Built-in agent</p><h2>Use GraphJin as the agent.</h2><p>Send one plain-English instruction to one endpoint. GraphJin returns the answer, the data behind it, and evidence from what ran.</p></div><div class="agent-request-response">
{{< code-card filename="terminal" language="bash" >}}
curl -sS localhost:8080/api/v1/agent \
  -H 'content-type: application/json' \
  -d '{"instruction": "What should we prioritize next?"}'
{{< /code-card >}}
{{< code-card filename="response.json" language="json" >}}
{
  "status": "answered",
  "answer": "Start the Northstar run — priority 1, largest volume.",
  "data": [
    { "product": "Northstar Blend 340g", "bags": 420 }
  ],
  "evidence": {
    "protocol": {
      "executions": [{ "has_data": true }]
    }
  }
}
{{< /code-card >}}
</div><div class="home-actions"><a class="button-primary" href="/agentic/server-agent/">Run the built-in agent</a></div></section>

<section id="watch-automation" class="home-section watch-automation-section" data-watch-story>
  <div class="home-section-heading">
    <p class="home-section-label">Highly reactive agents</p>
    <h2>The agent that notices changes—and silence.</h2>
    <p>GraphJin keeps governed questions running across your live systems. It detects what changed, notices what failed to happen, distills noise, correlates signals, and wakes the right conversation—or proposes an approved action.</p>
  </div>

  <div class="watch-capability-grid" aria-label="GraphJin reactive agent capabilities">
    <article><span>Change</span><strong>React to live data</strong><p>Cursor-backed questions keep running across your systems.</p></article>
    <article><span>Absence</span><strong>Notice what did not happen</strong><p>“No shipment scan in four hours” becomes a first-class event.</p></article>
    <article><span>Digest</span><strong>Turn noise into one signal</strong><p>Routine events drain into one useful, unseen summary.</p></article>
    <article><span>Rollup</span><strong>Correlate independent watches</strong><p>Combine exact watch IDs without unsafe loops.</p></article>
    <article><span>Snooze</span><strong>Defer without losing the event</strong><p>Hide an unseen event until later without acknowledging it.</p></article>
    <article><span>Recover</span><strong>Reconnect, resume, retry</strong><p>Persist cursors and back off transient failures automatically.</p></article>
  </div>

  <div class="watch-showcase" aria-label="Shipment silence and low inventory correlated by GraphJin">
    <section class="watch-showcase-column watch-input-column" data-watch-stage="1" aria-labelledby="watch-input-title">
      <div class="watch-stage-label"><span>01</span><strong id="watch-input-title">Watch change and silence</strong></div>
      <article class="watch-signal-card">
        <div class="watch-signal-heading"><span class="watch-signal-dot watch-signal-dot-roast" aria-hidden="true"></span><strong>Green-bean inventory</strong><span>change</span></div>
        <code>green_lots · below safety buffer</code>
        <p>A cursor-backed watch records the live inventory change.</p>
      </article>
      <article class="watch-signal-card">
        <div class="watch-signal-heading"><span class="watch-signal-dot watch-signal-dot-absence" aria-hidden="true"></span><strong>Shipment scans</strong><span>silent</span></div>
        <code>no scan · 4h absence window</code>
        <p>Silence becomes evidence instead of disappearing between polls.</p>
      </article>
    </section>

    <div class="watch-showcase-arrow" aria-hidden="true">→</div>

    <section class="watch-showcase-column watch-control-column" data-watch-stage="2" aria-labelledby="watch-control-title">
      <div class="watch-stage-label"><span>02</span><strong id="watch-control-title">Distill and correlate</strong></div>
      <ol class="watch-control-stack">
        <li><span>Watch</span><strong>Detect change + absence</strong></li>
        <li><span>Flow</span><strong>discard · digest · notify</strong></li>
        <li><span>Rollup</span><strong>Correlate exact watch IDs</strong></li>
        <li><span>Route</span><strong>Wake the exact resource</strong></li>
      </ol>
      <div class="watch-verdict">
        <div><span>rollup</span><span>absence</span><span>critical</span></div>
        <strong>Shipment activity is silent while inventory is below buffer.</strong>
        <p>Two independent streams become one governed operational signal.</p>
      </div>
    </section>

    <div class="watch-showcase-arrow" aria-hidden="true">→</div>

    <section class="watch-showcase-column watch-outcome-column" data-watch-stage="3" aria-labelledby="watch-outcome-title">
      <div class="watch-stage-label"><span>03</span><strong id="watch-outcome-title">Wake or act safely</strong></div>
      <article class="watch-conversation-card"><span>shipping watch only</span><strong>Shipping conversation</strong><p>No shipment scan has arrived in four hours.</p></article>
      <article class="watch-conversation-card"><span>operations rollup only</span><strong>Operations conversation</strong><p>Shipment silence now threatens the inventory buffer.</p></article>
      <aside class="watch-action-gate">
        <div><strong>Draft supplier escalation</strong><span>approval pending</span></div>
        <code>action_hash · exact version</code>
        <p>No workflow runs until this exact action is approved.</p>
      </aside>
    </section>
  </div>

  <div class="watch-reactive-proof">
    <p><strong>It remembers.</strong> Durable watches persist cursor checkpoints, reconnect after drops, and exponentially back off transient failures.</p>
    <p><strong>It respects attention.</strong> Digest noisy events, route by exact watch ID, or snooze one without marking it seen.</p>
  </div>
  <p class="watch-showcase-note"><strong>Alerts fail open.</strong> If AI triage is unavailable, GraphJin still sends the raw notification. <strong>Actions fail closed.</strong> A workflow never runs without the required approval.</p>
  <div class="home-actions"><a class="button-primary" href="/agentic/watch-automation/">Explore the reactive agent</a><a class="button-secondary" href="/start/demos/#coffee-roastery">Run the coffee demo</a></div>
</section>

<section id="proof" class="home-section"><div class="home-section-heading"><p class="home-section-label">Answers backed by evidence</p><h2>The model made up an answer. The ledger caught it.</h2><p>If no query ran, GraphJin will not let the model pretend that one did.</p></div><div class="agent-stage"><div class="proof-beats"><article><span>01 — The claim</span><h3>The model answers anyway</h3><p>It names roast batches to hold.</p></article><article><span>02 — The evidence</span><h3>No query ran</h3><p>The execution ledger has no rows behind the claim.</p></article><article><span>03 — The result</span><h3>GraphJin blocks it</h3><p>The invented answer never reaches the caller.</p></article></div><div class="code-stack">
{{< code-card filename="response.json" language="json" >}}
{
  "status": "blocked",
  "refusal": {
    "code": "saved_query_detail_required",
    "blocked_action": "execute_saved_query",
    "unblock": [
      {
        "tool": "query_catalog"
      }
    ],
    "retryable": true
  }
}
{{< /code-card >}}
</div></div><div class="home-actions"><a class="button-primary" href="/agentic/server-agent/">See how the guard works</a></div></section>

<section id="demos" class="home-section"><div class="home-section-heading"><p class="home-section-label">Try GraphJin</p><h2>Your first answer is one command away.</h2><p>Choose a demo, run the command, and ask a question. The data and agent are ready to go.</p></div><div class="demo-grid"><article class="home-card demo-card"><h3>Coffee roastery</h3><p class="demo-question">"Which roast batch should be held for quality — and why?"</p><code class="demo-cmd">graphjin serve --demo --path examples/coffee-roastery</code></article><article class="home-card demo-card"><h3>SaaS ops</h3><p class="demo-question">"Which account is most at risk of churning?"</p><code class="demo-cmd">graphjin serve --demo</code></article><article class="home-card demo-card"><h3>Corrugated plant</h3><p class="demo-question">"Which work orders should the corrugator run first?"</p><code class="demo-cmd">graphjin serve --demo --path examples/corrugated-plant</code></article><article class="home-card demo-card"><h3>PCB fab</h3><p class="demo-question">"Which fab order should we release next — and what's the evidence?"</p><code class="demo-cmd">graphjin serve --demo --path examples/pcb-fab</code></article></div><div class="home-actions"><a class="button-primary" href="/start/demos/">Try a demo</a></div></section>

<section id="databases" class="home-section database-section"><div class="database-copy"><p class="home-section-label">Supported systems</p><h2>12+ database engines.<br>APIs, files, and code too.</h2><p>Postgres, MySQL, Snowflake, Redshift, BigQuery, MongoDB, Oracle, SQL Server, SQLite, Cassandra and Keyspaces — plus remote HTTP APIs, object storage, filesystems, and CodeSQL source trees. Mix <strong>as many sources as your deployment needs</strong> behind one GraphQL and MCP surface.</p></div>

{{< database-logos >}}

</section>

<section id="how" class="home-section"><div class="home-section-heading"><p class="home-section-label">How it works</p><h2>One compiler. Any system. Any client.</h2><p>Point GraphJin at databases, object storage, source trees, and remote APIs. It's a compiler, not a resolver framework: it learns the live shape, plans the work, and emits one optimized database operation — then enforces RBAC and serves AI assistants, REST clients, and federated routers from the same engine. No N+1, no resolver code, test-backed against real compiler paths.</p></div>
{{< svg "compiler-flow" "GraphJin compiler flow" >}}
</section>

<section id="agentic" class="home-section"><div class="home-section-heading"><p class="home-section-label">The agent loop</p><h2>Discover, check, validate, act.</h2><p>A checked path from question to action.</p></div><div class="agentic-loop"><div class="agentic-steps"><article><span>01</span><h3>Discover</h3><code>gj_catalog</code></article><article><span>02</span><h3>Check</h3><code>gj_security</code></article><article><span>03</span><h3>Validate</h3><code>preview</code></article><article><span>04</span><h3>Act</h3><code>governed</code></article></div></div></section>

<section id="security-model" class="home-section"><div class="home-section-heading"><p class="home-section-label">Security model</p><h2>Safer agents, not smaller agents.</h2><p>One policy controls what agents can see, query, and change.</p></div><div class="security-layout"><ul class="security-controls"><li>RBAC and row filters</li><li>Saved queries and allow-lists</li><li>Read-only source boundaries</li><li>Preview before change</li></ul><aside class="code-stack security-query">
{{< code-card filename="policy-before-action.graphql" language="graphql" >}}
query {
  policy: gj_security(id: "summary") {
    mode
    summary
  }

  blockers: gj_security(
    where: { severity: { in: ["high", "critical"] } }
  ) {
    title
    recommendation
  }
}
{{< /code-card >}}
<p class="code-note">Checked before every action.</p></aside></div></section>

<section id="mcp" class="home-section feature-split"><div class="home-section-heading"><p class="home-section-label">AI integration</p><h2>Connect any agent over MCP, in one command.</h2><p>Add GraphJin to Codex or Claude, then start asking questions.</p></div><div class="feature-section-grid"><div class="feature-copy">
{{< svg "mcp-flow" "MCP discovery to governed action" >}}
</div><aside class="code-stack">
{{< code-card filename="terminal" language="bash" >}}
graphjin mcp add codex
graphjin mcp add claude
{{< /code-card >}}
</aside></div></section>

<section id="codesql" class="home-section"><div class="home-section-heading"><p class="home-section-label">Code intelligence</p><h2>CodeSQL: query your code as well.</h2><p>CodeSQL indexes your source tree into a read-only SQLite graph. Agents can ask where a column is used, which code references it, which symbol owns that reference, and what guarded change set would update it — all through the same GraphQL interface.</p></div><div class="codesql" aria-label="Querying source code with CodeSQL"><div class="codesql-head"><span class="codesql-kicker">agent asks</span><strong>Where is <code>users.email</code> used?</strong></div><div class="codesql-grid"><figure class="codesql-pane codesql-pane-query"><figcaption>GraphQL · gj_code</figcaption>{{< hl graphql >}}query {
  gj_code(where: {
    name: { eq: "users.email" }
    kind: { eq: "db_ref" }
  }) {
    path
    symbol_name
  }
}{{< /hl >}}</figure><div class="codesql-pane codesql-pane-rows"><div class="codesql-rows-head"><span>matched rows</span><em>3</em></div><div class="codesql-rowset"><div class="codesql-row"><span class="codesql-path">api/users.go</span><span class="codesql-sym">getUser</span></div><div class="codesql-row"><span class="codesql-path">api/invoices.ts</span><span class="codesql-sym">createInvoiceHandler</span></div><div class="codesql-row"><span class="codesql-path">graph/schema.go</span><span class="codesql-sym">userType</span></div></div></div></div><div class="codesql-foot"><span>tree-sitter → read-only SQLite</span><div class="codesql-kinds"><code>kind=file</code><code>kind=symbol</code><code>kind=db_ref</code><code>kind=doc</code></div></div></div></section>

<section id="capabilities" class="home-section"><div class="home-section-heading"><p class="home-section-label">In the box</p><h2>A full backend, not just an agent gateway.</h2><p>The same binary and config that govern the AI surface also run your realtime, files, remote APIs, auth, and federation — no extra services to operate.</p></div><div class="stats-grid"><article><strong>1</strong><span>Auditable config for agent access across the AI surface.</span></article><article><strong>12+</strong><span>Database and warehouse engines through one GraphQL surface.</span></article><article><strong>0</strong><span>Lines of resolver code. The compiler does the work.</span></article></div><div class="home-card-grid feature-grid-large"><article class="home-card"><div class="home-card-icon">FS</div><h3>Files as tables</h3><p>Uploads stream to local disk, S3, R2, or GCS; each bucket is a queryable table you can join with the rest of your schema.</p></article><article class="home-card"><div class="home-card-icon">API</div><h3>Remote APIs</h3><p>Drop in an OpenAPI 3 spec and its operations become joinable, RBAC-aware fields with per-spec auth caching.</p></article><article class="home-card"><div class="home-card-icon">RT</div><h3>Realtime</h3><p>Subscribe with the same GraphQL; cursor-based SSE and WebSocket streams resume after a drop, with polls batched into one statement.</p></article><article class="home-card"><div class="home-card-icon">JWT</div><h3>Authentication</h3><p>JWT and OIDC from Auth0, Firebase, Okta, or any JWKS — one auth pipeline across HTTP, WebSocket, SSE, and MCP.</p></article><article class="home-card"><div class="home-card-icon">WF</div><h3>Workflows</h3><p>Discover approved workflows and run them through GraphQL, REST, MCP, or the CLI.</p></article><article class="home-card"><div class="home-card-icon">RDS</div><h3>Caching</h3><p>Response caching on Redis with an in-memory fallback and stale-while-revalidate.</p></article><article class="home-card"><div class="home-card-icon">CLI</div><h3>One binary CLI</h3><p>Dev server, database toolchain, device-code login, and MCP wiring. What runs in CI matches production.</p></article><article class="home-card"><div class="home-card-icon">FED</div><h3>Federation</h3><p>Advanced: flip a flag and every keyed table becomes an Apollo Federation v2 subgraph.</p></article></div></section>

<nav class="home-paths" aria-label="Choose your path"><a href="/start/install/"><strong>Install</strong><span>Run GraphJin locally and scaffold a project.</span></a><a href="/start/demos/"><strong>Demos</strong><span>Five runnable verticals — coffee, SaaS ops, corrugated, PCB fab, webshop — one command each.</span></a><a href="/story/vision/"><strong>Vision</strong><span>Why GraphJin treats data, APIs, files, code, workflows, and policy as one governed graph.</span></a><a href="/core/query-language/"><strong>Query language</strong><span>Filters, geo filters, relationships, cursors, Expression aggregates.</span></a><a href="/agentic/mcp/"><strong>MCP for agents</strong><span>Catalog-first discovery, validation, execution, and policy checks.</span></a><a href="/agentic/server-agent/"><strong>Built-in agent</strong><span>Send an instruction and get back the answer, data, and evidence.</span></a><a href="/configure/sources-mode/"><strong>Sources mode</strong><span>Databases, filesystems, code, remote APIs, and capabilities.</span></a><a href="/reference/test-backed-examples/"><strong>Examples</strong><span>Feature map tied back to the Go example tests.</span></a><a href="/reference/config-reference/"><strong>Config reference</strong><span>Production, auth, caching, uploads, federation, OpenAPI.</span></a></nav>

<section id="quickstart" class="home-section"><div class="home-section-heading"><p class="home-section-label">Get started</p><h2>Run it in two minutes.</h2><p>Install GraphJin, add your model key, and start the demo.</p></div><div class="quickstart-simple">
{{< code-card filename="terminal" language="bash" >}}
# Install GraphJin
curl -fsSL https://graphjin.com/install.sh | bash

# Start the demo with your model key
OPENAI_API_KEY="your-key" graphjin serve --demo
{{< /code-card >}}
<p class="quickstart-next"><span aria-hidden="true">→</span> Open <code>localhost:8083/agent</code> and ask your first question.</p></div><div class="home-actions"><a class="button-primary" href="/start/quick-start/">Read the quick start</a></div></section>
