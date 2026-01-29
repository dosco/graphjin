package qcode

import (
	"testing"
)

// ============================================================================
// Schema Parsing Tests for New Directives
// ============================================================================

// TestParseSchemaWithDefault tests parsing schema with @default directive
func TestParseSchemaWithDefault(t *testing.T) {
	schema := []byte(`# dbinfo:postgres,1,public
type orders {
	id: BigInt! @id
	status: Text! @default(value: "'pending'")
	quantity: Integer @default(value: "1")
	created_at: Timestamp @default(value: "now()")
}
`)

	ds, err := ParseSchema(schema)
	if err != nil {
		t.Fatalf("ParseSchema failed: %v", err)
	}

	if len(ds.Columns) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(ds.Columns))
	}

	// Find status column and verify default
	var statusFound, quantityFound, createdAtFound bool
	for _, col := range ds.Columns {
		switch col.Name {
		case "status":
			statusFound = true
			if col.Default != "'pending'" {
				t.Errorf("status Default = %q, want %q", col.Default, "'pending'")
			}
		case "quantity":
			quantityFound = true
			if col.Default != "1" {
				t.Errorf("quantity Default = %q, want %q", col.Default, "1")
			}
		case "created_at":
			createdAtFound = true
			if col.Default != "now()" {
				t.Errorf("created_at Default = %q, want %q", col.Default, "now()")
			}
		}
	}

	if !statusFound {
		t.Error("status column not found")
	}
	if !quantityFound {
		t.Error("quantity column not found")
	}
	if !createdAtFound {
		t.Error("created_at column not found")
	}
}

// TestParseSchemaWithIndex tests parsing schema with @index directive
func TestParseSchemaWithIndex(t *testing.T) {
	schema := []byte(`# dbinfo:postgres,1,public
type users {
	id: BigInt! @id
	email: Text! @index
	name: Text
}
`)

	ds, err := ParseSchema(schema)
	if err != nil {
		t.Fatalf("ParseSchema failed: %v", err)
	}

	if len(ds.Columns) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(ds.Columns))
	}

	// Find email column and verify index
	var emailFound bool
	for _, col := range ds.Columns {
		if col.Name == "email" {
			emailFound = true
			if !col.Index {
				t.Error("email Index should be true")
			}
		}
	}

	if !emailFound {
		t.Error("email column not found")
	}
}

// TestParseSchemaWithIndexCustomName tests parsing schema with @index(name: ...) directive
func TestParseSchemaWithIndexCustomName(t *testing.T) {
	schema := []byte(`# dbinfo:postgres,1,public
type logs {
	id: BigInt! @id
	created_at: Timestamp @index(name: "idx_logs_time")
}
`)

	ds, err := ParseSchema(schema)
	if err != nil {
		t.Fatalf("ParseSchema failed: %v", err)
	}

	if len(ds.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(ds.Columns))
	}

	// Find created_at column and verify index name
	var createdAtFound bool
	for _, col := range ds.Columns {
		if col.Name == "created_at" {
			createdAtFound = true
			if !col.Index {
				t.Error("created_at Index should be true")
			}
			if col.IndexName != "idx_logs_time" {
				t.Errorf("created_at IndexName = %q, want %q", col.IndexName, "idx_logs_time")
			}
		}
	}

	if !createdAtFound {
		t.Error("created_at column not found")
	}
}

// TestParseSchemaWithRelationCascade tests parsing @relation with onDelete/onUpdate
func TestParseSchemaWithRelationCascade(t *testing.T) {
	schema := []byte(`# dbinfo:postgres,1,public
type posts {
	id: BigInt! @id
	author_id: BigInt! @relation(type: "users", field: "id", onDelete: "CASCADE", onUpdate: "SET NULL")
	title: Text!
}
`)

	ds, err := ParseSchema(schema)
	if err != nil {
		t.Fatalf("ParseSchema failed: %v", err)
	}

	if len(ds.Columns) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(ds.Columns))
	}

	// Find author_id column and verify cascade options
	var authorIdFound bool
	for _, col := range ds.Columns {
		if col.Name == "author_id" {
			authorIdFound = true
			if col.FKeyTable != "users" {
				t.Errorf("author_id FKeyTable = %q, want %q", col.FKeyTable, "users")
			}
			if col.FKeyCol != "id" {
				t.Errorf("author_id FKeyCol = %q, want %q", col.FKeyCol, "id")
			}
			if col.FKOnDelete != "CASCADE" {
				t.Errorf("author_id FKOnDelete = %q, want %q", col.FKOnDelete, "CASCADE")
			}
			if col.FKOnUpdate != "SET NULL" {
				t.Errorf("author_id FKOnUpdate = %q, want %q", col.FKOnUpdate, "SET NULL")
			}
		}
	}

	if !authorIdFound {
		t.Error("author_id column not found")
	}
}

