package serv

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3/internal/mcpcompat/mcp"
)

// registerDDLTools is retained as an inert compatibility hook.
func (ms *mcpServer) registerDDLTools() {
	return

	if !ms.service.conf.MCP.AllowSchemaUpdates {
		return
	}

	// preview_schema_changes - Preview SQL that would be applied
	ms.srv.AddTool(mcp.NewTool(
		"preview_schema_changes",
		mcp.WithDescription("Preview SQL changes needed to create or modify database tables. "+
			"Takes a GraphJin DDL schema definition and returns the SQL that would be executed, without applying it. "+
			"Use this to verify changes before calling apply_schema_changes. "+
			"Example schema: type products { id: BigInt! @id\\n name: Text!\\n price: Numeric }"),
		mcp.WithString("schema",
			mcp.Required(),
			mcp.Description("GraphJin DDL schema definition. Example:\n"+
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
		mcp.WithDescription("Apply database schema changes using GraphJin DDL format. "+
			"Creates or modifies tables by executing the necessary SQL. "+
			"IMPORTANT: Call preview_schema_changes first to review the SQL before applying. "+
			"After applying, the schema is automatically reloaded so new tables are immediately queryable."),
		mcp.WithString("schema",
			mcp.Required(),
			mcp.Description("GraphJin DDL schema definition. Example:\n"+
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
		case "snowflake":
			dbSchema = "main"
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
		return ms.toolResultJSON("preview_schema_changes", args, result)
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
	return ms.toolResultJSON("preview_schema_changes", args, result)
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
		ms.service.recordRuntimeEvent(ctx, runtimeEvent{
			Phase:        "schema",
			Status:       runtimeStatusFailed,
			Severity:     "warn",
			Summary:      "MCP schema apply diff computation failed.",
			NextAction:   "Review the GraphJin DDL schema input and retry preview before applying changes.",
			DatabaseName: database,
			ErrorCode:    "schema_diff_failed",
			Details:      map[string]any{"error": err.Error(), "destructive": destructive},
		})
		return mcp.NewToolResultError(fmt.Sprintf("failed to compute schema diff: %v", err)), nil
	}

	if len(ops) == 0 {
		result := DDLApplyResult{
			Success:           true,
			OperationsApplied: 0,
			Message:           "No schema changes needed - database already matches the provided schema",
		}
		ms.service.recordRuntimeEvent(ctx, runtimeEvent{
			Phase:        "schema",
			Status:       runtimeStatusReady,
			Severity:     "info",
			Summary:      "MCP schema apply completed with no database changes.",
			NextAction:   "Continue with the current schema; query gj_catalog if table shape matters.",
			DatabaseName: database,
			Details:      map[string]any{"operations_applied": 0, "destructive": destructive},
		})
		return ms.toolResultJSON("apply_schema_changes", args, result)
	}

	// Generate SQL statements
	sqls := core.GenerateDiffSQL(ops)
	if len(sqls) == 0 {
		result := DDLApplyResult{
			Success:           true,
			OperationsApplied: 0,
			Message:           "No SQL to execute",
		}
		ms.service.recordRuntimeEvent(ctx, runtimeEvent{
			Phase:        "schema",
			Status:       runtimeStatusReady,
			Severity:     "info",
			Summary:      "MCP schema apply completed without executable statements.",
			NextAction:   "Continue with the current schema; query gj_catalog if table shape matters.",
			DatabaseName: database,
			Details:      map[string]any{"operation_count": len(ops), "destructive": destructive},
		})
		return ms.toolResultJSON("apply_schema_changes", args, result)
	}

	// Apply changes in a transaction
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		ms.service.recordRuntimeEvent(ctx, runtimeEvent{
			Phase:        "schema",
			Status:       runtimeStatusFailed,
			Severity:     "error",
			Summary:      "MCP schema apply failed to start a database transaction.",
			NextAction:   "Check database connectivity and transaction support before retrying.",
			DatabaseName: database,
			ErrorCode:    "schema_apply_begin_failed",
			Details:      map[string]any{"error": err.Error()},
		})
		return mcp.NewToolResultError(fmt.Sprintf("failed to begin transaction: %v", err)), nil
	}

	for _, sqlStmt := range sqls {
		if _, err := tx.ExecContext(ctx, sqlStmt); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				ms.service.log.Warnf("Rollback failed: %s", rbErr)
			}
			ms.service.recordRuntimeEvent(ctx, runtimeEvent{
				Phase:        "schema",
				Status:       runtimeStatusFailed,
				Severity:     "error",
				Summary:      "MCP schema apply failed while executing a generated statement.",
				NextAction:   "Run preview_schema_changes, inspect the database error, and retry with a corrected schema.",
				DatabaseName: database,
				ErrorCode:    "schema_apply_exec_failed",
				Details: map[string]any{
					"error":           err.Error(),
					"statement_count": len(sqls),
					"destructive":     destructive,
				},
			})
			return mcp.NewToolResultError(fmt.Sprintf("failed to execute SQL: %s\nError: %v", sqlStmt, err)), nil
		}
	}

	if err := tx.Commit(); err != nil {
		ms.service.recordRuntimeEvent(ctx, runtimeEvent{
			Phase:        "schema",
			Status:       runtimeStatusFailed,
			Severity:     "error",
			Summary:      "MCP schema apply failed to commit.",
			NextAction:   "Check database transaction state, then rerun schema discovery before retrying.",
			DatabaseName: database,
			ErrorCode:    "schema_apply_commit_failed",
			Details:      map[string]any{"error": err.Error(), "statement_count": len(sqls)},
		})
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
			ms.service.recordRuntimeEvent(ctx, runtimeEvent{
				Phase:        "schema",
				Status:       runtimeStatusDegraded,
				Severity:     "warn",
				Summary:      "MCP schema apply succeeded but schema reload failed.",
				NextAction:   "Retry reload_schema before relying on newly applied schema changes.",
				DatabaseName: database,
				ErrorCode:    "schema_reload_after_apply_failed",
				Details:      map[string]any{"error": err.Error(), "statement_count": len(sqls)},
			})
		}
	}

	result := DDLApplyResult{
		Success:           true,
		OperationsApplied: len(sqls),
		TablesCreated:     tablesCreated,
		ColumnsAdded:      columnsAdded,
		Message:           "Schema changes applied successfully",
	}
	ms.service.recordRuntimeEvent(ctx, runtimeEvent{
		Phase:        "schema",
		Status:       runtimeStatusReady,
		Severity:     "info",
		Summary:      "MCP schema changes were applied successfully.",
		NextAction:   "Query gj_catalog before using newly created or changed tables.",
		DatabaseName: database,
		Details: map[string]any{
			"operations_applied": len(sqls),
			"tables_created":     tablesCreated,
			"columns_added":      columnsAdded,
			"destructive":        destructive,
		},
	})
	return ms.toolResultJSON("apply_schema_changes", args, result)
}
