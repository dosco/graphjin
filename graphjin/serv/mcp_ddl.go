package serv

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerDDLTools registers the DDL (schema modification) tools
func (ms *mcpServer) registerDDLTools() {
	if !ms.service.conf.MCP.AllowSchemaUpdates {
		return
	}

	// preview_schema_changes - Preview SQL that would be applied
	ms.srv.AddTool(mcp.NewTool(
		"preview_schema_changes",
		mcp.WithDescription("Preview SQL changes needed to create or modify database tables. "+
			"Takes a db.graphql schema definition and returns the SQL that would be executed, without applying it. "+
			"Use this to verify changes before calling apply_schema_changes. "+
			"Example schema: type products { id: BigInt! @id\\n name: Text!\\n price: Numeric }"),
		mcp.WithString("schema",
			mcp.Required(),
			mcp.Description("db.graphql schema definition. Example:\n"+
				"type products {\n  id: BigInt! @id\n  name: Text!\n  price: Numeric\n}"),
		),
		mcp.WithBoolean("destructive",
			mcp.Description("Include DROP TABLE/COLUMN operations. Default: false"),
		),
		mcp.WithString("database",
			mcp.Required(),
			mcp.Description("Database name to target (as configured in the databases map)."),
		),
	), ms.handlePreviewSchemaChanges)

	// apply_schema_changes - Apply schema changes to the database
	ms.srv.AddTool(mcp.NewTool(
		"apply_schema_changes",
		mcp.WithDescription("Apply database schema changes using db.graphql format. "+
			"Creates or modifies tables by executing the necessary SQL. "+
			"IMPORTANT: Call preview_schema_changes first to review the SQL before applying. "+
			"After applying, the schema is automatically reloaded so new tables are immediately queryable."),
		mcp.WithString("schema",
			mcp.Required(),
			mcp.Description("db.graphql schema definition. Example:\n"+
				"type products {\n  id: BigInt! @id\n  name: Text!\n  price: Numeric\n}"),
		),
		mcp.WithBoolean("destructive",
			mcp.Description("Allow DROP TABLE/COLUMN operations. Default: false"),
		),
		mcp.WithString("database",
			mcp.Required(),
			mcp.Description("Database name to target (as configured in the databases map)."),
		),
	), ms.handleApplySchemaChanges)
}

// DDLOperationInfo describes a single schema operation for JSON response
type DDLOperationInfo struct {
	Type        string `json:"type"`
	Table       string `json:"table"`
	Column      string `json:"column,omitempty"`
	SQL         string `json:"sql"`
	Destructive bool   `json:"destructive"`
}

// DDLPreviewResult is the response from preview_schema_changes
type DDLPreviewResult struct {
	Operations []DDLOperationInfo `json:"operations"`
	Summary    DDLSummary         `json:"summary"`
}

// DDLSummary summarizes the schema changes
type DDLSummary struct {
	Total          int `json:"total"`
	TablesToCreate int `json:"tables_to_create"`
	ColumnsToAdd   int `json:"columns_to_add"`
	IndexesToAdd   int `json:"indexes_to_add"`
	DestructiveOps int `json:"destructive_count"`
}

// DDLApplyResult is the response from apply_schema_changes
type DDLApplyResult struct {
	Success           bool     `json:"success"`
	OperationsApplied int      `json:"operations_applied"`
	TablesCreated     []string `json:"tables_created,omitempty"`
	ColumnsAdded      []string `json:"columns_added,omitempty"`
	Message           string   `json:"message"`
}

// getDBByName returns the *sql.DB for the given database name.
// If database is empty or not found, falls back to anyDB().
func (ms *mcpServer) getDBByName(database string) *sql.DB {
	if database != "" {
		if db, ok := ms.service.dbs[database]; ok {
			return db
		}
	}
	return ms.service.anyDB()
}

// prepareSchema ensures the schema bytes have the required dbinfo header.
// If database is provided, the matching DatabaseConfig is used for type/schema.
func (ms *mcpServer) prepareSchema(schema, database string) []byte {
	s := strings.TrimSpace(schema)

	// If schema already has a dbinfo header, use as-is
	if strings.HasPrefix(s, "# dbinfo:") {
		return []byte(s)
	}

	// Build header from database config
	var dbType, dbSchema string
	if database != "" {
		if dbConf, ok := ms.service.conf.Core.Databases[database]; ok {
			dbType = dbConf.Type
			dbSchema = dbConf.Schema
		}
	}
	if dbType == "" {
		dbType = ms.service.conf.DB.Type
	}
	if dbSchema == "" {
		dbSchema = ms.service.conf.DB.Schema
	}
	if dbSchema == "" {
		switch dbType {
		case "postgres", "postgresql":
			dbSchema = "public"
		case "mysql", "mariadb":
			dbSchema = "db"
		case "sqlite":
			dbSchema = "main"
		case "mssql":
			dbSchema = "dbo"
		default:
			dbSchema = "public"
		}
	}

	header := fmt.Sprintf("# dbinfo:%s,,%s\n\n", dbType, dbSchema)
	return []byte(header + s)
}

