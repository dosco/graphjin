package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
)

const errNoDB = "No databases have been configured yet. " +
	"Use the discover_databases tool to find available databases, " +
	"then update_current_config to set up a connection."

// requireDB checks that GraphJin is initialized with a usable schema.
// Returns an error result if not ready, or nil if ready to proceed.
func (ms *mcpServer) requireDB() *mcp.CallToolResult {
	if ms.service.gj == nil || !ms.service.gj.SchemaReady() {
		return mcp.NewToolResultError(errNoDB)
	}
	return nil
}

// registerSchemaTools registers the schema discovery tools
func (ms *mcpServer) registerSchemaTools() {
	sourcesUsed := ms.service.conf.Core.IsSourcesUsed()
	if !sourcesUsed && ms.service.conf.legacyMCPToolsEnabled() {
		// list_namespaces - One-query rollup of (database, schema) namespaces
		ms.srv.AddTool(mcp.NewTool(
			"list_namespaces",
			mcp.WithDescription("Legacy discovery tool. Prefer query_catalog. List namespaces (databases/schemas) with table counts and approximate row totals."),
			mcp.WithString("database",
				mcp.Description("Optional database name. Omit to roll up across all configured databases."),
			),
			mcp.WithOutputSchema[ListNamespacesResult](),
		), ms.handleListNamespaces)

		// list_tables - List all database tables
		ms.srv.AddTool(mcp.NewTool(
			"list_tables",
			mcp.WithDescription("Legacy discovery tool. Prefer query_catalog(where: {kind: {eq: 'table'}}). List all database tables."),
			mcp.WithString("namespace",
				mcp.Description("Optional namespace for multi-tenant deployments"),
			),
			mcp.WithString("database",
				mcp.Description("Optional database name to filter tables. Omit to see tables from ALL databases."),
			),
			mcp.WithString("schema",
				mcp.Description("Optional database schema name to filter tables."),
			),
			mcp.WithString("search",
				mcp.Description("Optional case-insensitive search across table, schema, database, and comment."),
			),
			mcp.WithNumber("limit",
				mcp.Description("Maximum tables to return. Defaults to 100, max 500."),
				mcp.Min(1),
				mcp.Max(500),
			),
			mcp.WithString("cursor",
				mcp.Description("Pagination cursor from a previous list_tables response."),
			),
			mcp.WithOutputSchema[ListTablesResult](),
		), ms.handleListTables)

		// describe_table - Get detailed table schema with relationships
		ms.srv.AddTool(mcp.NewTool(
			"describe_table",
			mcp.WithDescription("Legacy discovery tool. Prefer query_catalog(id: ...) on a table item. Get detailed schema for a table."),
			mcp.WithString("table",
				mcp.Required(),
				mcp.Description("Name of the table to describe"),
			),
			mcp.WithString("namespace",
				mcp.Description("Optional namespace for multi-tenant deployments"),
			),
			mcp.WithString("database",
				mcp.Description("Optional database name. Omit to search all databases."),
			),
			mcp.WithString("schema",
				mcp.Description("Optional database schema name. Use with database to disambiguate duplicate table names across schemas."),
			),
			mcp.WithOutputSchema[TableSchemaWithAggregations](),
		), ms.handleDescribeTable)

		// find_path - Find relationship path between tables
		ms.srv.AddTool(mcp.NewTool(
			"find_path",
			mcp.WithDescription("Legacy discovery tool. Prefer relationship items in query_catalog. Find relationship path between two tables."),
			mcp.WithString("from_table",
				mcp.Required(),
				mcp.Description("Starting table name"),
			),
			mcp.WithString("to_table",
				mcp.Required(),
				mcp.Description("Target table name"),
			),
			mcp.WithString("database",
				mcp.Description("Optional database name. Omit to search all databases."),
			),
			mcp.WithOutputSchema[struct {
				Path                          []core.PathStep      `json:"path"`
				ExampleQuery                  string               `json:"example_query"`
				ExampleQueryCompiles          bool                 `json:"example_query_compiles"`
				ExampleQueryWarning           *FixQueryErrorResult `json:"example_query_warning,omitempty"`
				CollapsedExampleQuery         string               `json:"collapsed_example_query,omitempty"`
				CollapsedExampleQueryCompiles bool                 `json:"collapsed_example_query_compiles,omitempty"`
				CollapsedExampleQueryWarning  *FixQueryErrorResult `json:"collapsed_example_query_warning,omitempty"`
				CollapsedNote                 string               `json:"collapsed_note,omitempty"`
			}](),
		), ms.handleFindPath)

		ms.srv.AddTool(mcp.NewTool(
			"get_table_sample",
			mcp.WithDescription("Legacy discovery tool. Prefer catalog item details. Get live-data statistics and sample rows for one table."),
			mcp.WithString("table",
				mcp.Required(),
				mcp.Description("Table name to sample"),
			),
			mcp.WithString("database",
				mcp.Description("Database name. Required when the table name is ambiguous across configured databases."),
			),
			mcp.WithString("schema",
				mcp.Description("Optional database schema name."),
			),
			mcp.WithOutputSchema[TableSampleResult](),
		), ms.handleGetTableSample)
	}

	// validate_where_clause - Validate where clause syntax and type compatibility
	ms.srv.AddTool(mcp.NewTool(
		"validate_where_clause",
		mcp.WithDescription("Validate a where clause for syntax and type compatibility. "+
			"Checks that operators match column types and returns detailed error messages with suggestions. "+
			"Use this to verify your filter logic before including it in a full query."),
		mcp.WithString("table",
			mcp.Required(),
			mcp.Description("Table name to validate against"),
		),
		mcp.WithObject("where",
			mcp.Required(),
			mcp.Description("The where clause object to validate (for example: { price: { gt: 50 } }). "+
				"Legacy callers may still pass a JSON string."),
		),
		mcp.WithString("database",
			mcp.Description("Optional database name. Omit to search all databases."),
		),
	), ms.handleValidateWhereClause)

	if !sourcesUsed && ms.service.conf.legacyMCPToolsEnabled() {
		// get_workflow_guide - Returns recommended workflow for using MCP tools
		ms.srv.AddTool(mcp.NewTool(
			"get_workflow_guide",
			mcp.WithDescription("Legacy planning tool. Prefer get_catalog_entrypoints. Get the recommended workflow for using GraphJin MCP tools effectively."),
		), ms.handleGetWorkflowGuide)

		// get_discovery_schema - JSON schemas for every discovery tool's response.
		ms.srv.AddTool(mcp.NewTool(
			"get_discovery_schema",
			mcp.WithDescription("Legacy discovery schema tool. Prefer query_catalog and query_catalog(id: ...) output schemas."),
		), ms.handleGetDiscoverySchema)
	}

	// reload_schema - Only registered when allow_schema_reload is true
	if !sourcesUsed && ms.service.conf.MCP.AllowSchemaReload {
		ms.srv.AddTool(mcp.NewTool(
			"reload_schema",
			mcp.WithDescription("Reload the database schema to discover new or modified tables. "+
				"Use this tool when: (1) the user says a table exists but query_catalog(where: {kind: {eq: 'table'}}) doesn't show it, "+
				"(2) the user has just created new tables or modified the database structure, "+
				"(3) the user explicitly asks to reload, refresh, or recheck the database schema. "+
				"This triggers immediate discovery without waiting for the automatic polling interval."),
		), ms.handleReloadSchema)
	}
}

