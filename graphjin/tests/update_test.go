package tests_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/require"
)

func Example_update() {
	gql := `mutation {
		products(id: $id, update: $data) {
			id
			name
		}
	}`

	vars := json.RawMessage(`{ 
		"id": 100,
		"data": { 
			"name": "Updated Product 100",
			"description": "Description for updated product 100"
		} 
	}`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":{"id":100,"name":"Updated Product 100"}}
}

func Example_updateMultipleRelatedTables1() {
	gql := `mutation {
		purchases(id: $id, update: $data) {
			quantity
			customer {
				full_name
			}
			product {
				description
			}
		}
	}`

	vars := json.RawMessage(`{
		"id": 100,
		"data": {
			"quantity": 6,
			"customer": {
				"full_name": "Updated user related to purchase 100"
			},
			"product": {
				"description": "Updated product related to purchase 100"
			}
		}
	}`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, Debug: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"purchases":{"customer":{"full_name":"Updated user related to purchase 100"},"product":{"description":"Updated product related to purchase 100"},"quantity":6}}
}

func Example_updateTableAndConnectToRelatedTables() {
	gql := `mutation {
		users(id: $id, update: $data) {
			full_name
			products {
				id
			}
		}
	}`

	vars := json.RawMessage(`{
		"id": 100,
		"data": {
			"full_name": "Updated user 100",
			"products": {
				"connect": { "id": 99 },
				"disconnect": { "id": 100 }
			}
		}
	}`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"users":{"full_name":"Updated user 100","products":[{"id":99}]}}
}

func Example_updateTableAndRelatedTable() {
	gql := `mutation {
		users(id: $id, update: $data) {
			full_name
			products {
				id
			}
		}
	}`

	vars := json.RawMessage(`{
		"id": 90,
		"data": {
			"full_name": "Updated user 90",
			"products": {
				"where": { "id": { "gt": 1 } },
				"name": "Updated Product 90"
			}
		}
	}`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"users":{"full_name":"Updated user 90","products":[{"id":90}]}}
}

func Example_setArrayColumnToValue() {
	gql := `mutation {
		products(where: { id: 100 }, update: { tags: ["super", "great", "wow"] }) {
			id
			tags
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}

	// Output: {"products":[{"id":100,"tags":["super","great","wow"]}]}
}

func Example_setArrayColumnToEmpty() {
	gql := `mutation {
		products(where: { id: 100 }, update: { tags: [] }) {
			id
			tags
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}

	// Output: {"products":[{"id":100,"tags":[]}]}
}

func TestMultiAliasUpdate(t *testing.T) {
	gql := `mutation {
		p1: products(id: 87, update: $data1) {
			id
			name
		}
		p2: products(id: 88, update: $data2) {
			id
			name
		}
	}`

	vars := json.RawMessage(`{
		"data1": { "name": "Multi Alias Product 87" },
		"data2": { "name": "Multi Alias Product 88" }
	}`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars, nil)
	require.NoError(t, err)

	var result map[string]json.RawMessage
	err = json.Unmarshal(res.Data, &result)
	require.NoError(t, err)

	// Both aliases must be present
	require.Contains(t, result, "p1")
	require.Contains(t, result, "p2")

	var p1 map[string]any
	err = json.Unmarshal(result["p1"], &p1)
	require.NoError(t, err)
	require.Equal(t, float64(87), p1["id"])
	require.Equal(t, "Multi Alias Product 87", p1["name"])

	var p2 map[string]any
	err = json.Unmarshal(result["p2"], &p2)
	require.NoError(t, err)
	require.Equal(t, float64(88), p2["id"])
	require.Equal(t, "Multi Alias Product 88", p2["name"])

	// Restore original names to avoid polluting other tests
	restoreGql := `mutation {
		p1: products(id: 87, update: $data1) { id }
		p2: products(id: 88, update: $data2) { id }
	}`
	restoreVars := json.RawMessage(`{
		"data1": { "name": "Product 87" },
		"data2": { "name": "Product 88" }
	}`)
	_, _ = gj.GraphQL(ctx, restoreGql, restoreVars, nil)
}

// TestMultiAliasUpdateThreeRoots verifies that 3+ aliased mutations on the
// same table produce unique CTE names (orders_0, orders_1, orders_2) and
// do not collide. This was the original reported bug.
func TestMultiAliasUpdateThreeRoots(t *testing.T) {
	gql := `mutation {
		a1: products(id: 81, update: $d1) { id name }
		a2: products(id: 82, update: $d2) { id name }
		a3: products(id: 83, update: $d3) { id name }
	}`

	vars := json.RawMessage(`{
		"d1": { "name": "Tri 81" },
		"d2": { "name": "Tri 82" },
		"d3": { "name": "Tri 83" }
	}`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars, nil)
	require.NoError(t, err)

	var result map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(res.Data, &result))

	require.Contains(t, result, "a1")
	require.Contains(t, result, "a2")
	require.Contains(t, result, "a3")

	type want struct {
		id   float64
		name string
	}
	checks := map[string]want{
		"a1": {81, "Tri 81"},
		"a2": {82, "Tri 82"},
		"a3": {83, "Tri 83"},
	}
	for alias, w := range checks {
		var row map[string]any
		require.NoError(t, json.Unmarshal(result[alias], &row), alias)
		require.Equal(t, w.id, row["id"], alias+" id")
		require.Equal(t, w.name, row["name"], alias+" name")
	}

	// Restore
	rGql := `mutation {
		a1: products(id: 81, update: $d1) { id }
		a2: products(id: 82, update: $d2) { id }
		a3: products(id: 83, update: $d3) { id }
	}`
	rVars := json.RawMessage(`{
		"d1": { "name": "Product 81" },
		"d2": { "name": "Product 82" },
		"d3": { "name": "Product 83" }
	}`)
	_, _ = gj.GraphQL(ctx, rGql, rVars, nil)
}

func TestMultiAliasDelete(t *testing.T) {
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)

	// Insert two throwaway users
	insGql := `mutation { users(insert: $data) { id } }`

	res1, err := gj.GraphQL(ctx, insGql,
		json.RawMessage(`{"data":{"full_name":"Del User A","email":"del_a_multi@test.com"}}`), nil)
	require.NoError(t, err)
	var ins1 struct{ Users []struct{ ID int `json:"id"` } `json:"users"` }
	require.NoError(t, json.Unmarshal(res1.Data, &ins1))
	require.NotEmpty(t, ins1.Users)

	res2, err := gj.GraphQL(ctx, insGql,
		json.RawMessage(`{"data":{"full_name":"Del User B","email":"del_b_multi@test.com"}}`), nil)
	require.NoError(t, err)
	var ins2 struct{ Users []struct{ ID int `json:"id"` } `json:"users"` }
	require.NoError(t, json.Unmarshal(res2.Data, &ins2))
	require.NotEmpty(t, ins2.Users)

	// Delete both with multi-alias
	delGql := fmt.Sprintf(`mutation {
		d1: users(delete: true, where: { id: { eq: %d } }) { id }
		d2: users(delete: true, where: { id: { eq: %d } }) { id }
	}`, ins1.Users[0].ID, ins2.Users[0].ID)

	res, err := gj.GraphQL(ctx, delGql, nil, nil)
	require.NoError(t, err)

	var result map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Contains(t, result, "d1")
	require.Contains(t, result, "d2")
}
