package serv

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
)

// QuerySyntaxReference contains the complete GraphJin query DSL reference
type QuerySyntaxReference struct {
	FilterOperators  FilterOperators        `json:"filter_operators"`
	LogicalOperators []string               `json:"logical_operators"`
	Pagination       PaginationSyntax       `json:"pagination"`
	Ordering         OrderingSyntax         `json:"ordering"`
	Aggregations     AggregationsSyntax     `json:"aggregations"`
	Recursive        RecursiveSyntax        `json:"recursive"`
	FullTextSearch   string                 `json:"full_text_search"`
	Directives       map[string]string      `json:"directives"`
	Variables        VariablesSyntax        `json:"variables"`
	JSONPaths        string                 `json:"json_paths"`
	CommonMistakes   []MistakeExample       `json:"common_mistakes"`
	Examples         QueryExamplesForSyntax `json:"examples"`
}

// AggregationsSyntax describes available aggregation functions
type AggregationsSyntax struct {
	Functions []string `json:"functions"`
	Usage     string   `json:"usage"`
	WithGroup string   `json:"with_group"`
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
	Recursive     []QueryExample `json:"recursive"`
	Spatial       []QueryExample `json:"spatial"`
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
	LimitOffset     string `json:"limit_offset"`
	ForwardCursor   string `json:"forward_cursor"`
	BackwardCursor  string `json:"backward_cursor"`
	CursorField     string `json:"cursor_field"`
	Distinct        string `json:"distinct"`
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
	Operations        MutationOperations `json:"operations"`
	NestedMutations   NestedMutationInfo `json:"nested_mutations"`
	ConnectDisconnect ConnectDisconnect  `json:"connect_disconnect"`
	Returning         ReturningInfo      `json:"returning"`
	Validation        ValidationSyntax   `json:"validation"`
	CommonMistakes    []MistakeExample   `json:"common_mistakes"`
	Examples          []QueryExample     `json:"examples"`
}

// ReturningInfo describes the returning clause behavior
type ReturningInfo struct {
	Description string `json:"description"`
	Example     string `json:"example"`
}

// MutationOperations shows mutation operation syntax
type MutationOperations struct {
	Insert      string `json:"insert"`
	BulkInsert  string `json:"bulk_insert"`
	Update      string `json:"update"`
	BulkUpdate  string `json:"bulk_update"`
	UpdateWhere string `json:"update_where"`
	Upsert      string `json:"upsert"`
	Delete      string `json:"delete"`
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
		ForwardCursor:  "first: 10, after: $cursor - paginate forward",
		BackwardCursor: "last: 10, before: $cursor - paginate backward",
		CursorField:    "<table>_cursor returns encrypted cursor for next/prev page",
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
		},
		Usage:     "{ products { count_id sum_price avg_price } }",
		WithGroup: "{ products(distinct: [category_id]) { category_id count_id sum_price } } - group by category",
	},
	Recursive: RecursiveSyntax{
		FindParents:  "comments(find: \"parents\") - walks up the tree via self-referencing FK",
		FindChildren: "comments(find: \"children\") - walks down the tree via self-referencing FK",
	},
	FullTextSearch: "products(search: \"search term\") - uses database full-text search",
	Directives: map[string]string{
		"@include(ifRole:)":   "Include field if user has specified role",
		"@skip(ifRole:)":      "Skip field if user has specified role",
		"@include(ifVar:)":    "Include field if variable is true",
		"@skip(ifVar:)":       "Skip field if variable is true",
		"@object":             "Return single object instead of array",
		"@schema(name:)":      "Use specific database schema",
		"@through(table:)":    "Specify join table for many-to-many",
		"@notRelated":         "Disable automatic relationship detection for a field",
		"@cacheControl(maxAge:)": "Set cache TTL in seconds for this query",
	},
	Variables: VariablesSyntax{
		Declaration: "Variables are declared with $ prefix and passed via variables parameter",
		Types:       []string{"$id: Int", "$name: String", "$ids: [Int]", "$active: Boolean"},
		Example:     "query($id: Int!) { products(id: $id) { name } }",
	},
	JSONPaths: "For JSONB columns, use underscore notation: metadata_key_subkey maps to metadata->'key'->'subkey'",
	CommonMistakes: []MistakeExample{
		{Wrong: `where: { price: { gt: "50" } }`, Right: `where: { price: { gt: 50 } }`, Reason: "Numeric operators need numbers, not strings"},
		{Wrong: `where: { id: { in: 1 } }`, Right: `where: { id: { in: [1] } }`, Reason: "in/nin operators need arrays, even for single values"},
		{Wrong: `where: { name: { ilike: "test" } }`, Right: `where: { name: { ilike: "%test%" } }`, Reason: "ilike needs % wildcards for partial matching"},
		{Wrong: `products(id: 1)`, Right: `products(where: { id: { eq: 1 } })`, Reason: "Filtering requires where clause with operators (except for shorthand id lookup)"},
		{Wrong: `where: { is_active: { eq: "true" } }`, Right: `where: { is_active: { eq: true } }`, Reason: "Boolean values must be true/false, not strings"},
		{Wrong: `products(first: 10) { products_cursor }`, Right: `products(first: 10) { id } products_cursor`, Reason: "Cursor field must be at query root level, not inside the selection"},
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
		},
		Pagination: []QueryExample{
			{Description: "Limit and offset", Query: "{ products(limit: 10, offset: 20) { id name } }"},
			{Description: "Forward cursor pagination", Query: "{ products(first: 10, after: $cursor) { id name } products_cursor }"},
			{Description: "Backward cursor pagination", Query: "{ products(last: 10, before: $cursor) { id name } products_cursor }"},
			{Description: "First page with cursor", Query: "{ products(first: 10) { id name } products_cursor }", Variables: "{}"},
			{Description: "Distinct results", Query: "{ products(distinct: [category_id]) { category_id } }"},
		},
		Aggregations: []QueryExample{
			{Description: "Count records", Query: "{ products { count_id } }"},
			{Description: "Sum values", Query: "{ products { sum_price } }"},
			{Description: "Multiple aggregations", Query: "{ products { count_id sum_price avg_price min_price max_price } }"},
			{Description: "Aggregations with grouping", Query: "{ products(distinct: [category_id]) { category_id count_id sum_price avg_price } }"},
			{Description: "Get table statistics", Query: "{ products { count_id min_price max_price avg_price } }"},
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
	},
}

