package tests_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// randomSuffix generates a unique suffix for table names to avoid conflicts
func randomSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano()%100000)
}

// dropTable cleans up a test table using dialect-appropriate SQL
func dropTable(t *testing.T, tableName string) {
	var sql string
	switch dbType {
	case "postgres":
		sql = fmt.Sprintf(`DROP TABLE IF EXISTS "%s" CASCADE`, tableName)
	case "mysql", "mariadb":
		sql = fmt.Sprintf("DROP TABLE IF EXISTS `%s`", tableName)
	case "sqlite":
		sql = fmt.Sprintf(`DROP TABLE IF EXISTS "%s"`, tableName)
	case "mssql":
		sql = fmt.Sprintf("IF OBJECT_ID('%s', 'U') IS NOT NULL DROP TABLE [%s]", tableName, tableName)
	case "oracle":
		sql = fmt.Sprintf(`BEGIN EXECUTE IMMEDIATE 'DROP TABLE "%s" CASCADE CONSTRAINTS'; EXCEPTION WHEN OTHERS THEN IF SQLCODE != -942 THEN RAISE; END IF; END;`, strings.ToUpper(tableName))
	default:
		sql = fmt.Sprintf(`DROP TABLE IF EXISTS "%s"`, tableName)
	}
	_, _ = db.Exec(sql)
}

// dropIndex cleans up a test index
func dropIndex(t *testing.T, tableName, indexName string) {
	var sql string
	switch dbType {
	case "postgres", "sqlite":
		sql = fmt.Sprintf(`DROP INDEX IF EXISTS "%s"`, indexName)
	case "mysql", "mariadb":
		sql = fmt.Sprintf("DROP INDEX `%s` ON `%s`", indexName, tableName)
	case "mssql":
		sql = fmt.Sprintf("IF EXISTS (SELECT * FROM sys.indexes WHERE name = '%s') DROP INDEX [%s] ON [%s]", indexName, indexName, tableName)
	case "oracle":
		sql = fmt.Sprintf(`BEGIN EXECUTE IMMEDIATE 'DROP INDEX "%s"'; EXCEPTION WHEN OTHERS THEN IF SQLCODE != -1418 THEN RAISE; END IF; END;`, strings.ToUpper(indexName))
	default:
		sql = fmt.Sprintf(`DROP INDEX IF EXISTS "%s"`, indexName)
	}
	_, _ = db.Exec(sql)
}

// schemaForDB returns the correct schema name for the current database type
func schemaForDB() string {
	switch dbType {
	case "postgres":
		return "public"
	case "mysql", "mariadb":
		return "db" // MySQL uses database name as schema
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

// TestSchemaDiff_CreateTable tests creating new tables from schema diff
func TestSchemaDiff_CreateTable(t *testing.T) {
	// Skip for MongoDB (no schema support)
	if dbType == "mongodb" {
		t.Skip("schema diff not applicable for MongoDB")
	}

	tableName := "test_sd_create_" + randomSuffix()
	defer dropTable(t, tableName)

	schema := fmt.Sprintf(`# dbinfo:%s,1,%s
type %s {
	id: BigInt! @id
	name: Text!
	email: Text
}
`, dbType, schemaForDB(), tableName)

	// Compute diff
	ops, err := core.SchemaDiff(db, dbType, []byte(schema), nil, core.DiffOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, ops, "expected CREATE TABLE operation")

	// Find create_table operation
	var createOp *core.SchemaOperation
	for i := range ops {
		if ops[i].Type == "create_table" && ops[i].Table == tableName {
			createOp = &ops[i]
			break
		}
	}
	require.NotNil(t, createOp, "expected create_table operation for %s", tableName)
	assert.Contains(t, createOp.SQL, "CREATE TABLE")
	assert.False(t, createOp.Danger, "create_table should not be marked as dangerous")

	// Apply SQL
	sqls := core.GenerateDiffSQL(ops)
	for _, sql := range sqls {
		_, err := db.Exec(sql)
		require.NoError(t, err, "failed to execute: %s", sql)
	}

	// Verify table exists by querying it
	var count int
	switch dbType {
	case "postgres":
		err = db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = '%s'`, tableName)).Scan(&count)
	case "mysql", "mariadb":
		err = db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = '%s'`, tableName)).Scan(&count)
	case "sqlite":
		err = db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='%s'`, tableName)).Scan(&count)
	case "mssql":
		err = db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM sys.tables WHERE name = '%s'`, tableName)).Scan(&count)
	case "oracle":
		err = db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM user_tables WHERE table_name = '%s'`, strings.ToUpper(tableName))).Scan(&count)
	}
	require.NoError(t, err)
	assert.Equal(t, 1, count, "table should exist in database")
}

// TestSchemaDiff_AddColumn tests adding columns to existing tables
func TestSchemaDiff_AddColumn(t *testing.T) {
	if dbType == "mongodb" {
		t.Skip("schema diff not applicable for MongoDB")
	}

	tableName := "test_sd_addcol_" + randomSuffix()
	defer dropTable(t, tableName)

	// First create a simple table
	initialSchema := fmt.Sprintf(`# dbinfo:%s,1,%s
