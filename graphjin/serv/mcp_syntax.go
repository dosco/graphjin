package serv

import (
	"context"

	"github.com/dosco/graphjin/serv/v3/internal/mcpcompat/mcp"
)

// QuerySyntaxReference contains the complete GraphJin query DSL reference
type QuerySyntaxReference struct {
	AnalyticsModeRules  []string                  `json:"analytics_mode_rules,omitempty"`
	Patterns            []QueryPattern            `json:"patterns,omitempty"`
	FilterOperators     FilterOperators           `json:"filter_operators"`
	LogicalOperators    []string                  `json:"logical_operators"`
	Pagination          PaginationSyntax          `json:"pagination"`
	Ordering            OrderingSyntax            `json:"ordering"`
	Aggregations        AggregationsSyntax        `json:"aggregations"`
	AnalyticsDirectives AnalyticsDirectivesSyntax `json:"analytics_directives"`
	Recursive           RecursiveSyntax           `json:"recursive"`
	FullTextSearch      string                    `json:"full_text_search"`
	Directives          map[string]string         `json:"directives"`
	Variables           VariablesSyntax           `json:"variables"`
	JSONPaths           string                    `json:"json_paths"`
	CommonMistakes      []MistakeExample          `json:"common_mistakes"`
	Examples            QueryExamplesForSyntax    `json:"examples"`
}

// AggregationsSyntax describes available aggregation functions
type AggregationsSyntax struct {
	Functions        []string `json:"functions"`
	Usage            string   `json:"usage"`
	HasuraCompatible string   `json:"hasura_compatible"`
	WithGroup        string   `json:"with_group"`
}

// AnalyticsDirectivesSyntax describes GraphJin reporting directives.
type AnalyticsDirectivesSyntax struct {
	Directives []string `json:"directives"`
	Usage      string   `json:"usage"`
	Arguments  string   `json:"arguments"`
	Rules      []string `json:"rules"`
	Dialects   string   `json:"dialects"`
}

// VariablesSyntax shows how to use variables in queries
type VariablesSyntax struct {
	Declaration string   `json:"declaration"`
	Types       []string `json:"types"`
	Example     string   `json:"example"`
}

// MistakeExample shows a common mistake and how to fix it
type MistakeExample struct {
	Wrong  string `json:"wrong"`
	Right  string `json:"right"`
	Reason string `json:"reason"`
}

// QueryExamplesForSyntax contains categorized query examples
type QueryExamplesForSyntax struct {
	Basic         []QueryExample `json:"basic"`
	Filtering     []QueryExample `json:"filtering"`
	Relationships []QueryExample `json:"relationships"`
	Pagination    []QueryExample `json:"pagination"`
	Aggregations  []QueryExample `json:"aggregations"`
	Analytics     []QueryExample `json:"analytics"`
	Recursive     []QueryExample `json:"recursive"`
	Spatial       []QueryExample `json:"spatial"`
	RemoteJoins   []QueryExample `json:"remote_joins"`
}

// FilterOperators groups filter operators by category
type FilterOperators struct {
	Comparison []string `json:"comparison"`
	List       []string `json:"list"`
	Null       []string `json:"null"`
	Text       []string `json:"text"`
	JSON       []string `json:"json"`
	Spatial    []string `json:"spatial"`
}

// PaginationSyntax shows pagination options
type PaginationSyntax struct {
	LimitOffset    string `json:"limit_offset"`
	ForwardCursor  string `json:"forward_cursor"`
	BackwardCursor string `json:"backward_cursor"`
	CursorField    string `json:"cursor_field"`
	Distinct       string `json:"distinct"`
}

// OrderingSyntax shows ordering options
type OrderingSyntax struct {
	Simple     string `json:"simple"`
	Multiple   string `json:"multiple"`
	Nested     string `json:"nested"`
	CustomList string `json:"custom_list"`
	NullsFirst string `json:"nulls_first"`
	NullsLast  string `json:"nulls_last"`
}

// RecursiveSyntax shows recursive query options
type RecursiveSyntax struct {
	FindParents  string `json:"find_parents"`
	FindChildren string `json:"find_children"`
}