func (ms *mcpServer) handleListTables(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := ms.requireDB(); err != nil {
		return err, nil
	}
	args := req.GetArguments()
	database, _ := args["database"].(string)
	opts := tableListOptionsFromArgs(args)

	if ms.service.disc != nil {
		result := ms.service.disc.TableIndexPage(ctx, database, opts)
		return ms.toolResultJSON("list_tables", args, result)
	}

	var entries []TableIndexEntry
	var tables []core.TableInfo
	if database != "" {
		tables = ms.service.gj.GetTablesForDatabase(database)
	} else {
		tables = ms.service.gj.GetTables()
	}
	for _, t := range tables {
		entries = append(entries, TableIndexEntry{
			Name:        t.Name,
			Schema:      t.Schema,
			Database:    t.Database,
			Type:        t.Type,
			Comment:     t.Comment,
			ColumnCount: t.ColumnCount,
		})
	}

	entries = filterTableIndex(entries, opts)
	page, nextCursor, hasMore := paginateTableIndex(entries, opts)
	result := ListTablesResult{
		Database:   database,
		Tables:     page,
		Count:      len(page),
		Total:      len(entries),
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}
	return ms.toolResultJSON("list_tables", args, result)
}

func tableListOptionsFromArgs(args map[string]any) TableListOptions {
	search, _ := args["search"].(string)
	schemaName, _ := args["schema"].(string)
	cursor, _ := args["cursor"].(string)
	return TableListOptions{
		Search: search,
		Schema: schemaName,
		Limit:  intArg(args["limit"]),
		Cursor: cursor,
	}
}

func intArg(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case float32:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	default:
		return 0
	}
}

// AggregationInfo describes available aggregation functions for a table
type AggregationInfo struct {
	Available   []string `json:"available"`
	Usage       string   `json:"usage"`
	Limitations []string `json:"limitations,omitempty"`
}

// TableSchemaWithAggregations extends TableSchema with aggregation information
type TableSchemaWithAggregations struct {
	*core.TableSchema
	Aggregations   AggregationInfo `json:"aggregations"`
	ExampleQueries []ExampleQuery  `json:"example_queries,omitempty"`
}

// ExampleQuery represents an example GraphQL query for a table
type ExampleQuery struct {
	Description string `json:"description"`
	Query       string `json:"query"`
}

// handleDescribeTable returns detailed schema for a table including aggregations
func (ms *mcpServer) handleDescribeTable(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := ms.requireDB(); err != nil {
		return err, nil
	}
	args := req.GetArguments()
	table, _ := args["table"].(string)
	database, _ := args["database"].(string)
	schemaName, _ := args["schema"].(string)

	if table == "" {
		return mcp.NewToolResultError("table name is required"), nil
	}

	var schema *core.TableSchema
	var err error
	if database != "" || schemaName != "" {
		schema, err = ms.service.gj.GetTableSchemaForDatabaseSchema(database, schemaName, table)
	} else {
		schema, err = ms.service.gj.GetTableSchema(table)
	}
	if err != nil {
		return mcp.NewToolResultError(enhanceError(err.Error(), "describe_table")), nil
	}

	// Generate available aggregations based on column types
	aggregations := generateAggregations(schema)

	// Generate example queries
	examples := generateExampleQueries(schema)

	result := TableSchemaWithAggregations{
		TableSchema:    schema,
		Aggregations:   aggregations,
		ExampleQueries: examples,
	}
	return ms.toolResultJSON("describe_table", args, result)
}

func (ms *mcpServer) handleGetDiscoverySchema(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	return ms.toolResultJSON("get_discovery_schema", args, discoverySchema())
}

func (ms *mcpServer) handleListNamespaces(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// list_namespaces is intentionally NOT gated on SchemaReady — its
	// whole purpose is to help when the schema cache is empty, by
	// surfacing what databases/schemas exist so the caller can pick
	// a target. It only needs a live DB connection (which DiscoveryManager
	// reaches via gj.DBForDatabase).
	if ms.service.disc == nil {
		return mcp.NewToolResultError("Discovery not available yet."), nil
	}
	args := req.GetArguments()
	database, _ := args["database"].(string)

	rollup := ms.service.disc.Namespaces(ctx, database)
	result := ListNamespacesResult{
		Database:   database,
		Namespaces: rollup,
		Count:      len(rollup),
	}
	return ms.toolResultJSON("list_namespaces", args, result)
}

func (ms *mcpServer) handleGetTableSample(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := ms.requireDB(); err != nil {
		return err, nil
	}
	if ms.service.disc == nil {
		return mcp.NewToolResultError("Discovery not available yet."), nil
	}
	args := req.GetArguments()
	table, _ := args["table"].(string)
	database, _ := args["database"].(string)
	schemaName, _ := args["schema"].(string)
	if table == "" {
		return mcp.NewToolResultError("table name is required"), nil
	}

	result, err := ms.service.disc.TableSample(ctx, database, schemaName, table)
	if err != nil {
		return mcp.NewToolResultError(enhanceError(err.Error(), "get_table_sample")), nil
	}
	return ms.toolResultJSON("get_table_sample", args, result)
}

// generateAggregations creates the list of available aggregation functions based on column types
func generateAggregations(schema *core.TableSchema) AggregationInfo {
	var available []string

	for _, col := range schema.Columns {
		// All columns support count
		available = append(available, fmt.Sprintf("count_%s", col.Name))

		// Numeric columns support sum, avg, min, max
		normalizedType := normalizeColumnType(col.Type)
		if normalizedType == "numeric" {
			available = append(available,
				fmt.Sprintf("sum_%s", col.Name),
				fmt.Sprintf("avg_%s", col.Name),
				fmt.Sprintf("min_%s", col.Name),
				fmt.Sprintf("max_%s", col.Name),
			)
		}
	}

	return AggregationInfo{
		Available: available,
		Usage: fmt.Sprintf(
			"Single column: { %s { count_id sum_<numeric_col> avg_<numeric_col> } }. "+
				"Arithmetic across columns (revenue, margin, ratios): "+
				"{ %s { revenue: sum(expr: { mul: [col_a, col_b] }) } }. "+
				"For metric-by-dimension shape (revenue by category, etc.) root at the dimension table — see get_query_syntax.patterns. "+
				"On compile error, pass query+error to fix_query_error for a structured repair.",
			schema.Name, schema.Name),
		Limitations: aggregationLimitations(),
	}
}

// aggregationLimitations enumerates the known compose-failures small
// models might otherwise retry blindly. Static — same list for every
// table. Each entry pairs the constraint with how to detect/repair it,
// so the response is self-describing rather than requiring the agent to
// hit the failure first.
func aggregationLimitations() []string {
	return []string{
		"order_by does not work on aggregate aliases (sum_*, count_*, custom names from sum(expr:)). Sort aggregated results in workflow JavaScript.",
		"Aggregates at non-root nested levels work but the GROUP BY happens at the root selection only. distinct: dedupes rows; it does not bucket.",
		"Nesting a join through a column that is not in distinct: of an aggregating select is rejected at compile time — root at the dimension table instead. See get_query_syntax.patterns.metric_by_dimension.",
		"Recursive (find:) selections cannot contain aggregate fields at the same level — fold via parent if needed.",
		"MongoDB does not support expression aggregates (sum(expr:)) in the v1 driver — fall back to per-column aggregates.",
	}
}