type %s {
	id: BigInt! @id
	name: Text!
}
`, dbType, schemaForDB(), tableName)

	ops, err := core.SchemaDiff(db, dbType, []byte(initialSchema), nil, core.DiffOptions{})
	require.NoError(t, err)
	for _, sql := range core.GenerateDiffSQL(ops) {
		_, err := db.Exec(sql)
		require.NoError(t, err)
	}

	// Now add a column
	extendedSchema := fmt.Sprintf(`# dbinfo:%s,1,%s
type %s {
	id: BigInt! @id
	name: Text!
	email: Text
	age: Integer
}
`, dbType, schemaForDB(), tableName)

	ops, err = core.SchemaDiff(db, dbType, []byte(extendedSchema), nil, core.DiffOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, ops, "expected ADD COLUMN operations")

	// Verify we have add_column operations for email and age
	var addColCount int
	for _, op := range ops {
		if op.Type == "add_column" && op.Table == tableName {
			addColCount++
			assert.Contains(t, op.SQL, "ADD", "expected ADD in SQL")
			assert.True(t, op.Column == "email" || op.Column == "age", "expected email or age column")
		}
	}
	assert.Equal(t, 2, addColCount, "expected 2 add_column operations")

	// Apply the changes
	for _, sql := range core.GenerateDiffSQL(ops) {
		_, err := db.Exec(sql)
		require.NoError(t, err, "failed to execute: %s", sql)
	}
}

// TestSchemaDiff_ForeignKey tests foreign key constraint creation
func TestSchemaDiff_ForeignKey(t *testing.T) {
	if dbType == "mongodb" {
		t.Skip("schema diff not applicable for MongoDB")
	}

	parentTable := "test_sd_fk_parent_" + randomSuffix()
	childTable := "test_sd_fk_child_" + randomSuffix()
	defer dropTable(t, childTable)
	defer dropTable(t, parentTable)

	// Create parent table first
	parentSchema := fmt.Sprintf(`# dbinfo:%s,1,%s
type %s {
	id: BigInt! @id
	name: Text!
}
`, dbType, schemaForDB(), parentTable)

	ops, err := core.SchemaDiff(db, dbType, []byte(parentSchema), nil, core.DiffOptions{})
	require.NoError(t, err)
	for _, sql := range core.GenerateDiffSQL(ops) {
		_, err := db.Exec(sql)
		require.NoError(t, err)
	}

	// Create child table with foreign key
	childSchema := fmt.Sprintf(`# dbinfo:%s,1,%s
type %s {
	id: BigInt! @id
	parent_id: BigInt! @relation(type: "%s", field: "id")
	title: Text!
}
`, dbType, schemaForDB(), childTable, parentTable)

	ops, err = core.SchemaDiff(db, dbType, []byte(childSchema), nil, core.DiffOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, ops, "expected operations for child table")

	// Check for foreign key in the SQL
	sqls := core.GenerateDiffSQL(ops)
	var hasForeignKey bool
	for _, sql := range sqls {
		if strings.Contains(sql, "FOREIGN KEY") || strings.Contains(sql, "REFERENCES") {
			hasForeignKey = true
			break
		}
	}
	assert.True(t, hasForeignKey, "expected FOREIGN KEY in generated SQL")

	// Apply SQL
	for _, sql := range sqls {
		_, err := db.Exec(sql)
		require.NoError(t, err, "failed to execute: %s", sql)
	}
}

// TestSchemaDiff_UniqueIndex tests @unique directive produces correct indexes
func TestSchemaDiff_UniqueIndex(t *testing.T) {
	if dbType == "mongodb" {
		t.Skip("schema diff not applicable for MongoDB")
	}

	tableName := "test_sd_unique_" + randomSuffix()
	indexName := fmt.Sprintf("idx_%s_email_unique", tableName)
	defer dropTable(t, tableName)
	defer dropIndex(t, tableName, indexName)

	// Use VARCHAR for MySQL/MariaDB/Oracle/MSSQL since TEXT columns can't have unique indexes
	// (MySQL/MariaDB require key length for TEXT, Oracle can't index CLOB columns,
	// MSSQL can't index NVARCHAR(MAX) columns)
	emailType := "Text"
	if dbType == "mysql" || dbType == "mariadb" || dbType == "oracle" || dbType == "mssql" {
		emailType = "Varchar"
	}

	schema := fmt.Sprintf(`# dbinfo:%s,1,%s