// MutationSyntaxReference contains the GraphJin mutation DSL reference
type MutationSyntaxReference struct {
	AnalyticsModeRules []string           `json:"analytics_mode_rules,omitempty"`
	Operations         MutationOperations `json:"operations"`
	CodeSQL            CodeSQLMutationDSL `json:"codesql,omitempty"`
	NestedMutations    NestedMutationInfo `json:"nested_mutations"`
	ConnectDisconnect  ConnectDisconnect  `json:"connect_disconnect"`
	Returning          ReturningInfo      `json:"returning"`
	Validation         ValidationSyntax   `json:"validation"`
	CommonMistakes     []MistakeExample   `json:"common_mistakes"`
	Examples           []QueryExample     `json:"examples"`
}

// CodeSQLMutationDSL describes the GraphQL-native CodeSQL edit workflow.
type CodeSQLMutationDSL struct {
	ReadBeforeWrite string   `json:"read_before_write"`
	Preview         string   `json:"preview"`
	Apply           string   `json:"apply"`
	Locks           string   `json:"locks"`
	FileOps         []string `json:"file_ops"`
	Rules           []string `json:"rules"`
}

// ReturningInfo describes the returning clause behavior
type ReturningInfo struct {
	Description string `json:"description"`
	Example     string `json:"example"`
}

// MutationOperations shows mutation operation syntax
type MutationOperations struct {
	Insert            string `json:"insert"`
	InsertConflictGet string `json:"insert_conflict_get"`
	BulkInsert        string `json:"bulk_insert"`
	Update            string `json:"update"`
	BulkUpdate        string `json:"bulk_update"`
	UpdateWhere       string `json:"update_where"`
	Upsert            string `json:"upsert"`
	Delete            string `json:"delete"`
	OpenAPICall       string `json:"openapi_call"`
}

// NestedMutationInfo describes nested mutations
type NestedMutationInfo struct {
	Description string `json:"description"`
	Example     string `json:"example"`
}

// ConnectDisconnect shows connect/disconnect syntax
type ConnectDisconnect struct {
	Connect    string `json:"connect"`
	Disconnect string `json:"disconnect"`
}

// ValidationSyntax shows validation directive options
type ValidationSyntax struct {
	Directive string   `json:"directive"`
	Options   []string `json:"options"`
	Example   string   `json:"example"`
}

// QueryExample represents an annotated query example
type QueryExample struct {
	Description string `json:"description"`
	Query       string `json:"query"`
	Variables   string `json:"variables,omitempty"`
}

