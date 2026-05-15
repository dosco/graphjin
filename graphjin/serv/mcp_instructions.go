package serv

func mcpServerInstructions(conf *Config) string {
	if conf != nil && conf.MCP.Disable {
		return disabledServerInstructions
	}
	if conf != nil && conf.legacyMCPToolsEnabled() {
		return legacyServerInstructions
	}
	if conf != nil && !conf.catalogToolsEnabled() {
		return sourceGraphQLServerInstructions
	}
	return catalogServerInstructions
}

const disabledServerInstructions = `GraphJin MCP is disabled by configuration.

No MCP discovery, prompt, resource, execution, workflow, config, schema, or catalog tools are available from this server. Do not recommend MCP tool calls unless MCP is enabled in configuration.
`

const sourceGraphQLServerInstructions = `GraphJin is running in sources mode without the GraphJin catalog source.

Use the GraphQL endpoint and registered execution/control-plane tools that are actually available on this server. Do not recommend legacy discovery tools unless mcp.legacy_discovery is enabled, and do not recommend catalog tools unless a sources entry with kind: graphjin enables catalog.
`

const catalogServerInstructions = `GraphJin is a GraphQL-to-SQL compiler. You query databases using GraphJin's own DSL (not standard GraphQL).

## Catalog-first operating loop

Discovery means selecting evidence-backed catalog items before acting. Do not write queries from memory.

1. Query gj_catalog to find relevant schema, relationship, workflow, language, config, policy, capability, query-pattern, mutation-pattern, or common-mistake items. Use search for intelligent ranked text search, and where for precise GraphJin-style filters.
2. Select details_json, evidence_json, examples_json, safety_json, and edges_json on the best matching gj_catalog item before choosing tables, columns, relationships, operators, or actions. In tool terms, inspect details, evidence, examples, safety notes, and nearby graph edges.
3. Resolve ambiguity by inspecting candidate items. If multiple tables or columns match, do not guess from names alone.
4. Use validate_where_clause before filters that depend on column types, operators, or real values.
5. Before config, workflow, schema, filesystem, or CodeSQL changes, inspect gj_catalog for gj_security.query and then query gj_security for high/critical findings and effective policy.
6. Prefer GraphJin control-plane GraphQL mutations for workflow/config actions after discovery: gj_workflow_execution(insert) for workflow execution, gj_workflow(insert/update/delete) for workflow management, and gj_config(id: "current", update: ...) for config changes. Use MCP tools such as reload_schema, validate_where_clause, and fix_query_error for schema refresh, filter checks, and query repair.
7. Prefer workflows for broad data questions after discovery. Workflows can page and aggregate safely.
8. Observe results, then return to catalog items when the result, error, or follow-up question changes the facts you need.

Legacy discovery tools such as list_tables, describe_table, find_path, get_table_sample, get_query_syntax, get_mutation_syntax, get_workflow_guide, and list_workflows are not available unless mcp.legacy_discovery is enabled. Do not recommend disabled legacy tools.

## Discovery recipes

- Catalog helpers, when registered, are get_catalog_entrypoints, query_catalog, and get_catalog_card; the canonical GraphQL root is still gj_catalog.
- Compatibility catalog query shape: query_catalog(search: "workflow", where: { kind: { eq: "workflow" } }).
- Canonical catalog query shape: gj_catalog(search: "join orders customers", where: { kind: { eq: "relationship" } }, order_by: { search_rank: desc }) { id kind name summary details_json edges_json }.
- Schema discovery: gj_catalog(where: { kind: { eq: "table" } }) { id name summary details_json } to find tables and table evidence.
- Column discovery: gj_catalog(where: { kind: { eq: "column" }, table_name: { eq: "<table>" } }) { id name type summary evidence_json } to find columns, types, sensitivity notes, indexes, and filter hints.
- Relationship discovery: gj_catalog(search: "join <source> <target>", where: { kind: { eq: "relationship" } }) { id name summary evidence_json edges_json } before nesting related selectors.
- GraphJin language discovery: gj_catalog(where: { kind: { in: ["directive", "operator_set", "query_pattern", "mutation_pattern"] } }) { id kind name summary examples_json } before using GraphJin-specific syntax.
- Analytics discovery: gj_catalog(search: "running revenue rank window", where: { kind: { in: ["directive", "query_pattern", "deprecated_feature"] } }, order_by: { search_rank: desc }) { id kind name summary examples_json } for reporting/window-style features.
- Value-sensitive filters: if a filter depends on real status strings, enum labels, tenant names, or conventions, inspect sample/profile availability on table or column items before choosing values.
- Config and policy discovery: gj_catalog(search: "permission role blocked", where: { kind: { in: ["config", "capability"] } }) { id kind name summary safety_json } explains enabled behavior with sensitive values redacted. Changes go through control-plane GraphQL mutations when enabled.
- Security posture: find guidance with gj_catalog(where: { kind: { eq: "system_capability" }, name: { eq: "gj_security.query" } }) { name summary details_json examples_json safety_json }, then query gj_security(where: { kind: { eq: "finding" }, severity: { in: ["high", "critical"] } }) { id severity title recommendation evidence_json }.
- Workflow reuse: gj_catalog(search: "workflow", where: { kind: { eq: "workflow" } }) { id name summary input_schema_json } to find reusable workflows, then run mutation { gj_workflow_execution(insert: { workflow_name: "...", variables: {...} }) { status result_json error duration_ms } }. gj_workflow_execution is mutation-only and returns an ephemeral result row; it does not store run history. Mark the workflows source or gj_workflow_execution table read_only to block it. The execute_workflow MCP tool is a legacy compatibility wrapper gated by mcp.legacy_discovery and mcp.allow_workflow_execution.
- Workflow create/update/delete: use mutation { gj_workflow(insert/update/delete: ...) { name source_hash catalog_revision } } when mcp.allow_workflow_updates is enabled.
- GraphQL catalog discovery: use gj_catalog as the single discovery root, for example gj_catalog(where: { kind: { eq: "table" } }) { id kind name summary } or gj_catalog(where: { kind: { eq: "capability" } }) { name summary safety_json }.
- Config and schema actions: use gj_config(id: "current", update: ...) for config changes and MCP tools such as reload_schema, preview_schema_changes, and apply_schema_changes for schema operations.
- Query repair: call fix_query_error with the query and error, then inspect relevant schema or language items before retrying.

## Key DSL rules

- GROUP BY does not exist. Use distinct: [columns] instead.
- Aggregation fields use the pattern <fn>_<column>: count_id, sum_price, avg_quantity, etc.
- For arithmetic metrics such as revenue = price * qty, use expression aggregates like sum(expr: { mul: [price, qty] }).
- Analytics/reporting directives attach to real columns. Use @running, @moving, @previous, @next, @first, @last, @rank, @denseRank, and @rowNumber.
- Filter operators are typed. Use validate_where_clause when unsure.
- in/nin values MUST be arrays: { id: { in: [1,2,3] } }.
- Every query level has a default row limit. Set explicit limits on top-level and nested selectors.
- Never infer relationship paths from column names alone. Use relationship items, evidence, and graph edges.

## Discovery vs action

Catalog tools own nouns, facts, context, and evidence. In GraphQL terms, gj_catalog is the single catalog discovery root: filter by kind for tables, columns, relationships, workflows, language features, entrypoints, capabilities, and system capabilities. Control-plane GraphQL mutation roots own workflow/config verbs: gj_workflow, gj_workflow_execution, and gj_config. MCP action tools handle schema reloads, schema changes, where-clause validation, and query repair. Discover first, act second.
`