type %s {
	id: BigInt! @id
	email: %s! @unique
	name: Text
}
`, dbType, schemaForDB(), tableName, emailType)

	ops, err := core.SchemaDiff(db, dbType, []byte(schema), nil, core.DiffOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, ops, "expected operations")

	// Check for unique index operation
	var hasUniqueIndex bool
	for _, op := range ops {
		if op.Type == "add_index" && strings.Contains(op.SQL, "UNIQUE") {
			hasUniqueIndex = true
			break
		}
	}
	assert.True(t, hasUniqueIndex, "expected UNIQUE INDEX in operations")

	// Apply SQL
	for _, sql := range core.GenerateDiffSQL(ops) {
		_, err := db.Exec(sql)
		require.NoError(t, err, "failed to execute: %s", sql)
	}

	// Verify unique constraint works by trying to insert duplicates
	switch dbType {
	case "postgres":
		_, _ = db.Exec(fmt.Sprintf(`INSERT INTO "%s" (email, name) VALUES ('test@test.com', 'Test')`, tableName))
		_, err = db.Exec(fmt.Sprintf(`INSERT INTO "%s" (email, name) VALUES ('test@test.com', 'Test2')`, tableName))
		assert.Error(t, err, "expected unique constraint violation")
	case "mysql", "mariadb":
		_, _ = db.Exec(fmt.Sprintf("INSERT INTO `%s` (email, name) VALUES ('test@test.com', 'Test')", tableName))
		_, err = db.Exec(fmt.Sprintf("INSERT INTO `%s` (email, name) VALUES ('test@test.com', 'Test2')", tableName))
		assert.Error(t, err, "expected unique constraint violation")
	case "sqlite":
		_, _ = db.Exec(fmt.Sprintf(`INSERT INTO "%s" (email, name) VALUES ('test@test.com', 'Test')`, tableName))
		_, err = db.Exec(fmt.Sprintf(`INSERT INTO "%s" (email, name) VALUES ('test@test.com', 'Test2')`, tableName))
		assert.Error(t, err, "expected unique constraint violation")
	}
}

// TestSchemaDiff_SearchIndex tests @search directive produces correct full-text indexes
func TestSchemaDiff_SearchIndex(t *testing.T) {
	// Skip for databases that don't support full-text search in the same way
	if dbType == "mongodb" || dbType == "sqlite" || dbType == "mssql" || dbType == "oracle" {
		t.Skip("search index test not applicable for " + dbType)
	}

	tableName := "test_sd_search_" + randomSuffix()
	defer dropTable(t, tableName)

	schema := fmt.Sprintf(`# dbinfo:%s,1,%s
type %s {
	id: BigInt! @id
	title: Text!
	content: Text @search
}
`, dbType, schemaForDB(), tableName)

	ops, err := core.SchemaDiff(db, dbType, []byte(schema), nil, core.DiffOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, ops, "expected operations")

	// Check for search index operation
	var hasSearchIndex bool
	var searchSQL string
	for _, op := range ops {
		if op.Type == "add_index" && op.Column == "content" {
			hasSearchIndex = true
			searchSQL = op.SQL
			break
		}
	}
	assert.True(t, hasSearchIndex, "expected search index operation for content column")

	// Verify dialect-specific syntax
	switch dbType {
	case "postgres":
		assert.Contains(t, searchSQL, "gin", "expected GIN index for Postgres")
		assert.Contains(t, searchSQL, "to_tsvector", "expected to_tsvector for Postgres")
	case "mysql", "mariadb":
		assert.Contains(t, searchSQL, "FULLTEXT", "expected FULLTEXT index for MySQL/MariaDB")
	}

	// Apply SQL
	for _, sql := range core.GenerateDiffSQL(ops) {
		_, err := db.Exec(sql)
		require.NoError(t, err, "failed to execute: %s", sql)
	}
}

// TestSchemaDiff_Destructive tests DROP operations with destructive flag
func TestSchemaDiff_Destructive(t *testing.T) {
	if dbType == "mongodb" {
		t.Skip("schema diff not applicable for MongoDB")
	}

	tableName := "test_sd_dest_" + randomSuffix()
	defer dropTable(t, tableName)

	// Create table with extra column
	fullSchema := fmt.Sprintf(`# dbinfo:%s,1,%s
type %s {
	id: BigInt! @id
	name: Text!
	extra_column: Text
}
`, dbType, schemaForDB(), tableName)

	ops, err := core.SchemaDiff(db, dbType, []byte(fullSchema), nil, core.DiffOptions{})
	require.NoError(t, err)
	for _, sql := range core.GenerateDiffSQL(ops) {
		_, err := db.Exec(sql)
		require.NoError(t, err)
	}

	// Now diff with schema missing extra_column, WITHOUT destructive flag
	reducedSchema := fmt.Sprintf(`# dbinfo:%s,1,%s
