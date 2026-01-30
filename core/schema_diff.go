package core

import (
	"database/sql"
	"fmt"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// DiffOptions controls what operations are included in the schema diff
type DiffOptions struct {
	// Destructive enables DROP TABLE and DROP COLUMN operations
	Destructive bool
}

// SchemaOperation represents a schema change operation
type SchemaOperation struct {
	Type   string // "create_table", "add_column", "drop_table", "drop_column", "add_index", "add_constraint"
	Table  string
	Column string
	SQL    string
	Danger bool // true if this is a destructive operation
}

// SchemaDiff computes the SQL statements needed to sync the database with the schema file
func SchemaDiff(db *sql.DB, dbType string, schemaBytes []byte, blocklist []string, opts DiffOptions) ([]SchemaOperation, error) {
	// Parse the schema file
	ds, err := qcode.ParseSchema(schemaBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema: %w", err)
	}

	// Determine schema based on dbType when not specified via @schema directive
	schema := ds.Schema
	if schema == "" {
		schema = defaultSchemaForDBType(dbType)
	}

	// Convert parsed schema to DBInfo (expected state)
	// Always use dbType parameter, not ds.Type from schema file
	expected := sdata.NewDBInfo(
		dbType,
		ds.Version,
		schema,
		"", // dbName - not needed for diff
		ds.Columns,
		ds.Functions,
		blocklist,
	)

	// Get current database schema
	current, err := sdata.GetDBInfo(db, dbType, blocklist)
	if err != nil {
		return nil, fmt.Errorf("failed to discover database schema: %w", err)
	}

	// Compute the diff
	ops := computeDiff(current, expected, opts)
	return ops, nil
}

// defaultSchemaForDBType returns the default schema name for a database type
func defaultSchemaForDBType(dbType string) string {
	switch dbType {
	case "postgres", "postgresql":
		return "public"
	case "mysql", "mariadb":
		return "db"
	case "sqlite":
		return "main"
	case "mssql":
		return "dbo"
	case "oracle":
		return "TESTER"
	default:
		return "public"
	}
}

// sortTablesByDependency performs a topological sort of tables based on FK dependencies
// Parent tables (referenced by FKs) come before child tables (containing FKs)
func sortTablesByDependency(tables []sdata.DBTable) []sdata.DBTable {
	// Build a map of table name -> table for quick lookup
	tableMap := make(map[string]sdata.DBTable)
	for _, t := range tables {
		tableMap[t.Name] = t
	}

	// Build dependency graph: table -> tables it depends on (FK references)
	// Exclude self-referential FKs (e.g., comments.reply_to_id -> comments.id)
	deps := make(map[string][]string)
	for _, t := range tables {
		for _, col := range t.Columns {
			if col.FKeyTable != "" && col.FKeyTable != t.Name {
				deps[t.Name] = append(deps[t.Name], col.FKeyTable)
			}
		}
	}

	// Topological sort using Kahn's algorithm
	// Calculate in-degree (number of dependencies) for each table
	inDegree := make(map[string]int)
	for _, t := range tables {
		inDegree[t.Name] = len(deps[t.Name])
	}

	// Start with tables that have no dependencies
	var queue []string
	for _, t := range tables {
		if inDegree[t.Name] == 0 {
			queue = append(queue, t.Name)
		}
	}

	var sorted []sdata.DBTable
	for len(queue) > 0 {
		// Pop first item
		name := queue[0]
		queue = queue[1:]

		if t, exists := tableMap[name]; exists {
			sorted = append(sorted, t)
		}

		// Reduce in-degree for tables that depend on this one
		for tName, depList := range deps {
			for _, dep := range depList {
				if dep == name {
					inDegree[tName]--
					if inDegree[tName] == 0 {
						queue = append(queue, tName)
					}
				}
			}
		}
	}

	// If we couldn't sort all tables (circular dependency), return original order
	if len(sorted) != len(tables) {
		return tables
	}

	return sorted
}

// computeDiff computes the difference between current and expected schemas
func computeDiff(current, expected *sdata.DBInfo, opts DiffOptions) []SchemaOperation {
	dialect := getDDLDialect(expected.Type)
	if dialect == nil {
		dialect = getDDLDialect(current.Type)
	}
	if dialect == nil {
		return nil
	}

	var ops []SchemaOperation

	// Build maps for efficient lookup
	currentTables := make(map[string]*sdata.DBTable)
	for i := range current.Tables {
		t := &current.Tables[i]
		currentTables[t.Name] = t
	}

	expectedTables := make(map[string]*sdata.DBTable)
	for i := range expected.Tables {
		t := &expected.Tables[i]
		expectedTables[t.Name] = t
	}

	// Find tables to create
	// Sort tables to create parent tables before children (FK dependency order)
	sortedTables := sortTablesByDependency(expected.Tables)
	for _, expTable := range sortedTables {
		if _, exists := currentTables[expTable.Name]; !exists {
			sql := dialect.CreateTable(expTable)
			ops = append(ops, SchemaOperation{
				Type:  "create_table",
				Table: expTable.Name,
				SQL:   sql,
			})

			// Add indexes for searchable, unique, and indexed columns
			for _, col := range expTable.Columns {
				if col.FullText {
					sql := dialect.CreateSearchIndex(expTable.Name, col)
					if sql != "" {
						ops = append(ops, SchemaOperation{
							Type:   "add_index",
							Table:  expTable.Name,
							Column: col.Name,
							SQL:    sql,
						})
					}
				}
				if col.UniqueKey && !col.PrimaryKey {
					sql := dialect.CreateUniqueIndex(expTable.Name, col)
					if sql != "" {
						ops = append(ops, SchemaOperation{
							Type:   "add_index",
							Table:  expTable.Name,
							Column: col.Name,
							SQL:    sql,
						})
					}
				}
				if col.Index {
					sql := dialect.CreateIndex(expTable.Name, col)
					if sql != "" {
						ops = append(ops, SchemaOperation{
							Type:   "add_index",
							Table:  expTable.Name,
							Column: col.Name,
							SQL:    sql,
						})
					}
				}
			}
		}
	}

	// Find columns to add to existing tables
	for _, expTable := range expected.Tables {
		currTable, exists := currentTables[expTable.Name]
		if !exists {
			continue
		}

		currentCols := make(map[string]*sdata.DBColumn)
		for i := range currTable.Columns {
			c := &currTable.Columns[i]
			currentCols[c.Name] = c
		}

		for _, expCol := range expTable.Columns {
			if _, exists := currentCols[expCol.Name]; !exists {
				sql := dialect.AddColumn(expTable.Name, expCol)
				ops = append(ops, SchemaOperation{
					Type:   "add_column",
					Table:  expTable.Name,
					Column: expCol.Name,
					SQL:    sql,
				})

				if expCol.FKeyTable != "" {
					sql := dialect.AddForeignKey(expTable.Name, expCol)
					if sql != "" {
						ops = append(ops, SchemaOperation{
							Type:   "add_constraint",
							Table:  expTable.Name,
							Column: expCol.Name,
							SQL:    sql,
						})
					}
				}

				if expCol.FullText {
					sql := dialect.CreateSearchIndex(expTable.Name, expCol)
					if sql != "" {
						ops = append(ops, SchemaOperation{
							Type:   "add_index",
							Table:  expTable.Name,
							Column: expCol.Name,
							SQL:    sql,
						})
					}
				}
				if expCol.UniqueKey && !expCol.PrimaryKey {
					sql := dialect.CreateUniqueIndex(expTable.Name, expCol)
					if sql != "" {
						ops = append(ops, SchemaOperation{
							Type:   "add_index",
							Table:  expTable.Name,
							Column: expCol.Name,
							SQL:    sql,
						})
					}
				}
				if expCol.Index {
					sql := dialect.CreateIndex(expTable.Name, expCol)
					if sql != "" {
						ops = append(ops, SchemaOperation{
							Type:   "add_index",
							Table:  expTable.Name,
							Column: expCol.Name,
							SQL:    sql,
						})
					}
				}
			}
		}

		// Find columns to drop (destructive)
		if opts.Destructive {
			expectedCols := make(map[string]bool)
			for _, c := range expTable.Columns {
				expectedCols[c.Name] = true
			}

			for _, currCol := range currTable.Columns {
				if !expectedCols[currCol.Name] {
					sql := dialect.DropColumn(expTable.Name, currCol.Name)
					ops = append(ops, SchemaOperation{
						Type:   "drop_column",
						Table:  expTable.Name,
						Column: currCol.Name,
						SQL:    sql,
						Danger: true,
					})
				}
			}
		}
	}

	// Find tables to drop (destructive)
	if opts.Destructive {
		for tableName := range currentTables {
			if _, exists := expectedTables[tableName]; !exists {
				sql := dialect.DropTable(tableName)
				ops = append(ops, SchemaOperation{
					Type:   "drop_table",
					Table:  tableName,
					SQL:    sql,
					Danger: true,
				})
			}
		}
	}

	return ops
}

// GenerateDiffSQL converts operations to SQL strings
func GenerateDiffSQL(ops []SchemaOperation) []string {
	var sqls []string
	for _, op := range ops {
		if op.SQL != "" {
			sqls = append(sqls, op.SQL)
		}
	}
	return sqls
}

// SchemaDiffMultiDB computes schema diffs across multiple databases.
// Tables are assigned to databases based on the @database directive in the schema.
// Tables without a @database directive are assigned to the default database (first in dbTypes).
func SchemaDiffMultiDB(
	connections map[string]*sql.DB,
	dbTypes map[string]string,
	schemaBytes []byte,
	blocklist []string,
	opts DiffOptions,
) (map[string][]SchemaOperation, error) {
	// Parse the schema file
	ds, err := qcode.ParseSchema(schemaBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema: %w", err)
	}

	// Group columns by database (from @database directive)
	columnsByDB := make(map[string][]sdata.DBColumn)

	// Determine default database from first connection
	var defaultDB string
	for dbName := range connections {
		defaultDB = dbName
		break
	}

	for _, col := range ds.Columns {
		dbName := col.Database
		if dbName == "" {
			dbName = defaultDB
		}
		columnsByDB[dbName] = append(columnsByDB[dbName], col)
	}

	results := make(map[string][]SchemaOperation)

	// Run diff for each database
	for dbName, dbConn := range connections {
		dbType, ok := dbTypes[dbName]
		if !ok {
			continue
		}

		// Skip MongoDB (no DDL support)
		if dbType == "mongodb" {
			continue
		}

		cols := columnsByDB[dbName]
		if len(cols) == 0 {
			continue
		}

		// Determine schema for this database type
		schema := ds.Schema
		if schema == "" {
			schema = defaultSchemaForDBType(dbType)
		}

		// Create expected DBInfo for this database
		expected := sdata.NewDBInfo(dbType, ds.Version, schema, "", cols, nil, blocklist)

		// Get current database schema
		current, err := sdata.GetDBInfo(dbConn, dbType, blocklist)
		if err != nil {
			return nil, fmt.Errorf("failed to get schema for %s: %w", dbName, err)
		}

		// Compute diff
		ops := computeDiff(current, expected, opts)
		if len(ops) > 0 {
			results[dbName] = ops
		}
	}

	return results, nil
}