const legacyServerInstructions = `GraphJin is a GraphQL-to-SQL compiler. You query databases using GraphJin's own DSL (not standard GraphQL).

## Before answering any data question

1. Call tool get_query_syntax — learn the query DSL.
2. Call tool list_tables — find relevant tables.
3. Use find_path or explore_relationships to understand how tables connect — do NOT guess join paths.
4. Use describe_table for column details on the specific tables you need.
5. Use get_table_sample on any table whose VALUES matter — to see real column
   values (status strings, enum labels, naming conventions like "leadid" vs
   "lead_id"), distributions, and a few sample rows. Call this BEFORE writing
   filters that depend on specific string values.
6. Check list_workflows for an existing workflow that already answers the question.
7. If no existing workflow fits, write a new workflow using execute_workflow.

## ALWAYS use workflows

ALWAYS use JavaScript workflows (execute_workflow) to answer data questions.
Do NOT use execute_graphql or write_query directly — tables can have hundreds of thousands of rows
and you cannot predict result sizes in advance. Workflows paginate through data server-side
and aggregate in JavaScript, so they handle any dataset size safely.

Check list_workflows first — reuse an existing workflow if one fits. Otherwise write a new one.

## Key DSL rules (for queries inside workflows)

- GROUP BY does not exist. Use distinct: [columns] instead.
- Aggregation fields use the pattern <fn>_<column>: count_id, sum_price, avg_quantity, etc.
- Analytics/reporting directives attach to real columns. Use @running for running
  totals, @moving for moving averages/totals, @previous/@next for period
  comparisons, @first/@last for group endpoints, and @rank/@denseRank/@rowNumber
  for row ranking. Use by only when the metric should be scoped within groups.
  These return one row per input row, unlike plain aggregates.
- For any metric involving ARITHMETIC (revenue = price × qty, margin, discounted totals,
  ratios), use EXPRESSION AGGREGATES — sum_<col> × sum_<col> is mathematically wrong.
  See "Expression aggregates" below.
- Filter operators: eq, neq, gt, gte, lt, lte, in (array), nin, is_null, like, ilike (needs % wildcards).
- in/nin values MUST be arrays: { id: { in: [1,2,3] } }
- Cursor pagination: { products(first: 20, after: $products_cursor) { id } products_cursor }
- MongoDB and known-old SQL database versions reject analytics directives at
  compile time with clear errors.

## Expression aggregates — USE THESE FIRST for any arithmetic metric

Before you reach for sum_<col>, check: does the metric multiply, subtract, or divide
across columns? If yes, use sum(expr: ...) with a structured operator tree. The query
compiler emits one server-side SQL aggregate — correct, fast, no client-side math.

Leaf values follow the parser-tag convention the where: clause already uses:
  bare identifier    — column reference:           price, qty
  quoted string      — column ref (dots allowed):  "product.standardcost"
  number             — numeric literal:            100, 2.5
  $var               — bind parameter:             $bump
  { op: ... }        — nested operator:            { mul: [...] }, { case: {...} }

(The explicit wrapper forms { col: "price" }, { num: 2 }, { var: "bump" } still
work and are accepted as equivalents for disambiguation.)

Three patterns you WILL use frequently:

1. SUM(a × b) — revenue, line totals, weighted sums:
   {
     salesorderdetail(distinct: [productid]) {
       productid
       revenue: sum(expr: { mul: [unitprice, orderqty] })
     }
   }

2. Global single-row total — no distinct, no other fields needed:
   { salesorderdetail {
       total_revenue: sum(expr: { mul: [unitprice, orderqty] })
     }
   }

3. Top N by computed metric — order_by on the expression alias:
   { salesorderdetail(distinct: [productid], order_by: { revenue: desc }, limit: 10) {
       productid
       revenue: sum(expr: { mul: [unitprice, orderqty] })
     }
   }

Other operators: add, sub, div, mod, neg, coalesce, nullif, case, cast.
Aggregate nodes inside expressions (for ratio-of-aggregates):
  ratio: ratio(expr: { div: [{ sum: revenue }, { sum: cost }] })

Joined columns: "related.field" (quoted, dots allowed) traverses FK joins
up to 3 hops.

For the full grammar and more patterns, call get_query_syntax.

## Analytics directives — reporting metrics while keeping rows visible

Use analytics directives when you need report-friendly row metrics such as
running totals, moving averages, rank within a segment, previous-period values,
or first/last value in an ordered group:

  {
    orders(limit: 100) {
      account_id
      month
      total
      running_total: total @running(aggregate: sum, by: "account_id", orderBy: { month: asc })
      moving_avg_total: total @moving(aggregate: avg, rows: 6, by: "account_id", orderBy: { month: asc })
      previous_total: total @previous(by: "account_id", orderBy: { month: asc })
      rank_by_total: total @rank(by: "account_id", order: desc)
    }
  }

For ordinary grouped summaries, continue using distinct plus aggregate fields:
  { orders(distinct: [account_id]) { account_id sum_total } }.

## CRITICAL: Default row limits

Every query — top-level AND nested — has a default row limit (call get_query_syntax for the exact value).
If you omit an explicit limit, results are SILENTLY truncated. You will get incomplete data with no error or warning.
ALWAYS set an explicit limit on EVERY level of your query, especially nested children:

  BAD:  { orders { order_items { productid qty } } }                    — nested items silently capped
  GOOD: { orders(limit: 100) { order_items(limit: 200) { productid qty } } }  — explicit at every level

## order_by on aggregation aliases

You CAN use order_by on aggregation fields when distinct is present:
  { salesorderdetail(distinct: [productid], order_by: { sum_orderqty: desc }) { ... } }
You can also order by an expression-aggregate alias (see "Expression aggregates" above):
  order_by: { revenue: desc }   // revenue is the alias of a sum(expr: ...) field

## Query direction: ALWAYS top-down

GraphJin resolves relationships through nesting, not through WHERE filters on parent tables.
ALWAYS start your query from the grouping/parent table and nest downward into child tables.
NEVER start from a leaf/detail table and try to filter upward through relationships.

Correct (top-down — start from territory, nest into orders, then details):
  { salesterritory { name salesorderheader { salesorderdetail(distinct: [productid]) { productid sum_orderqty } } } }

Wrong (bottom-up — filtering upward from detail through header to territory):
  { salesorderdetail(where: { salesorderheader: { territoryid: { eq: 1 } } }) { ... } }

## Nested aggregation (inside workflow queries)

You can aggregate on nested/child tables. Each nesting level gets its own GROUP BY via distinct.

Example query for use inside a workflow — top products by territory:
  {
    salesterritory {
      name
      salesorderheader {
        salesorderdetail(distinct: [productid]) {
          productid
          sum_orderqty
          specialofferproduct {
            product { name }
          }
        }
      }
    }
  }

## Relationship discovery

- Use find_path(from_table, to_table) to find the join path between any two tables.
- Use explore_relationships(table, depth) to map out the data model neighborhood around a table.
- Never guess at join paths or FK relationships — always verify with these tools first.

## CodeSQL source mutations

When editing source through a CodeSQL database:

1. Query gj_code by kind first, for example gj_code(search: "...", where: { kind: { in: ["file", "symbol", "reference"] } }) { id kind path name summary }.
2. Request path/hash plus code or code_context from gj_code so you have the current source context for replace/delete/rename.
3. Preview edits with gj_code(insert: { kind: "change_set", action: "preview", ... }) before applying.
4. Use edit op "replace", "create", "delete", or "rename"; create requires content and no expected_hash.
5. Include the exact old_text for each byte range replacement.
6. Apply with gj_code(update: { kind: "change_set", action: "apply", ... }) only after the preview diff looks correct.
7. If a hash is stale, re-read CodeSQL source and create a fresh preview.
8. Never use raw CodeSQL roots directly; use gj_code filtered by kind.
`
