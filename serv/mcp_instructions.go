package serv

const serverInstructions = `GraphJin is a GraphQL-to-SQL compiler. You query databases using GraphJin's own DSL (not standard GraphQL).

## Before answering any data question

1. Read resource graphjin://discovery/syntax — learn the query DSL.
2. Read resource graphjin://discovery/tables — find relevant tables.
3. Use find_path or explore_relationships to understand how tables connect — do NOT guess join paths.
4. Use describe_table for column details on the specific tables you need.
5. Check list_workflows for an existing workflow that already answers the question.
6. If no existing workflow fits, write a new workflow using execute_workflow.

## ALWAYS use workflows

ALWAYS use JavaScript workflows (execute_workflow) to answer data questions.
Do NOT use execute_graphql or write_query directly — tables can have hundreds of thousands of rows
and you cannot predict result sizes in advance. Workflows paginate through data server-side
and aggregate in JavaScript, so they handle any dataset size safely.

Check list_workflows first — reuse an existing workflow if one fits. Otherwise write a new one.

## Key DSL rules (for queries inside workflows)

- GROUP BY does not exist. Use distinct: [columns] instead.
- Aggregation fields use the pattern <fn>_<column>: count_id, sum_price, avg_quantity, etc.
- Filter operators: eq, neq, gt, gte, lt, lte, in (array), nin, is_null, like, ilike (needs % wildcards).
- in/nin values MUST be arrays: { id: { in: [1,2,3] } }
- Cursor pagination: { products(first: 20, after: $products_cursor) { id } products_cursor }

## CRITICAL: Default row limits

Every query — top-level AND nested — has a default row limit (check graphjin://discovery/syntax for the exact value).
If you omit an explicit limit, results are SILENTLY truncated. You will get incomplete data with no error or warning.
ALWAYS set an explicit limit on EVERY level of your query, especially nested children:

  BAD:  { orders { order_items { productid qty } } }                    — nested items silently capped
  GOOD: { orders(limit: 100) { order_items(limit: 200) { productid qty } } }  — explicit at every level

## order_by does NOT work on aggregation aliases

You cannot use order_by on aggregation fields like sum_orderqty or count_id.
Sort aggregated results in the workflow JavaScript layer, not in the query.

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
`