// mutationSyntaxReference is the static reference data for mutation syntax
var mutationSyntaxReference = MutationSyntaxReference{
	Operations: MutationOperations{
		Insert:      "products(insert: { name: \"New\", price: 10 })",
		BulkInsert:  "products(insert: $items) - where $items is an array of objects",
		Update:      "products(id: $id, update: { name: \"Updated\" })",
		BulkUpdate:  "products(update: $items) - where $items is array with id + fields to update",
		UpdateWhere: "products(where: { price: { lt: 10 } }, update: { on_sale: true })",
		Upsert:      "products(upsert: { id: $id, name: \"Name\" }) - insert or update based on id",
		Delete:      "products(delete: true, where: { id: { eq: $id } })",
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
	},
	Examples: []QueryExample{
		{Description: "Simple insert", Query: "mutation { users(insert: { email: $email }) { id } }"},
		{Description: "Bulk insert", Query: "mutation { products(insert: $items) { id name } }", Variables: `{"items": [{"name": "A", "price": 10}, {"name": "B", "price": 20}]}`},
		{Description: "Insert with nested create", Query: "mutation { purchases(insert: { quantity: 1, product: { name: $name, price: $price } }) { id } }"},
		{Description: "Update by ID", Query: "mutation { products(id: $id, update: { price: $price }) { id price } }"},
		{Description: "Update with where clause", Query: "mutation { products(where: { category: { eq: \"sale\" } }, update: { discount: 10 }) { id } }"},
		{Description: "Upsert (insert or update)", Query: "mutation { products(upsert: { id: $id, name: $name }) { id name } }"},
		{Description: "Delete by ID", Query: "mutation { products(delete: true, where: { id: { eq: $id } }) { id } }"},
		{Description: "Connect existing record", Query: "mutation { products(insert: { name: $name, owner: { connect: { id: $owner_id } } }) { id } }"},
		{Description: "Disconnect relationship", Query: "mutation { users(id: $id, update: { products: { disconnect: { id: $product_id } } }) { id } }"},
	},
}

// registerSyntaxTools registers the syntax reference tools
func (ms *mcpServer) registerSyntaxTools() {
	// get_query_syntax - Returns GraphJin query DSL reference
	ms.srv.AddTool(mcp.NewTool(
		"get_query_syntax",
		mcp.WithDescription("Get GraphJin query syntax reference. CALL THIS FIRST before writing queries. GraphJin uses its own DSL that differs from standard GraphQL."),
	), ms.handleGetQuerySyntax)

	// get_mutation_syntax - Returns GraphJin mutation DSL reference
	ms.srv.AddTool(mcp.NewTool(
		"get_mutation_syntax",
		mcp.WithDescription("Get GraphJin mutation syntax reference. Call this before writing mutations to learn insert, update, upsert, delete, and nested mutation syntax."),
	), ms.handleGetMutationSyntax)
}

// handleGetQuerySyntax returns the query syntax reference
func (ms *mcpServer) handleGetQuerySyntax(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(querySyntaxReference, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// handleGetMutationSyntax returns the mutation syntax reference
func (ms *mcpServer) handleGetMutationSyntax(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(mutationSyntaxReference, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}
