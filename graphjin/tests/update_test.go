package tests_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/require"
)

func parseInsertedIDLiteral(data []byte) (string, error) {
	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", err
	}
	idVal, ok := findJSONIDValue(payload)
	if !ok {
		return "", fmt.Errorf("missing id in mutation response: %s", string(data))
	}
	return idLiteralFromAny(idVal)
}

func idLiteralFromAny(idVal any) (string, error) {
	switch v := idVal.(type) {
	case float64:
		return strconv.FormatInt(int64(v), 10), nil
	case string:
		return strconv.Quote(v), nil
	default:
		return "", fmt.Errorf("unsupported id type %T", v)
	}
}

func findJSONIDValue(v any) (any, bool) {
	switch x := v.(type) {
	case map[string]any:
		if id, ok := x["id"]; ok {
			switch id.(type) {
			case float64, string:
				return id, true
			}
		}
		for _, child := range x {
			if id, ok := findJSONIDValue(child); ok {
				return id, true
			}
		}
	case []any:
		for _, child := range x {
			if id, ok := findJSONIDValue(child); ok {
				return id, true
			}
		}
	}
	return nil, false
}

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
	// Cleanup: restore product 100 to original state
	_, _ = db.Exec(`UPDATE products SET name = 'Product 100', description = 'Description for product 100' WHERE id = 100`)

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
	// Cleanup: restore data to original state
	_, _ = db.Exec(`UPDATE purchases SET quantity = 1000 WHERE id = 100`)
	_, _ = db.Exec(`UPDATE users SET full_name = 'User 1' WHERE id = 1`)
	_, _ = db.Exec(`UPDATE products SET description = 'Description for product 100' WHERE id = 100`)

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
	// Cleanup: restore product ownership to original state
	_, _ = db.Exec(`UPDATE products SET owner_id = 99 WHERE id = 99`)
	_, _ = db.Exec(`UPDATE products SET owner_id = 100 WHERE id = 100`)
	_, _ = db.Exec(`UPDATE users SET full_name = 'User 100' WHERE id = 100`)

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
	// Cleanup: restore data to original state
	_, _ = db.Exec(`UPDATE users SET full_name = 'User 90' WHERE id = 90`)
	_, _ = db.Exec(`UPDATE products SET name = 'Product 90' WHERE id = 90`)

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

	// Cleanup: restore tags to original state
	_, _ = db.Exec(`UPDATE products SET tags = list_value('Tag 1', 'Tag 2', 'Tag 3', 'Tag 4', 'Tag 5') WHERE id = 100`)

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

	// Cleanup: restore tags to original state
	_, _ = db.Exec(`UPDATE products SET tags = list_value('Tag 1', 'Tag 2', 'Tag 3', 'Tag 4', 'Tag 5') WHERE id = 100`)

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
	const insertID1 = 980001
	const insertID2 = 980002

	// Insert two throwaway users
	insGql := `mutation { users(insert: $data) { id } }`

	res1, err := gj.GraphQL(ctx, insGql,
		json.RawMessage(fmt.Sprintf(`{"data":{"id":%d,"full_name":"Del User A","email":"del_a_multi@test.com"}}`, insertID1)), nil)
	require.NoError(t, err)
	id1, err := parseInsertedIDLiteral(res1.Data)
	if err != nil {
		id1 = strconv.Itoa(insertID1)
		err = nil
	}
	require.NoError(t, err)

	res2, err := gj.GraphQL(ctx, insGql,
		json.RawMessage(fmt.Sprintf(`{"data":{"id":%d,"full_name":"Del User B","email":"del_b_multi@test.com"}}`, insertID2)), nil)
	require.NoError(t, err)
	id2, err := parseInsertedIDLiteral(res2.Data)
	if err != nil {
		id2 = strconv.Itoa(insertID2)
		err = nil
	}
	require.NoError(t, err)

	// Delete both with multi-alias
	delGql := fmt.Sprintf(`mutation {
		d1: users(delete: true, where: { id: { eq: %s } }) { id }
		d2: users(delete: true, where: { id: { eq: %s } }) { id }
	}`, id1, id2)

	res, err := gj.GraphQL(ctx, delGql, nil, nil)
	require.NoError(t, err)

	var result map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Contains(t, result, "d1")
	require.Contains(t, result, "d2")

	// Verify both rows were actually deleted.
	checkGql := fmt.Sprintf(`query {
		u1: users(where: { id: { eq: %s } }) { id }
		u2: users(where: { id: { eq: %s } }) { id }
	}`, id1, id2)
	checkRes, err := gj.GraphQL(ctx, checkGql, nil, nil)
	require.NoError(t, err)

	var check map[string][]map[string]any
	require.NoError(t, json.Unmarshal(checkRes.Data, &check))
	require.Empty(t, check["u1"])
	require.Empty(t, check["u2"])
}
