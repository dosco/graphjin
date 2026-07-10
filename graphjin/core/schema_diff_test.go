package core

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestComputeDiff_CreateTable(t *testing.T) {
	current := &sdata.DBInfo{
		Type:   "postgres",
		Tables: []sdata.DBTable{},
	}

	expected := &sdata.DBInfo{
		Type: "postgres",
		Tables: []sdata.DBTable{
			{
				Name: "users",
				Columns: []sdata.DBColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
					{Name: "name", Type: "text", NotNull: true},
					{Name: "email", Type: "text"},
				},
			},
		},
	}

	ops := computeDiff(current, expected, DiffOptions{Destructive: false})

	if len(ops) == 0 {
		t.Fatal("expected at least one operation")
	}

	// Should have a create_table operation
	found := false
	for _, op := range ops {
		if op.Type == "create_table" && op.Table == "users" {
			found = true
			if !strings.Contains(op.SQL, "CREATE TABLE") {
				t.Errorf("expected CREATE TABLE in SQL, got: %s", op.SQL)
			}
			if !strings.Contains(op.SQL, `"id"`) {
				t.Errorf("expected id column in SQL, got: %s", op.SQL)
			}
		}
	}

	if !found {
		t.Error("expected create_table operation for users table")
	}
}

func TestSupportsSchemaDDLBoundary(t *testing.T) {
	for _, dbType := range []string{"postgres", "mysql", "mariadb", "sqlite", "mssql", "oracle", "snowflake"} {
		if !SupportsSchemaDDL(dbType) {
			t.Fatalf("SupportsSchemaDDL(%q) = false, want true", dbType)
		}
	}
	for _, dbType := range []string{"bigquery", "redshift", "cassandra", "mongodb", "codesql", "graphjin", "workflow", "files"} {
		if SupportsSchemaDDL(dbType) {
			t.Fatalf("SupportsSchemaDDL(%q) = true, want false", dbType)
		}
	}
}

func TestGenerateSchemaSQL_BigQueryRendererAvailableForSimulator(t *testing.T) {
	sqls, err := GenerateSchemaSQL("bigquery", []byte(`
type users {
  id: Bigint! @id
  email: Text!
}
`), nil)
	if err != nil {
		t.Fatalf("GenerateSchemaSQL: %v", err)
	}
	joined := strings.Join(sqls, "\n")
	if !strings.Contains(joined, "CREATE TABLE `users`") {
		t.Fatalf("BigQuery renderer did not create table SQL:\n%s", joined)
	}
	if !strings.Contains(joined, "`id` INT64 NOT NULL PRIMARY KEY NOT ENFORCED") {
		t.Fatalf("BigQuery renderer did not preserve BigQuery PK DDL:\n%s", joined)
	}
}

func TestComputeDiff_BigQueryDDL(t *testing.T) {
	current := &sdata.DBInfo{Type: "bigquery"}
	expected := sdata.NewDBInfo("bigquery", 0, "", "project", []sdata.DBColumn{
		{Schema: "", Table: "users", Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
		{Schema: "", Table: "users", Name: "email", Type: "text", NotNull: true},
		{Schema: "", Table: "orders", Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
		{Schema: "", Table: "orders", Name: "user_id", Type: "bigint", FKeyTable: "users", FKeyCol: "id"},
		{Schema: "", Table: "orders", Name: "tags", Type: "text", Array: true},
	}, nil, nil)

	ops := computeDiff(current, expected, DiffOptions{})
	var usersSQL, ordersSQL string
	for _, op := range ops {
		if op.Type == "create_table" && op.Table == "users" {
			usersSQL = op.SQL
		}
		if op.Type == "create_table" && op.Table == "orders" {
			ordersSQL = op.SQL
		}
	}
	if !strings.Contains(usersSQL, "`id` INT64 NOT NULL PRIMARY KEY NOT ENFORCED") {
		t.Fatalf("bigquery users DDL missing non-enforced primary key:\n%s", usersSQL)
	}
	if !strings.Contains(ordersSQL, "REFERENCES `users`(`id`) NOT ENFORCED") {
		t.Fatalf("bigquery orders DDL missing non-enforced foreign key:\n%s", ordersSQL)
	}
	if !strings.Contains(ordersSQL, "`tags` ARRAY<STRING>") {
		t.Fatalf("bigquery orders DDL missing array type:\n%s", ordersSQL)
	}
}

func TestComputeDiff_BigQueryDDLWithDataset(t *testing.T) {
	current := &sdata.DBInfo{Type: "bigquery"}
	expected := sdata.NewDBInfo("bigquery", 0, "analytics", "project", []sdata.DBColumn{
		{Schema: "analytics", Table: "users", Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
		{Schema: "analytics", Table: "orders", Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
		{Schema: "analytics", Table: "orders", Name: "user_id", Type: "bigint", FKeySchema: "analytics", FKeyTable: "users", FKeyCol: "id"},
	}, nil, nil)

	ops := computeDiff(current, expected, DiffOptions{})
	var usersSQL, ordersSQL string
	for _, op := range ops {
		if op.Type == "create_table" && op.Table == "users" {
			usersSQL = op.SQL
		}
		if op.Type == "create_table" && op.Table == "orders" {
			ordersSQL = op.SQL
		}
	}
	if !strings.Contains(usersSQL, "CREATE TABLE `analytics.users`") {
		t.Fatalf("bigquery users DDL missing dataset-qualified table:\n%s", usersSQL)
	}
	if !strings.Contains(ordersSQL, "REFERENCES `analytics.users`(`id`) NOT ENFORCED") {
		t.Fatalf("bigquery orders DDL missing dataset-qualified FK:\n%s", ordersSQL)
	}
}

func TestComputeDiff_RedshiftDDL(t *testing.T) {
	current := &sdata.DBInfo{Type: "redshift"}
	expected := sdata.NewDBInfo("redshift", 0, "public", "dev", []sdata.DBColumn{
		{Schema: "public", Table: "events", Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
		{Schema: "public", Table: "events", Name: "payload", Type: "jsonb"},
		{Schema: "public", Table: "events", Name: "email", Type: "varchar(255)", UniqueKey: true},
		{Schema: "public", Table: "events", Name: "body", Type: "text", FullText: true},
		{Schema: "public", Table: "events", Name: "created_at", Type: "timestamp"},
	}, nil, nil)
	expected.Tables[0].ClusteringKeys = []string{"created_at"}

	ops := computeDiff(current, expected, DiffOptions{})
	var createSQL string
	for _, op := range ops {
		if op.Type == "create_table" {
			createSQL = op.SQL
		}
		if op.Type == "add_index" {
			t.Fatalf("redshift v1 should not emit index operations: %+v", op)
		}
	}
	if !strings.Contains(createSQL, `"id" BIGINT IDENTITY(1,1) PRIMARY KEY`) {
		t.Fatalf("redshift DDL missing identity primary key:\n%s", createSQL)
	}
	if !strings.Contains(createSQL, `"payload" SUPER`) {
		t.Fatalf("redshift DDL missing SUPER mapping:\n%s", createSQL)
	}
	if !strings.Contains(createSQL, `COMPOUND SORTKEY("created_at")`) {
		t.Fatalf("redshift DDL missing sort key:\n%s", createSQL)
	}
}

func TestComputeDiff_CassandraDDL(t *testing.T) {
	current := &sdata.DBInfo{Type: "cassandra"}
	expected := sdata.NewDBInfo("cassandra", 0, "app", "app", []sdata.DBColumn{
		{Schema: "app", Table: "posts", Name: "user_id", Type: "text", PrimaryKey: true, NotNull: true},
		{Schema: "app", Table: "posts", Name: "id", Type: "text", PrimaryKey: true, NotNull: true},
		{Schema: "app", Table: "posts", Name: "title", Type: "text", NotNull: true},
	}, nil, nil)

	ops := computeDiff(current, expected, DiffOptions{})
	var createSQL string
	for _, op := range ops {
		if op.Type == "create_table" {
			createSQL = op.SQL
		}
	}
	if !strings.Contains(createSQL, `CREATE TABLE "app"."posts"`) {
		t.Fatalf("cassandra DDL missing keyspace-qualified table:\n%s", createSQL)
	}
	if !strings.Contains(createSQL, `PRIMARY KEY (("user_id"), "id")`) {
		t.Fatalf("cassandra DDL missing composite primary key:\n%s", createSQL)
	}

	current = expected
	extended := sdata.NewDBInfo("cassandra", 0, "app", "app", []sdata.DBColumn{
		{Schema: "app", Table: "posts", Name: "user_id", Type: "text", PrimaryKey: true, NotNull: true},
		{Schema: "app", Table: "posts", Name: "id", Type: "text", PrimaryKey: true, NotNull: true},
		{Schema: "app", Table: "posts", Name: "title", Type: "text", NotNull: true},
		{Schema: "app", Table: "posts", Name: "tags", Type: "text", Array: true},
	}, nil, nil)
	ops = computeDiff(current, extended, DiffOptions{})
	var addSQL string
	for _, op := range ops {
		if op.Type == "add_column" && op.Column == "tags" {
			addSQL = op.SQL
		}
	}
	if !strings.Contains(addSQL, `ALTER TABLE "app"."posts" ADD "tags" list<text>;`) {
		t.Fatalf("cassandra DDL missing add-column list type:\n%s", addSQL)
	}

	ops = computeDiff(extended, current, DiffOptions{Destructive: true})
	var dropSQL string
	for _, op := range ops {
		if op.Type == "drop_column" && op.Column == "tags" {
			dropSQL = op.SQL
		}
	}
	if !strings.Contains(dropSQL, `ALTER TABLE "app"."posts" DROP "tags";`) {
		t.Fatalf("cassandra DDL missing drop-column:\n%s", dropSQL)
	}
}

func TestComputeDiff_AddColumn(t *testing.T) {
	current := &sdata.DBInfo{
		Type: "postgres",
		Tables: []sdata.DBTable{
			{
				Name: "users",
				Columns: []sdata.DBColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true},
				},
			},
		},
	}

	expected := &sdata.DBInfo{
		Type: "postgres",
		Tables: []sdata.DBTable{
			{
				Name: "users",
				Columns: []sdata.DBColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "email", Type: "text", NotNull: true},
				},
			},
		},
	}

	ops := computeDiff(current, expected, DiffOptions{Destructive: false})

	if len(ops) == 0 {
		t.Fatal("expected at least one operation")
	}

	found := false
	for _, op := range ops {
		if op.Type == "add_column" && op.Column == "email" {
			found = true
			if !strings.Contains(op.SQL, "ADD COLUMN") {
				t.Errorf("expected ADD COLUMN in SQL, got: %s", op.SQL)
			}
		}
	}

	if !found {
		t.Error("expected add_column operation for email column")
	}
}

func TestComputeDiff_DropColumn_NotDestructive(t *testing.T) {
	current := &sdata.DBInfo{
		Type: "postgres",
		Tables: []sdata.DBTable{
			{
				Name: "users",
				Columns: []sdata.DBColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "old_column", Type: "text"},
				},
			},
		},
	}

	expected := &sdata.DBInfo{
		Type: "postgres",
		Tables: []sdata.DBTable{
			{
				Name: "users",
				Columns: []sdata.DBColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true},
				},
			},
		},
	}

	// Without destructive flag
	ops := computeDiff(current, expected, DiffOptions{Destructive: false})

	for _, op := range ops {
		if op.Type == "drop_column" {
			t.Error("should not have drop_column operation when destructive is false")
		}
	}
}