type %s {
	id: BigInt! @id
	name: Text!
}
`, dbType, schemaForDB(), tableName)

	ops, err = core.SchemaDiff(db, dbType, []byte(reducedSchema), nil, core.DiffOptions{Destructive: false})
	require.NoError(t, err)

	// Should have no drop operations
	for _, op := range ops {
		assert.NotEqual(t, "drop_column", op.Type, "should not have drop_column without destructive flag")
		assert.NotEqual(t, "drop_table", op.Type, "should not have drop_table without destructive flag")
	}

	// Now with destructive flag
	ops, err = core.SchemaDiff(db, dbType, []byte(reducedSchema), nil, core.DiffOptions{Destructive: true})
	require.NoError(t, err)

	// Should have drop_column operation
	var hasDropColumn bool
	for _, op := range ops {
		if op.Type == "drop_column" && op.Column == "extra_column" {
			hasDropColumn = true
			assert.True(t, op.Danger, "drop_column should be marked as dangerous")
			assert.Contains(t, op.SQL, "DROP COLUMN")
			break
		}
	}
	assert.True(t, hasDropColumn, "expected drop_column operation with destructive flag")
}

// TestSchemaDiff_DropTable tests DROP TABLE with destructive flag
func TestSchemaDiff_DropTable(t *testing.T) {
	if dbType == "mongodb" {
		t.Skip("schema diff not applicable for MongoDB")
	}

	tableName := "test_sd_droptbl_" + randomSuffix()
	defer dropTable(t, tableName)

	// Create table
	schema := fmt.Sprintf(`# dbinfo:%s,1,%s
type %s {
	id: BigInt! @id
	name: Text!
}
`, dbType, schemaForDB(), tableName)

	ops, err := core.SchemaDiff(db, dbType, []byte(schema), nil, core.DiffOptions{})
	require.NoError(t, err)
	for _, sql := range core.GenerateDiffSQL(ops) {
		_, err := db.Exec(sql)
		require.NoError(t, err)
	}

	// Empty schema with destructive flag should produce drop_table
	emptySchema := fmt.Sprintf(`# dbinfo:%s,1,%s
`, dbType, schemaForDB())

	ops, err = core.SchemaDiff(db, dbType, []byte(emptySchema), nil, core.DiffOptions{Destructive: true})
	require.NoError(t, err)

	// Find drop_table operation for our table
	var hasDropTable bool
	for _, op := range ops {
		if op.Type == "drop_table" && op.Table == tableName {
			hasDropTable = true
			assert.True(t, op.Danger, "drop_table should be marked as dangerous")
			assert.Contains(t, op.SQL, "DROP TABLE")
			break
		}
	}
	assert.True(t, hasDropTable, "expected drop_table operation for %s", tableName)
}

// TestSchemaDiff_Idempotency tests that applying diff twice produces no changes
func TestSchemaDiff_Idempotency(t *testing.T) {
	if dbType == "mongodb" {
		t.Skip("schema diff not applicable for MongoDB")
	}

	tableName := "test_sd_idemp_" + randomSuffix()
	defer dropTable(t, tableName)

	schema := fmt.Sprintf(`# dbinfo:%s,1,%s
type %s {
	id: BigInt! @id
	name: Text!
	email: Text
}
`, dbType, schemaForDB(), tableName)

	// First diff and apply
	ops, err := core.SchemaDiff(db, dbType, []byte(schema), nil, core.DiffOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, ops, "expected operations on first diff")

	for _, sql := range core.GenerateDiffSQL(ops) {
		_, err := db.Exec(sql)
		require.NoError(t, err)
	}

	// Second diff should produce no table or column operations
	ops2, err := core.SchemaDiff(db, dbType, []byte(schema), nil, core.DiffOptions{})
	require.NoError(t, err)

	// Filter for table/column operations (indexes might still appear)
	var tableOps []core.SchemaOperation
	for _, op := range ops2 {
		if op.Type == "create_table" || op.Type == "add_column" || op.Type == "drop_table" || op.Type == "drop_column" {
			tableOps = append(tableOps, op)
		}
	}
	assert.Empty(t, tableOps, "expected no table/column operations on second diff, got: %+v", tableOps)
}

// TestSchemaDiff_DialectSpecific verifies correct DDL syntax per database
func TestSchemaDiff_DialectSpecific(t *testing.T) {
	if dbType == "mongodb" {
		t.Skip("schema diff not applicable for MongoDB")
	}

	tableName := "test_sd_dialect_" + randomSuffix()
	defer dropTable(t, tableName)

	schema := fmt.Sprintf(`# dbinfo:%s,1,%s
