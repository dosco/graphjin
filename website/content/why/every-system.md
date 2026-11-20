---
title: "Every system you run"
headline: "Every system you already run, behind one agent."
description: "DeepORG connects databases, warehouses, HTTP APIs, files and source code into one map the agent can search, so one question can draw on several of them. Adding a system is a few lines of config."
lede: "Databases, warehouses, HTTP APIs, files and source code. Each becomes part of one map the agent can search, and one question can draw on several of them."
scene: "sources"
scene_label: "Animated diagram: databases, warehouses, API specs, file stores and source code flow into one map that one agent reads."
scene_caption: "many systems / one map / one agent"
actions:
  - label: "Try it in 2 minutes"
    href: "/#quickstart"
  - label: "Support matrix"
    href: "/reference/database-support/"
landing: true
toc: false
---

{{< band wide="true" label="One question" title="Real questions do not stay inside one system." >}}

Who is at renewal risk is partly in the accounts table, partly in product usage in the warehouse, partly in the CRM, and partly in a policy document. An agent that sees one of those gives a partial answer and does not know it.

<figure class="company-system-map" aria-labelledby="company-system-question"><figcaption id="company-system-question"><span>One question</span><strong>“Which customers are at renewal risk — and why?”</strong></figcaption><div class="company-system-layers"><section class="company-system-layer organization-layer" aria-labelledby="organization-layer-title"><div class="company-layer-heading"><span>01</span><h3 id="organization-layer-title">Your systems</h3></div><div class="company-scale-row" aria-label="Organization scale"><div><strong>12+</strong><span>engines</span></div><div><strong>1,000s</strong><span>of tables</span></div><div><strong>100s</strong><span>of columns</span></div></div><p class="company-source-line">Postgres · Snowflake · MySQL · APIs · Files · Code</p></section><div class="company-map-connector" aria-hidden="true"><span>→</span></div><section class="company-system-layer graphjin-system-layer" aria-labelledby="graphjin-layer-title"><div class="company-layer-heading"><span>02</span><h3 id="graphjin-layer-title">DeepORG</h3></div><ol class="graphjin-system-steps"><li><strong>Discover</strong></li><li><strong>Connect</strong></li><li><strong>Run</strong></li><li><strong>Check</strong></li></ol></section><div class="company-map-connector" aria-hidden="true"><span>→</span></div><section class="company-system-layer outcome-layer" aria-labelledby="outcome-layer-title"><div class="company-layer-heading"><span>03</span><h3 id="outcome-layer-title">Agent answer</h3></div><ul class="company-outcome-list"><li>Who is at risk</li><li>Why</li><li>What can happen next</li></ul><p class="company-outcome-result">Answer · data · evidence · actions</p></section></div></figure>

{{< endband >}}

{{< band id="databases" label="Databases and warehouses" title="Name it, and it is on the map." >}}

DeepORG works with PostgreSQL, MySQL, SQLite, MongoDB, Oracle, SQL Server, Snowflake and Cassandra. The [support matrix](/reference/database-support/) says how far each one goes.

{{< database-logos >}}

```yaml
sources:
  - name: app
    kind: database
    type: postgres
    default: true
    connection_string: ${DATABASE_URL}

  - name: warehouse
    kind: database
    type: snowflake
    connection_string: ${SNOWFLAKE_DSN}

relationships:
  - from: app.orders.customer_id
    to: crm.customers.id
    as: customer
```

A relationship can cross from one source to another. Fields from different databases come back merged in one reply. A nested join across two databases is not one statement: the engine fetches the parent rows, then runs the child query on the other database.

MongoDB is not sent SQL. The same question is compiled into an aggregation pipeline instead.

{{< endband >}}

{{< band label="APIs" title="Drop in an OpenAPI spec. Its operations join your tables." >}}

```yaml
sources:
  - name: billing_api
    kind: api
    specs_dir: config/specs
    specs:
      stripe:
        base_url: "https://api.stripe.com"
        auth:
          scheme: bearer
          token: ${STRIPE_TOKEN}
        joins:
          listInvoices:
            parent_table: customers
            parent_column: stripe_customer_id
            param: customer
            expose_as: invoices
```

Now `customers` has `invoices`, and they come from Stripe. Supported GET operations become joins, single records or lists that sit beside database fields in the same query. Operations that change something are skipped unless you expose them, and then they wait for a person's approval.

{{< endband >}}

{{< band label="Files" title="A bucket is a table." >}}

```yaml
sources:
  - name: documents
    kind: file
    backend: s3
    bucket: app-documents
    prefix: tenants/${account_id}
    presign_ttl: 15m
    capabilities:
      files.list: true
      files.read: true
      files.write: false
```

Local disk, S3 and GCS all work the same way. Every file table has the same columns: `key`, `size`, `content_type`, `etag`, `modified_at`, `url` and `data`. A contract, a policy document or an export can be found and read by the same agent that reads the accounts table.

{{< endband >}}

{{< band id="codesql" label="Source code" title="CodeSQL: query your code as well." >}}

CodeSQL indexes your source tree into a read-only SQLite graph. Agents can ask where a column is used, which code references it, and which symbol owns that reference, through the same interface as everything else. It is evidence from source code without handing a model a shell.

<div class="codesql" aria-label="Querying source code with CodeSQL"><div class="codesql-head"><span class="codesql-kicker">agent asks</span><strong>Where is <code>users.email</code> used?</strong></div><div class="codesql-grid"><figure class="codesql-pane codesql-pane-query"><figcaption>GraphQL · gj_code</figcaption>{{< hl graphql >}}query {
  gj_code(where: {
    name: { eq: "users.email" }
    kind: { eq: "db_ref" }
  }) {
    path
    symbol_name
  }
}{{< /hl >}}</figure><div class="codesql-pane codesql-pane-rows"><div class="codesql-rows-head"><span>matched rows</span><em>3</em></div><div class="codesql-rowset"><div class="codesql-row"><span class="codesql-path">api/users.go</span><span class="codesql-sym">getUser</span></div><div class="codesql-row"><span class="codesql-path">api/invoices.ts</span><span class="codesql-sym">createInvoiceHandler</span></div><div class="codesql-row"><span class="codesql-path">graph/schema.go</span><span class="codesql-sym">userType</span></div></div></div></div><div class="codesql-foot"><span>tree-sitter → read-only SQLite</span><div class="codesql-kinds"><code>kind=file</code><code>kind=symbol</code><code>kind=db_ref</code><code>kind=doc</code></div></div></div>

{{< endband >}}

{{< band label="Same rules everywhere" title="One set of access rules, whichever system answers." >}}

Whatever the source, a query passes through the same checks before it reaches the backend: who is asking, what that source is allowed to do, and what their role may see. A source marked read-only stays read-only. [Your rules, your approval](/why/your-rules/) covers this.

{{< endband >}}

{{< band label="In the docs" title="Go deeper." >}}

- [Database support](/reference/database-support/): the matrix, engine by engine.
- [Multiple databases](/integrations/multi-database/): how cross-database queries run.
- [OpenAPI](/integrations/openapi/): remote APIs as joinable fields.
- [Files and uploads](/integrations/filesystem-uploads/) and [CodeSQL](/integrations/codesql/).
- [MongoDB](/integrations/mongodb/) and [sources](/configure/sources-mode/).

{{< endband >}}