func TestComputeDiff_DropColumn_Destructive(t *testing.T) {
	current := &sdata.DBInfo{
		Type: "postgres",
		Tables: []sdata.DBTable{
			{
				Name: "users",
				Columns: []sdata.DBColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "old_column", Type: "text"},
				},
			},
		},
	}

	expected := &sdata.DBInfo{
		Type: "postgres",
		Tables: []sdata.DBTable{
			{
				Name: "users",
				Columns: []sdata.DBColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true},
				},
			},
		},
	}

	// With destructive flag
	ops := computeDiff(current, expected, DiffOptions{Destructive: true})

	found := false
	for _, op := range ops {
		if op.Type == "drop_column" && op.Column == "old_column" {
			found = true
			if !op.Danger {
				t.Error("drop_column operation should be marked as dangerous")
			}
		}
	}

	if !found {
		t.Error("expected drop_column operation when destructive is true")
	}
}

func TestComputeDiff_DropTable_Destructive(t *testing.T) {
	current := &sdata.DBInfo{
		Type: "postgres",
		Tables: []sdata.DBTable{
			{
				Name: "old_table",
				Columns: []sdata.DBColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true},
				},
			},
		},
	}

	expected := &sdata.DBInfo{
		Type:   "postgres",
		Tables: []sdata.DBTable{},
	}

	// With destructive flag
	ops := computeDiff(current, expected, DiffOptions{Destructive: true})

	found := false
	for _, op := range ops {
		if op.Type == "drop_table" && op.Table == "old_table" {
			found = true
			if !op.Danger {
				t.Error("drop_table operation should be marked as dangerous")
			}
		}
	}

	if !found {
		t.Error("expected drop_table operation when destructive is true")
	}
}

func TestPostgresDialect_MapType(t *testing.T) {
	d := &postgresDialect{}

	tests := []struct {
		input      string
		notNull    bool
		primaryKey bool
		expected   string
	}{
		{"bigint", false, true, "BIGSERIAL PRIMARY KEY"},
		{"integer", true, false, "INTEGER NOT NULL"},
		{"text", false, false, "TEXT"},
		{"boolean", true, false, "BOOLEAN NOT NULL"},
		{"timestamp with time zone", false, false, "TIMESTAMPTZ"},
		{"jsonb", false, false, "JSONB"},
		{"uuid", false, true, "UUID PRIMARY KEY"},
	}

	for _, tc := range tests {
		result := d.MapType(tc.input, tc.notNull, tc.primaryKey)
		if result != tc.expected {
			t.Errorf("MapType(%q, %v, %v) = %q, want %q",
				tc.input, tc.notNull, tc.primaryKey, result, tc.expected)
		}
	}
}

func TestMySQLDialect_MapType(t *testing.T) {
	d := &mysqlDialect{}

	tests := []struct {
		input      string
		notNull    bool
		primaryKey bool
		expected   string
	}{
		{"bigint", false, true, "BIGINT AUTO_INCREMENT PRIMARY KEY"},
		{"integer", true, false, "INT NOT NULL"},
		{"text", false, false, "TEXT"},
		{"boolean", true, false, "TINYINT(1) NOT NULL"},
		{"json", false, false, "JSON"},
		{"uuid", false, false, "CHAR(36)"},
	}

	for _, tc := range tests {
		result := d.MapType(tc.input, tc.notNull, tc.primaryKey)
		if result != tc.expected {
			t.Errorf("MapType(%q, %v, %v) = %q, want %q",
				tc.input, tc.notNull, tc.primaryKey, result, tc.expected)
		}
	}
}

func TestSQLiteDialect_MapType(t *testing.T) {
	d := &sqliteDialect{}

	tests := []struct {
		input      string
		notNull    bool
		primaryKey bool
		expected   string
	}{
		{"bigint", false, true, "INTEGER PRIMARY KEY AUTOINCREMENT"},
		{"integer", true, false, "INTEGER NOT NULL"},
		{"text", false, false, "TEXT"},
		{"boolean", true, false, "INTEGER NOT NULL"},
		{"json", false, false, "TEXT"},
		{"timestamp", false, false, "TEXT"},
	}

	for _, tc := range tests {
		result := d.MapType(tc.input, tc.notNull, tc.primaryKey)
		if result != tc.expected {
			t.Errorf("MapType(%q, %v, %v) = %q, want %q",
				tc.input, tc.notNull, tc.primaryKey, result, tc.expected)
		}
	}
}