// TestParseSchemaWithRelationOnlyOnDelete tests @relation with only onDelete
func TestParseSchemaWithRelationOnlyOnDelete(t *testing.T) {
	schema := []byte(`# dbinfo:postgres,1,public
type comments {
	id: BigInt! @id
	post_id: BigInt! @relation(type: "posts", field: "id", onDelete: "CASCADE")
}
`)

	ds, err := ParseSchema(schema)
	if err != nil {
		t.Fatalf("ParseSchema failed: %v", err)
	}

	// Find post_id column
	var postIdFound bool
	for _, col := range ds.Columns {
		if col.Name == "post_id" {
			postIdFound = true
			if col.FKOnDelete != "CASCADE" {
				t.Errorf("post_id FKOnDelete = %q, want %q", col.FKOnDelete, "CASCADE")
			}
			if col.FKOnUpdate != "" {
				t.Errorf("post_id FKOnUpdate = %q, want empty string", col.FKOnUpdate)
			}
		}
	}

	if !postIdFound {
		t.Error("post_id column not found")
	}
}

// TestParseSchemaWithRelationSetNull tests @relation with SET NULL option
func TestParseSchemaWithRelationSetNull(t *testing.T) {
	schema := []byte(`# dbinfo:postgres,1,public
type products {
	id: BigInt! @id
	category_id: BigInt @relation(type: "categories", field: "id", onDelete: "SET NULL", onUpdate: "NO ACTION")
}
`)

	ds, err := ParseSchema(schema)
	if err != nil {
		t.Fatalf("ParseSchema failed: %v", err)
	}

	// Find category_id column
	var categoryIdFound bool
	for _, col := range ds.Columns {
		if col.Name == "category_id" {
			categoryIdFound = true
			if col.FKOnDelete != "SET NULL" {
				t.Errorf("category_id FKOnDelete = %q, want %q", col.FKOnDelete, "SET NULL")
			}
			if col.FKOnUpdate != "NO ACTION" {
				t.Errorf("category_id FKOnUpdate = %q, want %q", col.FKOnUpdate, "NO ACTION")
			}
		}
	}

	if !categoryIdFound {
		t.Error("category_id column not found")
	}
}

// TestParseSchemaWithMultipleDirectives tests combining multiple directives
func TestParseSchemaWithMultipleDirectives(t *testing.T) {
	schema := []byte(`# dbinfo:postgres,1,public
type accounts {
	id: BigInt! @id
	email: Text! @unique @index(name: "idx_accounts_email")
	status: Text! @default(value: "'active'") @index
}
`)

	ds, err := ParseSchema(schema)
	if err != nil {
		t.Fatalf("ParseSchema failed: %v", err)
	}

	// Find email column
	var emailFound bool
	for _, col := range ds.Columns {
		if col.Name == "email" {
			emailFound = true
			if !col.UniqueKey {
				t.Error("email UniqueKey should be true")
			}
			if !col.Index {
				t.Error("email Index should be true")
			}
			if col.IndexName != "idx_accounts_email" {
				t.Errorf("email IndexName = %q, want %q", col.IndexName, "idx_accounts_email")
			}
		}
	}

	if !emailFound {
		t.Error("email column not found")
	}

	// Find status column
	var statusFound bool
	for _, col := range ds.Columns {
		if col.Name == "status" {
			statusFound = true
			if col.Default != "'active'" {
				t.Errorf("status Default = %q, want %q", col.Default, "'active'")
			}
			if !col.Index {
				t.Error("status Index should be true")
			}
		}
	}

	if !statusFound {
		t.Error("status column not found")
	}
}

// TestParseSchemaWithTypeAliases tests parsing type aliases like Varchar255
func TestParseSchemaWithTypeAliases(t *testing.T) {
	schema := []byte(`# dbinfo:postgres,1,public
type items {
	id: BigInt! @id
	code: Varchar255!
	uuid: Char36
	price: Decimal10_2
}
`)

	ds, err := ParseSchema(schema)
	if err != nil {
		t.Fatalf("ParseSchema failed: %v", err)
	}

	if len(ds.Columns) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(ds.Columns))
	}

	// Verify column types (converted to snake_case with space)
	typeChecks := map[string]string{
		"code":  "varchar255",
		"uuid":  "char36",
		"price": "decimal10_2",
	}

	for _, col := range ds.Columns {
		expectedType, ok := typeChecks[col.Name]
		if ok {
			if col.Type != expectedType {
				t.Errorf("column %s Type = %q, want %q", col.Name, col.Type, expectedType)
			}
		}
	}
}

