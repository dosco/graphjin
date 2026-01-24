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
	Aggregations     []string               `json:"aggregations"`
	Recursive        RecursiveSyntax        `json:"recursive"`
	FullTextSearch   string                 `json:"full_text_search"`
	Directives       map[string]string      `json:"directives"`
	JSONPaths        string                 `json:"json_paths"`
	Examples         QueryExamplesForSyntax `json:"examples"`
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
	LimitOffset string `json:"limit_offset"`
	Cursor      string `json:"cursor"`
	CursorField string `json:"cursor_field"`
	Distinct    string `json:"distinct"`
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
	Validation        ValidationSyntax   `json:"validation"`
	Examples          []QueryExample     `json:"examples"`
}

// MutationOperations shows mutation operation syntax
type MutationOperations struct {
	Insert      string `json:"insert"`
	BulkInsert  string `json:"bulk_insert"`
	Update      string `json:"update"`
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
		LimitOffset: "limit: 10, offset: 20",
		Cursor:      "first: 10, after: $cursor",
		CursorField: "<table>_cursor returns encrypted cursor for next page",
		Distinct:    "distinct: [column1, column2]",
	},
	Ordering: OrderingSyntax{
		Simple:     "order_by: { price: desc }",
		Multiple:   "order_by: { price: desc, id: asc }",
		Nested:     "order_by: { owner: { name: asc } }",
		CustomList: "order_by: { id: [$list, \"asc\"] }",
		NullsFirst: "order_by: { price: { dir: desc, nulls: first } }",
		NullsLast:  "order_by: { price: { dir: asc, nulls: last } }",
	},
	Aggregations: []string{
		"count_<column>",
		"sum_<column>",
		"avg_<column>",
		"min_<column>",
		"max_<column>",
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
	},
	JSONPaths: "For JSONB columns, use underscore notation: metadata_key_subkey maps to metadata->'key'->'subkey'",
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
			{Description: "Cursor pagination", Query: "{ products(first: 10, after: $cursor) { id name } products_cursor }"},
			{Description: "Distinct results", Query: "{ products(distinct: [category_id]) { category_id } }"},
		},
		Aggregations: []QueryExample{
			{Description: "Count records", Query: "{ products { count_id } }"},
			{Description: "Sum values", Query: "{ products { sum_price } }"},
			{Description: "Multiple aggregations", Query: "{ products { count_id sum_price avg_price min_price max_price } }"},
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
		BulkInsert:  "products(insert: $items) - where $items is an array",
		Update:      "products(id: $id, update: { name: \"Updated\" })",
		UpdateWhere: "products(where: { price: { lt: 10 } }, update: { on_sale: true })",
		Upsert:      "products(upsert: { id: $id, name: \"Name\" }) - insert or update",
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
	Validation: ValidationSyntax{
		Directive: "@constraint",
		Options:   []string{"format", "min", "max", "required", "requiredIf", "greaterThan", "lessThan"},
		Example:   "mutation @constraint(variable: \"email\", format: \"email\") { users(insert: { email: $email }) { id } }",
	},
	Examples: []QueryExample{
		{Description: "Simple insert", Query: "mutation { users(insert: { email: $email }) { id } }"},
		{Description: "Insert with nested create", Query: "mutation { purchases(insert: { quantity: 1, product: { name: $name, price: $price } }) { id } }"},
		{Description: "Update by ID", Query: "mutation { products(id: $id, update: { price: $price }) { id price } }"},
		{Description: "Upsert (insert or update)", Query: "mutation { products(upsert: { id: $id, name: $name }) { id name } }"},
		{Description: "Delete", Query: "mutation { products(delete: true, where: { id: { eq: $id } }) { id } }"},
		{Description: "Connect existing record", Query: "mutation { products(insert: { name: $name, owner: { connect: { id: $owner_id } } }) { id } }"},
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
