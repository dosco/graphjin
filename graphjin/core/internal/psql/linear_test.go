package psql_test

import (
	"testing"
    "strings"

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
    
    if !strings.Contains(sql, "CREATE TEMP TABLE IF NOT EXISTS _gj_ids") {
        t.Errorf("Expected CREATE TEMP TABLE")
    }
    if !strings.Contains(sql, "INSERT INTO _gj_ids") {
        t.Errorf("Expected INSERT INTO _gj_ids")
    }
     if !strings.Contains(sql, "DROP TABLE _gj_ids") {
        t.Errorf("Expected DROP TABLE _gj_ids")
    }
}
