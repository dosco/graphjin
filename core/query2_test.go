package core_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dosco/graphjin/core"
	"github.com/stretchr/testify/assert"
)

func TestQuery(t *testing.T) {
	t.Run("queryWithVariableLimit", queryWithVariableLimit)
}

func queryWithVariableLimit(t *testing.T) {
	gql := `query {
		products(limit: $limit) {
			id
		}
	}`

	vars := json.RawMessage(`{
		"limit": 10
	}`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Error(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars, nil)

	switch dbType {
	case "mysql":
		assert.NotNil(t, err)
	default:
		exp := `{"products": [{"id": 1}, {"id": 2}, {"id": 3}, {"id": 4}, {"id": 5}, {"id": 6}, {"id": 7}, {"id": 8}, {"id": 9}, {"id": 10}]}`
		got := string(res.Data)
		assert.Equal(t, got, exp, "should equal")
	}
}

/*
var benchGQL = `query {
	products(
		# returns only 30 items
		limit: $limit,

		# starts from item 10, commented out for now
		# offset: 10,

		# orders the response items by highest price
		order_by: { price: desc },

		# only items with an id >= 30 and < 30 are returned
		where: { id: { and: { greater_or_equals: 20, lt: 28 } } }) {
		id
		NAME
		price
		users {
			full_name
			picture : avatar
			email
			category_counts {
				count
				category {
					name
				}
			}
		}
		categories {
			id
			name
		}
	}
}`

func Example_veryComplexQuery() {
	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	conf.Tables = []core.Table{
		{
			Name:  "category_counts",
			Table: "users",
			Type:  "json",
			Columns: []core.Column{
				{Name: "category_id", Type: "int", ForeignKey: "categories.id"},
				{Name: "count", Type: "int"},
			},
		},
		{
			Name:    "products",
			Columns: []core.Column{{Name: "category_ids", ForeignKey: "categories.id"}},
		},
	}

	vars := json.RawMessage(`{
		"limit": 10
	}`)

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		fmt.Println(">", err)
		return
	}

	res, err := gj.GraphQL(context.Background(), benchGQL, vars, nil)
	fmt.Println(res.SQL())
	if err != nil {
		fmt.Println(">", err)
		return
	}

	fmt.Println(string(res.Data))
	// Output: blah
}

var resultJSON json.RawMessage

func BenchmarkCompile(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		vars := json.RawMessage(`{
			"limit": 10
		}`)

		conf := &core.Config{DBType: dbType, DisableAllowList: true}
		conf.Tables = []core.Table{{
			Name:  "category_counts",
			Table: "users",
			Type:  "json",
			Columns: []core.Column{
				{Name: "category_id", Type: "int", ForeignKey: "categories.id"},
				{Name: "count", Type: "int"},
			},
		}}

		gj, err := core.NewGraphJin(conf, db)
		if err != nil {
			b.Error(err)
		}

		res, err := gj.GraphQL(context.Background(), benchGQL, vars, nil)
		if err != nil {
			b.Fatal(err)
		}

		resultJSON = res.Data
	}
}
*/