func TestPostgresDialect_CreateTable(t *testing.T) {
	d := &postgresDialect{}

	table := sdata.DBTable{
		Name: "posts",
		Columns: []sdata.DBColumn{
			{Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
			{Name: "title", Type: "text", NotNull: true},
			{Name: "user_id", Type: "bigint", NotNull: true, FKeyTable: "users", FKeyCol: "id"},
		},
	}

	sql := d.CreateTable(table)

	if !strings.Contains(sql, "CREATE TABLE") {
		t.Error("expected CREATE TABLE")
	}
	if !strings.Contains(sql, `"posts"`) {
		t.Error("expected quoted table name")
	}
	if !strings.Contains(sql, `"id"`) {
		t.Error("expected id column")
	}
	if !strings.Contains(sql, "BIGSERIAL PRIMARY KEY") {
		t.Error("expected BIGSERIAL PRIMARY KEY for id")
	}
	if !strings.Contains(sql, "FOREIGN KEY") {
		t.Error("expected foreign key constraint")
	}
	if !strings.Contains(sql, `REFERENCES "users"("id")`) {
		t.Error("expected foreign key reference to users(id)")
	}
}

func TestMySQLDialect_CreateTable(t *testing.T) {
	d := &mysqlDialect{}

	table := sdata.DBTable{
		Name: "posts",
		Columns: []sdata.DBColumn{
			{Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
			{Name: "title", Type: "text", NotNull: true},
		},
	}

	sql := d.CreateTable(table)

	if !strings.Contains(sql, "CREATE TABLE") {
		t.Error("expected CREATE TABLE")
	}
	if !strings.Contains(sql, "`posts`") {
		t.Error("expected backtick-quoted table name")
	}
	if !strings.Contains(sql, "ENGINE=InnoDB") {
		t.Error("expected InnoDB engine")
	}
	if !strings.Contains(sql, "AUTO_INCREMENT") {
		t.Error("expected AUTO_INCREMENT for primary key")
	}
}

func TestQuoteIdentifier(t *testing.T) {
	tests := []struct {
		dialect  DDLDialect
		input    string
		expected string
	}{
		{&postgresDialect{}, "users", `"users"`},
		{&mysqlDialect{}, "users", "`users`"},
		{&sqliteDialect{}, "users", `"users"`},
		{&mssqlDialect{}, "users", "[users]"},
		{&oracleDialect{}, "users", `"USERS"`},
		{&snowflakeDDLDialect{}, "users", `"users"`},
	}

	for _, tc := range tests {
		result := tc.dialect.QuoteIdentifier(tc.input)
		if result != tc.expected {
			t.Errorf("%s.QuoteIdentifier(%q) = %q, want %q",
				tc.dialect.Name(), tc.input, result, tc.expected)
		}
	}
}

func TestGenerateDiffSQL(t *testing.T) {
	ops := []SchemaOperation{
		{Type: "create_table", Table: "users", SQL: "CREATE TABLE users ();"},
		{Type: "add_column", Table: "users", Column: "email", SQL: "ALTER TABLE users ADD COLUMN email TEXT;"},
	}

	sqls := GenerateDiffSQL(ops)

	if len(sqls) != 2 {
		t.Errorf("expected 2 SQL statements, got %d", len(sqls))
	}

	if sqls[0] != "CREATE TABLE users ();" {
		t.Errorf("unexpected SQL: %s", sqls[0])
	}
}

func TestComputeDiff_NoChanges(t *testing.T) {
	schema := &sdata.DBInfo{
		Type: "postgres",
		Tables: []sdata.DBTable{
			{
				Name: "users",
				Columns: []sdata.DBColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "name", Type: "text"},
				},
			},
		},
	}

	ops := computeDiff(schema, schema, DiffOptions{Destructive: false})

	if len(ops) != 0 {
		t.Errorf("expected no operations when schemas are identical, got %d", len(ops))
	}
}

func TestComputeDiff_DifferentDBTypes(t *testing.T) {
	// With the new behavior, we use the dbType parameter (stored in expected.Type)
	// to select the dialect. The current.Type is only used as fallback.
	// This test verifies that diffs work correctly when expected has a valid type.
	current := &sdata.DBInfo{
		Type:   "postgres",
		Tables: []sdata.DBTable{},
	}

	expected := &sdata.DBInfo{
		Type: "mysql",
		Tables: []sdata.DBTable{
			{Name: "users", Columns: []sdata.DBColumn{{Name: "id", Type: "bigint"}}},
		},
	}

	ops := computeDiff(current, expected, DiffOptions{Destructive: false})

	// Should create the table using the mysql dialect (expected.Type)
	if len(ops) == 0 {
		t.Error("expected operations to create the users table")
	}
	if ops[0].Type != "create_table" {
		t.Errorf("expected create_table operation, got %s", ops[0].Type)
	}
	// Verify mysql dialect was used (uses backticks for quoting)
	if !strings.Contains(ops[0].SQL, "`users`") {
		t.Errorf("expected mysql quoting style with backticks, got: %s", ops[0].SQL)
	}
}

func TestAddForeignKey(t *testing.T) {
	d := &postgresDialect{}

	col := sdata.DBColumn{
		Name:      "user_id",
		Type:      "bigint",
		FKeyTable: "users",
		FKeyCol:   "id",
	}

	sql := d.AddForeignKey("posts", col)

	if !strings.Contains(sql, "ALTER TABLE") {
		t.Error("expected ALTER TABLE")
	}
	if !strings.Contains(sql, "ADD CONSTRAINT") {
		t.Error("expected ADD CONSTRAINT")
	}
	if !strings.Contains(sql, "FOREIGN KEY") {
		t.Error("expected FOREIGN KEY")
	}
	if !strings.Contains(sql, `REFERENCES "users"("id")`) {
		t.Error("expected correct REFERENCES clause")
	}
}

func TestCreateUniqueIndex(t *testing.T) {
	d := &postgresDialect{}

	col := sdata.DBColumn{
		Name:      "email",
		Type:      "text",
		UniqueKey: true,
	}

	sql := d.CreateUniqueIndex("users", col)

	if !strings.Contains(sql, "CREATE UNIQUE INDEX") {
		t.Error("expected CREATE UNIQUE INDEX")
	}
	if !strings.Contains(sql, `ON "users"`) {
		t.Error("expected ON users clause")
	}
	if !strings.Contains(sql, `("email")`) {
		t.Error("expected email column in index")
	}
}

func TestCreateSearchIndex_Postgres(t *testing.T) {
	d := &postgresDialect{}

	col := sdata.DBColumn{
		Name:     "content",
		Type:     "text",
		FullText: true,
	}

	sql := d.CreateSearchIndex("posts", col)

	if !strings.Contains(sql, "CREATE INDEX") {
		t.Error("expected CREATE INDEX")
	}
	if !strings.Contains(sql, "USING gin") {
		t.Error("expected GIN index")
	}
	if !strings.Contains(sql, "to_tsvector") {
		t.Error("expected to_tsvector function")
	}
}

func TestCreateSearchIndex_MySQL(t *testing.T) {
	d := &mysqlDialect{}

	col := sdata.DBColumn{
		Name:     "content",
		Type:     "text",
		FullText: true,
	}

	sql := d.CreateSearchIndex("posts", col)

	if !strings.Contains(sql, "CREATE FULLTEXT INDEX") {
		t.Error("expected CREATE FULLTEXT INDEX")
	}
}

// ============================================================================
// MSSQL Dialect Tests
// ============================================================================

func TestMSSQLDialect_MapType(t *testing.T) {
	d := &mssqlDialect{}

	tests := []struct {
		input      string
		notNull    bool
		primaryKey bool
		expected   string
	}{
		{"bigint", false, true, "BIGINT IDENTITY(1,1) PRIMARY KEY"},
		{"integer", true, false, "INT NOT NULL"},
		{"text", false, false, "NVARCHAR(MAX)"},
		{"boolean", true, false, "BIT NOT NULL"},
		{"json", false, false, "NVARCHAR(MAX)"},
		{"uuid", false, false, "UNIQUEIDENTIFIER"},
		{"uuid", false, true, "UNIQUEIDENTIFIER PRIMARY KEY"},
		{"money", false, false, "MONEY"},
		{"xml", false, false, "XML"},
		{"serial", false, false, "INT IDENTITY(1,1)"},
		{"bigserial", false, false, "BIGINT IDENTITY(1,1)"},
		{"interval", false, false, "VARCHAR(255)"},
		{"geometry", false, false, "GEOMETRY"},
		{"geography", false, false, "GEOGRAPHY"},
		{"varchar", false, false, "NVARCHAR(255)"},
		{"bytea", false, false, "VARBINARY(MAX)"},
		{"timestamp", false, false, "DATETIMEOFFSET"},
		{"date", false, false, "DATE"},
	}

	for _, tc := range tests {
		result := d.MapType(tc.input, tc.notNull, tc.primaryKey)
		if result != tc.expected {
			t.Errorf("MSSQL MapType(%q, %v, %v) = %q, want %q",
				tc.input, tc.notNull, tc.primaryKey, result, tc.expected)
		}
	}
}