// computeSchemaOps computes schema operations from the provided schema string.
// database selects which configured database to target.
func (ms *mcpServer) computeSchemaOps(schema string, destructive bool, database string) ([]core.SchemaOperation, error) {
	schemaBytes := ms.prepareSchema(schema, database)
	db := ms.getDBByName(database)

	dbType := ms.service.conf.DB.Type
	if database != "" {
		if dbConf, ok := ms.service.conf.Core.Databases[database]; ok {
			dbType = dbConf.Type
		}
	}

	opts := core.DiffOptions{Destructive: destructive}
	return core.SchemaDiff(
		db,
		dbType,
		schemaBytes,
		ms.service.conf.Blocklist,
		opts,
	)
}

// handlePreviewSchemaChanges previews schema changes without applying them
func (ms *mcpServer) handlePreviewSchemaChanges(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	schema, _ := args["schema"].(string)
	destructive, _ := args["destructive"].(bool)
	database, _ := args["database"].(string)

	if schema == "" {
		return mcp.NewToolResultError("schema is required"), nil
	}
	if database == "" {
		return mcp.NewToolResultError("database is required"), nil
	}

	db := ms.getDBByName(database)
	if db == nil {
		return mcp.NewToolResultError(fmt.Sprintf("database %q not found", database)), nil
	}

	ops, err := ms.computeSchemaOps(schema, destructive, database)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to compute schema diff: %v", err)), nil
	}

	if len(ops) == 0 {
		result := DDLPreviewResult{
			Operations: []DDLOperationInfo{},
			Summary:    DDLSummary{},
		}
		data, _ := mcpMarshalJSON(result, true)
		return mcp.NewToolResultText(string(data)), nil
	}

	// Convert operations to response format
	var opInfos []DDLOperationInfo
	summary := DDLSummary{Total: len(ops)}

	for _, op := range ops {
		info := DDLOperationInfo{
			Type:        op.Type,
			Table:       op.Table,
			Column:      op.Column,
			SQL:         op.SQL,
			Destructive: op.Danger,
		}
		opInfos = append(opInfos, info)

		switch op.Type {
		case "create_table":
			summary.TablesToCreate++
		case "add_column":
			summary.ColumnsToAdd++
		case "add_index":
			summary.IndexesToAdd++
		}
		if op.Danger {
			summary.DestructiveOps++
		}
	}

	result := DDLPreviewResult{
		Operations: opInfos,
		Summary:    summary,
	}

	data, err := mcpMarshalJSON(result, true)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// handleApplySchemaChanges applies schema changes to the database
func (ms *mcpServer) handleApplySchemaChanges(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	schema, _ := args["schema"].(string)
	destructive, _ := args["destructive"].(bool)
	database, _ := args["database"].(string)

	if schema == "" {
		return mcp.NewToolResultError("schema is required"), nil
	}
	if database == "" {
		return mcp.NewToolResultError("database is required"), nil
	}

	// Block DDL on read-only databases (uses startup snapshot, tamper-proof)
	if ms.isDBReadOnly(database) {
		return mcp.NewToolResultError(fmt.Sprintf("database %q is read-only: schema changes are not allowed", database)), nil
	}

	db := ms.getDBByName(database)
	if db == nil {
		return mcp.NewToolResultError(fmt.Sprintf("database %q not found", database)), nil
	}

	ops, err := ms.computeSchemaOps(schema, destructive, database)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to compute schema diff: %v", err)), nil
	}

	if len(ops) == 0 {
		result := DDLApplyResult{
			Success:           true,
			OperationsApplied: 0,
			Message:           "No schema changes needed - database already matches the provided schema",
		}
		data, _ := mcpMarshalJSON(result, true)
		return mcp.NewToolResultText(string(data)), nil
	}

	// Generate SQL statements
	sqls := core.GenerateDiffSQL(ops)
	if len(sqls) == 0 {
		result := DDLApplyResult{
			Success:           true,
			OperationsApplied: 0,
			Message:           "No SQL to execute",
		}
		data, _ := mcpMarshalJSON(result, true)
		return mcp.NewToolResultText(string(data)), nil
	}

	// Apply changes in a transaction
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to begin transaction: %v", err)), nil
	}

	for _, sqlStmt := range sqls {
		if _, err := tx.ExecContext(ctx, sqlStmt); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				ms.service.log.Warnf("Rollback failed: %s", rbErr)
			}
			return mcp.NewToolResultError(fmt.Sprintf("failed to execute SQL: %s\nError: %v", sqlStmt, err)), nil
		}
	}

	if err := tx.Commit(); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to commit transaction: %v", err)), nil
	}

	// Collect result info
	var tablesCreated []string
	var columnsAdded []string
	for _, op := range ops {
		switch op.Type {
		case "create_table":
			tablesCreated = append(tablesCreated, op.Table)
		case "add_column":
			columnsAdded = append(columnsAdded, fmt.Sprintf("%s.%s", op.Table, op.Column))
		}
	}

	// Reload schema so new tables are immediately queryable
	if ms.service.gj != nil {
		if err := ms.service.gj.Reload(); err != nil {
			ms.service.log.Warnf("Schema reload after DDL failed: %s", err)
		}
	}

	result := DDLApplyResult{
		Success:           true,
		OperationsApplied: len(sqls),
		TablesCreated:     tablesCreated,
		ColumnsAdded:      columnsAdded,
		Message:           "Schema changes applied successfully",
	}

	data, err := mcpMarshalJSON(result, true)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}
