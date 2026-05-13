package serv

const serverInstructions = `GraphJin is a GraphQL-to-SQL compiler. You query databases using GraphJin's own DSL (not standard GraphQL).

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
- Window functions for analytics/reporting use @window on an aggregate or built-in
  analytic field. Use sum_<column> @window for running totals, row_number/rank/
  dense_rank @window for ranking, and lag_<column>/lead_<column>/first_value_<column>/
  last_value_<column> @window for period comparisons. These return one row per
  input row, unlike plain aggregates.
- For any metric involving ARITHMETIC (revenue = price × qty, margin, discounted totals,
  ratios), use EXPRESSION AGGREGATES — sum_<col> × sum_<col> is mathematically wrong.
  See "Expression aggregates" below.
- Filter operators: eq, neq, gt, gte, lt, lte, in (array), nin, is_null, like, ilike (needs % wildcards).
- in/nin values MUST be arrays: { id: { in: [1,2,3] } }
- Cursor pagination: { products(first: 20, after: $products_cursor) { id } products_cursor }
- MongoDB does not support SQL window functions. SQL dialects with known-old
  versions fail at compile time with clear @window errors.

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

## Window functions — analytics/reporting rows without GROUP BY collapse

Use @window when you need report-friendly row metrics such as running totals,
rank within a segment, previous-period values, or first/last value in a frame:

  {
    orders(limit: 100) {
      account_id
      month
      total
      rank: row_number @window(partition: ["account_id"], order: ["total desc"])
      previous_total: lag_total @window(partition: ["account_id"], order: ["month"])
      running_total: sum_total @window(
        partition: ["account_id"],
        order: ["month"],
        frame: "rows unbounded preceding"
      )
    }
  }

Supported built-ins: row_number, rank, dense_rank, lag_<column>, lead_<column>,
first_value_<column>, last_value_<column>. Built-in analytic functions require
@window. Window order entries use strings like "month asc" or
"total desc nulls last"; NULLS FIRST/LAST is accepted only on dialects with
native support.

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

1. Query code_symbols, code_nodes, or code_captures first and request code or code_context.
2. Also request code_files { path hash } so you have the current expected_hash for replace/delete/rename.
3. Preview edits with code_change_sets(action: "preview") before applying.
4. Use edit op "replace", "create", "delete", or "rename"; create requires content and no expected_hash.
5. Include the exact old_text for each byte range replacement.
6. Apply with code_change_sets(action: "apply") only after the preview diff looks correct.
7. If a hash is stale, re-read CodeSQL source and create a fresh preview.
8. Never mutate code_files, code_symbols, code_nodes, or other derived CodeSQL tables directly.
`