// querySyntaxReference is the static reference data for query syntax
var querySyntaxReference = QuerySyntaxReference{
	FilterOperators: FilterOperators{
		Comparison: []string{"eq", "neq", "gt", "gte", "lt", "lte"},
		List:       []string{"in", "nin"},
		Null:       []string{"is_null"},
		Text:       []string{"like", "ilike", "regex", "iregex", "similar"},
		JSON:       []string{"has_key", "has_key_any", "has_key_all", "contains", "contained_in"},
		Spatial:    []string{"st_dwithin", "st_within", "st_contains", "st_intersects", "st_coveredby", "st_covers", "st_touches", "st_overlaps", "near"},
	},
	LogicalOperators: []string{"and", "or", "not"},
	Pagination: PaginationSyntax{
		LimitOffset:    "limit: 10, offset: 20",
		ForwardCursor:  "first: 10, after: $<table>_cursor — cursor variable name MUST be $<table>_cursor (e.g. $products_cursor). Pass via variables object, not string interpolation",
		BackwardCursor: "last: 10, before: $<table>_cursor — same naming rule as forward cursor",
		CursorField:    "<table>_cursor — request this field at query root level to get the cursor for the next page. Returns null when no more pages exist",
		Distinct:       "distinct: [column1, column2]",
	},
	Ordering: OrderingSyntax{
		Simple:     "order_by: { price: desc }",
		Multiple:   "order_by: { price: desc, id: asc }",
		Nested:     "order_by: { owner: { name: asc } }",
		CustomList: "order_by: { id: [$list, \"asc\"] }",
		NullsFirst: "order_by: { price: { dir: desc, nulls: first } }",
		NullsLast:  "order_by: { price: { dir: asc, nulls: last } }",
	},
	Aggregations: AggregationsSyntax{
		Functions: []string{
			"count_<column> - count non-null values",
			"sum_<column> - sum numeric column",
			"avg_<column> - average of numeric column",
			"min_<column> - minimum value",
			"max_<column> - maximum value",
			"sum(expr: {...}) - sum of an arithmetic expression (revenue = price × qty, margin, ratios)",
			"avg(expr: {...}) / min(expr: {...}) / max(expr: {...}) - same pattern for other aggregates",
		},
		Usage:            "{ products { count_id sum_price avg_price revenue: sum(expr: { mul: [price, quantity] }) } }",
		HasuraCompatible: "{ products_aggregate(where: {active: {eq: true}}) { aggregate { count sum { price } avg { price } min { price } max { price } } } } - compatibility syntax for query roots; nodes, count(columns:/distinct:), inner aliases, and subscriptions are not supported",
		WithGroup:        "{ products(distinct: [category_id]) { category_id count_id sum_price revenue: sum(expr: { mul: [price, quantity] }) } } - group by category",
	},
	AnalyticsDirectives: AnalyticsDirectivesSyntax{
		Directives: []string{
			"@running(aggregate: sum|avg|count|min|max, optional by:, orderBy:) - running metric without collapsing rows",
			"@moving(aggregate: sum|avg|count|min|max, rows: N, optional by:, orderBy:) - trailing moving metric over N rows including the current row",
			"@previous(optional by:, orderBy:) / @next(...) - prior or next row value for period comparisons",
			"@first(optional by:, orderBy:) / @last(...) - first or last value in an ordered group",
			"@rank(optional by:, order: asc|desc) / @denseRank(...) / @rowNumber(...) - rank rows inside an optional group",
		},
		Usage:     `{ orders { account_id month total running_total: total @running(aggregate: sum, by: "account_id", orderBy: { month: asc }) moving_avg_total: total @moving(aggregate: avg, rows: 6, by: "account_id", orderBy: { month: asc }) previous_total: total @previous(by: "account_id", orderBy: { month: asc }) rank_by_total: total @rank(by: "account_id", order: desc) } }`,
		Arguments: `by is optional and accepts a column name or list of column names. orderBy uses GraphJin object ordering like { month: asc }. order is shorthand for ordering by the annotated field.`,
		Rules: []string{
			"Use analytics directives when you need row-level reporting metrics: running totals, moving averages, previous-period values, or rank within a group.",
			"Ordinary grouped summaries still use distinct: [columns] plus aggregate fields like sum_total.",
			"Analytics directives attach to real columns; alias the field to name the derived metric.",
			"orderBy or order is required so reporting rows are deterministic.",
			"MongoDB and known-old SQL database versions reject analytics directives with clear compile-time errors.",
		},
		Dialects: "Postgres, MySQL 8.0+, MariaDB 10.2+, MSSQL 2012+, Oracle, SQLite 3.25+, Snowflake, CockroachDB. MongoDB is unsupported.",
	},
	Recursive: RecursiveSyntax{
		FindParents:  "comments(find: \"parents\") - walks up the tree via self-referencing FK",
		FindChildren: "comments(find: \"children\") - walks down the tree via self-referencing FK",
	},
	FullTextSearch: "products(search: \"search term\") - uses database full-text search",
	Directives: map[string]string{
		"@include(ifRole:)":      "Include field if user has specified role",
		"@skip(ifRole:)":         "Skip field if user has specified role",
		"@include(ifVar:)":       "Include field if variable is true",
		"@skip(ifVar:)":          "Skip field if variable is true",
		"@object":                "Return single object instead of array",
		"@schema(name:)":         "Use specific database schema",
		"@through(table:)":       "Specify the intermediate join table for many-to-many relationships",
		"@through(column:)":      "Disambiguate when the parent and the nested target share multiple foreign keys — name the FK column to follow. For composite foreign keys, naming any one column of the composite is sufficient. Example: billofmaterials { product @through(column: \"componentid\") { name } }",
		"@notRelated":            "Disable automatic relationship detection for a field",
		"@running":               "Create a running metric on a column. Example: running_total: total @running(aggregate: sum, by: \"account_id\", orderBy: { month: asc })",
		"@moving":                "Create a trailing moving metric on a column. Example: moving_avg: total @moving(aggregate: avg, rows: 6, by: \"account_id\", orderBy: { month: asc })",
		"@previous":              "Return the previous value of a column within an ordered group",
		"@next":                  "Return the next value of a column within an ordered group",
		"@first":                 "Return the first value of a column within an ordered group",
		"@last":                  "Return the last value of a column within an ordered group",
		"@rank":                  "Rank the annotated column within each group",
		"@denseRank":             "Dense-rank the annotated column within each group",
		"@rowNumber":             "Number rows within each group",
		"@cacheControl(maxAge:)": "Set cache TTL in seconds for this query",
		"@database(name:)":       "Assign table to a named database (REQUIRED on every table when multiple databases are configured). Used in schema definitions, e.g.: type users @database(name: \"mydb\") { ... }",
	},
	Variables: VariablesSyntax{
		Declaration: "Variables are declared with $ prefix and passed via variables parameter. In where filters, keep the filter object inline and use variables only as filter values.",
		Types:       []string{"$id: Int", "$name: String", "$ids: [Int]", "$active: Boolean"},
		Example:     "query($label: String) { user_fields(where: { label: { eq: $label } }) { id label } }",
	},
	JSONPaths: "For JSONB columns, use underscore notation: metadata_key_subkey maps to metadata->'key'->'subkey'",
	CommonMistakes: []MistakeExample{
		{Wrong: `where: $where`, Right: `where: { label: { eq: $label } }`, Reason: "GraphJin does not accept a variable for the whole where object; variables are supported only inside the inline filter shape"},
		{Wrong: `where: { price: { gt: "50" } }`, Right: `where: { price: { gt: 50 } }`, Reason: "Numeric operators need numbers, not strings"},
		{Wrong: `where: { id: { in: 1 } }`, Right: `where: { id: { in: [1] } }`, Reason: "in/nin operators need arrays, even for single values"},
		{Wrong: `where: { name: { ilike: "test" } }`, Right: `where: { name: { ilike: "%test%" } }`, Reason: "ilike needs % wildcards for partial matching"},
		{Wrong: `products(id: 1)`, Right: `products(where: { id: { eq: 1 } })`, Reason: "Filtering requires where clause with operators (except for shorthand id lookup)"},
		{Wrong: `where: { is_active: { eq: "true" } }`, Right: `where: { is_active: { eq: true } }`, Reason: "Boolean values must be true/false, not strings"},
		{Wrong: `products(first: 10) { products_cursor }`, Right: `products(first: 10) { id } products_cursor`, Reason: "Cursor field must be at query root level, not inside the selection"},
		{Wrong: `products(first: 10, after: "abc123")`, Right: `products(first: 10, after: $products_cursor)`, Reason: "Cursor must be a $variable (not a literal string) passed via the variables object. Variable name must be $<table>_cursor"},
	},
	Examples: QueryExamplesForSyntax{
		Basic: []QueryExample{
			{Description: "Fetch products with limit", Query: "{ products(limit: 10) { id name } }"},
			{Description: "Fetch by ID", Query: "{ products(id: $id) { id name price } }", Variables: "{\"id\": 1}"},
			{Description: "Fetch single object", Query: "{ product @object { id name } }"},
		},
		Filtering: []QueryExample{
			{Description: "Filter with comparison", Query: "{ products(where: { price: { gt: 50 } }) { id name } }"},
			{Description: "Filter with AND", Query: "{ products(where: { and: [{ price: { gt: 10 } }, { price: { lt: 100 } }] }) { id } }"},
			{Description: "Filter with OR", Query: "{ products(where: { or: { name: { ilike: \"%phone%\" }, name: { ilike: \"%tablet%\" } } }) { id } }"},
			{Description: "Filter with NOT", Query: "{ products(where: { not: { price: { is_null: true } } }) { id } }"},
			{Description: "Filter on relationship", Query: "{ products(where: { owner: { email: { eq: $email } } }) { id } }"},
			{Description: "Filter with IN list", Query: "{ products(where: { id: { in: $ids } }) { id name } }", Variables: "{\"ids\": [1, 2, 3]}"},
			{Description: "Full-text search", Query: "{ products(search: \"wireless\") { id name } }"},
			{Description: "JSON field filter", Query: "{ products(where: { metadata: { has_key: \"color\" } }) { id } }"},
		},
		Relationships: []QueryExample{
			{Description: "Parent to children (one-to-many)", Query: "{ users { email products { name } } }"},
			{Description: "Child to parent (many-to-one)", Query: "{ products { name owner { email } } }"},
			{Description: "Many-to-many through join table", Query: "{ products { name customers { email } } }"},
			{Description: "Deep nesting", Query: "{ users { products { purchases { customer { email } } } } }"},
			{Description: "Many-to-many when there are multiple possible join tables — @through(table:) picks the join table", Query: "{ products @through(table: \"categories\") { name } }"},
			{Description: "Multiple FKs to the same target table — @through(column:) names the FK column to follow", Query: "{ billofmaterials { id product @through(column: \"componentid\") { name } } }"},
		},
		Pagination: []QueryExample{
			{Description: "Limit and offset", Query: "{ products(limit: 10, offset: 20) { id name } }"},
			{Description: "Forward cursor pagination", Query: "{ products(first: 10, after: $products_cursor) { id name } products_cursor }", Variables: "{\"products_cursor\": null}"},
			{Description: "Backward cursor pagination", Query: "{ products(last: 10, before: $products_cursor) { id name } products_cursor }", Variables: "{\"products_cursor\": null}"},
			{Description: "First page (no cursor needed)", Query: "{ products(first: 10) { id name } products_cursor }"},
			{Description: "Distinct results", Query: "{ products(distinct: [category_id]) { category_id } }"},
		},
		Aggregations: []QueryExample{
			{Description: "Count records", Query: "{ products { count_id } }"},
			{Description: "Sum values", Query: "{ products { sum_price } }"},
			{Description: "Multiple aggregations", Query: "{ products { count_id sum_price avg_price min_price max_price } }"},
			{Description: "Aggregations with grouping", Query: "{ products(distinct: [category_id]) { category_id count_id sum_price avg_price } }"},
			{Description: "Get table statistics", Query: "{ products { count_id min_price max_price avg_price } }"},
			{Description: "Expression aggregate — SUM(price × quantity) as one server-side aggregate", Query: "{ order_items(distinct: [product_id]) { product_id revenue: sum(expr: { mul: [unitprice, quantity] }) } }"},
			{Description: "Global single-row total (no distinct needed when fields are all aggregates)", Query: "{ order_items { total_revenue: sum(expr: { mul: [unitprice, quantity] }) } }"},
			{Description: "Top-N by computed metric — order_by on expression alias", Query: "{ order_items(distinct: [product_id], order_by: { revenue: desc }, limit: 10) { product_id revenue: sum(expr: { mul: [unitprice, quantity] }) } }"},
			{Description: "Ratio-of-aggregates — bare expression with nested sum/avg nodes", Query: "{ order_items { margin_pct: ratio(expr: { div: [{ sum: { mul: [unitprice, quantity] } }, { sum: linetotal }] }) } }"},
			{Description: "Joined column via FK dot-notation (up to 3 hops)", Query: "{ order_items(distinct: [product_id]) { gross: sum(expr: { mul: [quantity, { sub: [unitprice, \"product.standardcost\"] }] }) } }"},
			{Description: "Conditional aggregate — SUM(CASE WHEN … THEN … ELSE 0 END)", Query: "{ orders(distinct: [customer_id]) { big_ticket: sum(expr: { case: { arms: [{ when: { total: { gt: 100 } }, then: total }], else: 0 } }) } }"},
		},
		Analytics: []QueryExample{
			{Description: "Running total per account", Query: `{ orders { account_id month total running_total: total @running(aggregate: sum, by: "account_id", orderBy: { month: asc }) } }`},
			{Description: "Moving average over the last 6 rows", Query: `{ orders { account_id month total moving_avg_total: total @moving(aggregate: avg, rows: 6, by: "account_id", orderBy: { month: asc }) } }`},
			{Description: "Previous and next values for period comparison", Query: `{ orders { account_id month total previous_total: total @previous(by: "account_id", orderBy: { month: asc }) next_total: total @next(by: "account_id", orderBy: { month: asc }) } }`},
			{Description: "First and last value in each ordered group", Query: `{ orders { account_id month total first_total: total @first(by: "account_id", orderBy: { month: asc }) last_total: total @last(by: "account_id", orderBy: { month: asc }) } }`},
			{Description: "Rank rows inside each group", Query: `{ orders { account_id total rank_by_total: total @rank(by: "account_id", order: desc) dense_rank_by_total: total @denseRank(by: "account_id", order: desc) row_num: total @rowNumber(by: "account_id", order: desc) } }`},
		},
		Recursive: []QueryExample{
			{Description: "Find all children (descendants)", Query: "{ comments(id: $id) { id body replies: comments(find: \"children\") { id body } } }"},
			{Description: "Find all parents (ancestors)", Query: "{ comments(id: $id) { id body thread: comments(find: \"parents\") { id body } } }"},
		},
		Spatial: []QueryExample{
			{Description: "Find within distance (meters)", Query: "{ locations(where: { geom: { st_dwithin: { point: [-122.4, 37.7], distance: 1000 } } }) { id name } }"},
			{Description: "Find within distance (miles)", Query: "{ locations(where: { geom: { st_dwithin: { point: [-122.4, 37.7], distance: 5, unit: \"miles\" } } }) { id name } }"},
			{Description: "Point in polygon", Query: "{ locations(where: { geom: { st_within: { polygon: [[-122.5, 37.7], [-122.3, 37.7], [-122.3, 37.9], [-122.5, 37.9], [-122.5, 37.7]] } } }) { id } }"},
			{Description: "Polygon contains point", Query: "{ regions(where: { boundary: { st_contains: { point: [-122.4, 37.7] } } }) { id name } }"},
			{Description: "Geometry intersection (GeoJSON)", Query: "{ parcels(where: { geom: { st_intersects: { geometry: { type: \"Polygon\", coordinates: [[[-122.5, 37.7], [-122.3, 37.7], [-122.3, 37.9], [-122.5, 37.9], [-122.5, 37.7]]] } } } }) { id } }"},
			{Description: "MongoDB near query", Query: "{ locations(where: { geom: { near: { point: [-122.4, 37.7], maxDistance: 5000 } } }) { id name } }"},
		},
		RemoteJoins: []QueryExample{
			{Description: "Query with remote API join (resolver)", Query: "{ users { email payments { desc amount } } }"},
			{Description: "Remote join - resolver fetches data from external API using DB column as $id", Query: "{ customers(limit: 10) { name stripe_subscriptions { plan status } } }"},
			{Description: "Filter, order, and page an OpenAPI virtual table", Query: "{ alerts(where: { severity: { eq: \"critical\" } }, order_by: { warningCount: desc }, limit: 10) { id severity warningCount } }"},
		},
	},
}