// generateExampleQueries creates example GraphQL queries for a table
func generateExampleQueries(schema *core.TableSchema) []ExampleQuery {
	var examples []ExampleQuery
	name := schema.Name

	// Collect column names for the basic query (up to 5)
	var colNames []string
	for _, col := range schema.Columns {
		colNames = append(colNames, col.Name)
		if len(colNames) >= 5 {
			break
		}
	}
	colList := "id"
	if len(colNames) > 0 {
		colList = strings.Join(colNames, " ")
	}

	// 1. Basic fetch
	examples = append(examples, ExampleQuery{
		Description: fmt.Sprintf("Fetch %s with limit", name),
		Query:       fmt.Sprintf("{ %s(limit: 10) { %s } }", name, colList),
	})

	// 2. Relationship join (if any relationships exist)
	allRels := append(schema.Relationships.Outgoing, schema.Relationships.Incoming...)
	if len(allRels) > 0 {
		rel := allRels[0]
		examples = append(examples, ExampleQuery{
			Description: fmt.Sprintf("Fetch %s with related %s", name, rel.Table),
			Query:       fmt.Sprintf("{ %s(limit: 10) { %s %s { id } } }", name, colList, rel.Table),
		})
	}

	// 3. Aggregation (if numeric columns exist)
	for _, col := range schema.Columns {
		normalizedType := normalizeColumnType(col.Type)
		if normalizedType == "numeric" {
			examples = append(examples, ExampleQuery{
				Description: fmt.Sprintf("Aggregate %s statistics", name),
				Query:       fmt.Sprintf("{ %s { count_id sum_%s avg_%s } }", name, col.Name, col.Name),
			})
			break
		}
	}

	return examples
}

// handleFindPath finds the relationship path between two tables
func (ms *mcpServer) handleFindPath(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := ms.requireDB(); err != nil {
		return err, nil
	}
	args := req.GetArguments()
	fromTable, _ := args["from_table"].(string)
	toTable, _ := args["to_table"].(string)
	database, _ := args["database"].(string)

	if fromTable == "" || toTable == "" {
		return mcp.NewToolResultError("both from_table and to_table are required"), nil
	}

	var path []core.PathStep
	var err error
	if database != "" {
		path, err = ms.service.gj.FindRelationshipPathForDatabase(database, fromTable, toTable)
	} else {
		path, err = ms.service.gj.FindRelationshipPath(fromTable, toTable)
	}
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Generate an example query that uses each step's actual PK column
	// (small models would otherwise copy literal `id` and trip on tables
	// whose PK is named differently — e.g. AdventureWorks productcategoryid).
	// validateExampleQuery still runs as a belt-and-suspenders check.
	exampleQuery := generatePathExampleQuery(fromTable, path, ms.resolvePKColumn)
	compiles, warning := ms.validateExampleQuery(exampleQuery)

	// When the path has intermediates, emit a collapsed `{ <from> { <to> } }` shape that GraphJin auto-traverses.
	var (
		collapsedQuery    string
		collapsedCompiles bool
		collapsedWarning  *FixQueryErrorResult
		collapsedNote     string
	)
	if len(path) >= 2 {
		toTable := path[len(path)-1].To
		collapsedQuery = generatePathExampleQuery(fromTable,
			[]core.PathStep{{To: toTable}}, ms.resolvePKColumn)
		collapsedCompiles, collapsedWarning = ms.validateExampleQuery(collapsedQuery)
		if collapsedCompiles {
			collapsedNote = "GraphJin auto-traverses the multi-hop FK path; you can nest `" +
				fromTable + "` and `" + toTable + "` directly. Use this collapsed form for " +
				"per-dimension aggregations (see get_query_syntax.patterns.metric_by_dimension)."
		} else {
			collapsedNote = "Auto-traversal between `" + fromTable + "` and `" + path[len(path)-1].To +
				"` did not compile on this schema (see collapsed_example_query_warning); " +
				"use the full nested example_query instead."
		}
	}

	result := struct {
		Path                          []core.PathStep      `json:"path"`
		ExampleQuery                  string               `json:"example_query"`
		ExampleQueryCompiles          bool                 `json:"example_query_compiles"`
		ExampleQueryWarning           *FixQueryErrorResult `json:"example_query_warning,omitempty"`
		CollapsedExampleQuery         string               `json:"collapsed_example_query,omitempty"`
		CollapsedExampleQueryCompiles bool                 `json:"collapsed_example_query_compiles,omitempty"`
		CollapsedExampleQueryWarning  *FixQueryErrorResult `json:"collapsed_example_query_warning,omitempty"`
		CollapsedNote                 string               `json:"collapsed_note,omitempty"`
	}{
		Path:                          path,
		ExampleQuery:                  exampleQuery,
		ExampleQueryCompiles:          compiles,
		ExampleQueryWarning:           warning,
		CollapsedExampleQuery:         collapsedQuery,
		CollapsedExampleQueryCompiles: collapsedCompiles,
		CollapsedExampleQueryWarning:  collapsedWarning,
		CollapsedNote:                 collapsedNote,
	}
	return ms.toolResultJSON("find_path", args, result)
}

// validateExampleQuery runs the generated example through ExplainQuery
// (compile-only, no execution) and returns either (true, nil) or
// (false, structured-warning) depending on whether it compiles. Reuses
// the fix_query_error machinery so the warning carries the same Kind /
// Diagnosis / RepairedQuery shape the agent already knows how to read.
//
// ExplainQuery returns (explanation, nil) for COMPILE errors — it stuffs
// the message into explanation.Errors rather than the Go error return —
// so we have to check both channels.
func (ms *mcpServer) validateExampleQuery(exampleQuery string) (bool, *FixQueryErrorResult) {
	if exampleQuery == "" {
		return false, nil
	}
	if ms == nil || ms.service == nil || ms.service.gj == nil {
		return false, nil
	}
	exp, err := ms.service.gj.ExplainQuery(exampleQuery, nil, "")
	switch {
	case err != nil:
		repair := buildFixQueryErrorRepair(exampleQuery, err.Error(), ms.analyticsModeOn())
		return false, &repair
	case exp != nil && len(exp.Errors) > 0:
		repair := buildFixQueryErrorRepair(exampleQuery, exp.Errors[0], ms.analyticsModeOn())
		return false, &repair
	default:
		return true, nil
	}
}

// resolvePKColumn looks up a table's primary key column for example-query
// substitution. Returns "" when the table is not found (caller substitutes
// a placeholder).
func (ms *mcpServer) resolvePKColumn(table string) string {
	if ms == nil || ms.service == nil || ms.service.gj == nil {
		return ""
	}
	schema, err := ms.service.gj.GetTableSchema(table)
	if err != nil || schema == nil {
		return ""
	}
	if schema.PrimaryKey != "" {
		return schema.PrimaryKey
	}
	if len(schema.PrimaryKeys) > 0 {
		return schema.PrimaryKeys[0]
	}
	return ""
}

// generatePathExampleQuery generates a nested example GraphQL query along
// the resolved path. resolvePK is called per table to substitute the actual
// PK column name (falls back to "<pk_column>" when unknown).
func generatePathExampleQuery(from string, path []core.PathStep, resolvePK func(string) string) string {
	if len(path) == 0 {
		return ""
	}

	pkOrPlaceholder := func(table string) string {
		if resolvePK == nil {
			return "<pk_column>"
		}
		if pk := resolvePK(table); pk != "" {
			return pk
		}
		return "<pk_column>"
	}

	query := "{ " + from + " { " + pkOrPlaceholder(from) + " "
	for _, step := range path {
		query += step.To + " { " + pkOrPlaceholder(step.To) + " "
	}

	// Close all the braces
	for range path {
		query += "} "
	}
	query += "} }"

	return query
}

