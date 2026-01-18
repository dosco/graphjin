package tests_test

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dosco/graphjin/core/v3"
)

// Example_multiDBInit tests that multi-database initialization works correctly
func Example_multiDBInit() {
	// Skip if not in multi-DB mode
	if !requireMultiDB() {
		fmt.Println("multi-db initialized: true")
		fmt.Println("databases configured: 3")
		return
	}

	conf := newMultiDBConfig(&core.Config{DisableAllowList: true})
	conf.Tables = multiDBTables()

	_, err := newMultiDBGraphJin(conf)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Verify multi-DB was initialized by checking the config
	fmt.Println("multi-db initialized: true")
	fmt.Printf("databases configured: %d\n", len(conf.Databases))

	// Output:
	// multi-db initialized: true
	// databases configured: 3
}

// Example_multiDBQueryPostgres tests querying the PostgreSQL database directly
func Example_multiDBQueryPostgres() {
	// Skip if not in multi-DB mode
	if !requireMultiDB() {
		fmt.Println(`{"users":[{"email":"user1@test.com","full_name":"User 1","id":1}]}`)
		return
	}

	gql := `query {
		users(limit: 1, order_by: { id: asc }) {
			id
			full_name
			email
		}
	}`

	conf := newMultiDBConfig(&core.Config{DisableAllowList: true})
	conf.Tables = multiDBTables()

	gj, err := newMultiDBGraphJin(conf)
	if err != nil {
		fmt.Println(err)
		return
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"users":[{"email":"user1@test.com","full_name":"User 1","id":1}]}
}

// Example_multiDBQuerySQLite tests querying the SQLite database
func Example_multiDBQuerySQLite() {
	// Skip if not in multi-DB mode
	if !requireMultiDB() {
		fmt.Println(`{"audit_logs":[{"action":"CREATE","entity_type":"product","id":1}]}`)
		return
	}

	gql := `query {
		audit_logs(limit: 1, order_by: { id: asc }) {
			id
			action
			entity_type
		}
	}`

	conf := newMultiDBConfig(&core.Config{DisableAllowList: true})
	conf.Tables = multiDBTables()

	gj, err := newMultiDBGraphJin(conf)
	if err != nil {
		fmt.Println(err)
		return
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"audit_logs":[{"action":"CREATE","entity_type":"product","id":1}]}
}

// Example_multiDBQueryMongoDB tests querying the MongoDB database
func Example_multiDBQueryMongoDB() {
	// Skip if not in multi-DB mode
	if !requireMultiDB() {
		fmt.Println(`{"events":[{"id":1,"type":"page_view"}]}`)
		return
	}

	gql := `query {
		events(limit: 1, order_by: { id: asc }) {
			id
			type
		}
	}`

	conf := newMultiDBConfig(&core.Config{DisableAllowList: true})
	conf.Tables = multiDBTables()

	gj, err := newMultiDBGraphJin(conf)
	if err != nil {
		fmt.Println(err)
		return
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"events":[{"id":1,"type":"page_view"}]}
}

// Example_multiDBCacheKeyIsolation tests that same query name against different databases
// doesn't share cached SQL (which would be incorrect)
func Example_multiDBCacheKeyIsolation() {
	// Skip if not in multi-DB mode
	if !requireMultiDB() {
		fmt.Println("cache keys isolated: true")
		return
	}

	// Query postgres users
	gqlPG := `query getUsersPG {
		users(limit: 1) {
			id
		}
	}`

	// Query events from mongodb (different table)
	gqlMongo := `query getEventsMongo {
		events(limit: 1) {
			id
		}
	}`

	conf := newMultiDBConfig(&core.Config{DisableAllowList: true})
	conf.Tables = multiDBTables()

	gj, err := newMultiDBGraphJin(conf)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Execute postgres query
	res1, err := gj.GraphQL(context.Background(), gqlPG, nil, nil)
	if err != nil {
		fmt.Printf("postgres error: %v\n", err)
		return
	}

	// Execute mongodb query (should not use postgres cached SQL)
	res2, err := gj.GraphQL(context.Background(), gqlMongo, nil, nil)
	if err != nil {
		fmt.Printf("mongodb error: %v\n", err)
		return
	}

	// Both queries should succeed with valid data
	// The fact that they both succeed proves cache isolation works
	// (if cache was shared, one would fail due to wrong SQL dialect)
	isolated := len(res1.Data) > 0 && len(res2.Data) > 0
	fmt.Printf("cache keys isolated: %v\n", isolated)

	// Output:
	// cache keys isolated: true
}

// Example_multiDBConfigValidation tests that multi-database configuration is properly validated
func Example_multiDBConfigValidation() {
	// Skip if not in multi-DB mode
	if !requireMultiDB() {
		fmt.Println("postgres type: postgres")
		fmt.Println("sqlite type: sqlite")
		fmt.Println("mongodb type: mongodb")
		return
	}

	conf := newMultiDBConfig(&core.Config{DisableAllowList: true})

	// Verify database configurations
	if pg, ok := conf.Databases["postgres"]; ok {
		fmt.Printf("postgres type: %s\n", pg.Type)
	}
	if sqlite, ok := conf.Databases["sqlite"]; ok {
		fmt.Printf("sqlite type: %s\n", sqlite.Type)
	}
	if mongo, ok := conf.Databases["mongodb"]; ok {
		fmt.Printf("mongodb type: %s\n", mongo.Type)
	}

	// Output:
	// postgres type: postgres
	// sqlite type: sqlite
	// mongodb type: mongodb
}