func TestMSSQLDialect_CreateTable(t *testing.T) {
	d := &mssqlDialect{}

	table := sdata.DBTable{
		Name: "posts",
		Columns: []sdata.DBColumn{
			{Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
			{Name: "title", Type: "text", NotNull: true},
			{Name: "user_id", Type: "bigint", NotNull: true, FKeyTable: "users", FKeyCol: "id"},
		},
	}

	sql := d.CreateTable(table)

	if !strings.Contains(sql, "CREATE TABLE") {
		t.Error("expected CREATE TABLE")
	}
	if !strings.Contains(sql, "[posts]") {
		t.Error("expected bracket-quoted table name")
	}
	if !strings.Contains(sql, "[id]") {
		t.Error("expected bracket-quoted id column")
	}
	if !strings.Contains(sql, "IDENTITY(1,1)") {
		t.Error("expected IDENTITY(1,1) for id")
	}
	if !strings.Contains(sql, "FOREIGN KEY") {
		t.Error("expected foreign key constraint")
	}
}

// ============================================================================
// Oracle Dialect Tests
// ============================================================================

func TestOracleDialect_MapType(t *testing.T) {
	d := &oracleDialect{}

	tests := []struct {
		input      string
		notNull    bool
		primaryKey bool
		expected   string
	}{
		{"bigint", false, true, "NUMBER(19) GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY"},
		{"integer", true, false, "NUMBER(10) NOT NULL"},
		{"text", false, false, "CLOB"},
		{"boolean", true, false, "NUMBER(1) NOT NULL"},
		{"json", false, false, "CLOB"},
		{"uuid", false, false, "RAW(16)"},
		{"money", false, false, "NUMBER(19,4)"},
		{"xml", false, false, "XMLTYPE"},
		{"serial", false, false, "NUMBER(10) GENERATED BY DEFAULT AS IDENTITY"},
		{"bigserial", false, false, "NUMBER(19) GENERATED BY DEFAULT AS IDENTITY"},
		{"interval", false, false, "INTERVAL DAY TO SECOND"},
		{"varchar", false, false, "VARCHAR2(255)"},
		{"bytea", false, false, "BLOB"},
		{"timestamp", false, false, "TIMESTAMP WITH TIME ZONE"},
		{"date", false, false, "DATE"},
		{"float", false, false, "BINARY_FLOAT"},
		{"double", false, false, "BINARY_DOUBLE"},
	}

	for _, tc := range tests {
		result := d.MapType(tc.input, tc.notNull, tc.primaryKey)
		if result != tc.expected {
			t.Errorf("Oracle MapType(%q, %v, %v) = %q, want %q",
				tc.input, tc.notNull, tc.primaryKey, result, tc.expected)
		}
	}
}

func TestOracleDialect_CreateTable(t *testing.T) {
	d := &oracleDialect{}

	table := sdata.DBTable{
		Name: "posts",
		Columns: []sdata.DBColumn{
			{Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
			{Name: "title", Type: "text", NotNull: true},
		},
	}

	sql := d.CreateTable(table)

	if !strings.Contains(sql, "CREATE TABLE") {
		t.Error("expected CREATE TABLE")
	}
	if !strings.Contains(sql, `"POSTS"`) {
		t.Error("expected uppercase quoted table name")
	}
	if !strings.Contains(sql, `"ID"`) {
		t.Error("expected uppercase quoted id column")
	}
	if !strings.Contains(sql, "GENERATED BY DEFAULT AS IDENTITY") {
		t.Error("expected GENERATED BY DEFAULT AS IDENTITY for id")
	}
}

func TestSnowflakeDialect_MapType(t *testing.T) {
	d := &snowflakeDDLDialect{}

	tests := []struct {
		input      string
		notNull    bool
		primaryKey bool
		expected   string
	}{
		{"bigint", false, true, "BIGINT NOT NULL PRIMARY KEY"},
		{"integer", true, false, "INTEGER NOT NULL"},
		{"text", false, false, "VARCHAR"},
		{"boolean", true, false, "BOOLEAN NOT NULL"},
		{"json", false, false, "JSON"},
		{"uuid", false, false, "VARCHAR(36)"},
	}

	for _, tc := range tests {
		result := d.MapType(tc.input, tc.notNull, tc.primaryKey)
		if result != tc.expected {
			t.Errorf("Snowflake MapType(%q, %v, %v) = %q, want %q",
				tc.input, tc.notNull, tc.primaryKey, result, tc.expected)
		}
	}
}

func TestSnowflakeDialect_CreateTable(t *testing.T) {
	d := &snowflakeDDLDialect{}

	table := sdata.DBTable{
		Name: "posts",
		Columns: []sdata.DBColumn{
			{Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
			{Name: "title", Type: "text", NotNull: true},
		},
	}

	sql := d.CreateTable(table)

	if !strings.Contains(sql, "CREATE TABLE") {
		t.Error("expected CREATE TABLE")
	}
	if !strings.Contains(sql, `"posts"`) {
		t.Error("expected double-quoted table name")
	}
	if !strings.Contains(sql, `"id"`) {
		t.Error("expected double-quoted id column")
	}
	if !strings.Contains(sql, "PRIMARY KEY") {
		t.Error("expected PRIMARY KEY on id column")
	}
}

func TestRedshiftDialect_CreateTable(t *testing.T) {
	d := &redshiftDDLDialect{}

	table := sdata.DBTable{
		Name: "events",
		Columns: []sdata.DBColumn{
			{Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
			{Name: "user_id", Type: "bigint", FKeyTable: "users", FKeyCol: "id"},
			{Name: "metadata", Type: "json"},
			{Name: "shape", Type: "geometry"},
			{Name: "created_at", Type: "timestamptz", NotNull: true},
		},
		ClusteringKeys: []string{"created_at", "id"},
	}

	sql := d.CreateTable(table)

	for _, want := range []string{
		`CREATE TABLE "events"`,
		`"id" BIGINT IDENTITY(1,1) PRIMARY KEY`,
		`"metadata" SUPER`,
		`"shape" GEOMETRY`,
		`CONSTRAINT "fk_events_user_id" FOREIGN KEY ("user_id") REFERENCES "users"("id")`,
		`DISTSTYLE AUTO ENCODE AUTO`,
		`COMPOUND SORTKEY("created_at", "id")`,
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("redshift DDL missing %q:\n%s", want, sql)
		}
	}
}

func TestRedshiftDialect_AddDropAndConstraints(t *testing.T) {
	d := &redshiftDDLDialect{}

	addCol := d.AddColumn("users", sdata.DBColumn{Name: "phone", Type: "varchar(32)"})
	if addCol != `ALTER TABLE "users" ADD COLUMN "phone" VARCHAR(32);` {
		t.Fatalf("unexpected add column SQL: %s", addCol)
	}

	dropCol := d.DropColumn("users", "phone")
	if dropCol != `ALTER TABLE "users" DROP COLUMN "phone";` {
		t.Fatalf("unexpected drop column SQL: %s", dropCol)
	}

	fk := d.AddForeignKey("events", sdata.DBColumn{Name: "user_id", FKeyTable: "users", FKeyCol: "id"})
	if fk != `ALTER TABLE "events" ADD CONSTRAINT "fk_events_user_id" FOREIGN KEY ("user_id") REFERENCES "users"("id");` {
		t.Fatalf("unexpected fk SQL: %s", fk)
	}

	unique := d.CreateUniqueIndex("users", sdata.DBColumn{Name: "email"})
	if unique != "" {
		t.Fatalf("unexpected unique SQL: %s", unique)
	}
}

func TestRedshiftDialect_NoGeneratedIndexes(t *testing.T) {
	d := &redshiftDDLDialect{}
	col := sdata.DBColumn{Name: "email", Type: "text", FullText: true, Index: true}

	if sql := d.CreateIndex("users", col); sql != "" {
		t.Fatalf("expected no Redshift user-managed index SQL, got: %s", sql)
	}
	if sql := d.CreateSearchIndex("users", col); sql != "" {
		t.Fatalf("expected no Redshift search-index SQL, got: %s", sql)
	}
}

func TestBigQueryDialect_GeneratedDDL(t *testing.T) {
	d := &bigqueryDDLDialect{}

	table := sdata.DBTable{
		Name: "orders",
		Columns: []sdata.DBColumn{
			{Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
			{Name: "user_id", Type: "bigint", FKeyTable: "users", FKeyCol: "id"},
			{Name: "tags", Type: "text", Array: true},
		},
	}

	sql := d.CreateTable(table)
	for _, want := range []string{
		"CREATE TABLE `orders`",
		"`id` INT64 NOT NULL PRIMARY KEY NOT ENFORCED",
		"REFERENCES `users`(`id`) NOT ENFORCED",
		"`tags` ARRAY<STRING>",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("BigQuery DDL missing %q:\n%s", want, sql)
		}
	}

	if got, want := d.AddColumn("orders", sdata.DBColumn{Name: "notes", Type: "text"}), "ALTER TABLE `orders` ADD COLUMN `notes` STRING;"; got != want {
		t.Fatalf("unexpected BigQuery ADD COLUMN:\ngot:  %s\nwant: %s", got, want)
	}
	if got, want := d.DropColumn("orders", "notes"), "ALTER TABLE `orders` DROP COLUMN `notes`;"; got != want {
		t.Fatalf("unexpected BigQuery DROP COLUMN:\ngot:  %s\nwant: %s", got, want)
	}
	if sql := d.CreateIndex("orders", sdata.DBColumn{Name: "user_id", Index: true}); sql != "" {
		t.Fatalf("expected no BigQuery index SQL, got: %s", sql)
	}
	if sql := d.CreateSearchIndex("orders", sdata.DBColumn{Name: "body", FullText: true}); sql != "" {
		t.Fatalf("expected no BigQuery search-index SQL, got: %s", sql)
	}
	if sql := d.CreateUniqueIndex("orders", sdata.DBColumn{Name: "email", UniqueKey: true}); sql != "" {
		t.Fatalf("expected no BigQuery unique-index SQL, got: %s", sql)
	}
}

func TestCassandraDialect_GeneratedDDL(t *testing.T) {
	d := &cassandraDDLDialect{}

	table := sdata.DBTable{
		Schema: "app",
		Name:   "posts",
		Columns: []sdata.DBColumn{
			{Name: "user_id", Type: "uuid", PrimaryKey: true, NotNull: true},
			{Name: "id", Type: "uuid", PrimaryKey: true, NotNull: true},
			{Name: "tags", Type: "text", Array: true},
		},
	}

	sql := d.CreateTable(table)
	for _, want := range []string{
		`CREATE TABLE "app"."posts"`,
		`"user_id" uuid`,
		`"tags" list<text>`,
		`PRIMARY KEY (("user_id"), "id")`,
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("Cassandra DDL missing %q:\n%s", want, sql)
		}
	}

	if got, want := d.AddColumn("posts", sdata.DBColumn{Name: "title", Type: "text"}), `ALTER TABLE "posts" ADD "title" text;`; got != want {
		t.Fatalf("unexpected Cassandra ADD COLUMN:\ngot:  %s\nwant: %s", got, want)
	}
	if got, want := d.DropColumn("posts", "title"), `ALTER TABLE "posts" DROP "title";`; got != want {
		t.Fatalf("unexpected Cassandra DROP COLUMN:\ngot:  %s\nwant: %s", got, want)
	}
	if sql := d.AddForeignKey("posts", sdata.DBColumn{Name: "user_id", FKeyTable: "users", FKeyCol: "id"}); sql != "" {
		t.Fatalf("expected no Cassandra FK SQL, got: %s", sql)
	}
	if sql := d.CreateIndex("posts", sdata.DBColumn{Name: "title", Index: true}); sql != "" {
		t.Fatalf("expected no Cassandra index SQL, got: %s", sql)
	}
}

// ============================================================================
// CreateIndex Tests for All Dialects
// ============================================================================

func TestCreateIndex_Postgres(t *testing.T) {
	d := &postgresDialect{}

	col := sdata.DBColumn{
		Name:  "email",
		Type:  "text",
		Index: true,
	}

	sql := d.CreateIndex("users", col)

	if !strings.Contains(sql, "CREATE INDEX") {
		t.Error("expected CREATE INDEX")
	}
	if !strings.Contains(sql, `"idx_users_email"`) {
		t.Error("expected index name idx_users_email")
	}
	if !strings.Contains(sql, `ON "users"`) {
		t.Error("expected ON users clause")
	}
	if !strings.Contains(sql, `("email")`) {
		t.Error("expected email column in index")
	}
}

func TestCreateIndex_MySQL(t *testing.T) {
	d := &mysqlDialect{}

	col := sdata.DBColumn{
		Name:  "email",
		Type:  "text",
		Index: true,
	}

	sql := d.CreateIndex("users", col)

	if !strings.Contains(sql, "CREATE INDEX") {
		t.Error("expected CREATE INDEX")
	}
	if !strings.Contains(sql, "`idx_users_email`") {
		t.Error("expected backtick-quoted index name")
	}
	if !strings.Contains(sql, "ON `users`") {
		t.Error("expected ON users clause with backticks")
	}
}

func TestCreateIndex_SQLite(t *testing.T) {
	d := &sqliteDialect{}

	col := sdata.DBColumn{
		Name:  "email",
		Type:  "text",
		Index: true,
	}

	sql := d.CreateIndex("users", col)

	if !strings.Contains(sql, "CREATE INDEX") {
		t.Error("expected CREATE INDEX")
	}
	if !strings.Contains(sql, `"idx_users_email"`) {
		t.Error("expected double-quoted index name")
	}
	if !strings.Contains(sql, `ON "users"`) {
		t.Error("expected ON users clause with double quotes")
	}
}

func TestCreateIndex_MSSQL(t *testing.T) {
	d := &mssqlDialect{}

	col := sdata.DBColumn{
		Name:  "email",
		Type:  "text",
		Index: true,
	}

	sql := d.CreateIndex("users", col)

	if !strings.Contains(sql, "CREATE INDEX") {
		t.Error("expected CREATE INDEX")
	}
	if !strings.Contains(sql, "[IX_users_email]") {
		t.Error("expected bracket-quoted index name with IX_ prefix")
	}
	if !strings.Contains(sql, "ON [users]") {
		t.Error("expected ON users clause with brackets")
	}
}

func TestCreateIndex_Oracle(t *testing.T) {
	d := &oracleDialect{}

	col := sdata.DBColumn{
		Name:  "email",
		Type:  "text",
		Index: true,
	}

	sql := d.CreateIndex("users", col)

	if !strings.Contains(sql, "CREATE INDEX") {
		t.Error("expected CREATE INDEX")
	}
	if !strings.Contains(sql, `"IX_USERS_EMAIL"`) {
		t.Error("expected uppercase quoted index name")
	}
	if !strings.Contains(sql, `ON "USERS"`) {
		t.Error("expected ON USERS clause with uppercase")
	}
}

func TestCreateIndex_CustomName(t *testing.T) {
	d := &postgresDialect{}

	col := sdata.DBColumn{
		Name:      "email",
		Type:      "text",
		Index:     true,
		IndexName: "my_custom_index",
	}

	sql := d.CreateIndex("users", col)

	if !strings.Contains(sql, `"my_custom_index"`) {
		t.Error("expected custom index name in SQL")
	}
	if strings.Contains(sql, "idx_users_email") {
		t.Error("should not use default index name when custom name provided")
	}
}

func TestCreateIndex_CustomName_MySQL(t *testing.T) {
	d := &mysqlDialect{}

	col := sdata.DBColumn{
		Name:      "status",
		Type:      "text",
		Index:     true,
		IndexName: "idx_custom_status",
	}

	sql := d.CreateIndex("orders", col)

	if !strings.Contains(sql, "`idx_custom_status`") {
		t.Error("expected custom index name in SQL")
	}
}

// ============================================================================
// DEFAULT Value Tests
// ============================================================================

func TestPostgresDialect_MapDefault(t *testing.T) {
	d := &postgresDialect{}

	tests := []struct {
		input    string
		expected string
	}{
		{"'active'", "'active'"},
		{"now()", "now()"},
		{"0", "0"},
		{"true", "true"},
		{"NULL", "NULL"},
		{"gen_random_uuid()", "gen_random_uuid()"},
	}

	for _, tc := range tests {
		result := d.MapDefault(tc.input)
		if result != tc.expected {
			t.Errorf("Postgres MapDefault(%q) = %q, want %q",
				tc.input, result, tc.expected)
		}
	}
}

func TestMySQLDialect_MapDefault(t *testing.T) {
	d := &mysqlDialect{}

	tests := []struct {
		input    string
		expected string
	}{
		{"'pending'", "'pending'"},
		{"CURRENT_TIMESTAMP", "CURRENT_TIMESTAMP"},
		{"0", "0"},
		{"1", "1"},
	}

	for _, tc := range tests {
		result := d.MapDefault(tc.input)
		if result != tc.expected {
			t.Errorf("MySQL MapDefault(%q) = %q, want %q",
				tc.input, result, tc.expected)
		}
	}
}

func TestCreateTable_WithDefault_Postgres(t *testing.T) {
	d := &postgresDialect{}

	table := sdata.DBTable{
		Name: "orders",
		Columns: []sdata.DBColumn{
			{Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
			{Name: "status", Type: "text", NotNull: true, Default: "'pending'"},
			{Name: "created_at", Type: "timestamp", Default: "now()"},
			{Name: "quantity", Type: "integer", Default: "1"},
		},
	}

	sql := d.CreateTable(table)

	if !strings.Contains(sql, "DEFAULT 'pending'") {
		t.Error("expected DEFAULT 'pending' for status column")
	}
	if !strings.Contains(sql, "DEFAULT now()") {
		t.Error("expected DEFAULT now() for created_at column")
	}
	if !strings.Contains(sql, "DEFAULT 1") {
		t.Error("expected DEFAULT 1 for quantity column")
	}
}

func TestCreateTable_WithDefault_MySQL(t *testing.T) {
	d := &mysqlDialect{}

	table := sdata.DBTable{
		Name: "orders",
		Columns: []sdata.DBColumn{
			{Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
			{Name: "status", Type: "varchar", NotNull: true, Default: "'active'"},
			{Name: "count", Type: "integer", Default: "0"},
		},
	}

	sql := d.CreateTable(table)

	if !strings.Contains(sql, "DEFAULT 'active'") {
		t.Error("expected DEFAULT 'active' for status column")
	}
	if !strings.Contains(sql, "DEFAULT 0") {
		t.Error("expected DEFAULT 0 for count column")
	}
}

func TestAddColumn_WithDefault_Postgres(t *testing.T) {
	d := &postgresDialect{}

	col := sdata.DBColumn{
		Name:    "status",
		Type:    "text",
		NotNull: true,
		Default: "'new'",
	}

	sql := d.AddColumn("orders", col)

	if !strings.Contains(sql, "ADD COLUMN") {
		t.Error("expected ADD COLUMN")
	}
	if !strings.Contains(sql, "DEFAULT 'new'") {
		t.Error("expected DEFAULT 'new' in ADD COLUMN")
	}
}

// ============================================================================
// ON DELETE / ON UPDATE Foreign Key Tests
// ============================================================================

func TestAddForeignKey_WithCascade(t *testing.T) {
	d := &postgresDialect{}

	col := sdata.DBColumn{
		Name:       "user_id",
		Type:       "bigint",
		FKeyTable:  "users",
		FKeyCol:    "id",
		FKOnDelete: "CASCADE",
	}

	sql := d.AddForeignKey("posts", col)

	if !strings.Contains(sql, "FOREIGN KEY") {
		t.Error("expected FOREIGN KEY")
	}
	if !strings.Contains(sql, "ON DELETE CASCADE") {
		t.Error("expected ON DELETE CASCADE")
	}
}

func TestAddForeignKey_WithSetNull(t *testing.T) {
	d := &postgresDialect{}

	col := sdata.DBColumn{
		Name:       "category_id",
		Type:       "bigint",
		FKeyTable:  "categories",
		FKeyCol:    "id",
		FKOnDelete: "SET NULL",
		FKOnUpdate: "CASCADE",
	}

	sql := d.AddForeignKey("products", col)

	if !strings.Contains(sql, "ON DELETE SET NULL") {
		t.Error("expected ON DELETE SET NULL")
	}
	if !strings.Contains(sql, "ON UPDATE CASCADE") {
		t.Error("expected ON UPDATE CASCADE")
	}
}

func TestCreateTable_ForeignKeyWithCascade(t *testing.T) {
	d := &postgresDialect{}

	table := sdata.DBTable{
		Name: "posts",
		Columns: []sdata.DBColumn{
			{Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
			{Name: "author_id", Type: "bigint", NotNull: true, FKeyTable: "users", FKeyCol: "id", FKOnDelete: "CASCADE"},
		},
	}

	sql := d.CreateTable(table)

	if !strings.Contains(sql, "FOREIGN KEY") {
		t.Error("expected FOREIGN KEY in CREATE TABLE")
	}
	if !strings.Contains(sql, "ON DELETE CASCADE") {
		t.Error("expected ON DELETE CASCADE in CREATE TABLE")
	}
}

func TestAddForeignKey_WithCascade_MySQL(t *testing.T) {
	d := &mysqlDialect{}

	col := sdata.DBColumn{
		Name:       "order_id",
		Type:       "bigint",
		FKeyTable:  "orders",
		FKeyCol:    "id",
		FKOnDelete: "CASCADE",
		FKOnUpdate: "NO ACTION",
	}

	sql := d.AddForeignKey("order_items", col)

	if !strings.Contains(sql, "ON DELETE CASCADE") {
		t.Error("expected ON DELETE CASCADE")
	}
	if !strings.Contains(sql, "ON UPDATE NO ACTION") {
		t.Error("expected ON UPDATE NO ACTION")
	}
}

func TestAddForeignKey_WithCascade_MSSQL(t *testing.T) {
	d := &mssqlDialect{}

	col := sdata.DBColumn{
		Name:       "parent_id",
		Type:       "bigint",
		FKeyTable:  "nodes",
		FKeyCol:    "id",
		FKOnDelete: "SET DEFAULT",
		FKOnUpdate: "CASCADE",
	}

	sql := d.AddForeignKey("children", col)

	if !strings.Contains(sql, "ON DELETE SET DEFAULT") {
		t.Error("expected ON DELETE SET DEFAULT")
	}
	if !strings.Contains(sql, "ON UPDATE CASCADE") {
		t.Error("expected ON UPDATE CASCADE")
	}
}

// ============================================================================
// New Type Tests (Money, Xml, Serial, Interval)
// ============================================================================

func TestPostgresDialect_NewTypes(t *testing.T) {
	d := &postgresDialect{}

	tests := []struct {
		input    string
		expected string
	}{
		{"money", "MONEY"},
		{"xml", "XML"},
		{"serial", "SERIAL"},
		{"bigserial", "BIGSERIAL"},
		{"interval", "INTERVAL"},
		{"geometry", "GEOMETRY"},
		{"geography", "GEOGRAPHY"},
		{"inet", "INET"},
		{"cidr", "CIDR"},
		{"macaddr", "MACADDR"},
		{"point", "POINT"},
		{"line", "LINE"},
		{"polygon", "POLYGON"},
	}

	for _, tc := range tests {
		result := d.MapType(tc.input, false, false)
		if result != tc.expected {
			t.Errorf("Postgres MapType(%q) = %q, want %q",
				tc.input, result, tc.expected)
		}
	}
}

func TestMySQLDialect_NewTypes(t *testing.T) {
	d := &mysqlDialect{}

	tests := []struct {
		input    string
		expected string
	}{
		{"money", "DECIMAL(19,4)"},
		{"xml", "LONGTEXT"},
		{"serial", "INT AUTO_INCREMENT"},
		{"bigserial", "BIGINT AUTO_INCREMENT"},
		{"interval", "VARCHAR(255)"},
		{"point", "POINT"},
		{"polygon", "POLYGON"},
		{"geometry", "GEOMETRY"},
	}

	for _, tc := range tests {
		result := d.MapType(tc.input, false, false)
		if result != tc.expected {
			t.Errorf("MySQL MapType(%q) = %q, want %q",
				tc.input, result, tc.expected)
		}
	}
}

func TestSQLiteDialect_NewTypes(t *testing.T) {
	d := &sqliteDialect{}

	tests := []struct {
		input    string
		expected string
	}{
		{"money", "REAL"},
		{"xml", "TEXT"},
		{"serial", "INTEGER"},
		{"bigserial", "INTEGER"},
		{"interval", "TEXT"},
	}

	for _, tc := range tests {
		result := d.MapType(tc.input, false, false)
		if result != tc.expected {
			t.Errorf("SQLite MapType(%q) = %q, want %q",
				tc.input, result, tc.expected)
		}
	}
}

func TestMSSQLDialect_NewTypes(t *testing.T) {
	d := &mssqlDialect{}

	tests := []struct {
		input    string
		expected string
	}{
		{"money", "MONEY"},
		{"xml", "XML"},
		{"serial", "INT IDENTITY(1,1)"},
		{"bigserial", "BIGINT IDENTITY(1,1)"},
		{"interval", "VARCHAR(255)"},
		{"geometry", "GEOMETRY"},
		{"geography", "GEOGRAPHY"},
	}

	for _, tc := range tests {
		result := d.MapType(tc.input, false, false)
		if result != tc.expected {
			t.Errorf("MSSQL MapType(%q) = %q, want %q",
				tc.input, result, tc.expected)
		}
	}
}

func TestOracleDialect_NewTypes(t *testing.T) {
	d := &oracleDialect{}

	tests := []struct {
		input    string
		expected string
	}{
		{"money", "NUMBER(19,4)"},
		{"xml", "XMLTYPE"},
		{"serial", "NUMBER(10) GENERATED BY DEFAULT AS IDENTITY"},
		{"bigserial", "NUMBER(19) GENERATED BY DEFAULT AS IDENTITY"},
		{"interval", "INTERVAL DAY TO SECOND"},
	}

	for _, tc := range tests {
		result := d.MapType(tc.input, false, false)
		if result != tc.expected {
			t.Errorf("Oracle MapType(%q) = %q, want %q",
				tc.input, result, tc.expected)
		}
	}
}

// ============================================================================
// Type Alias Tests (Varchar255, Decimal10_2, Char36)
// ============================================================================

func TestParseTypeWithSize(t *testing.T) {
	tests := []struct {
		input        string
		expectedBase string
		expectedSize string
	}{
		{"varchar255", "varchar", "255"},
		{"varchar100", "varchar", "100"},
		{"char36", "char", "36"},
		{"char1", "char", "1"},
		{"decimal10_2", "decimal", "10,2"},
		{"decimal19_4", "decimal", "19,4"},
		{"numeric15_3", "numeric", "15,3"},
		// Types with parentheses from @type(args: "...") directive
		{"numeric(7,2)", "numeric", "7,2"},
		{"decimal(10,2)", "decimal", "10,2"},
		{"varchar(100)", "varchar", "100"},
		{"char(36)", "char", "36"},
		{"varchar", "", ""},
		{"text", "", ""},
		{"integer", "", ""},
	}

	for _, tc := range tests {
		base, size := parseTypeWithSize(tc.input)
		if base != tc.expectedBase || size != tc.expectedSize {
			t.Errorf("parseTypeWithSize(%q) = (%q, %q), want (%q, %q)",
				tc.input, base, size, tc.expectedBase, tc.expectedSize)
		}
	}
}

func TestTypeAliases_Varchar_Postgres(t *testing.T) {
	d := &postgresDialect{}

	tests := []struct {
		input    string
		expected string
	}{
		{"varchar255", "VARCHAR(255)"},
		{"varchar100", "VARCHAR(100)"},
		{"varchar50", "VARCHAR(50)"},
	}

	for _, tc := range tests {
		result := d.MapType(tc.input, false, false)
		if result != tc.expected {
			t.Errorf("Postgres MapType(%q) = %q, want %q",
				tc.input, result, tc.expected)
		}
	}
}

func TestTypeAliases_Char_Postgres(t *testing.T) {
	d := &postgresDialect{}

	tests := []struct {
		input    string
		expected string
	}{
		{"char36", "CHAR(36)"},
		{"char1", "CHAR(1)"},
		{"char10", "CHAR(10)"},
	}

	for _, tc := range tests {
		result := d.MapType(tc.input, false, false)
		if result != tc.expected {
			t.Errorf("Postgres MapType(%q) = %q, want %q",
				tc.input, result, tc.expected)
		}
	}
}

func TestTypeAliases_Decimal_Postgres(t *testing.T) {
	d := &postgresDialect{}

	tests := []struct {
		input    string
		expected string
	}{
		{"decimal10_2", "NUMERIC(10,2)"},
		{"decimal19_4", "NUMERIC(19,4)"},
		{"numeric15_3", "NUMERIC(15,3)"},
	}

	for _, tc := range tests {
		result := d.MapType(tc.input, false, false)
		if result != tc.expected {
			t.Errorf("Postgres MapType(%q) = %q, want %q",
				tc.input, result, tc.expected)
		}
	}
}

func TestTypeAliases_MySQL(t *testing.T) {
	d := &mysqlDialect{}

	tests := []struct {
		input    string
		expected string
	}{
		{"varchar255", "VARCHAR(255)"},
		{"char36", "CHAR(36)"},
		{"decimal10_2", "DECIMAL(10,2)"},
		{"decimal19_4", "DECIMAL(19,4)"},
	}

	for _, tc := range tests {
		result := d.MapType(tc.input, false, false)
		if result != tc.expected {
			t.Errorf("MySQL MapType(%q) = %q, want %q",
				tc.input, result, tc.expected)
		}
	}
}

func TestTypeAliases_MSSQL(t *testing.T) {
	d := &mssqlDialect{}

	tests := []struct {
		input    string
		expected string
	}{
		{"varchar255", "NVARCHAR(255)"},
		{"char36", "NCHAR(36)"},
		{"decimal10_2", "DECIMAL(10,2)"},
	}

	for _, tc := range tests {
		result := d.MapType(tc.input, false, false)
		if result != tc.expected {
			t.Errorf("MSSQL MapType(%q) = %q, want %q",
				tc.input, result, tc.expected)
		}
	}
}

func TestTypeAliases_Oracle(t *testing.T) {
	d := &oracleDialect{}

	tests := []struct {
		input    string
		expected string
	}{
		{"varchar255", "VARCHAR2(255)"},
		{"char36", "CHAR(36)"},
		{"decimal10_2", "NUMBER(10,2)"},
	}

	for _, tc := range tests {
		result := d.MapType(tc.input, false, false)
		if result != tc.expected {
			t.Errorf("Oracle MapType(%q) = %q, want %q",
				tc.input, result, tc.expected)
		}
	}
}

// ============================================================================
// ComputeDiff Integration Tests with New Features
// ============================================================================

func TestComputeDiff_WithDefault(t *testing.T) {
	current := &sdata.DBInfo{
		Type:   "postgres",
		Tables: []sdata.DBTable{},
	}

	expected := &sdata.DBInfo{
		Type: "postgres",
		Tables: []sdata.DBTable{
			{
				Name: "orders",
				Columns: []sdata.DBColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
					{Name: "status", Type: "text", NotNull: true, Default: "'pending'"},
				},
			},
		},
	}

	ops := computeDiff(current, expected, DiffOptions{Destructive: false})

	if len(ops) == 0 {
		t.Fatal("expected at least one operation")
	}

	found := false
	for _, op := range ops {
		if op.Type == "create_table" && op.Table == "orders" {
			found = true
			if !strings.Contains(op.SQL, "DEFAULT 'pending'") {
				t.Errorf("expected DEFAULT 'pending' in SQL, got: %s", op.SQL)
			}
		}
	}

	if !found {
		t.Error("expected create_table operation for orders table")
	}
}

func TestComputeDiff_WithIndex(t *testing.T) {
	current := &sdata.DBInfo{
		Type:   "postgres",
		Tables: []sdata.DBTable{},
	}

	expected := &sdata.DBInfo{
		Type: "postgres",
		Tables: []sdata.DBTable{
			{
				Name: "users",
				Columns: []sdata.DBColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
					{Name: "email", Type: "text", Index: true},
				},
			},
		},
	}

	ops := computeDiff(current, expected, DiffOptions{Destructive: false})

	// Should have create_table and add_index operations
	var hasCreateTable, hasAddIndex bool
	for _, op := range ops {
		if op.Type == "create_table" && op.Table == "users" {
			hasCreateTable = true
		}
		if op.Type == "add_index" && op.Column == "email" {
			hasAddIndex = true
			if !strings.Contains(op.SQL, "CREATE INDEX") {
				t.Errorf("expected CREATE INDEX in SQL, got: %s", op.SQL)
			}
		}
	}

	if !hasCreateTable {
		t.Error("expected create_table operation")
	}
	if !hasAddIndex {
		t.Error("expected add_index operation for email column")
	}
}

func TestComputeDiff_WithCustomIndexName(t *testing.T) {
	current := &sdata.DBInfo{
		Type:   "postgres",
		Tables: []sdata.DBTable{},
	}

	expected := &sdata.DBInfo{
		Type: "postgres",
		Tables: []sdata.DBTable{
			{
				Name: "logs",
				Columns: []sdata.DBColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
					{Name: "created_at", Type: "timestamp", Index: true, IndexName: "idx_logs_time"},
				},
			},
		},
	}

	ops := computeDiff(current, expected, DiffOptions{Destructive: false})

	var foundIndex bool
	for _, op := range ops {
		if op.Type == "add_index" && op.Column == "created_at" {
			foundIndex = true
			if !strings.Contains(op.SQL, "idx_logs_time") {
				t.Errorf("expected custom index name idx_logs_time in SQL, got: %s", op.SQL)
			}
		}
	}

	if !foundIndex {
		t.Error("expected add_index operation for created_at column")
	}
}

func TestComputeDiff_WithCascade(t *testing.T) {
	// Parent table already exists
	current := &sdata.DBInfo{
		Type: "postgres",
		Tables: []sdata.DBTable{
			{
				Name: "users",
				Columns: []sdata.DBColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true},
				},
			},
		},
	}

	expected := &sdata.DBInfo{
		Type: "postgres",
		Tables: []sdata.DBTable{
			{
				Name: "users",
				Columns: []sdata.DBColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true},
				},
			},
			{
				Name: "posts",
				Columns: []sdata.DBColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
					{Name: "author_id", Type: "bigint", FKeyTable: "users", FKeyCol: "id", FKOnDelete: "CASCADE"},
				},
			},
		},
	}

	ops := computeDiff(current, expected, DiffOptions{Destructive: false})

	var foundCascade bool
	for _, op := range ops {
		if op.Type == "create_table" && op.Table == "posts" {
			if strings.Contains(op.SQL, "ON DELETE CASCADE") {
				foundCascade = true
			}
		}
	}

	if !foundCascade {
		t.Error("expected ON DELETE CASCADE in create_table SQL")
	}
}