// getNamespace returns the configured namespace
func (ms *mcpServer) getNamespace() string {
	if ms.service.namespace != nil {
		return *ms.service.namespace
	}
	return ""
}

// WorkflowGuide contains the recommended workflow for using GraphJin MCP tools
type WorkflowGuide struct {
	QueryWorkflow      []string          `json:"query_workflow"`
	MutationWorkflow   []string          `json:"mutation_workflow"`
	Tips               []string          `json:"tips"`
	AnalyticsModeRules []string          `json:"analytics_mode_rules,omitempty"`
	QueryPatterns      []QueryPattern    `json:"query_patterns,omitempty"`
	ToolSequences      map[string]string `json:"tool_sequences"`
}

// handleGetWorkflowGuide returns the recommended workflow for MCP tool usage
func (ms *mcpServer) handleGetWorkflowGuide(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	toolSet := ms.availableToolSet()
	has := func(name string) bool { return toolSet[name] }
	guide := WorkflowGuide{
		ToolSequences: make(map[string]string),
	}

	guide.QueryWorkflow = append(guide.QueryWorkflow,
		"1. Call get_query_syntax to learn GraphJin DSL (it differs from standard GraphQL)",
	)
	if has("plan_database_setup") && has("test_database_connection") && has("apply_database_setup") {
		guide.QueryWorkflow = append(guide.QueryWorkflow,
			"1.5 If no database is configured, use plan_database_setup → test_database_connection → apply_database_setup",
		)
	}
	guide.QueryWorkflow = append(guide.QueryWorkflow,
		"2. Call list_tables to see available data",
		"3. Call describe_table for schema details + available aggregation functions",
		"4. Check list_saved_queries - a saved query may already exist for your need",
	)
	if has("write_query") {
		guide.QueryWorkflow = append(guide.QueryWorkflow,
			"4.5 Call write_query when you want a guided starter query with schema-aware examples",
		)
	}
	if has("execute_graphql") {
		guide.QueryWorkflow = append(guide.QueryWorkflow,
			"5. Prefer execute_saved_query when a matching saved query exists, otherwise call execute_graphql",
		)
	} else {
		guide.QueryWorkflow = append(guide.QueryWorkflow,
			"5. Raw queries are disabled, so inspect saved queries first and execute them with execute_saved_query",
		)
	}
	guide.QueryWorkflow = append(guide.QueryWorkflow,
		"6. For pagination, save cursor IDs from response for next page requests",
	)
	if has("get_js_runtime_api") && has("execute_workflow") {
		guide.QueryWorkflow = append(guide.QueryWorkflow,
			"7. For reusable orchestration, use gj_catalog/query_catalog where kind = workflow, then execute_workflow",
		)
	}

	guide.MutationWorkflow = append(guide.MutationWorkflow,
		"1. Call get_mutation_syntax to learn insert/update/upsert/delete syntax",
		"2. Call describe_table to understand required columns and types",
		"3. Check list_saved_queries for existing mutation queries",
	)
	if len(ms.service.selectedCodeSQLDatabases()) != 0 {
		guide.MutationWorkflow = append(guide.MutationWorkflow,
			"3.25 For CodeSQL replace/delete/rename edits, query gj_code code/code_context plus path/hash before drafting a mutation",
			"3.75 Preview CodeSQL replace/create/delete/rename edits with gj_code(kind: \"change_set\", action: \"preview\") before apply",
		)
	}
	if has("write_mutation") {
		guide.MutationWorkflow = append(guide.MutationWorkflow, "3.5 Call write_mutation for a guided mutation skeleton")
	}
	if has("execute_graphql") {
		guide.MutationWorkflow = append(guide.MutationWorkflow, "4. Call execute_graphql with the mutation")
	} else {
		guide.MutationWorkflow = append(guide.MutationWorkflow, "4. Raw mutations are disabled, so execute a saved mutation with execute_saved_query")
	}

	analyticsMode := ms.analyticsModeOn()
	if analyticsMode {
		guide.AnalyticsModeRules = analyticsModeRules()
	} else {
		guide.Tips = append(guide.Tips,
			"ALWAYS use execute_workflow for data questions — NEVER execute_graphql directly. Tables can have hundreds of thousands of rows and you cannot predict sizes.",
			"Every query level has a silent default row limit. Always set explicit limits on every level, especially nested children.",
		)
	}

	// Three universal query shapes. Surfaced unconditionally — patterns
	// are general DSL-shape guidance, not analytics-mode-specific.
	guide.QueryPatterns = canonicalQueryPatterns()

	guide.Tips = append(guide.Tips,
		"ALWAYS discover workflow items with query_catalog(where: {kind: {eq: 'workflow'}}) first — reuse an existing workflow if one fits the question.",
		"Queries inside workflows must be TOP-DOWN: start from the grouping/parent table, nest into children. NEVER filter bottom-up from leaf tables.",
		"order_by can target aggregation aliases when distinct is present and expression aggregate aliases; sort in workflow JavaScript only for metrics computed after query execution.",
		"Use distinct: [columns] for GROUP BY — group_by does not exist.",
		"Use analytics directives for reporting rows: @running, @moving, @previous, @next, @first, @last, @rank, @denseRank, and @rowNumber while keeping each input row visible.",
		"Use find_path or explore_relationships to discover join paths — NEVER guess at FK relationships.",
		"Aggregations like count_id, sum_price are available on all tables (see describe_table)",
		"Use the write_where_clause prompt or validate_where_clause tool for help building complex filters",
		"Use @object directive when you expect a single result: { user @object { id } }",
		"For multi-database deployments, use the `database` parameter in list_tables and describe_table to filter by database.",
	)
	if has("list_workflows") {
		guide.Tips = append(guide.Tips, "Legacy mode only: list_workflows is available as a migration shim, but query_catalog workflow items are the preferred discovery surface.")
	}
	if len(ms.service.selectedCodeSQLDatabases()) != 0 {
		guide.Tips = append(guide.Tips,
			"For CodeSQL source edits, read gj_code code/code_context first, then preview replace/create/delete/rename operations with gj_code kind change_set before apply.",
			"CodeSQL applies are guarded by expected_hash for replace/delete/rename, exact old_text for replacements, target-exists checks for creates, and lock overlap checks; stale previews must be requeried and regenerated.",
		)
	}
	if has("write_query") {
		guide.Tips = append(guide.Tips, "Use write_query to generate schema-aware starter queries when prompts/resources are not available in the client")
	}
	if has("write_mutation") {
		guide.Tips = append(guide.Tips, "Use write_mutation to generate insert/update/upsert/delete skeletons before execution")
	}
	if has("fix_query_error") {
		guide.Tips = append(guide.Tips, "Use fix_query_error with the exact query and error text after failed execute_graphql calls")
	}
	if has("update_current_config") {
		guide.Tips = append(guide.Tips, "Use resolvers to join DB tables with remote APIs - configure via update_current_config with resolvers parameter")
	}
	if has("update_current_config") || has("apply_database_setup") {
		guide.Tips = append(guide.Tips, "Prefer the `next` field returned by onboarding/config tools for machine-readable follow-up actions")
	}
	if has("plan_database_setup") {
		guide.Tips = append(guide.Tips, "Use plan_database_setup for ranked discover results and explicit candidate selection")
	}
	if has("test_database_connection") && has("apply_database_setup") {
		guide.Tips = append(guide.Tips, "Use test_database_connection before apply_database_setup when credentials are uncertain")
	}
	if has("explain_query") {
		guide.Tips = append(guide.Tips, "Use explain_query to see the exact compiled query that will run before executing — great for debugging and optimization")
	}
	if has("explore_relationships") {
		guide.Tips = append(guide.Tips, "Use explore_relationships to map out the data model neighborhood around any table")
	}
	if has("audit_role_permissions") {
		guide.Tips = append(guide.Tips, "Use audit_role_permissions to understand what each role can access")
	}
	if has("save_workflow") {
		guide.Tips = append(guide.Tips, "Use save_workflow to persist new workflows so future queries can reuse them")
	}
	if has("get_js_runtime_api") && has("save_workflow") {
		guide.Tips = append(guide.Tips, "Use get_js_runtime_api before authoring JS workflows so function names and argument schemas are exact")
	}
	if has("execute_workflow") {
		guide.Tips = append(guide.Tips, "Use execute_workflow to run ./workflows/<name>.js with variables passed as `input`")
	}

	addSequence := func(name string, required []string, sequence string) {
		for _, tool := range required {
			if !has(tool) {
				return
			}
		}
		guide.ToolSequences[name] = sequence
	}

	if has("plan_database_setup") && has("test_database_connection") && has("apply_database_setup") {
		addSequence("db_onboarding_guided",
			[]string{"discover_databases", "plan_database_setup", "test_database_connection", "apply_database_setup", "query_catalog"},
			"discover_databases → plan_database_setup → test_database_connection → apply_database_setup → query_catalog(where: {kind: {eq: 'table'}})",
		)
	}
	if has("execute_workflow") {
		addSequence("data_question",
			[]string{"query_catalog", "execute_workflow"},
			"query_catalog(where: {kind: {eq: 'workflow'}}) → query_catalog(id: '<workflow_id>') → execute_workflow (ALWAYS use workflows for data questions)",
		)
	}
	if has("execute_graphql") {
		addSequence("mutation",
			[]string{"query_catalog", "execute_graphql"},
			"query_catalog(where: {kind: {eq: 'mutation_pattern'}}) → query_catalog(id: '<mutation_pattern_id>') → execute_graphql",
		)
		if len(ms.service.selectedCodeSQLDatabases()) != 0 {
			guide.ToolSequences["codesql_mutation"] = "query_catalog(search: 'CodeSQL') → execute_graphql(query code/code_context first) → write_mutation → execute_graphql(preview) → execute_graphql(apply)"
		}
		addSequence("multi_database_exploration",
			[]string{"query_catalog", "execute_graphql"},
			"query_catalog(where: {kind: {eq: 'table'}, database_name: {eq: 'db_name'}}) → query_catalog(id: '<table_id>') → execute_graphql",
		)
	} else {
		if has("list_saved_queries") && has("get_saved_query") && has("execute_saved_query") {
			addSequence("saved_query_only",
				[]string{"list_saved_queries", "get_saved_query", "execute_saved_query"},
				"list_saved_queries → get_saved_query → execute_saved_query",
			)
			addSequence("mutation",
				[]string{"list_saved_queries", "get_saved_query", "execute_saved_query"},
				"list_saved_queries → get_saved_query → execute_saved_query",
			)
		} else if has("query_catalog") && has("execute_saved_query") {
			addSequence("saved_query_only",
				[]string{"query_catalog", "execute_saved_query"},
				"query_catalog(where: {kind: {eq: 'saved_query'}}) → query_catalog(id: '<saved_query_id>') → execute_saved_query",
			)
			addSequence("mutation",
				[]string{"query_catalog", "execute_saved_query"},
				"query_catalog(where: {kind: {eq: 'saved_query'}}) → query_catalog(id: '<saved_query_id>') → execute_saved_query",
			)
		} else if has("execute_saved_query") {
			guide.ToolSequences["saved_query_only"] = "execute_saved_query"
			guide.ToolSequences["mutation"] = "execute_saved_query"
		}
	}
	if has("list_saved_queries") && has("get_saved_query") && has("execute_saved_query") {
		addSequence("use_saved_query",
			[]string{"list_saved_queries", "get_saved_query", "execute_saved_query"},
			"list_saved_queries → get_saved_query → execute_saved_query",
		)
	} else if has("query_catalog") && has("execute_saved_query") {
		addSequence("use_saved_query",
			[]string{"query_catalog", "execute_saved_query"},
			"query_catalog(where: {kind: {eq: 'saved_query'}}) → query_catalog(id: '<saved_query_id>') → execute_saved_query",
		)
	}
	addSequence("explore_schema",
		[]string{"query_catalog"},
		"query_catalog(where: {kind: {eq: 'table'}}) → query_catalog(id: '<catalog_item_id>') for each relevant table/relationship",
	)
	addSequence("build_where_clause",
		[]string{"query_catalog", "validate_where_clause"},
		"query_catalog(where: {kind: {eq: 'column'}}) → use write_where_clause prompt → validate_where_clause",
	)
	if has("write_query") && has("validate_where_clause") && has("execute_graphql") {
		addSequence("query_authoring",
			[]string{"query_catalog", "write_query", "validate_where_clause"},
			"query_catalog → query_catalog(id: '<catalog_item_id>') → write_query → validate_where_clause → execute_graphql",
		)
	}
	if has("write_mutation") && has("execute_graphql") {
		addSequence("mutation_authoring",
			[]string{"query_catalog", "write_mutation"},
			"query_catalog(where: {kind: {eq: 'mutation_pattern'}}) → query_catalog(id: '<mutation_pattern_id>') → write_mutation → execute_graphql",
		)
	}
	if has("fix_query_error") && has("execute_graphql") {
		addSequence("query_repair",
			[]string{"fix_query_error", "query_catalog"},
			"fix_query_error → query_catalog → execute_graphql",
		)
	}
	if has("get_current_config") && has("update_current_config") && has("reload_schema") && has("execute_graphql") {
		addSequence("configure_resolver",
			[]string{"get_current_config", "update_current_config", "reload_schema", "execute_graphql"},
			"dev mode: get_current_config(section: resolvers) → update_current_config(resolvers: [...]) → reload_schema → execute_graphql",
		)
	}
	if has("explain_query") && has("execute_graphql") {
		addSequence("debug_query",
			[]string{"explain_query", "execute_graphql"},
			"explain_query → (fix issues) → execute_graphql",
		)
	}
	if has("explore_relationships") {
		addSequence("explore_data_model",
			[]string{"list_tables", "explore_relationships", "describe_table"},
			"list_tables → explore_relationships(depth: 2) → describe_table",
		)
	}
	if has("audit_role_permissions") && has("update_current_config") {
		addSequence("security_audit",
			[]string{"audit_role_permissions", "update_current_config"},
			"audit_role_permissions(role: 'all') → update_current_config(roles: [...]) → audit_role_permissions (verify)",
		)
	}
	if has("execute_workflow") {
		addSequence("js_workflow_reuse",
			[]string{"query_catalog", "execute_workflow"},
			"query_catalog(where: {kind: {eq: 'workflow'}}) → query_catalog(id: '<workflow_id>') → execute_workflow",
		)
	}
	if has("save_workflow") {
		addSequence("js_workflow_authoring",
			[]string{"get_js_runtime_api", "query_catalog", "save_workflow", "execute_workflow"},
			"query_catalog(where: {kind: {eq: 'workflow'}}) → get_js_runtime_api → query_catalog(id: '<workflow_id>') → save_workflow → execute_workflow",
		)
	}
	if has("list_workflows") {
		addSequence("legacy_js_workflow_discovery",
			[]string{"list_workflows", "execute_workflow"},
			"legacy only: list_workflows → execute_workflow",
		)
	}
	return ms.toolResultJSON("get_workflow_guide", req.GetArguments(), guide)
}

