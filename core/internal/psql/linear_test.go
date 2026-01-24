package psql_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestLinearExecutionMySQL(t *testing.T) {
	schema, err := sdata.GetTestSchema()
	if err != nil {
		t.Fatal(err)
	}

	qc, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatal(err)
	}
    
    // Add role configuration similar to TestMain
    qc.AddRole("user", "public", "users", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "full_name", "email", "products"},
		},
        Insert: qcode.InsertConfig{},
	})
    
     qc.AddRole("user", "public", "products", qcode.TRConfig{
        Insert: qcode.InsertConfig{},
    })

	pc := psql.NewCompiler(psql.Config{
		DBType: "mysql",
	})
    
    gql := `mutation {
        users(insert: {
            full_name: "John Doe",
            email: "john@example.com",
            products: [
                { name: "Product A", price: 10 }
            ]
        }) {
            id
            full_name
            products {
                name
            }
        }
    }`
    
    // Compile QCode
	reqQC, err := qc.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
    
    // Compile SQL
    _, sqlBytes, err := pc.CompileEx(reqQC)
    if err != nil {
        t.Fatal(err)
    }
    
    sql := string(sqlBytes)
    t.Logf("Generated SQL: %s", sql)
    
    // Assertions
    if !strings.Contains(sql, "INSERT INTO `public`.`users`") {
        t.Errorf("Expected INSERT INTO `public`.`users`")
    }
    if !strings.Contains(sql, "LAST_INSERT_ID()") {
        t.Errorf("Expected LAST_INSERT_ID()")
    }
    if !strings.Contains(sql, "SET @") {
        t.Errorf("Expected SET @var")
    }
    if !strings.Contains(sql, ";") {
         t.Errorf("Expected multiple statements separated by ;")
    }
    
    // Check if ID is captured and used
    // Expected: SET @users_... = LAST_INSERT_ID()
    // INSERT INTO `products` ... VALUES (..., @users_...)
    
    // Check Result Selection
    // SELECT ... WHERE `users`.`id` = @users_...
    if !strings.Contains(sql, "SELECT") {
        t.Errorf("Expected final SELECT")
    }
     if !strings.Contains(sql, "WHERE") {
        t.Errorf("Expected WHERE clause in final SELECT")
    }
}

func TestLinearExecutionSQLite(t *testing.T) {
	schema, err := sdata.GetTestSchema()
	if err != nil {
		t.Fatal(err)
	}

	qc, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatal(err)
	}
    
    // Config same roles
    qc.AddRole("user", "public", "users", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "full_name"}},
        Insert: qcode.InsertConfig{},
	})
    qc.AddRole("user", "public", "products", qcode.TRConfig{Insert: qcode.InsertConfig{}})

	pc := psql.NewCompiler(psql.Config{DBType: "sqlite"})
    
    gql := `mutation {
        users(insert: {
            full_name: "Jane Doe",
            products: [{ name: "B" }]
        }) { id }
    }`
    
	reqQC, err := qc.Compile([]byte(gql), nil, "user", "")
	if err != nil { t.Fatal(err) }
    
    _, sqlBytes, err := pc.CompileEx(reqQC)
    if err != nil { t.Fatal(err) }
    
    sql := string(sqlBytes)
    t.Logf("Generated SQLite SQL: %s", sql)

    // SQLite uses temp table for ID capture
    if !strings.Contains(sql, "CREATE TEMP TABLE IF NOT EXISTS _gj_ids") {
        t.Errorf("Expected CREATE TEMP TABLE")
    }
    // SQLite uses comment-based ID capture (-- @gj_ids=varname) instead of explicit INSERT
    if !strings.Contains(sql, "-- @gj_ids=") {
        t.Errorf("Expected comment-based ID capture (-- @gj_ids=)")
    }
    // Verify cleanup statement exists
    if !strings.Contains(sql, "DROP TABLE") {
        t.Errorf("Expected DROP TABLE for cleanup")
    }
}

func TestLinearExecutionMySQLWithExplicitID(t *testing.T) {
	schema, err := sdata.GetTestSchema()
	if err != nil {
		t.Fatal(err)
	}

	qc, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatal(err)
	}
    
    // Add role configuration with insert allowed on products
    qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "description", "price", "owner_id"},
		},
        Insert: qcode.InsertConfig{
			Columns: []string{"id", "name", "description", "price"},
		},
	})
    
    // Add users for the join
    qc.AddRole("user", "public", "users", qcode.TRConfig{
        Query: qcode.QueryConfig{
            Columns: []string{"id", "full_name", "email"},
        },
    })

	pc := psql.NewCompiler(psql.Config{
		DBType: "mysql",
	})
    
    // This mutation uses JSON variable input similar to Example_insertWithPresets
    gql := `mutation {
        products(insert: $data) {
            id
            name
        }
    }`

    // Compile QCode with JSON vars containing explicit ID
	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{"id": 2001, "name": "Product 2001", "description": "Desc", "price": 10.5}`),
	}
    reqQC, err := qc.Compile([]byte(gql), vars, "user", "")
	if err != nil {
		t.Fatal(err)
	}
    
    // Compile SQL
    _, sqlBytes, err := pc.CompileEx(reqQC)
    if err != nil {
        t.Fatal(err)
    }
    
    sql := string(sqlBytes)
    t.Logf("Generated SQL: %s", sql)

    // Verify inline variable assignment captures the explicit PK
    if !strings.Contains(sql, "@products_0 :=") {
        t.Errorf("Expected inline variable assignment @products_0 :=")
    }

    // MySQL with explicit ID uses JSON_TABLE for lookup in WHERE clause
    // instead of direct variable comparison
    if !strings.Contains(sql, "JSON_TABLE") {
        t.Errorf("Expected JSON_TABLE for ID lookup")
    }

    // Verify the SELECT clause exists for returning results
    if !strings.Contains(sql, "SELECT") && !strings.Contains(sql, "json_object") {
        t.Errorf("Expected SELECT with json_object for result")
    }
}