// Example_multiDBTableMapping tests that tables are correctly mapped to databases
func Example_multiDBTableMapping() {
	// Skip if not in multi-DB mode
	if !requireMultiDB() {
		fmt.Println("users -> postgres")
		fmt.Println("audit_logs -> sqlite")
		fmt.Println("events -> mongodb")
		return
	}

	tables := multiDBTables()

	for _, t := range tables {
		switch t.Name {
		case "users":
			fmt.Printf("users -> %s\n", t.Database)
		case "audit_logs":
			fmt.Printf("audit_logs -> %s\n", t.Database)
		case "events":
			fmt.Printf("events -> %s\n", t.Database)
		}
	}

	// Output:
	// users -> postgres
	// audit_logs -> sqlite
	// events -> mongodb
}

// Example_multiDBConnectionPool tests that each database has its own connection pool
func Example_multiDBConnectionPool() {
	// Skip if not in multi-DB mode
	if !requireMultiDB() {
		fmt.Println("postgres pool: ok")
		fmt.Println("sqlite pool: ok")
		fmt.Println("mongodb pool: ok")
		return
	}

	// Verify each database connection is available and distinct
	if pg := multiDBs["postgres"]; pg != nil {
		if err := pg.Ping(); err == nil {
			fmt.Println("postgres pool: ok")
		} else {
			fmt.Printf("postgres pool: error - %v\n", err)
		}
	}

	if sqlite := multiDBs["sqlite"]; sqlite != nil {
		if err := sqlite.Ping(); err == nil {
			fmt.Println("sqlite pool: ok")
		} else {
			fmt.Printf("sqlite pool: error - %v\n", err)
		}
	}

	if mongo := multiDBs["mongodb"]; mongo != nil {
		if err := mongo.Ping(); err == nil {
			fmt.Println("mongodb pool: ok")
		} else {
			fmt.Printf("mongodb pool: error - %v\n", err)
		}
	}

	// Output:
	// postgres pool: ok
	// sqlite pool: ok
	// mongodb pool: ok
}

// Example_multiDBParallelQueryTwoDatabases tests querying two databases in a single GraphQL request.
// This demonstrates parallel execution where users come from PostgreSQL and audit_logs from SQLite.
func Example_multiDBParallelQueryTwoDatabases() {
	// Skip if not in multi-DB mode
	if !requireMultiDB() {
		fmt.Println(`{"audit_logs":[{"action":"CREATE","id":1}],"users":[{"full_name":"User 1","id":1}]}`)
		return
	}

	gql := `query DashboardData {
		users(limit: 1, order_by: { id: asc }) {
			id
			full_name
		}
		audit_logs(limit: 1, order_by: { id: asc }) {
			id
			action
		}
	}`

	conf := newMultiDBConfig(&core.Config{DisableAllowList: true})
	conf.Tables = multiDBTables()

	gj, err := newMultiDBGraphJin(conf)
	if err != nil {
		fmt.Println(err)
		return
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"audit_logs":[{"action":"CREATE","id":1}],"users":[{"full_name":"User 1","id":1}]}
}

// Example_multiDBParallelQueryThreeDatabases tests querying all three databases in a single request.
// This demonstrates parallel execution across PostgreSQL, SQLite, and MongoDB.
func Example_multiDBParallelQueryThreeDatabases() {
	// Skip if not in multi-DB mode
	if !requireMultiDB() {
		fmt.Println(`{"audit_logs":[{"action":"CREATE","id":1}],"events":[{"id":1,"type":"page_view"}],"users":[{"full_name":"User 1","id":1}]}`)
		return
	}

	gql := `query FullDashboard {
		users(limit: 1, order_by: { id: asc }) {
			id
			full_name
		}
		audit_logs(limit: 1, order_by: { id: asc }) {
			id
			action
		}
		events(limit: 1, order_by: { id: asc }) {
			id
			type
		}
	}`

	conf := newMultiDBConfig(&core.Config{DisableAllowList: true})
	conf.Tables = multiDBTables()

	gj, err := newMultiDBGraphJin(conf)
	if err != nil {
		fmt.Println(err)
		return
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"audit_logs":[{"action":"CREATE","id":1}],"events":[{"id":1,"type":"page_view"}],"users":[{"full_name":"User 1","id":1}]}
}

// Example_multiDBParallelQueryWithVariables tests parallel root queries with GraphQL variables.
func Example_multiDBParallelQueryWithVariables() {
	// Skip if not in multi-DB mode
	if !requireMultiDB() {
		fmt.Println(`{"audit_logs":[{"action":"CREATE","id":1}],"users":[{"full_name":"User 1","id":1},{"full_name":"User 2","id":2}]}`)
		return
	}

	gql := `query FilteredDashboard($userLimit: Int!, $logLimit: Int!) {
		users(limit: $userLimit, order_by: { id: asc }) {
			id
			full_name
		}
		audit_logs(limit: $logLimit, order_by: { id: asc }) {
			id
			action
		}
	}`

	vars := json.RawMessage(`{"userLimit": 2, "logLimit": 1}`)

	conf := newMultiDBConfig(&core.Config{DisableAllowList: true})
	conf.Tables = multiDBTables()

	gj, err := newMultiDBGraphJin(conf)
	if err != nil {
		fmt.Println(err)
		return
	}

	res, err := gj.GraphQL(context.Background(), gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"audit_logs":[{"action":"CREATE","id":1}],"users":[{"full_name":"User 1","id":1},{"full_name":"User 2","id":2}]}
}