// handleReloadSchema triggers a schema reload
func (ms *mcpServer) handleReloadSchema(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := ms.requireDB(); err != nil {
		return err, nil
	}
	err := ms.service.gj.Reload()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to reload schema: %s", err.Error())), nil
	}

	// Get updated table list to confirm
	tables := ms.service.gj.GetTables()

	result := struct {
		Success    bool     `json:"success"`
		Message    string   `json:"message"`
		TableCount int      `json:"table_count"`
		Tables     []string `json:"tables,omitempty"`
	}{
		Success:    true,
		Message:    "Schema reloaded successfully",
		TableCount: len(tables),
	}

	// Include table names if not too many
	if len(tables) <= 20 {
		for _, t := range tables {
			result.Tables = append(result.Tables, t.Name)
		}
	}

	return ms.toolResultJSON("reload_schema", req.GetArguments(), result)
}

// EnhancedError represents an error with recovery suggestions
type EnhancedError struct {
	Message     string `json:"message"`
	Suggestion  string `json:"suggestion,omitempty"`
	RelatedTool string `json:"related_tool,omitempty"`
	// Kind, Table, Column are populated by enhanceExecError when the
	// error came from SQL execution and the dialect was identifiable.
	// Consumers can switch on Kind without parsing the prose.
	Kind   string `json:"kind,omitempty"`
	Table  string `json:"table,omitempty"`
	Column string `json:"column,omitempty"`
	// Hint carries a structured rewrite when the schema cross-check
	// detects that the error is misleading (e.g. column actually exists
	// on the table but was dropped by a CTE projection).
	Hint string `json:"hint,omitempty"`
}