func TestComputeDiff_AddColumn_WithDefault(t *testing.T) {
	current := &sdata.DBInfo{
		Type: "postgres",
		Tables: []sdata.DBTable{
			{
				Name: "users",
				Columns: []sdata.DBColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true},
				},
			},
		},
	}

	expected := &sdata.DBInfo{
		Type: "postgres",
		Tables: []sdata.DBTable{
			{
				Name: "users",
				Columns: []sdata.DBColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "status", Type: "text", Default: "'active'"},
				},
			},
		},
	}

	ops := computeDiff(current, expected, DiffOptions{Destructive: false})

	var foundAddColumn bool
	for _, op := range ops {
		if op.Type == "add_column" && op.Column == "status" {
			foundAddColumn = true
			if !strings.Contains(op.SQL, "DEFAULT 'active'") {
				t.Errorf("expected DEFAULT 'active' in add_column SQL, got: %s", op.SQL)
			}
		}
	}

	if !foundAddColumn {
		t.Error("expected add_column operation for status column")
	}
}

func TestComputeDiff_AddColumn_WithIndex(t *testing.T) {
	current := &sdata.DBInfo{
		Type: "postgres",
		Tables: []sdata.DBTable{
			{
				Name: "products",
				Columns: []sdata.DBColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true},
				},
			},
		},
	}

	expected := &sdata.DBInfo{
		Type: "postgres",
		Tables: []sdata.DBTable{
			{
				Name: "products",
				Columns: []sdata.DBColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "sku", Type: "text", Index: true},
				},
			},
		},
	}

	ops := computeDiff(current, expected, DiffOptions{Destructive: false})

	var foundAddColumn, foundAddIndex bool
	for _, op := range ops {
		if op.Type == "add_column" && op.Column == "sku" {
			foundAddColumn = true
		}
		if op.Type == "add_index" && op.Column == "sku" {
			foundAddIndex = true
		}
	}

	if !foundAddColumn {
		t.Error("expected add_column operation for sku column")
	}
	if !foundAddIndex {
		t.Error("expected add_index operation for sku column")
	}
}

