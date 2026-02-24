package mongodriver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TestQueryDSLParsing tests the JSON query DSL parsing
func TestQueryDSLParsing(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantOp  string
		wantErr bool
	}{
		{
			name:   "aggregate query",
			query:  `{"operation":"aggregate","collection":"users","pipeline":[{"$match":{"age":{"$gt":25}}}]}`,
			wantOp: "aggregate",
		},
		{
			name:   "find query",
			query:  `{"operation":"find","collection":"users","filter":{"name":"test"}}`,
			wantOp: "find",
		},
		{
			name:   "introspect columns",
			query:  `{"operation":"introspect_columns","options":{"sample_size":100}}`,
			wantOp: "introspect_columns",
		},
		{
			name:    "missing operation",
			query:   `{"collection":"users"}`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			query:   `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := ParseQuery(tt.query)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseQuery() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && q.Operation != tt.wantOp {
				t.Errorf("ParseQuery() operation = %v, want %v", q.Operation, tt.wantOp)
			}
		})
	}
}

// TestParamSubstitution tests parameter placeholder substitution
func TestParamSubstitution(t *testing.T) {
	query := `{"operation":"aggregate","collection":"users","pipeline":[{"$match":{"age":{"$gt":"$1"},"name":"$2"}}],"params":["$1","$2"]}`

	q, err := ParseQuery(query)
	if err != nil {
		t.Fatalf("ParseQuery() error = %v", err)
	}

	err = q.SubstituteParams([]any{25, "John"})
	if err != nil {
		t.Fatalf("SubstituteParams() error = %v", err)
	}

	// Check that parameters were substituted
	match := q.Pipeline[0]["$match"].(map[string]any)
	ageFilter := match["age"].(map[string]any)
	if ageFilter["$gt"] != 25 {
		t.Errorf("Expected age.$gt = 25, got %v", ageFilter["$gt"])
	}
	if match["name"] != "John" {
		t.Errorf("Expected name = John, got %v", match["name"])
	}
}

func TestParamSubstitutionMultiMutation(t *testing.T) {
	query := `{
		"operation":"multi_mutation",
		"queries":[
			{"operation":"updateOne","collection":"users","field_name":"u1","filter":{"id":"$1"},"update":{"$set":{"full_name":"$2"}}},
			{"operation":"deleteOne","collection":"users","field_name":"u2","filter":{"id":"$3"}}
		]
	}`

	q, err := ParseQuery(query)
	if err != nil {
		t.Fatalf("ParseQuery() error = %v", err)
	}

	if err := q.SubstituteParams([]any{101, "Alice", 102}); err != nil {
		t.Fatalf("SubstituteParams() error = %v", err)
	}

	if got := q.Queries[0].Filter["id"]; got != 101 {
		t.Fatalf("query[0] filter.id = %v, want 101", got)
	}
	setMap := q.Queries[0].Update["$set"].(map[string]any)
	if got := setMap["full_name"]; got != "Alice" {
		t.Fatalf("query[0] update.$set.full_name = %v, want Alice", got)
	}
	if got := q.Queries[1].Filter["id"]; got != 102 {
		t.Fatalf("query[1] filter.id = %v, want 102", got)
	}
}

func TestExecuteMultiMutationAsQueryWithNullOps(t *testing.T) {
	conn := &Conn{}
	q := &QueryDSL{
		Operation: OpMultiMutation,
		Queries: []*QueryDSL{
			{Operation: OpNull, FieldName: "p1"},
			{Operation: OpNull, FieldName: "p2"},
		},
	}

	rows, err := conn.executeMultiMutationAsQuery(context.Background(), q)
	if err != nil {
		t.Fatalf("executeMultiMutationAsQuery() error = %v", err)
	}
	defer rows.Close()

	dest := make([]driver.Value, 1)
	if err := rows.Next(dest); err != nil {
		t.Fatalf("rows.Next() error = %v", err)
	}

	b, ok := dest[0].([]byte)
	if !ok {
		t.Fatalf("expected []byte row value, got %T", dest[0])
	}

	if string(b) != `{"p1":null,"p2":null}` {
		t.Fatalf("unexpected merged payload: %s", string(b))
	}
}

// TestConnectorCreation tests creating a MongoDB connector
func TestConnectorCreation(t *testing.T) {
	// This test doesn't require a running MongoDB instance
	// It just tests that the connector can be created
	ctx := context.Background()

	// Create a mock client (will fail to connect but that's ok for this test)
	clientOpts := options.Client().ApplyURI("mongodb://localhost:27017")
	client, err := mongo.Connect(clientOpts)
	if err != nil {
		t.Skipf("Skipping test - could not create mongo client: %v", err)
	}

	connector := NewConnector(client, "testdb")
	if connector == nil {
		t.Fatal("NewConnector returned nil")
	}

	if connector.Database() != "testdb" {
		t.Errorf("Database() = %v, want testdb", connector.Database())
	}

	// Test that we can open a sql.DB with the connector
	db := sql.OpenDB(connector)
	if db == nil {
		t.Fatal("sql.OpenDB returned nil")
	}
	defer db.Close()

	// The ping will fail without a running MongoDB, but that's expected
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_ = db.PingContext(ctx)
}

// Integration test that requires a running MongoDB instance
// Use testcontainers or skip if MongoDB is not available

func TestWithMongoDB(t *testing.T) {
	// Skip if MONGODB_URI is not set
	mongoURI := "mongodb://localhost:27017"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		t.Skipf("Skipping MongoDB integration test: %v", err)
	}

	// Check if MongoDB is actually running
	if err := client.Ping(ctx, nil); err != nil {
		t.Skipf("Skipping MongoDB integration test - server not available: %v", err)
	}
	defer client.Disconnect(ctx)

	// Create test database and collection
	db := client.Database("graphjin_test")
	coll := db.Collection("users")

	// Clean up before test
	coll.Drop(ctx)

	// Insert test data
	testDocs := []any{
		bson.M{"name": "Alice", "age": 30, "email": "alice@example.com"},
		bson.M{"name": "Bob", "age": 25, "email": "bob@example.com"},
		bson.M{"name": "Charlie", "age": 35, "email": "charlie@example.com"},
	}
	_, err = coll.InsertMany(ctx, testDocs)
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	// Create SQL DB using our driver
	connector := NewConnector(client, "graphjin_test")
	sqlDB := sql.OpenDB(connector)
	defer sqlDB.Close()

	t.Run("aggregate query", func(t *testing.T) {
		query := `{"operation":"aggregate","collection":"users","pipeline":[{"$match":{"age":{"$gt":25}}},{"$sort":{"name":1}}]}`

		rows, err := sqlDB.QueryContext(ctx, query)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		defer rows.Close()

		var result []byte
		if rows.Next() {
			if err := rows.Scan(&result); err != nil {
				t.Fatalf("Scan failed: %v", err)
			}
		}

		var users []map[string]any
		if err := json.Unmarshal(result, &users); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}

		if len(users) != 2 {
			t.Errorf("Expected 2 users, got %d", len(users))
		}

		// Should be sorted by name: Alice, Charlie
		if len(users) >= 2 {
			if users[0]["name"] != "Alice" {
				t.Errorf("Expected first user to be Alice, got %v", users[0]["name"])
			}
			if users[1]["name"] != "Charlie" {
				t.Errorf("Expected second user to be Charlie, got %v", users[1]["name"])
			}
		}
	})

	t.Run("introspect columns", func(t *testing.T) {
		query := `{"operation":"introspect_columns","options":{"sample_size":10}}`

		rows, err := sqlDB.QueryContext(ctx, query)
		if err != nil {
			t.Fatalf("Introspect failed: %v", err)
		}
		defer rows.Close()

		columns, err := rows.Columns()
		if err != nil {
			t.Fatalf("Columns() failed: %v", err)
		}

		expectedCols := []string{
			"table_schema", "table_name", "column_name", "data_type",
			"is_nullable", "is_primary_key", "is_unique_key", "is_array",
			"fkey_schema", "fkey_table", "fkey_column",
		}

		if len(columns) != len(expectedCols) {
			t.Errorf("Expected %d columns, got %d", len(expectedCols), len(columns))
		}

		// Should find the users collection
		foundUsers := false
		for rows.Next() {
			var schema, table, col, dataType string
			var nullable, pk, uk, isArray bool
			var fkSchema, fkTable, fkCol string

			err := rows.Scan(&schema, &table, &col, &dataType, &nullable, &pk, &uk, &isArray, &fkSchema, &fkTable, &fkCol)
			if err != nil {
				t.Logf("Scan error (may be type mismatch): %v", err)
				continue
			}

			if table == "users" {
				foundUsers = true
			}
		}

		if !foundUsers {
			t.Error("Expected to find 'users' collection in introspection")
		}
	})

	// Clean up
	coll.Drop(ctx)
}