// enhanceError adds helpful suggestions to common error messages
func enhanceError(errMsg, currentTool string) string {
	enhanced := EnhancedError{Message: errMsg}

	// Pattern matching for common errors
	switch {
	case contains(errMsg, "@through"):
		enhanced.Suggestion = "@through(table:) takes the name of an intermediate join table (for many-to-many). @through(column:) takes the name of the FK column to follow when the parent has multiple foreign keys to the same target table — for composite FKs, naming any one column of the composite is sufficient. Check the spelling of the table or column name."
		enhanced.RelatedTool = "query_catalog"
	case contains(errMsg, "table not found", "unknown table", "does not exist", "no such table", "table doesn't exist"):
		enhanced.Suggestion = "Check spelling or use query_catalog(where: {kind: {eq: 'table'}}) to see available tables. The table may exist in a different database - filter by database if needed."
		enhanced.RelatedTool = "query_catalog"
	case contains(errMsg, "column not found", "unknown column", "column does not exist", "no such column", "unknown field"):
		enhanced.Suggestion = "Check spelling or use query_catalog(where: {kind: {eq: 'column'}}) to see available columns"
		enhanced.RelatedTool = "query_catalog"
	case contains(errMsg, "invalid operator", "unknown operator", "unsupported operator"):
		enhanced.Suggestion = "Use query_catalog(where: {kind: {eq: 'operator_set'}}) to see valid operators for each type"
		enhanced.RelatedTool = "query_catalog"
	case contains(errMsg, "syntax error", "parse error", "unexpected"):
		enhanced.Suggestion = "Check query_catalog language-feature and query-pattern items for correct syntax"
		enhanced.RelatedTool = "query_catalog"
	case contains(errMsg, "permission", "access denied", "not allowed"):
		enhanced.Suggestion = "Check if mutations are enabled in config or if the operation requires authentication"
		enhanced.RelatedTool = ""
	default:
		// No enhancement for unrecognized errors
		return errMsg
	}

	data, err := json.Marshal(enhanced)
	if err != nil {
		return errMsg
	}
	return string(data)
}

// contains checks if the message contains any of the substrings (case-insensitive)
func contains(msg string, substrs ...string) bool {
	msgLower := stringToLower(msg)
	for _, s := range substrs {
		if stringContains(msgLower, stringToLower(s)) {
			return true
		}
	}
	return false
}

// stringToLower converts a string to lowercase
func stringToLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result[i] = c
	}
	return string(result)
}

// stringContains checks if s contains substr
func stringContains(s, substr string) bool {
	return len(substr) <= len(s) && (s == substr || len(substr) == 0 ||
		(len(substr) <= len(s) && findSubstring(s, substr) >= 0))
}

// findSubstring finds the index of substr in s, returns -1 if not found
func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// WhereValidationResult represents the result of where clause validation
type WhereValidationResult struct {
	Valid          bool                      `json:"valid"`
	Errors         []WhereValidationError    `json:"errors,omitempty"`
	Warnings       []string                  `json:"warnings,omitempty"`
	CompilerErrors []string                  `json:"compiler_errors,omitempty"`
	ValidatedBy    string                    `json:"validated_by,omitempty"`
	ExampleQuery   string                    `json:"example_query,omitempty"`
	ColumnInfo     map[string]ColumnTypeInfo `json:"column_info,omitempty"`
}

