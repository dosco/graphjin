package integration_tests

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/dosco/super-graph/core"
	"github.com/stretchr/testify/require"
)

func SetupSchema(t *testing.T, db *sql.DB) {

	_, err := db.Exec(`
CREATE TABLE users (
  id 		 integer PRIMARY KEY,
  full_name  text
)`)
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TABLE product (
  id            integer PRIMARY KEY,
  name          text,
  weight        float
)`)
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TABLE line_item (
  id            integer PRIMARY KEY,
  product       integer REFERENCES product(id),
  quantity      integer,
  price      	float
)`)
	require.NoError(t, err)
}

func DropSchema(t *testing.T, db *sql.DB) {

	_, err := db.Exec(`DROP TABLE IF EXISTS line_item`)
	require.NoError(t, err)

	_, err = db.Exec(`DROP TABLE IF EXISTS product`)
	require.NoError(t, err)

	_, err = db.Exec(`DROP TABLE IF EXISTS users`)
	require.NoError(t, err)
}

func TestSuperGraph(t *testing.T, db *sql.DB, before func(t *testing.T)) {
	config := core.Config{}
	config.UseAllowList = false
	config.AllowListFile = "./allow.list"
	config.RolesQuery = `SELECT * FROM users WHERE id = $user_id`

	config.Roles = []core.Role{
		core.Role{
			Name: "anon",
			Tables: []core.RoleTable{
				core.RoleTable{Name: "users", Query: core.Query{Limit: 100}},
				core.RoleTable{Name: "product", Query: core.Query{Limit: 100}},
				core.RoleTable{Name: "line_item", Query: core.Query{Limit: 100}},
			},
		},
	}

	sg, err := core.NewSuperGraph(&config, db)
	require.NoError(t, err)
	ctx := context.Background()

	t.Run("seed fixtures", func(t *testing.T) {
		before(t)
		res, err := sg.GraphQL(ctx,
			`mutation { products (insert: $products) { id } }`,
			json.RawMessage(`{"products":[
{"id":1, "name":"Charmin Ultra Soft", "weight": 0.5},
{"id":2, "name":"Hand Sanitizer", "weight": 0.2},
{"id":3, "name":"Case of Corona", "weight": 1.2}
]}`))
		require.NoError(t, err, res.SQL())
		require.Equal(t, `{"products": [{"id": 1}, {"id": 2}, {"id": 3}]}`, string(res.Data))

		res, err = sg.GraphQL(ctx,
			`mutation { line_items (insert: $line_items) { id } }`,
			json.RawMessage(`{"line_items":[
{"id":5001, "product":1, "price":6.95, "quantity":10},
{"id":5002, "product":2, "price":10.99, "quantity":2}
]}`))
		require.NoError(t, err, res.SQL())
		require.Equal(t, `{"line_items": [{"id": 5001}, {"id": 5002}]}`, string(res.Data))
	})

	t.Run("get line items", func(t *testing.T) {
		before(t)
		res, err := sg.GraphQL(ctx,
			`query { line_items { id, price, quantity } }`,
			json.RawMessage(`{}`))
		require.NoError(t, err, res.SQL())
		require.Equal(t, `{"line_items": [{"id": 5001, "price": 6.95, "quantity": 10}, {"id": 5002, "price": 10.99, "quantity": 2}]}`, string(res.Data))
	})

	t.Run("update line item", func(t *testing.T) {
		before(t)
		res, err := sg.GraphQL(ctx,
			`mutation { line_item(update:$update, id:$id) { id } }`,
			json.RawMessage(`{"id":5001, "update":{"quantity":20}}`))
		require.NoError(t, err, res.SQL())
		require.Equal(t, `{"line_item": {"id": 5001}}`, string(res.Data))

		res, err = sg.GraphQL(ctx,
			`query { line_item(id:$id) { id, price, quantity } }`,
			json.RawMessage(`{"id":5001}`))
		require.NoError(t, err, res.SQL())
		require.Equal(t, `{"line_item": {"id": 5001, "price": 6.95, "quantity": 20}}`, string(res.Data))
	})

	t.Run("delete line item", func(t *testing.T) {
		before(t)
		res, err := sg.GraphQL(ctx,
			`mutation { line_item(delete:true, id:$id) { id } }`,
			json.RawMessage(`{"id":5002}`))
		require.NoError(t, err, res.SQL())
		require.Equal(t, `{"line_item": {"id": 5002}}`, string(res.Data))

		res, err = sg.GraphQL(ctx,
			`query { line_items { id, price, quantity } }`,
			json.RawMessage(`{}`))
		require.NoError(t, err, res.SQL())
		require.Equal(t, `{"line_items": [{"id": 5001, "price": 6.95, "quantity": 20}]}`, string(res.Data))
	})

	t.Run("nested insert", func(t *testing.T) {
		before(t)
		res, err := sg.GraphQL(ctx,
			`mutation { line_items (insert: $line_item) { id, product { name } } }`,
			json.RawMessage(`{"line_item":
{"id":5003, "product": { "connect": { "id": 1} }, "price":10.95, "quantity":15}
}`))
		require.NoError(t, err, res.SQL())
		require.Equal(t, `{"line_items": [{"id": 5003, "product": {"name": "Charmin Ultra Soft"}}]}`, string(res.Data))
	})

}