// ============================================================================
// Clustering Key Tests
// ============================================================================

func TestSnowflakeCreateTable_WithClusteringKeys(t *testing.T) {
	d := &snowflakeDDLDialect{}

	table := sdata.DBTable{
		Name: "events",
		Columns: []sdata.DBColumn{
			{Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
			{Name: "created_at", Type: "timestamp", NotNull: true},
			{Name: "region", Type: "varchar", NotNull: true},
		},
		ClusteringKeys: []string{"created_at", "region"},
	}

	sql := d.CreateTable(table)

	if !strings.Contains(sql, "CLUSTER BY") {
		t.Errorf("expected CLUSTER BY in SQL, got: %s", sql)
	}
	if !strings.Contains(sql, `"created_at"`) {
		t.Errorf("expected created_at in CLUSTER BY, got: %s", sql)
	}
	if !strings.Contains(sql, `"region"`) {
		t.Errorf("expected region in CLUSTER BY, got: %s", sql)
	}
}

func TestSnowflakeCreateTable_WithoutClusteringKeys(t *testing.T) {
	d := &snowflakeDDLDialect{}

	table := sdata.DBTable{
		Name: "events",
		Columns: []sdata.DBColumn{
			{Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
		},
	}

	sql := d.CreateTable(table)

	if strings.Contains(sql, "CLUSTER BY") {
		t.Errorf("expected no CLUSTER BY in SQL, got: %s", sql)
	}
}

func TestSnowflakeAlterClusteringKey(t *testing.T) {
	d := &snowflakeDDLDialect{}

	sql := d.AlterClusteringKey("events", []string{"created_at", "user_id"})

	expected := `ALTER TABLE "events" CLUSTER BY ("created_at", "user_id");`
	if sql != expected {
		t.Errorf("got:\n%s\nwant:\n%s", sql, expected)
	}
}

func TestSnowflakeAlterClusteringKey_Empty(t *testing.T) {
	d := &snowflakeDDLDialect{}

	sql := d.AlterClusteringKey("events", nil)
	if sql != "" {
		t.Errorf("expected empty string for nil keys, got: %s", sql)
	}
}

func TestRedshiftAlterClusteringKey(t *testing.T) {
	d := &redshiftDDLDialect{}

	sql := d.AlterClusteringKey("events", []string{"created_at", "user_id"})

	expected := `ALTER TABLE "events" ALTER SORTKEY("created_at", "user_id");`
	if sql != expected {
		t.Errorf("got:\n%s\nwant:\n%s", sql, expected)
	}
}

func TestPostgresAlterClusteringKey_ReturnsEmpty(t *testing.T) {
	d := &postgresDialect{}
	sql := d.AlterClusteringKey("events", []string{"created_at"})
	if sql != "" {
		t.Errorf("expected empty string for postgres, got: %s", sql)
	}
}

func TestComputeDiff_ClusteringKeyChange(t *testing.T) {
	current := &sdata.DBInfo{
		Type: "snowflake",
		Tables: []sdata.DBTable{
			{
				Name: "events",
				Columns: []sdata.DBColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "created_at", Type: "timestamp"},
					{Name: "region", Type: "varchar"},
				},
				ClusteringKeys: []string{"created_at"},
			},
		},
	}

	expected := &sdata.DBInfo{
		Type: "snowflake",
		Tables: []sdata.DBTable{
			{
				Name: "events",
				Columns: []sdata.DBColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "created_at", Type: "timestamp"},
					{Name: "region", Type: "varchar"},
				},
				ClusteringKeys: []string{"created_at", "region"},
			},
		},
	}

	ops := computeDiff(current, expected, DiffOptions{})

	var found bool
	for _, op := range ops {
		if op.Type == "alter_clustering_key" {
			found = true
			if !strings.Contains(op.SQL, "CLUSTER BY") {
				t.Errorf("expected CLUSTER BY in SQL, got: %s", op.SQL)
			}
			if !strings.Contains(op.SQL, `"region"`) {
				t.Errorf("expected region in SQL, got: %s", op.SQL)
			}
		}
	}

	if !found {
		t.Error("expected alter_clustering_key operation")
	}
}