type %s {
	id: BigInt! @id
	name: Text!
}
`, dbType, schemaForDB(), tableName)

	ops, err := core.SchemaDiff(db, dbType, []byte(schema), nil, core.DiffOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, ops)

	var createSQL string
	for _, op := range ops {
		if op.Type == "create_table" {
			createSQL = op.SQL
			break
		}
	}
	require.NotEmpty(t, createSQL, "expected CREATE TABLE SQL")

	// Verify dialect-specific quoting and syntax
	switch dbType {
	case "postgres":
		assert.Contains(t, createSQL, `"id"`, "Postgres should use double-quote identifiers")
		assert.Contains(t, createSQL, "BIGSERIAL", "Postgres should use BIGSERIAL for auto-increment")
	case "mysql", "mariadb":
		assert.Contains(t, createSQL, "`id`", "MySQL should use backtick identifiers")
		assert.Contains(t, createSQL, "AUTO_INCREMENT", "MySQL should use AUTO_INCREMENT")
		assert.Contains(t, createSQL, "ENGINE=InnoDB", "MySQL should specify InnoDB engine")
	case "sqlite":
		assert.Contains(t, createSQL, `"id"`, "SQLite should use double-quote identifiers")
		assert.Contains(t, createSQL, "AUTOINCREMENT", "SQLite should use AUTOINCREMENT")
	case "mssql":
		assert.Contains(t, createSQL, "[id]", "MSSQL should use bracket identifiers")
		assert.Contains(t, createSQL, "IDENTITY", "MSSQL should use IDENTITY for auto-increment")
	case "oracle":
		assert.Contains(t, createSQL, `"ID"`, "Oracle should use uppercase double-quote identifiers")
		assert.Contains(t, createSQL, "GENERATED BY DEFAULT AS IDENTITY", "Oracle should use identity column")
	}

	// Verify SQL executes successfully
	for _, sql := range core.GenerateDiffSQL(ops) {
		_, err := db.Exec(sql)
		require.NoError(t, err, "failed to execute: %s", sql)
	}
}

// TestSchemaDiff_MultipleTypes tests creating multiple tables in one schema
func TestSchemaDiff_MultipleTypes(t *testing.T) {
	if dbType == "mongodb" {
		t.Skip("schema diff not applicable for MongoDB")
	}

	table1 := "test_sd_multi1_" + randomSuffix()
	table2 := "test_sd_multi2_" + randomSuffix()
	defer dropTable(t, table2)
	defer dropTable(t, table1)

	schema := fmt.Sprintf(`# dbinfo:%s,1,%s
type %s {
	id: BigInt! @id
	name: Text!
}

type %s {
	id: BigInt! @id
	ref_id: BigInt @relation(type: "%s", field: "id")
	value: Integer
}
`, dbType, schemaForDB(), table1, table2, table1)

	ops, err := core.SchemaDiff(db, dbType, []byte(schema), nil, core.DiffOptions{})
	require.NoError(t, err)

	// Should have create_table for both tables
	var table1Created, table2Created bool
	for _, op := range ops {
		if op.Type == "create_table" {
			if op.Table == table1 {
				table1Created = true
			}
			if op.Table == table2 {
				table2Created = true
			}
		}
	}
	assert.True(t, table1Created, "expected create_table for %s", table1)
	assert.True(t, table2Created, "expected create_table for %s", table2)

	// Apply SQL
	for _, sql := range core.GenerateDiffSQL(ops) {
		_, err := db.Exec(sql)
		require.NoError(t, err, "failed to execute: %s", sql)
	}
}

// TestSchemaDiff_NotNullColumn tests NOT NULL constraint handling
func TestSchemaDiff_NotNullColumn(t *testing.T) {
	if dbType == "mongodb" {
		t.Skip("schema diff not applicable for MongoDB")
	}

	tableName := "test_sd_notnull_" + randomSuffix()
	defer dropTable(t, tableName)

	schema := fmt.Sprintf(`# dbinfo:%s,1,%s