// mutationSyntaxReference is the static reference data for mutation syntax
var mutationSyntaxReference = MutationSyntaxReference{
	Operations: MutationOperations{
		Insert:            "products(insert: { name: \"New\", price: 10 })",
		InsertConflictGet: "users(insert: { email: $email, name: $name }, on_conflict: get) - insert or return the unchanged row matching the one inferred supplied unique key; PostgreSQL and SQLite only; single non-nested object only",
		BulkInsert:        "products(insert: $items) - where $items is an array of objects",
		Update:            "products(id: $id, update: { name: \"Updated\" })",
		BulkUpdate:        "products(update: $items) - where $items is array with id + fields to update",
		UpdateWhere:       "products(where: { price: { lt: 10 } }, update: { on_sale: true })",
		Upsert:            "products(upsert: { id: $id, name: \"Name\" }) - insert or update based on id",
		Delete:            "products(delete: true, where: { id: { eq: $id } })",
		OpenAPICall:       "mutation ($request: JSON!) { <catalog_api_root>(call: $request) { ok status_code operation_id request_id response_json } } - use only a caller-visible api_operation catalog root",
	},
	CodeSQL: CodeSQLMutationDSL{
		ReadBeforeWrite: `Query gj_code(where: { kind: { eq: "symbol" } }) or gj_code(where: { kind: { eq: "file" } }) and request code/code_context plus path/hash before editing source.`,
		Preview:         `mutation { gj_code(insert: { kind: "change_set", action: "preview", title: "...", edits: [{ op: "replace", path: "main.go", expected_hash: "...", replacements: [{ start_byte: 10, end_byte: 20, old_text: "old", new_text: "new" }] }] }) { id kind status diff errors_json } }`,
		Apply:           `mutation { gj_code(id: "change_set:123", update: { kind: "change_set", id: 123, action: "apply" }) { id kind status files_changed files_reindexed errors_json } }`,
		Locks:           `mutation { gj_code(insert: { kind: "lock", action: "acquire", path: "main.go", ranges: [{ start_byte: 10, end_byte: 20 }], owner: "agent" }) { id kind lease_token status } } For create/rename target reservation, acquire whole_file: true on the target path.`,
		FileOps: []string{
			`create: { op: "create", path: "new.go", content: "package main\n", mkdirs: true }`,
			`delete: { op: "delete", path: "old.go", expected_hash: "current-file-hash" }`,
			`rename: { op: "rename", path: "old.go", new_path: "pkg/new.go", expected_hash: "current-file-hash", mkdirs: true }`,
		},
		Rules: []string{
			"Never query or mutate raw CodeSQL index roots like code_files, code_symbols, code_nodes, or code_captures directly; use gj_code filtered by kind.",
			"Always query code/code_context first, then include the exact expected_hash for replace/delete/rename and exact old_text in replacements.",
			"Create does not use expected_hash, but fails if the target path already exists.",
			"Preview before apply. Apply fails if the file hash changed, old_text no longer matches, or an overlapping lock exists.",
			"On stale expected_hash, re-query CodeSQL source and submit a new preview.",
			"Use gj_code kind lock only for longer edit sessions; short preview/apply flows rely on automatic range locking.",
		},
	},
	NestedMutations: NestedMutationInfo{
		Description: "Insert across multiple related tables atomically in a single mutation",
		Example:     "purchases(insert: { quantity: 5, customer: { email: \"new@test.com\" }, product: { name: \"New\" } })",
	},
	ConnectDisconnect: ConnectDisconnect{
		Connect:    "products(insert: { name: \"X\", owner: { connect: { id: 5 } } }) - link to existing record",
		Disconnect: "users(id: $id, update: { products: { disconnect: { id: 10 } } }) - unlink existing record",
	},
	Returning: ReturningInfo{
		Description: "After mutation, select fields to return in the response",
		Example:     "mutation { products(insert: { name: $name }) { id name created_at } } - returns inserted record with selected fields",
	},
	Validation: ValidationSyntax{
		Directive: "@constraint",
		Options:   []string{"format", "min", "max", "required", "requiredIf", "greaterThan", "lessThan"},
		Example:   "mutation @constraint(variable: \"email\", format: \"email\") { users(insert: { email: $email }) { id } }",
	},
	CommonMistakes: []MistakeExample{
		{Wrong: `products(insert: { id: 1, name: "X" })`, Right: `products(insert: { name: "X" })`, Reason: "Don't include auto-generated ID in insert unless using upsert"},
		{Wrong: `products(update: { name: "X" })`, Right: `products(id: $id, update: { name: "X" })`, Reason: "Update requires id or where clause to identify records"},
		{Wrong: `products(delete: true)`, Right: `products(delete: true, where: { id: { eq: $id } })`, Reason: "Delete requires where clause to prevent accidental mass deletion"},
		{Wrong: `owner: { id: 5 }`, Right: `owner: { connect: { id: 5 } }`, Reason: "Use connect to link to existing records, not direct assignment"},
		{Wrong: `users(insert: { id: $id, email: $email }, on_conflict: get)`, Right: `users(insert: { email: $email }, on_conflict: get)`, Reason: "Supply exactly one inferable unique target; primary key plus another unique key is ambiguous"},
		{Wrong: `users(update: { name: $name }, on_conflict: get)`, Right: `users(insert: { email: $email, name: $name }, on_conflict: get)`, Reason: "on_conflict: get is insert-only; use upsert for insert-or-update"},
		{Wrong: `api_root(call: { url: "https://example.com", body: {...} })`, Right: `api_root(call: $request)`, Reason: "OpenAPI calls accept only declared path/query/header/body values; method, URL, authentication, and media type come from the server registry"},
	},
	Examples: []QueryExample{
		{Description: "Simple insert", Query: "mutation { users(insert: { email: $email }) { id } }"},
		{Description: "Insert or return the existing row unchanged", Query: "mutation { users(insert: { email: $email, name: $name }, on_conflict: get) { id email name } }"},
		{Description: "Bulk insert", Query: "mutation { products(insert: $items) { id name } }", Variables: `{"items": [{"name": "A", "price": 10}, {"name": "B", "price": 20}]}`},
		{Description: "Insert with nested create", Query: "mutation { purchases(insert: { quantity: 1, product: { name: $name, price: $price } }) { id } }"},
		{Description: "Update by ID", Query: "mutation { products(id: $id, update: { price: $price }) { id price } }"},
		{Description: "Update with where clause", Query: "mutation { products(where: { category: { eq: \"sale\" } }, update: { discount: 10 }) { id } }"},
		{Description: "Upsert (insert or update)", Query: "mutation { products(upsert: { id: $id, name: $name }) { id name } }"},
		{Description: "Delete by ID", Query: "mutation { products(delete: true, where: { id: { eq: $id } }) { id } }"},
		{Description: "Connect existing record", Query: "mutation { products(insert: { name: $name, owner: { connect: { id: $owner_id } } }) { id } }"},
		{Description: "Disconnect relationship", Query: "mutation { users(id: $id, update: { products: { disconnect: { id: $product_id } } }) { id } }"},
		{Description: "Call an authorized OpenAPI mutation discovered as an api_operation catalog item", Query: "mutation ($request: JSON!) { external_create_resource(call: $request) { ok status_code operation_id request_id response_json } }", Variables: `{"request":{"body":{"name":"Example resource","enabled":true}}}`},
	},
}