// TestParseSchemaWithNewTypes tests parsing new types (Money, Xml, Serial, Interval)
func TestParseSchemaWithNewTypes(t *testing.T) {
	schema := []byte(`# dbinfo:postgres,1,public
type transactions {
	id: Serial! @id
	amount: Money!
	description: Xml
	duration: Interval
}
`)

	ds, err := ParseSchema(schema)
	if err != nil {
		t.Fatalf("ParseSchema failed: %v", err)
	}

	if len(ds.Columns) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(ds.Columns))
	}

	// Verify column types
	typeChecks := map[string]string{
		"id":          "serial",
		"amount":      "money",
		"description": "xml",
		"duration":    "interval",
	}

	for _, col := range ds.Columns {
		expectedType, ok := typeChecks[col.Name]
		if ok {
			if col.Type != expectedType {
				t.Errorf("column %s Type = %q, want %q", col.Name, col.Type, expectedType)
			}
		}
	}
}

// TestParseSchemaWithDatabase tests parsing @database directive
func TestParseSchemaWithDatabase(t *testing.T) {
	schema := []byte(`# dbinfo:postgres,1,public
type remote_users @database(name: "replica") {
	id: BigInt! @id
	name: Text!
}
`)

	ds, err := ParseSchema(schema)
	if err != nil {
		t.Fatalf("ParseSchema failed: %v", err)
	}

	if len(ds.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(ds.Columns))
	}

	// Verify database is set on columns
	for _, col := range ds.Columns {
		if col.Database != "replica" {
			t.Errorf("column %s Database = %q, want %q", col.Name, col.Database, "replica")
		}
	}
}

// TestParseSchemaComplete tests a complete schema with all features
func TestParseSchemaComplete(t *testing.T) {
	schema := []byte(`# dbinfo:postgres,1,public
type users {
	id: BigInt! @id
	email: Varchar255! @unique @index(name: "idx_users_email")
	name: Text!
	status: Text! @default(value: "'active'")
	balance: Decimal10_2 @default(value: "0.00")
	created_at: Timestamp @default(value: "now()")
}

type posts {
	id: BigInt! @id
	author_id: BigInt! @relation(type: "users", field: "id", onDelete: "CASCADE")
	title: Text!
	content: Text @search
	views: Integer @default(value: "0") @index
}
`)

	ds, err := ParseSchema(schema)
	if err != nil {
		t.Fatalf("ParseSchema failed: %v", err)
	}

	// We should have 11 columns total (6 from users, 5 from posts)
	if len(ds.Columns) != 11 {
		t.Fatalf("expected 11 columns, got %d", len(ds.Columns))
	}

	// Verify users.email
	var usersEmailFound bool
	for _, col := range ds.Columns {
		if col.Table == "users" && col.Name == "email" {
			usersEmailFound = true
			if !col.UniqueKey {
				t.Error("users.email UniqueKey should be true")
			}
			if !col.Index {
				t.Error("users.email Index should be true")
			}
			if col.IndexName != "idx_users_email" {
				t.Errorf("users.email IndexName = %q, want idx_users_email", col.IndexName)
			}
		}
	}
	if !usersEmailFound {
		t.Error("users.email column not found")
	}

	// Verify posts.author_id
	var postsAuthorIdFound bool
	for _, col := range ds.Columns {
		if col.Table == "posts" && col.Name == "author_id" {
			postsAuthorIdFound = true
			if col.FKeyTable != "users" {
				t.Errorf("posts.author_id FKeyTable = %q, want users", col.FKeyTable)
			}
			if col.FKOnDelete != "CASCADE" {
				t.Errorf("posts.author_id FKOnDelete = %q, want CASCADE", col.FKOnDelete)
			}
		}
	}
	if !postsAuthorIdFound {
		t.Error("posts.author_id column not found")
	}

	// Verify posts.content (search)
	var postsContentFound bool
	for _, col := range ds.Columns {
		if col.Table == "posts" && col.Name == "content" {
			postsContentFound = true
			if !col.FullText {
				t.Error("posts.content FullText should be true")
			}
		}
	}
	if !postsContentFound {
		t.Error("posts.content column not found")
	}

	// Verify posts.views
	var postsViewsFound bool
	for _, col := range ds.Columns {
		if col.Table == "posts" && col.Name == "views" {
			postsViewsFound = true
			if col.Default != "0" {
				t.Errorf("posts.views Default = %q, want 0", col.Default)
			}
			if !col.Index {
				t.Error("posts.views Index should be true")
			}
		}
	}
	if !postsViewsFound {
		t.Error("posts.views column not found")
	}
}

// TestPascalToSnakeSpace tests the pascalToSnakeSpace helper function
func TestPascalToSnakeSpace(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"BigInt", "big int"},
		{"Text", "text"},
		{"Varchar255", "varchar255"},
		{"Decimal10_2", "decimal10_2"},
		{"TimestampWithTimeZone", "timestamp with time zone"},
		{"Jsonb", "jsonb"},
		{"UUID", "u u i d"},
		{"Id", "id"},
	}

	for _, tc := range tests {
		result := pascalToSnakeSpace(tc.input)
		if result != tc.expected {
			t.Errorf("pascalToSnakeSpace(%q) = %q, want %q",
				tc.input, result, tc.expected)
		}
	}
}