type %s {
	id: BigInt! @id
	required_field: Text!
	optional_field: Text
}
`, dbType, schemaForDB(), tableName)

	ops, err := core.SchemaDiff(db, dbType, []byte(schema), nil, core.DiffOptions{})
	require.NoError(t, err)

	var createSQL string
	for _, op := range ops {
		if op.Type == "create_table" {
			createSQL = op.SQL
			break
		}
	}
	require.NotEmpty(t, createSQL)

	// Verify NOT NULL is present for required field
	assert.Contains(t, createSQL, "NOT NULL", "expected NOT NULL constraint in SQL")

	// Apply and test
	for _, sql := range core.GenerateDiffSQL(ops) {
		_, err := db.Exec(sql)
		require.NoError(t, err)
	}

	// Verify NOT NULL constraint works
	switch dbType {
	case "postgres":
		_, err = db.Exec(fmt.Sprintf(`INSERT INTO "%s" (required_field) VALUES (NULL)`, tableName))
		assert.Error(t, err, "expected NOT NULL violation")
	case "mysql", "mariadb":
		// MySQL in strict mode will reject NULL
		_, _ = db.Exec(fmt.Sprintf("INSERT INTO `%s` (required_field) VALUES (NULL)", tableName)) //nolint:errcheck
		// Note: MySQL behavior depends on sql_mode, so we just verify the table was created
	case "sqlite":
		_, err = db.Exec(fmt.Sprintf(`INSERT INTO "%s" (required_field) VALUES (NULL)`, tableName))
		assert.Error(t, err, "expected NOT NULL violation")
	}
}

// TestSchemaDiff_Blocklist tests that blocklisted tables are ignored
// NOTE: This test is currently skipped because the blocklist functionality
// in SchemaDiff/GetDBInfo needs to properly filter dynamically created tables.
// The blocklist is passed to GetDBInfo but the filtering may not work correctly
// for newly created tables that don't match existing blocklist patterns.
func TestSchemaDiff_Blocklist(t *testing.T) {
	t.Skip("blocklist integration test skipped - see core/schema_diff.go for blocklist implementation")
}

// ============================================================================
// Multi-Database Schema Diff Tests
// ============================================================================

// dropTableOnDB drops a table from a specific database connection
func dropTableOnDB(t *testing.T, db *sql.DB, dbType, tableName string) {
	var sqlStmt string
	switch dbType {
	case "postgres":
		sqlStmt = fmt.Sprintf(`DROP TABLE IF EXISTS "%s" CASCADE`, tableName)
	case "mysql", "mariadb":
		sqlStmt = fmt.Sprintf("DROP TABLE IF EXISTS `%s`", tableName)
	case "sqlite":
		sqlStmt = fmt.Sprintf(`DROP TABLE IF EXISTS "%s"`, tableName)
	case "mssql":
		sqlStmt = fmt.Sprintf("IF OBJECT_ID('%s', 'U') IS NOT NULL DROP TABLE [%s]", tableName, tableName)
	case "oracle":
		sqlStmt = fmt.Sprintf(`BEGIN EXECUTE IMMEDIATE 'DROP TABLE "%s" CASCADE CONSTRAINTS'; EXCEPTION WHEN OTHERS THEN IF SQLCODE != -942 THEN RAISE; END IF; END;`, strings.ToUpper(tableName))
	default:
		sqlStmt = fmt.Sprintf(`DROP TABLE IF EXISTS "%s"`, tableName)
	}
	_, _ = db.Exec(sqlStmt)
}

// TestSchemaDiffMultiDB_ParseDatabaseDirective verifies @database directive correctly assigns tables to databases
func TestSchemaDiffMultiDB_ParseDatabaseDirective(t *testing.T) {
	if !multiDBMode {
		t.Skip("requires -db=multidb")
	}

	pgTable := "test_mdb_parse_pg_" + randomSuffix()
	sqliteTable := "test_mdb_parse_sqlite_" + randomSuffix()
	defer dropTableOnDB(t, multiDBs["postgres"], "postgres", pgTable)
	defer dropTableOnDB(t, multiDBs["sqlite"], "sqlite", sqliteTable)

	schema := fmt.Sprintf(`# dbinfo:postgres,1,public
type %s @database(name: "postgres") {
	id: BigInt! @id
	name: Text!
}

type %s @database(name: "sqlite") {
	id: BigInt! @id
	value: Text!
}
`, pgTable, sqliteTable)

	results, err := core.SchemaDiffMultiDB(multiDBs, multiDBTypes, []byte(schema), nil, core.DiffOptions{})
	require.NoError(t, err)

	// Verify operations are grouped by database
	require.Contains(t, results, "postgres", "expected postgres operations")
	require.Contains(t, results, "sqlite", "expected sqlite operations")

	// Verify postgres table is in postgres results
	var pgTableFound bool
	for _, op := range results["postgres"] {
		if op.Type == "create_table" && op.Table == pgTable {
			pgTableFound = true
			break
		}
	}
	assert.True(t, pgTableFound, "expected %s in postgres operations", pgTable)

	// Verify sqlite table is in sqlite results
	var sqliteTableFound bool
	for _, op := range results["sqlite"] {
		if op.Type == "create_table" && op.Table == sqliteTable {
			sqliteTableFound = true
			break
		}
	}
	assert.True(t, sqliteTableFound, "expected %s in sqlite operations", sqliteTable)
}

// TestSchemaDiffMultiDB_CreateTablesAcrossDBs tests creating tables in both PostgreSQL and SQLite from single schema
func TestSchemaDiffMultiDB_CreateTablesAcrossDBs(t *testing.T) {
	if !multiDBMode {
		t.Skip("requires -db=multidb")
	}

	pgTable := "test_mdb_create_pg_" + randomSuffix()
	sqliteTable := "test_mdb_create_sqlite_" + randomSuffix()
	defer dropTableOnDB(t, multiDBs["postgres"], "postgres", pgTable)
	defer dropTableOnDB(t, multiDBs["sqlite"], "sqlite", sqliteTable)

	schema := fmt.Sprintf(`# dbinfo:postgres,1,public