// registerResources registers MCP resources that clients can prefetch.
func (ms *mcpServer) registerResources() {
	if ms.service != nil && ms.service.conf != nil && ms.service.conf.mcpDisabled() {
		return
	}
	ms.registerWatchResources()
}

// registerSyntaxTools is retained as an inert compatibility hook. Syntax
// guidance now lives in catalog/help rows instead of standalone MCP tools.
func (ms *mcpServer) registerSyntaxTools() {}

// handleGetQuerySyntax returns the query syntax reference
func (ms *mcpServer) handleGetQuerySyntax(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ref := querySyntaxReference
	if ms.analyticsModeOn() {
		ref.AnalyticsModeRules = analyticsModeRules()
	}
	// Three universal query shapes (metric-by-dimension, time-series,
	// top-N). Surfaced unconditionally — patterns are general DSL-shape
	// guidance, not analytics-mode-specific.
	ref.Patterns = canonicalQueryPatterns()
	return ms.toolResultJSON("get_query_syntax", req.GetArguments(), ref)
}

// handleGetMutationSyntax returns the mutation syntax reference
func (ms *mcpServer) handleGetMutationSyntax(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ref := mutationSyntaxReference
	if ms.analyticsModeOn() {
		ref.AnalyticsModeRules = analyticsModeRules()
	}
	return ms.toolResultJSON("get_mutation_syntax", req.GetArguments(), ref)
}