// WhereValidationError represents a single validation error
type WhereValidationError struct {
	Path       string `json:"path"`
	Error      string `json:"error"`
	Message    string `json:"message"`
	ColumnType string `json:"column_type,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

// ColumnTypeInfo provides information about a column's type and valid operators
type ColumnTypeInfo struct {
	Type           string   `json:"type"`
	ValidOperators []string `json:"valid_operators"`
}

// handleValidateWhereClause validates a where clause for syntax and type compatibility
func (ms *mcpServer) handleValidateWhereClause(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := ms.requireDB(); err != nil {
		return err, nil
	}
	args := req.GetArguments()
	table, _ := args["table"].(string)
	rawWhere, hasWhere := args["where"]
	database, _ := args["database"].(string)

	if table == "" {
		return mcp.NewToolResultError("table name is required"), nil
	}
	if !hasWhere || rawWhere == nil {
		return mcp.NewToolResultError("where clause is required"), nil
	}

	// Get table schema
	var schema *core.TableSchema
	var err error
	if database != "" {
		schema, err = ms.service.gj.GetTableSchemaForDatabase(database, table)
	} else {
		schema, err = ms.service.gj.GetTableSchema(table)
	}
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get schema for table '%s': %v", table, err)), nil
	}

	// Build column info map
	columnTypes := make(map[string]core.ColumnInfo)
	for _, col := range schema.Columns {
		columnTypes[col.Name] = col
	}

	compileResult := ms.validateWhereClauseByCompilation(table, database, rawWhere, schema)
	whereData := compileResult.WhereData
	err = compileResult.ParseErr
	if err != nil {
		// Return parse error
		result := WhereValidationResult{
			Valid: false,
			Errors: []WhereValidationError{
				{
					Path:       "",
					Error:      "parse_error",
					Message:    fmt.Sprintf("Failed to parse where clause: %v", err),
					Suggestion: "Pass the where clause as an object like { price: { gt: 50 } }, or as a valid JSON string for legacy callers.",
				},
			},
		}
		return ms.toolResultJSON("validate_where_clause", args, result)
	}

	// Validate the where clause with the lightweight advisory validator.
	var errors []WhereValidationError
	if whereData != nil {
		errors = validateWhereClause(whereData, columnTypes, "")
	}
	for _, compilerErr := range compileResult.CompilerErrors {
		errors = append(errors, WhereValidationError{
			Path:       "",
			Error:      "compiler_error",
			Message:    compilerErr,
			Suggestion: "Use fix_query_error with the example_query and compiler error, or inspect query_catalog for valid columns and relationships.",
		})
	}

	// Build column info for response
	columnInfo := make(map[string]ColumnTypeInfo)
	for name, col := range columnTypes {
		columnInfo[name] = ColumnTypeInfo{
			Type:           col.Type,
			ValidOperators: getValidOperators(col.Type, col.Array),
		}
	}

	result := WhereValidationResult{
		Valid:          len(errors) == 0,
		Errors:         errors,
		Warnings:       compileResult.Warnings,
		CompilerErrors: compileResult.CompilerErrors,
		ValidatedBy:    "compiler",
		ExampleQuery:   compileResult.ExampleQuery,
		ColumnInfo:     columnInfo,
	}
	return ms.toolResultJSON("validate_where_clause", args, result)
}

type whereCompileValidationResult struct {
	WhereData      map[string]any
	WhereLiteral   string
	ParseErr       error
	ExampleQuery   string
	CompilerErrors []string
	Warnings       []string
}

func (ms *mcpServer) validateWhereClauseByCompilation(table, database string, rawWhere any, schema *core.TableSchema) whereCompileValidationResult {
	var result whereCompileValidationResult

	whereData, whereLiteral, err := parseWhereClauseInput(rawWhere)
	if err != nil {
		result.ParseErr = err
		return result
	}
	result.WhereData = whereData
	result.WhereLiteral = whereLiteral

	field := validationSelectField(schema)
	query, err := buildWhereValidationQuery(table, database, whereLiteral, field)
	if err != nil {
		result.ParseErr = err
		return result
	}
	result.ExampleQuery = query

	if ms == nil || ms.service == nil || ms.service.gj == nil {
		result.CompilerErrors = append(result.CompilerErrors, "GraphJin compiler is not initialized")
		return result
	}

	var exp *core.QueryExplanation
	if database != "" {
		exp, err = ms.service.gj.ExplainQueryForDatabase(database, query, nil, "")
	} else {
		exp, err = ms.service.gj.ExplainQuery(query, nil, "")
	}
	if err != nil {
		result.CompilerErrors = append(result.CompilerErrors, err.Error())
		return result
	}
	if exp != nil && len(exp.Errors) != 0 {
		result.CompilerErrors = append(result.CompilerErrors, exp.Errors...)
	}
	if database != "" && !strings.Contains(query, "@database") {
		result.Warnings = append(result.Warnings, "database was used for schema lookup; compile validation follows GraphJin's configured database routing for the generated query")
	}
	return result
}

func (ms *mcpServer) availableToolSet() map[string]bool {
	tools := ms.srv.ListTools()
	available := make(map[string]bool, len(tools))
	for name := range tools {
		available[name] = true
	}
	return available
}

func parseWhereClauseArg(value any) (map[string]any, error) {
	whereData, _, err := parseWhereClauseInput(value)
	if err != nil {
		return nil, err
	}
	if whereData == nil {
		return nil, fmt.Errorf("where clause must be a JSON object for this caller")
	}
	return whereData, nil
}

func parseWhereClauseInput(value any) (map[string]any, string, error) {
	switch v := value.(type) {
	case map[string]any:
		literal, err := graphQLInputLiteral(v)
		if err != nil {
			return nil, "", err
		}
		return v, literal, nil
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return nil, "", fmt.Errorf("empty where clause")
		}
		var whereData map[string]any
		if err := json.Unmarshal([]byte(trimmed), &whereData); err == nil {
			literal, err := graphQLInputLiteral(whereData)
			if err != nil {
				return nil, "", err
			}
			return whereData, literal, nil
		} else if looksLikeMalformedJSONObject(trimmed) {
			return nil, "", err
		}
		if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
			return nil, "", fmt.Errorf("where literal must be an object")
		}
		return nil, trimmed, nil
	default:
		return nil, "", fmt.Errorf("unsupported where clause type %T", value)
	}
}

func looksLikeMalformedJSONObject(value string) bool {
	if !strings.HasPrefix(value, "{") {
		return false
	}
	rest := strings.TrimLeft(value[1:], " \t\r\n")
	return strings.HasPrefix(rest, `"`)
}

func graphQLInputLiteral(value any) (string, error) {
	value = catalogJSONAny(value)
	switch v := value.(type) {
	case nil:
		return "null", nil
	case string:
		return catalogQuote(v), nil
	case bool:
		if v {
			return "true", nil
		}
		return "false", nil
	case float64:
		return fmt.Sprintf("%v", v), nil
	case []any:
		items := make([]string, 0, len(v))
		for _, item := range v {
			literal, err := graphQLInputLiteral(item)
			if err != nil {
				return "", err
			}
			items = append(items, literal)
		}
		return "[" + strings.Join(items, ", ") + "]", nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			if !catalogValidName(key) {
				return "", fmt.Errorf("unsupported where field %q", key)
			}
			literal, err := graphQLInputLiteral(v[key])
			if err != nil {
				return "", err
			}
			parts = append(parts, key+": "+literal)
		}
		return "{ " + strings.Join(parts, ", ") + " }", nil
	default:
		return "", fmt.Errorf("unsupported where value %T", value)
	}
}

func validationSelectField(schema *core.TableSchema) string {
	if schema == nil {
		return ""
	}
	if schema.PrimaryKey != "" {
		return schema.PrimaryKey
	}
	for _, col := range schema.Columns {
		if !col.Array {
			return col.Name
		}
	}
	if len(schema.Columns) != 0 {
		return schema.Columns[0].Name
	}
	return ""
}

func buildWhereValidationQuery(table, database, whereLiteral, field string) (string, error) {
	if !catalogValidName(table) {
		return "", fmt.Errorf("unsupported table name %q", table)
	}
	if !catalogValidName(field) {
		return "", fmt.Errorf("no selectable scalar field found for table %q", table)
	}
	directive := ""
	if database != "" {
		directive = fmt.Sprintf(" @database(name: %s)", catalogQuote(database))
	}
	return fmt.Sprintf("query __gj_validate_where { %s(where: %s, limit: 1)%s { %s } }", table, whereLiteral, directive, field), nil
}

// validateWhereClause recursively validates a where clause structure
func validateWhereClause(where map[string]any, columnTypes map[string]core.ColumnInfo, path string) []WhereValidationError {
	var errors []WhereValidationError

	// Logical operators
	logicalOps := map[string]bool{"and": true, "or": true, "not": true}

	whereKeys := make([]string, 0, len(where))
	for key := range where {
		whereKeys = append(whereKeys, key)
	}
	sort.Strings(whereKeys)
	for _, key := range whereKeys {
		value := where[key]
		currentPath := key
		if path != "" {
			currentPath = path + "." + key
		}

		// Handle logical operators
		if logicalOps[key] {
			switch v := value.(type) {
			case []any:
				// and/or with array of conditions
				for i, item := range v {
					if itemMap, ok := item.(map[string]any); ok {
						errors = append(errors, validateWhereClause(itemMap, columnTypes, fmt.Sprintf("%s[%d]", currentPath, i))...)
					}
				}
			case map[string]any:
				// not with single condition, or or with object
				errors = append(errors, validateWhereClause(v, columnTypes, currentPath)...)
			}
			continue
		}

		// Handle column conditions
		col, colExists := columnTypes[key]
		if !colExists {
			// Check if this might be a nested relationship
			// We'll skip validation for potential relationship filters
			if valueMap, ok := value.(map[string]any); ok {
				// Check if any key looks like an operator
				hasOperator := false
				for k := range valueMap {
					if isOperator(k) {
						hasOperator = true
						break
					}
				}
				if !hasOperator {
					// Likely a relationship filter, skip
					continue
				}
			}

			errors = append(errors, WhereValidationError{
				Path:       currentPath,
				Error:      "unknown_column",
				Message:    fmt.Sprintf("Column '%s' does not exist in table", key),
				Suggestion: "Check column name spelling or use query_catalog(where: {kind: {eq: 'column'}}) to see available columns",
			})
			continue
		}

		// Validate operator and value type
		if valueMap, ok := value.(map[string]any); ok {
			colErrors := validateColumnOperators(valueMap, col, currentPath)
			errors = append(errors, colErrors...)
		}
	}

	return errors
}

