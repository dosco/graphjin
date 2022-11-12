//go:build !mysql

package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"

	"github.com/dosco/graphjin/core"
	"github.com/stretchr/testify/assert"
)

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

func TestFunctionTables(t *testing.T) {
	gql := `query {
		get_latest5_products(limit: 3) {
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
	}
	exp := `{"get_latest5_products":[{"id":100,"name":"Product 100"},{"id":99,"name":"Product 99"},{"id":98,"name":"Product 98"}]}`
	assert.Equal(t, exp, stdJSON(res.Data))
}

func TestFunctionTablesWithArgs(t *testing.T) {
	gql := `query {
		get_latest_users(limit: 2, args: [4, $tag]) {
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
	}
	exp := `{"get_latest_users":[{"full_name":"User 100","id":100,"tag_name":"boo"},{"full_name":"User 99","id":99,"tag_name":"boo"}]}`
	assert.Equal(t, exp, stdJSON(res.Data))
}