func TestComputeDiff_ClusteringKeyUnchanged(t *testing.T) {
	current := &sdata.DBInfo{
		Type: "snowflake",
		Tables: []sdata.DBTable{
			{
				Name:           "events",
				Columns:        []sdata.DBColumn{{Name: "id", Type: "bigint", PrimaryKey: true}},
				ClusteringKeys: []string{"created_at"},
			},
		},
	}

	expected := &sdata.DBInfo{
		Type: "snowflake",
		Tables: []sdata.DBTable{
			{
				Name:           "events",
				Columns:        []sdata.DBColumn{{Name: "id", Type: "bigint", PrimaryKey: true}},
				ClusteringKeys: []string{"created_at"},
			},
		},
	}

	ops := computeDiff(current, expected, DiffOptions{})

	for _, op := range ops {
		if op.Type == "alter_clustering_key" {
			t.Errorf("unexpected alter_clustering_key op when keys are unchanged: %s", op.SQL)
		}
	}
}

func TestSchemaDDLTemporalColumns(t *testing.T) {
	ddl := []byte(`
type orders {
  id: Bigint! @id
  due_date: Date!
  created_at: TimestampWithTimeZone! @default(value: "now()")
  updated_at: Timestamp
  open_time: Time
  name: Text!
  quantity: Integer
}

type notes {
  id: Bigint! @id
  body: Text!
}
`)
	tables, err := SchemaDDLTemporalColumns(ddl)
	if err != nil {
		t.Fatalf("SchemaDDLTemporalColumns: %v", err)
	}
	cols := tables["orders"]
	if len(cols) != 3 {
		t.Fatalf("orders temporal columns = %+v, want due_date, created_at, updated_at", cols)
	}
	byName := map[string]TemporalColumn{}
	for _, c := range cols {
		byName[c.Name] = c
	}
	if c, ok := byName["due_date"]; !ok || !c.DateOnly {
		t.Errorf("due_date = %+v, want DateOnly", c)
	}
	if c, ok := byName["created_at"]; !ok || c.DateOnly {
		t.Errorf("created_at = %+v, want timestamp", c)
	}
	if _, ok := byName["open_time"]; ok {
		t.Error("time-of-day column open_time must be excluded")
	}
	if len(tables["notes"]) != 0 {
		t.Errorf("notes has no temporal columns, got %+v", tables["notes"])
	}
}