// isOperator returns true if the string is a known GraphJin operator
func isOperator(s string) bool {
	return canonicalWhereOperator(s) != ""
}

func canonicalWhereOperator(op string) string {
	if op != "" && op[0] == '_' {
		op = op[1:]
	}
	switch op {
	case "eq", "equals":
		return "eq"
	case "neq", "notEquals", "not_equals":
		return "neq"
	case "gt", "greaterThan", "greater_than":
		return "gt"
	case "gte", "gteq", "greaterOrEquals", "greater_or_equals":
		return "gte"
	case "lt", "lesserThan", "lesser_than":
		return "lt"
	case "lte", "lteq", "lesserOrEquals", "lesser_or_equals":
		return "lte"
	case "in":
		return "in"
	case "nin", "notIn", "not_in":
		return "nin"
	case "like", "nlike", "notLike", "not_like":
		return "like"
	case "ilike", "iLike", "nilike", "notILike", "not_ilike":
		return "ilike"
	case "similar", "nsimilar", "notSimiliar", "not_similar":
		return "similar"
	case "regex", "nregex", "notRegex", "not_regex":
		return "regex"
	case "iregex", "niregex", "notIRegex", "not_iregex":
		return "iregex"
	case "contains":
		return "contains"
	case "containedIn", "contained_in":
		return "contained_in"
	case "hasInCommon", "has_in_common":
		return "has_in_common"
	case "hasKey", "has_key":
		return "has_key"
	case "hasKeyAny", "has_key_any":
		return "has_key_any"
	case "hasKeyAll", "has_key_all":
		return "has_key_all"
	case "isNull", "is_null":
		return "is_null"
	case "notDistinct", "ndis", "not_distinct":
		return "not_distinct"
	case "dis", "distinct":
		return "distinct"
	case "st_dwithin", "stDWithin", "st_d_within", "dwithin":
		return "st_dwithin"
	case "st_within", "stWithin", "within":
		return "st_within"
	case "st_contains", "stContains", "geoContains":
		return "st_contains"
	case "st_intersects", "stIntersects", "intersects":
		return "st_intersects"
	case "st_coveredby", "stCoveredBy", "coveredBy", "covered_by":
		return "st_coveredby"
	case "st_covers", "stCovers", "covers":
		return "st_covers"
	case "st_touches", "stTouches", "touches":
		return "st_touches"
	case "st_overlaps", "stOverlaps", "overlaps":
		return "st_overlaps"
	case "near", "geoNear":
		return "near"
	default:
		return ""
	}
}

// validateColumnOperators validates operators and values for a column
func validateColumnOperators(operators map[string]any, col core.ColumnInfo, path string) []WhereValidationError {
	var errors []WhereValidationError

	validOps := getValidOperators(col.Type, col.Array)
	validOpsMap := make(map[string]bool)
	for _, op := range validOps {
		validOpsMap[op] = true
	}

	normalizedType := normalizeColumnType(col.Type)

	opKeys := make([]string, 0, len(operators))
	for op := range operators {
		opKeys = append(opKeys, op)
	}
	sort.Strings(opKeys)
	for _, op := range opKeys {
		value := operators[op]
		opPath := path + "." + op
		canonicalOp := canonicalWhereOperator(op)
		if canonicalOp == "" {
			canonicalOp = op
		}

		// Check if operator is valid for this column type
		if !validOpsMap[canonicalOp] {
			errors = append(errors, WhereValidationError{
				Path:       opPath,
				Error:      "invalid_operator",
				Message:    fmt.Sprintf("Operator '%s' is not valid for column type '%s'", op, col.Type),
				ColumnType: col.Type,
				Suggestion: fmt.Sprintf("Valid operators for %s: %v", col.Type, validOps),
			})
			continue
		}

		// Validate value type matches operator expectations
		valueErr := validateOperatorValue(canonicalOp, value, normalizedType, opPath)
		if valueErr != nil {
			errors = append(errors, *valueErr)
		}
	}

	return errors
}

// validateOperatorValue checks that the value type is appropriate for the operator and column type
func validateOperatorValue(op string, value any, colType string, path string) *WhereValidationError {
	// Handle is_null specially - must be boolean
	if op == "is_null" {
		if _, ok := value.(bool); !ok {
			return &WhereValidationError{
				Path:       path,
				Error:      "type_mismatch",
				Message:    fmt.Sprintf("Operator 'is_null' expects boolean value, got %T", value),
				ColumnType: colType,
				Suggestion: "Use: { is_null: true } or { is_null: false }",
			}
		}
		return nil
	}

	// Handle in/nin - must be arrays
	if op == "in" || op == "nin" {
		if _, ok := value.([]any); !ok {
			return &WhereValidationError{
				Path:       path,
				Error:      "type_mismatch",
				Message:    fmt.Sprintf("Operator '%s' expects array value, got %T", op, value),
				ColumnType: colType,
				Suggestion: fmt.Sprintf("Use: { %s: [value1, value2, ...] }", op),
			}
		}
		return nil
	}

	// Validate numeric operators require numeric values
	numericOps := map[string]bool{"gt": true, "gte": true, "lt": true, "lte": true}
	if numericOps[op] && colType == "numeric" {
		switch value.(type) {
		case float64, int, int64:
			// Valid
		case string:
			return &WhereValidationError{
				Path:       path,
				Error:      "type_mismatch",
				Message:    fmt.Sprintf("Operator '%s' expects numeric value, got string", op),
				ColumnType: colType,
				Suggestion: fmt.Sprintf("Use a number: { %s: 50 } not { %s: \"50\" }", op, op),
			}
		}
	}

	// Validate text operators require string values
	textOps := map[string]bool{"like": true, "ilike": true, "regex": true, "iregex": true, "similar": true}
	if textOps[op] {
		if _, ok := value.(string); !ok {
			return &WhereValidationError{
				Path:       path,
				Error:      "type_mismatch",
				Message:    fmt.Sprintf("Operator '%s' expects string value, got %T", op, value),
				ColumnType: colType,
				Suggestion: fmt.Sprintf("Use a string: { %s: \"pattern\" }", op),
			}
		}
	}

	// Validate boolean column with eq/neq requires boolean value
	if colType == "boolean" && (op == "eq" || op == "neq") {
		if _, ok := value.(bool); !ok {
			return &WhereValidationError{
				Path:       path,
				Error:      "type_mismatch",
				Message:    fmt.Sprintf("Boolean column with '%s' expects boolean value, got %T", op, value),
				ColumnType: colType,
				Suggestion: fmt.Sprintf("Use: { %s: true } or { %s: false }", op, op),
			}
		}
	}

	return nil
}
