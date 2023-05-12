//go:build !mysql

package tests_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/assert"
)

func Example_queryWithWhereInWithVariableArrayColumn() {
	gql := `query {
		products(where: { tags: { in: $list } }, 
			limit: 2, 
			order_by: {id: asc}) {
			id
		}
	}`

	vars := json.RawMessage(`{
		"list": ["Tag 1", "Tag 2"]
	}`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":[{"id":1},{"id":2}]}
}

func Example_queryWithWhereInWithStaticArrayColumn() {
	gql := `query {
		products(where: { tags: { in: ["Tag 1", "Tag 2"] } }, 
			limit: 2, 
			order_by: {id: asc}) {
			id
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":[{"id":1},{"id":2}]}
}

func Example_queryWithWhereInWithVariableNumericArrayColumn() {
	gql := `query {
		products(where: { category_ids: { in: $list } }, 
			limit: 2, 
			order_by: {id: asc}) {
			id
		}
	}`

	vars := json.RawMessage(`{
		"list": [1, 2]
	}`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":[{"id":1},{"id":2}]}
}

func Example_queryWithWhereInWithStaticNumericArrayColumn() {
	gql := `query {
		products(where: { category_ids: { in: [1,2] } }, 
			limit: 2, 
			order_by: {id: asc}) {
			id
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":[{"id":1},{"id":2}]}
}

func Example_queryWithFunctionFields() {
	gql := `
	query {
		products(id: 51) {
			id
			name
			is_hot_product(args: { id: id })
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":{"id":51,"is_hot_product":true,"name":"Product 51"}}
}

func Example_queryWithFunctionFieldsArgList() {
	gql := `
	query {
		products(id: 51) {
			id
			name
			is_hot_product(args: {a0: 51})
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":{"id":51,"is_hot_product":true,"name":"Product 51"}}
}

func Example_queryWithFunctionReturingTables() {
	gql := `query {
		get_oldest5_products(limit: 3) {
			id
			name
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"get_oldest5_products":[{"id":1,"name":"Product 1"},{"id":2,"name":"Product 2"},{"id":3,"name":"Product 3"}]}
}

func Example_queryWithFunctionReturingTablesWithArgs() {
	gql := `query {
		get_oldest_users(limit: 2, args: {a0: 4, a1: $tag}) {
			tag_name
			id
			full_name
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	vars := json.RawMessage(`{ "tag": "boo" }`)
	res, err := gj.GraphQL(context.Background(), gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"get_oldest_users":[{"full_name":"User 1","id":1,"tag_name":"boo"},{"full_name":"User 2","id":2,"tag_name":"boo"}]}
}

func Example_queryWithFunctionReturingTablesWithNamedArgs() {
	gql := `query {
		get_oldest_users(limit: 2, args: { user_count: 4, tag: $tag }) {
			tag_name
			id
			full_name
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	vars := json.RawMessage(`{ "tag": "boo" }`)
	res, err := gj.GraphQL(context.Background(), gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"get_oldest_users":[{"full_name":"User 1","id":1,"tag_name":"boo"},{"full_name":"User 2","id":2,"tag_name":"boo"}]}
}

func Example_queryWithFunctionReturingUserDefinedTypes() {
	gql := `query {
		get_product(limit: 2, args: { a0: 1 }) {
			id
			name
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"get_product":[{"id":1,"name":"Product 1"},{"id":2,"name":"Product 2"}]}
}

func Example_queryWithFunctionAndDirectives() {
	gql := `
	query {
		products(id: 51) {
			id
			name
			is_hot_product(args: {id: id}, skipIf: { id: { eq: 51 } })
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":{"id":51,"is_hot_product":null,"name":"Product 51"}}
}

func Example_queryWithVariableLimit() {
	gql := `query {
		products(limit: $limit) {
			id
		}
	}`

	vars := json.RawMessage(`{
		"limit": 10
	}`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":[{"id":1},{"id":2},{"id":3},{"id":4},{"id":5},{"id":6},{"id":7},{"id":8},{"id":9},{"id":10}]}
}

func TestMutiSchema(t *testing.T) {
	totalSchemas := 20
	totalTables := 5

	for i := 0; i < totalSchemas; i++ {
		createSchemaSQL := `CREATE SCHEMA test_schema_%d;`
		_, err := db.Exec(fmt.Sprintf(createSchemaSQL, i))
		if err != nil {
			t.Fatal(err)
		}

		for j := 0; j < totalTables; j++ {
			st := fmt.Sprintf("test_schema_%d.test_table_%d_%d", i, i, j)
			refCol := "bigint"
			if i != 0 {
				refCol = fmt.Sprintf("bigint references test_schema_%d.test_table_%d_%d(id)",
					(i - 1), (i - 1), j)
			}
			createTableSQL := `CREATE TABLE %s (
   			id BIGSERIAL PRIMARY KEY,
   			column1 TEXT,
   			column2 TEXT,
   			column3 TEXT,
   			column4 TEXT,
   			column5 TEXT,
   			column6 JSON,
   			column7 bool DEFAULT false,
   			column8 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
   			column9 NUMERIC,
   			column10 JSONB,
   			column11 TEXT,
   			"colUMN_12" TEXT,
			"colUMN_13" %s
   		  );`

			_, err := db.Exec(fmt.Sprintf(createTableSQL, st, refCol))
			if err != nil {
				t.Fatal(err)
			}
		}
	}

	sn := rand.Intn(totalSchemas - 1) //nolint:gosec
	tn := rand.Intn(totalTables - 1)  //nolint:gosec

	gql := fmt.Sprintf(`query {
		test_table_%d_%d @schema(name: "test_schema_%d") {
   			column1
   			column2
   			column3
			colUMN_13
   		}
   	}`, sn, tn, sn)

	tname := fmt.Sprintf(`test_schema_%d.test_table_%d_%d`, sn, sn, tn)
	exp := fmt.Sprintf(`{"test_table_%d_%d":[]}`, sn, tn)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})

	err := conf.AddRoleTable("user", tname, core.Query{
		Filters: []string{`{ colUMN_12: { is_null: true } }`},
		Limit:   1,
	})
	assert.NoError(t, err)

	gj, err := core.NewGraphJin(conf, db)
	assert.NoError(t, err)

	ctx := context.WithValue(context.Background(), core.UserIDKey, 1)
	res, err := gj.GraphQL(ctx, gql, nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, exp, stdJSON(res.Data))
}