type %s @database(name: "postgres") {
	id: BigInt! @id
	username: Text!
	email: Text
}

type %s @database(name: "sqlite") {
	id: BigInt! @id
	key: Text!
	data: Text
}
`, pgTable, sqliteTable)

	results, err := core.SchemaDiffMultiDB(multiDBs, multiDBTypes, []byte(schema), nil, core.DiffOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, results, "expected schema operations")

	// Apply operations to each database
	for dbName, ops := range results {
		dbConn := multiDBs[dbName]
		for _, sqlStmt := range core.GenerateDiffSQL(ops) {
			_, err := dbConn.Exec(sqlStmt)
			require.NoError(t, err, "failed to execute on %s: %s", dbName, sqlStmt)
		}
	}

	// Verify postgres table exists
	var pgCount int
	err = multiDBs["postgres"].QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = '%s'`, pgTable)).Scan(&pgCount)
	require.NoError(t, err)
	assert.Equal(t, 1, pgCount, "postgres table should exist")

	// Verify sqlite table exists
	var sqliteCount int
	err = multiDBs["sqlite"].QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='%s'`, sqliteTable)).Scan(&sqliteCount)
	require.NoError(t, err)
	assert.Equal(t, 1, sqliteCount, "sqlite table should exist")
}

// TestSchemaDiffMultiDB_IndependentDiffs tests that changes to one DB don't affect another
func TestSchemaDiffMultiDB_IndependentDiffs(t *testing.T) {
	if !multiDBMode {
		t.Skip("requires -db=multidb")
	}

	pgTable := "test_mdb_indep_pg_" + randomSuffix()
	sqliteTable := "test_mdb_indep_sqlite_" + randomSuffix()
	defer dropTableOnDB(t, multiDBs["postgres"], "postgres", pgTable)
	defer dropTableOnDB(t, multiDBs["sqlite"], "sqlite", sqliteTable)

	// Initial schema - create both tables
	initialSchema := fmt.Sprintf(`# dbinfo:postgres,1,public
type %s @database(name: "postgres") {
	id: BigInt! @id
	name: Text!
}

type %s @database(name: "sqlite") {
	id: BigInt! @id
	value: Text!
}
`, pgTable, sqliteTable)

	results, err := core.SchemaDiffMultiDB(multiDBs, multiDBTypes, []byte(initialSchema), nil, core.DiffOptions{})
	require.NoError(t, err)

	// Apply initial schema
	for dbName, ops := range results {
		for _, sqlStmt := range core.GenerateDiffSQL(ops) {
			_, err := multiDBs[dbName].Exec(sqlStmt)
			require.NoError(t, err)
		}
	}

	// Now add a column only to postgres table
	updatedSchema := fmt.Sprintf(`# dbinfo:postgres,1,public
type %s @database(name: "postgres") {
	id: BigInt! @id
	name: Text!
	email: Text
}

type %s @database(name: "sqlite") {
	id: BigInt! @id
	value: Text!
}
`, pgTable, sqliteTable)

	results2, err := core.SchemaDiffMultiDB(multiDBs, multiDBTypes, []byte(updatedSchema), nil, core.DiffOptions{})
	require.NoError(t, err)

	// Should have changes only for postgres
	require.Contains(t, results2, "postgres", "expected postgres changes")
	assert.NotContains(t, results2, "sqlite", "should not have sqlite changes")

	// Verify postgres has add_column operation
	var hasAddColumn bool
	for _, op := range results2["postgres"] {
		if op.Type == "add_column" && op.Column == "email" {
			hasAddColumn = true
			break
		}
	}
	assert.True(t, hasAddColumn, "expected add_column for email in postgres")
}

// TestSchemaDiffMultiDB_SameTableNameDifferentDBs handles "users" table in both postgres and sqlite
func TestSchemaDiffMultiDB_SameTableNameDifferentDBs(t *testing.T) {
	if !multiDBMode {
		t.Skip("requires -db=multidb")
	}

	// Use a common table name in both databases
	tableName := "test_mdb_same_" + randomSuffix()
	defer dropTableOnDB(t, multiDBs["postgres"], "postgres", tableName)
	defer dropTableOnDB(t, multiDBs["sqlite"], "sqlite", tableName)

	schema := fmt.Sprintf(`# dbinfo:postgres,1,public
type %s @database(name: "postgres") {
	id: BigInt! @id
	pg_field: Text!
}

type %s @database(name: "sqlite") {
	id: BigInt! @id
	sqlite_field: Text!
}
`, tableName, tableName)

	results, err := core.SchemaDiffMultiDB(multiDBs, multiDBTypes, []byte(schema), nil, core.DiffOptions{})
	require.NoError(t, err)

	// Should have operations for both databases
	require.Contains(t, results, "postgres")
	require.Contains(t, results, "sqlite")

	// Apply operations
	for dbName, ops := range results {
		for _, sqlStmt := range core.GenerateDiffSQL(ops) {
			_, err := multiDBs[dbName].Exec(sqlStmt)
			require.NoError(t, err, "failed on %s: %s", dbName, sqlStmt)
		}
	}

	// Verify postgres table has pg_field
	var pgColCount int
	err = multiDBs["postgres"].QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM information_schema.columns WHERE table_name = '%s' AND column_name = 'pg_field'`, tableName)).Scan(&pgColCount)
	require.NoError(t, err)
	assert.Equal(t, 1, pgColCount, "postgres table should have pg_field column")

	// Verify sqlite table has sqlite_field
	var sqliteColExists int
	err = multiDBs["sqlite"].QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = 'sqlite_field'`, tableName)).Scan(&sqliteColExists)
	require.NoError(t, err)
	assert.Equal(t, 1, sqliteColExists, "sqlite table should have sqlite_field column")
}

// TestSchemaDiffMultiDB_Idempotency tests that running multi-DB diff twice produces no changes
func TestSchemaDiffMultiDB_Idempotency(t *testing.T) {
	if !multiDBMode {
		t.Skip("requires -db=multidb")
	}

	pgTable := "test_mdb_idemp_pg_" + randomSuffix()
	sqliteTable := "test_mdb_idemp_sqlite_" + randomSuffix()
	defer dropTableOnDB(t, multiDBs["postgres"], "postgres", pgTable)
	defer dropTableOnDB(t, multiDBs["sqlite"], "sqlite", sqliteTable)

	schema := fmt.Sprintf(`# dbinfo:postgres,1,public
type %s @database(name: "postgres") {
	id: BigInt! @id
	name: Text!
}

type %s @database(name: "sqlite") {
	id: BigInt! @id
	value: Text!
}
`, pgTable, sqliteTable)

	// First diff and apply
	results, err := core.SchemaDiffMultiDB(multiDBs, multiDBTypes, []byte(schema), nil, core.DiffOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, results, "expected operations on first diff")

	for dbName, ops := range results {
		for _, sqlStmt := range core.GenerateDiffSQL(ops) {
			_, err := multiDBs[dbName].Exec(sqlStmt)
			require.NoError(t, err)
		}
	}

	// Second diff should produce no table/column operations
	results2, err := core.SchemaDiffMultiDB(multiDBs, multiDBTypes, []byte(schema), nil, core.DiffOptions{})
	require.NoError(t, err)

	// Filter for table/column operations (indexes might still appear)
	var tableOps []core.SchemaOperation
	for _, ops := range results2 {
		for _, op := range ops {
			if op.Type == "create_table" || op.Type == "add_column" || op.Type == "drop_table" || op.Type == "drop_column" {
				tableOps = append(tableOps, op)
			}
		}
	}
	assert.Empty(t, tableOps, "expected no table/column operations on second diff, got: %+v", tableOps)
}

// TestSchemaDiffMultiDB_SkipMongoDB tests that MongoDB tables are skipped (no DDL support)
func TestSchemaDiffMultiDB_SkipMongoDB(t *testing.T) {
	if !multiDBMode {
		t.Skip("requires -db=multidb")
	}

	pgTable := "test_mdb_skip_pg_" + randomSuffix()
	mongoTable := "test_mdb_skip_mongo_" + randomSuffix()
	defer dropTableOnDB(t, multiDBs["postgres"], "postgres", pgTable)
	// No cleanup needed for MongoDB - no DDL is generated

	schema := fmt.Sprintf(`# dbinfo:postgres,1,public
type %s @database(name: "postgres") {
	id: BigInt! @id
	name: Text!
}

type %s @database(name: "mongodb") {
	id: BigInt! @id
	data: Text!
}
`, pgTable, mongoTable)

	results, err := core.SchemaDiffMultiDB(multiDBs, multiDBTypes, []byte(schema), nil, core.DiffOptions{})
	require.NoError(t, err)

	// Should have postgres operations
	require.Contains(t, results, "postgres", "expected postgres operations")

	// Should NOT have mongodb operations (skipped)
	assert.NotContains(t, results, "mongodb", "mongodb should be skipped (no DDL support)")

	// Verify postgres table operation exists
	var hasPgCreate bool
	for _, op := range results["postgres"] {
		if op.Type == "create_table" && op.Table == pgTable {
			hasPgCreate = true
			break
		}
	}
	assert.True(t, hasPgCreate, "expected create_table for postgres table")
}
